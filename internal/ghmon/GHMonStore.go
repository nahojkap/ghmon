package ghmon

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Storage struct {
	logger *log.Logger
	cachedPullRequestFolder string
}

func (ghmStorage *Storage) createCachedPullRequestWrapperFilename(id uint32) string {
	return filepath.Join(ghmStorage.cachedPullRequestFolder,fmt.Sprintf("%d.json",id))
}

func (ghmStorage *Storage) loadPullRequest(id uint32, channel chan *PullRequestWrapper) {

	pullRequestWrapperFilePath := ghmStorage.createCachedPullRequestWrapperFilename(id)

	if _, err := os.Stat(pullRequestWrapperFilePath); err == nil {
		// If it exist, read it & parse the JSON
		if bytes, err := ioutil.ReadFile(pullRequestWrapperFilePath); err == nil {

			var pullRequestWrapper PullRequestWrapper
			if err = json.Unmarshal(bytes, &pullRequestWrapper); err == nil {
				channel <- &pullRequestWrapper
			}

		}


	} else if os.IsNotExist(err) {
		// path/to/whatever does *not* exist
	} else {
		// Could be a path issue, or read-permission
	}


	channel <- nil

}

func (ghmStorage *Storage) DeletePullRequestWrapper(id uint32) {
	pullRequestWrapperFilePath := ghmStorage.createCachedPullRequestWrapperFilename(id)
	err := os.Remove(pullRequestWrapperFilePath)
	if err != nil {

	}
}

func (ghmStorage *Storage) LoadPullRequestWrapper(id uint32) chan *PullRequestWrapper {
	channel := make(chan *PullRequestWrapper,1)
	go ghmStorage.loadPullRequest(id, channel)
	return channel
}

func (ghmStorage *Storage) StorePullRequestWrapper(pullRequestWrapper *PullRequestWrapper) {

	pullRequestWrapperFilePath := ghmStorage.createCachedPullRequestWrapperFilename(pullRequestWrapper.Id)

	if bytes, err := json.Marshal(pullRequestWrapper); err == nil {

		if _, err := os.Stat(pullRequestWrapperFilePath); err == nil || os.IsNotExist(err) {
			// If it exist, write out the JSON
			if err = ioutil.WriteFile(pullRequestWrapperFilePath, bytes, 0644); err == nil {
				// All good :)
			}
		} else if os.IsNotExist(err) {
			//
		} else {
			// Could be a path issue, or read-permission
		}

	}

}

func (ghmStorage *Storage) loadStoredPullRequestIdentifiers() ([]uint32, error) {

	// List all the files in the pull request folder

	f, err := os.Open(ghmStorage.cachedPullRequestFolder)
	if err != nil {
		return nil,err
	}
	names, err := f.Readdirnames(0)
	if err != nil {
		return nil,err
	}
	identifiers := make([]uint32, 0)
	for _,name := range names {
		parseUint, _ := strconv.ParseUint(strings.Split(name, ".json")[0], 10, 32)
		identifiers = append(identifiers, uint32(parseUint))
	}
	return identifiers, nil
}

