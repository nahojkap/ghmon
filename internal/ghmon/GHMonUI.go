	package ghmon

	import (
		"fmt"
		"github.com/gdamore/tcell/v2"
		tview "gitlab.com/tslocum/cview"
		"log"
		"os/exec"
		"runtime"
		"strings"
		"sync"
		"time"
		"github.com/andanhm/go-prettytime"
	)


type PullRequestEntry struct {
	tableIndex int
	pullRequestWrapper *PullRequestWrapper
}

type PullRequestGroup struct {
	pullRequestTable    *tview.Table
	pullRequestEntries []*PullRequestEntry
	currentlySelectedPullRequestEntry *PullRequestEntry
	currentlySelectedPullRequestEntryIndex int
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

	reviewPullRequestTable := tview.NewTable()
	reviewPullRequestTable.SetBorders(false)
	reviewPullRequestTable.SetSeparator('|')
	reviewPullRequestTable.SetBackgroundColor(tcell.Color16)
	reviewPullRequestTable.SetEvaluateAllRows(true)

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

	grid := tview.NewGrid()
	grid.SetRows(1, -2, 1, 8, 1, -3, 1)
	grid.SetColumns(-2,-3)
	grid.SetBorders(true)
	grid.SetBackgroundColor(tcell.Color16)
	grid.SetBackgroundTransparent(false)

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

	ghui := UI {
		ghMon: ghm,app: app, grid: grid, reviewerTable: reviewerTable,
		status: status, pullRequestDetails: pullRequestDetails,
		pullRequestBody:  pullRequestBody,
		timerCanceled: make(chan bool,1),
		reviewPullRequestGroup: &PullRequestGroup{pullRequestTable: reviewPullRequestTable},
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// We navigate in between focuses here
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
				case 'Q','q' :
					ghui.app.Stop()
					return nil
			case 'R', 'r' :
				go ghui.refreshPullRequests()
				return nil
			case 'p' :
				go ghui.purgePullRequests()
				return nil
			case 'z' :
				go ghui.snoozePullRequest()
				return nil
			default:
			}

		}
		return event
	})

	reviewPullRequestTable.Select(0, 0)
	reviewPullRequestTable.SetFixed(0, 0)
	reviewPullRequestTable.SetDoneFunc(func(key tcell.Key) {
		// What here?
	})
	reviewPullRequestTable.SetSelectedFunc(func(row int, column int) {
		ghui.openBrowser(ghui.reviewPullRequestGroup.pullRequestEntries[row].pullRequestWrapper)
	})
	reviewPullRequestTable.SetSelectable(true,false)

	reviewPullRequestTable.SetSelectionChangedFunc(func(row, column int) {
		ghui.handlePullRequestSelectionChanged(ghui.reviewPullRequestGroup, row)
	})

	reviewPullRequestTable.SetInputCapture(app.GetInputCapture())

	app.SetFocus(ghui.reviewPullRequestGroup.pullRequestTable)

	go ghui.app.QueueUpdateDraw(func() {
		app.SetFocus(ghui.reviewPullRequestGroup.pullRequestTable)
	})


	return &ghui
}

func (ghui *UI) getCurrentlySelectedPullRequest() *PullRequestEntry {
	return ghui.reviewPullRequestGroup.currentlySelectedPullRequestEntry
}

func (ghui *UI) handlePullRequestSelectionChanged(pullRequestGroup *PullRequestGroup, row int) {

	pullRequestGroup.currentlySelectedPullRequestEntryIndex = row
	pullRequestGroup.currentlySelectedPullRequestEntry = pullRequestGroup.pullRequestEntries[row]

	go ghui.app.QueueUpdateDraw(func() {
		ghui.handlePullRequestSelected(pullRequestGroup.pullRequestEntries[row])
	})
}

func (ghui *UI) startSeenTimer(pullRequestWrapper *PullRequestWrapper) {

	select {
		case <-time.After(5 * time.Second):
			// do something for timeout, like change state
			ghui.ghMon.Logger().Printf("Seen timer timeout for %d", pullRequestWrapper.Id)
			// if current pull request is still the same one as we started 'seeing'
			currentlySelectedPullRequest := ghui.getCurrentlySelectedPullRequest()

			if currentlySelectedPullRequest != nil && currentlySelectedPullRequest.pullRequestWrapper.Id == pullRequestWrapper.Id {
				ghui.ghMon.Logger().Printf("Marking %d as seen", pullRequestWrapper.Id)
				ghui.ghMon.UpdateSeen(pullRequestWrapper, true)
				ghui.app.QueueUpdateDraw(func() {
					ghui.handlePullRequestUpdated(pullRequestWrapper)
				})
			}
		case <-ghui.timerCanceled:
			ghui.ghMon.Logger().Printf("Timer cancelled for %d", pullRequestWrapper.Id)
	}

}

func (ghui *UI) handlePullRequestSelected(pullRequestEntry *PullRequestEntry) {

	pullRequestWrapper := pullRequestEntry.pullRequestWrapper
	ghui.ghMon.Logger().Printf("Selected pull request %d", pullRequestWrapper.Id)
	ghui.ghMon.Logger().Printf("Pull Request Seen? %t", pullRequestWrapper.Seen)

	ghui.updatePullRequestDetails(pullRequestWrapper)

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

func (ghui *UI) refreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
}

func (ghui *UI) snoozePullRequest() {
}

func (ghui *UI) purgePullRequests() {
	ghui.ghMon.PurgeDeletedPullRequests()
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

func (ghui *UI) openBrowser(pullRequestWrapper *PullRequestWrapper) {

	url := pullRequestWrapper.PullRequest.HtmlURL.String()

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

func pruneTo(str string, requiredLen int) string {
	if len(str) >= requiredLen {

		if len(str) > 3 {
			trimmedString := string(str[0 : requiredLen-3])
			trimmedString += "..."
			return trimmedString
		}
	}
	return str
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

func (ghui *UI) hasPullReviewStatus(pullRequstReviewStatus PullRequestReviewStatus, pullRequestWrapper *PullRequestWrapper) bool {
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		for _, pullRequestReview := range pullRequestReviews {
			if pullRequestReview.Status == pullRequstReviewStatus {
				return true
			}
		}

	}
	return false
}

func (ghui *UI) getHeatPattern(wrapper *PullRequestWrapper) (int, string) {

	if wrapper.Deleted {
		return 0,""
	}

	if wrapper.Score.Total > 75 {
		return 5,"[red]▒▒▒▒▒▒"
	}
	if wrapper.Score.Total > 50	 {
		return 8,"[orange]▒▒▒▒▒▒"
	}
	return 8, "[green]▒▒▒▒▒▒"
}

func (ghui *UI) updatePullRequestDetails(pullRequestWrapper *PullRequestWrapper) {

	ghui.pullRequestDetails.SetCell(0,0,tview.NewTableCell(" [::b]ID:"))
	ghui.pullRequestDetails.SetCell(0,1,tview.NewTableCell(fmt.Sprintf("[::b]%d",pullRequestWrapper.PullRequest.Id)))
	ghui.pullRequestDetails.SetCell(1,0,tview.NewTableCell(" [::b]Title:"))
	ghui.pullRequestDetails.SetCell(1,1,tview.NewTableCell(fmt.Sprintf("[::b]%s",ghui.escapeSquareBracketsInString(pullRequestWrapper.PullRequest.Title))))
	ghui.pullRequestDetails.SetCell(2,0,tview.NewTableCell(" [::b]Creator: "))
	ghui.pullRequestDetails.SetCell(2,1,tview.NewTableCell(fmt.Sprintf("[#00ff1a::]%s",pullRequestWrapper.PullRequest.Creator.Username)))
	ghui.pullRequestDetails.SetCell(3,0,tview.NewTableCell(" [::b]Created: "))
	ghui.pullRequestDetails.SetCell(3,1,tview.NewTableCell(pullRequestWrapper.PullRequest.CreatedAt.String()))
	ghui.pullRequestDetails.SetCell(4,0,tview.NewTableCell(" [::b]Updated: "))
	ghui.pullRequestDetails.SetCell(4,1,tview.NewTableCell(prettytime.Format(pullRequestWrapper.PullRequest.UpdatedAt)))
	ghui.pullRequestDetails.SetCell(5,0,tview.NewTableCell(" [::b]First Seen: "))
	ghui.pullRequestDetails.SetCell(5,1,tview.NewTableCell(pullRequestWrapper.FirstSeen.String()))
	ghui.pullRequestDetails.SetCell(6,0,tview.NewTableCell(" [::b]Score: "))
	ghui.pullRequestDetails.SetCell(6,1,tview.NewTableCell(fmt.Sprintf("[::b]%f",pullRequestWrapper.Score.Total)))
	ghui.pullRequestDetails.SetCell(7,0,tview.NewTableCell(" [::b]Deleted: "))
	ghui.pullRequestDetails.SetCell(7,1,tview.NewTableCell(fmt.Sprintf("[::b]%t",pullRequestWrapper.Deleted)))
	ghui.pullRequestBody.SetText(fmt.Sprintf("%s", pullRequestWrapper.PullRequest.Body))

	ghui.reviewerTable.Clear()
	for i, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByPriority {
		status := fmt.Sprintf("[%s][%s[]", ghui.getPullRequestReviewColorString(pullRequestReviews[0]),ghui.ghMon.ConvertPullRequestReviewStateToString(pullRequestReviews[0].Status))
		ghui.reviewerTable.SetCell(i, 1, tview.NewTableCell(status))
		ghui.reviewerTable.SetCell(i, 2, tview.NewTableCell(pullRequestReviews[0].User.Username))
		ghui.reviewerTable.SetCell(i, 3, tview.NewTableCell(fmt.Sprintf("[%f[]", pullRequestReviews[0].Score)))

	}
}

func (ghui *UI) updatePullRequestEntry(pullRequestEntry *PullRequestEntry) {

	pullRequestGroup := ghui.reviewPullRequestGroup
	pullRequestTable := pullRequestGroup.pullRequestTable

	pullRequestWrapper := pullRequestEntry.pullRequestWrapper
	pullRequestItem := pullRequestWrapper.PullRequest

	seen := " "
	if !pullRequestWrapper.Seen {
		seen = "*"
	}

	_,_, width, _ := pullRequestTable.GetRect()

	// We can expand the title and repo fields to ensure consistent display
	// AvailableSpace := width - border (2) + dividers (3 * 6) + Seen (1) + heat pattern (5) + brief status (9) + Date (30) + Repo Name (35) + User (20)
	availableSpace := width - (2 + 3*6 + 1 + 5 + 9 + 30 + 35 + 20)

	expandedRepoName := padToLen(pruneTo(pullRequestItem.Repo.Name, 33), 35)
	title := ghui.escapeSquareBracketsInString(pullRequestItem.Title)

	stylingLength := 0
	if pullRequestWrapper.Deleted {
		title = "[::s]" + title + "[::-]"
		stylingLength = 10
	}
	expandedTitle := padToLen(title, availableSpace-stylingLength)


	expandedDate := padToLen(pullRequestItem.UpdatedAt.String(), 30)
	expandedAttributes := padToLen(ghui.getPullRequestReviewStatusString(pullRequestWrapper), 9)
	expandedSeen := padToLen(seen, 3)
	expandedUser := padToLen(pullRequestItem.Creator.Username, 20)

	coloringLength, heatPattern := ghui.getHeatPattern(pullRequestWrapper)
	expandedHeatPattern := padToLen(heatPattern,9-coloringLength)

	pullRequestTable.SetCell(pullRequestEntry.tableIndex,0, tview.NewTableCell(expandedSeen))
	pullRequestTable.SetCell(pullRequestEntry.tableIndex,1, tview.NewTableCell(expandedHeatPattern))
	pullRequestTable.SetCell(pullRequestEntry.tableIndex,2, tview.NewTableCell(expandedAttributes))
	pullRequestTable.SetCell(pullRequestEntry.tableIndex,3,tview.NewTableCell(expandedTitle))
	pullRequestTable.SetCell(pullRequestEntry.tableIndex,4, tview.NewTableCell(expandedRepoName))
	pullRequestTable.SetCell(pullRequestEntry.tableIndex,5, tview.NewTableCell(expandedUser))
	pullRequestTable.SetCell(pullRequestEntry.tableIndex,6, tview.NewTableCell(expandedDate))

}

func (ghui *UI)handlePullRequestsUpdates(loadedPullRequestWrappers []*PullRequestWrapper) {

	if len(loadedPullRequestWrappers) <= 0 {
		return
	}

	selectedIndex := 0
	var currentlySelectedEntry *PullRequestEntry = nil

	if len(ghui.reviewPullRequestGroup.pullRequestEntries) > 0 {
		selectedIndex,_ = ghui.reviewPullRequestGroup.pullRequestTable.GetSelection()
		currentlySelectedEntry = ghui.reviewPullRequestGroup.pullRequestEntries[selectedIndex]
	}

	pullRequestGroup := ghui.reviewPullRequestGroup
	pullRequestGroup.pullRequestTable.Clear()
	pullRequestGroup.pullRequestEntries = make([]*PullRequestEntry, 0)

	for counter, pullRequestWrapper := range loadedPullRequestWrappers {

		if currentlySelectedEntry != nil && pullRequestWrapper.Id == currentlySelectedEntry.pullRequestWrapper.Id {
			selectedIndex = counter
		}

		pullRequestEntry := &PullRequestEntry{pullRequestWrapper: pullRequestWrapper,tableIndex: counter}
		ghui.updatePullRequestEntry(pullRequestEntry)
		pullRequestGroup.pullRequestEntries = append(pullRequestGroup.pullRequestEntries, pullRequestEntry)
	}

	for selectedIndex >= pullRequestGroup.pullRequestTable.GetRowCount() {
		selectedIndex--
	}

	pullRequestGroup.pullRequestTable.Select(selectedIndex,0)

}

func (ghui *UI)getPullRequestReviewStatusString(pullRequestWrapper *PullRequestWrapper) string {

	statusString := []byte{'-','-','-','-','-','-','-', '-'}

	if ghui.hasPullReviewStatus(PullRequestReviewStatusPending, pullRequestWrapper) {
		statusString[0] = 'P'
	}

	if ghui.hasPullReviewStatus(PullRequestReviewStatusRequested, pullRequestWrapper) {
		statusString[1] = 'R'
	}
	if ghui.hasPullReviewStatus(PullRequestReviewStatusCommented, pullRequestWrapper) {
		statusString[2] = 'C'
	}
	if ghui.hasPullReviewStatus(PullRequestReviewStatusApproved, pullRequestWrapper) {
		statusString[3] = 'A'
	}
	if ghui.hasPullReviewStatus(PullRequestReviewStatusChangesRequested, pullRequestWrapper) {
		statusString[4] = 'B'
	}

	if ghui.hasPullReviewStatus(PullRequestReviewStatusDismissed, pullRequestWrapper) {
		statusString[5] = 'D'
	}

	if ghui.hasPullReviewStatus(PullRequestReviewStatusUnknown, pullRequestWrapper) {
		statusString[6] = 'U'
	}

	if pullRequestWrapper.Deleted {
		statusString[7] = 'X'
	}

	return string(statusString)


}

func (ghui *UI) handlePullRequestDeleted(pullRequestWrapper *PullRequestWrapper) {

	// Should simply update the pull request entry at this point - the list will be updated later

	var pullRequestEntry *PullRequestEntry = nil
	var pullRequestEntryIndex int = -1
	for index, existingPullRequestEntry := range ghui.reviewPullRequestGroup.pullRequestEntries {
		if existingPullRequestEntry.pullRequestWrapper.Id == pullRequestWrapper.Id {
			pullRequestEntry = existingPullRequestEntry
			pullRequestEntry.pullRequestWrapper = pullRequestWrapper
			pullRequestEntryIndex = index
		}
	}

	if pullRequestEntry != nil {
		ghui.updatePullRequestEntry(pullRequestEntry)

		selectedRow,_ := ghui.reviewPullRequestGroup.pullRequestTable.GetSelection()
		if  selectedRow == pullRequestEntryIndex {
			ghui.updatePullRequestDetails(pullRequestWrapper)
		}
	}


}

func (ghui *UI) handlePullRequestUpdated(pullRequestWrapper *PullRequestWrapper) {

	// First find the item in the current list of pull request entries

	var pullRequestEntry *PullRequestEntry = nil
	var pullRequestEntryIndex int = -1
	for index, existingPullRequestEntry := range ghui.reviewPullRequestGroup.pullRequestEntries {
		if existingPullRequestEntry.pullRequestWrapper.Id == pullRequestWrapper.Id {
			pullRequestEntry = existingPullRequestEntry
			pullRequestEntry.pullRequestWrapper = pullRequestWrapper
			pullRequestEntryIndex = index
		}
	}

	// If not in the list, safe to ignore
	// If in the list, update the list entry
	// If in the list && the index == currently selected index, update the details view too

	if pullRequestEntry != nil {
		ghui.updatePullRequestEntry(pullRequestEntry)

		selectedRow,_ := ghui.reviewPullRequestGroup.pullRequestTable.GetSelection()
		if  selectedRow == pullRequestEntryIndex {
			ghui.updatePullRequestDetails(pullRequestWrapper)
		}
	}

}

func (ghui *UI) handleStatusUpdate(status string) {
	go ghui.app.QueueUpdateDraw(func() {
		ghui.status.SetText(" " + status)
	})
}

func (ghui *UI) pollEvents() {
	events := ghui.ghMon.events
	for {
		event := <-events
		switch event.eventType {
		case PullRequestDeleted:
			go ghui.app.QueueUpdateDraw(func(){
				ghui.handlePullRequestDeleted(event.payload.(*PullRequestWrapper))
			})
		case PullRequestUpdated:
			go ghui.app.QueueUpdateDraw(func() {
				ghui.handlePullRequestUpdated(event.payload.(*PullRequestWrapper))
			})
		case PullRequestRefreshFinished,PullRequestsUpdates:

			go ghui.app.QueueUpdateDraw(func() {
				pullRequestsUpdatesEvent := event.payload.(PullRequestsUpdatesEvent)
				ghui.handlePullRequestsUpdates(pullRequestsUpdatesEvent.pullRequestWrappers)
			})

		case Status:
			go ghui.handleStatusUpdate(event.payload.(string))
		}
	}
}

func (ghui *UI) EventLoop() {

	go ghui.pollEvents()

	ghui.app.SetRoot(ghui.grid, true)
	ghui.app.SetFocus(ghui.reviewerTable)
	ghui.app.EnableMouse(false)

	if err := ghui.app.Run(); err != nil {
		panic(err)
	}

}