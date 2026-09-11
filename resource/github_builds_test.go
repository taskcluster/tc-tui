package resource

import (
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestGithubBuildsResourceScopedListPull(t *testing.T) {
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	fake := &fakeTaskcluster{
		githubBuilds: taskcluster.GithubBuildList{
			{
				Organization:      "myorg",
				Repository:        "myrepo",
				SHA:               "abcdef0123456789abcdef0123456789abcdef01",
				State:             "success",
				TaskGroupID:       "TASKGROUPIDxxxxxxxxxxx",
				EventType:         "pull_request.synchronize",
				PullRequestNumber: 42,
				Created:           tcclient.Time(created),
				Updated:           tcclient.Time(updated),
			},
		},
	}
	res := NewGithubBuildsResource(fake)

	rows, err := res.ScopedList("myorg/myrepo/pull/42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.githubBuildsFilter.Organization != "myorg" || fake.githubBuildsFilter.Repository != "myrepo" || fake.githubBuildsFilter.PullRequest != "42" {
		t.Fatalf("unexpected filter sent: %+v", fake.githubBuildsFilter)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.ID != "TASKGROUPIDxxxxxxxxxxx" {
		t.Fatalf("unexpected row ID: %q", row.ID)
	}
	if row.NavTarget == nil || row.NavTarget.ResourceName != "taskgroup" || row.NavTarget.ID != "TASKGROUPIDxxxxxxxxxxx" || row.NavTarget.Kind != NavScopedList {
		t.Fatalf("unexpected NavTarget: %+v", row.NavTarget)
	}
	if row.Cells[1] != "42" {
		t.Fatalf("expected PR cell %q, got %q", "42", row.Cells[1])
	}
	if row.Cells[2] != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("expected full SHA in cell, got %q", row.Cells[2])
	}
	if !strings.Contains(row.Cells[3], "success") {
		t.Fatalf("expected STATE cell to contain state, got %q", row.Cells[3])
	}
}

// The Builds API always returns oldest-updated-first with no descending
// option (see the type doc comment) — ScopedList must re-sort, or the
// first row for a PR with several pushes would be its OLDEST build.
func TestGithubBuildsResourceScopedListSortsNewestFirst(t *testing.T) {
	older := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	fake := &fakeTaskcluster{
		githubBuilds: taskcluster.GithubBuildList{
			// Deliberately oldest-first, matching the real API's ordering.
			{TaskGroupID: "OLDERTASKGROUPxxxxxxxx", Updated: tcclient.Time(older)},
			{TaskGroupID: "NEWERTASKGROUPxxxxxxxx", Updated: tcclient.Time(newer)},
		},
	}
	res := NewGithubBuildsResource(fake)

	rows, err := res.ScopedList("myorg/myrepo/pull/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "NEWERTASKGROUPxxxxxxxx" {
		t.Fatalf("expected newest build first, got %+v", rows[0])
	}
	if rows[1].ID != "OLDERTASKGROUPxxxxxxxx" {
		t.Fatalf("expected oldest build second, got %+v", rows[1])
	}
}

func TestGithubBuildsResourceScopedListTranslatesDots(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewGithubBuildsResource(fake)

	sha := "abcdef0123456789abcdef0123456789abcdef01"
	if _, err := res.ScopedList("my.org/foo.bar/sha/" + sha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.githubBuildsFilter.Organization != "my%org" {
		t.Fatalf("expected dotted org translated to %%, got %q", fake.githubBuildsFilter.Organization)
	}
	if fake.githubBuildsFilter.Repository != "foo%bar" {
		t.Fatalf("expected dotted repo translated to %%, got %q", fake.githubBuildsFilter.Repository)
	}
	if fake.githubBuildsFilter.SHA != sha {
		t.Fatalf("expected sha %q, got %q", sha, fake.githubBuildsFilter.SHA)
	}
}

func TestGithubBuildsResourceScopedListNormalizesUppercaseSHA(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewGithubBuildsResource(fake)

	if _, err := res.ScopedList("myorg/myrepo/sha/ABCDEF0123456789ABCDEF0123456789ABCDEF01"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "abcdef0123456789abcdef0123456789abcdef01"
	if fake.githubBuildsFilter.SHA != want {
		t.Fatalf("expected normalized lowercase sha %q, got %q", want, fake.githubBuildsFilter.SHA)
	}
}

func TestGithubBuildsResourceScopedListRejectsAbbreviatedSHA(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if _, err := res.ScopedList("myorg/myrepo/sha/abcdef0"); err == nil {
		t.Fatalf("expected error for abbreviated sha")
	}
}

func TestGithubBuildsResourceScopedListRejectsNonHexSHA(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if _, err := res.ScopedList("myorg/myrepo/sha/" + strings.Repeat("g", 40)); err == nil {
		t.Fatalf("expected error for non-hex sha")
	}
}

func TestGithubBuildsResourceScopedListRejectsNonNumericPR(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if _, err := res.ScopedList("myorg/myrepo/pull/notanumber"); err == nil {
		t.Fatalf("expected error for non-numeric pull request")
	}
}

func TestGithubBuildsResourceScopedListRejectsMalformedScope(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	for _, scope := range []string{
		"",
		"myorg",
		"myorg/myrepo",
		"myorg/myrepo/pull",
		"myorg//pull/1",
		"myorg/myrepo/branch/main",
	} {
		if _, err := res.ScopedList(scope); err == nil {
			t.Fatalf("expected error for malformed scope %q", scope)
		}
	}
}

// A '%' in org/repo is Taskcluster's OWN internal '.'-sanitization marker
// (not something a real Github name can contain), and a tview
// region-tag-shaped sequence would corrupt rendering elsewhere — both must
// be rejected locally rather than silently accepted.
func TestGithubBuildsResourceScopedListRejectsInvalidOrgRepoCharacters(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	for _, scope := range []string{
		"my org/myrepo/pull/1",
		"myorg/foo%bar/pull/1",
		"my[red]org[white]/myrepo/pull/1",
	} {
		if _, err := res.ScopedList(scope); err == nil {
			t.Fatalf("expected error for invalid org/repo characters in scope %q", scope)
		}
	}
}

func TestGithubBuildsResourceListReturnsError(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected List() to error — githubbuilds is scope-only")
	}
}

func TestGithubBuildsResourceDescribeIsUnreachable(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if _, err := res.Describe("TASKGROUPIDxxxxxxxxxxx"); err == nil {
		t.Fatalf("expected Describe() to error — builds have no Detail page")
	}
}

func TestGithubBuildsResourceEmptyScopeResource(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "githubrepo" {
		t.Fatalf("expected EmptyScopeResource() = %q, got %q", "githubrepo", got)
	}
}

func TestGithubBuildsResourceListWebURLLinksToPullRequest(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	got := res.ListWebURL("https://tc.example.com", "my.org/foo.bar/pull/42")
	want := "https://github.com/my.org/foo.bar/pull/42"
	if got != want {
		t.Fatalf("ListWebURL = %q, want %q", got, want)
	}
}

func TestGithubBuildsResourceListWebURLLinksToCommit(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	sha := "abcdef0123456789abcdef0123456789abcdef01"
	got := res.ListWebURL("https://tc.example.com", "myorg/myrepo/sha/"+sha)
	want := "https://github.com/myorg/myrepo/commit/" + sha
	if got != want {
		t.Fatalf("ListWebURL = %q, want %q", got, want)
	}
}

func TestGithubBuildsResourceListWebURLEmptyOnMalformedScope(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", "not-a-valid-scope"); got != "" {
		t.Fatalf("expected empty ListWebURL for malformed scope, got %q", got)
	}
}

// ListWebURL is computed by the shell as soon as a scope is typed —
// BEFORE that scope's list has actually been fetched/validated — so it
// must independently reject anything ScopedList would also reject, not
// just anything shaped like 4 non-empty segments.
func TestGithubBuildsResourceListWebURLEmptyOnInvalidPullNumber(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", "myorg/myrepo/pull/not-a-number"); got != "" {
		t.Fatalf("expected empty ListWebURL for non-numeric pull request, got %q", got)
	}
}

func TestGithubBuildsResourceListWebURLEmptyOnAbbreviatedSHA(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", "myorg/myrepo/sha/abc123"); got != "" {
		t.Fatalf("expected empty ListWebURL for abbreviated sha, got %q", got)
	}
}

func TestGithubBuildsResourceListWebURLEmptyOnInvalidOrgRepoCharacters(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", "myorg/foo%bar/pull/1"); got != "" {
		t.Fatalf("expected empty ListWebURL for invalid org/repo characters, got %q", got)
	}
}

func TestGithubBuildsResourceDetailWebURLIsEmpty(t *testing.T) {
	res := NewGithubBuildsResource(&fakeTaskcluster{})

	if got := res.DetailWebURL("https://tc.example.com", "TASKGROUPIDxxxxxxxxxxx"); got != "" {
		t.Fatalf("expected empty DetailWebURL, got %q", got)
	}
}

func TestRenderGithubBuildState(t *testing.T) {
	cases := map[string]string{
		"success":   "green",
		"failure":   "red",
		"error":     "red",
		"pending":   "yellow",
		"cancelled": "white",
		"unknown":   "white",
	}
	for state, color := range cases {
		got := renderGithubBuildState(state)
		if !strings.Contains(got, "["+color+"]") {
			t.Fatalf("state %q: expected color %q in %q", state, color, got)
		}
	}
}

func TestRenderGithubBuildStateEmptyPassesThrough(t *testing.T) {
	if got := renderGithubBuildState(""); got != "" {
		t.Fatalf("expected empty state to pass through unchanged, got %q", got)
	}
}
