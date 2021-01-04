package ghmon

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

type GHMonStorage struct {
	cachedPullRequestFolder string
}

func (ghmStorage *GHMonStorage) createCachedPullRequestWrapperFilename(id uint32) string {
	return filepath.Join(ghmStorage.cachedPullRequestFolder,fmt.Sprintf("%d.json",id))
}

func (ghmStorage *GHMonStorage) loadPullRequest(id uint32, channel chan *PullRequestWrapper) {

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

func (ghmStorage *GHMonStorage) LoadPullRequestWrapper(id uint32) chan *PullRequestWrapper {
	channel := make(chan *PullRequestWrapper,1)
	go ghmStorage.loadPullRequest(id, channel)
	return channel
}

func (ghmStorage *GHMonStorage) StorePullRequestWrapper(pullRequestWrapper *PullRequestWrapper) {

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

