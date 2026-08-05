package controller

import (
	"strings"
	"testing"

	"github.com/taskcluster/tc-tui/resource"
)

// `tc-tui --help` must never require a configured deployment just to print
// usage: HelpText has to work with no TASKCLUSTER_ROOT_URL in the
// environment (where constructing a real Taskcluster client panics).
func TestHelpTextNeedsNoTaskclusterClient(t *testing.T) {
	t.Setenv("TASKCLUSTER_ROOT_URL", "")

	text := HelpText()

	for _, want := range []string{"workerpools", "hooks", "secrets", "task"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
}

// rootResource is what a session with no restorable history opens, so it has
// to resolve in the same registry the shell is handed — a name that doesn't
// would land a first launch on the unknown-resource error screen. It also has
// to be openable with no argument, or that launch just prompts for one.
func TestRootResourceIsRegisteredAndNeedsNoArgument(t *testing.T) {
	registry := buildRegistry(nil)

	res, ok := registry.Resolve(rootResource)
	if !ok {
		t.Fatalf("root resource %q is not registered", rootResource)
	}
	if _, needsArg := res.(resource.ScopedResource); needsArg {
		t.Errorf("root resource %q is scoped, so a fresh launch would prompt for a scope", rootResource)
	}
	if _, needsArg := res.(resource.DirectLookup); needsArg {
		t.Errorf("root resource %q is a DirectLookup, so a fresh launch would prompt for an id", rootResource)
	}
}

// Every scoped resource must say what its scope is when opened without one —
// either by prompting for it directly (DirectScopedResource) or via
// ScopePrompt. One that does neither silently redirects to some parent list
// instead, which reads as the command having been ignored.
func TestEveryScopedResourcePromptsForItsScope(t *testing.T) {
	registry := buildRegistry(nil)

	for _, name := range registry.Names() {
		res, _ := registry.Resolve(name)
		if _, isScoped := res.(resource.ScopedResource); !isScoped {
			continue
		}
		if _, isDirect := res.(resource.DirectScopedResource); isDirect {
			continue // prompts for its scope by its own route
		}

		prompt, ok := res.(resource.ScopePrompt)
		if !ok {
			t.Errorf("%s is a ScopedResource but implements neither DirectScopedResource "+
				"nor ScopePrompt — a bare `:%s` would silently redirect", name, name)
			continue
		}
		if strings.TrimSpace(prompt.ScopePromptLabel()) == "" {
			t.Errorf("%s: empty ScopePromptLabel", name)
		}
	}
}

// An empty-scope fallback must name a resource that actually exists, and never
// itself — the shell doesn't guard against a redirect cycle.
func TestEmptyScopeFallbacksResolveAndDoNotSelfReference(t *testing.T) {
	registry := buildRegistry(nil)

	for _, name := range registry.Names() {
		res, _ := registry.Resolve(name)
		scoped, isScoped := res.(resource.ScopedResource)
		if !isScoped {
			continue
		}

		fallback := scoped.EmptyScopeResource()
		if fallback == "" {
			continue // no parent to browse; the prompt requires a value
		}
		if fallback == name {
			t.Errorf("%s: EmptyScopeResource points at itself", name)
		}
		if _, ok := registry.Resolve(fallback); !ok {
			t.Errorf("%s: EmptyScopeResource %q is not registered", name, fallback)
		}
	}
}
