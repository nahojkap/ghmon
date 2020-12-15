	package ghmon

	import (
		"fmt"
		ui "github.com/gizak/termui/v3"
		"github.com/gizak/termui/v3/widgets"
		tview "github.com/rivo/tview"
		"log"
		"os/exec"
		"runtime"
		"sync"
	)

type PullRequestGroup struct {
	pullRequestType     PullRequestType
	pullRequestList     *widgets.List
	pullRequestTable    *tview.Table
	pullRequestWrappers []*PullRequestWrapper
}

type UI struct {
	ghMon                   *GHMon
	uiLock sync.Mutex

	status                  *widgets.Paragraph

	pullRequestDetails      *widgets.Paragraph
	pullRequestBody         *widgets.Paragraph
	reviewerTable            *widgets.Table

	grid 					*tview.Grid
	reviewPullRequestGroup 	*PullRequestGroup
	myPullRequestGroup 		*PullRequestGroup

	pullRequestGroups []*PullRequestGroup
	currentFocusedPullRequestGroup    int
}

const ColorListSelectedBackground ui.Color = 249
const ColorListNotSelectedBackground ui.Color = ui.ColorClear

const ColorLime ui.Color = 10
const ColorLimeStr string = "lime"
const ColorRed3 ui.Color = 124
const ColorRed3Str string = "red3"
const ColorOrange3 ui.Color = 172
const ColorOrange3Str string = "orange3"
const ColorYellow3 ui.Color = 184
const ColorYellow3Str string = "yellow3"

func NewGHMonUI(ghm *GHMon) *UI {

	status := widgets.NewParagraph()
	status.Text = ""
	status.Title = "Status"
	status.BorderStyle = ui.NewStyle(8)

	myRevuewPullRequestTable := tview.NewTable().SetBorders(false)

	myReviewPullRequestList := widgets.NewList()
	myReviewPullRequestList.Title = " My Pull Request(s) "
	myReviewPullRequestList.WrapText = false
	myReviewPullRequestList.TextStyle = ui.NewStyle(ui.ColorWhite)
	myReviewPullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)
	myReviewPullRequestList.TitleStyle = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)

	reviewPullRequestTable := tview.NewTable().SetBorders(false)

	reviewPullRequestList := widgets.NewList()
	reviewPullRequestList.Title = " Active Review Request(s) "
	reviewPullRequestList.WrapText = false
	reviewPullRequestList.TextStyle = ui.NewStyle(ui.ColorWhite)
	reviewPullRequestList.BorderStyle = ui.NewStyle(8)

	reviewerTable := widgets.NewTable()
	reviewerTable.Title = "Reviewers"
	reviewerTable.RowSeparator = false
	reviewerTable.ColumnWidths = make([]int,2)
	reviewerTable.ColumnWidths[0] = 17
	reviewerTable.ColumnWidths[1] = -1
	reviewerTable.PaddingLeft = 1

	ui.StyleParserColorMap[ColorLimeStr] = ColorLime
	ui.StyleParserColorMap[ColorRed3Str] = ColorRed3
	ui.StyleParserColorMap[ColorOrange3Str] = ColorOrange3
	ui.StyleParserColorMap[ColorYellow3Str] = ColorYellow3

	reviewerTable.BorderStyle = ui.NewStyle(8)

	pullRequestDetails := widgets.NewParagraph()
	pullRequestDetails.BorderStyle = ui.NewStyle(8)

	pullRequestBody := widgets.NewParagraph()
	pullRequestBody.Title = "Details"
	pullRequestBody.BorderStyle = ui.NewStyle(8)

	newPrimitive := func(text string) tview.Primitive {
		return tview.NewTextView().
			SetTextAlign(tview.AlignCenter).
			SetText(text)
	}

	//menu := newPrimitive("Menu")
	main := newPrimitive("Main content")
	reviewers := newPrimitive("Reviewers")
	details := newPrimitive("Details")

	grid := tview.NewGrid().
		SetRows(0, 0, 0, 0, 1).
		SetColumns(60, 0).
		SetBorders(true)

	grid.AddItem(newPrimitive("Footer"), 4, 0, 1, 2, 0, 0, false)

	// Layout for screens narrower than 100 cells (menu and side bar are hidden).
	grid.AddItem(myRevuewPullRequestTable, 0, 0, 2, 1, 0, 0, false)
	grid.AddItem(reviewPullRequestTable, 2, 0, 2, 1, 0, 0, false)

	grid.AddItem(main, 0, 1, 1, 1, 0, 0, false)
	grid.AddItem(reviewers, 1, 1, 1, 1, 0, 0, false)
	grid.AddItem(details, 2, 1, 2, 1, 0, 0, false)

	// Layout for screens wider than 100 cells.
	grid.AddItem(myRevuewPullRequestTable, 0, 0, 2, 1, 0, 100, false)
	grid.AddItem(reviewPullRequestTable, 2, 0, 2, 1, 0, 100, false)

	grid.AddItem(main, 0, 1, 1, 1, 0, 100, false)
	grid.AddItem(reviewers, 1, 1, 1, 1, 0, 100, false)
	grid.AddItem(details, 2, 1, 2, 1, 0, 100, false)

	ghui := UI{ghMon: ghm,grid: grid, reviewerTable: reviewerTable, status: status, pullRequestDetails: pullRequestDetails, pullRequestBody:  pullRequestBody}

	ghui.reviewPullRequestGroup = &PullRequestGroup{pullRequestList: reviewPullRequestList, pullRequestType: Reviewer, pullRequestTable: reviewPullRequestTable}
	ghui.myPullRequestGroup = &PullRequestGroup{pullRequestList: myReviewPullRequestList, pullRequestType: Own, pullRequestTable: myRevuewPullRequestTable}
	ghui.pullRequestGroups = make([]*PullRequestGroup, 2)
	ghui.currentFocusedPullRequestGroup = 0
	ghui.pullRequestGroups[0] = ghui.myPullRequestGroup
	ghui.pullRequestGroups[1] = ghui.reviewPullRequestGroup

	//ghui.setFocused(ghui.myPullRequestGroup)
	//ghui.setUnfocused(ghui.reviewPullRequestGroup)

	return &ghui
}

func (ghui *UI) renderAll() {
	ghui.renderPullRequestLists()
	ghui.renderPullRequestDetails()
	ghui.renderStatus()
}

func (ghui *UI) renderStatus() {
	ghui.uiLock.Lock()
	ui.Render(ghui.status)
	ghui.uiLock.Unlock()
}

func (ghui *UI) renderPullRequestDetails() {
	ghui.uiLock.Lock()
	ui.Render(ghui.reviewerTable, ghui.pullRequestDetails, ghui.pullRequestBody)
	ghui.uiLock.Unlock()
}


func (ghui *UI) renderPullRequestLists() {
	ghui.uiLock.Lock()
	ui.Render(ghui.reviewPullRequestGroup.pullRequestList,ghui.myPullRequestGroup.pullRequestList)
	ghui.uiLock.Unlock()
}

func (ghui *UI) Resize(height int, width int) {
	ghui.myPullRequestGroup.pullRequestList.SetRect(0, 0, width / 2, (height-3)/2)
	ghui.reviewPullRequestGroup.pullRequestList.SetRect(0, (height-3)/2, width / 2, height-3)
	ghui.status.SetRect(0, height-3, width, height)
	ghui.pullRequestDetails.SetRect(width / 2,0,width,7)
	ghui.reviewerTable.SetRect(width / 2, 7, width, 15)
	ghui.pullRequestBody.SetRect(width / 2, 15, width, height-3)
}

func (ghui *UI) setUnfocused(pullRequestGroup *PullRequestGroup) {
	pullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(8)
	pullRequestGroup.pullRequestList.TitleStyle = ui.NewStyle(8, ui.ColorClear, ui.ModifierClear)

	color := ui.ColorClear
	if len(pullRequestGroup.pullRequestWrappers) > 0 {
		color = ghui.getOverallPullRequestColor(pullRequestGroup.pullRequestWrappers[pullRequestGroup.pullRequestList.SelectedRow])
	}
	pullRequestGroup.pullRequestList.SelectedRowStyle = ui.NewStyle(color, ColorListNotSelectedBackground)
}

func (ghui *UI) setFocused(pullRequestGroup *PullRequestGroup) {
	pullRequestGroup.pullRequestList.BorderStyle = ui.NewStyle(ui.ColorWhite)
	pullRequestGroup.pullRequestList.TitleStyle = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)
	color := ui.ColorClear
	if len(pullRequestGroup.pullRequestWrappers) > 0 {
		color = ghui.getOverallPullRequestColor(pullRequestGroup.pullRequestWrappers[pullRequestGroup.pullRequestList.SelectedRow])
	}
	pullRequestGroup.pullRequestList.SelectedRowStyle = ui.NewStyle(color, ColorListSelectedBackground, ui.ModifierBold)
}

func (ghui *UI) UpdateAccordingToFocus(pullRequestGroup *PullRequestGroup) {
	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	if currentlySelectedPullRequestGroup == pullRequestGroup {
		ghui.setFocused(pullRequestGroup)
	} else {
		ghui.setUnfocused(pullRequestGroup)
	}

}


func (ghui *UI) NewFocus() {

	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	ghui.currentFocusedPullRequestGroup++
	nextSelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]

	ghui.setUnfocused(currentlySelectedPullRequestGroup)
	ghui.setFocused(nextSelectedPullRequestGroup)

	go ghui.renderPullRequestLists()
	go ghui.UpdatePullRequestDetails(nextSelectedPullRequestGroup.pullRequestWrappers, nextSelectedPullRequestGroup.pullRequestList.SelectedRow)
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

func (ghui *UI) getPullRequestReviewColor(pullRequestView *PullRequestReview) (color ui.Color) {
	switch pullRequestView.Status {
	case "APPROVED":
		color = ui.ColorGreen
	case "COMMENTED":
		color = ColorOrange3
	case "CHANGES_REQUESTED":
		color = ColorRed3
	case "PENDING":
		color = ColorYellow3
	case "REQUESTED":
		color = ui.ColorWhite
	default:
		color = ui.ColorWhite
	}
	return
}

func (ghui *UI) getOverallPullRequestColor(pullRequestWrapper *PullRequestWrapper) ui.Color {

	pullRequestScore := pullRequestWrapper.Score

	if pullRequestScore.ChangesRequested != 0 {
		return ColorRed3
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		return ui.ColorGreen
	}

	if pullRequestScore.Comments > 0 {
		return ColorOrange3
	}

	return ui.ColorWhite

}

func (ghui *UI) getOverallPullRequestColorStr(pullRequestWrapper *PullRequestWrapper) string {

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

func (ghui *UI)UpdatePullRequestDetails(pullRequestList []*PullRequestWrapper, selectedPullRequest int) {

	if len(pullRequestList) == 0 {
		return
	}

	pullRequestWrapper := pullRequestList[uint32(selectedPullRequest)]
	ghui.pullRequestDetails.WrapText = false
	ghui.pullRequestDetails.Text = fmt.Sprintf(" [ID](fg:white): %d\n [Title](fg:white): %s\n [Creator](fg:white): %s\n [Created](fg:white): %s\n [Updated](fg:white): %s", pullRequestWrapper.PullRequest.Id,pullRequestWrapper.PullRequest.Title, pullRequestWrapper.PullRequest.Creator.Username, pullRequestWrapper.PullRequest.CreatedAt, pullRequestWrapper.PullRequest.UpdatedAt)
	ghui.pullRequestBody.Text = fmt.Sprintf("%s", pullRequestWrapper.PullRequest.Body)

	ghui.reviewerTable.Rows = make([][]string, 0)

	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByPriority {
		status := fmt.Sprintf("[%s](fg:%s)", pullRequestReviews[0].Status, ghui.getPullRequestReviewColorString(pullRequestReviews[0]))
		row := make([]string,2)
		row[0] = status
		row[1] = pullRequestReviews[0].User.Username
		ghui.reviewerTable.Rows = append(ghui.reviewerTable.Rows,row)
	}

	go ghui.renderPullRequestDetails()

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

	//currentPullRequests := pullRequestGroup.pullRequestWrappers
	pullRequestList := pullRequestGroup.pullRequestList

	//var currentlySelectedPullRequest *PullRequest
	//if len(currentPullRequests) > 0 {
	//	currentlySelectedPullRequest = currentPullRequests[pullRequestList.SelectedRow].PullRequest
	//}

	newSelectedRow := 0
	// pullRequestList.Rows = make([]string, len(loadedPullRequestWrappers))
	pullRequestGroup.pullRequestTable.Clear()

	for counter, pullRequestWrapper := range loadedPullRequestWrappers {

		pullRequestItem := pullRequestWrapper.PullRequest
		// var listLabel string

		//seen := " "
		if !pullRequestWrapper.Seen {
		//	seen = "*"
		}

		//paddedRepoName := padToLen(pullRequestItem.Repo.Name, longestRepoName)

		//if currentlySelectedPullRequest != nil && pullRequestItem.Id == currentlySelectedPullRequest.Id {
		//	newSelectedRow=counter
		//	pullRequestList.SelectedRowStyle = ui.NewStyle(172,ColorListSelectedBackground)
		//	listLabel = fmt.Sprintf("%s %d %s %s", seen, pullRequestItem.Id, paddedRepoName, pullRequestItem.Title)
		//} else {
		//listLabel = fmt.Sprintf("%s [%d %s %s](fg:%s)", seen, pullRequestItem.Id, paddedRepoName, pullRequestItem.Title, ghui.getOverallPullRequestColorStr(pullRequestWrapper))
		//}

		pullRequestGroup.pullRequestTable.SetCell(counter,0, tview.NewTableCell(string(pullRequestItem.Id)))
		pullRequestGroup.pullRequestTable.SetCell(counter,1, tview.NewTableCell(pullRequestItem.Repo.Name))
		pullRequestGroup.pullRequestTable.SetCell(counter,2, tview.NewTableCell(pullRequestItem.Title))
		pullRequestGroup.pullRequestTable.SetCell(counter,3, tview.NewTableCell(pullRequestItem.UpdatedAt.String()))

//		pullRequestList.Rows[counter] = listLabel
		counter++
	}



	pullRequestGroup.pullRequestWrappers = loadedPullRequestWrappers
	pullRequestList.SelectedRow = newSelectedRow

	ghui.UpdateAccordingToFocus(pullRequestGroup)

	go ghui.renderPullRequestLists()
}

func (ghui *UI) handlePullRequestUpdated(loadedPullRequest *PullRequestWrapper) {
	currentlySelectedPullRequestGroup := ghui.pullRequestGroups[ghui.currentFocusedPullRequestGroup % len(ghui.pullRequestGroups)]
	if len(currentlySelectedPullRequestGroup.pullRequestWrappers) > 0 {
		if currentlySelectedPullRequestGroup.pullRequestType == loadedPullRequest.PullRequestType && loadedPullRequest.Id == currentlySelectedPullRequestGroup.pullRequestWrappers[currentlySelectedPullRequestGroup.pullRequestList.SelectedRow].Id {
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
			ghui.handlePullRequestUpdated(event.payload.(*PullRequestWrapper))
		case PullRequestsUpdates:
			ghui.handlePullRequestsUpdates(event.payload.([]*PullRequestWrapper))
		case Status:
			ghui.handleStatusUpdate(event.payload.(string))
		}
	}
}



func (ghui *UI) EventLoop() {

	app := tview.NewApplication()

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

	if err := app.SetRoot(ghui.grid, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}

	if true {
		return
	}

	err := ui.Init()
	if err != nil {
		panic(err)
	}

	defer ui.Close()

	termWidth, termHeight := ui.TerminalDimensions()

	ghui.Resize(termHeight, termWidth)
	go ghui.renderAll()

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
		go ghui.renderAll()
	}

}
