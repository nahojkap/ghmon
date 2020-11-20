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
	pullRequestList *widgets.List
	reviewerList *widgets.List
	pullRequests []*PullRequest
}

func NewGHMonUI(ghm *GHMon) *UI {

	status := widgets.NewParagraph()
	status.Text = ""
	status.Title = "Status"

	pullRequestList := widgets.NewList()
	pullRequestList.Title = "Active Pull Requests"
	pullRequestList.WrapText = false
	pullRequestList.TextStyle = ui.NewStyle(15)
	pullRequestList.SelectedRowStyle = ui.NewStyle(ui.ColorWhite)

	reviewerList := widgets.NewList()
	reviewerList.Title = "Reviewers"
	reviewerList.WrapText = false
	reviewerList.TextStyle = ui.NewStyle(15)
	reviewerList.SelectedRowStyle = ui.NewStyle(ui.ColorWhite)

	pullRequestDetails := widgets.NewParagraph()

	pullRequestBody := widgets.NewParagraph()
	pullRequestBody.Title = "Details"

	ghui := UI{ghMon: ghm, pullRequestList: pullRequestList, reviewerList: reviewerList, status: status, pullRequestDetails: pullRequestDetails, pullRequestBody: pullRequestBody}

	ghm.AddStatusListener(func (status string) {
		ghui.status.Text = " " + status
		ghui.render()
	})

	ghm.AddPullRequestListener(func (loadedPullRequests map[uint32]*PullRequest) {

		// FIXME: Should store currently selected PR and make sure
		// FIXME: that is displayed (if still in the list) after loading
		// FIXME: the new list of PRs

		ghui.pullRequests = ghui.pullRequests[:0]

		numPRs := len(loadedPullRequests)
		pullRequestList.Rows = make([]string, numPRs)
		for i, pullRequestItem := range loadedPullRequests {
			listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
			pullRequestList.Rows[i] = listLabel
			ghui.pullRequests = append(ghui.pullRequests, pullRequestItem)
		}

		ghui.pullRequestList.SelectedRow = 0
		ghui.UpdatePullRequestDetails(0)

		ghui.render()

	})

	return &ghui
}

func (ghui *UI) render() {
	ui.Render(ghui.pullRequestList,ghui.reviewerList, ghui.pullRequestBody, ghui.status, ghui.pullRequestDetails)
}

func (ghui *UI) Resize(height int, width int) {

		ghui.pullRequestList.SetRect(0, 0, width / 2, height-3)
		ghui.status.SetRect(0, height-3, width, height)

		ghui.pullRequestDetails.SetRect(width / 2,0,width,7)
		ghui.reviewerList.SetRect(width / 2, 7, width, 15)
		ghui.pullRequestBody.SetRect(width / 2, 15, width, height-3)

}

func (ghui *UI)UpdatePullRequestList(pullRequests []PullRequest) {

	ghui.status.Text = "loading pull requests"

	numPRs := len(pullRequests)
	ghui.pullRequestList.Rows = make([]string, numPRs)
	for i, pullRequestItem := range pullRequests {
		listLabel := fmt.Sprintf("[%d] %s", pullRequestItem.Id, pullRequestItem.Title)
		ghui.pullRequestList.Rows[i] = listLabel
	}

}

func (ghui *UI)UpdatePullRequestDetails(selectedPullRequest int) {
	pullRequestItem := ghui.ghMon.pullRequests[uint32(selectedPullRequest)]
	ghui.pullRequestDetails.WrapText = false
	ghui.pullRequestDetails.Text = fmt.Sprintf(" [ID](fg:15): %d\n [Title](fg:15): %s\n [Creator](fg:15): %s\n [Created](fg:15): %s\n [Updated](fg:15): %s", pullRequestItem.Id,pullRequestItem.Title, pullRequestItem.Creator.Username, pullRequestItem.CreatedAt, pullRequestItem.UpdatedAt)
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
		case "q", "<C-c>":
			return
		case "j", "<Down>":
			ghui.pullRequestList.ScrollDown()
			ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
		case "k", "<Up>":
			ghui.pullRequestList.ScrollUp()
			ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
		case "<C-d>":
			ghui.pullRequestList.ScrollHalfPageDown()
			ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
		case "<C-u>":
			ghui.pullRequestList.ScrollHalfPageUp()
			ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
		case "<C-f>":
			ghui.pullRequestList.ScrollPageDown()
			ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
		case "<C-b>":

			ghui.pullRequestList.ScrollPageUp()
			ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
		case "g":
			if previousKey == "g" {
				ghui.pullRequestList.ScrollTop()
				ghui.UpdatePullRequestDetails(ghui.pullRequestList.SelectedRow)
			}
		case "<Home>":
			ghui.pullRequestList.ScrollTop()
		case "G", "<End>":
			ghui.pullRequestList.ScrollBottom()
		case "<Enter>" :
			ghui.openBrowser(ghui.pullRequestList.SelectedRow)
		case "<Resize>":
			payload := e.Payload.(ui.Resize)
			ghui.Resize(payload.Height, payload.Width)
			ui.Clear()
			ghui.render()
		}

		if previousKey == "g" {
			previousKey = ""
		} else {
			previousKey = e.ID
		}

		ghui.render()
	}

}