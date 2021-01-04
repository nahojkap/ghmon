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
	myPullRequestGroup 		*PullRequestGroup


	pullRequestGroups []*PullRequestGroup
	currentFocusedPullRequestGroup int
}

func NewGHMonUI(ghm *GHMon) *UI {

	myReviewPullRequestTable := tview.NewTable().SetBorders(false)
	// myReviewPullRequestTable.SetTitle("Active Pull Request(s)")
	reviewPullRequestTable := tview.NewTable().SetBorders(false)
	// reviewPullRequestTable.SetTitle("Active Pull Request(s)")

	reviewerTable := tview.NewTable()
	pullRequestDetails := tview.NewTable()
	pullRequestBody := tview.NewTextView()

	status := tview.NewTextView()
	status.SetTextAlign(tview.AlignLeft)
	status.SetText("")

	monitoring := tview.NewTextView()
	monitoring.SetTextAlign(tview.AlignLeft)
	monitoring.SetText("Your Pull Requests")

	grid := tview.NewGrid().
		SetRows(5, 0, 0, 0, 1).
		SetColumns(-2,-1).
		SetBorders(true)

	grid.AddItem(status, 4, 0, 1, 2, 0, 0, false)
	grid.AddItem(monitoring, 0, 0, 1, 1, 0, 0, false)

	// Layout for screens narrower than 100 cells (menu and side bar are hidden).
	grid.AddItem(myReviewPullRequestTable, 1, 0, 2, 1, 0, 0, false)
	grid.AddItem(reviewPullRequestTable, 2, 0, 2, 1, 0, 0, false)

	grid.AddItem(pullRequestDetails, 0, 1, 1, 1, 0, 0, false)
	grid.AddItem(reviewerTable, 1, 1, 1, 1, 0, 0, false)
	grid.AddItem(pullRequestBody, 2, 1, 2, 1, 0, 0, false)

	// Layout for screens wider than 100 cells.
	grid.AddItem(myReviewPullRequestTable, 1, 0, 2, 1, 0, 100, false)
	grid.AddItem(reviewPullRequestTable, 2, 0, 2, 1, 0, 100, false)

	grid.AddItem(pullRequestDetails, 0, 1, 1, 1, 0, 100, false)
	grid.AddItem(reviewerTable, 1, 1, 1, 1, 0, 100, false)
	grid.AddItem(pullRequestBody, 2, 1, 2, 1, 0, 100, false)

	app := tview.NewApplication()
	grid.SetBackgroundColor(tcell.ColorBlack)

	ghui := UI{
		ghMon: ghm,app: app, grid: grid, reviewerTable: reviewerTable,
		status: status, pullRequestDetails: pullRequestDetails,
		pullRequestBody:  pullRequestBody,
	}

	ghui.reviewPullRequestGroup = &PullRequestGroup{pullRequestType: Reviewer, pullRequestTable: reviewPullRequestTable}
	ghui.myPullRequestGroup = &PullRequestGroup{pullRequestType: Own, pullRequestTable: myReviewPullRequestTable}
	ghui.pullRequestGroups = make([]*PullRequestGroup, 2)
	ghui.currentFocusedPullRequestGroup = 0
	ghui.pullRequestGroups[0] = ghui.myPullRequestGroup
	ghui.pullRequestGroups[1] = ghui.reviewPullRequestGroup

	app.SetFocus(myReviewPullRequestTable)
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// We navigate in between focuses here
		if event.Key() == tcell.KeyTab {
			ghui.NewFocus()
			return nil
		}
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

	myReviewPullRequestTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
	}).SetSelectedFunc(func(row int, column int) {
		ghui.openBrowser(row)
	})
	myReviewPullRequestTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return event
	})
	myReviewPullRequestTable.SetSelectable(true,false)
	myReviewPullRequestTable.SetSelectionChangedFunc(func(row, column int) {
		if row >= 0 {
			ghui.updateSelectedPullRequestWrapper(ghui.pullRequestGroups[0], row)
			go app.QueueUpdateDraw(func() {
				ghui.UpdatePullRequestDetails(ghui.pullRequestGroups[0])
			})
		}
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
		if row >= 0 {
			ghui.updateSelectedPullRequestWrapper(ghui.pullRequestGroups[1], row)
			go app.QueueUpdateDraw(func() {
				ghui.UpdatePullRequestDetails(ghui.pullRequestGroups[1])
			})
		}
	})

	return &ghui
}

func (ghui *UI) updateSelectedPullRequestWrapper(pullRequestGroup *PullRequestGroup, index int) {
	if len(pullRequestGroup.pullRequestWrappers) > 0 && index >= 0 {
		pullRequestGroup.currentlySelectedPullRequestWrapperIndex = index
		pullRequestGroup.currentlySelectedPullRequestWrapper = pullRequestGroup.pullRequestWrappers[index]
	}
}

func (ghui *UI) setUnfocused(pullRequestGroup *PullRequestGroup) {
	//pullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(8)
	//pullRequestGroup.pullRequestList.TitleStyle = ui.NewStyle(8, ui.ColorClear, ui.ModifierClear)
	//
	//color := ui.ColorClear
	//if len(pullRequestGroup.pullRequestWrappers) > 0 {
	//	color = ghui.getOverallPullRequestColor(pullRequestGroup.pullRequestWrappers[pullRequestGroup.pullRequestList.SelectedRow])
	//}
	//pullRequestGroup.pullRequestList.SelectedRowStyle = ui.NewStyle(color, ColorListNotSelectedBackground)
}

func (ghui *UI) setFocused(pullRequestGroup *PullRequestGroup) {
	//pullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)
	//pullRequestGroup.pullRequestList.TitleStyle = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)
	//color := ui.ColorClear
	//if len(pullRequestGroup.pullRequestWrappers) > 0 {
	//	color = ghui.getOverallPullRequestColor(pullRequestGroup.pullRequestWrappers[pullRequestGroup.pullRequestList.SelectedRow])
	//}
	//pullRequestGroup.pullRequestList.SelectedRowStyle = ui.NewStyle(color, ColorListSelectedBackground, ui.ModifierBold)

}

//func (ghui *UI) UpdateAccordingToFocus(pullRequestGroup *PullRequestGroup) {
//	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
//	if currentlySelectedPullRequestGroup == pullRequestGroup {
//		ghui.setFocused(pullRequestGroup)
//	} else {
//		ghui.setUnfocused(pullRequestGroup)
//	}
//
//}


func (ghui *UI) NewFocus() {

	ghui.currentFocusedPullRequestGroup++
	nextSelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]

	ghui.app.SetFocus(nextSelectedPullRequestGroup.pullRequestTable)

	go ghui.app.QueueUpdateDraw(func() {
		go ghui.UpdatePullRequestDetails(nextSelectedPullRequestGroup)
	})
}

func (ghui *UI) RefreshPullRequests() {
	go ghui.ghMon.RetrievePullRequests()
	go ghui.ghMon.RetrieveMyPullRequests()
}

func (ghui *UI) getPullRequestReviewColorString(pullRequestView *PullRequestReview) (color string) {
	switch pullRequestView.Status {
	case "APPROVED":
		color = "green"
	case "COMMENTED":
		color = "orange"
	case "CHANGES_REQUESTED":
		color = "red"
	case "PENDING":
		color = "yellow"
	case "REQUESTED":
		color = "white"
	default:
		color = "white"
	}
	return
}

func (ghui *UI) getPullRequestReviewColor(pullRequestView *PullRequestReview) (color tcell.Color) {
	switch pullRequestView.Status {
	case "APPROVED":
		color = tcell.ColorGreen
	case "COMMENTED":
		color = tcell.ColorOrange
	case "CHANGES_REQUESTED":
		color = tcell.ColorDarkRed
	case "PENDING":
		color = tcell.ColorYellow
	case "REQUESTED":
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
		s := string(r)
		switch r {
		case '[' :
			if !inSquareBracket {
				inSquareBracket = true
			}
			b.WriteString(s)
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

	ghui.pullRequestDetails.SetCell(0,0,tview.NewTableCell(" [::b]Title:"))
	ghui.pullRequestDetails.SetCell(0,1,tview.NewTableCell(fmt.Sprintf("[::b]%s",pullRequestWrapper.PullRequest.Title)))
	ghui.pullRequestDetails.SetCell(1,0,tview.NewTableCell(" [::b]Creator: "))
	ghui.pullRequestDetails.SetCell(1,1,tview.NewTableCell(fmt.Sprintf("[#00ff1a]%s",pullRequestWrapper.PullRequest.Creator.Username)))
	ghui.pullRequestDetails.SetCell(2,0,tview.NewTableCell(" [::b]Created: "))
	ghui.pullRequestDetails.SetCell(2,1,tview.NewTableCell(pullRequestWrapper.PullRequest.CreatedAt.String()))
	ghui.pullRequestDetails.SetCell(3,0,tview.NewTableCell(" [::b]Updated: "))
	ghui.pullRequestDetails.SetCell(3,1,tview.NewTableCell(pullRequestWrapper.PullRequest.UpdatedAt.String()))
	ghui.pullRequestDetails.SetCell(4,0,tview.NewTableCell(" [::b]First Seen: "))
	ghui.pullRequestDetails.SetCell(4,1,tview.NewTableCell(pullRequestWrapper.FirstSeen.String()))

	ghui.pullRequestBody.SetText(fmt.Sprintf("%s", pullRequestWrapper.PullRequest.Body))

	ghui.reviewerTable.Clear()

	for i, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByPriority {
		status := fmt.Sprintf(" [%s][%s[]", ghui.getPullRequestReviewColorString(pullRequestReviews[0]), pullRequestReviews[0].Status)
		ghui.reviewerTable.SetCell(i, 1, tview.NewTableCell(status))
		ghui.reviewerTable.SetCell(i, 2, tview.NewTableCell(pullRequestReviews[0].User.Username))
	}
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


func (ghui *UI)handlePullRequestsUpdates(loadedPullRequestWrappers []*PullRequestWrapper) {

	if len(loadedPullRequestWrappers) <= 0 {
		return
	}

	// FIXME: Somewhat clunky way of figuring out the type of review being submitted
	pullRequestType := Reviewer
	for key := range loadedPullRequestWrappers {
		pullRequestType = loadedPullRequestWrappers[key].PullRequestType
		break
	}

	var pullRequestGroup *PullRequestGroup
	if pullRequestType == Reviewer {
		pullRequestGroup = ghui.reviewPullRequestGroup
	} else {
		pullRequestGroup = ghui.myPullRequestGroup
	}

	longestRepoName := 0
	for _,pullRequestWrapper := range loadedPullRequestWrappers {
		if len(pullRequestWrapper.PullRequest.Repo.Name) > longestRepoName {
			longestRepoName = len(pullRequestWrapper.PullRequest.Repo.Name)
		}
	}

	pullRequestTable := pullRequestGroup.pullRequestTable
	currentlySelectedPullRequest := pullRequestGroup.currentlySelectedPullRequestWrapper
	newSelectedRow := 0

	pullRequestGroup.pullRequestTable.Clear()

	for counter, pullRequestWrapper := range loadedPullRequestWrappers {

		pullRequestItem := pullRequestWrapper.PullRequest

		seen := " "
		if !pullRequestWrapper.Seen {
			seen = "*"
		}

		if currentlySelectedPullRequest != nil && pullRequestItem.Id == currentlySelectedPullRequest.Id {
			newSelectedRow=counter
		}

		pullRequestTable.SetCell(counter,0, tview.NewTableCell(seen))
		pullRequestTable.SetCell(counter,1, tview.NewTableCell(pullRequestItem.Title))
		pullRequestTable.SetCell(counter,2, tview.NewTableCell(fmt.Sprintf("[%s]",pullRequestItem.Repo.Name)))
		pullRequestTable.SetCell(counter,3, tview.NewTableCell(pullRequestItem.UpdatedAt.String()))

		counter++
	}

	pullRequestGroup.pullRequestWrappers = loadedPullRequestWrappers
	ghui.updateSelectedPullRequestWrapper(pullRequestGroup, newSelectedRow)

	go ghui.app.QueueUpdateDraw(func() {
		ghui.UpdatePullRequestDetails(pullRequestGroup)
	})

}

func (ghui *UI) handlePullRequestUpdated(loadedPullRequest *PullRequestWrapper) {

	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	currentlySelectedPullRequestWrapper := currentlySelectedPullRequestGroup.currentlySelectedPullRequestWrapper
	if currentlySelectedPullRequestWrapper != nil && currentlySelectedPullRequestWrapper.Id == loadedPullRequest.Id {
		ghui.UpdatePullRequestDetails(currentlySelectedPullRequestGroup)
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
			})
		case PullRequestsUpdates:

			go ghui.app.QueueUpdateDraw(func() {
				ghui.handlePullRequestsUpdates(event.payload.(PullRequestsUpdatesEvent).pullRequestWrappers)
			})

		case Status:
			ghui.handleStatusUpdate(event.payload.(string))
		}
	}
}



func (ghui *UI) EventLoop() {

	//pullRequestsTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
	//	if key == tcell.KeyEscape {
	//		app.Stop()
	//	}
	//	if key == tcell.KeyEnter {
	//		pullRequestsTable.SetSelectable(true, false)
	//	}
	//}).SetSelectedFunc(func(row int, column int) {
	//	pullRequestsTable.GetCell(row, column).SetTextColor(tcell.ColorRed)
	//	pullRequestsTable.SetSelectable(false, false)
	//})
	//
	//myPullRequestsTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
	//	if key == tcell.KeyEscape {
	//		app.Stop()
	//	}
	//	if key == tcell.KeyEnter {
	//		myPullRequestsTable.SetSelectable(true, false)
	//	}
	//}).SetSelectedFunc(func(row int, column int) {
	//	myPullRequestsTable.GetCell(row, column).SetTextColor(tcell.ColorRed)
	//	myPullRequestsTable.SetSelectable(false, false)
	//})

	go ghui.pollEvents()

	if err := ghui.app.SetRoot(ghui.grid, true).EnableMouse(true).Run(); err != nil {
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
