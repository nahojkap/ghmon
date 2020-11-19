package ghmon

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os/exec"
	"time"
)

type GHMon struct {
	user                 *User
	pullRequestListeners []func(pullRequests []PullRequest)
	pullRequests         []PullRequest
	statusListeners      []func(status string)
}

type User struct {
	Id uint32
	Username string
}

func NewPullRequest(id uint32, title string ,htmURL string, pullRequestURL string, creator *User, createdAt time.Time, updatedAt time.Time) PullRequest {
	htmURLURL, err := url.Parse(htmURL)
	if err != nil {
		log.Fatal(err)
	}
	pullRequestURLURL , err := url.Parse(pullRequestURL)
	pullRequest := PullRequest{Id: id, Creator: creator, Title: title, Body: "", HtmlURL: htmURLURL, CreatedAt: createdAt, UpdatedAt: updatedAt, PullRequestURL: pullRequestURLURL}
	return pullRequest
}

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
}

func NewGHMon() *GHMon {
	ghm := GHMon{}
	return &ghm
}

func (ghm *GHMon) AddStatusListener(statusListener func(statusUpdate string)) {
	ghm.statusListeners = append(ghm.statusListeners, statusListener)
}


func (ghm *GHMon) AddPullRequestListener(pullRequestListener func(pullRequests []PullRequest)) {
	ghm.pullRequestListeners = append(ghm.pullRequestListeners, pullRequestListener)
}

func (ghm *GHMon) monitorGithub() {
	for {
		go ghm.RetrievePullRequests()
		time.Sleep(15 * time.Minute)
	}
}

func (ghm *GHMon) Initialize() {

	if ghm.IsLoggedIn() {
		ghm.statusListeners[0]("logged in, retrieving user")
		user := ghm.RetrieveUser()
		ghm.statusListeners[0](fmt.Sprintf("Running as %s", user.Username))
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


func MakeAPIRequest(apiParams string) map[string]interface{} {
	cmd := exec.Command("gh","api", apiParams)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	b, _ := ioutil.ReadAll(stdout)
	//out := string(b)

	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}

	var result map[string]interface{}

	err = json.Unmarshal([]byte(b), &result)
	if err != nil {
		log.Fatal(err)
	}
	return result

}

func MakeAPIRequestForArray(apiParams string) []interface{} {
	cmd := exec.Command("gh","api", apiParams)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	b, _ := ioutil.ReadAll(stdout)
	//out := string(b)

	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}

	var result []interface{}
	json.Unmarshal([]byte(b), &result)

	return result

}

func (ghm *GHMon)IsLoggedIn() bool {

	cmd := exec.Command("gh","auth", "status")
	_, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}

	return err == nil
}

func (ghm *GHMon) RetrieveUser() *User {

	if ghm.user != nil {
		return ghm.user
	}

	// Retrieve the current logged in user
	result := MakeAPIRequest("/user")

	ghm.user = &User{uint32(result["id"].(float64)), result["login"].(string)}

	return ghm.user


}

func (ghm *GHMon) RetrievePullRequests() {

	ghm.statusListeners[0]("fetching pull requests")

	// result := MakeAPIRequest("/search/issues?q=is:open+is:pr+review-requested:" + ghm.user.Username + "+archived:false")
	result := MakeAPIRequest("/search/issues?q=is:open+is:pr+repo:nahojkap/ghmon-test-project+archived:false")

	pullRequestItems := result["items"].([]interface{})
	count := len(pullRequestItems)

	ghm.statusListeners[0](fmt.Sprintf("Fetched %d pull requests", count))

	ghm.pullRequests = make([]PullRequest, count)

	for i, pullRequestItem := range pullRequestItems {

		item := pullRequestItem.(map[string]interface{})
		pullRequestId := uint32(item["id"].(float64))

		user := item["user"].(map[string]interface{})

		createdAt,_ := time.Parse(time.RFC3339, item["created_at"].(string))
		updatedAt,_ := time.Parse(time.RFC3339, item["updated_at"].(string))

		pullRequest := item["pull_request"].(map[string]interface{})

		creator := &User{uint32(user["id"].(float64)),user["login"].(string)}

		ghm.pullRequests[i] = NewPullRequest(pullRequestId,item["title"].(string), item["html_url"].(string), pullRequest["url"].(string),creator, createdAt, updatedAt)
		ghm.statusListeners[0](fmt.Sprintf("processing pull request %d", ghm.pullRequests[i].Id))

		go ghm.addPullRequestReviewers(&ghm.pullRequests[i])

		if body, ok := item["body"]; ok {
			ghm.pullRequests[i].Body = body.(string)
		}

	}

	if len(ghm.pullRequests) > 0 {
		for _, pullRequestListener := range ghm.pullRequestListeners {
			pullRequestListener(ghm.pullRequests)
		}
	}

	ghm.statusListeners[0]("idle")

}

func (ghm *GHMon) addPullRequestReviewers(pullRequest *PullRequest) {

	// Use the pullRequest URL but strip out the https://api.github.com/ part
	pullRequestResult := MakeAPIRequest(pullRequest.PullRequestURL.Path)
	requestedReviewers := pullRequestResult["requested_reviewers"].([]interface{})

	pullRequest.PullRequestReviews = make(map[uint32][]PullRequestReview,0)

	for _, requestedReviewerItem := range requestedReviewers {
		requestedReviewer := requestedReviewerItem.(map[string]interface{})
		id := uint32(requestedReviewer["id"].(float64))
		if _, ok := pullRequest.PullRequestReviews[id]; !ok {
			pullRequest.PullRequestReviews[id] = make([]PullRequestReview,0)
		}
		user := &User{uint32(requestedReviewer["id"].(float64)), requestedReviewer["login"].(string)}
		pullRequest.PullRequestReviews[id] = append(pullRequest.PullRequestReviews[id],PullRequestReview{User: user,Status: "REQUESTED"})
	}

	pullRequestReviewResult := MakeAPIRequestForArray(pullRequest.PullRequestURL.Path + "/reviews")
	for _, reviewItem := range pullRequestReviewResult {
		requestedReviewer := reviewItem.(map[string]interface{})
		user := requestedReviewer["user"].(map[string]interface{})

		id := uint32(user["id"].(float64))

		state := requestedReviewer["state"].(string)
		pullRequestReview := PullRequestReview{User: &User{id, user["login"].(string)}, Status: state}

		if _, ok := pullRequest.PullRequestReviews[id]; !ok {
			pullRequest.PullRequestReviews[id] = make([]PullRequestReview,0)
		}

		pullRequest.PullRequestReviews[id] = append(pullRequest.PullRequestReviews[id], pullRequestReview)

		if timeString, ok := requestedReviewer["submitted_at"].(string); ok {
			pullRequestReview.SubmittedAt,_ = time.Parse(time.RFC3339, timeString)
		}

	}

}