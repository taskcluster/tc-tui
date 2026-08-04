package shell

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/taskcluster/tc-tui/resource"
)

// fakeActionableResource is a minimal resource.Actionable used to exercise
// the shared action flow without any real Taskcluster calls.
type fakeActionableResource struct {
	fakeResource
	actions       []resource.Action
	describeCalls *int32
}

func (f fakeActionableResource) Describe(id string) (resource.Detail, error) {
	if f.describeCalls != nil {
		atomic.AddInt32(f.describeCalls, 1)
	}
	return resource.Detail{Title: f.name + " " + id}, nil
}

func (f fakeActionableResource) Actions(id string) []resource.Action {
	return f.actions
}

// readOnUI runs fn on the tview event loop and returns its result, so a test
// can inspect Shell/tview state a background action goroutine may be mutating
// without racing it — production code only ever touches that state on the UI
// goroutine too.
func readOnUI[T any](s *Shell, fn func() T) T {
	ch := make(chan T, 1)
	s.app.QueueUpdateDraw(func() { ch <- fn() })
	return <-ch
}

func actionableShell(t *testing.T, actions []resource.Action) (*Shell, *fakeActionableResource) {
	t.Helper()
	var describeCalls int32
	res := &fakeActionableResource{
		fakeResource:  fakeResource{name: "widgets"},
		actions:       actions,
		describeCalls: &describeCalls,
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := New(registry)
	s.stack.Push(View{ResourceName: "widgets", Kind: DetailKind, SelectedID: "w1"})
	return s, res
}

func TestSubmitActionBlocksOnParseError(t *testing.T) {
	var performed int32
	action := resource.Action{
		Key:        'y',
		Label:      "edit widget",
		Prompt:     "Apply this definition?",
		Input:      resource.InputYAML,
		InputLabel: "definition",
		Perform:    func(resource.ActionInput) error { atomic.AddInt32(&performed, 1); return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})

	s.startAction(action)
	s.actionView.setFieldText(s.actionView.inputIndex, "name: [unterminated")
	s.submitAction()

	if atomic.LoadInt32(&performed) != 0 {
		t.Fatalf("Perform must not run when the input fails to parse")
	}
	if !s.actionOpen {
		t.Fatalf("dialog should stay open after a validation error")
	}
	if got := s.actionView.status.GetText(true); !strings.Contains(got, "invalid YAML") {
		t.Fatalf("expected an invalid-YAML status, got %q", got)
	}
}

func TestSubmitActionBlocksOnFieldValidationError(t *testing.T) {
	var performed int32
	action := resource.Action{
		Key:        'p',
		Label:      "set priority",
		Prompt:     "Set a new priority?",
		Input:      resource.InputLine,
		InputLabel: "priority",
		Validate: func(in resource.ActionInput) error {
			if in.Raw != "high" {
				return fmt.Errorf("priority must be one of: low, high")
			}
			return nil
		},
		Perform: func(resource.ActionInput) error { atomic.AddInt32(&performed, 1); return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})

	s.startAction(action)
	s.actionView.setFieldText(s.actionView.inputIndex, "bogus")
	s.submitAction()

	if atomic.LoadInt32(&performed) != 0 {
		t.Fatalf("Perform must not run when field validation fails")
	}
	if got := s.actionView.status.GetText(true); !strings.Contains(got, "priority must be one of") {
		t.Fatalf("expected the validator's message in the status, got %q", got)
	}
}

func TestSubmitActionRequiresConfirmWordForDestructive(t *testing.T) {
	var performed int32
	action := resource.Action{
		Key:         'd',
		Label:       "delete widget",
		Destructive: true,
		ConfirmWord: "DELETE",
		Prompt:      "Delete widget w1?",
		Perform:     func(resource.ActionInput) error { atomic.AddInt32(&performed, 1); return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})

	s.startAction(action)
	s.actionView.setFieldText(s.actionView.confirmIndex, "delete") // wrong case
	s.submitAction()

	if atomic.LoadInt32(&performed) != 0 {
		t.Fatalf("Perform must not run until the confirm word matches exactly")
	}
	if got := s.actionView.status.GetText(true); !strings.Contains(got, "confirm") {
		t.Fatalf("expected a confirm-word prompt in the status, got %q", got)
	}
}

func TestActionSuccessInvalidatesCacheAndClosesDialog(t *testing.T) {
	var performed int32
	action := resource.Action{
		Key:         'd',
		Label:       "delete widget",
		Destructive: true,
		Prompt:      "Delete widget w1?",
		Invalidates: []string{"secrets"},
		Perform:     func(resource.ActionInput) error { atomic.AddInt32(&performed, 1); return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})
	startRunning(t, s)

	// Seed a cache entry the mutation should drop.
	s.cache.set(cacheKey{resource: "secrets"}, cacheEntry{fetchedAt: time.Now()})

	s.app.QueueUpdateDraw(func() { s.startAction(action) })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })

	s.app.QueueUpdateDraw(func() { s.submitAction() })

	waitFor(t, func() bool { return atomic.LoadInt32(&performed) == 1 })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return !s.actionOpen }) })

	cacheHit := readOnUI(s, func() bool {
		_, ok := s.cache.get(cacheKey{resource: "secrets"}, time.Minute)
		return ok
	})
	if cacheHit {
		t.Fatalf("expected the invalidated cache entry to be dropped after success")
	}
	if got := readOnUI(s, func() string { return s.footerBreadcrumb.GetText(true) }); !strings.Contains(got, "done") {
		t.Fatalf("expected a success message in the footer, got %q", got)
	}
}

func TestNonDestructiveActionRefreshesCurrentView(t *testing.T) {
	action := resource.Action{
		Key:        'e',
		Label:      "edit widget",
		Prompt:     "Apply?",
		Input:      resource.InputLine,
		InputLabel: "value",
		Perform:    func(resource.ActionInput) error { return nil },
	}
	s, res := actionableShell(t, []resource.Action{action})
	startRunning(t, s)

	s.app.QueueUpdateDraw(func() {
		s.startAction(action)
		s.actionView.setFieldText(s.actionView.inputIndex, "new-value")
		s.submitAction()
	})

	// A non-destructive success force-refreshes the current detail view, so
	// Describe is re-issued.
	waitFor(t, func() bool { return atomic.LoadInt32(res.describeCalls) >= 1 })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return !s.actionOpen }) })
}

func TestDestructiveActionWithRefreshAfterRefreshesView(t *testing.T) {
	action := resource.Action{
		Key:          'K',
		Label:        "cancel widget",
		Destructive:  true,
		RefreshAfter: true,
		Prompt:       "Cancel widget w1?",
		Perform:      func(resource.ActionInput) error { return nil },
	}
	s, res := actionableShell(t, []resource.Action{action})
	startRunning(t, s)

	s.app.QueueUpdateDraw(func() { s.startAction(action); s.submitAction() })

	// A destructive action that resolves-but-doesn't-delete opts into the
	// post-action refresh, so its detail re-Describes to show the new state.
	waitFor(t, func() bool { return atomic.LoadInt32(res.describeCalls) >= 1 })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return !s.actionOpen }) })
}

func TestDestructiveActionWithoutRefreshAfterSkipsRefresh(t *testing.T) {
	action := resource.Action{
		Key:         'd',
		Label:       "delete widget",
		Destructive: true,
		Prompt:      "Delete widget w1?",
		Perform:     func(resource.ActionInput) error { return nil },
	}
	s, res := actionableShell(t, []resource.Action{action})
	startRunning(t, s)

	s.app.QueueUpdateDraw(func() { s.startAction(action); s.submitAction() })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return !s.actionOpen }) })

	// A plain destructive (delete-style) action must NOT re-Describe — the
	// entity may be gone and a re-fetch would 404.
	if got := atomic.LoadInt32(res.describeCalls); got != 0 {
		t.Fatalf("destructive delete refreshed the view (%d Describe calls); want 0", got)
	}
}

// currentActionTarget must resolve a list view with an empty id (list context),
// even when a row is highlighted, so list action hints and dispatch agree and a
// per-id (detail-only) lifecycle key stays inert on a list.
func TestCurrentActionTargetPassesEmptyIdForList(t *testing.T) {
	s, _ := actionableShell(t, []resource.Action{{Key: 'c', Label: "create widget"}})
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})
	s.table.SetData(
		[]resource.Column{{Title: "ID"}},
		[]resource.Row{{ID: "row1", Cells: []string{"row1"}}},
		SortState{},
	)
	s.table.Select(1, 0)

	_, id, ok := s.currentActionTarget()
	if !ok {
		t.Fatal("expected the list's actionable resource to resolve")
	}
	if id != "" {
		t.Fatalf("list context should resolve an empty id, got %q", id)
	}
}

func TestActionNextNavigatesToCreatedEntityOnSuccess(t *testing.T) {
	const createdID = "NEW-task-123"
	action := resource.Action{
		Key:     'c',
		Label:   "create widget",
		Prompt:  "Create a widget?",
		Perform: func(resource.ActionInput) error { return nil },
		Next: func() (resource.NavTarget, bool) {
			return resource.NavTarget{ResourceName: "target", ID: createdID, Kind: resource.NavDetail}, true
		},
	}
	s, _ := actionableShell(t, []resource.Action{action})
	// showDetail resolves the navigation target through the registry, so the
	// resource Next points at must be registered.
	s.registry.Register(fakeResource{name: "target"})
	startRunning(t, s)

	s.app.QueueUpdateDraw(func() { s.startAction(action) })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })

	s.app.QueueUpdateDraw(func() { s.submitAction() })

	// A successful Perform with a Next target navigates to that entity's
	// detail (pushed on top of the launching view) and closes the dialog.
	waitFor(t, func() bool {
		return readOnUI(s, func() bool {
			top, ok := s.stack.Top()
			return ok && top.ResourceName == "target" && top.SelectedID == createdID && top.Kind == DetailKind
		})
	})
	if readOnUI(s, func() bool { return s.actionOpen }) {
		t.Fatalf("dialog should be closed after navigating to the created entity")
	}
}

func TestActionInputHistoryCyclesWithCtrlPCtrlN(t *testing.T) {
	action := resource.Action{
		Key:          'c',
		Label:        "create widget",
		Prompt:       "Paste a definition",
		Input:        resource.InputYAML,
		InputLabel:   "definition",
		InitialText:  "newest",
		InputHistory: []string{"newest", "middle", "oldest"},
		Perform:      func(resource.ActionInput) error { return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})
	s.startAction(action)

	ta, ok := s.actionView.form.GetFormItem(s.actionView.inputIndex).(*tview.TextArea)
	if !ok {
		t.Fatalf("expected a multi-line text area for a YAML input")
	}

	// The dialog installs an input capture on the text area for the history
	// keys; drive it directly (as tview would on a keystroke). Ctrl-P walks
	// toward older entries, Ctrl-N back toward newer, clamping at each end.
	capture := ta.GetInputCapture()
	if capture == nil {
		t.Fatalf("expected a history input capture on the text area")
	}
	ctrlP := tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone)
	ctrlN := tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone)

	if capture(ctrlP) != nil { // history keys are consumed, not forwarded
		t.Fatalf("Ctrl-P should be consumed by the history capture")
	}
	if got := ta.GetText(); got != "middle" {
		t.Fatalf("after one Ctrl-P: text = %q, want middle", got)
	}
	capture(ctrlP)
	if got := ta.GetText(); got != "oldest" {
		t.Fatalf("after two Ctrl-P: text = %q, want oldest", got)
	}
	capture(ctrlP) // clamps at the oldest
	if got := ta.GetText(); got != "oldest" {
		t.Fatalf("Ctrl-P past the end should clamp at oldest, got %q", got)
	}
	if capture(ctrlN) != nil {
		t.Fatalf("Ctrl-N should be consumed by the history capture")
	}
	if got := ta.GetText(); got != "middle" {
		t.Fatalf("after Ctrl-N: text = %q, want middle", got)
	}

	// A non-history key passes through untouched.
	other := tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone)
	if capture(other) != other {
		t.Fatalf("a normal key should pass through the capture")
	}
}

func TestActionInputHistoryPreservesEditedDraft(t *testing.T) {
	// Editing the opening buffer (index 0) and then cycling into history must
	// not discard the edit: cycling back restores the edited draft, not the
	// pristine newest history entry. Likewise an edit made while browsing an
	// older entry survives a round trip.
	action := resource.Action{
		Key:          'c',
		Label:        "create widget",
		Prompt:       "Paste a definition",
		Input:        resource.InputYAML,
		InputLabel:   "definition",
		InitialText:  "newest",
		InputHistory: []string{"newest", "middle", "oldest"},
		Perform:      func(resource.ActionInput) error { return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})
	s.startAction(action)

	ta, ok := s.actionView.form.GetFormItem(s.actionView.inputIndex).(*tview.TextArea)
	if !ok {
		t.Fatalf("expected a multi-line text area for a YAML input")
	}
	capture := ta.GetInputCapture()
	ctrlP := tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone)
	ctrlN := tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone)

	// Edit the opening draft, then walk to an older entry and back.
	ta.SetText("my edited draft", true)
	capture(ctrlP)
	if got := ta.GetText(); got != "middle" {
		t.Fatalf("after Ctrl-P: text = %q, want middle", got)
	}
	capture(ctrlN)
	if got := ta.GetText(); got != "my edited draft" {
		t.Fatalf("Ctrl-N should restore the edited draft, got %q", got)
	}

	// An edit made on a history entry is likewise preserved across a round trip.
	capture(ctrlP) // -> middle
	ta.SetText("edited middle", true)
	capture(ctrlP) // -> oldest
	if got := ta.GetText(); got != "oldest" {
		t.Fatalf("after Ctrl-P to oldest: text = %q, want oldest", got)
	}
	capture(ctrlN) // -> back to (edited) middle
	if got := ta.GetText(); got != "edited middle" {
		t.Fatalf("Ctrl-N should restore the edited middle entry, got %q", got)
	}
}

func TestActionPerformErrorKeepsDialogOpen(t *testing.T) {
	action := resource.Action{
		Key:     'g',
		Label:   "do it",
		Prompt:  "Proceed?",
		Perform: func(resource.ActionInput) error { return fmt.Errorf("boom from API") },
	}
	s, _ := actionableShell(t, []resource.Action{action})
	startRunning(t, s)

	s.app.QueueUpdateDraw(func() { s.startAction(action) })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })

	s.app.QueueUpdateDraw(func() { s.submitAction() })

	waitFor(t, func() bool {
		return strings.Contains(readOnUI(s, func() string { return s.actionView.status.GetText(true) }), "boom from API")
	})

	if !readOnUI(s, func() bool { return s.actionOpen }) {
		t.Fatalf("dialog should stay open so the user can retry after an API failure")
	}
}

func TestActionKeyDispatchOpensDialog(t *testing.T) {
	action := resource.Action{
		Key:     'C',
		Label:   "cancel widget",
		Prompt:  "Cancel widget w1?",
		Perform: func(resource.ActionInput) error { return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})

	consumed := s.globalInputCapture(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone))

	if consumed != nil {
		t.Fatalf("expected the action key to be consumed, got %+v", consumed)
	}
	if !s.actionOpen {
		t.Fatalf("expected the action dialog to open on its key")
	}
	if s.currentAction.Label != "cancel widget" {
		t.Fatalf("expected the dispatched action to be current, got %q", s.currentAction.Label)
	}
}

func TestActionResolvesOnListWithoutSelectedRow(t *testing.T) {
	// A resource-level action (e.g. create task) must be reachable from a list
	// that has no highlighted row yet — empty or still loading — since it
	// ignores the row id.
	action := resource.Action{
		Key:     'c',
		Label:   "create widget",
		Prompt:  "Create a widget?",
		Perform: func(resource.ActionInput) error { return nil },
	}
	s, _ := actionableShell(t, []resource.Action{action})
	// Put a list view of the actionable resource on top; s.table has no rows,
	// so SelectedRow() reports no selection.
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	resolved, ok := s.resolveActionByKey('c')
	if !ok {
		t.Fatalf("expected the action to resolve on a list with no selected row")
	}
	if resolved.Label != "create widget" {
		t.Fatalf("resolved wrong action: %q", resolved.Label)
	}
}

func TestGlobalKeysPassThroughWhileActionOpen(t *testing.T) {
	s, _ := actionableShell(t, nil)
	s.actionOpen = true

	// 'q' would normally quit; while the dialog owns input it must reach the
	// form untouched instead.
	event := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if got := s.globalInputCapture(event); got != event {
		t.Fatalf("expected keys to pass through to the action form, got %+v", got)
	}
}

func TestCreateTaskCommandOpensDialogFromAnywhere(t *testing.T) {
	s, _ := actionableShell(t, nil) // helper pushes a non-empty base stack
	s.registry.Register(resource.NewCreateTaskResource(nil, resource.NewTaskDefHistory()))
	s.openInEditor = func(seed string) (string, error) { return seed, nil }
	startRunning(t, s)

	s.app.QueueUpdateDraw(func() { s.switchResource("createtask", "") })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })
	if readOnUI(s, func() string { return s.currentAction.Label }) == "" {
		t.Fatal(":createtask dispatched without the create action")
	}
}

func TestEditorHandoffReportsUnavailableScreen(t *testing.T) {
	s, _ := actionableShell(t, nil)
	s.openInEditor = func(string) (string, error) { return "", errEditorScreenUnavailable }
	startRunning(t, s)

	a := resource.Action{Label: "create task", Input: resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil }}
	s.app.QueueUpdateDraw(func() { s.startAction(a) })
	waitFor(t, func() bool {
		return strings.Contains(readOnUI(s, func() string { return s.footerBreadcrumb.GetText(true) }), "unavailable")
	})
	if readOnUI(s, func() bool { return s.actionOpen }) {
		t.Fatal("a failed handoff must not leave the action open")
	}
}

// shellWithRootAndCreateTask builds a Shell with a real listable resource
// named root plus CreateTaskResource, and an EMPTY nav stack — the cold-start
// shape (`tc-tui createtask` with no restored/base view) actionableShell
// deliberately doesn't produce.
func shellWithRootAndCreateTask(t *testing.T, root string) *Shell {
	t.Helper()
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: root})
	registry.Register(resource.NewCreateTaskResource(nil, resource.NewTaskDefHistory()))
	return New(registry)
}

func TestColdStartCommandActionCancelReturnsToRoot(t *testing.T) {
	// `tc-tui createtask` from an empty stack: cancelling must land on the root
	// view (widgets), not a blank screen. This also proves the deferred handoff
	// runs after the loop starts (replaces the old defer-only test).
	s := shellWithRootAndCreateTask(t, "widgets")
	s.openInEditor = func(string) (string, error) { return "name: x", nil }
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	s.app.SetScreen(screen)
	go func() { _ = s.StartAt("widgets", "createtask", "") }()
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })
	s.app.QueueUpdateDraw(func() { s.closeAction() })
	waitFor(t, func() bool {
		return !readOnUI(s, func() bool { return s.actionOpen }) &&
			readOnUI(s, func() bool { v, ok := s.stack.Top(); return ok && v.ResourceName == "widgets" })
	})
	readOnUI(s, func() struct{} { s.Stop(); return struct{}{} })
}

func TestColdStartCommandActionEditorFailureReturnsToRoot(t *testing.T) {
	s := shellWithRootAndCreateTask(t, "widgets")
	s.openInEditor = func(string) (string, error) { return "", errEditorScreenUnavailable }
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	s.app.SetScreen(screen)
	go func() { _ = s.StartAt("widgets", "createtask", "") }()
	waitFor(t, func() bool {
		return !readOnUI(s, func() bool { return s.actionOpen }) &&
			readOnUI(s, func() bool { v, ok := s.stack.Top(); return ok && v.ResourceName == "widgets" })
	})
	readOnUI(s, func() struct{} { s.Stop(); return struct{}{} })
}

func TestEditorConfirmCancelAfterReEditReturnsToOrigin(t *testing.T) {
	// startAction from an origin page (widgets' detail); edit; Edit Again;
	// Cancel must land back on the origin page, not on pageEditorConfirm, with
	// actionOpen=false.
	s, _ := actionableShell(t, nil)
	s.openInEditor = func(seed string) (string, error) { return seed + "-edited", nil }
	startRunning(t, s)

	// actionableShell only pushes a Detail View onto the stack without
	// rendering it; render it for real so the origin page is genuinely
	// pageDetail (not whatever page New() happens to default to).
	s.app.QueueUpdateDraw(func() { s.showDetail("widgets", "w1") })
	waitFor(t, func() bool {
		return readOnUI(s, func() string { name, _ := s.content.GetFrontPage(); return name }) == pageDetail
	})

	a := resource.Action{Label: "create task", Input: resource.InputExternalEditor,
		InitialText: "name: x", Perform: func(resource.ActionInput) error { return nil }}
	s.app.QueueUpdateDraw(func() { s.startAction(a) })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })
	if front := readOnUI(s, func() string { name, _ := s.content.GetFrontPage(); return name }); front != pageEditorConfirm {
		t.Fatalf("expected the confirm screen to be front, got %q", front)
	}

	// Edit Again: dispatches through the confirm view's own key handler,
	// exactly as globalInputCapture would once actionOpen routes it there.
	s.app.QueueUpdateDraw(func() {
		s.editorConfirm.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone), func(tview.Primitive) {})
	})
	// openInEditor appends "-edited" every call, including the initial handoff
	// (which already turned "name: x" into "name: x-edited") — a SECOND
	// "-edited" here proves Edit Again re-seeded from the CURRENT buffer, not
	// the action's original InitialText.
	waitFor(t, func() bool {
		return readOnUI(s, func() bool { return s.editorConfirm.buffer == "name: x-edited-edited" })
	})

	s.app.QueueUpdateDraw(func() {
		s.editorConfirm.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})
	})
	waitFor(t, func() bool { return !readOnUI(s, func() bool { return s.actionOpen }) })

	if front := readOnUI(s, func() string { name, _ := s.content.GetFrontPage(); return name }); front != pageDetail {
		t.Fatalf("Cancel after Edit Again returned to %q, want the true origin pageDetail", front)
	}
}

func TestEditorConfirmRetryAfterPerformFailureReusesIdenticalRaw(t *testing.T) {
	// End-to-end through the real submit path (performActionInput), not just
	// EditorConfirmView in isolation: a Perform failure must not change what
	// the confirm screen resubmits on retry — createTaskAction's
	// retry-idempotency memo depends on the raw text staying byte-identical
	// across attempts (see resource/create_task.go), so a lost-response retry
	// reuses the same taskId instead of minting a new one.
	s, _ := actionableShell(t, nil)
	s.openInEditor = func(seed string) (string, error) { return seed, nil }
	startRunning(t, s)

	var gotRaw []string
	var callCount int32
	a := resource.Action{
		Label:       "create task",
		Input:       resource.InputExternalEditor,
		InitialText: "name: x",
		Perform: func(in resource.ActionInput) error {
			gotRaw = append(gotRaw, in.Raw)
			if atomic.AddInt32(&callCount, 1) == 1 {
				return fmt.Errorf("transport blip")
			}
			return nil
		},
	}
	s.app.QueueUpdateDraw(func() { s.startAction(a) })
	waitFor(t, func() bool { return readOnUI(s, func() bool { return s.actionOpen }) })

	confirm := func() {
		s.app.QueueUpdateDraw(func() {
			s.editorConfirm.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone), func(tview.Primitive) {})
		})
	}
	confirm() // first attempt: fails
	waitFor(t, func() bool { return atomic.LoadInt32(&callCount) == 1 })
	confirm() // retry: must resubmit the identical raw text
	waitFor(t, func() bool { return atomic.LoadInt32(&callCount) == 2 })

	if readOnUI(s, func() string { return s.editorConfirm.buffer }) != "name: x" {
		t.Fatalf("v.buffer must stay byte-identical after a Perform failure, got %q",
			readOnUI(s, func() string { return s.editorConfirm.buffer }))
	}
	if len(gotRaw) != 2 || gotRaw[0] != gotRaw[1] || gotRaw[0] != "name: x" {
		t.Fatalf("Perform got %v across retries, want [\"name: x\", \"name: x\"]", gotRaw)
	}
}

func TestRenderHeaderHintsShowsActionKeys(t *testing.T) {
	s := New(resource.NewRegistry())
	s.stack.Push(View{ResourceName: "widgets", Kind: DetailKind, SelectedID: "w1"})
	s.currentActions = []resource.Action{{Key: 'd', Label: "delete widget", Destructive: true}}

	s.renderHeaderHints()

	text := s.headerHint.GetText(false)
	if !strings.Contains(text, "delete widget") || !strings.Contains(text, "d") {
		t.Fatalf("expected the action key hint to be shown, got %q", text)
	}
}

// renderActionView draws v to an offscreen simulation screen and returns the
// visible text, one string per row. It's how the button-visibility tests below
// assert on what's actually painted rather than on internal focus state.
func renderActionView(t *testing.T, v *ActionView, width, height int) string {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	screen.SetSize(width, height)
	v.SetRect(0, 0, width, height)
	v.Draw(screen)
	screen.Show()

	cells, w, h := screen.GetContents()
	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			runes := cells[y*w+x].Runes
			if len(runes) == 0 {
				b.WriteRune(' ')
				continue
			}
			b.WriteString(string(runes))
		}
		b.WriteRune('\n')
	}
	return b.String()
}

// TestActionViewRendersBothButtons guards against the dialog opening too short
// for its content: tview's Form scrolls to keep only the focused item visible,
// so a box a row short clipped the button row on a confirm-only action and hid
// the input once focus reached the buttons. Both buttons — and any input —
// must be painted at once, without tabbing.
func TestActionViewRendersBothButtons(t *testing.T) {
	tests := []struct {
		name   string
		action resource.Action
		want   []string
	}{
		{
			name: "confirm-only destructive",
			action: resource.Action{
				Label:       "cancel task",
				Prompt:      "Cancel task GJ8BQGOPS62axPVVkc0qiQ? Its current run will be resolved as an exception with reason \"canceled\".",
				Destructive: true,
			},
			want: []string{"[ Confirm ]", "[ Cancel ]"},
		},
		{
			name: "single-line input",
			action: resource.Action{
				Label:      "change priority",
				Prompt:     "Change priority of task GJ8BQGOPS62axPVVkc0qiQ — one of: highest, very-high, high, medium, low, very-low, lowest, normal.",
				Input:      resource.InputLine,
				InputLabel: "priority",
			},
			want: []string{"priority", "[ Confirm ]", "[ Cancel ]"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewActionView()
			v.SetAction(tc.action, func() {}, func() {})
			got := renderActionView(t, v, 160, 40)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("rendered dialog missing %q; got:\n%s", want, got)
				}
			}
		})
	}
}
