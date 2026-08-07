package shell

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
	"github.com/taskcluster/tc-tui/taskcluster"
)

// newTestShellForSort builds a Shell as if renderList had just populated a
// two-column, two-row list view for a resource named "widgets" — without
// actually calling renderList (see note above).
func newTestShellForSort() *Shell {
	s := New(resource.NewRegistry())
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}, {Title: "VALUE"}}
	s.lastRows = []resource.Row{
		{ID: "b", Cells: []string{"b", "2"}},
		{ID: "a", Cells: []string{"a", "1"}},
	}
	return s
}

type fakeClientFacetedResource struct {
	fakeResource
	column  int
	options []string
}

func (f fakeClientFacetedResource) FacetColumn() int                          { return f.column }
func (f fakeClientFacetedResource) FacetOptions(rows []resource.Row) []string { return f.options }

type fakeServerFacetedListResource struct {
	fakeResource
	options []string
	rows    map[string][]resource.Row
	counts  map[string]int
	err     error
	ttl     time.Duration // overrides fakeResource's default RefreshInterval() of 0

	mu    sync.Mutex
	calls []string // records the `value` FacetList was called with, in order
}

func (f *fakeServerFacetedListResource) RefreshInterval() time.Duration { return f.ttl }
func (f *fakeServerFacetedListResource) FacetOptions() []string         { return f.options }
func (f *fakeServerFacetedListResource) FacetList(scope, value string) ([]resource.Row, error) {
	f.mu.Lock()
	f.calls = append(f.calls, value)
	f.mu.Unlock()

	if f.err != nil {
		return nil, f.err
	}
	return f.rows[value], nil
}
func (f *fakeServerFacetedListResource) FacetCounts(scope string) (map[string]int, error) {
	return f.counts, nil
}

// lastCall returns the most recent value FacetList was called with, and
// whether it's been called at all — synchronized since FacetList runs on a
// background goroutine (loadList) while the test polls from the main one.
func (f *fakeServerFacetedListResource) lastCall() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.calls) == 0 {
		return "", false
	}
	return f.calls[len(f.calls)-1], true
}

func TestRenderListSetsUpClientFaceted(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeClientFacetedResource{
		fakeResource: fakeResource{name: "workerpools"},
		column:       1,
		options:      []string{"aws", "gcp"},
	}

	s.renderList(res, "", false)

	if s.currentFaceted == nil {
		t.Fatalf("expected currentFaceted to be set")
	}
	if s.currentServerFaceted != nil {
		t.Fatalf("expected currentServerFaceted to remain nil")
	}
	if s.currentFacetValue != "" {
		t.Fatalf("expected default facet value \"\" (All), got %q", s.currentFacetValue)
	}
}

// renderList must clear out whatever the PREVIOUS resource/view left behind
// synchronously, before its own (async) fetch has any chance to complete —
// otherwise a sort/filter/facet keypress fired in that window would hand
// the old, differently-shaped rows to the NEW resource's Augment. This is
// exactly what used to panic: WorkerPoolsResource.Augment unconditionally
// indexes Cells[4..6], which a row from some other resource may not have.
func TestRenderListClearsStaleRowsBeforeNewResourcesFetchCompletes(t *testing.T) {
	fake := &fakeWorkerPoolsTC{
		pools: taskcluster.WorkerPoolList{{WorkerPoolID: "proj/pool-a", ProviderID: "gcp"}},
	}
	res := resource.NewWorkerPoolsResource(fake)
	registry := resource.NewRegistry()
	registry.Register(res)

	s := New(registry)
	s.currentListResource = "other"
	s.currentColumns = []resource.Column{{Title: "ONLY"}}
	s.lastRows = []resource.Row{{ID: "x", Cells: []string{"one-cell-only"}}}
	s.augmentedRowIDs = map[string]bool{"x": true}
	s.visibleRowIDs.Store(map[string]bool{"x": true})

	s.renderList(res, "", false)

	if len(s.lastRows) != 0 {
		t.Fatalf("expected lastRows cleared synchronously by renderList, got %+v", s.lastRows)
	}
	if len(s.augmentedRowIDs) != 0 {
		t.Fatalf("expected augmentedRowIDs cleared synchronously by renderList, got %+v", s.augmentedRowIDs)
	}
	if ids, _ := s.visibleRowIDs.Load().(map[string]bool); len(ids) != 0 {
		t.Fatalf("expected visibleRowIDs cleared synchronously by renderList, got %+v", ids)
	}

	// Would panic before the fix, since s.currentColumns is now
	// WorkerPoolsResource's 7 columns but s.lastRows (if not cleared) still
	// held the 1-cell row from "other".
	s.toggleSort(0)
}

func TestRenderListSetsUpServerFacetedWithDefaultTab(t *testing.T) {
	s := New(resource.NewRegistry())
	res := &fakeServerFacetedListResource{
		fakeResource: fakeResource{name: "workers"},
		options:      []string{"running", "stopped"},
	}

	s.renderList(res, "gcp/pool-a", false)

	if s.currentServerFaceted == nil {
		t.Fatalf("expected currentServerFaceted to be set")
	}
	if s.currentFaceted != nil {
		t.Fatalf("expected currentFaceted to remain nil")
	}
	if s.currentFacetValue != "running" {
		t.Fatalf("expected default facet value %q, got %q", "running", s.currentFacetValue)
	}
}

func TestRenderListRestoresRememberedFacetValue(t *testing.T) {
	s := New(resource.NewRegistry())
	s.facetByResource["workers"] = "stopped"
	res := &fakeServerFacetedListResource{
		fakeResource: fakeResource{name: "workers"},
		options:      []string{"running", "stopped"},
	}

	s.renderList(res, "gcp/pool-a", false)

	if s.currentFacetValue != "stopped" {
		t.Fatalf("expected remembered facet value %q, got %q", "stopped", s.currentFacetValue)
	}
}

func TestRenderListFallsBackToDefaultForStaleRememberedValue(t *testing.T) {
	s := New(resource.NewRegistry())
	s.facetByResource["workers"] = "no-longer-valid"
	res := &fakeServerFacetedListResource{
		fakeResource: fakeResource{name: "workers"},
		options:      []string{"running", "stopped"},
	}

	s.renderList(res, "gcp/pool-a", false)

	if s.currentFacetValue != "running" {
		t.Fatalf("expected fallback to first option %q, got %q", "running", s.currentFacetValue)
	}
}

func TestRenderListRestoresRememberedFilterQuery(t *testing.T) {
	s := New(resource.NewRegistry())
	s.filterByResource["workerpools"] = "proj-task"
	res := fakeResource{name: "workerpools"}

	s.renderList(res, "", false)

	if s.filterQuery != "proj-task" {
		t.Fatalf("expected remembered filter query %q, got %q", "proj-task", s.filterQuery)
	}
}

func TestRenderListRecordsScope(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeScopedResource{fakeResource: fakeResource{name: "runs"}}

	s.renderList(res, "task-1", false)

	if s.currentListScope != "task-1" {
		t.Fatalf("expected currentListScope %q, got %q", "task-1", s.currentListScope)
	}
}

func TestRenderListClearsScopeForUnscopedList(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListScope = "stale"
	res := fakeResource{name: "workerpools"}

	s.renderList(res, "", false)

	if s.currentListScope != "" {
		t.Fatalf("expected empty currentListScope, got %q", s.currentListScope)
	}
}

func TestRenderListDefaultsToEmptyFilterQueryWhenNeverSet(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeResource{name: "workerpools"}

	s.renderList(res, "", false)

	if s.filterQuery != "" {
		t.Fatalf("expected empty filter query, got %q", s.filterQuery)
	}
}

func TestApplyListResultBumpsAugmentEpoch(t *testing.T) {
	s := newTestShellForSort()
	before := s.augmentEpoch

	s.applyListResult(fakeResource{name: "widgets"}, s.lastRows, nil, "", nil, false)

	if s.augmentEpoch != before+1 {
		t.Fatalf("expected augmentEpoch to increment by exactly 1, got %d (was %d)", s.augmentEpoch, before)
	}
}

func TestApplyListResultResetsAugmentProgress(t *testing.T) {
	s := newTestShellForSort()
	s.augmentCompleted, s.augmentTotal = 3, 10

	s.applyListResult(fakeResource{name: "widgets"}, s.lastRows, nil, "", nil, false)

	if s.augmentCompleted != 0 || s.augmentTotal != 0 {
		t.Fatalf("expected progress reset to 0/0, got %d/%d", s.augmentCompleted, s.augmentTotal)
	}
}

func TestRefreshTableShowsActiveFilterInTitle(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workerpools"
	s.currentColumns = []resource.Column{{Title: "NAME"}}
	s.lastRows = []resource.Row{{ID: "a", Cells: []string{"aws-1"}}}

	s.filterQuery = "aws"
	s.refreshTable()
	if got, want := s.content.GetTitle(), "[ Taskcluster :: workerpools (aws) · 1 ]"; got != want {
		t.Fatalf("title with active filter = %q, want %q", got, want)
	}

	s.filterQuery = ""
	s.refreshTable()
	if got, want := s.content.GetTitle(), "[ Taskcluster :: workerpools · 1 ]"; got != want {
		t.Fatalf("title with cleared filter = %q, want %q", got, want)
	}
}

func TestRefreshTableShowsFilteredCountInTitle(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "NAME"}}
	s.lastRows = []resource.Row{
		{ID: "1", Cells: []string{"one"}},
		{ID: "2", Cells: []string{"two"}},
		{ID: "3", Cells: []string{"three"}},
	}

	s.filterQuery = "o" // matches one and two, hides three
	s.refreshTable()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets (o) · 2 of 3 ]"; got != want {
		t.Fatalf("filtered-count title = %q, want %q", got, want)
	}
}

func TestRefreshTableShowsScopeInTitle(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "runs"
	s.currentListScope = "task-1"
	s.currentColumns = []resource.Column{{Title: "RUN"}}
	s.lastRows = []resource.Row{{ID: "task-1/0", Cells: []string{"0"}}}

	s.refreshTable()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: runs (task-1) · 1 ]"; got != want {
		t.Fatalf("title with scope = %q, want %q", got, want)
	}
}

func TestRefreshTableShowsScopeSubtitleInTitle(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "taskgroup"
	s.currentListScope = "grp-1"
	s.currentScopeSubtitle = "not sealed"
	s.currentColumns = []resource.Column{{Title: "TASK ID"}}
	s.lastRows = []resource.Row{{ID: "task-1", Cells: []string{"task-1"}}}

	s.refreshTable()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: taskgroup (grp-1) [not sealed] · 1 ]"; got != want {
		t.Fatalf("title with scope subtitle = %q, want %q", got, want)
	}
}

func TestRefreshTableShowsAugmentProgressInTitle(t *testing.T) {
	s := newTestShellForSort() // currentListResource: "widgets", two rows already set up
	s.augmentCompleted, s.augmentTotal = 2, 5

	s.refreshTable()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2 [2/5] ]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRefreshTableHidesAugmentSuffixOnceComplete(t *testing.T) {
	s := newTestShellForSort()
	s.augmentCompleted, s.augmentTotal = 5, 5

	s.refreshTable()

	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2 ]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRefreshTableSkipsClientFilterForServerFaceted(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workers"
	s.currentColumns = []resource.Column{{Title: "STATE"}}
	s.currentServerFaceted = &fakeServerFacetedListResource{options: []string{"running"}}
	s.currentFacetValue = "running"
	// lastRows already server-filtered to "running" — refreshTable must not
	// drop any of them via a (nonexistent) client-side facet filter.
	s.lastRows = []resource.Row{
		{ID: "a", Cells: []string{"running"}},
		{ID: "b", Cells: []string{"running"}},
	}

	s.refreshTable()

	if s.table.GetRowCount() != 3 { // 1 header row + 2 data rows
		t.Fatalf("expected 2 data rows to survive refreshTable, got %d total rows", s.table.GetRowCount())
	}
}

func TestCycleFacetClientSideIsInstantAndPersists(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentListResource = "workerpools"
	s.currentColumns = []resource.Column{{Title: "PROVIDER"}}
	s.currentFaceted = fakeClientFacetedResource{column: 0, options: []string{"aws", "gcp"}}
	s.currentFacetValue = ""
	s.lastRows = []resource.Row{
		{ID: "a", Cells: []string{"aws"}},
		{ID: "b", Cells: []string{"gcp"}},
	}

	s.cycleFacet(1) // All -> aws

	if s.currentFacetValue != "aws" {
		t.Fatalf("expected \"aws\", got %q", s.currentFacetValue)
	}
	if s.facetByResource["workerpools"] != "aws" {
		t.Fatalf("expected facet remembered for workerpools, got %+v", s.facetByResource)
	}
	if s.table.GetRowCount() != 2 { // 1 header + 1 matching row
		t.Fatalf("expected table filtered to 1 matching row, got %d total rows", s.table.GetRowCount())
	}
}

func TestCycleFacetServerSideTriggersRefetch(t *testing.T) {
	s := New(resource.NewRegistry())
	res := &fakeServerFacetedListResource{
		fakeResource: fakeResource{name: "workers"},
		options:      []string{"running", "stopped"},
		rows: map[string][]resource.Row{
			"stopped": {{ID: "a", Cells: []string{"stopped"}}},
		},
	}
	s.stack.Push(View{ResourceName: "workers", Kind: ListKind, Scope: "gcp/pool-a"})
	s.currentListResource = "workers"
	s.currentColumns = []resource.Column{{Title: "STATE"}}
	s.currentServerFaceted = res
	s.currentFacetValue = "running"

	s.cycleFacet(1) // running -> stopped, triggers loadList in a goroutine

	if s.currentFacetValue != "stopped" {
		t.Fatalf("expected \"stopped\", got %q", s.currentFacetValue)
	}
	if s.facetByResource["workers"] != "stopped" {
		t.Fatalf("expected facet remembered for workers, got %+v", s.facetByResource)
	}

	waitFor(t, func() bool {
		last, ok := res.lastCall()
		return ok && last == "stopped"
	})
}

func TestLoadListServesFromCacheWithoutFetching(t *testing.T) {
	s := New(resource.NewRegistry())
	res := &fakeServerFacetedListResource{
		fakeResource: fakeResource{name: "workers"},
		ttl:          time.Minute,
		options:      []string{"running"},
	}
	s.currentListResource = "workers"
	s.currentColumns = []resource.Column{{Title: "STATE"}}
	s.currentServerFaceted = res
	s.currentFacetValue = "running"
	s.cache.set(cacheKeyFor(res, "pool-a", "running"), cacheEntry{
		rows:      []resource.Row{{ID: "cached", Cells: []string{"running"}}},
		fetchedAt: time.Now(),
	})

	s.loadList(res, "pool-a", "running", true, false, false)

	if len(s.lastRows) != 1 || s.lastRows[0].ID != "cached" {
		t.Fatalf("expected cache-hit rows to be applied, got %+v", s.lastRows)
	}
	if _, called := res.lastCall(); called {
		t.Fatalf("expected no fetch on a cache hit, but FacetList was called")
	}
}

type fakeAugmentableResource struct {
	fakeResource
	rows []resource.Row
	ttl  time.Duration // overrides fakeResource's default RefreshInterval() of 0

	// pauseAfter/resume/done let a test simulate a large, slow-draining
	// batch: Augment blocks after processing pauseAfter rows until resume
	// is closed, giving the test a window to change what's visible before
	// the rest of rows gets its wanted check. Left nil/zero, Augment just
	// runs straight through — the tests that don't care about this behave
	// exactly as before. done, if set, is closed once Augment returns.
	pauseAfter int
	resume     chan struct{}
	done       chan struct{}

	// pausePoints additionally blocks at these row indices (0-based, checked
	// before that row's wanted check) until the corresponding channel is
	// closed — lets a test inject a SECOND, independently-timed pause within
	// one Augment call, e.g. to change what's visible between two specific
	// rows rather than only once via pauseAfter/resume.
	pausePoints map[int]chan struct{}

	// callPause, if set for a given 0-based Augment CALL number (the Nth
	// time Augment is invoked on this resource — e.g. call 0 is the
	// initial dispatch, call 1 a nested/follow-up one triggered from
	// within call 0's own tick handler), blocks that whole call before it
	// processes even its first row. Unlike pauseAfter/pausePoints (which
	// gate row indices WITHIN one call and would apply identically to
	// every call sharing this same resource), this lets a test pause one
	// SPECIFIC dispatch independently of any other — needed because a
	// nested dispatch's rows can otherwise finish so fast (no real network
	// delay) that there's no way to observe its in-flight state at all.
	callPause map[int]chan struct{}
	callCount int // guarded by mu

	// tickOnUpdate, when true, calls onUpdate once per row (like a real
	// Augmentable resource's progressive ticks) rather than only recording
	// IDs — needed by tests that exercise the shell's own onUpdate/redraw
	// handling (e.g. its redraw throttling), not just what Augment was
	// asked for.
	tickOnUpdate bool

	mu           sync.Mutex
	augmentedIDs []string
}

func (f *fakeAugmentableResource) List() ([]resource.Row, error)  { return f.rows, nil }
func (f *fakeAugmentableResource) RefreshInterval() time.Duration { return f.ttl }

func (f *fakeAugmentableResource) Augment(rows []resource.Row, wanted func(id string) bool, onUpdate func(rows []resource.Row, completed, total int)) {
	f.mu.Lock()
	myCall := f.callCount
	f.callCount++
	f.mu.Unlock()
	if ch, ok := f.callPause[myCall]; ok {
		<-ch
	}

	for i, row := range rows {
		if f.resume != nil && i == f.pauseAfter {
			<-f.resume
		}
		if ch, ok := f.pausePoints[i]; ok {
			<-ch
		}
		if wanted(row.ID) {
			f.mu.Lock()
			f.augmentedIDs = append(f.augmentedIDs, row.ID)
			f.mu.Unlock()
		}
		if f.tickOnUpdate {
			onUpdate(append([]resource.Row(nil), rows[:i+1]...), i+1, len(rows))
		}
	}
	if f.done != nil {
		close(f.done)
	}
}

func (f *fakeAugmentableResource) recordedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.augmentedIDs...)
}

// newRunningTestShell returns a Shell backed by a live tview.Application
// (via a SimulationScreen, so no real terminal is needed). Most tests in
// this file deliberately never call Application.Run() and only assert on
// work a background goroutine does before its result reaches
// QueueUpdateDraw (see waitFor's doc comment) — but loadList's Augment call
// happens only after its own QueueUpdateDraw round-trip completes, which
// blocks forever unless something is actually draining the update queue.
func newRunningTestShell(t *testing.T, registry *resource.Registry) *Shell {
	t.Helper()
	s := New(registry)
	startRunning(t, s)
	return s
}

// startRunning starts s.app's event loop against a SimulationScreen (no real
// terminal needed) and registers cleanup to stop it. Split out from
// newRunningTestShell so a test can do setup that must NOT race the live
// Draw loop (e.g. a synchronous cache-hit render, which mutates Shell/tview
// state directly on the calling goroutine rather than via QueueUpdateDraw)
// before the app is actually running.
func startRunning(t *testing.T, s *Shell) {
	t.Helper()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	s.app.SetScreen(screen)

	go s.app.Run()
	t.Cleanup(s.app.Stop)
}

// A row hidden by the active filter shouldn't be handed to Augment at all —
// enriching a row the user can't see is wasted API calls.
func TestLoadListOnlyAugmentsFilterMatchingRows(t *testing.T) {
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows: []resource.Row{
			{ID: "a", Cells: []string{"alpha"}},
			{ID: "b", Cells: []string{"beta"}},
		},
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.filterQuery = "alpha"
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	waitFor(t, func() bool { return len(res.recordedIDs()) > 0 })

	ids := res.recordedIDs()
	if len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("expected only the filter-matching row to be augmented, got %+v", ids)
	}
}

// Widening (or clearing) the filter after the initial load must retrigger
// Augment for whatever newly became visible — otherwise those rows are
// stuck at their placeholder forever, since Augment only ever ran once
// against the filter active at load time.
func TestClearingFilterAugmentsNewlyVisibleRows(t *testing.T) {
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows: []resource.Row{
			{ID: "a", Cells: []string{"alpha"}},
			{ID: "b", Cells: []string{"beta"}},
		},
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.filterQuery = "alpha"
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)
	waitFor(t, func() bool { return len(res.recordedIDs()) == 1 })

	s.app.QueueUpdateDraw(func() {
		s.filterQuery = ""
		s.refreshTable()
	})

	waitFor(t, func() bool { return len(res.recordedIDs()) == 2 })

	ids := res.recordedIDs()
	if !(len(ids) == 2 && ids[0] == "a" && ids[1] == "b") {
		t.Fatalf("expected row a then newly-revealed row b to be augmented, got %+v", ids)
	}
}

func TestShouldRedrawAugmentTickAlwaysTrueOnFinalTick(t *testing.T) {
	now := time.Now()
	if !shouldRedrawAugmentTick(5, 5, now, now) {
		t.Fatalf("expected the final tick (completed == total) to always redraw, even with lastRedraw == now")
	}
	if !shouldRedrawAugmentTick(6, 5, now, now) {
		t.Fatalf("expected completed > total to also count as final")
	}
}

func TestShouldRedrawAugmentTickThrottlesRapidNonFinalTicks(t *testing.T) {
	now := time.Now()
	lastRedraw := now
	soon := now.Add(augmentRedrawInterval / 2)

	if shouldRedrawAugmentTick(2, 10, lastRedraw, soon) {
		t.Fatalf("expected a non-final tick within augmentRedrawInterval of the last redraw to be throttled")
	}
}

func TestShouldRedrawAugmentTickRedrawsOnceIntervalElapses(t *testing.T) {
	now := time.Now()
	lastRedraw := now
	later := now.Add(augmentRedrawInterval + time.Millisecond)

	if !shouldRedrawAugmentTick(2, 10, lastRedraw, later) {
		t.Fatalf("expected a non-final tick to redraw once augmentRedrawInterval has elapsed")
	}
}

func TestMightMatchAugmentedColumn(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"5", true},
		{"gcp5", true},
		{"pool-5", true},
		{"gcp", false},
		{"proj/pool-a", false},
		{"", false},
	}
	for _, c := range cases {
		if got := mightMatchAugmentedColumn(c.query); got != c.want {
			t.Errorf("mightMatchAugmentedColumn(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

// A purely alphabetic query that matches nothing is a definitive, complete
// answer already — base columns are always fully known, and alphabetic text
// can never match a numeric augmented column, so there is nothing the
// fallback could possibly find by augmenting more rows. It must not walk
// the hidden list for a plain typo.
func TestFilterWithNoMatchesAndNoDigitsDoesNotTriggerFallback(t *testing.T) {
	rows := []resource.Row{
		{ID: "a", Cells: []string{"alpha"}},
		{ID: "b", Cells: []string{"beta"}},
	}
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.filterQuery = "zzzz" // matches nothing, no digits — a plain typo
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	// Nothing should ever get dispatched — give it a moment to (wrongly)
	// prove otherwise, since there's no positive event to wait for here.
	time.Sleep(50 * time.Millisecond)
	if ids := res.recordedIDs(); len(ids) != 0 {
		t.Fatalf("expected no rows to be augmented for a non-matching, non-numeric filter, got %+v", ids)
	}
}

// The fallback loop iterates s.lastRows independently of the visible loop
// above it, so without its own "already staged this call" guard, a row
// that's BOTH currently visible AND not yet augmented (the common case
// right after a fresh load with a digit-bearing filter) would be appended
// to toAugment twice — once from the visible loop, once again from the
// fallback loop, since s.augmentedRowIDs isn't populated until
// markRowsAugmented runs after both loops.
func TestFallbackDoesNotDuplicateARowAlreadyStagedFromVisible(t *testing.T) {
	rows := []resource.Row{
		{ID: "a", Cells: []string{"pool-5"}}, // visible via its own cell AND a fallback candidate
		{ID: "b", Cells: []string{"beta"}},
	}
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.filterQuery = "5" // matches "a" directly, AND enables the fallback (has a digit)
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	waitFor(t, func() bool { return len(res.recordedIDs()) >= 2 })

	ids := res.recordedIDs()
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	if seen["a"] != 1 {
		t.Fatalf("expected row a to be augmented exactly once, got %d times (%+v)", seen["a"], ids)
	}
}

// Without the throttle, a big batch that ticks near-instantly (no network
// round-trip pacing it, e.g. because wanted skipped most of it) would drive
// hundreds of full refreshTable/SetData calls back to back, each blocking
// the single UI goroutine — this is what made filter changes feel stuck.
// With it, the whole burst should complete in about one redraw interval's
// worth of wall-clock time, not hundreds.
func TestAugmentRedrawThrottleKeepsLargeBurstFast(t *testing.T) {
	const rowCount = 300

	rows := make([]resource.Row, rowCount)
	for i := range rows {
		id := fmt.Sprintf("pool-%03d", i)
		rows[i] = resource.Row{ID: id, Cells: []string{id}}
	}
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
		tickOnUpdate: true,
		done:         make(chan struct{}),
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	// Count actual redraws directly via the test hook rather than
	// wall-clock timing — a SimulationScreen's in-memory Draw() is far
	// cheaper than a real terminal's, so timing alone wouldn't reliably
	// distinguish throttled from unthrottled here even though the
	// difference is very real against a real terminal.
	var mu sync.Mutex
	var redraws int
	s.onAugmentRedrawForTest = func() {
		mu.Lock()
		redraws++
		mu.Unlock()
	}

	s.loadList(res, "", "", true, false, false)

	select {
	case <-res.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Augment did not finish in time")
	}

	waitFor(t, func() bool {
		var completed, total int
		s.app.QueueUpdateDraw(func() { completed, total = s.augmentCompleted, s.augmentTotal })
		return completed == rowCount && total == rowCount
	})

	if len(res.recordedIDs()) != rowCount {
		t.Fatalf("expected all %d rows to be augmented, got %d", rowCount, len(res.recordedIDs()))
	}

	mu.Lock()
	got := redraws
	mu.Unlock()
	// Without throttling this would be rowCount (300) redraws — one per
	// tick. With it, only ticks at least augmentRedrawInterval apart
	// actually redraw, so a burst this fast should collapse to a small
	// constant number regardless of rowCount (generous margin: well under
	// rowCount, not a tight bound on the exact count).
	if got >= rowCount/10 {
		t.Fatalf("expected far fewer than %d redraws for a %d-row burst, got %d — throttling may not be working", rowCount/10, rowCount, got)
	}
}

// The scenario a large fxci-scale worker pool list runs into: Augment gets
// dispatched for a big unfiltered batch, and while it's still working
// through it (most rows still queued, per Augmentable.Augment's own
// concurrency), the user narrows the filter. Rows not yet reached at that
// point must be skipped via wanted rather than dutifully processed anyway —
// this is what actually stops wasted API calls, as opposed to
// TestLoadListOnlyAugmentsFilterMatchingRows which only covers not
// REQUESTING a row Augment was never even asked about.
func TestNarrowingFilterMidBatchSkipsRowsNoLongerVisible(t *testing.T) {
	rows := make([]resource.Row, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		rows = append(rows, resource.Row{ID: id, Cells: []string{id}})
	}
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
		pauseAfter:   2, // processes a, b, then blocks until the test says go
		resume:       make(chan struct{}),
		done:         make(chan struct{}),
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)
	waitFor(t, func() bool { return len(res.recordedIDs()) == 2 })

	// Narrow the filter to just "a" — c, d, e (not yet reached) are now
	// invisible; b (already recorded before the pause) stays recorded,
	// matching Augment's contract that wanted is a live, per-row check, not
	// a retroactive undo.
	s.app.QueueUpdateDraw(func() {
		s.filterQuery = "a"
		s.refreshTable()
	})
	close(res.resume)

	select {
	case <-res.done:
	case <-time.After(2 * time.Second):
		t.Fatal("Augment did not finish in time")
	}

	ids := res.recordedIDs()
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("expected c, d, e to be skipped once no longer visible, got %+v", ids)
	}
}

// Rows Augment skipped because wanted rejected them must not stay marked
// "augmented" forever — otherwise widening the filter back out (or clearing
// it) would never retry them, leaving them stuck at their placeholder
// indefinitely, even though the whole point of skipping was "not right
// now", not "never".
func TestWideningFilterAfterMidBatchSkipRetriesThoseRows(t *testing.T) {
	rows := make([]resource.Row, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		rows = append(rows, resource.Row{ID: id, Cells: []string{id}})
	}
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
		pauseAfter:   2, // processes a, b, then blocks until the test says go
		resume:       make(chan struct{}),
		tickOnUpdate: true, // the unmark-on-final-tick logic lives inside onUpdate
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)
	waitFor(t, func() bool { return len(res.recordedIDs()) == 2 })

	// Narrow the filter — c, d, e (not yet reached) get skipped by wanted.
	s.app.QueueUpdateDraw(func() {
		s.filterQuery = "a"
		s.refreshTable()
	})
	close(res.resume)

	// Wait for the batch's final tick (where the just-skipped rows get
	// un-marked) to actually land before widening back out.
	waitFor(t, func() bool {
		var completed, total int
		s.app.QueueUpdateDraw(func() { completed, total = s.augmentCompleted, s.augmentTotal })
		return total == 5 && completed == total
	})

	// Clear the filter — c, d, e are visible again.
	s.app.QueueUpdateDraw(func() {
		s.filterQuery = ""
		s.refreshTable()
	})

	waitFor(t, func() bool {
		seen := map[string]bool{}
		for _, id := range res.recordedIDs() {
			seen[id] = true
		}
		return seen["c"] && seen["d"] && seen["e"]
	})
}

// A row can be rejected by wanted while the filter is narrow and then become
// visible again BEFORE the batch's final tick — re-checking wanted() only at
// that final tick (rather than recording every rejection as it happens)
// would see the row as visible again and wrongly leave it marked
// "augmented" despite it never having actually been fetched. This
// reproduces exactly that race: c is rejected, then the filter widens back
// out while the batch is still mid-flight (before c's own tick even lands),
// so by the final tick wanted(c) would say true.
func TestRowRejectedThenVisibleAgainBeforeBatchEndsStillGetsRetried(t *testing.T) {
	rows := make([]resource.Row, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		rows = append(rows, resource.Row{ID: id, Cells: []string{id}})
	}
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
		pauseAfter:   2, // pause before c (index 2), after a, b are done
		resume:       make(chan struct{}),
		pausePoints: map[int]chan struct{}{
			3: make(chan struct{}), // pause again before d (index 3), after c's own wanted check
		},
		tickOnUpdate: true, // the rejection-tracking/unmark logic lives inside onUpdate
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)
	waitFor(t, func() bool { return len(res.recordedIDs()) == 2 }) // a, b done

	// Narrow the filter — c (about to be checked) will be rejected.
	s.app.QueueUpdateDraw(func() {
		s.filterQuery = "a"
		s.refreshTable()
	})
	close(res.resume) // let c's own wanted check run narrow, then it pauses again before d

	waitFor(t, func() bool {
		var completed int
		s.app.QueueUpdateDraw(func() { completed = s.augmentCompleted })
		return completed == 3 // a, b, c's tick has landed (c itself skipped)
	})

	// Widen back out BEFORE the batch's final tick (d, e haven't run yet) —
	// c is visible again well before completed reaches total.
	s.app.QueueUpdateDraw(func() {
		s.filterQuery = ""
		s.refreshTable()
	})
	close(res.pausePoints[3])

	waitFor(t, func() bool {
		seen := map[string]bool{}
		for _, id := range res.recordedIDs() {
			seen[id] = true
		}
		return seen["c"]
	})
}

// With no filter active, every row is visible, so every row should still be
// augmented — this fix must not change behavior for the common unfiltered
// case.
func TestLoadListAugmentsAllRowsWhenUnfiltered(t *testing.T) {
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows: []resource.Row{
			{ID: "a", Cells: []string{"alpha"}},
			{ID: "b", Cells: []string{"beta"}},
		},
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	waitFor(t, func() bool { return len(res.recordedIDs()) == 2 })
}

// A cache hit's rows are assumed already settled (whatever a prior Augment
// run last merged into them before caching) — loadList must not re-trigger
// Augment for them just because refreshTable ran.
func TestLoadListCacheHitDoesNotReAugmentRowsAlreadyMarkedDone(t *testing.T) {
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		ttl:          time.Minute,
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := New(registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.cache.set(cacheKeyFor(res, "", ""), cacheEntry{
		rows:       []resource.Row{{ID: "a", Cells: []string{"alpha"}}},
		fetchedAt:  time.Now(),
		settledIDs: map[string]bool{"a": true},
	})

	s.loadList(res, "", "", true, false, false)

	if ids := res.recordedIDs(); len(ids) != 0 {
		t.Fatalf("expected no Augment call for a row the cache already marked done, got %+v", ids)
	}
}

// A cache entry written before Augment ever ran for a row (e.g. it was
// hidden by a narrower filter at the time it was cached) must NOT be treated
// as settled just because it came from the cache — this is the bug from a
// prior fix attempt: blindly marking every cache-hit row as done meant a row
// that was never actually augmented got permanently stuck at its
// placeholder for the lifetime of that cache entry.
func TestLoadListCacheHitStillAugmentsRowsNotYetMarkedDone(t *testing.T) {
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		ttl:          time.Minute,
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	// s.loadList's cache-hit branch renders synchronously on whatever
	// goroutine calls it (no background fetch involved) — do that here,
	// before the app is running, so it can't race the live Draw loop.
	// startRunning is only needed afterward, for the async Augment call
	// triggerAugmentForNewlyVisibleRows fires for row "b".
	s := New(registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})
	s.cache.set(cacheKeyFor(res, "", ""), cacheEntry{
		rows:       []resource.Row{{ID: "a", Cells: []string{"alpha"}}, {ID: "b", Cells: []string{"beta"}}},
		fetchedAt:  time.Now(),
		settledIDs: map[string]bool{"a": true}, // "b" was never actually augmented
	})

	s.loadList(res, "", "", true, false, false)
	startRunning(t, s)

	waitFor(t, func() bool { return len(res.recordedIDs()) > 0 })

	ids := res.recordedIDs()
	if len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("expected only row b (not yet marked done) to be augmented, got %+v", ids)
	}
}

// refreshTable, called from within an Augment tick's own handler, may
// itself dispatch a BRAND NEW batch (e.g. for a row that just became
// visible) before that handler writes to the cache. That new batch's rows
// get marked "requested" (s.augmentedRowIDs) the instant they're
// dispatched, well before they've had any chance to actually fetch
// anything — the cache write right after must not treat them as settled,
// or navigating away and back within the cache TTL would see their
// still-placeholder cells as done forever.
func TestCacheWriteFromOneBatchDoesNotSettleANestedlyDispatchedBatch(t *testing.T) {
	rows := []resource.Row{
		{ID: "a", Cells: []string{"a"}},
		{ID: "b", Cells: []string{"b"}},
	}
	batch2Pause := make(chan struct{})
	res := &fakeAugmentableResource{
		fakeResource: fakeResource{name: "widgets"},
		rows:         rows,
		ttl:          time.Minute, // cache.get requires ttl > 0 to ever return a hit
		pauseAfter:   0,           // block call 0 (batch 1) before processing "a" at all
		resume:       make(chan struct{}),
		// call 1 is batch 2 (the nested dispatch for "b") — held here so
		// the test can observe the cache write batch 1 makes DURING batch
		// 2's in-flight window; without this, batch 2's own trivial,
		// synchronous fake work finishes essentially instantly (no real
		// network delay), leaving no observable gap at all.
		callPause:    map[int]chan struct{}{1: batch2Pause},
		tickOnUpdate: true,
	}
	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = "widgets"
	s.currentColumns = []resource.Column{{Title: "ID"}}
	s.filterQuery = "a" // only "a" visible initially — batch 1 is just ["a"]
	s.stack.Push(View{ResourceName: "widgets", Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	// markRowsAugmented runs synchronously, before the fetch goroutine (and
	// its eventual pause) even starts — reliable to wait on.
	waitFor(t, func() bool {
		var marked bool
		s.app.QueueUpdateDraw(func() { marked = s.augmentedRowIDs["a"] })
		return marked
	})

	// Widen the filter WITHOUT calling refreshTable ourselves. "b" only
	// becomes visible once batch 1's own (single, final) tick fires and
	// calls refreshTable internally — which is what dispatches batch 2 for
	// "b" from WITHIN that tick's handler, immediately before its cache
	// write. Calling refreshTable directly here instead would dispatch
	// batch 2 right now, from this goroutine, not reproducing the nested
	// case at all.
	s.app.QueueUpdateDraw(func() { s.filterQuery = "" })

	close(res.resume) // let batch 1 process "a" and fire its one (final) tick

	// Wait for batch 2 (nested, for "b") to actually be dispatched —
	// markRowsAugmented for it runs synchronously inside batch 1's tick
	// handler, before batch 2's goroutine is even started, so this is safe
	// to wait on even though batch 2 itself is still held on callPause[1].
	waitFor(t, func() bool {
		var marked bool
		s.app.QueueUpdateDraw(func() { marked = s.augmentedRowIDs["b"] })
		return marked
	})

	// The cache write triggered by batch 1's tick handler (the same one
	// that just dispatched batch 2) must not list "b" as settled — batch 2
	// is still blocked on callPause[1] and hasn't fetched anything yet.
	var settled map[string]bool
	s.app.QueueUpdateDraw(func() {
		entry, ok := s.cache.get(cacheKeyFor(res, "", ""), res.RefreshInterval())
		if ok {
			settled = entry.settledIDs
		}
	})
	if settled["b"] {
		t.Fatalf("expected b (a nested, not-yet-started batch) to NOT be cached as settled, got %+v", settled)
	}
	if !settled["a"] {
		t.Fatalf("expected a (batch 1, actually finished) to be cached as settled, got %+v", settled)
	}

	close(batch2Pause) // let batch 2 finish so it doesn't leak
}

func TestLoadListForceRefreshBypassesCache(t *testing.T) {
	s := New(resource.NewRegistry())
	res := &fakeServerFacetedListResource{
		fakeResource: fakeResource{name: "workers"},
		ttl:          time.Minute,
		options:      []string{"running"},
		rows: map[string][]resource.Row{
			"running": {{ID: "fresh", Cells: []string{"running"}}},
		},
	}
	s.currentListResource = "workers"
	s.currentColumns = []resource.Column{{Title: "STATE"}}
	s.currentServerFaceted = res
	s.currentFacetValue = "running"
	s.stack.Push(View{ResourceName: "workers", Kind: ListKind, Scope: "pool-a"})
	s.cache.set(cacheKeyFor(res, "pool-a", "running"), cacheEntry{
		rows:      []resource.Row{{ID: "stale", Cells: []string{"running"}}},
		fetchedAt: time.Now(),
	})

	s.loadList(res, "pool-a", "running", false, true, false)

	waitFor(t, func() bool {
		_, called := res.lastCall()
		return called
	})
}

// waitFor polls cond briefly and fails the test if it never becomes true.
// It's used to observe a background goroutine's synchronous side effects
// (e.g. FacetList being called) without needing the queued QueueUpdateDraw
// callback to actually run — s.app is never Run() in these tests, so that
// callback would never be drained; the assertions here only depend on work
// the goroutine does before reaching QueueUpdateDraw.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition never became true")
}

func TestToggleSortFirstPressSortsAscending(t *testing.T) {
	s := newTestShellForSort()

	s.toggleSort(1) // VALUE column

	if s.currentSort.Column != 1 || s.currentSort.Direction != SortAsc {
		t.Fatalf("expected ascending sort on column 1, got %+v", s.currentSort)
	}
}

func TestToggleSortSameColumnReversesDirection(t *testing.T) {
	s := newTestShellForSort()

	s.toggleSort(1)
	s.toggleSort(1)

	if s.currentSort.Direction != SortDesc {
		t.Fatalf("expected second press to reverse to descending, got %+v", s.currentSort)
	}

	s.toggleSort(1)
	if s.currentSort.Direction != SortAsc {
		t.Fatalf("expected third press to reverse back to ascending, got %+v", s.currentSort)
	}
}

func TestToggleSortDifferentColumnStartsAscending(t *testing.T) {
	s := newTestShellForSort()

	s.toggleSort(1)
	s.toggleSort(1) // now descending on column 1
	s.toggleSort(0) // switch to column 0

	if s.currentSort.Column != 0 || s.currentSort.Direction != SortAsc {
		t.Fatalf("expected fresh ascending sort on column 0, got %+v", s.currentSort)
	}
}

func TestToggleSortOutOfRangeColumnIsNoOp(t *testing.T) {
	s := newTestShellForSort() // 2 columns

	s.toggleSort(5)

	if s.currentSort.Direction != SortNone {
		t.Fatalf("expected no-op for out-of-range column, got %+v", s.currentSort)
	}
}

func TestToggleSortRemembersPerResourceInMap(t *testing.T) {
	s := newTestShellForSort()

	s.toggleSort(1)

	got, ok := s.sortByResource["widgets"]
	if !ok || got.Column != 1 || got.Direction != SortAsc {
		t.Fatalf("expected widgets' sort remembered in map, got %+v (ok=%v)", got, ok)
	}

	// renderList's restore-on-switch does `s.currentSort =
	// s.sortByResource[res.Name()]` — for a resource with no memory yet,
	// that's the zero value (unsorted), same as this lookup.
	restored := s.sortByResource["gadgets"]
	if restored.Direction != SortNone {
		t.Fatalf("expected zero-value (unsorted) for a resource with no memory, got %+v", restored)
	}
}

func TestToggleSortResetsSelectionToTopRow(t *testing.T) {
	s := newTestShellForSort()
	s.refreshTable() // populate the table, as renderList/loadList would

	// Simulate the user having moved the cursor down to the second row
	// ("a") before triggering a sort.
	s.table.Select(2, 0)
	if row, _ := s.table.GetSelection(); row != 2 {
		t.Fatalf("test setup: expected cursor on row 2, got row %d", row)
	}

	s.toggleSort(0) // sort by ID ascending

	row, _ := s.table.GetSelection()
	if row != 1 {
		t.Fatalf("expected sorting to reset the cursor to the top row, got row %d", row)
	}
}

func TestToggleExpandColumnsFlipsTableStateAndShowsInTitle(t *testing.T) {
	s := newTestShellForSort()
	s.refreshTable() // populate the table, as renderList/loadList would

	if s.table.ExpandColumns() {
		t.Fatalf("expected columns not expanded by default")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2 ]"; got != want {
		t.Fatalf("title before toggling = %q, want %q", got, want)
	}

	s.toggleExpandColumns()

	if !s.table.ExpandColumns() {
		t.Fatalf("expected first toggle to expand columns")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2 [no truncation] ]"; got != want {
		t.Fatalf("title after toggling = %q, want %q", got, want)
	}

	s.toggleExpandColumns()

	if s.table.ExpandColumns() {
		t.Fatalf("expected second toggle to restore truncation")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widgets · 2 ]"; got != want {
		t.Fatalf("title after second toggle = %q, want %q", got, want)
	}
}

func TestToggleDetailWrapFlipsDetailStateAndShowsInTitle(t *testing.T) {
	s := newTestShellForSort()
	s.currentDetailTitle = "widget:1"
	s.refreshDetailTitle()

	if !s.detail.WrapEnabled() {
		t.Fatalf("expected wrap enabled by default")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widget:1 ]"; got != want {
		t.Fatalf("title before toggling = %q, want %q", got, want)
	}

	s.toggleDetailWrap()

	if s.detail.WrapEnabled() {
		t.Fatalf("expected first toggle to disable wrap")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widget:1 [no wrap] ]"; got != want {
		t.Fatalf("title after toggling = %q, want %q", got, want)
	}

	s.toggleDetailWrap()

	if !s.detail.WrapEnabled() {
		t.Fatalf("expected second toggle to restore wrap")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widget:1 ]"; got != want {
		t.Fatalf("title after second toggle = %q, want %q", got, want)
	}
}

func TestGlobalInputCaptureXKeyTogglesDetailWrapOnDetailPage(t *testing.T) {
	s := newTestShellForSort()
	s.content.SwitchToPage(pageDetail)

	event := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	if got := s.globalInputCapture(event); got != nil {
		t.Fatalf("expected 'x' key to be swallowed, got %#v", got)
	}

	if s.detail.WrapEnabled() {
		t.Fatalf("expected 'x' to toggle wrap off on the detail page")
	}
}

func TestGlobalInputCaptureXKeyTogglesExpandColumnsOnTablePage(t *testing.T) {
	s := newTestShellForSort()

	event := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	if got := s.globalInputCapture(event); got != nil {
		t.Fatalf("expected 'x' key to be swallowed, got %#v", got)
	}

	if !s.table.ExpandColumns() {
		t.Fatalf("expected 'x' to toggle ExpandColumns on")
	}
}

func TestGlobalInputCaptureXKeyIsNoOpOffTablePage(t *testing.T) {
	s := newTestShellForSort()
	s.content.SwitchToPage(pageDetail)

	event := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	s.globalInputCapture(event)

	if s.table.ExpandColumns() {
		t.Fatalf("expected 'x' to be a no-op when the table page isn't showing")
	}
}

func TestToggleDetailLineNumbersFlipsDetailStateAndShowsInTitle(t *testing.T) {
	s := newTestShellForSort()
	s.currentDetailTitle = "widget:1"
	s.refreshDetailTitle()

	if s.detail.LineNumbersEnabled() {
		t.Fatalf("expected line numbers disabled by default")
	}

	s.toggleDetailLineNumbers()

	if !s.detail.LineNumbersEnabled() {
		t.Fatalf("expected first toggle to enable line numbers")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widget:1 [#] ]"; got != want {
		t.Fatalf("title after toggling = %q, want %q", got, want)
	}

	s.toggleDetailLineNumbers()

	if s.detail.LineNumbersEnabled() {
		t.Fatalf("expected second toggle to disable line numbers")
	}
	if got, want := s.content.GetTitle(), "[ Taskcluster :: widget:1 ]"; got != want {
		t.Fatalf("title after second toggle = %q, want %q", got, want)
	}
}

func TestGlobalInputCaptureNKeyTogglesLineNumbersOnDetailPage(t *testing.T) {
	s := newTestShellForSort()
	s.content.SwitchToPage(pageDetail)

	event := tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)
	if got := s.globalInputCapture(event); got != nil {
		t.Fatalf("expected 'n' key to be swallowed, got %#v", got)
	}

	if !s.detail.LineNumbersEnabled() {
		t.Fatalf("expected 'n' to toggle line numbers on the detail page")
	}
}

func TestGlobalInputCaptureNKeyIsNoOpOffDetailPage(t *testing.T) {
	s := newTestShellForSort()

	event := tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)
	s.globalInputCapture(event)

	if s.detail.LineNumbersEnabled() {
		t.Fatalf("expected 'n' to be a no-op when the detail page isn't showing")
	}
}

func TestGlobalInputCaptureDigitTriggersSortOnTablePage(t *testing.T) {
	s := newTestShellForSort()

	event := tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone)
	if got := s.globalInputCapture(event); got != nil {
		t.Fatalf("expected digit key to be swallowed, got %#v", got)
	}

	if s.currentSort.Column != 1 || s.currentSort.Direction != SortAsc {
		t.Fatalf("expected '2' to sort column index 1 ascending, got %+v", s.currentSort)
	}
}

func TestGlobalInputCaptureDigitBeyondColumnCountIsNoOp(t *testing.T) {
	s := newTestShellForSort() // 2 columns

	event := tcell.NewEventKey(tcell.KeyRune, '9', tcell.ModNone)
	s.globalInputCapture(event)

	if s.currentSort.Direction != SortNone {
		t.Fatalf("expected out-of-range digit to be a no-op, got %+v", s.currentSort)
	}
}

// fakeHistoryRecorder is a minimal resource.HistoryRecorder for tests that
// need to observe what Shell recorded, without going through the real
// HistoryResource (whose own behavior is covered by resource/history_test.go).
type fakeHistoryRecorder struct {
	mu      sync.Mutex
	entries []resource.HistoryEntry
}

func (f *fakeHistoryRecorder) Record(e resource.HistoryEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
}

func (f *fakeHistoryRecorder) Entries() []resource.HistoryEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]resource.HistoryEntry(nil), f.entries...)
}

func (f *fakeHistoryRecorder) Restore(entries []resource.HistoryEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = entries
}

// fakeScopedTTLResource is a ScopedResource with a settable RefreshInterval,
// needed to exercise loadList's cache-hit branch for a scoped (not
// ServerFaceted) resource — fakeResource's RefreshInterval() is hardcoded to
// 0, which the cache always treats as a miss (see cache.go's get).
type fakeScopedTTLResource struct {
	fakeResource
	ttl time.Duration
}

func (f fakeScopedTTLResource) RefreshInterval() time.Duration                  { return f.ttl }
func (f fakeScopedTTLResource) ScopedList(scope string) ([]resource.Row, error) { return nil, nil }
func (f fakeScopedTTLResource) EmptyScopeResource() string                      { return "" }

func TestLoadListRecordsScopedVisitOnSuccessfulCacheHit(t *testing.T) {
	s := New(resource.NewRegistry())
	rec := &fakeHistoryRecorder{}
	s.historyRecorder = rec
	s.currentColumns = []resource.Column{{Title: "ID"}}

	res := fakeScopedTTLResource{fakeResource: fakeResource{name: "workers"}, ttl: time.Minute}
	s.cache.set(cacheKeyFor(res, "pool-a", ""), cacheEntry{
		rows:      []resource.Row{{ID: "worker-1", Cells: []string{"worker-1"}}},
		fetchedAt: time.Now(),
	})

	s.loadList(res, "pool-a", "", true, false, false)

	entries := rec.Entries()
	if len(entries) != 1 || entries[0].ResourceName != "workers" || entries[0].Scope != "pool-a" || entries[0].Kind != 0 {
		t.Fatalf("expected one recorded scoped-list visit, got %+v", entries)
	}
}

func TestLoadListAppliesCachedScopeSubtitle(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentColumns = []resource.Column{{Title: "ID"}}

	res := fakeScopedTTLResource{fakeResource: fakeResource{name: "taskgroup"}, ttl: time.Minute}
	s.cache.set(cacheKeyFor(res, "grp-1", ""), cacheEntry{
		rows:      []resource.Row{{ID: "task-1", Cells: []string{"task-1"}}},
		subtitle:  "not sealed",
		fetchedAt: time.Now(),
	})

	s.loadList(res, "grp-1", "", true, false, false)

	if s.currentScopeSubtitle != "not sealed" {
		t.Fatalf("expected cached subtitle applied, got %q", s.currentScopeSubtitle)
	}
}

// fakeScopeSubtitleTrackingResource is a ScopedResource + ScopeSubtitle with
// thread-safe call tracking, needed to observe (from the polling test
// goroutine) that loadList's background fetch actually calls Subtitle —
// s.app is never Run() in these tests, so the QueueUpdateDraw callback that
// would apply the result never executes; only side effects before that
// point are observable (see waitFor's doc comment).
type fakeScopeSubtitleTrackingResource struct {
	fakeResource
	ttl time.Duration

	mu     sync.Mutex
	called bool
}

func (f *fakeScopeSubtitleTrackingResource) RefreshInterval() time.Duration { return f.ttl }
func (f *fakeScopeSubtitleTrackingResource) ScopedList(scope string) ([]resource.Row, error) {
	return nil, nil
}
func (f *fakeScopeSubtitleTrackingResource) EmptyScopeResource() string { return "" }
func (f *fakeScopeSubtitleTrackingResource) Subtitle(scope string) (string, error) {
	f.mu.Lock()
	f.called = true
	f.mu.Unlock()
	return "not sealed", nil
}
func (f *fakeScopeSubtitleTrackingResource) wasCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func TestLoadListFetchesScopeSubtitleAlongsideRows(t *testing.T) {
	s := New(resource.NewRegistry())
	s.currentColumns = []resource.Column{{Title: "ID"}}

	res := &fakeScopeSubtitleTrackingResource{fakeResource: fakeResource{name: "taskgroup"}, ttl: time.Minute}

	s.loadList(res, "grp-1", "", true, false, false)

	waitFor(t, res.wasCalled)
}

func TestLoadListDoesNotRecordUnscopedVisit(t *testing.T) {
	s := New(resource.NewRegistry())
	rec := &fakeHistoryRecorder{}
	s.historyRecorder = rec
	s.currentColumns = []resource.Column{{Title: "ID"}}

	res := fakeScopedTTLResource{fakeResource: fakeResource{name: "workerpools"}, ttl: time.Minute}
	s.cache.set(cacheKeyFor(res, "", ""), cacheEntry{
		rows:      []resource.Row{{ID: "pool-a", Cells: []string{"pool-a"}}},
		fetchedAt: time.Now(),
	})

	s.loadList(res, "", "", true, false, false)

	if len(rec.Entries()) != 0 {
		t.Fatalf("expected no recorded visit for an unscoped list, got %+v", rec.Entries())
	}
}

func TestLoadListDoesNotRecordDuringRestore(t *testing.T) {
	s := New(resource.NewRegistry())
	rec := &fakeHistoryRecorder{}
	s.historyRecorder = rec
	s.currentColumns = []resource.Column{{Title: "ID"}}

	res := fakeScopedTTLResource{fakeResource: fakeResource{name: "workers"}, ttl: time.Minute}
	s.cache.set(cacheKeyFor(res, "pool-a", ""), cacheEntry{
		rows:      []resource.Row{{ID: "worker-1", Cells: []string{"worker-1"}}},
		fetchedAt: time.Now(),
	})

	s.loadList(res, "pool-a", "", true, false, true) // isRestore=true

	if len(rec.Entries()) != 0 {
		t.Fatalf("expected no recorded visit while isRestore=true, got %+v", rec.Entries())
	}
}

func TestLoadListDoesNotRecordForHistoryResourceItself(t *testing.T) {
	s := New(resource.NewRegistry())
	rec := &fakeHistoryRecorder{}
	s.historyRecorder = rec
	s.currentColumns = []resource.Column{{Title: "ID"}}

	res := fakeScopedTTLResource{fakeResource: fakeResource{name: "history"}, ttl: time.Minute}
	s.cache.set(cacheKeyFor(res, "pool-a", ""), cacheEntry{
		rows:      []resource.Row{{ID: "x", Cells: []string{"x"}}},
		fetchedAt: time.Now(),
	})

	s.loadList(res, "pool-a", "", true, false, false)

	if len(rec.Entries()) != 0 {
		t.Fatalf("expected no recorded visit for the history resource itself, got %+v", rec.Entries())
	}
}

func TestSwitchResourceDirectLookupWithIDGoesStraightToDetail(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"},
		label:        "task id",
	})
	s := New(registry)

	s.switchResource("task", "task-1")

	top, ok := s.stack.Top()
	if !ok || top.Kind != DetailKind || top.SelectedID != "task-1" || top.ResourceName != "task" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourcePlainResourceWithIDGoesStraightToDetail(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	s := New(registry)

	// workerpools has a List() and a Describe(id), but is neither a
	// ScopedResource nor a DirectLookup — `:wp proj-taskcluster/ci` used to
	// error ("does not support a scoped list") because switchResource fell
	// through to the scoped-list branch. It should instead be treated the
	// same as a DirectLookup id: open that entity's Detail view directly.
	s.switchResource("wp", "proj-taskcluster/ci")

	top, ok := s.stack.Top()
	if !ok || top.Kind != DetailKind || top.SelectedID != "proj-taskcluster/ci" || top.ResourceName != "workerpools" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourcePlainResourceWithoutIDShowsUnscopedList(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	s := New(registry)

	s.switchResource("wp", "")

	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.Scope != "" || top.ResourceName != "workerpools" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourceDirectScopedResourceWithIDPushesScopedList(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeDirectScopedResource{
		fakeScopedResource: fakeScopedResource{fakeResource: fakeResource{name: "taskgroup"}},
		label:              "task group id",
	})
	s := New(registry)

	s.switchResource("taskgroup", "grp-1")

	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.Scope != "grp-1" || top.ResourceName != "taskgroup" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourceHistoryPushesRatherThanResets(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	registry.Register(fakePeekResource{fakeResource{name: "history", aliases: []string{"hist"}}})
	s := New(registry)

	// Simulate having a screen open before `:history` is run.
	s.switchResource("wp", "")

	s.switchResource("history", "")

	if got := s.stack.Len(); got != 2 {
		t.Fatalf("expected history to be pushed onto the existing stack (len 2), got len %d", got)
	}

	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.ResourceName != "history" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}

	// Esc should return to the screen that was open before `:history`.
	s.goBack()
	top, ok = s.stack.Top()
	if !ok || top.ResourceName != "workerpools" {
		t.Fatalf("expected Esc from history to return to workerpools, got %+v (ok=%v)", top, ok)
	}
}

func TestNavigateToFromHistoryReplacesHistoryInsteadOfStackingOnTopOfIt(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools", aliases: []string{"wp"}})
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"},
		label:        "task id",
	})
	registry.Register(fakePeekResource{fakeResource{name: "history", aliases: []string{"hist"}}})
	s := New(registry)

	// Open a screen, then `:history`, then jump to a row's target — as if
	// the user picked a past visit from the history list.
	s.switchResource("wp", "")
	s.switchResource("history", "")
	s.navigateTo(resource.NavTarget{ResourceName: "task", ID: "task-1", Kind: resource.NavDetail})

	if got := s.stack.Len(); got != 2 {
		t.Fatalf("expected history to be replaced rather than stacked under the target (len 2), got len %d: %+v",
			got, s.stack.Views())
	}

	top, ok := s.stack.Top()
	if !ok || top.Kind != DetailKind || top.ResourceName != "task" || top.SelectedID != "task-1" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}

	// Esc should skip history entirely and land back on workerpools.
	s.goBack()
	top, ok = s.stack.Top()
	if !ok || top.ResourceName != "workerpools" {
		t.Fatalf("expected Esc to skip history and return to workerpools, got %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourceDirectScopedResourceWithoutIDOpensPrompt(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeDirectScopedResource{
		fakeScopedResource: fakeScopedResource{fakeResource: fakeResource{name: "taskgroup"}},
		label:              "task group id",
	})
	s := New(registry)

	s.switchResource("taskgroup", "")

	if s.footerMode != footerPrompt {
		t.Fatalf("expected footerMode footerPrompt, got %v", s.footerMode)
	}
	if got, want := s.footerInput.GetLabel(), "[yellow]task group id:[white] "; got != want {
		t.Fatalf("expected footer label %q, got %q", want, got)
	}

	// Entering an id in the prompt should push the scoped List view, not a
	// Detail view (unlike a plain DirectLookup's prompt).
	s.footerInput.SetText("grp-1")
	s.handleFooterInputDone(tcell.KeyEnter)

	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.Scope != "grp-1" || top.ResourceName != "taskgroup" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourceRootBrowsableWithoutIDBrowsesRoot(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeRootBrowsableResource{fakeDirectScopedResource{
		fakeScopedResource: fakeScopedResource{fakeResource: fakeResource{name: "index"}},
		label:              "namespace or full index path",
	}})
	s := New(registry)

	s.switchResource("index", "")

	if s.footerMode == footerPrompt {
		t.Fatalf("expected no id prompt for a root-browsable resource")
	}
	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.Scope != "" || top.ResourceName != "index" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourceRootBrowsableWithIDPushesScopedList(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeRootBrowsableResource{fakeDirectScopedResource{
		fakeScopedResource: fakeScopedResource{fakeResource: fakeResource{name: "index"}},
		label:              "namespace or full index path",
	}})
	s := New(registry)

	s.switchResource("index", "gecko.v2")

	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind || top.Scope != "gecko.v2" || top.ResourceName != "index" {
		t.Fatalf("unexpected top view: %+v (ok=%v)", top, ok)
	}
}

func TestSwitchResourceDirectLookupWithoutIDOpensPrompt(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeDirectLookupResource{
		fakeResource: fakeResource{name: "task"},
		label:        "task id",
	})
	s := New(registry)

	s.switchResource("task", "")

	if s.footerMode != footerPrompt {
		t.Fatalf("expected footerMode footerPrompt, got %v", s.footerMode)
	}
	if s.pendingLookupCommit == nil {
		t.Fatalf("expected pendingLookupCommit to be set")
	}
	if got, want := s.footerInput.GetLabel(), "[yellow]task id:[white] "; got != want {
		t.Fatalf("expected footer label %q, got %q", want, got)
	}
}

func TestRenderRestoredTopFallsBackToRootWhenStackIsEmpty(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	s := New(registry)
	s.restoreFallback = "workerpools"

	s.renderRestoredTop()

	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" || top.Kind != ListKind {
		t.Fatalf("expected root view on top, got %+v (ok=%v)", top, ok)
	}
}

func TestRenderRestoredTopDropsUnresolvableEntriesThenFallsBackToRoot(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	s := New(registry)
	s.restoreFallback = "workerpools"
	s.stack.Push(View{ResourceName: "long-gone-resource", Kind: ListKind})

	s.renderRestoredTop()

	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" || top.Kind != ListKind {
		t.Fatalf("expected root view on top after dropping the unresolvable entry, got %+v (ok=%v)", top, ok)
	}
}

// TestRenderRestoredTopRendersResolvableTopView confirms a resolvable
// restored view stays on top of the stack while its fetch is in flight. It
// no longer asserts on an "isRestore" flag directly: isRestore is now a
// plain function argument private to loadDetail's goroutine, not an
// externally observable field — and the argument's effect
// (recording/pop-and-retry decisions)
// lives entirely inside a QueueUpdateDraw callback, which never runs in
// these tests since s.app.Run() is never called (see the waitFor helper's
// comment above for the same, pre-existing limitation on the async
// success/failure paths in general).
func TestRenderRestoredTopRendersResolvableTopView(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	s := New(registry)
	s.restoreFallback = "workerpools"
	s.stack.Push(View{ResourceName: "workerpools", Kind: DetailKind, SelectedID: "abc"})

	s.renderRestoredTop()

	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" || top.Kind != DetailKind || top.SelectedID != "abc" {
		t.Fatalf("expected the restored detail view to remain on top, got %+v (ok=%v)", top, ok)
	}
}

func TestIsStaleLoadDetectsSupersededDispatchToIdenticalTarget(t *testing.T) {
	s := New(resource.NewRegistry())

	// Simulate two dispatches to the IDENTICAL target — e.g. a pending
	// restore-replay for (task, "A") and a manual `:task A` navigation
	// racing it, per the bug this guards against. Each dispatch increments
	// loadGeneration and captures its own value, exactly as loadList/
	// loadDetail do internally.
	s.loadGeneration++
	gen1 := s.loadGeneration // the older, soon-to-be-superseded dispatch

	s.loadGeneration++
	gen2 := s.loadGeneration // the newer dispatch, targeting the same View

	if !s.isStaleLoad(gen1) {
		t.Fatalf("expected the older dispatch (gen1=%d) to be stale once a newer one (gen2=%d) started", gen1, gen2)
	}
	if s.isStaleLoad(gen2) {
		t.Fatalf("expected the newer dispatch (gen2=%d) to still be current", gen2)
	}
}

func TestLoadDetailIncrementsGenerationOnEachNavigationDispatch(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeResource{name: "task"}

	before := s.loadGeneration
	s.loadDetail(res, "A", true, true, false) // e.g. a restore-replay dispatch

	if s.loadGeneration != before+1 {
		t.Fatalf("expected loadGeneration to increment by 1, got %d -> %d", before, s.loadGeneration)
	}
	firstGen := s.loadGeneration

	s.loadDetail(res, "A", true, false, false) // e.g. a manual re-navigation to the identical target

	if s.loadGeneration != firstGen+1 {
		t.Fatalf("expected loadGeneration to increment again, got %d -> %d", firstGen, s.loadGeneration)
	}
	// The first dispatch's captured generation (firstGen) is now stale,
	// even though it targeted the exact same (resource, id) as the second —
	// this is precisely the case isTopView's View-equality check cannot
	// distinguish on its own.
	if !s.isStaleLoad(firstGen) {
		t.Fatalf("expected the first dispatch's generation to be stale after a second dispatch to the identical target")
	}
}

func TestLoadListIncrementsGenerationOnEachNavigationDispatch(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeResource{name: "workerpools"}

	before := s.loadGeneration
	s.loadList(res, "", "", true, false, true) // e.g. a restore-replay dispatch

	if s.loadGeneration != before+1 {
		t.Fatalf("expected loadGeneration to increment by 1, got %d -> %d", before, s.loadGeneration)
	}
	firstGen := s.loadGeneration

	s.loadList(res, "", "", true, false, false) // e.g. a manual re-navigation to the identical target

	if s.loadGeneration != firstGen+1 {
		t.Fatalf("expected loadGeneration to increment again, got %d -> %d", firstGen, s.loadGeneration)
	}
	if !s.isStaleLoad(firstGen) {
		t.Fatalf("expected the first dispatch's generation to be stale after a second dispatch to the identical target")
	}
}

func TestLoadDetailBackgroundRefreshDoesNotBumpGeneration(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeResource{name: "task"}

	s.loadDetail(res, "A", true, false, false) // a genuine navigation dispatch (isInitial=true)
	genAfterInitial := s.loadGeneration

	s.loadDetail(res, "A", false, false, false) // a background refresh tick (isInitial=false) — must NOT bump generation

	if s.loadGeneration != genAfterInitial {
		t.Fatalf("expected a background refresh (isInitial=false) not to bump loadGeneration, got %d -> %d", genAfterInitial, s.loadGeneration)
	}
	// Critically: the initial dispatch's captured generation must still be
	// considered current after the refresh tick, since nothing has
	// actually superseded it as a navigation target.
	if s.isStaleLoad(genAfterInitial) {
		t.Fatalf("expected the initial dispatch's generation to remain current after a mere background refresh tick")
	}
}

func TestLoadListBackgroundRefreshDoesNotBumpGeneration(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeResource{name: "workerpools"}

	s.loadList(res, "", "", true, false, false) // a genuine navigation dispatch (isInitial=true)
	genAfterInitial := s.loadGeneration

	s.loadList(res, "", "", false, true, false) // a background refresh tick (isInitial=false, forceRefresh=true, as Invalidate dispatches it)

	if s.loadGeneration != genAfterInitial {
		t.Fatalf("expected a background refresh (isInitial=false) not to bump loadGeneration, got %d -> %d", genAfterInitial, s.loadGeneration)
	}
	if s.isStaleLoad(genAfterInitial) {
		t.Fatalf("expected the initial dispatch's generation to remain current after a mere background refresh tick")
	}
}

func TestNextLoadGenerationInheritsForRefreshButBumpsForNavigation(t *testing.T) {
	s := New(resource.NewRegistry())

	gen1 := s.nextLoadGeneration(true) // initial navigation to A

	genRefresh := s.nextLoadGeneration(false) // a same-target refresh tick
	if genRefresh != gen1 {
		t.Fatalf("expected a refresh dispatch to inherit the current generation (%d), got %d", gen1, genRefresh)
	}

	gen2 := s.nextLoadGeneration(true) // navigate away to B
	gen3 := s.nextLoadGeneration(true) // navigate back to A — a fresh re-visit

	if gen2 == gen1 || gen3 == gen2 {
		t.Fatalf("expected each navigation dispatch to bump the generation, got gen1=%d gen2=%d gen3=%d", gen1, gen2, gen3)
	}

	// The old refresh's captured generation must now be stale relative to
	// the fresh re-visit to A — even though isTopView would match again —
	// since two navigation dispatches have incremented the generation since
	// it was captured.
	if !s.isStaleLoad(genRefresh) {
		t.Fatalf("expected the old refresh's generation (%d) to be stale after two subsequent navigation dispatches (current %d)", genRefresh, s.loadGeneration)
	}
}

func TestNextLoadGenerationRefreshRemainsCurrentWithoutInterveningNavigation(t *testing.T) {
	s := New(resource.NewRegistry())

	s.nextLoadGeneration(true)                // initial navigation to A
	genRefresh := s.nextLoadGeneration(false) // a same-target refresh tick, nothing navigated away in between

	if s.isStaleLoad(genRefresh) {
		t.Fatalf("expected a refresh's captured generation to remain current when nothing has navigated away in the meantime")
	}
}

func TestGoBackAtRootIsNoOp(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	s := New(registry)
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})

	s.goBack()

	if s.stack.Len() != 1 {
		t.Fatalf("expected the root view to remain on the stack, got length %d", s.stack.Len())
	}
	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" {
		t.Fatalf("expected root view to remain on top, got %+v (ok=%v)", top, ok)
	}
}

func TestGoBackPopsOneLevelWhenNotAtRoot(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	registry.Register(fakeResource{name: "workers"})
	s := New(registry)
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})
	s.stack.Push(View{ResourceName: "workers", Kind: ListKind, Scope: "pool-a"})

	s.goBack()

	if s.stack.Len() != 1 {
		t.Fatalf("expected exactly one view left on the stack, got %d", s.stack.Len())
	}
	top, ok := s.stack.Top()
	if !ok || top.ResourceName != "workerpools" {
		t.Fatalf("expected to be back at workerpools, got %+v (ok=%v)", top, ok)
	}
}

func TestEscClearsActiveListFilterBeforeGoingBack(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	registry.Register(fakeResource{name: "workers"})
	s := New(registry)
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})
	s.stack.Push(View{ResourceName: "workers", Kind: ListKind, Scope: "pool-a"})
	s.currentListResource = "workers"
	s.filterQuery = "proj-task"
	s.filterByResource["workers"] = "proj-task"

	s.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if s.filterQuery != "" {
		t.Fatalf("expected the first Esc to clear the active filter, got %q", s.filterQuery)
	}
	if s.stack.Len() != 2 {
		t.Fatalf("expected the first Esc to leave the stack untouched, got length %d", s.stack.Len())
	}

	s.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if s.stack.Len() != 1 {
		t.Fatalf("expected the second Esc to pop the stack now that the filter is clear, got length %d", s.stack.Len())
	}
}

func TestEscClearsActiveDetailFilterBeforeGoingBack(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeResource{name: "workerpools"})
	registry.Register(fakeResource{name: "workers"})
	s := New(registry)
	s.stack.Push(View{ResourceName: "workerpools", Kind: ListKind})
	s.stack.Push(View{ResourceName: "workers", Kind: DetailKind, SelectedID: "worker-1"})
	s.detail.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	s.detail.SetFilterQuery("beta")
	s.content.SwitchToPage(pageDetail)

	s.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if s.detail.FilterQuery() != "" {
		t.Fatalf("expected the first Esc to clear the active detail filter, got %q", s.detail.FilterQuery())
	}
	if s.stack.Len() != 2 {
		t.Fatalf("expected the first Esc to leave the stack untouched, got length %d", s.stack.Len())
	}

	s.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if s.stack.Len() != 1 {
		t.Fatalf("expected the second Esc to pop the stack now that the filter is clear, got length %d", s.stack.Len())
	}
}

type fakeScopeActionsResource struct {
	fakeScopedResource
	actions []resource.DetailAction
}

func (f fakeScopeActionsResource) ScopeActions(scope string) []resource.DetailAction {
	return f.actions
}

func TestRenderListPopulatesDetailActionsFromScopeActions(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeScopeActionsResource{
		fakeScopedResource: fakeScopedResource{fakeResource: fakeResource{name: "workers"}},
		actions: []resource.DetailAction{
			{Key: 'e', Label: "errors", Target: resource.NavTarget{ResourceName: "errors", ID: "pool-a"}},
		},
	}

	s.renderList(res, "pool-a", false)

	if len(s.currentDetailActions) != 1 || s.currentDetailActions[0].Key != 'e' {
		t.Fatalf("expected ScopeActions to populate currentDetailActions, got %+v", s.currentDetailActions)
	}
}

func TestRenderListLeavesDetailActionsNilWithoutScopeActions(t *testing.T) {
	s := New(resource.NewRegistry())
	res := fakeResource{name: "workerpools"}

	s.renderList(res, "", false)

	if s.currentDetailActions != nil {
		t.Fatalf("expected nil currentDetailActions for a resource without ScopeActions, got %+v", s.currentDetailActions)
	}
}
