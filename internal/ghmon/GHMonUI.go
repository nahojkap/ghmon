package ghmon

import (
	"fmt"
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	"log"
	"os/exec"
	"runtime"
)

type PullRequestGroup struct {
	pullRequestType 		PullRequestType
	pullRequestList			*widgets.List
	pullRequests            []*PullRequest
}

type UI struct {
	ghMon                   *GHMon
	status                  *widgets.Paragraph

	pullRequestDetails      *widgets.Paragraph
	pullRequestBody         *widgets.Paragraph
	reviewerList            *widgets.List

	reviewPullRequestGroup 	*PullRequestGroup
	myPullRequestGroup 		*PullRequestGroup

	pullRequestGroups []*PullRequestGroup
	currentFocusedPullRequestGroup    int
}

func NewGHMonUI(ghm *GHMon) *UI {

	status := widgets.NewParagraph()
	status.Text = ""
	status.Title = "Status"
	status.BorderStyle = ui.NewStyle(8)

	reviewPullRequestList := widgets.NewList()
	reviewPullRequestList.Title = "Active Pull Requests"
	reviewPullRequestList.WrapText = false
	reviewPullRequestList.TextStyle = ui.NewStyle(ui.ColorWhite)
	reviewPullRequestList.SelectedRowStyle = ui.NewStyle(243)
	reviewPullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)

	myReviewPullRequestList := widgets.NewList()
	myReviewPullRequestList.Title = "My Pull Requests"
	myReviewPullRequestList.WrapText = false
	myReviewPullRequestList.TextStyle = ui.NewStyle(ui.ColorWhite)
	myReviewPullRequestList.SelectedRowStyle = ui.NewStyle(243)
	myReviewPullRequestList.BorderStyle = ui.NewStyle(8)

	reviewerList := widgets.NewList()
	reviewerList.Title = "Reviewers"
	reviewerList.WrapText = false
	reviewerList.PaddingLeft = 1
	reviewerList.BorderStyle = ui.NewStyle(8)

	pullRequestDetails := widgets.NewParagraph()
	pullRequestDetails.BorderStyle = ui.NewStyle(8)

	pullRequestBody := widgets.NewParagraph()
	pullRequestBody.Title = "Details"
	pullRequestBody.BorderStyle = ui.NewStyle(8)

	ghui := UI{ghMon: ghm, reviewerList: reviewerList, status: status, pullRequestDetails: pullRequestDetails, pullRequestBody:  pullRequestBody}

	ghui.reviewPullRequestGroup = &PullRequestGroup{pullRequestList: reviewPullRequestList, pullRequestType: Reviewer}
	ghui.myPullRequestGroup = &PullRequestGroup{pullRequestList: myReviewPullRequestList, pullRequestType: Own}
	ghui.pullRequestGroups = make([]*PullRequestGroup, 2)
	ghui.currentFocusedPullRequestGroup = 0
	ghui.pullRequestGroups[0] = ghui.reviewPullRequestGroup
	ghui.pullRequestGroups[1] = ghui.myPullRequestGroup

	ghm.AddStatusListener(func (status string) {
		ghui.status.Text = " " + status
		ghui.render()
	})

	ghm.AddPullRequestUpdatedListener(func (pullRequest *PullRequest) {
		currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
		if currentlySelectedPullRequestGroup.pullRequestType == pullRequest.PullRequestType {
			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequests, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
		}
	})

	ghm.AddPullRequestListener(func (pullRequestType PullRequestType, loadedPullRequests map[uint32]*PullRequest) {

		// FIXME: Should store currently selected PR and make sure
		// FIXME: that is displayed (if still in the list) after loading
		// FIXME: the new list of PRs

		var pullRequestGroup *PullRequestGroup
		if pullRequestType == Reviewer {
			pullRequestGroup = ghui.reviewPullRequestGroup
		} else {
			pullRequestGroup = ghui.myPullRequestGroup
		}
		pullRequests := pullRequestGroup.pullRequests
		pullRequestList := pullRequestGroup.pullRequestList

		newSelectedRow := 0
		var currentlySelectedPullRequest *PullRequest
		if len(pullRequests) > 0 {
			currentlySelectedPullRequest = pullRequests[pullRequestList.SelectedRow]
		}
		numPRs := len(loadedPullRequests)
		pullRequests = make([]*PullRequest,numPRs)
		pullRequestList.Rows = make([]string, numPRs)
		counter := 0
		for _, pullRequestItem := range loadedPullRequests {
			listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
			pullRequestList.Rows[counter] = listLabel
			pullRequests[counter] = pullRequestItem
			if currentlySelectedPullRequest != nil && pullRequestItem.Id == currentlySelectedPullRequest.Id {
				newSelectedRow=counter
			}
			counter++
		}
		pullRequestGroup.pullRequests = pullRequests
		pullRequestList.SelectedRow = newSelectedRow
		ghui.UpdatePullRequestDetails(pullRequests, 0)

		ghui.render()

	})

	return &ghui
}


func (ghui *UI) renderPullRequestDetails() {
	ui.Render(ghui.reviewerList, ghui.status, ghui.pullRequestDetails, ghui.pullRequestBody)
}


func (ghui *UI) render() {
	ui.Render(ghui.reviewPullRequestGroup.pullRequestList,ghui.myPullRequestGroup.pullRequestList)
}

func (ghui *UI) Resize(height int, width int) {
	ghui.reviewPullRequestGroup.pullRequestList.SetRect(0, 0, width / 2, (height-3)/2)
	ghui.myPullRequestGroup.pullRequestList.SetRect(0, (height-3)/2, width / 2, height-3)
	ghui.status.SetRect(0, height-3, width, height)
	ghui.pullRequestDetails.SetRect(width / 2,0,width,7)
	ghui.reviewerList.SetRect(width / 2, 7, width, 15)
	ghui.pullRequestBody.SetRect(width / 2, 15, width, height-3)
}

func (ghui *UI) UpdateReviewPullRequestList(pullRequests []PullRequest) {

	ghui.status.Text = "loading pull requests"

	numPRs := len(pullRequests)
	ghui.reviewPullRequestGroup.pullRequestList.Rows = make([]string, numPRs)
	for i, pullRequestItem := range pullRequests {
		listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
		ghui.reviewPullRequestGroup.pullRequestList.Rows[i] = listLabel
	}

}

func (ghui *UI) UpdateMyPullRequestList(pullRequests []PullRequest) {

	ghui.status.Text = "loading pull requests"

	numPRs := len(pullRequests)
	ghui.myPullRequestGroup.pullRequestList.Rows = make([]string, numPRs)
	for i, pullRequestItem := range pullRequests {
		listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
		ghui.myPullRequestGroup.pullRequestList.Rows[i] = listLabel
	}

}


func (ghui *UI) NewFocus() {
	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	ghui.currentFocusedPullRequestGroup++
	currentlySelectedPullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(8)
	nextSelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	nextSelectedPullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)
	ghui.UpdatePullRequestDetails(nextSelectedPullRequestGroup.pullRequests, nextSelectedPullRequestGroup.pullRequestList.SelectedRow)
	ghui.render()
}

func (ghui *UI) RefreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
}

func (ghui *UI)UpdatePullRequestDetails(pullRequestList []*PullRequest, selectedPullRequest int) {

	currentlySelectedReview := ghui.reviewerList.SelectedRow
	pullRequestItem := pullRequestList[uint32(selectedPullRequest)]
	ghui.pullRequestDetails.WrapText = false
	ghui.pullRequestDetails.Text = fmt.Sprintf(" [ID](fg:white): %d\n [Title](fg:white): %s\n [Creator](fg:white): %s\n [Created](fg:white): %s\n [Updated](fg:white): %s", pullRequestItem.Id,pullRequestItem.Title, pullRequestItem.Creator.Username, pullRequestItem.CreatedAt, pullRequestItem.UpdatedAt)
	ghui.pullRequestBody.Text = fmt.Sprintf("%s", pullRequestItem.Body)

	ghui.reviewerList.Rows = make([]string, 0)
	counter := 0

	ghui.reviewerList.SelectedRowStyle = ui.NewStyle(ui.ColorWhite)
	for _, pullRequestReview := range pullRequestItem.PullRequestReviews {
		listLabel := fmt.Sprintf("%s %s", pullRequestReview[0].User.Username, pullRequestReview[0].Status)
		if pullRequestReview[0].Status == "APPROVED" && counter == currentlySelectedReview {
			ghui.reviewerList.SelectedRowStyle = ui.NewStyle(ui.ColorGreen)
		}
		ghui.reviewerList.Rows = append(ghui.reviewerList.Rows,listLabel)
		counter++
	}

	ghui.renderPullRequestDetails()

}


func (ghui *UI) openBrowser(selectedPullRequest int) {

	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	pullRequestItem := currentlySelectedPullRequestGroup.pullRequests[uint32(selectedPullRequest)]
	url := pullRequestItem.HtmlURL.String()
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


func (ghui *UI) EventLoop() {

	err := ui.Init()
	if err != nil {
		panic(err)
	}

	defer ui.Close()

	termWidth, termHeight := ui.TerminalDimensions()

	ghui.Resize(termHeight, termWidth)
	ghui.render()

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
		ghui.render()
	}

}
