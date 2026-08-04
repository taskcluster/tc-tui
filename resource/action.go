package resource

import (
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

// InputMode describes what an Action collects from the user before it runs.
type InputMode int

const (
	// InputNone is confirm-only: the action takes no user-entered value, so
	// the shell shows just a confirmation (a plain prompt, or a stronger
	// destructive warning). This is the zero value.
	InputNone InputMode = iota
	// InputLine collects a single-line value (e.g. a quarantine reason, a new
	// task priority) in an ordinary input field.
	InputLine
	// InputText collects free-form multi-line text with no structural
	// parsing, in a text area.
	InputText
	// InputYAML collects multi-line YAML, parsed and structurally validated
	// before Perform runs. ActionInput.Value carries the parsed structure.
	InputYAML
	// InputJSON collects multi-line JSON, parsed and structurally validated
	// before Perform runs. ActionInput.Value carries the parsed structure.
	InputJSON
	// InputExternalEditor collects its value by handing the user off to
	// $EDITOR (k9s-style) rather than an in-TUI field — the shell owns the
	// handoff and renders a dedicated read-only confirm screen instead of the
	// ActionView form. ActionInput.Raw carries the buffer verbatim; there is
	// no structural parse (an action's own Validate does that).
	InputExternalEditor
)

// Multiline reports whether mode is collected in a multi-line text area
// rather than a single-line input field. InputExternalEditor is deliberately
// excluded — it is not an in-TUI text area at all.
func (m InputMode) Multiline() bool {
	return m == InputText || m == InputYAML || m == InputJSON
}

// ActionInput carries what the user entered for an Action after the shell has
// parsed it according to the Action's InputMode.
type ActionInput struct {
	// Raw is exactly what the user typed, untouched.
	Raw string
	// Value is the parsed structure for InputYAML/InputJSON — the result of
	// unmarshalling Raw into an interface{} (a map, slice, or scalar). It is
	// nil for InputNone/InputLine/InputText, where Raw is the value, and nil
	// for an optional YAML/JSON input left blank.
	Value interface{}
}

// Action is a mutating, authenticated operation a Resource exposes on an
// entity — the write-side counterpart to DetailAction's navigation. A
// resource declares its actions via Actionable; the shell drives a single
// shared flow for all of them: optional input entry, structural + field
// validation, a confirmation (stronger for a destructive action), progress
// indication while it runs, then Perform, then cache invalidation and
// success/error surfacing. This keeps every future mutation (cancel a task,
// delete a secret, quarantine a worker, ...) consistent and spares each
// resource from re-implementing the UI plumbing.
type Action struct {
	// Key is the rune that triggers the action from the entity's view. It
	// must avoid the shell's global keys (q, r, o, s, x, n, L, :, /, ?) and
	// any DetailAction key exposed on the same view, or it will be shadowed.
	Key rune
	// Label is the short human description shown in the header hints and the
	// confirmation dialog title, e.g. "cancel task".
	Label string
	// Destructive marks an irreversible or dangerous action (delete,
	// terminate, cancel). The shell warns in a stronger, red style and — when
	// ConfirmWord is set — requires the user to type that word before the
	// confirm button does anything.
	Destructive bool

	// RefreshAfter forces the shell to re-fetch the launching view after a
	// successful Perform even when Destructive is true. A destructive action
	// normally skips that refresh, because "destructive" has meant "delete" and
	// re-Describing a deleted entity would 404. Set this for a destructive
	// action that resolves-but-does-not-remove its entity (e.g. cancel a task),
	// so the fresh state and re-gated actions show immediately. Ignored for a
	// non-destructive action, which always refreshes.
	RefreshAfter bool

	// Prompt is the confirmation question shown before Perform runs, e.g.
	// "Cancel task abc123? Running work will be stopped." Required.
	Prompt string
	// ConfirmWord, if non-empty, must be typed verbatim to enable a
	// destructive action — a deliberate speed-bump against an accidental
	// keystroke on something irreversible. Ignored when Destructive is false.
	ConfirmWord string

	// Input describes the value the action collects; the zero value
	// (InputNone) is confirm-only.
	Input InputMode
	// InputLabel labels the input field, e.g. "reason" or "task definition".
	// Defaults to "value" when empty.
	InputLabel string
	// InitialText prefills the input field — the current value for an edit, a
	// template for a create. Ignored when Input is InputNone.
	InitialText string
	// InputHistory optionally holds earlier values, newest first, that the
	// user can cycle back through in a multi-line input (Ctrl-P for older,
	// Ctrl-N for newer) to reuse or tweak one — e.g. recently submitted task
	// definitions. Ignored for a single-line or confirm-only action.
	InputHistory []string
	// OptionalInput allows the collected value to be left blank. By default an
	// action with an Input requires a non-empty value.
	OptionalInput bool

	// Transforms are named, on-demand rewrites offered on the
	// InputExternalEditor confirm screen (e.g. "update timestamps") — each is
	// bound to a key the confirm view dispatches, applying the rewrite to the
	// current buffer and re-validating. Ignored for any other Input mode.
	Transforms []BufferTransform

	// Validate optionally checks the parsed input beyond structural
	// well-formedness — required fields, value ranges, schema conformance. It
	// runs only after a successful parse (so an InputYAML/InputJSON Validate
	// can trust ActionInput.Value is populated) and its returned error is
	// shown to the user, who corrects the input and retries. nil means "no
	// extra validation".
	Validate func(ActionInput) error

	// Perform executes the mutation. It runs off the UI thread; a nil return
	// is success, and any error is surfaced with the action left open to
	// retry or cancel. Required.
	Perform func(ActionInput) error

	// Invalidates lists resource names whose cached list views should be
	// dropped after a successful Perform so they re-fetch fresh data (e.g. a
	// "delete secret" invalidates "secrets"). The view the action was
	// launched from is always refreshed regardless; this is for OTHER views
	// the mutation affects.
	Invalidates []string

	// Next optionally returns where to navigate after a successful Perform —
	// e.g. the detail view of a newly created entity. It runs on the UI
	// thread once caches are invalidated and the dialog is closed; returning
	// ok=false navigates nowhere and keeps the default behavior (a
	// non-destructive action refreshes the view it was launched from).
	// Perform typically stashes the new entity's id in a captured variable
	// that Next then reads, so a fresh Action must be built per dialog.
	Next func() (NavTarget, bool)
}

// CommandAction is implemented by a resource with no list/detail view of its
// own that instead runs a single action immediately when invoked from the
// command bar (e.g. `:createtask`). The shell dispatches CommandAction()
// rather than rendering a List.
type CommandAction interface {
	Resource
	CommandAction() Action
}

// BufferTransform is a named, on-demand rewrite of an InputExternalEditor
// action's current buffer, offered as a hotkey on the confirm screen (e.g.
// "update timestamps" rebasing a stale task definition's created/deadline/
// expires to now). Apply returns the rewritten buffer, or an error to show
// instead of applying it.
type BufferTransform struct {
	Key   rune
	Label string
	Apply func(raw string) (string, error)
}

// Actionable is implemented by a Resource that exposes mutating,
// authenticated operations on its entities — the write-side counterpart to
// WebLinkable/Downloadable. The shell renders each returned Action's key in
// the header hints (on a Detail view) and drives the shared action flow when
// its key is pressed.
type Actionable interface {
	Resource
	// Actions returns the mutating actions currently valid for id. It may
	// return different actions depending on the entity's state (e.g. only a
	// running task offers "cancel"); returning nil/empty is fine and simply
	// exposes no action keys.
	Actions(id string) []Action
}

// ParseActionInput builds an ActionInput from raw text according to mode,
// enforcing structural validity: a required value must be non-empty, and
// InputYAML/InputJSON must parse. The returned error is human-readable and
// suitable for showing directly to the user. required is what the shell
// derives from Action.OptionalInput (required == !OptionalInput).
func ParseActionInput(mode InputMode, raw string, required bool) (ActionInput, error) {
	input := ActionInput{Raw: raw}
	empty := strings.TrimSpace(raw) == ""

	if mode == InputNone {
		return input, nil
	}

	if empty {
		if required {
			return input, fmt.Errorf("a value is required")
		}
		return input, nil
	}

	switch mode {
	case InputLine, InputText:
		return input, nil
	case InputJSON:
		var v interface{}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return input, fmt.Errorf("invalid JSON: %w", err)
		}
		input.Value = v
	case InputYAML:
		var v interface{}
		if err := yaml.Unmarshal([]byte(raw), &v); err != nil {
			return input, fmt.Errorf("invalid YAML: %w", err)
		}
		input.Value = v
	case InputExternalEditor:
		return input, nil
	default:
		return input, fmt.Errorf("unknown input mode")
	}

	return input, nil
}
