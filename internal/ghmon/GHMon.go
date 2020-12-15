package ghmon

import (
	"encoding/json"
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"io/ioutil"
	"log"
	"net/url"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type Configuration struct {
	OwnQuery string `split_words:"true"`
	ReviewQuery string `split_words:"true"`
	RefreshInterval time.Duration `default:"15m" split_words:"true"`
}

type GHMon struct {
	cachedRepoInformation  map[*url.URL]*Repo
	user                   *User
	pullRequestWrappers    map[uint32]*PullRequestWrapper
	myPullRequestsWrappers map[uint32]*PullRequestWrapper
	events                 chan Event
	configuration          Configuration
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

type Repo struct {
	Id uint32
	Name string
	FullName string
	Description string
	Url *url.URL
}

type PullRequest struct {
	Id uint32
	Repo *Repo
	Creator *User
	Title string
	Body string
	HtmlURL *url.URL
	PullRequestURL *url.URL
	CreatedAt time.Time
	UpdatedAt time.Time
	PullRequestReviews map[uint32][]*PullRequestReview
	PullRequestReviewsByPriority [][]*PullRequestReview
	PullRequestType PullRequestType
	Lock sync.Mutex
}

type PullRequestScore struct {
	Seen bool
	AgeSec uint32
	Approvals uint
	Comments uint
	ChangesRequested uint
	NumReviewers uint
}

type PullRequestWrapper struct {
	Id uint32
	PullRequestType PullRequestType
	FirstSeen time.Time
	Seen bool
	Score PullRequestScore
	PullRequest *PullRequest
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
	ghm := GHMon{
		cachedRepoInformation: make(map[*url.URL]*Repo,0),
		events : make(chan Event,5),
	}
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
		log.Printf("Error while waiting: %s", b)
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

func (ghm *GHMon) getRepo(repoURL *url.URL) *Repo {

	if repo, ok := ghm.cachedRepoInformation[repoURL]; ok {
		return repo
	}

	// Use the URL but strip out the https://api.github.com/ part
	result := makeAPIRequest(repoURL.Path)
	id := uint32(result["id"].(float64))
	name := result["name"].(string)
	fullName := result["full_name"].(string)
	repo := Repo{
		Id: id, Name: name, FullName: fullName,
	}
	if description, ok := result["description"]; ok {
		if description != nil {
			repo.Description = description.(string)
		}
	}
	ghm.cachedRepoInformation[repoURL] = &repo
	return &repo

}


func (ghm *GHMon) parsePullRequestQueryResult(pullRequestType PullRequestType, pullRequestsWrappers map[uint32]*PullRequestWrapper, result map[string]interface{}, lock *sync.Mutex) {

	pullRequestItems := result["items"].([]interface{})
	count := len(pullRequestItems)

	ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("Fetched %d pull requests", count)}

	for _, pullRequestItem := range pullRequestItems {

		item := pullRequestItem.(map[string]interface{})
		pullRequestId := uint32(item["id"].(float64))

		lock.Lock()
		// If we already have the item, loop around
		if _,ok := pullRequestsWrappers[pullRequestId]; ok {
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

		repoURLURL , err := url.Parse(item["repository_url"].(string))
		if err != nil {
			log.Fatal("Could not parse repo url", err)
		}
		repo := ghm.getRepo(repoURLURL)

		pullRequest := PullRequest {
			Id: pullRequestId, Title: item["title"].(string), HtmlURL: htmURLURL, PullRequestURL: pullRequestURLURL,
			Creator: creator, CreatedAt: createdAt, UpdatedAt: updatedAt,PullRequestType: pullRequestType,
			Repo: repo,
		}
		if body, ok := item["body"]; ok {
			if body != nil {
				pullRequest.Body = body.(string)
			}
		}

		currentPullRequestWrapper := ghm.getCurrentPullRequestWrapper(pullRequest.Id)
		pullRequestWrapper := ghm.mergePullRequestWrappers(&pullRequest, currentPullRequestWrapper)


		pullRequestsWrappers[pullRequestId] = pullRequestWrapper

		lock.Unlock()

		ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("processing pull request %d", pullRequestsWrappers[pullRequestId].Id)}

		go ghm.addPullRequestReviewers(pullRequestsWrappers[pullRequestId])


	}

}


func  (ghm *GHMon) pullRequestReviewStatusToInt(pullRequestReview *PullRequestReview) int {
	switch pullRequestReview.Status {
	case "APPROVED":
		return 12
	case "COMMENTED":
		return 15
	case "CHANGES_REQUESTED":
		return 10
	case "PENDING":
		return 17
	case "REQUESTED":
		return 20
	default:
		return 50
	}
}


func (ghm *GHMon)extractMostImportantFirst(pullRequestReviews []*PullRequestReview) *PullRequestReview {

	// FIXME: The reviews need to be sorted by date as well - e.g. its possible to have an approval *after* a comment
	// FIXME: and then another comment, which officially requires a re-review
	// FIXME: Or should it be that the presence of any approval should be signalled differently and only be
	// FIXME: cancelled if there is a request for change?

	sort.Slice(pullRequestReviews, func (i, j int) bool {
		if (pullRequestReviews[i].Status == "APPROVED" || pullRequestReviews[i].Status == "CHANGES_REQUESTED") && (pullRequestReviews[j].Status == "APPROVED" || pullRequestReviews[j].Status == "CHANGES_REQUESTED") {
			return pullRequestReviews[i].SubmittedAt.After(pullRequestReviews[j].SubmittedAt)
		}
		return ghm.pullRequestReviewStatusToInt(pullRequestReviews[i]) < ghm.pullRequestReviewStatusToInt(pullRequestReviews[j])
	})

	return pullRequestReviews[0]

}


func (ghm *GHMon) updatePullRequestScore(pullRequestWrapper *PullRequestWrapper) {

	pullRequestScore := PullRequestScore{}

	importantPullRequestReviews := make([]*PullRequestReview, 0)
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviews {
		pullRequestReview := ghm.extractMostImportantFirst(pullRequestReviews)
		importantPullRequestReviews = append(importantPullRequestReviews, pullRequestReview)
	}

	pullRequestScore.NumReviewers = uint(len(importantPullRequestReviews))
	pullRequestScore.Seen = pullRequestWrapper.Seen
	pullRequestScore.AgeSec = uint32(time.Now().Unix() - pullRequestWrapper.FirstSeen.Unix())

	for _, pullRequestReview := range importantPullRequestReviews {
		switch pullRequestReview.Status {
		case "APPROVED" : pullRequestScore.Approvals++
		case "CHANGES_REQUESTED" : pullRequestScore.ChangesRequested++
		case "COMMENTED" : pullRequestScore.Comments++
		}
	}

	pullRequestWrapper.Score = pullRequestScore

}


func (ghm *GHMon) calculateScore(pullRequestScore PullRequestScore) int {

	if pullRequestScore.ChangesRequested > 0 {
		return -1
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		return 100
	}

	if pullRequestScore.Comments > 0 {
		return 50
	}

	return 150
}

func (ghm *GHMon) getCurrentPullRequestWrapper(pullRequestId uint32) *PullRequestWrapper {

	for _, pullRequestWrapper := range ghm.pullRequestWrappers {
		if pullRequestWrapper.Id == pullRequestId {
			return pullRequestWrapper
		}
	}
	for _, pullRequestWrapper := range ghm.myPullRequestsWrappers {
		if pullRequestWrapper.Id == pullRequestId {
			return pullRequestWrapper
		}
	}
	return nil
}

func (ghm *GHMon) mergePullRequestWrappers(pullRequest *PullRequest, currentPullRequestWrapper *PullRequestWrapper) *PullRequestWrapper {
	pullRequestWrapper := PullRequestWrapper{Id: pullRequest.Id, PullRequestType: pullRequest.PullRequestType, PullRequest: pullRequest, Score: PullRequestScore{}, Seen: false, FirstSeen: time.Now()}
	if currentPullRequestWrapper != nil {
		pullRequestWrapper.FirstSeen = currentPullRequestWrapper.FirstSeen
		pullRequestWrapper.Score = currentPullRequestWrapper.Score
		pullRequestWrapper.Seen = currentPullRequestWrapper.Seen
	}
	ghm.updatePullRequestScore(&pullRequestWrapper)
	return &pullRequestWrapper
}

func (ghm *GHMon) sortPullRequestWrappers(pullRequestWrappers map[uint32]*PullRequestWrapper) []*PullRequestWrapper {

	sortedPullRequestWrappers := make([]*PullRequestWrapper, 0)
	for _, loadedPullRequest  := range pullRequestWrappers {
		sortedPullRequestWrappers = append(sortedPullRequestWrappers, loadedPullRequest)
	}

	sort.Slice(sortedPullRequestWrappers, func(i,j int) bool {

		left := sortedPullRequestWrappers[i]
		right := sortedPullRequestWrappers[j]

		leftScore := ghm.calculateScore(left.Score)
		rightScore := ghm.calculateScore(right.Score)

		if leftScore == rightScore {
			return left.PullRequest.Id < right.PullRequest.Id
		}

		return leftScore < rightScore

	})

	return sortedPullRequestWrappers

}


func (ghm *GHMon) RetrievePullRequests() {

	ghm.events <- Event{eventType: Status, payload: "fetching pull requests"}
	pullRequestWrappers := make(map[uint32]*PullRequestWrapper, 0)

	var lock sync.Mutex
	if ghm.configuration.ReviewQuery != "" {
		// Need the set of PR that has been 'seen' by the user as well as those requested
		result := makeAPIRequest("/search/issues?q=" + ghm.configuration.ReviewQuery)
		ghm.parsePullRequestQueryResult(Reviewer,pullRequestWrappers,result, &lock)
	} else {

		var waitGroup sync.WaitGroup
		waitGroup.Add(2)

		retrieveRequestedPullRequests := func() {
			// Need the set of PR that has been 'seen' by the user as well as those requested
			result := makeAPIRequest("/search/issues?q=is:open+is:pr+review-requested:@me+archived:false")
			ghm.parsePullRequestQueryResult(Reviewer,pullRequestWrappers,result, &lock)
			waitGroup.Done()
		}

		retrieveReviewedByPullRequests := func() {
			result := makeAPIRequest("/search/issues?q=is:open+is:pr+reviewed-by:@me+archived:false")
			ghm.parsePullRequestQueryResult(Reviewer, pullRequestWrappers, result, &lock)
			waitGroup.Done()
		}

		go retrieveRequestedPullRequests()
		go retrieveReviewedByPullRequests()

		waitGroup.Wait()

	}

	sortedPullRequestWrappers := ghm.sortPullRequestWrappers(pullRequestWrappers)

	changesMade := len(pullRequestWrappers) != len(ghm.pullRequestWrappers)
	ghm.pullRequestWrappers = pullRequestWrappers
	if changesMade {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: sortedPullRequestWrappers}
	}

	ghm.events <- Event{eventType: Status, payload: "idle"}
}


func (ghm *GHMon) RetrieveMyPullRequests() {

	var lock sync.Mutex
	ghm.events <- Event{eventType: Status, payload: "fetching my pull requests"}
	pullRequestWrappers := make(map[uint32]*PullRequestWrapper, 0)

	if ghm.configuration.OwnQuery != "" {
		// Need the set of PR that has been 'seen' by the user as well as those requested
		result := makeAPIRequest("/search/issues?q=" + ghm.configuration.OwnQuery)
		ghm.parsePullRequestQueryResult(Own, pullRequestWrappers,result, &lock)
	} else {
		// Need the set of PR that has been 'seen' by the user as well as those requested
		result := makeAPIRequest("/search/issues?q=is:open+is:pr+author:@me+archived:false")
		ghm.parsePullRequestQueryResult(Own,pullRequestWrappers,result, &lock)
	}

	sortedPullRequestWrappers := ghm.sortPullRequestWrappers(pullRequestWrappers)

	changesMade := len(pullRequestWrappers) != len(ghm.myPullRequestsWrappers)
	ghm.myPullRequestsWrappers = pullRequestWrappers

	if changesMade {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: sortedPullRequestWrappers}
	}

	ghm.events <- Event{eventType: Status, payload: "idle"}

}


func (ghm *GHMon) addPullRequestReviewers(pullRequestWrapper *PullRequestWrapper) {

	pullRequest := pullRequestWrapper.PullRequest
	pullRequest.PullRequestReviews = make(map[uint32][]*PullRequestReview,0)

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
				pullRequest.PullRequestReviews[id] = make([]*PullRequestReview,0)
			}
			pullRequest.PullRequestReviews[id] = append(pullRequest.PullRequestReviews[id],&PullRequestReview{User: user,Status: "REQUESTED"})
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
				pullRequest.PullRequestReviews[id] = make([]*PullRequestReview, 0)
			}
			pullRequest.PullRequestReviews[id] = append(pullRequest.PullRequestReviews[id], &pullRequestReview)
			pullRequest.Lock.Unlock()

		}
		waitGroup.Done()
	}

	go retriveReviewers()
	go retriveReviews()

	waitGroup.Wait()

	ghm.sortPullRequestReviewers(pullRequestWrapper)
	ghm.updatePullRequestScore(pullRequestWrapper)

	ghm.events <- Event{eventType: PullRequestUpdated, payload: pullRequestWrapper}
}

func (ghm *GHMon) sortPullRequestReviewers(pullRequestWrapper *PullRequestWrapper) {

	keys := make([]uint32,0)
	for key := range pullRequestWrapper.PullRequest.PullRequestReviews {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		left := ghm.extractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviews[keys[i]])
		right := ghm.extractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviews[keys[j]])

		leftToInt := ghm.pullRequestReviewStatusToInt(left)
		rightToInt := ghm.pullRequestReviewStatusToInt(right)

		if leftToInt == rightToInt {
			return left.User.Id < right.User.Id
		}
		return leftToInt < rightToInt
	})

	sortedPullRequestReviews := make([][]*PullRequestReview, 0)
	for _,key := range keys {
		sortedPullRequestReviews = append(sortedPullRequestReviews, pullRequestWrapper.PullRequest.PullRequestReviews[key])
	}
	pullRequestWrapper.PullRequest.PullRequestReviewsByPriority = sortedPullRequestReviews

}