package shell

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/taskcluster/tc-tui/resource"
)

// editorConfirmWidth/Height size the centered confirm box, mirroring
// actionModalWidthMultiline. Height is generous to fit the scrollable buffer
// plus the dedicated hint line and its separator (see hints, separator).
const (
	editorConfirmWidth  = 96
	editorConfirmHeight = 32
)

// EditorConfirmView renders the read-only, scrollable screen shown after an
// InputExternalEditor action returns from $EDITOR: the buffer as last saved,
// a persistent hotkey line (confirm, re-edit, cancel, each named
// BufferTransform), and a validity status line. Unlike ActionView there is no
// Form and no clickable button — the hint line is the only affordance telling
// the user these are keystrokes, not selectable UI elements, so it must read
// unambiguously as shortcuts (matching the app's header-hint styling) rather
// than a row of inert labels. Only a transform or a re-edit round trip
// changes the buffer — the user never types into this view directly.
type EditorConfirmView struct {
	*tview.Flex // the centered wrapper; the primitive added to s.content

	hints     *tview.TextView
	separator *tview.TextView
	content   *tview.TextView
	status    *tview.TextView

	action resource.Action
	buffer string
	valid  bool

	onConfirm func(raw string)
	onReEdit  func(current string)
	onCancel  func()
}

func NewEditorConfirmView() *EditorConfirmView {
	v := &EditorConfirmView{
		Flex:      tview.NewFlex(),
		hints:     tview.NewTextView().SetDynamicColors(true),
		separator: tview.NewTextView().SetTextColor(tview.Styles.SecondaryTextColor),
		content:   tview.NewTextView().SetWrap(false).SetDynamicColors(true),
		status:    tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
	}
	v.content.SetScrollable(true)
	// A fixed-width rule under the hint line — tview clips it to whatever
	// width the box actually gets, so overshooting is harmless and avoids
	// having to recompute it against the box's own layout.
	v.separator.SetText(strings.Repeat("─", 300))
	return v
}

// SetContent rebuilds the view for a's buffer — the text just returned from
// $EDITOR, a transform's rewrite, or a re-edit round trip. buffer is taken as
// an argument rather than read from a field set before a reset, so a stale
// buffer left over from a previous action can never leak into this render.
// onConfirm/onReEdit/onCancel are invoked by the matching hotkey.
func (v *EditorConfirmView) SetContent(a resource.Action, buffer string, onConfirm func(string), onReEdit func(string), onCancel func()) {
	v.action = a
	v.buffer = buffer
	v.onConfirm = onConfirm
	v.onReEdit = onReEdit
	v.onCancel = onCancel

	v.hints.SetText(editorConfirmHints(a))
	v.content.SetText(tview.Escape(buffer)).ScrollToBeginning()
	v.revalidate()

	box := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.hints, 1, 0, false).
		AddItem(v.separator, 1, 0, false).
		AddItem(v.content, 0, 1, true).
		AddItem(v.status, 2, 0, false)
	box.SetBorder(true).SetTitle(editorConfirmTitle(a))

	// Rebuild the centered wrapper (spacer | box | spacer, vertically and
	// horizontally), same pattern as ActionView.SetAction — Clear() leaves
	// the root Flex reusable so s.content keeps referencing this same
	// primitive across successive edit/re-edit rounds.
	v.Clear()
	v.SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(box, editorConfirmHeight, 0, true).
			AddItem(nil, 0, 1, false), editorConfirmWidth, 0, true).
		AddItem(nil, 0, 1, false)
}

// editorConfirmTitle is the confirm box's bordered title — just the action's
// label, sentence-cased and bracketed (e.g. "[ Create task ]", matching the
// app's own "[ Taskcluster :: ... ]" title style); the hotkeys themselves live
// in the persistent hint line below the border (editorConfirmHints), not
// crammed into the title.
func editorConfirmTitle(a resource.Action) string {
	label := a.Label
	if label == "" {
		label = "confirm"
	}
	return fmt.Sprintf("[ %s ]", capitalizeFirst(label))
}

// capitalizeFirst upper-cases only s's first rune, leaving the rest (and any
// other words) untouched — "create task" -> "Create task", not "Create Task".
func capitalizeFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// editorConfirmHints renders the confirm screen's hotkey line in the same
// "[yellow]key[white] label" styling used everywhere else in the app (see
// Shell.renderHeaderHints) — a plain word list (as the title used to show)
// reads like a row of clickable buttons; coloring the actual key each maps to
// is what marks it as a keystroke instead.
func editorConfirmHints(a resource.Action) string {
	hints := "[yellow]c[white]/[yellow]Enter[white] confirm    " +
		"[yellow]e[white] edit again    " +
		"[yellow]Esc[white] cancel"
	for _, tr := range a.Transforms {
		hints += fmt.Sprintf("    [yellow]%c[white] %s", tr.Key, tr.Label)
	}
	return " " + hints
}

// SetStatus shows the status line under the content pane — a validation
// result (green when valid, red otherwise) or an editor-launch failure on
// re-edit (kept red, the confirm view left open so the user can retry). A
// Perform failure (identified by performFailurePrefix) is routed to
// showPerformFailure instead: it can be an arbitrarily long API error (e.g.
// an auth failure's full call summary) that this 2-row line has no way to
// show in full, let alone scroll to read.
func (v *EditorConfirmView) SetStatus(msg string, isError bool) {
	if isError && strings.HasPrefix(msg, performFailurePrefix) {
		v.showPerformFailure(strings.TrimPrefix(msg, performFailurePrefix))
		return
	}
	color := "green"
	if isError {
		color = "red"
	}
	v.status.SetText(fmt.Sprintf("[%s]%s[white]", color, tview.Escape(msg)))
}

// showPerformFailure surfaces a Perform failure by prepending it as a
// comment block ahead of the buffer in the DISPLAYED content pane, so the
// whole error is actually readable (PgUp/PgDn/j/k/G, same as the definition
// itself) — mirrors k9s falling back to its edit view with the error
// commented into the definition on a failed apply.
//
// Critically, this only changes what's rendered — v.buffer itself (what a
// subsequent Confirm actually submits) is left byte-for-byte untouched.
// createTaskAction's retry-idempotency memo is keyed on that exact raw text
// staying identical across attempts, so the queue reuses the same taskId
// instead of minting a new one; if the first attempt actually succeeded
// server-side and only its response was lost, mutating the buffer here would
// make a plain retry create a duplicate task. Applying a transform or
// re-editing still legitimately changes the buffer (and clears this display)
// through their own paths, same as before.
func (v *EditorConfirmView) showPerformFailure(errText string) {
	var b strings.Builder
	b.WriteString("# submit failed:\n")
	for _, line := range strings.Split(errText, "\n") {
		b.WriteString("# ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(v.buffer)

	v.content.SetText(tview.Escape(b.String())).ScrollToBeginning()
	v.status.SetText("[red]submit failed — see the commented error above; fix and retry, or 'e' to re-edit[white]")
}

// revalidate re-parses/validates the current buffer against the action,
// updating v.valid and the status line — called after SetContent and after
// any buffer change (a transform application). This is a UI nudge only:
// performActionInput independently re-checks before Perform ever runs, so an
// invalid buffer can't submit even if this gate were somehow bypassed.
func (v *EditorConfirmView) revalidate() {
	in, err := resource.ParseActionInput(v.action.Input, v.buffer, !v.action.OptionalInput)
	if err == nil && v.action.Validate != nil {
		err = v.action.Validate(in)
	}
	if err != nil {
		v.valid = false
		v.SetStatus(err.Error(), true)
		return
	}
	v.valid = true
	v.SetStatus("valid — will submit as a new task", false)
}

// confirm fires onConfirm with the current buffer, unless it's currently
// invalid — a status nudge instead, so Confirm on a broken buffer is a no-op
// rather than reaching performActionInput's own (redundant but authoritative)
// validation error.
func (v *EditorConfirmView) confirm() {
	if !v.valid {
		v.SetStatus("fix the definition before confirming", true)
		return
	}
	if v.onConfirm != nil {
		v.onConfirm(v.buffer)
	}
}

// applyTransform rewrites the buffer via tr.Apply, re-rendering the content
// pane and re-validating on success; a transform error is shown in the
// status line without touching the buffer.
func (v *EditorConfirmView) applyTransform(tr resource.BufferTransform) {
	out, err := tr.Apply(v.buffer)
	if err != nil {
		v.SetStatus(err.Error(), true)
		return
	}
	v.buffer = out
	v.content.SetText(tview.Escape(out)).ScrollToBeginning()
	v.revalidate()
}

// InputHandler maps hotkeys (confirm, re-edit, cancel, each BufferTransform's
// key) before falling through to the content TextView's own input handler,
// so PgUp/PgDn/Home/End/arrows/g/G/j/k/h/l keep scrolling it — see
// tview.TextView.InputHandler, which itself would otherwise swallow Enter/Esc
// as "done" keys, which is why those must be intercepted here first.
func (v *EditorConfirmView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		switch event.Key() {
		case tcell.KeyEscape:
			if v.onCancel != nil {
				v.onCancel()
			}
			return
		case tcell.KeyEnter:
			v.confirm()
			return
		}

		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'c':
				v.confirm()
				return
			case 'e':
				if v.onReEdit != nil {
					v.onReEdit(v.buffer)
				}
				return
			}
			for _, tr := range v.action.Transforms {
				if event.Rune() == tr.Key {
					v.applyTransform(tr)
					return
				}
			}
		}

		if handler := v.content.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	}
}
