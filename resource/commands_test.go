package resource

import (
	"strings"
	"testing"
)

// These cover the routing shapes commandArgument distinguishes; stubResource
// itself (registry_test.go) is the plain-list case.
type stubScopedResource struct {
	stubResource
	emptyScope string
}

func (s stubScopedResource) ScopedList(string) ([]Row, error) { return nil, nil }
func (s stubScopedResource) EmptyScopeResource() string       { return s.emptyScope }

type stubScopePromptResource struct {
	stubScopedResource
	promptLabel string
}

func (s stubScopePromptResource) ScopePromptLabel() string { return s.promptLabel }

type stubDirectLookupResource struct {
	stubResource
	label string
}

func (s stubDirectLookupResource) IDPromptLabel() string { return s.label }

type stubDirectScopedResource struct {
	stubScopedResource
	label string
}

func (s stubDirectScopedResource) IDPromptLabel() string { return s.label }

type stubRootBrowsableResource struct {
	stubDirectScopedResource
}

func (s stubRootBrowsableResource) BrowsesRoot() {}

type stubCommandActionResource struct {
	stubResource
}

func (s stubCommandActionResource) CommandAction() Action { return Action{Label: "create task"} }

func newTestCommandsRegistry() *Registry {
	r := NewRegistry()
	r.Register(stubResource{name: "workerpools", aliases: []string{"wp", "pools"}})
	r.Register(stubScopedResource{
		stubResource: stubResource{name: "workers", aliases: []string{"w"}},
		emptyScope:   "workerpools",
	})
	r.Register(stubDirectLookupResource{
		stubResource: stubResource{name: "task", aliases: []string{"t"}},
		label:        "task id",
	})
	r.Register(stubDirectScopedResource{
		stubScopedResource: stubScopedResource{
			stubResource: stubResource{name: "taskgroup", aliases: []string{"g"}},
			emptyScope:   "tasks",
		},
		label: "task group id",
	})
	r.Register(stubCommandActionResource{stubResource{name: "createtask", aliases: []string{"newtask"}}})
	r.Register(NewCommandsResource(r))
	return r
}

// rowByID finds a palette row by the command it represents.
func rowByID(t *testing.T, rows []Row, id string) Row {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("no row for %q in %+v", id, rows)
	return Row{}
}

func TestCommandsListCoversEveryRegisteredCommand(t *testing.T) {
	registry := newTestCommandsRegistry()
	rows, err := NewCommandsResource(registry).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got, want := len(rows), len(registry.Names())+len(BuiltinCommands()); got != want {
		t.Fatalf("expected one row per registered resource plus one per builtin (%d), got %d", want, got)
	}
	for _, name := range registry.Names() {
		rowByID(t, rows, name) // fails the test if missing
	}
}

// The palette claims to list "every available command", so the shell's own
// non-Resource commands have to be in it too — they're just as typeable after
// `:` as any resource.
func TestCommandsListIncludesTheShellBuiltins(t *testing.T) {
	rows, err := NewCommandsResource(newTestCommandsRegistry()).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	quit := rowByID(t, rows, "quit")
	if got := quit.Cells[1]; got != "q" {
		t.Fatalf("expected quit's alias cell to be %q, got %q", "q", got)
	}
	if got := quit.Cells[2]; got != runsAtOnceArgument {
		t.Fatalf("expected quit to take no argument, got %q", got)
	}
	// Selection has to dispatch like any other command, or the row lies.
	if quit.NavTarget == nil || quit.NavTarget.Kind != NavCommand || quit.NavTarget.ResourceName != "quit" {
		t.Fatalf("unexpected quit NavTarget: %+v", quit.NavTarget)
	}

	rowByID(t, rows, "help")
}

func TestResolveBuiltin(t *testing.T) {
	tests := []struct {
		input string
		want  string // "" = not a builtin
	}{
		{"help", "help"},
		{"HELP", "help"},
		{"quit", "quit"},
		{"q", "quit"},
		{"Q", "quit"},
		{" q ", "quit"},
		{"", ""},
		{"workerpools", ""},
		{"qu", ""},
	}

	for _, tt := range tests {
		cmd, ok := ResolveBuiltin(tt.input)
		if tt.want == "" {
			if ok {
				t.Errorf("ResolveBuiltin(%q) = %q, want no match", tt.input, cmd.Name)
			}
			continue
		}
		if !ok || cmd.Name != tt.want {
			t.Errorf("ResolveBuiltin(%q) = %q/%v, want %q", tt.input, cmd.Name, ok, tt.want)
		}
	}
}

func TestCommandsListIsSortedAlphabetically(t *testing.T) {
	rows, err := NewCommandsResource(newTestCommandsRegistry()).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for i := 1; i < len(rows); i++ {
		if rows[i-1].ID > rows[i].ID {
			t.Fatalf("rows are not alphabetical: %q before %q", rows[i-1].ID, rows[i].ID)
		}
	}
}

func TestCommandsRowsCarryAliasesAndDescription(t *testing.T) {
	rows, err := NewCommandsResource(newTestCommandsRegistry()).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	row := rowByID(t, rows, "workerpools")
	if got := row.Cells[0]; got != "workerpools" {
		t.Fatalf("expected the command name in the first cell, got %q", got)
	}
	// Aliases must be in a cell, not just the name column: the shell's '/'
	// filter matches on cell text, so `wp` has to find this row.
	if got := row.Cells[1]; got != "wp, pools" {
		t.Fatalf("expected aliases cell %q, got %q", "wp, pools", got)
	}
}

func TestCommandsRowsNavigateAsCommands(t *testing.T) {
	rows, err := NewCommandsResource(newTestCommandsRegistry()).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Every row — including a command-only one, which has no list or detail
	// view to navigate to — must dispatch through NavCommand so the shell runs
	// it rather than opening a fake empty resource.
	for _, name := range []string{"workerpools", "workers", "task", "taskgroup", "createtask"} {
		row := rowByID(t, rows, name)
		if row.NavTarget == nil {
			t.Fatalf("%s row has no NavTarget", name)
		}
		if row.NavTarget.Kind != NavCommand || row.NavTarget.ResourceName != name {
			t.Fatalf("%s: unexpected NavTarget %+v", name, *row.NavTarget)
		}
	}
}

func TestCommandArgumentDescribesWhatEachKindTakes(t *testing.T) {
	tests := []struct {
		name string
		res  Resource
		want string
	}{
		{"plain list", stubResource{name: "workerpools"}, ""},
		{
			"scoped",
			stubScopedResource{stubResource: stubResource{name: "workers"}, emptyScope: "workerpools"},
			"<id>",
		},
		{
			"direct lookup",
			stubDirectLookupResource{stubResource: stubResource{name: "task"}, label: "task id"},
			"<task id>",
		},
		{
			// Also satisfies ScopedResource, so this pins the type-switch
			// order: the more specific case must win.
			"direct scoped",
			stubDirectScopedResource{
				stubScopedResource: stubScopedResource{stubResource: stubResource{name: "taskgroup"}},
				label:              "task group id",
			},
			"<task group id>",
		},
		{
			// A DirectScopedResource too, but one that opens on a root list
			// with no argument — shown in brackets to say so.
			"root browsable",
			stubRootBrowsableResource{stubDirectScopedResource{
				stubScopedResource: stubScopedResource{stubResource: stubResource{name: "index"}},
				label:              "namespace or full index path",
			}},
			"[namespace or full index path]",
		},
		{
			// Ditto — the palette must show what the scope actually is, not
			// the generic "<id>".
			"scope prompt",
			stubScopePromptResource{
				stubScopedResource: stubScopedResource{stubResource: stubResource{name: "workers"}},
				promptLabel:        "worker pool id",
			},
			"<worker pool id>",
		},
		{"command only", stubCommandActionResource{stubResource{name: "createtask"}}, "(runs at once)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandArgument(tt.res); got != tt.want {
				t.Fatalf("commandArgument = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandsDescribeKnownAndUnknown(t *testing.T) {
	r := NewCommandsResource(newTestCommandsRegistry())

	detail, err := r.Describe("task")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if detail.Title != "task" {
		t.Fatalf("unexpected title %q", detail.Title)
	}
	if !strings.Contains(detail.Body, "<task id>") {
		t.Fatalf("expected the argument in the body, got %q", detail.Body)
	}

	// A builtin isn't in the registry, but `:commands quit` still describes it.
	detail, err = r.Describe("quit")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if detail.Title != "quit" || !strings.Contains(detail.Body, runsAtOnceArgument) {
		t.Fatalf("unexpected detail %+v", detail)
	}

	if _, err := r.Describe("nope"); err == nil {
		t.Fatal("expected an error describing an unregistered command")
	}
}

// The shell pushes a peek resource onto the view stack rather than resetting
// to it, so Esc returns to the screen it was opened over.
func TestPeekResources(t *testing.T) {
	for _, res := range []Resource{NewCommandsResource(NewRegistry()), NewHistoryResource()} {
		if _, ok := res.(PeekResource); !ok {
			t.Errorf("%s must implement PeekResource", res.Name())
		}
	}
}
