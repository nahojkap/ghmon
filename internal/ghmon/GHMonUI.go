package ghmon

import (
	"fmt"
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	"log"
	"os/exec"
	"runtime"
)

type UI struct {
	ghMon *GHMon
	status *widgets.Paragraph
	pullRequestDetails *widgets.Paragraph
	pullRequestBody *widgets.Paragraph
	reviewPullRequestList *widgets.List
	myReviewPullRequestList *widgets.List
	reviewerList *widgets.List
	pullRequests []*PullRequest
	focusableWidgets []*ui.Block
	currentFocusedWidget int
}

func NewGHMonUI(ghm *GHMon) *UI {

	status := widgets.NewParagraph()
	status.Text = ""
	status.Title = "Status"

	reviewreviewPullRequestList := widgets.NewList()
	reviewreviewPullRequestList.Title = "Active Pull Requests"
	reviewreviewPullRequestList.WrapText = false
	reviewreviewPullRequestList.TextStyle = ui.NewStyle(15)
	reviewreviewPullRequestList.SelectedRowStyle = ui.NewStyle(ui.ColorWhite)

	myReviewPullRequestList := widgets.NewList()
	myReviewPullRequestList.Title = "Active Pull Requests"
	myReviewPullRequestList.WrapText = false
	myReviewPullRequestList.TextStyle = ui.NewStyle(15)
	myReviewPullRequestList.SelectedRowStyle = ui.NewStyle(ui.ColorWhite)

	reviewerList := widgets.NewList()
	reviewerList.Title = "Reviewers"
	reviewerList.WrapText = false
	reviewerList.TextStyle = ui.NewStyle(15)
	reviewerList.SelectedRowStyle = ui.NewStyle(ui.ColorWhite)

	pullRequestDetails := widgets.NewParagraph()

	pullRequestBody := widgets.NewParagraph()
	pullRequestBody.Title = "Details"

	ghui := UI{ghMon: ghm, reviewPullRequestList: reviewreviewPullRequestList, myReviewPullRequestList: myReviewPullRequestList, reviewerList: reviewerList, status: status, pullRequestDetails: pullRequestDetails, pullRequestBody: pullRequestBody}

	ghui.currentFocusedWidget = 0
	ghui.focusableWidgets = make([]*ui.Block,0)
	ghui.focusableWidgets = append(ghui.focusableWidgets, &ghui.reviewPullRequestList.Block, &ghui.reviewerList.Block)

	ghm.AddStatusListener(func (status string) {
		ghui.status.Text = " " + status
		ghui.render()
	})

	ghm.AddPullRequestListener(func (loadedPullRequests map[uint32]*PullRequest) {

		// FIXME: Should store currently selected PR and make sure
		// FIXME: that is displayed (if still in the list) after loading
		// FIXME: the new list of PRs

		newSelectedRow := 0
		var currentlySelectedPullRequest *PullRequest
		if len(ghui.pullRequests) > 0 {
			currentlySelectedPullRequest = ghui.pullRequests[ghui.reviewPullRequestList.SelectedRow]
		}
		numPRs := len(loadedPullRequests)
		pullRequests := make([]*PullRequest,numPRs)
		reviewreviewPullRequestList.Rows = make([]string, numPRs)
		counter := 0
		for _, pullRequestItem := range loadedPullRequests {
			listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
			reviewreviewPullRequestList.Rows[counter] = listLabel
			pullRequests[counter] = pullRequestItem
			if currentlySelectedPullRequest != nil && pullRequestItem.Id == currentlySelectedPullRequest.Id {
				newSelectedRow=counter
			}
			counter++
		}

		ghui.pullRequests = pullRequests
		ghui.reviewPullRequestList.SelectedRow = newSelectedRow
		ghui.UpdatePullRequestDetails(0)

		ghui.render()

	})

	return &ghui
}

func (ghui *UI) render() {
	ui.Render(ghui.reviewPullRequestList,ghui.reviewerList, ghui.pullRequestBody, ghui.status, ghui.pullRequestDetails)
}

func (ghui *UI) Resize(height int, width int) {

		ghui.reviewPullRequestList.SetRect(0, 0, width / 2, height-3/2)
		ghui.myReviewPullRequestList.SetRect(0, height-3/2, width / 2, height-3)
		ghui.status.SetRect(0, height-3, width, height)

		ghui.pullRequestDetails.SetRect(width / 2,0,width,7)
		ghui.reviewerList.SetRect(width / 2, 7, width, 15)
		ghui.pullRequestBody.SetRect(width / 2, 15, width, height-3)

}

func (ghui *UI)UpdatereviewPullRequestList(pullRequests []PullRequest) {

	ghui.status.Text = "loading pull requests"

	numPRs := len(pullRequests)
	ghui.reviewPullRequestList.Rows = make([]string, numPRs)
	for i, pullRequestItem := range pullRequests {
		listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
		ghui.reviewPullRequestList.Rows[i] = listLabel
	}

}

func (ghui *UI) SelectNextForFocus() {

	currentSelectedWidget := ghui.focusableWidgets[ghui.currentFocusedWidget % len(ghui.focusableWidgets)]
	ghui.currentFocusedWidget++
	currentSelectedWidget.BorderStyle = ui.NewStyle(15)
	nextSelectedWidget := ghui.focusableWidgets[ghui.currentFocusedWidget % len(ghui.focusableWidgets)]
	nextSelectedWidget.BorderStyle = ui.NewStyle(ui.ColorWhite)
	ghui.render()
}

func (ghui *UI) RefreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
}

func (ghui *UI)UpdatePullRequestDetails(selectedPullRequest int) {
	pullRequestItem := ghui.pullRequests[uint32(selectedPullRequest)]
	ghui.pullRequestDetails.WrapText = false
	ghui.pullRequestDetails.Text = fmt.Sprintf(" [ID](fg:99): \t\t%d\n [Title](fg:15): \t\t%s\n [Creator](fg:15): %s\n [Created](fg:15): %s\n [Updated](fg:15): %s", pullRequestItem.Id,pullRequestItem.Title, pullRequestItem.Creator.Username, pullRequestItem.CreatedAt, pullRequestItem.UpdatedAt)
	ghui.pullRequestBody.Text = fmt.Sprintf("%s", pullRequestItem.Body)

	numReviews := len(pullRequestItem.PullRequestReviews)

	ghui.reviewerList.Rows = make([]string, numReviews)
	counter := 0
	for _, pullRequestReview := range pullRequestItem.PullRequestReviews {
		listLabel := fmt.Sprintf("[%s](fg:76) %s", pullRequestReview[0].User.Username, pullRequestReview[0].Status)
		ghui.reviewerList.Rows[counter] = listLabel
		counter++
	}

}


func (ghui *UI) openBrowser(selectedPullRequest int) {

	pullRequestItem := ghui.ghMon.pullRequests[uint32(selectedPullRequest)]
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
		e := <-uiEvents
		switch e.ID {
		case "<Tab>":
			ghui.SelectNextForFocus()
		case "r":
			ghui.RefreshPullRequests()
		case "q", "<C-c>":
			return
		case "j", "<Down>":
			ghui.reviewPullRequestList.ScrollDown()
			ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
		case "k", "<Up>":
			ghui.reviewPullRequestList.ScrollUp()
			ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
		case "<C-d>":
			ghui.reviewPullRequestList.ScrollHalfPageDown()
			ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
		case "<C-u>":
			ghui.reviewPullRequestList.ScrollHalfPageUp()
			ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
		case "<C-f>":
			ghui.reviewPullRequestList.ScrollPageDown()
			ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
		case "<C-b>":

			ghui.reviewPullRequestList.ScrollPageUp()
			ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
		case "g":
			if previousKey == "g" {
				ghui.reviewPullRequestList.ScrollTop()
				ghui.UpdatePullRequestDetails(ghui.reviewPullRequestList.SelectedRow)
			}
		case "<Home>":
			ghui.reviewPullRequestList.ScrollTop()
		case "G", "<End>":
			ghui.reviewPullRequestList.ScrollBottom()
		case "<Enter>" :
			ghui.openBrowser(ghui.reviewPullRequestList.SelectedRow)
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
