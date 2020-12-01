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
	pullRequestType 		PullRequestType
	pullRequestList			*widgets.List
	pullRequests            []*PullRequestWrapper
}

type PullRequestScore struct {
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
	ghui.UpdatePullRequestDetails(nextSelectedPullRequestGroup.pullRequests, nextSelectedPullRequestGroup.pullRequestList.SelectedRow)
}

func (ghui *UI) RefreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
	go ghui.ghMon.RetrieveMyPullRequests()
}

func  (ghui *UI) pullRequestReviewStatusToInt(pullRequestReview PullRequestReview) int {
	switch pullRequestReview.Status {
	case "APPROVED":
		return 30
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

func (ghui *UI) getOverallPullRequestColor(pullRequest *PullRequest) string {

	pullRequestReviews := ghui.extractPullRequestReviews(pullRequest.PullRequestReviews)

	if len(pullRequestReviews) == 0 {
		return "white"
	}

	comments := 0
	approvals := 0
	changesRequested := 0

	for _, pullRequestReview := range pullRequestReviews {
		switch pullRequestReview.Status {
		case "APPROVED" : approvals++
		case "CHANGES_REQUESTED" : changesRequested++
		case "COMMENTED" : comments++
		}
	}

	if changesRequested != 0 {
		return "red3"
	}

	if approvals == len(pullRequestReviews) {
		return "green"
	}

	if comments > 0 {
		return "orange3"
	}

	return "white"


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
		return ghui.pullRequestReviewStatusToInt(left) < ghui.pullRequestReviewStatusToInt(right)
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
	pullRequest := currentlySelectedPullRequestGroup.pullRequests[uint32(selectedPullRequest)]
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

func (ghui *UI)handlePullRequestsUpdates(loadedPullRequests map[uint32]*PullRequest) {

	// FIXME: Should store currently selected PR and make sure
	// FIXME: that is displayed (if still in the list) after loading
	// FIXME: the new list of PRs

	keys := make([]uint32,0)
	pullRequestType := Own
	for key := range loadedPullRequests {
		// FIXME: This needs to be more top-level really
		pullRequestType = loadedPullRequests[key].PullRequestType
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var pullRequestGroup *PullRequestGroup
	if pullRequestType == Reviewer {
		pullRequestGroup = ghui.reviewPullRequestGroup
	} else {
		pullRequestGroup = ghui.myPullRequestGroup
	}

	currentPullRequests := pullRequestGroup.pullRequests
	pullRequestList := pullRequestGroup.pullRequestList

	newSelectedRow := 0
	var currentlySelectedPullRequest *PullRequest
	if len(currentPullRequests) > 0 {
		currentlySelectedPullRequest = currentPullRequests[pullRequestList.SelectedRow].PullRequest
	}
	numPRs := len(loadedPullRequests)


	pullRequestWrappers := make([]*PullRequestWrapper,0)

    //	pullRequests = make([]*PullRequest,numPRs)
	pullRequestList.Rows = make([]string, numPRs)

	counter := 0
	for _,key  := range keys {
		pullRequestItem := loadedPullRequests[key]
		var listLabel string
		if currentlySelectedPullRequest != nil && pullRequestItem.Id == currentlySelectedPullRequest.Id {
			newSelectedRow=counter
			pullRequestList.SelectedRowStyle = ui.NewStyle(172,ColorListSelectedBackground)
			listLabel = fmt.Sprintf(" %d  %s", pullRequestItem.Id, pullRequestItem.Title)
		} else {
			listLabel = fmt.Sprintf(" [%d  %s](fg:%s)", pullRequestItem.Id, pullRequestItem.Title, ghui.getOverallPullRequestColor(pullRequestItem))
		}

		pullRequestList.Rows[counter] = listLabel
		pullRequestWrappers = append(pullRequestWrappers,&PullRequestWrapper{PullRequestType: pullRequestItem.PullRequestType,PullRequest: pullRequestItem, Score: PullRequestScore{},Seen: false, FirstSeen:time.Now()})
		counter++
	}
	pullRequestGroup.pullRequests = pullRequestWrappers
	pullRequestList.SelectedRow = newSelectedRow

	ghui.renderPullRequestLists()
}

func (ghui *UI) handlePullRequestUpdated(loadedPullRequest *PullRequest) {
	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	if currentlySelectedPullRequestGroup.pullRequestType == loadedPullRequest.PullRequestType && loadedPullRequest.Id == currentlySelectedPullRequestGroup.pullRequests[currentlySelectedPullRequestGroup.pullRequestList.SelectedRow].PullRequest.Id {
		ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
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
			go ghui.handlePullRequestUpdated(event.payload.(*PullRequest))
		case PullRequestsUpdates:
			go ghui.handlePullRequestsUpdates(event.payload.(map[uint32]*PullRequest))
		case Status:
			go ghui.handleStatusUpdate(event.payload.(string))
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
		currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
		e := <-uiEvents
		switch e.ID {
		case "<Tab>":
			ghui.NewFocus()
		case "r":
			ghui.RefreshPullRequests()
		case "q", "<C-c>":
			return
		case "j", "<Down>":
			if currentlySelectedPullRequestGroup.pullRequestList.SelectedRow != (len(currentlySelectedPullRequestGroup.pullRequestList.Rows)-1) {
				currentlySelectedPullRequestGroup.pullRequestList.ScrollDown()
				ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
			}
		case "k", "<Up>":
			if currentlySelectedPullRequestGroup.pullRequestList.SelectedRow != 0 {
				currentlySelectedPullRequestGroup.pullRequestList.ScrollUp()
				ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
			}
		case "<C-d>":
			currentlySelectedPullRequestGroup.pullRequestList.ScrollHalfPageDown()
			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests,currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		case "<C-u>":
			currentlySelectedPullRequestGroup.pullRequestList.ScrollHalfPageUp()
			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests,currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		case "<C-f>":
			currentlySelectedPullRequestGroup.pullRequestList.ScrollPageDown()
			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests,currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		case "<C-b>":
			currentlySelectedPullRequestGroup.pullRequestList.ScrollPageUp()
			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		case "g":
			if previousKey == "g" {
				currentlySelectedPullRequestGroup.pullRequestList.ScrollTop()
				ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
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
