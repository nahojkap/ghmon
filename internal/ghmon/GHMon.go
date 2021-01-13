package ghmon

import (
	"encoding/json"
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"github.com/kirsle/configdir"
)

type Configuration struct {
	OwnQuery string `split_words:"true"`
	ReviewQuery string `split_words:"true"`
	RefreshInterval time.Duration `default:"15m" split_words:"true"`
	SynchronousRequests bool `default:"false" split_words:"true"`
}

type GHMon struct {
	configPath              string
	cachedPullRequestFolder string
	cachedRepoInformation   map[*url.URL]*Repo
	user                    *User
	pullRequestWrappers     map[uint32]*PullRequestWrapper
	myPullRequestsWrappers  map[uint32]*PullRequestWrapper
	events                  chan Event
	configuration           Configuration
	store                   *GHMonStorage
	logger                  *log.Logger
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
	Id                           uint32
	Repo                         *Repo
	Creator                      *User
	Title                        string
	Body                         string
	HtmlURL                      *url.URL
	PullRequestURL               *url.URL
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
	PullRequestReviewsByUser     map[uint32][]*PullRequestReview
	PullRequestReviewsByPriority [][]*PullRequestReview
	PullRequestType              PullRequestType
	Lock                         sync.Mutex
}

type PullRequestScore struct {
	Total float32
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

type PullRequestsUpdatesEvent struct {
	pullRequestType     PullRequestType
	pullRequestWrappers []*PullRequestWrapper
}

type Event struct {
	eventType EventType
	payload interface{}
}

func NewGHMon() *GHMon {

	var configuration Configuration
	err := envconfig.Process("ghmon", &configuration)
	if err != nil {
		log.Fatal("Error extracting environment variables")
	}

	configPath := configdir.LocalConfig("ghmon")
	err = configdir.MakePath(configPath)
	if err != nil {
		panic(err)
	}

	cachedPullRequestFolder := filepath.Join(configPath, "pull-requests")
	err = configdir.MakePath(cachedPullRequestFolder)
	if err != nil {
		panic(err)
	}

	logDirectory := filepath.Join(configPath,"logs")
	err = configdir.MakePath(logDirectory)
	if err != nil {
		panic(err)
	}
	logFile := filepath.Join(logDirectory, "ghmon.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	// defer f.Close()

	logger := log.New(f, "", log.LstdFlags)

	logger.Printf("Initializing GHMon")
	logger.Printf("Synchronous Requests: %t", configuration.SynchronousRequests)
	logger.Printf("Refresh Interval: %s", configuration.RefreshInterval)

	ghm := GHMon{
		cachedRepoInformation: make(map[*url.URL]*Repo,0),
		events : make(chan Event,5),
		store: &GHMonStorage{
			cachedPullRequestFolder: cachedPullRequestFolder,
		},
		cachedPullRequestFolder: cachedPullRequestFolder,
		configPath: configPath,
		logger : logger,
	}

	return &ghm
}

func (ghm *GHMon) Events() <-chan Event {
	return ghm.events
}

func (ghm *GHMon) monitorGithub() {
	for {
		if ghm.configuration.SynchronousRequests {
			ghm.RetrievePullRequests()
			ghm.RetrieveMyPullRequests()
		} else {
			go ghm.RetrievePullRequests()
			go ghm.RetrieveMyPullRequests()
		}
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
		log.Fatal("installing 'gh' is in your future ...")
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

	ghm.logger.Println("Checking logged in status")
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

		lock.Lock()
		currentPullRequestWrapper := ghm.getCurrentPullRequestWrapper(pullRequest.Id)
		pullRequestWrapper := ghm.mergePullRequestWrappers(&pullRequest, currentPullRequestWrapper)
		pullRequestsWrappers[pullRequestId] = pullRequestWrapper
		lock.Unlock()

		ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("processing pull request %d", pullRequestsWrappers[pullRequestId].Id)}

		if ghm.configuration.SynchronousRequests {
			ghm.addPullRequestReviewers(pullRequestsWrappers[pullRequestId])
		} else {
			go ghm.addPullRequestReviewers(pullRequestsWrappers[pullRequestId])
		}



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
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
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

func (ghm *GHMon) MarkSeen(pullRequestWrapper *PullRequestWrapper) {
	pullRequestWrapper.Seen = true
	go ghm.store.StorePullRequestWrapper(pullRequestWrapper)
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

	channel := ghm.store.LoadPullRequestWrapper(pullRequestId)

	return <-channel

}

func (ghm *GHMon) mergePullRequestWrappers(pullRequest *PullRequest, currentPullRequestWrapper *PullRequestWrapper) *PullRequestWrapper {
	var pullRequestWrapper *PullRequestWrapper
	if currentPullRequestWrapper != nil {
		pullRequestWrapper = currentPullRequestWrapper
		pullRequestWrapper.PullRequest = pullRequest
	} else {
		pullRequestWrapper = &PullRequestWrapper{Id: pullRequest.Id, PullRequestType: pullRequest.PullRequestType, PullRequest: pullRequest, Score: PullRequestScore{}, Seen: false, FirstSeen: time.Now()}
	}
	ghm.updatePullRequestScore(pullRequestWrapper)
	return pullRequestWrapper
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

		if ghm.configuration.SynchronousRequests {
			retrieveRequestedPullRequests()
			retrieveReviewedByPullRequests()
		} else {
			go retrieveRequestedPullRequests()
			go retrieveReviewedByPullRequests()
		}

		waitGroup.Wait()

	}

	sortedPullRequestWrappers := ghm.sortPullRequestWrappers(pullRequestWrappers)

	changesMade := len(pullRequestWrappers) != len(ghm.pullRequestWrappers)
	ghm.pullRequestWrappers = pullRequestWrappers
	if changesMade {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: PullRequestsUpdatesEvent{pullRequestType: Reviewer, pullRequestWrappers: sortedPullRequestWrappers}}
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
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: PullRequestsUpdatesEvent{pullRequestType : Own, pullRequestWrappers: sortedPullRequestWrappers}}
	}

	ghm.events <- Event{eventType: Status, payload: "idle"}

}


func (ghm *GHMon) addPullRequestReviewers(pullRequestWrapper *PullRequestWrapper) {

	pullRequest := pullRequestWrapper.PullRequest
	pullRequest.PullRequestReviewsByUser = make(map[uint32][]*PullRequestReview,0)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	ghm.logger.Printf("Adding reviewers to : %d/%s", pullRequest.Id, pullRequest.Title)

	retrieveRequestedReviewers := func() {
		// Use the pullRequest URL but strip out the https://api.github.com/ part
		pullRequestResult := makeAPIRequest(pullRequest.PullRequestURL.Path)
		requestedReviewers := pullRequestResult["requested_reviewers"].([]interface{})

		for _, requestedReviewerItem := range requestedReviewers {
			requestedReviewer := requestedReviewerItem.(map[string]interface{})
			id := uint32(requestedReviewer["id"].(float64))
			user := &User{uint32(requestedReviewer["id"].(float64)), requestedReviewer["login"].(string)}

			pullRequest.Lock.Lock()
			if _, ok := pullRequest.PullRequestReviewsByUser[id]; !ok {
				pullRequest.PullRequestReviewsByUser[id] = make([]*PullRequestReview,0)
			}
			pullRequestReview := &PullRequestReview{User: user,Status: "REQUESTED"}
			ghm.logger.Printf("Adding review request: %s/%s", pullRequestReview.User.Username, pullRequestReview.Status)
			pullRequest.PullRequestReviewsByUser[id] = append(pullRequest.PullRequestReviewsByUser[id],pullRequestReview)
			pullRequest.Lock.Unlock()
		}
		waitGroup.Done()
	}

	retrieveReviews := func() {
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
			if _, ok := pullRequest.PullRequestReviewsByUser[id]; !ok {
				ghm.logger.Printf("No existing items for %d, will add new list", id)
				pullRequest.PullRequestReviewsByUser[id] = make([]*PullRequestReview, 0)
			}
			ghm.logger.Printf("Adding review: %s/%s", pullRequestReview.User.Username, pullRequestReview.Status)
			pullRequest.PullRequestReviewsByUser[id] = append(pullRequest.PullRequestReviewsByUser[id], &pullRequestReview)
			pullRequest.Lock.Unlock()

		}
		waitGroup.Done()
	}

	if ghm.configuration.SynchronousRequests {
		retrieveRequestedReviewers()
		retrieveReviews()
	} else {
		go retrieveRequestedReviewers()
		go retrieveReviews()
	}

	// FIXME: Should also retrieve comments directly on the ticket here ...

	waitGroup.Wait()

	ghm.sortPullRequestReviewers(pullRequestWrapper)
	ghm.updatePullRequestScore(pullRequestWrapper)
	go ghm.store.StorePullRequestWrapper(pullRequestWrapper)

	ghm.events <- Event{eventType: PullRequestUpdated, payload: pullRequestWrapper}
}

func (ghm *GHMon) sortPullRequestReviewers(pullRequestWrapper *PullRequestWrapper) {

	keys := make([]uint32,0)
	for key := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		left := ghm.extractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviewsByUser[keys[i]])
		right := ghm.extractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviewsByUser[keys[j]])

		leftToInt := ghm.pullRequestReviewStatusToInt(left)
		rightToInt := ghm.pullRequestReviewStatusToInt(right)

		if leftToInt == rightToInt {
			return left.User.Id < right.User.Id
		}
		return leftToInt < rightToInt
	})

	sortedPullRequestReviews := make([][]*PullRequestReview, 0)
	for _,key := range keys {
		sortedPullRequestReviews = append(sortedPullRequestReviews, pullRequestWrapper.PullRequest.PullRequestReviewsByUser[key])
	}
	pullRequestWrapper.PullRequest.PullRequestReviewsByPriority = sortedPullRequestReviews

}

func (ghm *GHMon) Logger() *log.Logger {
	return ghm.logger
}