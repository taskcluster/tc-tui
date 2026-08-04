package shell

import (
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

// partialCall records one ListPartial invocation.
type partialCall struct {
	scope   string
	facet   string
	loadAll bool
}

// fakePartialResource is a PartialLister whose capped fetch always reports
// more rows remaining; a loadAll fetch reports complete.
type fakePartialResource struct {
	fakeResource
	rows []resource.Row

	mu    sync.Mutex
	calls []partialCall
}

func (f *fakePartialResource) ListPartial(scope, facetValue string, loadAll bool) ([]resource.Row, bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, partialCall{scope: scope, facet: facetValue, loadAll: loadAll})
	f.mu.Unlock()

	return f.rows, !loadAll, nil
}

func (f *fakePartialResource) recordedCalls() []partialCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]partialCall(nil), f.calls...)
}

func newPartialTestShell(t *testing.T) (*Shell, *fakePartialResource) {
	t.Helper()

	res := &fakePartialResource{
		fakeResource: fakeResource{name: "widgets", columns: []resource.Column{{Title: "ID"}}},
		rows: []resource.Row{
			{ID: "a", Cells: []string{"alpha"}},
			{ID: "b", Cells: []string{"beta"}},
		},
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	return s, res
}

// onEventLoop runs fn on the tview event loop and waits for it — the safe
// way for a test goroutine to read Shell state a running app mutates.
func onEventLoop(s *Shell, fn func()) {
	s.app.QueueUpdateDraw(fn)
}

func TestRefreshTableShowsTruncatedRowCountInTitle(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.lastRows = []resource.Row{{ID: "a", Cells: []string{"alpha"}}, {ID: "b", Cells: []string{"beta"}}}
	s.currentListTruncated = true

	s.refreshTable()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2+ ]"; got != want {
		t.Fatalf("title with truncated rows = %q, want %q", got, want)
	}

	s.currentListTruncated = false
	s.refreshTable()
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2 ]"; got != want {
		t.Fatalf("title once complete = %q, want %q", got, want)
	}
}

func TestRenderHeaderHintsShowsLoadAllOnTruncatedList(t *testing.T) {
	s := New(resource.NewRegistry())

	s.currentListTruncated = true
	s.renderHeaderHints()
	if !strings.Contains(s.headerHint.GetText(true), "load all") {
		t.Fatalf("expected a 'load all' hint on a truncated list, got %q", s.headerHint.GetText(true))
	}

	s.currentListTruncated = false
	s.renderHeaderHints()
	if strings.Contains(s.headerHint.GetText(true), "load all") {
		t.Fatalf("expected no 'load all' hint on a complete list")
	}
}

func TestLoadListPrefersListPartialAndMarksTruncated(t *testing.T) {
	s, res := newPartialTestShell(t)

	onEventLoop(s, func() {
		s.currentListResource = "widgets"
		s.currentColumns = res.Columns()
		s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})
		s.loadList(res, "", "", true, false, false)
	})

	waitFor(t, func() bool { return len(res.recordedCalls()) == 1 })

	var truncated bool
	var title string
	waitFor(t, func() bool {
		onEventLoop(s, func() { truncated = s.currentListTruncated; title = s.content.GetTitle() })
		return truncated
	})

	calls := res.recordedCalls()
	if calls[0].loadAll {
		t.Fatalf("expected the initial fetch to be capped (loadAll=false), got %+v", calls[0])
	}
	if title != "[ Taskcluster :: widgets · 2+ ]" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestLoadAllKeyRefetchesUncapped(t *testing.T) {
	s, res := newPartialTestShell(t)

	onEventLoop(s, func() {
		s.currentListResource = "widgets"
		s.currentColumns = res.Columns()
		s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})
		s.loadList(res, "", "", true, false, false)
	})

	var truncated bool
	waitFor(t, func() bool {
		onEventLoop(s, func() { truncated = s.currentListTruncated })
		return truncated
	})

	onEventLoop(s, func() {
		s.globalInputCapture(tcell.NewEventKey(tcell.KeyRune, 'L', tcell.ModNone))
	})

	waitFor(t, func() bool {
		calls := res.recordedCalls()
		return len(calls) == 2 && calls[1].loadAll
	})

	var title string
	waitFor(t, func() bool {
		onEventLoop(s, func() { truncated = s.currentListTruncated; title = s.content.GetTitle() })
		return !truncated
	})
	if strings.Contains(title, "+") {
		t.Fatalf("expected the truncation suffix to disappear after loading all, got %q", title)
	}
}

// A fresh-but-truncated cache entry must not satisfy a load once the user
// has asked for everything — otherwise navigating away and back within the
// TTL, after pressing 'L' but before the uncapped fetch landed, would pin
// the view to the capped snapshot.
func TestLoadListSkipsTruncatedCacheEntryWhenLoadAllRequested(t *testing.T) {
	s, res := newPartialTestShell(t)

	onEventLoop(s, func() {
		s.currentListResource = "widgets"
		s.currentColumns = res.Columns()
		s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})
		s.loadList(res, "", "", true, false, false)
	})

	var truncated bool
	waitFor(t, func() bool {
		onEventLoop(s, func() { truncated = s.currentListTruncated })
		return truncated
	})

	// The capped result is now cached and fresh. Ask for everything, then
	// dispatch a plain (non-forced) load, as back-navigation would.
	onEventLoop(s, func() {
		s.loadAllKeys[cacheKeyFor(res, "", "")] = true
		s.loadList(res, "", "", true, false, false)
	})

	waitFor(t, func() bool {
		calls := res.recordedCalls()
		return len(calls) == 2 && calls[1].loadAll
	})
}

func TestLoadAllKeyPassesThroughWhenListNotTruncated(t *testing.T) {
	s := New(resource.NewRegistry())

	event := tcell.NewEventKey(tcell.KeyRune, 'L', tcell.ModNone)
	if got := s.globalInputCapture(event); got != event {
		t.Fatalf("expected 'L' to pass through when nothing is truncated, got %#v", got)
	}
}
