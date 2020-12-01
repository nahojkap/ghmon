package ghmon

import (
	"encoding/json"
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"io/ioutil"
	"log"
	"net/url"
	"os/exec"
	"sync"
	"time"
)

type Configuration struct {
	OwnQuery string `split_words:"true"`
	ReviewQuery string `split_words:"true"`
	RefreshInterval time.Duration `default:"15m" split_words:"true"`
}

type GHMon struct {
	user                 *User
	pullRequests map[uint32]*PullRequest
	myPullRequests map[uint32]*PullRequest
	events chan Event
	configuration Configuration
}

type User struct {
	Id uint32
	Username string
}

type PullRequestType int

const (
	Own PullRequestType = iota
	Reviewer
)

type PullRequestReview struct {
	User *User
	Status string
	SubmittedAt time.Time
}

type PullRequest struct {
	Id uint32
	Creator *User
	Title string
	Body string
	HtmlURL *url.URL
	PullRequestURL *url.URL
	CreatedAt time.Time
	UpdatedAt time.Time
	PullRequestReviews map[uint32][]PullRequestReview
	PullRequestType PullRequestType
	Lock sync.Mutex
}

type EventType int

const (
	Status EventType = iota
	PullRequestsUpdates
	PullRequestUpdated
)


type Event struct {
	eventType EventType
	payload interface{}
}

func NewGHMon() *GHMon {
	ghm := GHMon{}

	ghm.events = make(chan Event)
	err := envconfig.Process("ghmon", &ghm.configuration)
	if err != nil {
		log.Fatal("Error extracting environment variables")
	}
	return &ghm
}

func (ghm *GHMon)Events() <-chan Event {
	return ghm.events
}

func (ghm *GHMon) monitorGithub() {
	for {
		go ghm.RetrievePullRequests()
		go ghm.RetrieveMyPullRequests()
		time.Sleep(15 * time.Minute)
	}
}

func (ghm *GHMon) Initialize() {

	if ghm.IsLoggedIn() {
		ghm.events <- Event{eventType: Status, payload: "logged in, retrieving user"}
		user := ghm.RetrieveUser()
		ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("Running as %s", user.Username)}
		go ghm.monitorGithub()
	}
}

func (ghm *GHMon) HasValidSetup() bool {

	_,err := exec.LookPath("gh")
	if err != nil {
		log.Fatal("installing 'gh' is in your future ...\n")
		return false
	}
	return true
}


func makeAPIRequest(apiParams string) map[string]interface{} {
	cmd := exec.Command("gh","api", apiParams)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal("Error getting stdout pipe", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal("Error starting gh", err)
	}

	b, _ := ioutil.ReadAll(stdout)

	if err := cmd.Wait(); err != nil {
		log.Fatal("Error waiting for gh to complete",err)
	}

	var result map[string]interface{}

	err = json.Unmarshal(b, &result)
	if err != nil {
		log.Fatal("Error unmarshalling response", err)
	}
	return result

}

func MakeAPIRequestForArray(apiParams string) []interface{} {
	cmd := exec.Command("gh","api", apiParams)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal("Error getting stdout pipe", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal("Error starting gh", err)
	}

	b, _ := ioutil.ReadAll(stdout)

	if err := cmd.Wait(); err != nil {
		log.Fatal("Error waiting for gh to complete",err)
	}

	var result []interface{}
	if err = json.Unmarshal(b, &result); err != nil {
		log.Fatal("Error unmarshalling response", err)
	}

	return result

}

func (ghm *GHMon)IsLoggedIn() bool {

	cmd := exec.Command("gh","auth", "status")
	_, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal("Error getting stdout pipe", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal("Error starting gh", err)
	}

	if err := cmd.Wait(); err != nil {
		log.Fatal("Error waiting for gh to complete",err)
	}

	return true
}

func (ghm *GHMon) RetrieveUser() *User {

	if ghm.user != nil {
		return ghm.user
	}

	// Retrieve the current logged in user
	result := makeAPIRequest("/user")

	ghm.user = &User{uint32(result["id"].(float64)), result["login"].(string)}

	return ghm.user


}

func (ghm *GHMon) parsePullRequestQueryResult(pullRequestType PullRequestType, pullRequests map[uint32]*PullRequest, result map[string]interface{}, lock *sync.Mutex) {

	pullRequestItems := result["items"].([]interface{})
	count := len(pullRequestItems)

	ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("Fetched %d pull requests", count)}

	for _, pullRequestItem := range pullRequestItems {

		item := pullRequestItem.(map[string]interface{})
		pullRequestId := uint32(item["id"].(float64))

		lock.Lock()
		// If we already have the item, loop around
		if _,ok := pullRequests[pullRequestId]; ok {
			lock.Unlock()
			continue
		}

		user := item["user"].(map[string]interface{})

		createdAt,_ := time.Parse(time.RFC3339, item["created_at"].(string))
		updatedAt,_ := time.Parse(time.RFC3339, item["updated_at"].(string))

		pullRequestObj := item["pull_request"].(map[string]interface{})

		creator := &User{Id: uint32(user["id"].(float64)),Username: user["login"].(string)}

		htmURLURL, err := url.Parse(item["html_url"].(string))
		if err != nil {
			log.Fatal("Could not parse HTML url", err)

		}
		pullRequestURLURL , err := url.Parse(pullRequestObj["url"].(string))
		if err != nil {
			log.Fatal("Could not parse url", err)
		}

		pullRequest := PullRequest {
			Id: pullRequestId, Title: item["title"].(string), HtmlURL: htmURLURL, PullRequestURL: pullRequestURLURL,
			Creator: creator, CreatedAt: createdAt, UpdatedAt: updatedAt,PullRequestType: pullRequestType,
		}
		if body, ok := item["body"]; ok {
			if body != nil {
				pullRequest.Body = body.(string)
			}
		}

		pullRequests[pullRequestId] = &pullRequest

		lock.Unlock()

		ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("processing pull request %d", pullRequests[pullRequestId].Id)}

		go ghm.addPullRequestReviewers(pullRequests[pullRequestId])


	}

}

func (ghm *GHMon) RetrievePullRequests() {

	ghm.events <- Event{eventType: Status, payload: "fetching pull requests"}
	ghm.pullRequests = make(map[uint32]*PullRequest, 0)

	var lock sync.Mutex
	if ghm.configuration.ReviewQuery != "" {
		// Need the set of PR that has been 'seen' by the user as well as those requested
		result := makeAPIRequest("/search/issues?q=" + ghm.configuration.ReviewQuery)
		ghm.parsePullRequestQueryResult(Reviewer,ghm.pullRequests,result, &lock)
	} else {

		var waitGroup sync.WaitGroup
		waitGroup.Add(2)

		retrieveRequestedPullRequests := func() {
			// Need the set of PR that has been 'seen' by the user as well as those requested
			result := makeAPIRequest("/search/issues?q=is:open+is:pr+review-requested:@me+archived:false")
			ghm.parsePullRequestQueryResult(Reviewer,ghm.pullRequests,result, &lock)
			waitGroup.Done()
		}

		retrieveReviewedByPullRequests := func() {
			result := makeAPIRequest("/search/issues?q=is:open+is:pr+reviewed-by:@me+archived:false")
			ghm.parsePullRequestQueryResult(Reviewer, ghm.pullRequests, result, &lock)
			waitGroup.Done()
		}

		go retrieveRequestedPullRequests()
		go retrieveReviewedByPullRequests()

		waitGroup.Wait()

	}

	if len(ghm.pullRequests) > 0 {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: ghm.pullRequests}
	}

	ghm.events <- Event{eventType: Status, payload: "idle"}
}


func (ghm *GHMon) RetrieveMyPullRequests() {

	var lock sync.Mutex
	ghm.events <- Event{eventType: Status, payload: "fetching my pull requests"}
	ghm.myPullRequests = make(map[uint32]*PullRequest, 0)

	if ghm.configuration.OwnQuery != "" {
		// Need the set of PR that has been 'seen' by the user as well as those requested
		result := makeAPIRequest("/search/issues?q=" + ghm.configuration.OwnQuery)
		ghm.parsePullRequestQueryResult(Own, ghm.myPullRequests,result, &lock)
	} else {
		// Need the set of PR that has been 'seen' by the user as well as those requested
		result := makeAPIRequest("/search/issues?q=is:open+is:pr+author:@me+archived:false")
		ghm.parsePullRequestQueryResult(Own,ghm.myPullRequests,result, &lock)
	}

	if len(ghm.myPullRequests) > 0 {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: ghm.myPullRequests}
	}

	ghm.events <- Event{eventType: Status, payload: "idle"}

}


func (ghm *GHMon) addPullRequestReviewers(pullRequest *PullRequest) {

	pullRequest.PullRequestReviews = make(map[uint32][]PullRequestReview,0)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	retriveReviewers := func() {
		// Use the pullRequest URL but strip out the https://api.github.com/ part
		pullRequestResult := makeAPIRequest(pullRequest.PullRequestURL.Path)
		requestedReviewers := pullRequestResult["requested_reviewers"].([]interface{})

		for _, requestedReviewerItem := range requestedReviewers {
			requestedReviewer := requestedReviewerItem.(map[string]interface{})
			id := uint32(requestedReviewer["id"].(float64))
			user := &User{uint32(requestedReviewer["id"].(float64)), requestedReviewer["login"].(string)}

			pullRequest.Lock.Lock()
			if _, ok := pullRequest.PullRequestReviews[id]; !ok {
				pullRequest.PullRequestReviews[id] = make([]PullRequestReview,0)
			}
			pullRequest.PullRequestReviews[id] = append(pullRequest.PullRequestReviews[id],PullRequestReview{User: user,Status: "REQUESTED"})
			pullRequest.Lock.Unlock()
		}
		waitGroup.Done()
	}

	retriveReviews := func() {
		pullRequestReviewResult := MakeAPIRequestForArray(pullRequest.PullRequestURL.Path + "/reviews")
		for _, reviewItem := range pullRequestReviewResult {
			requestedReviewer := reviewItem.(map[string]interface{})
			user := requestedReviewer["user"].(map[string]interface{})

			id := uint32(user["id"].(float64))

			state := requestedReviewer["state"].(string)
			pullRequestReview := PullRequestReview{User: &User{id, user["login"].(string)}, Status: state}
			if timeString, ok := requestedReviewer["submitted_at"].(string); ok {
				pullRequestReview.SubmittedAt, _ = time.Parse(time.RFC3339, timeString)
			}

			pullRequest.Lock.Lock()
			if _, ok := pullRequest.PullRequestReviews[id]; !ok {
				pullRequest.PullRequestReviews[id] = make([]PullRequestReview, 0)
			}
			pullRequest.PullRequestReviews[id] = append(pullRequest.PullRequestReviews[id], pullRequestReview)
			pullRequest.Lock.Unlock()

		}
		waitGroup.Done()
	}

	go retriveReviewers()
	go retriveReviews()

	waitGroup.Wait()

	ghm.events <- Event{eventType: PullRequestUpdated, payload: pullRequest}
}