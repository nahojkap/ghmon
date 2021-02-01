package ghmon

import (
	"encoding/json"
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"github.com/kirsle/configdir"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Configuration struct {
	OwnQuery string `split_words:"true"`
	ReviewQuery string `split_words:"true"`
	RefreshInterval time.Duration `default:"15m" split_words:"true"`
	SynchronousRequests bool `default:"true" split_words:"true"`
}

type GHMon struct {
	configPath              string
	cachedPullRequestFolder string
	cachedRepoInformation   map[*url.URL]*Repo
	user                    *User
	pullRequestWrappers     map[uint32]*PullRequestWrapper
	events                  chan Event
	configuration           *Configuration
	store                   *GHMonStorage
	logger                  *log.Logger
	scoreCalculator			*ScoreCalculator
	internalEvents			chan Event
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
	Status PullRequestReviewStatus
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
	deleted bool
}

type PullRequestReviewStatus int
const (
	PullRequestReviewStatusUnknown PullRequestReviewStatus = iota
	PullRequestReviewStatusApproved
	PullRequestReviewStatusCommented
	PullRequestReviewStatusChangesRequested
	PullRequestReviewStatusPending
	PullRequestReviewStatusRequested
	PullRequestReviewStatusDismissed
)

type EventType int

const (
	Status EventType = iota
	PullRequestRefreshStarted
	PullRequestsUpdates
	PullRequestUpdated
	PullRequestRefreshFinished
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
			logger: logger,
		},
		cachedPullRequestFolder: cachedPullRequestFolder,
		configPath: configPath,
		logger : logger,
		scoreCalculator: &ScoreCalculator{
			logger: logger,
		},
		configuration: &configuration,
		internalEvents: make(chan Event, 5),
		pullRequestWrappers: make(map[uint32]*PullRequestWrapper,0),
	}

	go ghm.processInternalEvents()

	return &ghm
}

func (ghm *GHMon) processInternalEvents()  {

	var previousPullRequestWrappers map[uint32]*PullRequestWrapper = nil
	for {
		event := <- ghm.internalEvents
		switch event.eventType {
		case PullRequestRefreshStarted:
			ghm.events <- Event{eventType: Status, payload: "fetching pull requests"}
			previousPullRequestWrappers = make(map[uint32]*PullRequestWrapper, len(ghm.pullRequestWrappers))
			for k,v := range ghm.pullRequestWrappers {
				key := k
				value := v
				previousPullRequestWrappers[key] = value
			}
		case PullRequestRefreshFinished:

			for k,_ := range previousPullRequestWrappers {
				key := k
				ghm.pullRequestWrappers[key].deleted = true
				ghm.events <- Event{eventType: PullRequestUpdated, payload: ghm.pullRequestWrappers[key]}
			}

			ghm.events <- Event{eventType: Status, payload: "idle"}
		case PullRequestUpdated:

			pullRequestWrapper := event.payload.(*PullRequestWrapper)
			if _, ok := ghm.pullRequestWrappers[pullRequestWrapper.Id]; !ok {
				ghm.pullRequestWrappers[pullRequestWrapper.Id] = pullRequestWrapper
				sortedPullRequestWrappers := ghm.sortPullRequestWrappers(ghm.pullRequestWrappers)
				ghm.events <- Event{eventType: PullRequestsUpdates, payload: PullRequestsUpdatesEvent{pullRequestType: Reviewer, pullRequestWrappers: sortedPullRequestWrappers}}
			}
			ghm.events <- event

		case PullRequestsUpdates:
			ghm.events <- event
		}
	}


}

func (ghm *GHMon) Events() <-chan Event {
	return ghm.events
}

func (ghm *GHMon) monitorGithub() {
	for {
		// FIXME: Should take into consideration start of this method vs when it finished
		ghm.RetrievePullRequests()
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


func (ghm *GHMon) parsePullRequestQueryResult(pullRequestType PullRequestType, result map[string]interface{}) {

	pullRequestItems := result["items"].([]interface{})
	count := len(pullRequestItems)

	ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("Fetched %d pull requests", count)}

	for _, pullRequestItem := range pullRequestItems {

		item := pullRequestItem.(map[string]interface{})
		pullRequestId := uint32(item["id"].(float64))

		ghm.events <- Event{eventType: Status, payload: fmt.Sprintf("processing pull request %d", pullRequestId)}

		user := item["user"].(map[string]interface{})
		userID := uint32(user["id"].(float64))

		if pullRequestType == Reviewer && userID == ghm.user.Id {
			ghm.logger.Printf("Filtering out %d from list of reviewer", pullRequestId)
			continue
		}

		createdAt,_ := time.Parse(time.RFC3339, item["created_at"].(string))
		updatedAt,_ := time.Parse(time.RFC3339, item["updated_at"].(string))

		pullRequestObj := item["pull_request"].(map[string]interface{})

		creator := &User{Id: userID ,Username: user["login"].(string)}

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

		ghm.addPullRequestReviewers(pullRequestWrapper)

		ghm.internalEvents <- Event{eventType: PullRequestUpdated,payload: pullRequestWrapper}

	}

}

func (ghm *GHMon) updatePullRequestScore(pullRequestWrapper *PullRequestWrapper) {
	ghm.scoreCalculator.CalculateScore(pullRequestWrapper)
}

func (ghm *GHMon) getCurrentPullRequestWrapper(pullRequestId uint32) *PullRequestWrapper {

	for _, pullRequestWrapper := range ghm.pullRequestWrappers {
		pullRequestWrapperInstance := pullRequestWrapper
		if pullRequestWrapperInstance.Id == pullRequestId {
			return pullRequestWrapperInstance
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

		leftScore := ghm.scoreCalculator.CalculateTotalScore(left.Score)
		rightScore := ghm.scoreCalculator.CalculateTotalScore(right.Score)

		if leftScore == rightScore {
			return left.PullRequest.Id < right.PullRequest.Id
		}

		return leftScore < rightScore

	})

	return sortedPullRequestWrappers

}

func (ghm *GHMon) RetrievePullRequests() {

	ghm.internalEvents <- Event{eventType: PullRequestRefreshStarted}

	retrieveMyPullRequests := func() {
		if ghm.configuration.OwnQuery != "" {
			// Need the set of PR that has been 'seen' by the user as well as those requested
			result := makeAPIRequest("/search/issues?q=" + ghm.configuration.OwnQuery)
			ghm.parsePullRequestQueryResult(Own, result)
		} else {
			// Need the set of PR that has been 'seen' by the user as well as those requested
			result := makeAPIRequest("/search/issues?q=is:open+is:pr+author:@me+archived:false")
			ghm.parsePullRequestQueryResult(Own, result)
		}
	}

	retrievePullRequests := func() {
		if ghm.configuration.ReviewQuery != "" {
			// Need the set of PR that has been 'seen' by the user as well as those requested
			result := makeAPIRequest("/search/issues?q=" + ghm.configuration.ReviewQuery)
			ghm.parsePullRequestQueryResult(Reviewer,result)
		} else {

			var waitGroup sync.WaitGroup
			waitGroup.Add(2)

			retrieveRequestedPullRequests := func() {
				// Need the set of PR that has been 'seen' by the user as well as those requested
				result := makeAPIRequest("/search/issues?q=is:open+is:pr+review-requested:@me+archived:false")
				ghm.parsePullRequestQueryResult(Reviewer,result)
				waitGroup.Done()
			}

			retrieveReviewedByPullRequests := func() {
				result := makeAPIRequest("/search/issues?q=is:open+is:pr+reviewed-by:@me+archived:false")
				ghm.parsePullRequestQueryResult(Reviewer, result)
				waitGroup.Done()
			}

			go retrieveRequestedPullRequests()
			go retrieveReviewedByPullRequests()

			waitGroup.Wait()

		}
	}

	go retrieveMyPullRequests()
	go retrievePullRequests()

/*	sortedPullRequestWrappers := ghm.sortPullRequestWrappers(pullRequestWrappers)

	changesMade := len(pullRequestWrappers) != len(ghm.pullRequestWrappers)
	ghm.pullRequestWrappers = pullRequestWrappers
	if changesMade {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: PullRequestsUpdatesEvent{pullRequestType: Reviewer, pullRequestWrappers: sortedPullRequestWrappers}}
	}
*/
	// ghm.events <- Event{eventType: Status, payload: "idle"}
}

/*
func (ghm *GHMon) RetrieveMyPullRequests() {

	ghm.events <- Event{eventType: Status, payload: "fetching my pull requests"}
	pullRequestWrappers := make(map[uint32]*PullRequestWrapper, 0)

	sortedPullRequestWrappers := ghm.sortPullRequestWrappers(pullRequestWrappers)

	changesMade := len(pullRequestWrappers) != len(ghm.myPullRequestsWrappers)
	ghm.myPullRequestsWrappers = pullRequestWrappers

	if changesMade {
		ghm.events <- Event{eventType: PullRequestsUpdates, payload: PullRequestsUpdatesEvent{pullRequestType : Own, pullRequestWrappers: sortedPullRequestWrappers}}
	}

	ghm.events <- Event{eventType: Status, payload: "idle"}

}
*/

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

			if pullRequestWrapper.Id == id {
				ghm.logger.Printf("Filtering out review comments on own pull request for %d", pullRequestWrapper.Id)
				continue
			}

			user := &User{uint32(requestedReviewer["id"].(float64)), requestedReviewer["login"].(string)}

			pullRequest.Lock.Lock()
			if _, ok := pullRequest.PullRequestReviewsByUser[id]; !ok {
				pullRequest.PullRequestReviewsByUser[id] = make([]*PullRequestReview,0)
			}
			pullRequestReview := &PullRequestReview{User: user,Status: PullRequestReviewStatusRequested}
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

			if pullRequestWrapper.Id == id {
				ghm.logger.Printf("Filtering out review comments on own pull request for %d", pullRequestWrapper.Id)
				continue
			}

			stateStr := requestedReviewer["state"].(string)
			pullRequestReview := PullRequestReview{User: &User{id, user["login"].(string)}, Status: ghm.ConvertToPullRequestReviewState(stateStr)}
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

	go retrieveRequestedReviewers()
	go retrieveReviews()

	waitGroup.Wait()

	ghm.sortPullRequestReviewers(pullRequestWrapper)
	ghm.updatePullRequestScore(pullRequestWrapper)
	go ghm.store.StorePullRequestWrapper(pullRequestWrapper)

	ghm.events <- Event{eventType: PullRequestUpdated, payload: pullRequestWrapper}
}

func (ghm *GHMon) ConvertToPullRequestReviewState(pullRequestReviewStatusString string) PullRequestReviewStatus {
	switch pullRequestReviewStatusString {
	case "APPROVED":
		return PullRequestReviewStatusApproved
	case "COMMENTED":
		return PullRequestReviewStatusCommented
	case "CHANGES_REQUESTED":
		return PullRequestReviewStatusChangesRequested
	case "PENDING":
		return PullRequestReviewStatusPending
	case "REQUESTED":
		return PullRequestReviewStatusRequested
	case "DISMISSED":
		return PullRequestReviewStatusDismissed
	default:
		ghm.logger.Print("Unknown pull request review state: %s", pullRequestReviewStatusString)
		return PullRequestReviewStatusUnknown
	}
}


func (ghm *GHMon) ConvertPullRequestReviewStateToString(pullRequestReviewStatus PullRequestReviewStatus) string {
	switch pullRequestReviewStatus {
	case PullRequestReviewStatusApproved:
		return "Approved"
	case PullRequestReviewStatusCommented:
		return "Commented"
	case PullRequestReviewStatusChangesRequested:
		return "Changes Requested"
	case PullRequestReviewStatusPending:
		return "Pending"
	case PullRequestReviewStatusRequested:
		return "Requested"
	default:
		return "Unknown Status"
	}
}


func (ghm *GHMon) sortPullRequestReviewers(pullRequestWrapper *PullRequestWrapper) {

	keys := make([]uint32,0)
	for key := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {

		left := ghm.scoreCalculator.ExtractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviewsByUser[keys[i]])
		right := ghm.scoreCalculator.ExtractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviewsByUser[keys[j]])

		rank := ghm.scoreCalculator.RankPullRequestReview(left,right)

		if rank == 0 {
			return left.User.Id < right.User.Id
		}

		return rank < 0
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

func (ghm *GHMon) UpdateSeen(pullRequestWrapper *PullRequestWrapper, seen bool) {
	pullRequestWrapper.Seen = seen
	ghm.store.StorePullRequestWrapper(pullRequestWrapper)
}