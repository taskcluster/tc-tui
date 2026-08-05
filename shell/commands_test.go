package shell

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

// fakeCommandActionResource is a command-only resource (no list, no detail) —
// the `:createtask` shape.
type fakeCommandActionResource struct {
	fakeResource
}

func (f fakeCommandActionResource) CommandAction() resource.Action {
	return resource.Action{
		Label:   "create task",
		Prompt:  "Create a task?",
		Perform: func(resource.ActionInput) error { return nil },
	}
}

// paletteShell builds a Shell with a real CommandsResource over a registry
// holding an ordinary list resource and a command-only one, sitting on the
// list resource's view.
func paletteShell(t *testing.T) *Shell {
	t.Helper()

	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	registry.Register(fakeCommandActionResource{fakeResource{name: "createtask"}})
	registry.Register(resource.NewCommandsResource(registry))

	s := New(registry)
	s.switchResource("wp", "")
	return s
}

func ctrlA() *tcell.EventKey { return tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModNone) }

func TestCtrlAOpensCommandPaletteOverTheCurrentView(t *testing.T) {
	s := paletteShell(t)

	if got := s.globalInputCapture(ctrlA()); got != nil {
		t.Fatalf("expected Ctrl-A to be swallowed, got %#v", got)
	}

	if got := s.stack.Len(); got != 2 {
		t.Fatalf("expected the palette to be pushed over workerpools (len 2), got len %d: %+v", got, s.stack.Views())
	}
	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.ResourceName != resource.CommandsResourceName {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestCtrlADismissesAnAlreadyOpenCommandPalette(t *testing.T) {
	s := paletteShell(t)
	s.globalInputCapture(ctrlA())

	s.globalInputCapture(ctrlA())

	if got := s.stack.Len(); got != 1 {
		t.Fatalf("expected the second Ctrl-A to dismiss the palette (len 1), got len %d: %+v", got, s.stack.Views())
	}
	top, _ := s.stack.Top()
	if top.ResourceName != "workerpools" {
		t.Fatalf("expected to return to workerpools, got %+v", top)
	}
}

func TestReopeningAPeekResourceDoesNotStackASecondCopy(t *testing.T) {
	s := paletteShell(t)

	// `:commands` from within the palette (as opposed to Ctrl-A, which
	// toggles) must not leave two identical views for Esc to pop through.
	s.switchResource(resource.CommandsResourceName, "")
	s.switchResource(resource.CommandsResourceName, "")

	if got := s.stack.Len(); got != 2 {
		t.Fatalf("expected a single palette view over workerpools (len 2), got len %d: %+v", got, s.stack.Views())
	}
}

func TestCtrlAIsANoOpWithoutARegisteredPalette(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	s := New(registry)
	s.switchResource("wp", "")

	s.globalInputCapture(ctrlA())

	if got := s.stack.Len(); got != 1 {
		t.Fatalf("expected no navigation, got len %d: %+v", got, s.stack.Views())
	}
	if name, _ := s.content.GetFrontPage(); name != pageTable {
		t.Fatalf("expected to stay on the list view rather than show an error, got page %q", name)
	}
}

func TestCtrlAPassesThroughToAnActiveFooterInput(t *testing.T) {
	s := paletteShell(t)
	s.openCommandBar()

	event := ctrlA()
	if got := s.globalInputCapture(event); got != event {
		t.Fatalf("expected Ctrl-A to reach the footer input while it's open, got %#v", got)
	}
	if got := s.stack.Len(); got != 1 {
		t.Fatalf("expected no navigation while typing a command, got len %d", got)
	}
}

func TestSelectingAPaletteRowRunsItAsACommand(t *testing.T) {
	s := paletteShell(t)
	s.globalInputCapture(ctrlA())

	// A NavCommand row for an ordinary resource behaves exactly like typing
	// `:workerpools`: the stack resets to it, palette included.
	s.navigateTo(resource.NavTarget{ResourceName: "workerpools", Kind: resource.NavCommand})

	if got := s.stack.Len(); got != 1 {
		t.Fatalf("expected the command to reset the stack (len 1), got len %d: %+v", got, s.stack.Views())
	}
	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.ResourceName != "workerpools" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSelectingACommandOnlyPaletteRowFiresItsActionDirectly(t *testing.T) {
	s := paletteShell(t)
	s.globalInputCapture(ctrlA())

	s.navigateTo(resource.NavTarget{ResourceName: "createtask", Kind: resource.NavCommand})

	if !s.actionOpen {
		t.Fatal("expected a command-only row to open its action dialog")
	}
	if got := s.currentAction.Label; got != "create task" {
		t.Fatalf("unexpected action %q", got)
	}
	// The palette stays on the stack underneath, so cancelling the dialog
	// returns to it rather than to a screen the user never chose.
	top, ok := s.stack.Top()
	if !ok || top.ResourceName != resource.CommandsResourceName {
		t.Fatalf("expected the palette to remain the top view, got %+v (ok=%v)", top, ok)
	}
}

func TestCommandPaletteIsListedInTheHelpAndHeaderHints(t *testing.T) {
	s := paletteShell(t)
	s.renderHeaderHints()

	if got := s.headerHint.GetText(true); !strings.Contains(got, "Ctrl-A commands") {
		t.Fatalf("expected a Ctrl-A hint in the header, got %q", got)
	}
	if got := buildHelpText(s.registry); !strings.Contains(got, "Ctrl-A") || !strings.Contains(got, "command palette") {
		t.Fatalf("expected the help screen to document Ctrl-A, got %q", got)
	}
}

// scopePromptShell registers a scoped resource that prompts for its scope
// (blank falling back to the browsable parent), plus that parent.
func scopePromptShell(t *testing.T, emptyScope string) *Shell {
	t.Helper()

	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	registry.Register(fakeScopePromptResource{
		fakeScopedResource: fakeScopedResource{
			fakeResource: fakeResource{name: "workers", aliases: []string{"w"}},
			emptyScope:   emptyScope,
		},
		promptLabel: "worker pool id",
	})
	registry.Register(resource.NewCommandsResource(registry))

	s := New(registry)
	s.switchResource("wp", "")
	return s
}

func TestEmptyScopeAsksForTheScopeInsteadOfRedirecting(t *testing.T) {
	s := scopePromptShell(t, "workerpools")

	s.switchResource("workers", "")

	if s.footerMode != footerPrompt {
		t.Fatalf("expected a scope prompt, got footer mode %v", s.footerMode)
	}
	// The label has to advertise the blank-submit escape hatch, or the browse
	// path is simply undiscoverable.
	if got := s.footerInput.GetLabel(); !strings.Contains(got, "worker pool id") ||
		!strings.Contains(got, "blank to browse") {
		t.Fatalf("unexpected prompt label %q", got)
	}
	// Nothing navigated yet — the redirect must not have already happened.
	if top, _ := s.stack.Top(); top.ResourceName != "workerpools" || top.Kind != ListKind {
		t.Fatalf("expected to stay put while prompting, got %+v", top)
	}
}

func TestScopePromptOpensTheScopedListForAnEnteredID(t *testing.T) {
	s := scopePromptShell(t, "workerpools")
	s.switchResource("workers", "")

	s.footerInput.SetText("proj-taskcluster/ci")
	s.handleFooterInputDone(tcell.KeyEnter)

	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workers" || top.Kind != ListKind || top.Scope != "proj-taskcluster/ci" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestScopePromptFallsBackToTheParentListOnABlankSubmit(t *testing.T) {
	s := scopePromptShell(t, "workerpools")
	s.switchResource("workers", "")

	s.footerInput.SetText("")
	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerIdle {
		t.Fatalf("expected the blank submit to close the prompt, got mode %v", s.footerMode)
	}
	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" || top.Kind != ListKind {
		t.Fatalf("expected the parent list, got %+v (ok=%v)", top, ok)
	}
}

func TestScopePromptWithNoParentRequiresAValue(t *testing.T) {
	// Nothing to fall back to, so a blank submit must keep the prompt open
	// rather than navigating to an unresolvable empty resource name.
	s := scopePromptShell(t, "")
	s.switchResource("workers", "")

	if got := s.footerInput.GetLabel(); strings.Contains(got, "blank to browse") {
		t.Fatalf("must not advertise a browse fallback that doesn't exist: %q", got)
	}

	s.footerInput.SetText("")
	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerPrompt {
		t.Fatalf("expected the prompt to stay open, got mode %v", s.footerMode)
	}
	if name, _ := s.content.GetFrontPage(); name == pageError {
		t.Fatal("a blank submit must not land on an error screen")
	}
}

func TestOrdinaryIDPromptStillRejectsABlankSubmit(t *testing.T) {
	// The blank-submit escape hatch is opt-in per prompt: `:task` with no id
	// has nothing to browse, so Enter on an empty field is still a no-op.
	registry := resource.NewRegistry()
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"},
		label:        "task id",
	})
	s := New(registry)
	s.switchResource("task", "")

	s.footerInput.SetText("")
	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerPrompt {
		t.Fatalf("expected the id prompt to stay open, got mode %v", s.footerMode)
	}
}

func TestScopedResourceWithoutAPromptStillRedirects(t *testing.T) {
	// ScopePrompt is opt-in; a plain ScopedResource keeps the old behavior.
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	registry.Register(fakeScopedResource{
		fakeResource: fakeResource{name: "workers"},
		emptyScope:   "workerpools",
	})
	s := New(registry)

	s.switchResource("workers", "")

	if s.footerMode != footerIdle {
		t.Fatalf("expected no prompt, got mode %v", s.footerMode)
	}
	if top, _ := s.stack.Top(); top.ResourceName != "workerpools" {
		t.Fatalf("expected the redirect, got %+v", top)
	}
}

func TestSelectingTheQuitPaletteRowQuits(t *testing.T) {
	// The palette lists `quit` because it's a real command; selecting it has to
	// actually quit rather than fall through to "unknown resource".
	s := paletteShell(t)
	stopped := false
	s.onStopForTest = func() { stopped = true }
	s.globalInputCapture(ctrlA())

	s.navigateTo(resource.NavTarget{ResourceName: "quit", Kind: resource.NavCommand})

	if !stopped {
		t.Fatal("expected the quit row to stop the app")
	}
}

func TestSelectingTheHelpPaletteRowOpensHelp(t *testing.T) {
	s := paletteShell(t)
	s.globalInputCapture(ctrlA())

	s.navigateTo(resource.NavTarget{ResourceName: "help", Kind: resource.NavCommand})

	if !s.helpOpen {
		t.Fatal("expected the help row to open the help overlay")
	}
	if name, _ := s.content.GetFrontPage(); name != pageHelp {
		t.Fatalf("expected the help page to be front, got %q", name)
	}
}

func TestBuiltinCommandsAreNotTreatedAsUnknownResources(t *testing.T) {
	for _, name := range []string{"help", "quit", "q"} {
		t.Run(name, func(t *testing.T) {
			s := paletteShell(t)
			s.onStopForTest = func() {}

			s.runCommand(name, "")

			if page, _ := s.content.GetFrontPage(); page == pageError {
				t.Fatalf("%q fell through to the unknown-resource error screen", name)
			}
		})
	}
}

func TestColdStartHelpReturnsToRootOnClose(t *testing.T) {
	// `tc-tui help`: the overlay opens over an EMPTY stack, so without the
	// cold-start guard closeHelp restores the blank page that was underneath
	// and Esc has nothing to pop.
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	s := New(registry)
	s.restoreFallback = "workerpools"

	s.runCommand("help", "")
	s.closeHelp()

	if s.currentListResource != "workerpools" {
		t.Fatalf("expected the root list to be rendered under the help overlay, got %q",
			s.currentListResource)
	}
	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" || top.Kind != ListKind {
		t.Fatalf("expected the root view on the stack, got %+v (ok=%v)", top, ok)
	}
}

func TestColdStartHelpWithARestoredStackRendersItUnderneath(t *testing.T) {
	// The same cold start, but with a session restored by RestoreState. The
	// stack is non-empty yet nothing has been DRAWN — StartAt renders only via
	// the command it queues, and `help` doesn't navigate — so gating on "the
	// stack is empty" misses this and the overlay still opens over a blank page.
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	s := New(registry)
	s.restoreFallback = "workerpools"
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind, Scope: "proj/ci"})

	s.runCommand("help", "")
	s.closeHelp()

	// pageTable is the front page from construction, so asserting on it proves
	// nothing here — what matters is that a list was actually rendered onto it.
	if s.currentListResource != "workerpools" || s.currentListScope != "proj/ci" {
		t.Fatalf("expected the restored list to be rendered under the help overlay, got %q/%q",
			s.currentListResource, s.currentListScope)
	}
	top, ok := s.stack.Top()
	if !ok || top.Scope != "proj/ci" {
		t.Fatalf("expected the restored view to be kept, got %+v (ok=%v)", top, ok)
	}
}

func TestCommandsWithAnArgumentDescribesThatCommand(t *testing.T) {
	// `:commands workers` must reach CommandsResource.Describe rather than
	// being swallowed by the peek branch's empty-scope list render.
	s := paletteShell(t)

	s.switchResource(resource.CommandsResourceName, "workers")

	top, ok := s.stack.Top()
	if !ok || top.Kind != DetailKind || top.ResourceName != resource.CommandsResourceName ||
		top.SelectedID != "workers" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
	// Pushed, not reset — a peek resource never discards the screen below it.
	if got := s.stack.Len(); got != 2 {
		t.Fatalf("expected the detail to be pushed over workerpools (len 2), got len %d: %+v",
			got, s.stack.Views())
	}
}

func TestScopePromptDoesNotOfferBrowsingANonBrowsableFallback(t *testing.T) {
	// artifacts' fallback is `task`, a DirectLookup — submitting blank there
	// would just open a second prompt for the same id.
	registry := resource.NewRegistry()
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"},
		label:        "task id",
	})
	registry.Register(fakeScopePromptResource{
		fakeScopedResource: fakeScopedResource{
			fakeResource: fakeResource{name: "artifacts"},
			emptyScope:   "task",
		},
		promptLabel: "task id",
	})
	s := New(registry)

	s.switchResource("artifacts", "")

	if got := s.footerInput.GetLabel(); strings.Contains(got, "blank to browse") {
		t.Fatalf("must not advertise browsing a prompt-only fallback: %q", got)
	}

	s.footerInput.SetText("")
	s.handleFooterInputDone(tcell.KeyEnter)

	if s.footerMode != footerPrompt {
		t.Fatalf("expected a blank submit to be rejected, got mode %v", s.footerMode)
	}
}

func TestBrowsableFallback(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"}, label: "task id",
	})
	registry.Register(fakeDirectScopedResource{
		fakeScopedResource: fakeScopedResource{fakeResource: fakeResource{name: "taskgroup"}},
		label:              "task group id",
	})
	registry.Register(fakeScopedResource{
		fakeResource: fakeResource{name: "workers"}, emptyScope: "workerpools",
	})
	registry.Register(fakeCommandActionResource{fakeResource{name: "createtask"}})

	tests := []struct {
		name string
		want bool
	}{
		{"workerpools", true}, // a plain list — the only useful blank-submit target
		{"task", false},       // DirectLookup: prompts again
		{"taskgroup", false},  // DirectScopedResource: prompts again
		{"workers", false},    // ScopedResource: prompts or redirects onward
		{"createtask", false}, // CommandAction: runs a mutation, shows nothing
		{"nope", false},       // unregistered
		{"", false},           // no fallback declared
	}

	for _, tt := range tests {
		if _, got := browsableFallbackIn(registry, tt.name); got != tt.want {
			t.Errorf("browsableFallbackIn(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHelpTextOnlyPromisesBrowsingWhereItsPossible(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"}, label: "task id",
	})
	registry.Register(fakeScopePromptResource{
		fakeScopedResource: fakeScopedResource{
			fakeResource: fakeResource{name: "workers"}, emptyScope: "workerpools",
		},
		promptLabel: "worker pool id",
	})
	registry.Register(fakeScopePromptResource{
		fakeScopedResource: fakeScopedResource{
			fakeResource: fakeResource{name: "artifacts"}, emptyScope: "task",
		},
		promptLabel: "task id",
	})

	// Match the per-resource "requires a scope" line, not the global-keys
	// blurb that also happens to mention `:workers`.
	var workers, artifacts string
	for _, line := range strings.Split(buildHelpText(registry), "\n") {
		switch {
		case strings.Contains(line, "`:workers <id>`"):
			workers = line
		case strings.Contains(line, "`:artifacts <id>`"):
			artifacts = line
		}
	}

	if !strings.Contains(workers, "blank to browse `workerpools`") {
		t.Errorf("workers should advertise browsing its parent list: %q", workers)
	}
	if artifacts == "" {
		t.Fatal("no scope line rendered for artifacts")
	}
	if strings.Contains(artifacts, "blank to browse") {
		t.Errorf("artifacts has no browsable parent, so help must not claim otherwise: %q", artifacts)
	}
}
