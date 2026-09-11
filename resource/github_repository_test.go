package resource

import (
	"strings"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcgithub"
)

func TestGithubRepositoryResourceDescribeInstalled(t *testing.T) {
	fake := &fakeTaskcluster{githubRepository: &tcgithub.RepositoryResponse{Installed: true}}
	res := NewGithubRepositoryResource(fake)

	detail, err := res.Describe("myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.githubRepositoryOwner != "myorg" || fake.githubRepositoryRepo != "myrepo" {
		t.Fatalf("unexpected owner/repo sent: %q/%q", fake.githubRepositoryOwner, fake.githubRepositoryRepo)
	}
	if !strings.Contains(stripRegionTags(detail.Body), "true") {
		t.Fatalf("expected body to report installed=true, got: %s", detail.Body)
	}
	if !strings.Contains(strings.ToLower(detail.Body), "experimental") {
		t.Fatalf("expected body to caveat the experimental endpoint, got: %s", detail.Body)
	}
	if len(detail.Actions) != 0 {
		t.Fatalf("expected no DetailActions, got %+v", detail.Actions)
	}
}

func TestGithubRepositoryResourceDescribeNotInstalled(t *testing.T) {
	fake := &fakeTaskcluster{githubRepository: &tcgithub.RepositoryResponse{Installed: false}}
	res := NewGithubRepositoryResource(fake)

	detail, err := res.Describe("myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stripRegionTags(detail.Body), "false") {
		t.Fatalf("expected body to report installed=false, got: %s", detail.Body)
	}
}

func TestGithubRepositoryResourceDescribeDoesNotTranslateDots(t *testing.T) {
	fake := &fakeTaskcluster{githubRepository: &tcgithub.RepositoryResponse{Installed: true}}
	res := NewGithubRepositoryResource(fake)

	if _, err := res.Describe("myorg/foo.bar"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.githubRepositoryRepo != "foo.bar" {
		t.Fatalf("expected repo passed through untranslated, got %q", fake.githubRepositoryRepo)
	}
}

func TestGithubRepositoryResourceDescribeMalformedID(t *testing.T) {
	res := NewGithubRepositoryResource(&fakeTaskcluster{})

	for _, id := range []string{"", "myorg", "myorg/myrepo/extra", "/myrepo", "myorg/"} {
		if _, err := res.Describe(id); err == nil {
			t.Fatalf("expected error for malformed id %q", id)
		}
	}
}

// A repo/org name containing characters Github names can't have (a space,
// a literal '%', or a tview region-tag-shaped sequence like "[red]") must
// be rejected rather than silently accepted: '%' is Taskcluster's OWN
// internal '.'-sanitization marker, so passing it through could
// accidentally address a completely different repo's stored record, and
// unescaped "[...]" would be rendered as a real tview color tag in
// Describe's body rather than as plain text.
func TestGithubRepositoryResourceDescribeRejectsInvalidCharacters(t *testing.T) {
	res := NewGithubRepositoryResource(&fakeTaskcluster{})

	for _, id := range []string{"my org/myrepo", "myorg/foo%bar", "my[red]org[white]/myrepo"} {
		if _, err := res.Describe(id); err == nil {
			t.Fatalf("expected error for invalid characters in id %q", id)
		}
	}
}

func TestGithubRepositoryResourceDetailWebURLReturnsGithubURL(t *testing.T) {
	res := NewGithubRepositoryResource(&fakeTaskcluster{})

	got := res.DetailWebURL("https://tc.example.com", "myorg/myrepo")
	want := "https://github.com/myorg/myrepo"
	if got != want {
		t.Fatalf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestGithubRepositoryResourceDetailWebURLEmptyOnMalformedID(t *testing.T) {
	res := NewGithubRepositoryResource(&fakeTaskcluster{})

	for _, id := range []string{"not-valid", "my org/my repo", "myorg/foo%bar"} {
		if got := res.DetailWebURL("https://tc.example.com", id); got != "" {
			t.Fatalf("expected empty DetailWebURL for malformed id %q, got %q", id, got)
		}
	}
}

func TestGithubRepositoryResourceListWebURLIsEmpty(t *testing.T) {
	res := NewGithubRepositoryResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", ""); got != "" {
		t.Fatalf("expected empty ListWebURL, got %q", got)
	}
}

func TestGithubRepositoryResourceListReturnsError(t *testing.T) {
	res := NewGithubRepositoryResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected List() to error — githubrepo is a DirectLookup")
	}
}
