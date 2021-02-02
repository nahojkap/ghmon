	package ghmon

	import (
		"fmt"
		"github.com/gdamore/tcell/v2"
		tview "github.com/rivo/tview"
		"log"
		"os/exec"
		"runtime"
		"strings"
		"sync"
		"time"
	)

type PullRequestGroup struct {
	pullRequestType     PullRequestType
	pullRequestTable    *tview.Table
	pullRequestWrappers []*PullRequestWrapper
	currentlySelectedPullRequestWrapper *PullRequestWrapper
	currentlySelectedPullRequestWrapperIndex int
}

type UI struct {
	ghMon                   *GHMon
	uiLock sync.Mutex

	app 					*tview.Application

	status                  *tview.TextView

	pullRequestDetails      *tview.Table
	pullRequestBody         *tview.TextView
	reviewerTable           *tview.Table

	grid 					*tview.Grid
	reviewPullRequestGroup 	*PullRequestGroup

	timerCanceled chan bool

}

func NewGHMonUI(ghm *GHMon) *UI {

	tview.Styles.MoreContrastBackgroundColor = tcell.Color16
	tview.Styles.ContrastBackgroundColor = tcell.Color16
	tview.Styles.PrimitiveBackgroundColor = tcell.Color16

	reviewPullRequestTable := tview.NewTable().SetBorders(false).SetSeparator('|')

	reviewerTable := tview.NewTable()
	pullRequestDetails := tview.NewTable()
	pullRequestBody := tview.NewTextView()

	status := tview.NewTextView()
	status.SetTextAlign(tview.AlignLeft)
	status.SetText("")

	reviewPullRequestLabel := tview.NewTextView()
	reviewPullRequestLabel.SetTextAlign(tview.AlignLeft)
	reviewPullRequestLabel.SetText(" Pending Pull Request(s)")

	pullRequestDetailsLabel := tview.NewTextView()
	pullRequestDetailsLabel.SetTextAlign(tview.AlignLeft)
	pullRequestDetailsLabel.SetText(" Pull Request Details")

	descriptionLabel := tview.NewTextView()
	descriptionLabel.SetTextAlign(tview.AlignLeft)
	descriptionLabel.SetText(" Description")

	reviewersLabel := tview.NewTextView()
	reviewersLabel.SetTextAlign(tview.AlignLeft)
	reviewersLabel.SetText(" Reviewers")

	grid := tview.NewGrid().
		SetRows(1, -2, 1, 8, 1, -3, 1).
		SetColumns(-2,-3).
		SetBorders(true)

	grid.SetBackgroundColor(tcell.Color16)

	// FIXME: Layout for screens narrower than 100 cells

	// Layout for screens wider than 100 cells.
	grid.AddItem(reviewPullRequestLabel, 0, 0, 1, 2, 0, 0, false)
	grid.AddItem(reviewPullRequestTable, 1, 0, 1, 2, 0, 0, false)

	grid.AddItem(pullRequestDetailsLabel, 2, 0, 1, 1, 0, 0, false)
	grid.AddItem(pullRequestDetails, 3, 0, 1, 1, 0, 0, false)
	grid.AddItem(reviewersLabel, 4, 0, 1, 1, 0, 0, false)
	grid.AddItem(reviewerTable, 5, 0, 1, 1, 0, 0, false)

	grid.AddItem(descriptionLabel, 2, 1, 1, 1, 0, 0, false)
	grid.AddItem(pullRequestBody, 3, 1, 3, 1, 0, 0, false)

	grid.AddItem(status, 6, 0, 1, 2, 0, 0, false)
	app := tview.NewApplication()

	ghui := UI{
		ghMon: ghm,app: app, grid: grid, reviewerTable: reviewerTable,
		status: status, pullRequestDetails: pullRequestDetails,
		pullRequestBody:  pullRequestBody,
		timerCanceled: make(chan bool,1),
	}

	ghui.reviewPullRequestGroup = &PullRequestGroup{pullRequestType: Reviewer, pullRequestTable: reviewPullRequestTable}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// We navigate in between focuses here
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
				case 'Q':
				case 'q':
					ghui.app.Stop()
					return nil
			case 'R':
			case 'r':
				go ghui.RefreshPullRequests()
				return nil
				default:
			}

		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		return event, action
	})

	reviewPullRequestTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		// What here?
	}).SetSelectedFunc(func(row int, column int) {
		ghui.openBrowser(row)
	})
	reviewPullRequestTable.SetSelectable(true,false)
	reviewPullRequestTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return event
	})
	reviewPullRequestTable.SetSelectionChangedFunc(func(row, column int) {
		ghui.handlePullRequestSelectionChanged(ghui.reviewPullRequestGroup, row)
	})
	reviewPullRequestTable.SetMouseCapture(func(action tview.MouseAction,event *tcell.EventMouse) (tview.MouseAction,*tcell.EventMouse) {
		return action,nil
	})

	app.SetFocus(ghui.reviewPullRequestGroup.pullRequestTable)

	go ghui.app.QueueUpdateDraw(func() {
		app.SetFocus(ghui.reviewPullRequestGroup.pullRequestTable)
		ghui.handlePullRequestSelectionChanged(ghui.reviewPullRequestGroup, ghui.reviewPullRequestGroup.currentlySelectedPullRequestWrapperIndex)
	})


	return &ghui
}

func (ghui *UI) getCurrentlySelectedPullRequest() *PullRequestWrapper {
	return ghui.reviewPullRequestGroup.currentlySelectedPullRequestWrapper
}

func (ghui *UI) handlePullRequestSelectionChanged(pullRequestGroup *PullRequestGroup, row int) {

	if len(pullRequestGroup.pullRequestWrappers) > 0 && row >= 0 {

		ghui.updateSelectedPullRequestWrapper(pullRequestGroup, row)

		go ghui.handlePullRequestSelected(pullRequestGroup.currentlySelectedPullRequestWrapper)

		go ghui.app.QueueUpdateDraw(func() {
			ghui.UpdatePullRequestDetails(pullRequestGroup)
		})
	}
	// FIXME: else, clear the pull request details
}


func (ghui *UI) startSeenTimer(pullRequestWrapper *PullRequestWrapper) {

	select {
		case <-time.After(5 * time.Second):
			// do something for timeout, like change state
			ghui.ghMon.Logger().Printf("Seen timer timeout for %d", pullRequestWrapper.Id)
			// if current pull request is still the same one as we started 'seeing'
			currentlySelectedPullRequest := ghui.getCurrentlySelectedPullRequest()
			if currentlySelectedPullRequest != nil && currentlySelectedPullRequest.Id == pullRequestWrapper.Id {
				ghui.ghMon.Logger().Printf("Marking %d as seen", pullRequestWrapper.Id)
				ghui.ghMon.UpdateSeen(pullRequestWrapper, true)
				ghui.app.QueueUpdateDraw(func() {
					ghui.handlePullRequestsUpdates(pullRequestWrapper.PullRequestType, ghui.getPullRequestGroup(pullRequestWrapper).pullRequestWrappers)
				})
			}
		case <-ghui.timerCanceled:
			ghui.ghMon.Logger().Printf("Timer cancelled for %d", pullRequestWrapper.Id)
	}

}

func (ghui *UI) handlePullRequestSelected(pullRequestWrapper *PullRequestWrapper) {

	ghui.ghMon.Logger().Printf("Selected pull request %d", pullRequestWrapper.Id)
	ghui.ghMon.Logger().Printf("Pull Request Seen? %t", pullRequestWrapper.Seen)

	// FIXME: Slightly weird since we essentially throw away the channel each time
	ghui.timerCanceled <- true
	ghui.timerCanceled = make(chan bool, 1)

	if !pullRequestWrapper.Seen {
		ghui.ghMon.Logger().Printf("%d not seen, will start timer", pullRequestWrapper.Id)
		go ghui.startSeenTimer(pullRequestWrapper)
	} else {
		ghui.ghMon.Logger().Printf("%d already seen, will not start new timer", pullRequestWrapper.Id)
	}

}

func (ghui *UI) updateSelectedPullRequestWrapper(pullRequestGroup *PullRequestGroup, index int) {
	if len(pullRequestGroup.pullRequestWrappers) > 0 && index >= 0 {
		pullRequestGroup.currentlySelectedPullRequestWrapperIndex = index
		pullRequestGroup.currentlySelectedPullRequestWrapper = pullRequestGroup.pullRequestWrappers[index]
	}
}

func (ghui *UI) RefreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
}

func (ghui *UI) getPullRequestReviewColorString(pullRequestReview *PullRequestReview) (color string) {
	switch pullRequestReview.Status {
	case PullRequestReviewStatusApproved:
		color = "green"
	case PullRequestReviewStatusCommented:
		color = "orange"
	case PullRequestReviewStatusChangesRequested:
		color = "red"
	case PullRequestReviewStatusPending:
		color = "yellow"
	case PullRequestReviewStatusRequested:
		color = "white"
	default:
		color = "white"
	}
	return
}

func (ghui *UI) getPullRequestReviewColor(pullRequestReview *PullRequestReview) (color tcell.Color) {
	switch pullRequestReview.Status {
	case PullRequestReviewStatusApproved:
		color = tcell.ColorGreen
	case PullRequestReviewStatusCommented:
		color = tcell.ColorOrange
	case PullRequestReviewStatusChangesRequested:
		color = tcell.ColorDarkRed
	case PullRequestReviewStatusPending:
		color = tcell.ColorYellow
	case PullRequestReviewStatusRequested:
		color = tcell.ColorWhite
	default:
		color = tcell.ColorWhite
	}
	return
}

func (ghui *UI) getOverallPullRequestColor(pullRequestWrapper *PullRequestWrapper) tcell.Color {

	pullRequestScore := pullRequestWrapper.Score

	if pullRequestScore.ChangesRequested != 0 {
		return tcell.ColorRed
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		return tcell.ColorGreen
	}

	if pullRequestScore.Comments > 0 {
		return tcell.ColorOrange
	}

	return tcell.ColorWhite

}

func (ghui *UI) getOverallPullRequestColorStr(pullRequestWrapper *PullRequestWrapper) string {

	pullRequestScore := pullRequestWrapper.Score

	if pullRequestScore.ChangesRequested != 0 {
		return "red"
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		return "green"
	}

	if pullRequestScore.Comments > 0 {
		return "orange"
	}

	return "white"

}

func (ghui *UI)escapeSquareBracketsInString(str string) string {

	if !strings.ContainsRune(str,'[') {
		return str
	}

	var b strings.Builder
	b.Grow(len(str) + 5)

	inSquareBracket := false
	for _,r := range str {
		switch r {
		case '[' :
			if !inSquareBracket {
				inSquareBracket = true
			}
			b.WriteRune(r)
		case ']' :
			if inSquareBracket {
				inSquareBracket = false
				b.WriteRune('[')
			}
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (ghui *UI)UpdatePullRequestDetails(pullRequestGroup *PullRequestGroup) {

	pullRequestWrappers := pullRequestGroup.pullRequestWrappers

	if len(pullRequestWrappers) == 0 {
		return
	}

	pullRequestWrapper := pullRequestWrappers[uint32(pullRequestGroup.currentlySelectedPullRequestWrapperIndex)]

	ghui.pullRequestDetails.SetCell(0,0,tview.NewTableCell(" [::b]ID:"))
	ghui.pullRequestDetails.SetCell(0,1,tview.NewTableCell(fmt.Sprintf("[::b]%d",pullRequestWrapper.PullRequest.Id)))
	ghui.pullRequestDetails.SetCell(1,0,tview.NewTableCell(" [::b]Title:"))
	ghui.pullRequestDetails.SetCell(1,1,tview.NewTableCell(fmt.Sprintf("[::b]%s",ghui.escapeSquareBracketsInString(pullRequestWrapper.PullRequest.Title))))
	ghui.pullRequestDetails.SetCell(2,0,tview.NewTableCell(" [::b]Creator: "))
	ghui.pullRequestDetails.SetCell(2,1,tview.NewTableCell(fmt.Sprintf("[#00ff1a]%s",pullRequestWrapper.PullRequest.Creator.Username)))
	ghui.pullRequestDetails.SetCell(3,0,tview.NewTableCell(" [::b]Created: "))
	ghui.pullRequestDetails.SetCell(3,1,tview.NewTableCell(pullRequestWrapper.PullRequest.CreatedAt.String()))
	ghui.pullRequestDetails.SetCell(4,0,tview.NewTableCell(" [::b]Updated: "))
	ghui.pullRequestDetails.SetCell(4,1,tview.NewTableCell(pullRequestWrapper.PullRequest.UpdatedAt.String()))
	ghui.pullRequestDetails.SetCell(5,0,tview.NewTableCell(" [::b]First Seen: "))
	ghui.pullRequestDetails.SetCell(5,1,tview.NewTableCell(pullRequestWrapper.FirstSeen.String()))
	ghui.pullRequestDetails.SetCell(5,0,tview.NewTableCell(" [::b]Score: "))
	ghui.pullRequestDetails.SetCell(5,1,tview.NewTableCell(fmt.Sprintf("[::b]%f",pullRequestWrapper.Score.Total)))
	ghui.pullRequestBody.SetText(fmt.Sprintf("%s", pullRequestWrapper.PullRequest.Body))

	ghui.reviewerTable.Clear()

	for i, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByPriority {
		status := fmt.Sprintf("[%s][%s[]", ghui.getPullRequestReviewColorString(pullRequestReviews[0]),ghui.ghMon.ConvertPullRequestReviewStateToString(pullRequestReviews[0].Status))
		ghui.reviewerTable.SetCell(i, 1, tview.NewTableCell(status))
		ghui.reviewerTable.SetCell(i, 2, tview.NewTableCell(pullRequestReviews[0].User.Username))
	}
}


func (ghui *UI) openBrowser(selectedPullRequest int) {

	pullRequest := ghui.reviewPullRequestGroup.pullRequestWrappers[uint32(selectedPullRequest)]
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

func (ghui *UI) getPullRequestGroup(pullRequestWrapper *PullRequestWrapper) *PullRequestGroup {
	return ghui.reviewPullRequestGroup
}

func (ghui *UI) getPullRequestGroupFromType(pullRequestType PullRequestType) *PullRequestGroup {
	return ghui.reviewPullRequestGroup
}

func padToLen(str string, requiredLen int) string {
	paddedString := " "
	paddedString += str
	for len(paddedString) < (requiredLen-2) {
		paddedString += " "
	}
	paddedString += " "
	return paddedString
}

func (ghui *UI) HasPullReviewStatus(pullRequstReviewStatus PullRequestReviewStatus, pullRequestWrapper *PullRequestWrapper) bool {
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		for _, pullRequestReview := range pullRequestReviews {
			if pullRequestReview.Status == pullRequstReviewStatus {
				return true
			}
		}

	}
	return false
}

func (ghui *UI)handlePullRequestsUpdates(pullRequestType PullRequestType, loadedPullRequestWrappers []*PullRequestWrapper) {

	if len(loadedPullRequestWrappers) <= 0 {
		return
	}

	pullRequestGroup := ghui.getPullRequestGroupFromType(pullRequestType)

	pullRequestTable := pullRequestGroup.pullRequestTable
	pullRequestGroup.pullRequestTable.Clear()

	for counter, pullRequestWrapper := range loadedPullRequestWrappers {

		pullRequestItem := pullRequestWrapper.PullRequest

		seen := " "
		if !pullRequestWrapper.Seen {
			seen = "*"
		}

		_,_, width, _ := pullRequestTable.GetRect()

		// We can expand the title and repo fields to ensure consistent display
		// AvailableSpace := width - border (2) + dividers (3 * 6) + Seen (1) + heat pattern (5) + brief status (8) + Date (30) + Repo Name (30) + User (20)
		availableSpace := width - (2 + 3*6 + 1 + 5 + 8 + 30 + 35 + 20)

		expandedRepoName := padToLen(pullRequestItem.Repo.Name, 35	)
		expandedTitle := padToLen(ghui.escapeSquareBracketsInString(pullRequestItem.Title), availableSpace)
		expandedDate := padToLen(pullRequestItem.UpdatedAt.String(), 30)
		expandedAttributes := padToLen(ghui.getPullRequestReviewStatusString(pullRequestWrapper), 7)
		expandedSeen := padToLen(seen, 3)
		expandedUser := padToLen(pullRequestItem.Creator.Username, 20)

		expandedHeatPattern := ghui.getHeatPattern(pullRequestWrapper)

		pullRequestTable.SetCell(counter,0, tview.NewTableCell(expandedSeen))
		pullRequestTable.SetCell(counter,1, tview.NewTableCell(expandedHeatPattern))
		pullRequestTable.SetCell(counter,2, tview.NewTableCell(expandedAttributes))
		pullRequestTable.SetCell(counter,3,tview.NewTableCell(expandedTitle))
		pullRequestTable.SetCell(counter,4, tview.NewTableCell(expandedRepoName))
		pullRequestTable.SetCell(counter,5, tview.NewTableCell(expandedUser))
		pullRequestTable.SetCell(counter,6, tview.NewTableCell(expandedDate))

		counter++
	}

	pullRequestGroup.pullRequestWrappers = loadedPullRequestWrappers

	go ghui.app.QueueUpdateDraw(func() {
		ghui.handlePullRequestSelectionChanged(pullRequestGroup, pullRequestGroup.currentlySelectedPullRequestWrapperIndex)
	})

}

func (ghui *UI)getPullRequestReviewStatusString(pullRequestWrapper *PullRequestWrapper) string {

	statusString := []byte{'-','-','-','-','-','-','-'}

	if ghui.HasPullReviewStatus(PullRequestReviewStatusPending, pullRequestWrapper) {
		statusString[0] = 'P'
	}

	if ghui.HasPullReviewStatus(PullRequestReviewStatusRequested, pullRequestWrapper) {
		statusString[1] = 'R'
	}
	if ghui.HasPullReviewStatus(PullRequestReviewStatusCommented, pullRequestWrapper) {
		statusString[2] = 'C'
	}
	if ghui.HasPullReviewStatus(PullRequestReviewStatusApproved, pullRequestWrapper) {
		statusString[3] = 'A'
	}
	if ghui.HasPullReviewStatus(PullRequestReviewStatusChangesRequested, pullRequestWrapper) {
		statusString[4] = 'B'
	}
	if ghui.HasPullReviewStatus(PullRequestReviewStatusDismissed, pullRequestWrapper) {
		statusString[5] = 'D'
	}
	if ghui.HasPullReviewStatus(PullRequestReviewStatusUnknown, pullRequestWrapper) {
		statusString[6] = 'U'
	}

	return string(statusString)


}

func (ghui *UI) handlePullRequestUpdated(loadedPullRequest *PullRequestWrapper) {

	currentlySelectedPullRequestWrapper := ghui.reviewPullRequestGroup.currentlySelectedPullRequestWrapper
	if currentlySelectedPullRequestWrapper != nil && currentlySelectedPullRequestWrapper.Id == loadedPullRequest.Id {
		ghui.UpdatePullRequestDetails(ghui.reviewPullRequestGroup)
	}
}

func (ghui *UI) handleStatusUpdate(status string) {
	ghui.status.SetText(" " + status)
}

func (ghui *UI) pollEvents() {
	events := ghui.ghMon.events
	for {
		event := <-events
		switch event.eventType {
		case PullRequestUpdated:
			go ghui.app.QueueUpdateDraw(func() {
				ghui.handlePullRequestUpdated(event.payload.(*PullRequestWrapper))
				ghui.handlePullRequestsUpdates(Reviewer,ghui.getPullRequestGroup(event.payload.(*PullRequestWrapper)).pullRequestWrappers)
			})
		case PullRequestsUpdates:

			go ghui.app.QueueUpdateDraw(func() {
				pullRequestsUpdatesEvent := event.payload.(PullRequestsUpdatesEvent)
				ghui.handlePullRequestsUpdates(pullRequestsUpdatesEvent.pullRequestType, pullRequestsUpdatesEvent.pullRequestWrappers)
			})

		case Status:
			go ghui.handleStatusUpdate(event.payload.(string))
		}
	}
}

func (ghui *UI) EventLoop() {

	go ghui.pollEvents()

	if err := ghui.app.SetRoot(ghui.grid, true).EnableMouse(false).Run(); err != nil {
		panic(err)
	}

	//err := ui.Init()
	//if err != nil {
	//	panic(err)
	//}
	//
	//defer ui.Close()
	//
	//termWidth, termHeight := ui.TerminalDimensions()
	//
	//ghui.Resize(termHeight, termWidth)
	//go ghui.renderAll()
	//
	//go ghui.pollEvents()
	//
	//previousKey := ""
	//uiEvents := ui.PollEvents()
	//for {
	//	e := <-uiEvents
	//	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	//	switch e.ID {
	//	case "<Tab>":
	//		ghui.NewFocus()
	//	case "r":
	//		ghui.RefreshPullRequests()
	//	case "q", "<C-c>":
	//		return
	//	case "j", "<Down>":
	//		if len(currentlySelectedPullRequestGroup.pullRequestWrappers) > 0 {
	//			if currentlySelectedPullRequestGroup.pullRequestList.SelectedRow != (len(currentlySelectedPullRequestGroup.pullRequestList.Rows)-1) {
	//				currentlySelectedPullRequestGroup.pullRequestList.ScrollDown()
	//				ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
	//			}
	//		}
	//	case "k", "<Up>":
	//		if len(currentlySelectedPullRequestGroup.pullRequestWrappers) > 0 {
	//			if currentlySelectedPullRequestGroup.pullRequestList.SelectedRow != 0 {
	//				currentlySelectedPullRequestGroup.pullRequestList.ScrollUp()
	//				ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
	//			}
	//		}
	//	case "g":
	//		if previousKey == "g" {
	//			currentlySelectedPullRequestGroup.pullRequestList.ScrollTop()
	//			ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup.pullRequestWrappers, currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
	//		}
	//	case "<Home>":
	//		currentlySelectedPullRequestGroup.pullRequestList.ScrollTop()
	//	case "G", "<End>":
	//		currentlySelectedPullRequestGroup.pullRequestList.ScrollBottom()
	//	case "<Enter>" :
	//		ghui.openBrowser(currentlySelectedPullRequestGroup.pullRequestList.SelectedRow)
	//	case "<Resize>":
	//		payload := e.Payload.(ui.Resize)
	//		ghui.Resize(payload.Height, payload.Width)
	//		ui.Clear()
	//	}
	//
	//	if previousKey == "g" {
	//		previousKey = ""
	//	} else {
	//		previousKey = e.ID
	//	}
	//
	//	// We always render!
	//	go ghui.renderAll()
	//}

}

func (ghui *UI) getHeatPattern(wrapper *PullRequestWrapper) string {
	if wrapper.Score.Total < 25 {
		return "[red]▒▒▒▒▒▒"
	}
	if wrapper.Score.Total < 50	 {
		return "[orange]▒▒▒▒▒▒"
	}
	return "[green]▒▒▒▒▒▒"
}
