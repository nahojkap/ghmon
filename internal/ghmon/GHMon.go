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
}

type GHMon struct {
	configPath              string
	cachedPullRequestFolder string
	cachedRepoInformation   map[*url.URL]*Repo
	user                    *User
	pullRequestWrappers     map[uint32]*PullRequestWrapper
	sortedPullRequestWrappers []*PullRequestWrapper
	events                  chan Event
	configuration           *Configuration
	store                   *Storage
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
	Score float32
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
	Total            float32
	Seen             bool
	AgeSec           uint32
	Approvals        uint
	ApprovedByMe     bool
	Comments         uint
	Dismissed        uint
	ChangesRequested uint
	NumReviewers     uint
	IsMyPullRequest  bool
}

type PullRequestWrapper struct {
	Id              uint32
	PullRequestType PullRequestType
	FirstSeen       time.Time
	Seen            bool
	/* Score is between 0 and 100 (higher score, more critical) */
	Score           PullRequestScore
	PullRequest     *PullRequest
	Deleted         bool
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
	PullRequestDeleted
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
	logger.Printf("Refresh Interval: %s", configuration.RefreshInterval)

	ghm := GHMon{
		cachedRepoInformation: make(map[*url.URL]*Repo,0),
		events : make(chan Event,5),
		store: &Storage{
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

	for {
		event := <- ghm.internalEvents
		switch event.eventType {
		case PullRequestRefreshStarted:
			ghm.events <- Event{eventType: Status, payload: "fetching pull requests"}
		case PullRequestRefreshFinished:
			ghm.sortedPullRequestWrappers = ghm.sortPullRequestWrappers(ghm.pullRequestWrappers)
			ghm.events <- Event{eventType: PullRequestsUpdates, payload: PullRequestsUpdatesEvent{pullRequestType: Reviewer, pullRequestWrappers: ghm.sortedPullRequestWrappers}}
			ghm.events <- Event{eventType: Status, payload: "idle"}
		case PullRequestDeleted:
			ghm.events <- event
		case PullRequestUpdated:
			pullRequestWrapper := event.payload.(*PullRequestWrapper)
			ghm.pullRequestWrappers[pullRequestWrapper.Id] = pullRequestWrapper
			pullRequestWrapper.Score = ghm.scoreCalculator.CalculateScore(ghm.user, pullRequestWrapper)
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

		pullRequestWrapper.Score = ghm.scoreCalculator.CalculateScore(ghm.user, pullRequestWrapper)

		ghm.internalEvents <- Event{eventType: PullRequestUpdated,payload: pullRequestWrapper}

	}

}

func (ghm *GHMon) updatePullRequestScore(pullRequestWrapper *PullRequestWrapper) {
	pullRequestWrapper.Score = ghm.scoreCalculator.CalculateScore(ghm.user, pullRequestWrapper)
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
		pullRequestWrapper.Deleted = false
	} else {
		pullRequestWrapper = &PullRequestWrapper{Id: pullRequest.Id, PullRequestType: pullRequest.PullRequestType, PullRequest: pullRequest, Score: PullRequestScore{}, Seen: false, FirstSeen: time.Now(), Deleted: false}
	}
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

		leftScore := left.Score.Total // ghm.scoreCalculator.CalculateTotalScore(left)
		rightScore := right.Score.Total // ghm.scoreCalculator.CalculateTotalScore(right)

		if leftScore == rightScore {
			return left.PullRequest.Id < right.PullRequest.Id
		}

		return leftScore > rightScore

	})

	return sortedPullRequestWrappers

}

func (ghm *GHMon) RetrievePullRequests() {

	var retrieveAllPullRequestsWaitGroup sync.WaitGroup
	retrieveAllPullRequestsWaitGroup.Add(2)

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
		retrieveAllPullRequestsWaitGroup.Done()
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
		retrieveAllPullRequestsWaitGroup.Done()

	}

	go retrieveMyPullRequests()
	go retrievePullRequests()
	go ghm.waitForRetrievalsToFinish(&retrieveAllPullRequestsWaitGroup)

}

func (ghm *GHMon) waitForRetrievalsToFinish(waitGroup *sync.WaitGroup) {

	waitGroup.Wait()
	waitGroup.Add(1)

	// Now, we retrieve all saved pull requests & mark them Deleted if they are not in the list of PRs
	retrieveSavedPullRequests := func() {

		pullRequestIdentifiers, err := ghm.store.loadStoredPullRequestIdentifiers()
		if err == nil {
			ghm.logger.Printf("Loaded %d pull requests from disk", len(pullRequestIdentifiers))
			for _, pullRequestIdentifier := range pullRequestIdentifiers {
				channel := ghm.store.LoadPullRequestWrapper(pullRequestIdentifier)
				pullRequestWrapper := <- channel
				if pullRequestWrapper != nil {
					if _, ok := ghm.pullRequestWrappers[pullRequestIdentifier]; !ok {
						// Ok, the PR does not exist on GitHub, lets use the one loaded from
						// disk and mark it deleted
						pullRequestWrapper.Deleted = true
						ghm.updatePullRequestScore(pullRequestWrapper)
						ghm.internalEvents <- Event{eventType: PullRequestUpdated, payload: pullRequestWrapper}
					}
				}
			}
		}
		waitGroup.Done()
	}

	go retrieveSavedPullRequests()

	waitGroup.Wait()

	ghm.internalEvents <- Event{eventType: PullRequestRefreshFinished}
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

			if pullRequestWrapper.PullRequest.Creator.Id == id {
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

			if pullRequestWrapper.PullRequest.Creator.Id == id {
				ghm.logger.Printf("Filtering out review comments on own pull request for %d", pullRequestWrapper.Id)
				continue
			}

			stateStr := requestedReviewer["state"].(string)
			state := ghm.ConvertToPullRequestReviewState(stateStr)
			pullRequestReview := PullRequestReview{User: &User{id, user["login"].(string)}, Status: state, Score: float32(ghm.scoreCalculator.PullRequestReviewStatusToInt(state))}
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

	ghm.internalEvents <- Event{eventType: PullRequestUpdated, payload: pullRequestWrapper}
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
	case PullRequestReviewStatusDismissed:
		return "Dismissed"
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

func (ghm *GHMon) PurgeDeletedPullRequests() int {

	var pullRequestDeleted = 0
	for _,pullRequestWrapper := range ghm.pullRequestWrappers {
		if pullRequestWrapper.Deleted {
			ghm.internalEvents <- Event{eventType: PullRequestDeleted, payload: pullRequestWrapper}
			go ghm.store.DeletePullRequestWrapper(pullRequestWrapper.Id)
			delete(ghm.pullRequestWrappers,pullRequestWrapper.Id)
			pullRequestDeleted++
		}
	}

	if pullRequestDeleted > 0 {
		sortedPullRequestWrappers := ghm.sortPullRequestWrappers(ghm.pullRequestWrappers)
		ghm.internalEvents <- Event{eventType: PullRequestsUpdates, payload:PullRequestsUpdatesEvent{pullRequestType: Reviewer, pullRequestWrappers: sortedPullRequestWrappers} }
	}

	return pullRequestDeleted
}