package shell

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/taskcluster/tc-tui/resource"
)

// actionModalWidth sizes the centered action dialog; a multiline
// (YAML/JSON/text) action gets a wider box for its text area, while a
// confirm-only or single-line action gets a compact one. The height is
// computed per action in SetAction to fit its content exactly.
const (
	actionModalWidth          = 78
	actionModalWidthMultiline = 96
	actionTextAreaHeight      = 12
)

// ActionView renders the shared authenticated-action dialog: a centered,
// bordered box holding a warning/prompt message, an optional input field
// (single-line or a multi-line text area, per the Action's InputMode), an
// optional "type the confirm word" field for a destructive action, Confirm/
// Cancel buttons, and a status line for validation errors, progress, and API
// failures. It is generic over resource.Action — it knows nothing about what
// any particular action does; the Shell wires the submit/cancel callbacks and
// drives validation, Perform, and cache invalidation around it.
type ActionView struct {
	*tview.Flex // the centered wrapper; the primitive added to s.content

	message *tview.TextView
	form    *tview.Form
	status  *tview.TextView

	// inputIndex/confirmIndex are the form-item positions of the value input
	// and the destructive confirm-word field, or -1 when the current action
	// has no such field. Buttons are not form items, so these index only the
	// text inputs, in the order they were added.
	inputIndex   int
	confirmIndex int

	// historyItems holds the action's InputHistory (newest first) and
	// historyIndex tracks which one the multi-line input currently shows, so
	// Ctrl-P/Ctrl-N can cycle through recently submitted values. historyDrafts
	// records edits keyed by index so cycling away from a modified buffer and
	// back restores the edit rather than the pristine history entry — in
	// particular it preserves the working draft at index 0 (the buffer the
	// dialog opened with), which a user typically edits before submitting.
	historyItems  []string
	historyIndex  int
	historyDrafts map[int]string

	onSubmit func()
	onCancel func()
}

func NewActionView() *ActionView {
	v := &ActionView{
		Flex:         tview.NewFlex(),
		message:      tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		status:       tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		inputIndex:   -1,
		confirmIndex: -1,
	}
	return v
}

// SetAction rebuilds the dialog for a. onSubmit is invoked when the user
// activates the Confirm button (the Shell then validates and performs);
// onCancel when they pick Cancel or press Esc.
func (v *ActionView) SetAction(a resource.Action, onSubmit, onCancel func()) {
	v.onSubmit = onSubmit
	v.onCancel = onCancel
	v.inputIndex = -1
	v.confirmIndex = -1
	v.historyItems = nil
	v.historyIndex = 0
	v.historyDrafts = nil

	v.message.SetText(actionMessage(a))
	v.status.SetText("")

	form := tview.NewForm()
	form.SetButtonsAlign(tview.AlignRight)

	// Make the focused button unmistakable. tview lays form buttons out one row
	// high, so a drawn box border isn't possible here — instead the active
	// button fills with a solid accent block (red for a destructive action,
	// else the theme's border color) and bold dark text, while the inactive
	// button stays a dim, unfilled label. The bracketed labels give both a
	// visible frame; only the focused one lights up.
	buttonAccent, buttonText := tview.Styles.BorderColor, tcell.ColorBlack
	if a.Destructive {
		buttonAccent, buttonText = tcell.ColorRed, tcell.ColorWhite
	}
	form.SetButtonStyle(tcell.StyleDefault.Foreground(tcell.ColorGray))
	form.SetButtonActivatedStyle(tcell.StyleDefault.
		Background(buttonAccent).
		Foreground(buttonText).
		Bold(true))

	next := 0
	if a.Input != resource.InputNone {
		label := a.InputLabel
		if label == "" {
			label = "value"
		}
		if a.Input.Multiline() {
			form.AddTextArea(label, a.InitialText, 0, actionTextAreaHeight, 0, nil)
			if len(a.InputHistory) > 0 {
				if ta, ok := form.GetFormItem(next).(*tview.TextArea); ok {
					v.historyItems = a.InputHistory
					ta.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
						switch event.Key() {
						case tcell.KeyCtrlP: // older
							v.cycleInputHistory(ta, 1)
							return nil
						case tcell.KeyCtrlN: // newer
							v.cycleInputHistory(ta, -1)
							return nil
						}
						return event
					})
				}
			}
		} else {
			form.AddInputField(label, a.InitialText, 0, nil, nil)
		}
		v.inputIndex = next
		next++
	}
	if a.Destructive && a.ConfirmWord != "" {
		form.AddInputField(fmt.Sprintf("type %q to confirm", a.ConfirmWord), "", 0, nil, nil)
		v.confirmIndex = next
		next++
	}

	form.AddButton("[ Confirm ]", func() {
		if v.onSubmit != nil {
			v.onSubmit()
		}
	})
	form.AddButton("[ Cancel ]", func() {
		if v.onCancel != nil {
			v.onCancel()
		}
	})
	form.SetCancelFunc(func() {
		if v.onCancel != nil {
			v.onCancel()
		}
	})
	// A confirm-only action has no fields to fill in — start focus on the
	// Confirm button so Enter alone can drive it. An action with input starts
	// on the first field instead.
	if a.Input == resource.InputNone {
		form.SetFocus(form.GetFormItemCount())
	}
	v.form = form

	box := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.message, actionMessageHeight(a), 0, false).
		AddItem(form, 0, 1, true).
		AddItem(v.status, 2, 0, false)
	box.SetBorder(true).SetTitle(actionTitle(a))
	if a.Destructive {
		box.SetBorderColor(tcell.ColorRed).SetTitleColor(tcell.ColorRed)
	}

	width := actionModalWidth
	if a.Input.Multiline() {
		width = actionModalWidthMultiline
	}

	// Size the box to fit its content exactly so tview's Form never scrolls a
	// focused field or the button row out of view. (A fixed height that came up
	// a row short clipped the buttons on confirm-only actions, and hid the
	// input once focus reached the buttons.) The form draws each of its `next`
	// items one row tall — actionTextAreaHeight for a multiline field — with a
	// blank pad row after each, then the button row, all inside 1 row of border
	// padding top and bottom.
	extraTextArea := 0
	if a.Input.Multiline() {
		extraTextArea = actionTextAreaHeight - 1
	}
	formRows := 2*next + extraTextArea + 3               // item rows + pad rows + button row + form border padding
	height := actionMessageHeight(a) + formRows + 2 + 2 // message + form + status + box border

	// Rebuild the centered wrapper (spacer | box | spacer, vertically and
	// horizontally) so the box floats mid-screen at the size this action
	// needs. Clear() leaves the root Flex reusable — s.content keeps
	// referencing the same primitive across successive actions.
	v.Clear()
	v.SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(box, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

// cycleInputHistory replaces the text area with an older (delta +1) or newer
// (delta -1) entry, staying within bounds. The dialog opens showing
// historyItems[0] (== InitialText), so the cursor starts at index 0. Before
// moving, the current buffer is saved as the draft for the index being left, so
// any edits (including to the opening buffer at index 0) survive a round trip
// and are restored when the user cycles back rather than being overwritten by
// the pristine history entry.
func (v *ActionView) cycleInputHistory(ta *tview.TextArea, delta int) {
	next := v.historyIndex + delta
	if next < 0 || next >= len(v.historyItems) {
		return
	}
	if v.historyDrafts == nil {
		v.historyDrafts = make(map[int]string)
	}
	v.historyDrafts[v.historyIndex] = ta.GetText()

	v.historyIndex = next
	text := v.historyItems[next]
	if draft, ok := v.historyDrafts[next]; ok {
		text = draft
	}
	ta.SetText(text, true)
}

// InputText returns the current value-input text, or "" when the action has
// no input field.
func (v *ActionView) InputText() string {
	return v.formItemText(v.inputIndex)
}

// ConfirmWordText returns what the user typed into the destructive confirm-
// word field, or "" when the action has none.
func (v *ActionView) ConfirmWordText() string {
	return v.formItemText(v.confirmIndex)
}

func (v *ActionView) formItemText(index int) string {
	if index < 0 || v.form == nil {
		return ""
	}
	switch item := v.form.GetFormItem(index).(type) {
	case *tview.TextArea:
		return item.GetText()
	case *tview.InputField:
		return item.GetText()
	default:
		return ""
	}
}

// setFieldText writes text into the given form field — used only by tests to
// simulate typing, since a SimulationScreen can't send real keystrokes into a
// focused field ergonomically.
func (v *ActionView) setFieldText(index int, text string) {
	if index < 0 || v.form == nil {
		return
	}
	switch item := v.form.GetFormItem(index).(type) {
	case *tview.TextArea:
		item.SetText(text, true)
	case *tview.InputField:
		item.SetText(text)
	}
}

// SetStatus shows a status line under the form — a validation message or an
// API failure (isError, red) or neutral progress (not isError, yellow).
func (v *ActionView) SetStatus(msg string, isError bool) {
	color := "yellow"
	if isError {
		color = "red"
	}
	v.status.SetText(fmt.Sprintf("[%s]%s[white]", color, tview.Escape(msg)))
}

// actionTitle is the dialog's bordered title.
func actionTitle(a resource.Action) string {
	if a.Destructive {
		return fmt.Sprintf(" [!] %s ", a.Label)
	}
	return fmt.Sprintf(" %s ", a.Label)
}

// actionMessage is the warning/prompt block shown above the form: a stern
// destructive banner (when applicable) followed by the action's prompt.
func actionMessage(a resource.Action) string {
	prompt := tview.Escape(a.Prompt)
	if a.Destructive {
		return "[red]⚠ This action is destructive and cannot be undone.[white]\n\n[white]" + prompt
	}
	return "[white]" + prompt
}

// actionMessageHeight budgets rows for actionMessage — enough for the
// two-line destructive banner plus a wrapped prompt, or just a wrapped
// prompt otherwise.
func actionMessageHeight(a resource.Action) int {
	if a.Destructive {
		return 5
	}
	return 3
}

// startAction opens the action dialog for a, remembering which content page
// to restore on close. The Shell drives the rest of the flow (submitAction,
// finishAction, closeAction).
func (s *Shell) startAction(a resource.Action) {
	s.actionReturnPage, _ = s.content.GetFrontPage()
	s.actionOpen = true
	s.actionBusy = false
	s.currentAction = a

	if a.Input == resource.InputExternalEditor {
		s.startEditorAction(a)
		return
	}

	s.actionView.SetAction(a,
		func() { s.submitAction() },
		func() { s.closeAction() },
	)

	s.content.SwitchToPage(pageAction)
	s.updateBorderColor()
	s.app.SetFocus(s.actionView.form)
}

// startEditorAction/reEdit both funnel into runEditorHandoff — the initial
// open (seeded from the action's InitialText) and a later "edit again" from
// the confirm screen (seeded from whatever's currently in the buffer,
// possibly already rewritten by a transform), respectively.
func (s *Shell) startEditorAction(a resource.Action)      { s.runEditorHandoff(a, a.InitialText, false) }
func (s *Shell) reEdit(a resource.Action, current string) { s.runEditorHandoff(a, current, true) }

// runEditorHandoff hands off to $EDITOR via s.openInEditor and, on success,
// shows the confirm screen. It runs on the event-loop goroutine — an input
// handler (the `:createtask` command, or the confirm screen's 'e' key) or the
// queued initial dispatch from StartAt — so s.openInEditor's Suspend call is
// made directly. Do NOT wrap this in QueueUpdate/Draw: both block on the
// loop and would deadlock when called from the loop goroutine itself.
//
// reedit distinguishes the initial open (a launch failure clears the action,
// same as any other action-open failure) from a later "edit again" (a launch
// failure instead keeps the confirm screen open, reporting the error there,
// so the user doesn't lose the buffer they were about to re-edit).
func (s *Shell) runEditorHandoff(a resource.Action, seed string, reedit bool) {
	text, err := s.openInEditor(seed)
	if err != nil {
		if reedit {
			s.editorConfirm.SetStatus(err.Error(), true)
			return
		}
		s.actionOpen = false
		label := a.Label
		if label == "" {
			label = "create task"
		}
		s.showTransientWarning(fmt.Sprintf("%s: %s", label, err))
		return
	}
	s.showEditorConfirm(a, text)
}

// showEditorConfirm renders the read-only, scrollable confirm screen for a's
// just-edited buffer. It deliberately does NOT touch s.actionReturnPage —
// that was captured once, in startAction, so an Edit Again round trip through
// this screen and back still closes to the action's true origin rather than
// to pageEditorConfirm itself.
func (s *Shell) showEditorConfirm(a resource.Action, buffer string) {
	s.editorConfirm.SetContent(a, buffer,
		func(raw string) { s.performActionInput(a, raw, s.editorConfirm.SetStatus) },
		func(cur string) { s.reEdit(a, cur) },
		func() { s.closeAction() },
	)
	s.content.SwitchToPage(pageEditorConfirm)
	s.updateBorderColor()
	s.app.SetFocus(s.editorConfirm)
}

// closeAction dismisses the dialog and returns to the page it was launched
// from, without performing anything.
func (s *Shell) closeAction() {
	if !s.actionOpen {
		return
	}
	s.actionOpen = false
	s.actionBusy = false

	page := s.actionReturnPage
	if page == "" || page == pageAction {
		page = pageDetail
	}
	s.content.SwitchToPage(page)
	s.updateBorderColor()
	s.app.SetFocus(s.activeContent)
}

// submitAction validates the collected input against the current action and,
// if it passes, runs Perform off the UI thread with a progress indicator. A
// validation failure stays in the dialog with a red status so the user can
// fix it; an API failure does the same after Perform returns.
func (s *Shell) submitAction() {
	if s.actionBusy {
		return // Perform already in flight; ignore a double activation
	}
	a := s.currentAction

	if a.Destructive && a.ConfirmWord != "" {
		if s.actionView.ConfirmWordText() != a.ConfirmWord {
			s.actionView.SetStatus(fmt.Sprintf("type %q exactly to confirm", a.ConfirmWord), true)
			return
		}
	}

	s.performActionInput(a, s.actionView.InputText(), s.actionView.SetStatus)
}

// performFailurePrefix marks a status message as a Perform (not
// parse/Validate) failure — the only status setStatus ever sends after the
// dialog/confirm screen has already accepted the input as well-formed and
// valid. EditorConfirmView.SetStatus keys off this exact prefix to route a
// Perform failure (which can be an arbitrarily long API error, e.g. an auth
// failure's full call summary) into its scrollable content pane instead of
// its 2-row status line — see EditorConfirmView.showPerformFailure.
const performFailurePrefix = "failed: "

// performActionInput validates raw for a and, if valid, performs it off the UI
// thread, reporting status via setStatus. Shared by the form dialog and the
// editor confirm screen so there is one submit path.
func (s *Shell) performActionInput(a resource.Action, raw string, setStatus func(string, bool)) {
	if s.actionBusy {
		return // Perform already in flight; ignore a double activation
	}
	input, err := resource.ParseActionInput(a.Input, raw, !a.OptionalInput)
	if err != nil {
		setStatus(err.Error(), true)
		return
	}
	if a.Validate != nil {
		if err := a.Validate(input); err != nil {
			setStatus(err.Error(), true)
			return
		}
	}

	s.actionBusy = true
	setStatus("Working…", false)

	go func() {
		err := a.Perform(input)
		s.app.QueueUpdateDraw(func() {
			s.actionBusy = false
			if err != nil {
				setStatus(performFailurePrefix+err.Error(), true)
				return
			}
			s.finishAction(a)
		})
	}()
}

// finishAction runs once Perform succeeds: it drops the caches the mutation
// invalidated (plus the current list's own, so the change is picked up on the
// next load), closes the dialog, and reports success in the footer.
//
// For a non-destructive edit it also force-refreshes the view it was launched
// from, so the new state is visible immediately — the fresh content is the
// real confirmation. A destructive action deliberately does NOT auto-refresh:
// the entity may no longer exist (a re-Describe would 404 into an error
// screen), and skipping the refresh also lets the "done" toast survive rather
// than being wiped by the re-render. The invalidated cache still guarantees
// the affected lists show the removal on their next load or auto-refresh
// tick.
func (s *Shell) finishAction(a resource.Action) {
	for _, name := range a.Invalidates {
		s.cache.invalidate(name)
	}
	if top, ok := s.stack.Top(); ok && top.Kind == ListKind {
		s.cache.invalidate(top.ResourceName)
	}

	s.closeAction()

	label := a.Label
	if label == "" {
		label = "action"
	}
	s.showTransientInfo(fmt.Sprintf("%s: done", label))

	// A create-style action navigates straight to what it just produced (the
	// new task's detail); its re-render replaces the toast, same as a refresh
	// would, and the fresh content is the real confirmation.
	if a.Next != nil {
		if target, ok := a.Next(); ok {
			s.navigateTo(target)
			return
		}
	}

	if !a.Destructive || a.RefreshAfter {
		// refreshCurrent re-fetches the top view; the list cache was just
		// dropped above so a list re-fetches fresh, and a detail always
		// re-Describes. Its re-render replaces the toast above, which is fine
		// here — the refreshed content is the confirmation.
		//
		// A destructive action normally skips this (the entity may be gone and
		// a re-Describe would 404), but one that resolves-but-doesn't-remove its
		// entity (RefreshAfter, e.g. cancel a task) refreshes so the new state
		// and re-gated actions show at once.
		s.refreshCurrent()
	}
}

// currentActionTarget resolves which Actionable resource + entity id the
// action keys apply to: the entity a Detail view shows, or the row currently
// highlighted on a List view (so an action can fire straight from a list
// without stepping into the entity first). Mirrors currentDownloadableTarget.
func (s *Shell) currentActionTarget() (res resource.Actionable, id string, ok bool) {
	top, hasTop := s.stack.Top()
	if !hasTop {
		return nil, "", false
	}

	var resourceName, entityID string
	if top.Kind == DetailKind {
		resourceName, entityID = top.ResourceName, top.SelectedID
	} else {
		// List context resolves with an empty id, matching how renderList builds
		// its action hints (Actions("")). This is the empty-id convention: a
		// resource distinguishes its list-level action (e.g. create task) from a
		// per-entity detail action (e.g. cancel this task) by whether id is
		// empty. Threading the highlighted row's id here instead would make list
		// dispatch disagree with the hints and let a detail-only action fire on
		// a highlighted row it was never advertised for.
		resourceName = top.ResourceName
	}

	r, rok := s.registry.Resolve(resourceName)
	if !rok {
		return nil, "", false
	}
	act, aok := r.(resource.Actionable)
	if !aok {
		return nil, "", false
	}
	return act, entityID, true
}

// resolveActionByKey finds the action bound to key on the current target, if
// any — the dispatch used by globalInputCapture.
func (s *Shell) resolveActionByKey(key rune) (resource.Action, bool) {
	act, id, ok := s.currentActionTarget()
	if !ok {
		return resource.Action{}, false
	}
	for _, a := range act.Actions(id) {
		if a.Key == key {
			return a, true
		}
	}
	return resource.Action{}, false
}
