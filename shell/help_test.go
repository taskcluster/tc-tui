package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/taskcluster/tc-tui/resource"
)

type fakeResource struct {
	name        string
	aliases     []string
	description string
	columns     []resource.Column
}

func (f fakeResource) Name() string                                { return f.name }
func (f fakeResource) Aliases() []string                           { return f.aliases }
func (f fakeResource) Description() string                         { return f.description }
func (f fakeResource) Columns() []resource.Column                  { return f.columns }
func (f fakeResource) List() ([]resource.Row, error)               { return nil, nil }
func (f fakeResource) Describe(id string) (resource.Detail, error) { return resource.Detail{}, nil }
func (f fakeResource) RefreshInterval() time.Duration              { return 0 }

type fakeScopedResource struct {
	fakeResource
	emptyScope string
}

func (f fakeScopedResource) ScopedList(scope string) ([]resource.Row, error) { return nil, nil }
func (f fakeScopedResource) EmptyScopeResource() string                      { return f.emptyScope }

type fakeScopePromptResource struct {
	fakeScopedResource
	promptLabel string
}

func (f fakeScopePromptResource) ScopePromptLabel() string { return f.promptLabel }

type fakePeekResource struct {
	fakeResource
}

func (f fakePeekResource) IsPeek() {}

type fakeDirectLookupResource struct {
	fakeResource
	label string
}

func (f fakeDirectLookupResource) IDPromptLabel() string { return f.label }

type fakeDirectScopedResource struct {
	fakeScopedResource
	label string
}

func (f fakeDirectScopedResource) IDPromptLabel() string { return f.label }

type fakeScopeSubtitleResource struct {
	fakeScopedResource
	subtitle    string
	subtitleErr error
}

func (f fakeScopeSubtitleResource) Subtitle(scope string) (string, error) {
	return f.subtitle, f.subtitleErr
}

type fakeFacetedHelpResource struct {
	fakeResource
}

func (f fakeFacetedHelpResource) FacetColumn() int                          { return 0 }
func (f fakeFacetedHelpResource) FacetOptions(rows []resource.Row) []string { return nil }

type fakeServerFacetedHelpResource struct {
	fakeResource
	options []string
}

func (f fakeServerFacetedHelpResource) FacetOptions() []string { return f.options }
func (f fakeServerFacetedHelpResource) FacetList(scope, value string) ([]resource.Row, error) {
	return nil, nil
}
func (f fakeServerFacetedHelpResource) FacetCounts(scope string) (map[string]int, error) {
	return nil, nil
}

func TestBuildHelpTextListsGlobalKeys(t *testing.T) {
	text := buildHelpText(resource.NewRegistry())

	for _, want := range []string{"q", ":", "/", "Esc", "?"} {
		if !strings.Contains(text, want) {
			t.Errorf("buildHelpText() missing global key %q\ngot:\n%s", want, text)
		}
	}
}

func TestBuildHelpTextExplainsSorting(t *testing.T) {
	text := buildHelpText(resource.NewRegistry())

	for _, want := range []string{"1-9", "sort", "reverse"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("buildHelpText() missing sorting explanation containing %q\ngot:\n%s", want, text)
		}
	}
}

func TestBuildHelpTextListsPlainResource(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{
		name:        "roles",
		aliases:     []string{"role"},
		description: "IAM-style roles and the scopes they grant",
		columns:     []resource.Column{{Title: "ROLE ID"}},
	})

	text := buildHelpText(registry)

	for _, want := range []string{"roles", "role", "IAM-style roles and the scopes they grant", "ROLE ID"} {
		if !strings.Contains(text, want) {
			t.Errorf("buildHelpText() missing %q\ngot:\n%s", want, text)
		}
	}
}

func TestBuildHelpTextFlagsScopedResource(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeScopedResource{
		fakeResource: fakeResource{
			name:        "workers",
			aliases:     []string{"w"},
			description: "Individual workers within a worker pool (scoped list)",
			columns:     []resource.Column{{Title: "STATE"}},
		},
		emptyScope: "workerpools",
	})

	text := buildHelpText(registry)

	for _, want := range []string{"workers", "requires a scope", "workerpools"} {
		if !strings.Contains(text, want) {
			t.Errorf("buildHelpText() missing %q\ngot:\n%s", want, text)
		}
	}
}

func TestBuildHelpTextFlagsDirectLookupResource(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{
			name:        "task",
			description: "A single task, looked up directly by task ID",
		},
		label: "task id",
	})

	text := buildHelpText(registry)

	for _, want := range []string{"task", "requires an id", "opens a prompt", "task id"} {
		if !strings.Contains(text, want) {
			t.Errorf("buildHelpText() missing %q\ngot:\n%s", want, text)
		}
	}
	if strings.Contains(text, "columns:") {
		t.Errorf("buildHelpText() should omit columns for a DirectLookup resource\ngot:\n%s", text)
	}
}

func TestBuildHelpTextExplainsTabCycling(t *testing.T) {
	text := buildHelpText(resource.NewRegistry())

	for _, want := range []string{"Tab", "Shift+Tab"} {
		if !strings.Contains(text, want) {
			t.Errorf("buildHelpText() missing %q\ngot:\n%s", want, text)
		}
	}
}

func TestBuildHelpTextFlagsClientFacetedResource(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeFacetedHelpResource{
		fakeResource: fakeResource{name: "workerpools", description: "pools"},
	})

	text := buildHelpText(registry)

	if !strings.Contains(text, "tabs by provider") {
		t.Errorf("buildHelpText() missing facet note for workerpools\ngot:\n%s", text)
	}
}

func TestBuildHelpTextFlagsServerFacetedResourceWithItsOptions(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(&fakeServerFacetedHelpResource{
		fakeResource: fakeResource{name: "workers", description: "workers"},
		options:      []string{"running", "stopped"},
	})

	text := buildHelpText(registry)

	for _, want := range []string{"tabs:", "running", "stopped"} {
		if !strings.Contains(text, want) {
			t.Errorf("buildHelpText() missing %q\ngot:\n%s", want, text)
		}
	}
}
