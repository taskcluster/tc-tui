package shell

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/taskcluster/tc-tui/resource"
)

// fakeDetailCacheResource is a Detail-only fake with a configurable
// RefreshInterval (needed for detailCache.get, which always misses at
// ttl<=0 — see fakeResource's own default of 0) and a Describe that renders
// a distinct title/body per id, so a test can tell which id's content is on
// screen.
type fakeDetailCacheResource struct {
	fakeResource
	ttl   time.Duration
	calls int
}

func (f *fakeDetailCacheResource) Describe(id string) (resource.Detail, error) {
	f.calls++
	return resource.Detail{Title: fmt.Sprintf("Task %s", id), Body: fmt.Sprintf("body for %s", id)}, nil
}

func (f *fakeDetailCacheResource) RefreshInterval() time.Duration { return f.ttl }

func TestRenderDetailPrimesFromCacheSynchronouslyWithoutLoadingFlash(t *testing.T) {
	registry := resource.NewRegistry()
	res := &fakeDetailCacheResource{fakeResource: fakeResource{name: "task"}, ttl: time.Minute}
	registry.Register(res)
	s := New(registry)

	s.detailCache.set(detailCacheKeyFor(res, "id1"), detailCacheEntry{
		detail:    resource.Detail{Title: "Cached Title", Body: "cached body line", Actions: []resource.DetailAction{{Key: 'w', Label: "worker"}}},
		fetchedAt: time.Now(),
	})

	// renderDetail's cache-hit path runs synchronously on this goroutine,
	// before the background Describe (dispatched into a `go func` that would
	// otherwise block forever on QueueUpdateDraw with no app.Run() pumping
	// it — the same pattern already used by e.g.
	// TestRefreshCurrentTriggersDetailRefetch) ever gets a chance to run.
	s.renderDetail(res, "id1", false)

	if s.currentDetailTitle != "Cached Title" {
		t.Fatalf("expected the cached title to render immediately, got %q", s.currentDetailTitle)
	}
	if len(s.currentDetailActions) != 1 || s.currentDetailActions[0].Key != 'w' {
		t.Fatalf("expected the cached detail's actions to be restored immediately, got %+v", s.currentDetailActions)
	}
	if body := s.detail.GetText(true); !strings.Contains(body, "cached body line") {
		t.Fatalf("expected the detail view to show the cached body immediately, got %q", body)
	}
}

func TestRenderDetailWithoutCacheHitShowsLoadingTitle(t *testing.T) {
	registry := resource.NewRegistry()
	res := &fakeDetailCacheResource{fakeResource: fakeResource{name: "task"}, ttl: time.Minute}
	registry.Register(res)
	s := New(registry)

	s.renderDetail(res, "id1", false)

	if title := s.content.GetTitle(); !strings.Contains(title, "Loading task...") {
		t.Fatalf("expected the plain Loading title on a cache miss, got %q", title)
	}
	if got := s.detail.GetText(true); got != "" {
		t.Fatalf("expected a blank detail view before any fetch has completed, got %q", got)
	}
}

func TestGoBackToPreviouslyVisitedDetailPrimesFromCacheWithoutBlankTitle(t *testing.T) {
	registry := resource.NewRegistry()
	res := &fakeDetailCacheResource{fakeResource: fakeResource{name: "task"}, ttl: time.Minute}
	registry.Register(res)
	s := newRunningTestShell(t, registry)

	// Every call that mutates Shell/tview state, and every read of it, is
	// routed through QueueUpdateDraw — app.Run()'s single event-loop
	// goroutine also redraws on its own (not just in response to a queued
	// update), so touching this state directly from the test goroutine
	// while the app is running is a real, -race-detectable race, not just
	// a theoretical one.
	readTitle := func() string {
		var title string
		s.app.QueueUpdateDraw(func() { title = s.currentDetailTitle })
		return title
	}

	s.app.QueueUpdateDraw(func() { s.showDetail("task", "A") })
	waitFor(t, func() bool { return readTitle() == "Task A" })

	s.app.QueueUpdateDraw(func() { s.showDetail("task", "B") })
	waitFor(t, func() bool { return readTitle() == "Task B" })

	// goBack's cache-hit path (renderDetail's primed branch) redraws
	// synchronously within this same callback, before the background
	// re-fetch it also dispatches gets any chance to run.
	var titleAfterGoBack string
	s.app.QueueUpdateDraw(func() {
		s.goBack()
		titleAfterGoBack = s.currentDetailTitle
	})
	if titleAfterGoBack != "Task A" {
		t.Fatalf("expected Esc back to task A to redraw its cached title immediately (not a blank/Loading flash), got %q", titleAfterGoBack)
	}
}
