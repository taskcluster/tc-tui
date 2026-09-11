package taskcluster

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcgithub"
)

func TestGetGithubBuildsSendsFilterToCorrectQueryParams(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"builds": []}`))
	}))
	defer server.Close()

	tc := &TC{github: tcgithub.New(nil, server.URL)}
	_, err := tc.GetGithubBuilds(GithubBuildFilter{
		Organization: "myorg",
		Repository:   "myrepo",
		PullRequest:  "42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Get("organization") != "myorg" {
		t.Fatalf("organization = %q, want %q", got.Get("organization"), "myorg")
	}
	if got.Get("repository") != "myrepo" {
		t.Fatalf("repository = %q, want %q", got.Get("repository"), "myrepo")
	}
	if got.Get("pullRequest") != "42" {
		t.Fatalf("pullRequest = %q, want %q", got.Get("pullRequest"), "42")
	}
	if got.Get("sha") != "" {
		t.Fatalf("sha = %q, want empty", got.Get("sha"))
	}
}

func TestGetGithubBuildsSHAFilter(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"builds": []}`))
	}))
	defer server.Close()

	tc := &TC{github: tcgithub.New(nil, server.URL)}
	_, err := tc.GetGithubBuilds(GithubBuildFilter{
		Organization: "myorg",
		Repository:   "myrepo",
		SHA:          "abc123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Get("sha") != "abc123" {
		t.Fatalf("sha = %q, want %q", got.Get("sha"), "abc123")
	}
	if got.Get("pullRequest") != "" {
		t.Fatalf("pullRequest = %q, want empty", got.Get("pullRequest"))
	}
}

func TestGetGithubBuildsPagesUntilContinuationTokenEmpty(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Write([]byte(`{"builds": [{"organization":"myorg","repository":"myrepo","sha":"a","taskGroupId":"tg1"}], "continuationToken":"next"}`))
			return
		}
		w.Write([]byte(`{"builds": [{"organization":"myorg","repository":"myrepo","sha":"b","taskGroupId":"tg2"}]}`))
	}))
	defer server.Close()

	tc := &TC{github: tcgithub.New(nil, server.URL)}
	builds, err := tc.GetGithubBuilds(GithubBuildFilter{Organization: "myorg", Repository: "myrepo", PullRequest: "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("expected 2 builds across 2 pages, got %d", len(builds))
	}
	if calls != 2 {
		t.Fatalf("expected 2 requests, got %d", calls)
	}
}

func TestGetGithubRepositoryDoesNotTranslateDots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/github/v1/repository/myorg/my.repo" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"installed": true}`))
	}))
	defer server.Close()

	tc := &TC{github: tcgithub.New(nil, server.URL)}
	resp, err := tc.GetGithubRepository("myorg", "my.repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Installed {
		t.Fatalf("expected Installed=true")
	}
}
