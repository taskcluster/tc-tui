package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/taskcluster/tc-tui/resource"
	"github.com/taskcluster/tc-tui/taskcluster"
)

// fakeWorkerPoolsTC implements just enough of taskcluster.Taskcluster for
// WorkerPoolsResource's List/Augment — every other method panics via the nil
// embedded interface if ever called, which would fail the test loudly rather
// than silently.
type fakeWorkerPoolsTC struct {
	taskcluster.Taskcluster
	pools              taskcluster.WorkerPoolList
	errorCounts        map[string]int
	taskQueueCountsErr error
}

func (f *fakeWorkerPoolsTC) GetWorkerPools() (taskcluster.WorkerPoolList, error) {
	return f.pools, nil
}

func (f *fakeWorkerPoolsTC) GetWorkerPoolErrorCounts() (map[string]int, error) {
	return f.errorCounts, nil
}

func (f *fakeWorkerPoolsTC) GetTaskQueueCounts(workerPoolIDs []string, wanted func(workerPoolID string) bool, onEach func(workerPoolID string, counts taskcluster.TaskQueueCounts)) error {
	for _, id := range workerPoolIDs {
		if !wanted(id) {
			onEach(id, taskcluster.TaskQueueCounts{})
			continue
		}
		onEach(id, taskcluster.TaskQueueCounts{PendingKnown: true, Pending: 1, ClaimedKnown: true, Claimed: 2})
	}
	return f.taskQueueCountsErr
}

// This is the exact scenario reported as still broken: open a list with a
// filter already narrowed (e.g. restored from a prior session), then widen
// it back to unfiltered — the pool that was hidden at load time must still
// get augmented once it becomes visible, using the real WorkerPoolsResource
// rather than a hand-rolled fake.
func TestWorkerPoolsClearingFilterAugmentsRevealedPool(t *testing.T) {
	fake := &fakeWorkerPoolsTC{
		pools: taskcluster.WorkerPoolList{
			{WorkerPoolID: "proj/pool-a", ProviderID: "gcp"},
			{WorkerPoolID: "proj/pool-b", ProviderID: "gcp"},
		},
		errorCounts: map[string]int{"proj/pool-a": 0, "proj/pool-b": 0},
	}
	res := resource.NewWorkerPoolsResource(fake)

	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = res.Name()
	s.currentColumns = res.Columns()
	s.filterQuery = "pool-a"
	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	waitFor(t, func() bool { return rowAugmented(s, "proj/pool-a") })
	if rowAugmented(s, "proj/pool-b") {
		t.Fatalf("expected filtered-out pool-b to NOT be augmented yet")
	}

	s.app.QueueUpdateDraw(func() {
		s.filterQuery = ""
		s.refreshTable()
	})

	waitFor(t, func() bool { return rowAugmented(s, "proj/pool-b") })
}

// A filter restored from a prior session (or just typed immediately after
// opening the list) that targets an augmented column — Errors, here — must
// not permanently show zero rows just because no row's base/placeholder
// cells happen to contain the query text. Real WorkerPoolsResource, real
// filter-driven fallback path, not a hand-rolled shortcut.
func TestFilterOnAugmentedColumnEventuallyFindsMatchViaFallback(t *testing.T) {
	fake := &fakeWorkerPoolsTC{
		pools: taskcluster.WorkerPoolList{
			{WorkerPoolID: "proj/alpha", ProviderID: "gcp"},
			{WorkerPoolID: "proj/beta", ProviderID: "gcp"},
		},
		errorCounts: map[string]int{"proj/alpha": 0, "proj/beta": 5},
	}
	res := resource.NewWorkerPoolsResource(fake)

	registry := resource.NewRegistry()
	registry.Register(res)

	// renderList mutates tview state directly (e.g. SetFocus) on whatever
	// goroutine calls it — safe here since the app isn't running yet, but
	// would race a live Draw() loop. startRunning comes after, once that's
	// done; its own pending QueueUpdateDraw call (from the async fetch this
	// kicks off) just waits until then to be drained.
	s := New(registry)
	s.filterByResource[res.Name()] = "5" // simulates a filter restored from a prior session
	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind})

	s.renderList(res, "", false)
	startRunning(t, s)

	waitFor(t, func() bool {
		var found bool
		s.app.QueueUpdateDraw(func() {
			for _, row := range FilterRows(s.lastRows, s.filterQuery) {
				if row.ID == "proj/beta" {
					found = true
				}
			}
		})
		return found
	})
}

// A row matching via a base column (its pool ID happens to contain "5")
// must not stop a DIFFERENT row from also being checked against an
// augmented column (Errors = 5) — a non-empty visible set on its own says
// nothing about whether every possible match has been found. If the
// fallback only fired on a totally-empty visible set, proj/beta here would
// never get augmented and would never show up for this query, even though
// it's a genuine match.
func TestFilterMatchingBaseColumnDoesNotHideAugmentedColumnMatch(t *testing.T) {
	fake := &fakeWorkerPoolsTC{
		pools: taskcluster.WorkerPoolList{
			{WorkerPoolID: "proj/pool-5", ProviderID: "gcp"}, // matches "5" via its own ID (base column)
			{WorkerPoolID: "proj/beta", ProviderID: "gcp"},   // matches "5" only via Errors (augmented column)
		},
		errorCounts: map[string]int{"proj/pool-5": 0, "proj/beta": 5},
	}
	res := resource.NewWorkerPoolsResource(fake)

	registry := resource.NewRegistry()
	registry.Register(res)

	s := New(registry)
	s.filterByResource[res.Name()] = "5"
	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind})

	s.renderList(res, "", false)
	startRunning(t, s)

	// Sanity check: proj/pool-5 should be visible right away via its own ID
	// (a base column), well before any augmentation completes — confirming
	// visible is genuinely non-empty for this query, which is exactly the
	// condition that used to disable the fallback entirely.
	waitFor(t, func() bool {
		var found bool
		s.app.QueueUpdateDraw(func() {
			for _, row := range FilterRows(s.lastRows, s.filterQuery) {
				if row.ID == "proj/pool-5" {
					found = true
				}
			}
		})
		return found
	})

	waitFor(t, func() bool {
		var found bool
		s.app.QueueUpdateDraw(func() {
			for _, row := range FilterRows(s.lastRows, s.filterQuery) {
				if row.ID == "proj/beta" {
					found = true
				}
			}
		})
		return found
	})
}

// Guards against the fallback getting too eager: a purely alphabetic query
// that already matches something via a base column (here, ProviderID) must
// NOT trigger the fallback at all — otherwise every filtered view of a
// large list (e.g. fxci's 400+ pools filtered down to one provider) would
// background-augment the rest of the list regardless, reintroducing the
// wasted-API-call cost `wanted` exists to avoid.
func TestFilterMatchingBaseColumnAlphabeticQueryDoesNotTriggerFallback(t *testing.T) {
	fake := &fakeWorkerPoolsTC{
		pools: taskcluster.WorkerPoolList{
			{WorkerPoolID: "proj/pool-a", ProviderID: "gcp"},
			{WorkerPoolID: "proj/pool-b", ProviderID: "aws"},
		},
		errorCounts: map[string]int{"proj/pool-a": 0, "proj/pool-b": 0},
	}
	res := resource.NewWorkerPoolsResource(fake)

	registry := resource.NewRegistry()
	registry.Register(res)

	s := New(registry)
	s.filterByResource[res.Name()] = "gcp"
	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind})

	s.renderList(res, "", false)
	startRunning(t, s)

	waitFor(t, func() bool { return rowAugmented(s, "proj/pool-a") })

	// Give any (incorrect) fallback a moment to have kicked in — everything
	// here resolves synchronously in the fake, so this is generous, not a
	// timing-sensitive assertion.
	time.Sleep(50 * time.Millisecond)
	if rowAugmented(s, "proj/pool-b") {
		t.Fatalf("expected non-matching pool-b to remain un-augmented for a purely alphabetic query")
	}
}

// rowAugmented reports whether id's row in s.lastRows has moved past the
// "..." loading placeholder in its PENDING column (index 4 — see
// WorkerPoolsResource.Columns). Reads s.lastRows via QueueUpdateDraw — with
// the app actually running (newRunningTestShell), s.lastRows is UI-thread
// state, and QueueUpdateDraw blocks until its closure has run, so this is
// synchronized the same way the polling in waitFor is meant to be.
func rowAugmented(s *Shell, id string) bool {
	var augmented bool
	s.app.QueueUpdateDraw(func() {
		for _, row := range s.lastRows {
			if row.ID == id {
				augmented = row.Cells[4] != "..." && row.Cells[4] != ""
				return
			}
		}
	})
	return augmented
}

// A credential that can't read pending/claimed counts leaves both columns
// empty, which on its own looks indistinguishable from a list that's still
// loading. The reason an Augmentable reports has to reach the footer — and
// has to survive the final tick's redraw, which rewrites the very line it
// lands on.
func TestScopeDeniedCountsWarningReachesTheFooter(t *testing.T) {
	fake := &fakeWorkerPoolsTC{
		pools: taskcluster.WorkerPoolList{
			{WorkerPoolID: "proj/pool-a", ProviderID: "gcp"},
		},
		errorCounts:        map[string]int{"proj/pool-a": 0},
		taskQueueCountsErr: taskcluster.ErrTaskQueueCountScopes,
	}
	res := resource.NewWorkerPoolsResource(fake)

	registry := resource.NewRegistry()
	registry.Register(res)

	s := newRunningTestShell(t, registry)
	s.currentListResource = res.Name()
	s.currentColumns = res.Columns()
	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind})

	s.loadList(res, "", "", true, false, false)

	waitFor(t, func() bool {
		got := readOnUI(s, func() string { return s.footerBreadcrumb.GetText(true) })
		return strings.Contains(got, "queue:claimed-count")
	})
}
