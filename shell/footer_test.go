package shell

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

func TestSplitCommandNameOnly(t *testing.T) {
	name, scope := splitCommand("workerpools")
	if name != "workerpools" || scope != "" {
		t.Fatalf("unexpected split: name=%q scope=%q", name, scope)
	}
}

func TestSplitCommandNameAndScope(t *testing.T) {
	name, scope := splitCommand("workers gcp/linux-b-gpu")
	if name != "workers" || scope != "gcp/linux-b-gpu" {
		t.Fatalf("unexpected split: name=%q scope=%q", name, scope)
	}
}

func TestSplitCommandCollapsesExtraWhitespace(t *testing.T) {
	name, scope := splitCommand("  workers   gcp/linux-b-gpu  ")
	if name != "workers" || scope != "gcp/linux-b-gpu" {
		t.Fatalf("unexpected split: name=%q scope=%q", name, scope)
	}
}

func TestSplitCommandEmptyInput(t *testing.T) {
	name, scope := splitCommand("")
	if name != "" || scope != "" {
		t.Fatalf("unexpected split: name=%q scope=%q", name, scope)
	}
}

func TestSplitCommandWhitespaceOnlyInput(t *testing.T) {
	name, scope := splitCommand("   ")
	if name != "" || scope != "" {
		t.Fatalf("unexpected split: name=%q scope=%q", name, scope)
	}
}

func TestRenderHeaderHintsShowsTabHintWhenFaceted(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentFaceted = fakeFacetedColumn{column: 0, options: []string{"aws"}}

	s.renderHeaderHints()

	text := s.headerHint.GetText(false)
	if !strings.Contains(text, "Tab") || !strings.Contains(text, "Shift+Tab") {
		t.Fatalf("expected Tab/Shift+Tab hint, got %q", text)
	}
}

func TestRenderHeaderHintsOmitsTabHintWhenNotFaceted(t *testing.T) {
	s := New(resource.NewRegistry())

	s.renderHeaderHints()

	text := s.headerHint.GetText(false)
	if strings.Contains(text, "Shift+Tab") {
		t.Fatalf("expected no Tab hint for a non-faceted resource, got %q", text)
	}
}

func TestHeaderRowsNeededNeverShrinksBelowThree(t *testing.T) {
	for _, hintCount := range []int{0, 1, 5, 9} {
		if got := headerRowsNeeded(hintCount); got != 3 {
			t.Errorf("headerRowsNeeded(%d) = %d, want 3", hintCount, got)
		}
	}
}

func TestHeaderRowsNeededGrowsWithHintCount(t *testing.T) {
	cases := []struct {
		hintCount int
		want      int
	}{
		{10, 4}, // one hint past the 3x3 grid needs a 4th row
		{12, 4},
		{13, 5},
	}
	for _, c := range cases {
		if got := headerRowsNeeded(c.hintCount); got != c.want {
			t.Errorf("headerRowsNeeded(%d) = %d, want %d", c.hintCount, got, c.want)
		}
	}
}

func TestRenderHeaderHintsShowsFilterOnListView(t *testing.T) {
	s := New(resource.NewRegistry())
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})

	s.renderHeaderHints()

	text := s.headerHint.GetText(false)
	if !strings.Contains(text, "filter") {
		t.Fatalf("expected filter hint on a list view, got %q", text)
	}
}

func TestRenderHeaderHintsOnDetailView(t *testing.T) {
	s := New(resource.NewRegistry())
	s.stack.Push(View{ResourceName: "workerpools", Kind: DetailKind, SelectedID: "gcp/pool-a"})

	s.renderHeaderHints()

	text := s.headerHint.GetText(false)
	if !strings.Contains(text, "/[white] filter") {
		t.Fatalf("expected a filter hint on a detail view, got %q", text)
	}
	if strings.Contains(text, "truncate") || strings.Contains(text, "load all") {
		t.Fatalf("expected no list-only hints on a detail view, got %q", text)
	}
}

func TestRenderHeaderHintsRendersDetailActionsWithoutBrackets(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentDetailActions = []resource.DetailAction{
		{Key: 'w', Label: "workers"},
	}

	s.renderHeaderHints()

	text := s.headerHint.GetText(false)
	if !strings.Contains(text, "[yellow]w[white] workers") {
		t.Fatalf("expected bracket-free detail action hint, got %q", text)
	}
	if strings.Contains(text, "<w>") {
		t.Fatalf("expected no angle-bracket detail action hint, got %q", text)
	}
}

func TestRenderBreadcrumbsEmptyStack(t *testing.T) {
	s := New(resource.NewRegistry())

	s.renderBreadcrumbs()

	text := strings.TrimSpace(s.footerBreadcrumb.GetText(false))
	if text != "" {
		t.Fatalf("expected empty breadcrumb for empty stack, got %q", text)
	}
}

func TestRenderBreadcrumbsListView(t *testing.T) {
	s := New(resource.NewRegistry())
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})

	s.renderBreadcrumbs()

	text := strings.TrimSpace(s.footerBreadcrumb.GetText(false))
	if text != "workerpools" {
		t.Fatalf("unexpected breadcrumb: %q", text)
	}
}

func TestRenderBreadcrumbsScopedListView(t *testing.T) {
	s := New(resource.NewRegistry())
	s.stack.Push(View{ResourceName: "workers", Kind: ListKind, Scope: "gecko-3/b-linux"})

	s.renderBreadcrumbs()

	text := strings.TrimSpace(s.footerBreadcrumb.GetText(false))
	if text != "workers (gecko-3/b-linux)" {
		t.Fatalf("unexpected breadcrumb: %q", text)
	}
}

func TestRenderBreadcrumbsMultiLevelStack(t *testing.T) {
	s := New(resource.NewRegistry())
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})
	s.stack.Push(View{ResourceName: "workers", Kind: ListKind, Scope: "gecko-3/b-linux"})
	s.stack.Push(View{ResourceName: "worker", Kind: DetailKind, SelectedID: "worker-1"})

	s.renderBreadcrumbs()

	text := strings.TrimSpace(s.footerBreadcrumb.GetText(false))
	want := "workerpools › workers (gecko-3/b-linux) › worker:worker-1"
	if text != want {
		t.Fatalf("unexpected breadcrumb: got %q, want %q", text, want)
	}
}

func TestHandleFooterInputDonePromptWithIDNavigatesAndCloses(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeDirectLookupResource{fakeResource: fakeResource{name: "task"}, label: "task id"}
	s.footerMode = footerPrompt
	s.pendingLookupCommit = func(id string) { s.switchToDetail(res, id) }
	s.footerInput.SetText("task-1")

	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerIdle {
		t.Fatalf("expected footer to close, got mode %v", s.footerMode)
	}
	if s.pendingLookupCommit != nil {
		t.Fatalf("expected pendingLookupCommit cleared, got non-nil")
	}
	top, ok := s.stack.Top()
	if !ok || top.Kind != DetailKind || top.SelectedID != "task-1" || top.ResourceName != "task" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestHandleFooterInputDonePromptWithEmptyTextStaysOpen(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerPrompt
	s.pendingLookupCommit = func(id string) { t.Fatalf("commit should not be called for empty text") }
	s.footerInput.SetText("   ")

	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerPrompt {
		t.Fatalf("expected prompt to stay open, got mode %v", s.footerMode)
	}
	if s.pendingLookupCommit == nil {
		t.Fatalf("expected pendingLookupCommit to remain set")
	}
}

func TestHandleFooterInputDonePromptEscapeCancels(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerPrompt
	s.pendingLookupCommit = func(id string) {}

	s.handleFooterInputDone(tcell.KeyEscape)

	if s.footerMode != footerIdle {
		t.Fatalf("expected footer to close, got mode %v", s.footerMode)
	}
	if s.pendingLookupCommit != nil {
		t.Fatalf("expected pendingLookupCommit cleared, got non-nil")
	}
}

func TestHandleFooterInputDoneFilterEnterPersistsPerResource(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workerpools"
	s.footerMode = footerFilter
	s.footerInput.SetText("proj-task")

	s.handleFooterInputDone(tcell.KeyEnter)

	if s.filterQuery != "proj-task" {
		t.Fatalf("expected filterQuery %q, got %q", "proj-task", s.filterQuery)
	}
	if got := s.filterByResource["workerpools"]; got != "proj-task" {
		t.Fatalf("expected filterByResource[workerpools] = %q, got %q", "proj-task", got)
	}
}

func TestHandleFooterInputDoneFilterEscapeClearsPersistedValue(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workerpools"
	s.filterByResource["workerpools"] = "proj-task"
	s.filterQuery = "proj-task"
	s.footerMode = footerFilter

	s.handleFooterInputDone(tcell.KeyEscape)

	if s.filterQuery != "" {
		t.Fatalf("expected filterQuery cleared, got %q", s.filterQuery)
	}
	if got := s.filterByResource["workerpools"]; got != "" {
		t.Fatalf("expected filterByResource[workerpools] cleared, got %q", got)
	}
}

func TestOpenFilterPrefillsFromDetailQueryOnDetailPage(t *testing.T) {
	s := New(resource.NewRegistry())
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	s.detail.SetFilterQuery("beta")
	s.content.SwitchToPage(pageDetail)
	s.filterQuery = "should-not-be-used"

	s.openFilter()

	if got := s.footerInput.GetText(); got != "beta" {
		t.Fatalf("expected footer prefilled with the detail's own query %q, got %q", "beta", got)
	}
}

func TestHandleFooterInputChangedAppliesDetailFilterLive(t *testing.T) {
	s := New(resource.NewRegistry())
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\ngamma\n"})
	s.content.SwitchToPage(pageDetail)
	s.footerMode = footerFilter

	s.handleFooterInputChanged("beta")

	if got := s.detail.GetText(true); got != "beta" {
		t.Fatalf("expected the detail body filtered live, got %q", got)
	}
	if s.filterQuery != "" {
		t.Fatalf("expected the list's own filterQuery untouched by a detail-page filter, got %q", s.filterQuery)
	}
}

func TestHandleFooterInputDoneFilterEnterOnDetailDoesNotPersistPerResource(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workerpools"
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	s.content.SwitchToPage(pageDetail)
	s.footerMode = footerFilter
	s.detail.SetFilterQuery("beta")
	s.footerInput.SetText("beta")

	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerIdle {
		t.Fatalf("expected footer to close, got mode %v", s.footerMode)
	}
	if got := s.filterByResource["workerpools"]; got != "" {
		t.Fatalf("expected a detail-page filter to never touch the list's per-resource memory, got %q", got)
	}
	if s.detail.FilterQuery() != "beta" {
		t.Fatalf("expected the detail's own filter to survive closing the footer, got %q", s.detail.FilterQuery())
	}
}

func TestHandleFooterInputDoneFilterEscapeOnDetailClearsOnlyDetailFilter(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workerpools"
	s.filterByResource["workerpools"] = "proj-task"
	s.filterQuery = "proj-task"
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	s.content.SwitchToPage(pageDetail)
	s.detail.SetFilterQuery("beta")
	s.footerMode = footerFilter

	s.handleFooterInputDone(tcell.KeyEscape)

	if s.detail.FilterQuery() != "" {
		t.Fatalf("expected the detail's filter cleared, got %q", s.detail.FilterQuery())
	}
	if s.filterQuery != "proj-task" || s.filterByResource["workerpools"] != "proj-task" {
		t.Fatalf("expected the list's own filter state left untouched, got filterQuery=%q filterByResource=%q",
			s.filterQuery, s.filterByResource["workerpools"])
	}
}

func TestGlobalInputCaptureSlashOpensFilterOnDetailPage(t *testing.T) {
	s := newTestShellForSort()
	s.content.SwitchToPage(pageDetail)

	event := tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone)
	if got := s.globalInputCapture(event); got != nil {
		t.Fatalf("expected '/' to be swallowed, got %#v", got)
	}

	if s.footerMode != footerFilter {
		t.Fatalf("expected '/' to open the filter on a detail page, got mode %v", s.footerMode)
	}
}

func TestUpdateBorderColorTintsOnActiveDetailFilter(t *testing.T) {
	s := New(resource.NewRegistry())
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	s.content.SwitchToPage(pageDetail)
	s.detail.SetFilterQuery("beta")

	s.updateBorderColor()

	if got := s.content.GetBorderColor(); got != tcell.ColorBlue {
		t.Fatalf("expected blue border while a detail filter is active, got %v", got)
	}
}

func TestCycleFooterHistoryUpRecallsMostRecentEntry(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerHistory[historyKeyCommand] = []string{"workers gcp", "workerpools"}
	s.footerHistoryIndex = len(s.footerHistory[historyKeyCommand])
	s.footerInput.SetText("")

	s.cycleFooterHistory(-1)

	if got := s.footerInput.GetText(); got != "workerpools" {
		t.Fatalf("expected most recent history entry %q, got %q", "workerpools", got)
	}
}

func TestCycleFooterHistoryUpTwiceRecallsOlderEntry(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerHistory[historyKeyCommand] = []string{"workers gcp", "workerpools"}
	s.footerHistoryIndex = len(s.footerHistory[historyKeyCommand])
	s.footerInput.SetText("")

	s.cycleFooterHistory(-1)
	s.cycleFooterHistory(-1)

	if got := s.footerInput.GetText(); got != "workers gcp" {
		t.Fatalf("expected older history entry %q, got %q", "workers gcp", got)
	}
}

func TestCycleFooterHistoryUpAtOldestEntryStaysThere(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerHistory[historyKeyCommand] = []string{"workers gcp", "workerpools"}
	s.footerHistoryIndex = len(s.footerHistory[historyKeyCommand])

	s.cycleFooterHistory(-1)
	s.cycleFooterHistory(-1)
	s.cycleFooterHistory(-1) // one past the oldest entry — should stay put

	if got := s.footerInput.GetText(); got != "workers gcp" {
		t.Fatalf("expected to stay on the oldest entry %q, got %q", "workers gcp", got)
	}
}

func TestCycleFooterHistoryDownRestoresDraftAfterUp(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerHistory[historyKeyCommand] = []string{"workerpools"}
	s.footerHistoryIndex = len(s.footerHistory[historyKeyCommand])
	s.footerInput.SetText("wor")

	s.cycleFooterHistory(-1)
	if got := s.footerInput.GetText(); got != "workerpools" {
		t.Fatalf("expected history entry recalled, got %q", got)
	}

	s.cycleFooterHistory(1)
	if got := s.footerInput.GetText(); got != "wor" {
		t.Fatalf("expected draft restored after cycling past newest entry, got %q", got)
	}
}

func TestCycleFooterHistoryDownWithNoHistoryIsNoop(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerInput.SetText("wor")

	s.cycleFooterHistory(1)

	if got := s.footerInput.GetText(); got != "wor" {
		t.Fatalf("expected text untouched, got %q", got)
	}
}

func TestCycleFooterHistoryScopedPerMode(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerHistory[historyKeyCommand] = []string{"workerpools"}
	s.footerHistory[historyKeyFilter] = []string{"proj-task"}

	s.footerMode = footerFilter
	s.footerHistoryKey = historyKeyFilter
	s.footerHistoryIndex = len(s.footerHistory[historyKeyFilter])
	s.footerInput.SetText("")
	s.cycleFooterHistory(-1)

	if got := s.footerInput.GetText(); got != "proj-task" {
		t.Fatalf("expected filter history entry, got %q", got)
	}
}

func TestRecordFooterHistoryAppendsNonEmptyEntry(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand

	s.recordFooterHistory(historyKeyCommand, "workerpools")

	if got := s.footerHistory[historyKeyCommand]; len(got) != 1 || got[0] != "workerpools" {
		t.Fatalf("expected history to contain %q, got %v", "workerpools", got)
	}
}

func TestRecordFooterHistorySkipsConsecutiveDuplicate(t *testing.T) {
	s := New(resource.NewRegistry())

	s.recordFooterHistory(historyKeyCommand, "workerpools")
	s.recordFooterHistory(historyKeyCommand, "workerpools")

	if got := s.footerHistory[historyKeyCommand]; len(got) != 1 {
		t.Fatalf("expected duplicate entry to be skipped, got %v", got)
	}
}

func TestRecordFooterHistorySkipsEmptyEntry(t *testing.T) {
	s := New(resource.NewRegistry())

	s.recordFooterHistory(historyKeyCommand, "   ")

	if got := s.footerHistory[historyKeyCommand]; len(got) != 0 {
		t.Fatalf("expected no history entry recorded, got %v", got)
	}
}

func TestHandleFooterInputDoneRecordsCommandHistory(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerInput.SetText("workerpools")

	s.handleFooterInputDone(tcell.KeyEnter)

	if got := s.footerHistory[historyKeyCommand]; len(got) != 1 || got[0] != "workerpools" {
		t.Fatalf("expected command recorded to history, got %v", got)
	}
}

func TestQuitCommandStopsApp(t *testing.T) {
	// The command bar's ':' is the trigger key, not part of the input, so a
	// user typing ':quit' produces the input "quit" here.
	for _, cmd := range []string{"quit", "q", "QUIT"} {
		t.Run(cmd, func(t *testing.T) {
			s := New(resource.NewRegistry())
			stopped := false
			s.onStopForTest = func() { stopped = true }
			s.footerMode = footerCommand
			s.footerHistoryKey = historyKeyCommand
			s.footerInput.SetText(cmd)

			s.handleFooterInputDone(tcell.KeyEnter)

			if !stopped {
				t.Fatalf("expected %q to trigger Stop()", cmd)
			}
		})
	}
}

func TestNonQuitCommandDoesNotStopApp(t *testing.T) {
	s := New(resource.NewRegistry())
	s.onStopForTest = func() { t.Fatal("Stop() should not be called for a non-quit command") }
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerInput.SetText("workerpools")

	s.handleFooterInputDone(tcell.KeyEnter)
}

func TestOpenCommandBarResetsHistoryNavToNewest(t *testing.T) {
	s := New(resource.NewRegistry())
	s.footerHistory[historyKeyCommand] = []string{"workerpools"}
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.footerHistoryIndex = 0 // simulate having browsed history in a prior open

	s.openCommandBar()

	if s.footerHistoryIndex != len(s.footerHistory[historyKeyCommand]) {
		t.Fatalf("expected history nav reset to newest (%d), got %d", len(s.footerHistory[historyKeyCommand]), s.footerHistoryIndex)
	}
}

func TestGlobalInputCaptureUpArrowCyclesHistoryWhenFooterActive(t *testing.T) {
	s := newTestShellForSort()
	s.openCommandBar()
	s.footerHistory[historyKeyCommand] = []string{"workerpools"}
	s.footerHistoryIndex = len(s.footerHistory[historyKeyCommand])

	event := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	if got := s.globalInputCapture(event); got != nil {
		t.Fatalf("expected Up arrow to be swallowed, got %#v", got)
	}

	if got := s.footerInput.GetText(); got != "workerpools" {
		t.Fatalf("expected history entry recalled via global input capture, got %q", got)
	}
}

func TestOpenIDPromptScopesHistoryToIDLookupKey(t *testing.T) {
	s := New(resource.NewRegistry())

	s.openIDPrompt("task id", historyKeyIDPrompt, func(id string) {})

	if s.footerHistoryKey != historyKeyIDPrompt {
		t.Fatalf("expected footerHistoryKey %q, got %q", historyKeyIDPrompt, s.footerHistoryKey)
	}
}

func TestFooterHistorySeparatesSavePathFromIDLookup(t *testing.T) {
	s := New(resource.NewRegistry())

	s.openIDPrompt("task id", historyKeyIDPrompt, func(id string) {})
	s.footerInput.SetText("aBcDeF123")
	s.handleFooterInputDone(tcell.KeyEnter)

	s.openIDPrompt("save as", historyKeySavePath, func(path string) {})
	s.footerInput.SetText("out.log")
	s.handleFooterInputDone(tcell.KeyEnter)

	if got := s.footerHistory[historyKeyIDPrompt]; len(got) != 1 || got[0] != "aBcDeF123" {
		t.Fatalf("expected id-lookup history to contain only the task id, got %v", got)
	}
	if got := s.footerHistory[historyKeySavePath]; len(got) != 1 || got[0] != "out.log" {
		t.Fatalf("expected save-path history to contain only the filename, got %v", got)
	}

	// Reopening the id-lookup prompt must recall the task id, not the
	// filename just saved.
	s.openIDPrompt("task id", historyKeyIDPrompt, func(id string) {})
	s.cycleFooterHistory(-1)
	if got := s.footerInput.GetText(); got != "aBcDeF123" {
		t.Fatalf("expected id-lookup history recall %q, got %q", "aBcDeF123", got)
	}

	// And reopening save-as must recall the filename, not the task id.
	s.openIDPrompt("save as", historyKeySavePath, func(path string) {})
	s.cycleFooterHistory(-1)
	if got := s.footerInput.GetText(); got != "out.log" {
		t.Fatalf("expected save-path history recall %q, got %q", "out.log", got)
	}
}

func TestRefreshDetailTitleAppendsFilterSuffix(t *testing.T) {
	s := New(resource.NewRegistry())
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	s.currentDetailTitle = "task:abc"
	s.detail.SetFilterQuery("beta")

	s.refreshDetailTitle()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: task:abc (beta) ]"; got != want {
		t.Fatalf("unexpected title: got %q, want %q", got, want)
	}
}
