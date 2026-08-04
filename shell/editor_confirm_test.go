package shell

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

func TestEditorConfirmValidatesAndGatesConfirm(t *testing.T) {
	var confirmed bool
	a := resource.Action{
		Label: "create task",
		Input: resource.InputExternalEditor,
		Validate: func(in resource.ActionInput) error {
			if !strings.Contains(in.Raw, "ok") {
				return fmt.Errorf("must contain ok")
			}
			return nil
		},
		Perform: func(resource.ActionInput) error { return nil },
	}
	v := NewEditorConfirmView()
	v.SetContent(a, "not valid", func(string) { confirmed = true }, func(string) {}, func() {})

	if v.valid {
		t.Fatal("an invalid buffer must not be marked valid")
	}
	if got := v.status.GetText(true); !strings.Contains(got, "must contain ok") {
		t.Fatalf("expected the validation error in the status, got %q", got)
	}

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone), nil)
	if confirmed {
		t.Fatal("Confirm on an invalid buffer must not invoke onConfirm")
	}
}

func TestEditorConfirmTransformRewritesAndRevalidates(t *testing.T) {
	a := resource.Action{
		Label: "create task",
		Input: resource.InputExternalEditor,
		Transforms: []resource.BufferTransform{{
			Key:   'u',
			Label: "uppercase",
			Apply: func(raw string) (string, error) { return strings.ToUpper(raw), nil },
		}},
		Validate: func(in resource.ActionInput) error {
			if in.Raw != strings.ToUpper(in.Raw) {
				return fmt.Errorf("must be uppercase")
			}
			return nil
		},
		Perform: func(resource.ActionInput) error { return nil },
	}
	v := NewEditorConfirmView()
	v.SetContent(a, "lowercase", func(string) {}, func(string) {}, func() {})
	if v.valid {
		t.Fatal("lowercase buffer should be invalid before the transform")
	}

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone), nil)

	if v.buffer != "LOWERCASE" {
		t.Fatalf("transform did not rewrite the buffer: %q", v.buffer)
	}
	if !v.valid {
		t.Fatalf("buffer should be valid after the transform, status: %q", v.status.GetText(true))
	}
}

func TestEditorConfirmSubmitsValidBuffer(t *testing.T) {
	var gotRaw string
	a := resource.Action{
		Label:   "create task",
		Input:   resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil },
	}
	v := NewEditorConfirmView()
	v.SetContent(a, "name: x", func(raw string) { gotRaw = raw }, func(string) {}, func() {})
	if !v.valid {
		t.Fatalf("expected a no-Validate action to be valid, status: %q", v.status.GetText(true))
	}

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone), nil)

	if gotRaw != "name: x" {
		t.Fatalf("onConfirm got %q, want the current buffer", gotRaw)
	}
}

func TestEditorConfirmReEditUsesCurrentBuffer(t *testing.T) {
	var gotSeed string
	a := resource.Action{
		Label:   "create task",
		Input:   resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil },
		Transforms: []resource.BufferTransform{{
			Key:   'u',
			Label: "uppercase",
			Apply: func(raw string) (string, error) { return strings.ToUpper(raw), nil },
		}},
	}
	v := NewEditorConfirmView()
	v.SetContent(a, "name: x", func(string) {}, func(cur string) { gotSeed = cur }, func() {})

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone), nil) // transform first
	handler(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone), nil) // re-edit

	if gotSeed != "NAME: X" {
		t.Fatalf("re-edit seed = %q, want the transformed buffer", gotSeed)
	}
}

func TestEditorConfirmCancelInvokesOnCancel(t *testing.T) {
	var canceled bool
	a := resource.Action{Label: "create task", Input: resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil }}
	v := NewEditorConfirmView()
	v.SetContent(a, "name: x", func(string) {}, func(string) {}, func() { canceled = true })

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), nil)

	if !canceled {
		t.Fatal("Esc should invoke onCancel")
	}
}

func TestEditorConfirmScrollKeyReachesContent(t *testing.T) {
	// A 'j' (Down) key is handled by the content pane (row offset changes),
	// proving scroll keys reach the TextView rather than being swallowed —
	// TextView.InputHandler applies 'j'/lineOffset synchronously, unlike
	// 'G'/PgDn which only take effect on the next Draw (no draw happens here).
	a := resource.Action{Label: "create task", Input: resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil }}
	v := NewEditorConfirmView()
	body := strings.Repeat("line\n", 200)
	v.SetContent(a, body, func(string) {}, func(string) {}, func() {})

	handler := v.InputHandler()
	for i := 0; i < 5; i++ {
		handler(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone), nil)
	}

	row, _ := v.content.GetScrollOffset()
	if row == 0 {
		t.Fatal("expected the scroll key to reach the content TextView and move the offset")
	}
}

func TestEditorConfirmTitleIsBracketedAndCapitalized(t *testing.T) {
	if got := editorConfirmTitle(resource.Action{Label: "create task"}); got != "[ Create task ]" {
		t.Fatalf("title = %q, want %q", got, "[ Create task ]")
	}
}

func TestEditorConfirmPerformFailureShowsScrollableCommentedError(t *testing.T) {
	// A 2-row status line can't show a multi-line API error (e.g. an auth
	// failure's full call summary) — it must land in the scrollable content
	// pane instead, prepended as a comment ahead of the original definition.
	a := resource.Action{Label: "create task", Input: resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil }}
	v := NewEditorConfirmView()
	v.SetContent(a, "name: x", func(string) {}, func(string) {}, func() {})

	longErr := "auth failed\nCALL SUMMARY\nmethod: POST\nurl: https://example.com\nretries: 3"
	v.SetStatus(performFailurePrefix+longErr, true)

	if !strings.Contains(v.content.GetText(true), "CALL SUMMARY") || !strings.Contains(v.content.GetText(true), "auth failed") {
		t.Fatalf("expected the full error visible in the content pane, got %q", v.content.GetText(true))
	}
	if !strings.Contains(v.content.GetText(true), "name: x") {
		t.Fatal("expected the original definition to still be shown, below the error comment")
	}
}

func TestEditorConfirmPerformFailureNeverMutatesTheSubmittedBuffer(t *testing.T) {
	// createTaskAction memoizes its retry-idempotency by exact raw text (see
	// resource/create_task.go): a retry with byte-identical input reuses the
	// same taskId rather than minting a new one, which is what makes it safe
	// to resubmit after a request whose response was lost even though the
	// queue actually processed it. Commenting the error into v.buffer itself
	// (rather than only into what's displayed) would silently change that raw
	// text on every failed attempt and defeat that guarantee — a lost-response
	// retry would then create a duplicate task instead of a no-op.
	var gotRaw []string
	a := resource.Action{
		Label:   "create task",
		Input:   resource.InputExternalEditor,
		Perform: func(resource.ActionInput) error { return nil },
	}
	v := NewEditorConfirmView()
	v.SetContent(a, "name: x", func(raw string) { gotRaw = append(gotRaw, raw) }, func(string) {}, func() {})

	handler := v.InputHandler()
	v.SetStatus(performFailurePrefix+"boom (transport blip)", true)
	handler(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone), nil) // retry after the failure
	handler(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone), nil) // and again

	if v.buffer != "name: x" {
		t.Fatalf("v.buffer must stay byte-identical after a Perform failure, got %q", v.buffer)
	}
	for i, raw := range gotRaw {
		if raw != "name: x" {
			t.Fatalf("onConfirm call #%d got %q, want the untouched original buffer every time", i, raw)
		}
	}
}
