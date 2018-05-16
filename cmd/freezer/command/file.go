// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package command

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"

	"github.com/marcoziti/gringotts"
	"github.com/marcoziti/gringotts/cmd/freezer/models"
)

// GetFileInfoByFilename takes the long way of finding a FileInfo object
// by scanning all FileInfo objects registered for a given user. If a matching
// file is found it is returned and the error value will be null; otherwise
// an error will be set.
// NOTE: implemented like this to support encrypted filenames.
func (s *State) GetFileInfoByFilename(filename string) (foundFile filefreezer.FileInfo, e error) {
	// get the entire file info list so that we can go through each file info
	// and find the right one for a given filename.
	allFileInfos, err := s.GetAllFileHashes()
	if err != nil {
		return foundFile, fmt.Errorf("failed to getall of the file hashes: %v", err)
	}

	// iterate through all of the files
	for _, fi := range allFileInfos {
		decryptedFilename, err := s.DecryptString(fi.FileName)
		if err != nil {
			return foundFile, err
		}

		if decryptedFilename == filename {
			return fi, nil
		}
	}

	return foundFile, fmt.Errorf("could not find the file: %s", filename)
}

// RmFile takes the filename and attempts to find it in the list of filenames
// registered on the storage server for the user. If it does find it, an
// API method is called to delete the object. If dryRun is set to true
// the file removal command is never executed. A non-nil error is returned on failure.
func (s *State) RmFile(filename string, dryRun bool) error {
	fi, err := s.GetFileInfoByFilename(filename)
	if err != nil {
		return err
	}

	if !dryRun {
		target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fi.FileID)
		_, err = s.RunAuthRequest(target, "DELETE", s.AuthToken, nil)
		if err != nil {
			return fmt.Errorf("Failed to remove the file %s: %v", filename, err)
		}
	}

	s.Printf("Removed file: %s\n", filename)

	return nil
}

// RmRxFiles removes files by regular expression matching against the filenames.
// The dryRun argument controls whether or not the actual removeal request is
// sent to the server allowing the user to preview the result of the regex match.
// A non-nil error is returned on failure.
func (s *State) RmRxFiles(pattern string, dryRun bool) error {
	allFiles, err := s.GetAllFileHashes()
	if err != nil {
		return fmt.Errorf("could not get all of the files from the server: %v", err)
	}

	compiledFilter, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("failed to compile the regular expression: %v", err)
	}

	for _, fi := range allFiles {
		plaintextFilename, err := s.DecryptString(fi.FileName)
		if err != nil {
			return fmt.Errorf("failed to decrypt one of the file names: %v", err)
		}

		if compiledFilter.MatchString(plaintextFilename) {
			// only attempt to actually delete when not on a dryRun
			if !dryRun {
				target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fi.FileID)
				_, err = s.RunAuthRequest(target, "DELETE", s.AuthToken, nil)
				if err != nil {
					return fmt.Errorf("Failed to remove the file %s: %v", plaintextFilename, err)
				}
			}

			s.Printf("Removed file: %s\n", plaintextFilename)
		}
	}

	return nil
}

// RmFileByID takes the file id directly and an API method is called to
// delete the object. A non-nil error is returned on failure.
func (s *State) RmFileByID(fileID int) error {
	target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fileID)
	_, err := s.RunAuthRequest(target, "DELETE", s.AuthToken, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file by file ID (%d): %v", fileID, err)
	}

	s.Printf("Removed file by ID: %d\n", fileID)

	return nil
}

// GetFileVersions will return a slice of global version IDs and a matching
// slice of version numbers for the filename provided. A non-nil error is returned on error.
func (s *State) GetFileVersions(filename string) (versions []filefreezer.FileVersionInfo, err error) {
	fi, err := s.GetFileInfoByFilename(filename)
	if err != nil {
		return nil, err
	}

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/%d/versions", s.HostURI, fi.FileID)
	body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the file versions for %s: %v", target, err)
	}

	var r models.FileGetAllVersionsResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the file versions: %v", err)
	}

	return r.Versions, nil
}

// RmFileVersions removes a range of versions (inclusive) from minVersion to
// maxVersion from storage. A non-nil error is returned on failure.
func (s *State) RmFileVersions(filename string, minVersion int, maxVersion int, dryRun bool) error {
	fi, err := s.GetFileInfoByFilename(filename)
	if err != nil {
		return err
	}

	var putReq models.FileDeleteVersionsRequest
	putReq.MinVersion = minVersion
	putReq.MaxVersion = maxVersion

	if maxVersion >= fi.CurrentVersion.VersionNumber {
		return fmt.Errorf("the maxiumum version number cannot be equal or greater than the current version number")
	}

	// get the file id for the filename provided
	if !dryRun {
		target := fmt.Sprintf("%s/api/file/%d/versions", s.HostURI, fi.FileID)
		body, err := s.RunAuthRequest(target, "DELETE", s.AuthToken, putReq)
		if err != nil {
			return fmt.Errorf("Failed to delete the file versions for %s: %v", target, err)
		}

		var r models.FileDeleteVersionsResponse
		err = json.Unmarshal(body, &r)
		if err != nil {
			return fmt.Errorf("Failed to delete the file versions: %v", err)
		}

		if !r.Status {
			return fmt.Errorf("an unknown error caused a failed status to be returned while deleting file versions")
		}
	}
	return nil
}

// RmRxFileVersions removes a range of versions (inclusive) from minVersion to
// maxVersion from storage for all files matching a regexp pattern.
// A non-nil error is returned on failure.
func (s *State) RmRxFileVersions(pattern string, minVersion int, maxVersionStr string, dryRun bool) error {
	allFiles, err := s.GetAllFileHashes()
	if err != nil {
		return fmt.Errorf("could not get all of the files from the server: %v", err)
	}

	compiledFilter, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("failed to compile the regular expression: %v", err)
	}

	for _, fi := range allFiles {
		plaintextFilename, err := s.DecryptString(fi.FileName)
		if err != nil {
			return fmt.Errorf("failed to decrypt one of the file names: %v", err)
		}

		if compiledFilter.MatchString(plaintextFilename) {
			var maxVersion int
			if maxVersionStr == "H~" {
				maxVersion = fi.CurrentVersion.VersionNumber - 1
			} else {
				maxVersion, err = strconv.Atoi(maxVersionStr)
				if err != nil {
					log.Fatalf("Failed to parse the supplied max version as a number: %v", err)
				}
			}

			// silently ignore any file where the max version is >= the current version.
			// a case where this applies is regex matching a file with only one version and
			// supplying "H~" which will then evaluate to 0.
			if maxVersion >= fi.CurrentVersion.VersionNumber {
				continue
			}

			// only attempt to actually delete when not on a dryRun
			if !dryRun {
				var putReq models.FileDeleteVersionsRequest
				putReq.MinVersion = minVersion
				putReq.MaxVersion = maxVersion

				target := fmt.Sprintf("%s/api/file/%d/versions", s.HostURI, fi.FileID)
				body, err := s.RunAuthRequest(target, "DELETE", s.AuthToken, putReq)
				if err != nil {
					return fmt.Errorf("Failed to delete the file versions for %s: %v", plaintextFilename, err)
				}

				var r models.FileDeleteVersionsResponse
				err = json.Unmarshal(body, &r)
				if err != nil {
					return fmt.Errorf("Failed to delete the file versions for %s: %v", plaintextFilename, err)
				}

				if !r.Status {
					return fmt.Errorf("an unknown error caused a failed status to be returned while deleting file versions")
				}
			}

			s.Printf("%s -- successfully removed versions %d to %d.\n", plaintextFilename, minVersion, maxVersion)
		}
	}

	return nil
}

// GetMissingChunksForFile will return a slice of chunk numbers (index starts at zero and
// is local to the specific file) for a given file located by file ID. A non-nil
// error is returned on error.
func (s *State) GetMissingChunksForFile(fileID int) ([]int, error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fileID)
	body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the file's missing chunk list: %v", err)
	}

	var r models.FileGetResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the file's missing chunk list: %v", err)
	}

	return r.MissingChunks, nil
}
