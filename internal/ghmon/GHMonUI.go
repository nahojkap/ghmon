package ghmon

import (
	"fmt"
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	"log"
	"os/exec"
	"runtime"
	"sort"
	"time"
)

type PullRequestGroup struct {
	pullRequestType     PullRequestType
	pullRequestList     *widgets.List
	pullRequestWrappers []*PullRequestWrapper
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
	PullRequestType PullRequestType
	FirstSeen time.Time
	Seen bool
	Score PullRequestScore
	PullRequest *PullRequest
}

type UI struct {
	ghMon                   *GHMon

	status                  *widgets.Paragraph

	pullRequestDetails      *widgets.Paragraph
	pullRequestBody         *widgets.Paragraph
	reviewerTable            *widgets.Table

	reviewPullRequestGroup 	*PullRequestGroup
	myPullRequestGroup 		*PullRequestGroup

	pullRequestGroups []*PullRequestGroup
	currentFocusedPullRequestGroup    int
}

const ColorListSelectedBackground ui.Color = 249

func NewGHMonUI(ghm *GHMon) *UI {

	status := widgets.NewParagraph()
	status.Text = ""
	status.Title = "Status"
	status.BorderStyle = ui.NewStyle(8)

	myReviewPullRequestList := widgets.NewList()
	myReviewPullRequestList.Title = "My Pull Request(s)"
	myReviewPullRequestList.WrapText = false
	myReviewPullRequestList.TextStyle = ui.NewStyle(ui.ColorWhite)
	myReviewPullRequestList.SelectedRowStyle = ui.NewStyle(243, ColorListSelectedBackground)
	myReviewPullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)
	myReviewPullRequestList.TitleStyle = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)

	reviewPullRequestList := widgets.NewList()
	reviewPullRequestList.Title = "Active Review Request(s)"
	reviewPullRequestList.WrapText = false
	reviewPullRequestList.TextStyle = ui.NewStyle(ui.ColorWhite)
	reviewPullRequestList.SelectedRowStyle = ui.NewStyle(243, ColorListSelectedBackground)
	reviewPullRequestList.BorderStyle = ui.NewStyle(8)

	reviewerTable := widgets.NewTable()
	reviewerTable.Title = "Reviewers"
	reviewerTable.RowSeparator = false
	reviewerTable.ColumnWidths = make([]int,2)
	reviewerTable.ColumnWidths[0] = 17
	reviewerTable.ColumnWidths[1] = -1
	reviewerTable.PaddingLeft = 1

	ui.StyleParserColorMap["lime"] = 10
	ui.StyleParserColorMap["red3"] = 124
	ui.StyleParserColorMap["orange3"] = 172
	ui.StyleParserColorMap["yellow3"] = 184

	reviewerTable.BorderStyle = ui.NewStyle(8)

	pullRequestDetails := widgets.NewParagraph()
	pullRequestDetails.BorderStyle = ui.NewStyle(8)

	pullRequestBody := widgets.NewParagraph()
	pullRequestBody.Title = "Details"
	pullRequestBody.BorderStyle = ui.NewStyle(8)

	ghui := UI{ghMon: ghm, reviewerTable: reviewerTable, status: status, pullRequestDetails: pullRequestDetails, pullRequestBody:  pullRequestBody}

	ghui.reviewPullRequestGroup = &PullRequestGroup{pullRequestList: reviewPullRequestList, pullRequestType: Reviewer}
	ghui.myPullRequestGroup = &PullRequestGroup{pullRequestList: myReviewPullRequestList, pullRequestType: Own}
	ghui.pullRequestGroups = make([]*PullRequestGroup, 2)
	ghui.currentFocusedPullRequestGroup = 0
	ghui.pullRequestGroups[0] = ghui.myPullRequestGroup
	ghui.pullRequestGroups[1] = ghui.reviewPullRequestGroup

	return &ghui
}

func (ghui *UI) renderAll() {
	ghui.renderPullRequestLists()
	ghui.renderPullRequestDetails()
	ghui.renderStatus()
}

func (ghui *UI) renderStatus() {
	ui.Render(ghui.status)
}

func (ghui *UI) renderPullRequestDetails() {
	ui.Render(ghui.reviewerTable, ghui.pullRequestDetails, ghui.pullRequestBody)
}


func (ghui *UI) renderPullRequestLists() {
	ui.Render(ghui.reviewPullRequestGroup.pullRequestList,ghui.myPullRequestGroup.pullRequestList)
}

func (ghui *UI) Resize(height int, width int) {
	ghui.myPullRequestGroup.pullRequestList.SetRect(0, 0, width / 2, (height-3)/2)
	ghui.reviewPullRequestGroup.pullRequestList.SetRect(0, (height-3)/2, width / 2, height-3)
	ghui.status.SetRect(0, height-3, width, height)
	ghui.pullRequestDetails.SetRect(width / 2,0,width,7)
	ghui.reviewerTable.SetRect(width / 2, 7, width, 15)
	ghui.pullRequestBody.SetRect(width / 2, 15, width, height-3)
}

func (ghui *UI) NewFocus() {
	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	ghui.currentFocusedPullRequestGroup++
	currentlySelectedPullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(8)
	currentlySelectedPullRequestGroup.pullRequestList.TitleStyle = ui.NewStyle(8, ui.ColorClear, ui.ModifierClear)
	nextSelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	nextSelectedPullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)
	nextSelectedPullRequestGroup.pullRequestList.TitleStyle = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)
	ghui.renderPullRequestLists()
	ghui.UpdatePullRequestDetails(nextSelectedPullRequestGroup.pullRequestWrappers, nextSelectedPullRequestGroup.pullRequestList.SelectedRow)
}

func (ghui *UI) RefreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
	go ghui.ghMon.RetrieveMyPullRequests()
}

func  (ghui *UI) pullRequestReviewStatusToInt(pullRequestReview PullRequestReview) int {
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

func (ghui *UI) getPullRequestReviewColor(pullRequestView PullRequestReview) (color string) {
	switch pullRequestView.Status {
	case "APPROVED":
		color = "green"
	case "COMMENTED":
		color = "orange3"
	case "CHANGES_REQUESTED":
		color = "red3"
	case "PENDING":
		color = "yellow3"
	case "REQUESTED":
		color = "white"
	default:
		color = "white"
	}
	return
}

func (ghui *UI)extractMostImportantFirst(pullRequestReviews []PullRequestReview) PullRequestReview {

	// FIXME: The reviews need to be sorted by date as well - e.g. its possible to have an approval *after* a comment
	// FIXME: and then another comment, which officially requires a re-review
	// FIXME: Or should it be that the presence of any approval should be signalled differently and only be
	// FIXME: cancelled if there is a request for change?

	sort.Slice(pullRequestReviews, func (i, j int) bool {
		if (pullRequestReviews[i].Status == "APPROVED" || pullRequestReviews[i].Status == "CHANGES_REQUESTED") && (pullRequestReviews[j].Status == "APPROVED" || pullRequestReviews[j].Status == "CHANGES_REQUESTED") {
			return pullRequestReviews[i].SubmittedAt.After(pullRequestReviews[j].SubmittedAt)
		}
		return ghui.pullRequestReviewStatusToInt(pullRequestReviews[i]) < ghui.pullRequestReviewStatusToInt(pullRequestReviews[j])
	})

	return pullRequestReviews[0]

}

func (ghui *UI) extractPullRequestReviews(pullRequestReviews map[uint32][]PullRequestReview) []PullRequestReview {

	extractedPullRequestReviews := make([]PullRequestReview,0)
	for _,pullRequestReviews := range pullRequestReviews {
		pullRequestReview := ghui.extractMostImportantFirst(pullRequestReviews)
		extractedPullRequestReviews = append(extractedPullRequestReviews, pullRequestReview)
	}

	return extractedPullRequestReviews
}

func (ghui *UI) getOverallPullRequestColor(pullRequestWrapper *PullRequestWrapper) string {

	pullRequestScore := pullRequestWrapper.Score

	if pullRequestScore.ChangesRequested != 0 {
		return "red3"
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		return "green"
	}

	if pullRequestScore.Comments > 0 {
		return "orange3"
	}

	return "white"

}

func (ghui *UI) scorePullRequest(pullRequestWrapper *PullRequestWrapper) PullRequestScore {

	pullRequestScore := PullRequestScore{}

	importantPullRequestReviews := make([]*PullRequestReview, 0)
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviews {
		pullRequestReview := ghui.extractMostImportantFirst(pullRequestReviews)
		importantPullRequestReviews = append(importantPullRequestReviews, &pullRequestReview)
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
	return pullRequestScore
}

func (ghui *UI)UpdatePullRequestDetails(pullRequestList []*PullRequestWrapper, selectedPullRequest int) {

	if len(pullRequestList) == 0 {
		return
	}

	pullRequestWrapper := pullRequestList[uint32(selectedPullRequest)]
	ghui.pullRequestDetails.WrapText = false
	ghui.pullRequestDetails.Text = fmt.Sprintf(" [ID](fg:white): %d\n [Title](fg:white): %s\n [Creator](fg:white): %s\n [Created](fg:white): %s\n [Updated](fg:white): %s", pullRequestWrapper.PullRequest.Id,pullRequestWrapper.PullRequest.Title, pullRequestWrapper.PullRequest.Creator.Username, pullRequestWrapper.PullRequest.CreatedAt, pullRequestWrapper.PullRequest.UpdatedAt)
	ghui.pullRequestBody.Text = fmt.Sprintf("%s", pullRequestWrapper.PullRequest.Body)

	ghui.reviewerTable.Rows = make([][]string, 0)

	keys := make([]uint32,0)
	for key := range pullRequestWrapper.PullRequest.PullRequestReviews {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		left := ghui.extractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviews[keys[i]])
		right := ghui.extractMostImportantFirst(pullRequestWrapper.PullRequest.PullRequestReviews[keys[j]])
		leftToInt := ghui.pullRequestReviewStatusToInt(left)
		rightToInt := ghui.pullRequestReviewStatusToInt(right)

		if leftToInt == rightToInt {
			return left.User.Id < right.User.Id
		}
		return leftToInt < rightToInt
	})

	for _, key := range keys {
		pullRequestReview := pullRequestWrapper.PullRequest.PullRequestReviews[key]
		status := fmt.Sprintf("[%s](fg:%s)", pullRequestReview[0].Status, ghui.getPullRequestReviewColor(pullRequestReview[0]))
		row := make([]string,2)
		row[0] = status
		row[1] = pullRequestReview[0].User.Username
		ghui.reviewerTable.Rows = append(ghui.reviewerTable.Rows,row)
	}

	ghui.renderPullRequestDetails()

}


func (ghui *UI) openBrowser(selectedPullRequest int) {

	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	pullRequest := currentlySelectedPullRequestGroup.pullRequestWrappers[uint32(selectedPullRequest)]
	url := pullRequest.PullRequest.HtmlURL.String()

	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}

	if err != nil {
		log.Fatal(err)
	}

}

func padToLen(str string, requiredLen int) string {
	paddedString := str
	for len(paddedString) < requiredLen {
		paddedString += " "
	}
	return paddedString
}


func (ghui *UI)handlePullRequestsUpdates(loadedPullRequests map[uint32]*PullRequest) {

	if len(loadedPullRequests) <= 0 {
		return
	}

	// FIXME: Somewhat clunky way of figuring out the type of review being submittec
	pullRequestType := Reviewer
	for key := range loadedPullRequests {
		pullRequestType = loadedPullRequests[key].PullRequestType
		break
	}

	var pullRequestGroup *PullRequestGroup
	if pullRequestType == Reviewer {
		pullRequestGroup = ghui.reviewPullRequestGroup
	} else {
		pullRequestGroup = ghui.myPullRequestGroup
	}

	pullRequestWrappers := make([]*PullRequestWrapper,0)
	for _, loadedPullRequest  := range loadedPullRequests {
		currentPullRequestWrapper := ghui.getCurrentPullRequestWrapper(pullRequestGroup, loadedPullRequest.Id)
		pullRequestWrapper := ghui.createPullRequestWrapperFromCurrent(loadedPullRequest, currentPullRequestWrapper)
		pullRequestWrappers = append(pullRequestWrappers, pullRequestWrapper)
	}

	sort.Slice(pullRequestWrappers, func(i,j int) bool {
		left := pullRequestWrappers[i]
		right := pullRequestWrappers[j]

		leftScore := ghui.calculateScore(left.Score)
		rightScore := ghui.calculateScore(right.Score)

		if leftScore == rightScore {
			return left.PullRequest.Id < right.PullRequest.Id
		}

		return leftScore < rightScore

	})

	// FIXME: This method should be split into 2 - one for scoring / ordering the PRs and one for display

	longestRepoName := 0
	for _, pullRequestWrapper := range pullRequestWrappers {
		if len(pullRequestWrapper.PullRequest.Repo.Name) > longestRepoName {
			longestRepoName = len(pullRequestWrapper.PullRequest.Repo.Name)
		}
	}


	currentPullRequests := pullRequestGroup.pullRequestWrappers
	pullRequestList := pullRequestGroup.pullRequestList

	var currentlySelectedPullRequest *PullRequest
	if len(currentPullRequests) > 0 {
		currentlySelectedPullRequest = currentPullRequests[pullRequestList.SelectedRow].PullRequest
	}

	newSelectedRow := 0
	pullRequestList.Rows = make([]string, len(pullRequestWrappers))

	for counter, pullRequestWrapper := range pullRequestWrappers {
		pullRequestItem := pullRequestWrapper.PullRequest
		var listLabel string

		seen := " "
		if !pullRequestWrapper.Seen {
			seen = "*"
		}

		paddedRepoName := padToLen(pullRequestItem.Repo.Name, longestRepoName)

		if currentlySelectedPullRequest != nil && pullRequestItem.Id == currentlySelectedPullRequest.Id {
			newSelectedRow=counter
			pullRequestList.SelectedRowStyle = ui.NewStyle(172,ColorListSelectedBackground)
			listLabel = fmt.Sprintf("%s %d %s %s", seen, pullRequestItem.Id, paddedRepoName, pullRequestItem.Title)
		} else {
			listLabel = fmt.Sprintf("%s [%d %s %s](fg:%s,mod:bold)", seen, pullRequestItem.Id, paddedRepoName, pullRequestItem.Title, ghui.getOverallPullRequestColor(pullRequestWrapper))
		}
		pullRequestList.Rows[counter] = listLabel
		counter++
	}

	pullRequestGroup.pullRequestWrappers = pullRequestWrappers
	pullRequestList.SelectedRow = newSelectedRow

	ghui.renderPullRequestLists()
}

func (ghui *UI) handlePullRequestUpdated(loadedPullRequest *PullRequest) {
	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	if len(currentlySelectedPullRequestGroup.pullRequestWrappers) > 0 {
		if currentlySelectedPullRequestGroup.pullRequestType == loadedPullRequest.PullRequestType && loadedPullRequest.Id == currentlySelectedPullRequestGroup.pullRequestWrappers[currentlySelectedPullRequestGroup.pullRequestList.SelectedRow].PullRequest.Id {
			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		}
	}
}

func (ghui *UI) handleStatusUpdate(status string) {
	ghui.status.Text = " " + status
	ghui.renderStatus()
}

func (ghui *UI) pollEvents() {
	events := ghui.ghMon.events
	for {
		event := <-events
		switch event.eventType {
		case PullRequestUpdated:
			ghui.handlePullRequestUpdated(event.payload.(*PullRequest))
		case PullRequestsUpdates:
			ghui.handlePullRequestsUpdates(event.payload.(map[uint32]*PullRequest))
		case Status:
			ghui.handleStatusUpdate(event.payload.(string))
		}
	}
}

func (ghui *UI) EventLoop() {

	err := ui.Init()
	if err != nil {
		panic(err)
	}

	defer ui.Close()

	termWidth, termHeight := ui.TerminalDimensions()

	ghui.Resize(termHeight, termWidth)
	ghui.renderAll()

	go ghui.pollEvents()

	previousKey := ""
	uiEvents := ui.PollEvents()
	for {
		e := <-uiEvents
		currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
		switch e.ID {
		case "<Tab>":
			ghui.NewFocus()
		case "r":
			ghui.RefreshPullRequests()
		case "q", "<C-c>":
			return
		case "j", "<Down>":
			if len(currentlySelectedPullRequestGroup.pullRequestWrappers) > 0 {
				if currentlySelectedPullRequestGroup.pullRequestList.SelectedRow != (len(currentlySelectedPullRequestGroup.pullRequestList.Rows)-1) {
					currentlySelectedPullRequestGroup.pullRequestList.ScrollDown()
					ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
				}
			}
		case "k", "<Up>":
			if len(currentlySelectedPullRequestGroup.pullRequestWrappers) > 0 {
				if currentlySelectedPullRequestGroup.pullRequestList.SelectedRow != 0 {
					currentlySelectedPullRequestGroup.pullRequestList.ScrollUp()
					ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
				}
			}
		case "g":
			if previousKey == "g" {
				currentlySelectedPullRequestGroup.pullRequestList.ScrollTop()
				ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
			}
		case "<Home>":
			currentlySelectedPullRequestGroup.pullRequestList.ScrollTop()
		case "G", "<End>":
			currentlySelectedPullRequestGroup.pullRequestList.ScrollBottom()
		case "<Enter>" :
			ghui.openBrowser(currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		case "<Resize>":
			payload := e.Payload.(ui.Resize)
			ghui.Resize(payload.Height, payload.Width)
			ui.Clear()
		}

		if previousKey == "g" {
			previousKey = ""
		} else {
			previousKey = e.ID
		}

		// We always render!
		ghui.renderAll()
	}

}

func (ghui *UI) createPullRequestWrapperFromCurrent(pullRequestItem *PullRequest, current *PullRequestWrapper) *PullRequestWrapper {

	pullRequestWrapper := PullRequestWrapper{PullRequestType: pullRequestItem.PullRequestType, PullRequest: pullRequestItem, Score: PullRequestScore{}, Seen: false, FirstSeen: time.Now()}

	if current != nil {
		pullRequestWrapper.FirstSeen = current.FirstSeen
		pullRequestWrapper.Score = current.Score
		pullRequestWrapper.Seen = current.Seen
	}

	pullRequestWrapper.Score = ghui.scorePullRequest(&pullRequestWrapper)

	return &pullRequestWrapper
}

func (ghui *UI) getCurrentPullRequestWrapper(pullRequestGroup *PullRequestGroup, pullRequestId uint32) *PullRequestWrapper {

	for _, pullRequestWrapper := range pullRequestGroup.pullRequestWrappers {
		if pullRequestWrapper.PullRequest.Id == pullRequestId {
			return pullRequestWrapper
		}
	}
	return nil
}

func (ghui *UI) calculateScore(pullRequestScore PullRequestScore) int {

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
