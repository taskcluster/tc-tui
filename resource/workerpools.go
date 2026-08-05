package resource

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/taskcluster/tc-tui/crash"
	"github.com/taskcluster/tc-tui/taskcluster"
)

// loadingPlaceholder fills the Pending/Claimed/Errors cells until Augment
// has enriched them — distinct from both a real number and a blank
// "unavailable" cell.
const loadingPlaceholder = "..."

type WorkerPoolsResource struct {
	tc taskcluster.Taskcluster
}

func NewWorkerPoolsResource(tc taskcluster.Taskcluster) *WorkerPoolsResource {
	return &WorkerPoolsResource{tc: tc}
}

func (r *WorkerPoolsResource) Name() string      { return "workerpools" }
func (r *WorkerPoolsResource) Aliases() []string { return []string{"wp", "pools"} }
func (r *WorkerPoolsResource) Description() string {
	return "Worker pool provisioning configuration — provider, capacity, launch config"
}

func (r *WorkerPoolsResource) Columns() []Column {
	return []Column{
		{Title: "WORKER POOL ID"},
		{Title: "PROVIDER", Width: 32},
		{Title: "CAPACITY", Width: 16},
		{Title: "REQUESTED", Width: 16},
		{Title: "PENDING", Width: 12},
		{Title: "CLAIMED", Width: 12},
		{Title: "ERRORS (7D)", Width: 14},
	}
}

// FacetColumn identifies the PROVIDER column (see Columns()).
func (r *WorkerPoolsResource) FacetColumn() int { return 1 }

// FacetOptions derives the distinct providers actually present in the
// already-loaded pool list — no listProviders call, since the full pool
// list (including provider) is already fetched for the table.
func (r *WorkerPoolsResource) FacetOptions(rows []Row) []string {
	seen := map[string]bool{}
	var options []string
	for _, row := range rows {
		p := row.Cells[r.FacetColumn()]
		if !seen[p] {
			seen[p] = true
			options = append(options, p)
		}
	}

	sort.Strings(options)
	return options
}

func (r *WorkerPoolsResource) List() ([]Row, error) {
	pools, err := r.tc.GetWorkerPools()
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(pools))
	for _, pool := range pools {
		rows = append(rows, Row{
			ID: pool.WorkerPoolID,
			Cells: []string{
				pool.WorkerPoolID,
				pool.ProviderID,
				fmt.Sprintf("%10d", pool.CurrentCapacity),
				fmt.Sprintf("%10d", pool.RequestedCapacity),
				loadingPlaceholder,
				loadingPlaceholder,
				loadingPlaceholder,
			},
		})
	}

	return rows, nil
}

// Augment enriches rows with Pending/Claimed (per pool, concurrently via
// GetTaskQueueCounts) and Errors (one bulk call), calling onUpdate after
// every individual piece of data arrives so the shell can redraw
// progressively instead of blocking until everything is in. See
// resource.Augmentable. wanted is threaded straight into GetTaskQueueCounts,
// which is the actual per-row cost at scale (one HTTP call per pool) —
// GetWorkerPoolErrorCounts is a single bulk call regardless of row count, so
// there's no API cost to save there by skipping a row, though wanted is
// still honored when writing that result in so a skipped row's cells are
// left untouched rather than filled in with data it was never "given".
func (r *WorkerPoolsResource) Augment(rows []Row, wanted func(id string) bool, onUpdate func(rows []Row, completed, total int)) {
	if len(rows) == 0 {
		return
	}

	// rows' Cells slices may alias the caller's own row state (the shell
	// keeps rows it hands to Augment and rows it renders backed by the same
	// arrays), so mutating them in place below — from these goroutines,
	// unsynchronized with whatever else reads that state on the UI thread —
	// would be a data race. Working on an independent copy from the start
	// avoids that regardless of what the caller does with its own rows.
	working := make([]Row, len(rows))
	for i, row := range rows {
		working[i] = Row{ID: row.ID, Cells: append([]string(nil), row.Cells...), NavTarget: row.NavTarget}
	}
	rows = working

	total := len(rows) + 1 // one tick per pool (pending/claimed) + one for the bulk errors call

	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		completed int
	)

	indexByID := make(map[string]int, len(rows))
	for i, row := range rows {
		indexByID[row.ID] = i
	}

	// tick must be called with mu already held by the caller, since it
	// reads the just-written cells as part of building the snapshot; it
	// releases mu around the onUpdate call (a caller-supplied function
	// shouldn't run while this resource holds its own internal lock) and
	// re-acquires it before returning, since callers' deferred mu.Unlock()
	// still expects to hold it.
	tick := func() {
		completed++
		snapshot := make([]Row, len(rows))
		copy(snapshot, rows)
		for i := range snapshot {
			snapshot[i].Cells = append([]string(nil), snapshot[i].Cells...)
		}
		c := completed
		mu.Unlock()
		onUpdate(snapshot, c, total)
		mu.Lock()
	}

	wg.Add(1)
	crash.Go(func() {
		defer wg.Done()

		errorCounts, err := r.tc.GetWorkerPoolErrorCounts()
		mu.Lock()
		for id, idx := range indexByID {
			if !wanted(id) {
				continue
			}
			if err == nil {
				rows[idx].Cells[6] = fmt.Sprintf("%10d", errorCounts[id])
			} else {
				rows[idx].Cells[6] = "" // the whole bulk call failed — unavailable, not a placeholder forever
			}
		}
		tick()
		mu.Unlock()
	})

	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}

	wg.Add(1)
	crash.Go(func() {
		defer wg.Done()
		r.tc.GetTaskQueueCounts(ids, wanted, func(id string, counts taskcluster.TaskQueueCounts) {
			mu.Lock()
			// wanted is rechecked here too — GetTaskQueueCounts's zero-value
			// TaskQueueCounts{} for a skipped id (both Known flags false) is
			// indistinguishable from a genuine fetch failure, and a
			// genuinely-unwanted row must be left untouched, not written as
			// "unavailable".
			if idx, ok := indexByID[id]; ok && wanted(id) {
				if counts.PendingKnown {
					rows[idx].Cells[4] = fmt.Sprintf("%10d", counts.Pending)
				} else {
					rows[idx].Cells[4] = "" // couldn't be obtained — unavailable, not a placeholder forever
				}
				if counts.ClaimedKnown {
					rows[idx].Cells[5] = fmt.Sprintf("%10d", counts.Claimed)
				} else {
					rows[idx].Cells[5] = ""
				}
			}
			tick()
			mu.Unlock()
		})
	})

	wg.Wait()
}

// workerPoolActions returns the standard set of quick-jump keys to a worker
// pool's sub-resources (workers/pending/claimed/launchconfigs/errors) and to
// the pool itself, scoped pool-wide to workerPoolID. exclude omits the
// action whose ResourceName matches — typically the resource currently
// showing the hints itself, since pressing its own key to "jump" to the
// view already on screen isn't useful. If exclude doesn't match any of the
// 7 ResourceNames (e.g. a typo), it has no effect and all 7 actions are
// returned.
func workerPoolActions(workerPoolID, exclude string) []DetailAction {
	all := []DetailAction{
		{Key: 'W', Label: "worker pool", Target: NavTarget{ResourceName: "workerpools", ID: workerPoolID, Kind: NavDetail}},
		{Key: 'w', Label: "workers", Target: NavTarget{ResourceName: "workers", ID: workerPoolID, Kind: NavScopedList}},
		{Key: 'p', Label: "pending", Target: NavTarget{ResourceName: "pending", ID: workerPoolID, Kind: NavScopedList}},
		{Key: 'c', Label: "claimed", Target: NavTarget{ResourceName: "claimed", ID: workerPoolID, Kind: NavScopedList}},
		{Key: 'l', Label: "launchconfigs", Target: NavTarget{ResourceName: "launchconfigs", ID: workerPoolID, Kind: NavScopedList}},
		{Key: 'e', Label: "errors", Target: NavTarget{ResourceName: "errors", ID: workerPoolID, Kind: NavScopedList}},
		{Key: 'P', Label: "purge cache", Target: NavTarget{ResourceName: "purgecache", ID: workerPoolID, Kind: NavScopedList}},
	}

	actions := make([]DetailAction, 0, len(all))
	for _, a := range all {
		if a.Target.ResourceName == exclude {
			continue
		}
		actions = append(actions, a)
	}
	return actions
}

func (r *WorkerPoolsResource) Describe(id string) (Detail, error) {
	pool, err := r.tc.GetWorkerPool(id)
	if err != nil {
		return Detail{}, err
	}

	// Best-effort summary lines: a failure in either just omits that line
	// rather than failing the whole Detail fetch, since these are
	// supplementary to the pool data rendered below.
	var summary strings.Builder
	if total, active, err := launchConfigCounts(r.tc, id); err == nil {
		summary.WriteString(fmt.Sprintf("[green]Launch configs:[blue] %d[white] (%d archived)\n", total, total-active))
	}
	if count, err := r.tc.GetWorkerPoolErrorCount(id); err == nil {
		summary.WriteString(fmt.Sprintf("[green]Errors (last 7d):[blue] %d[white]\n", count))
	}

	body := fmt.Sprintf(
		"[green]Description:[white]\n%s\n\n"+
			"[green]Created:[white] %s\n"+
			"[green]Owner:[white] %s\n\n"+
			"%s\n"+
			"%s"+
			"%s\n"+
			"[green]Config:[white]\n%s\n\n",
		renderMarkdown(pool.Description),
		pool.Created,
		pool.Owner,
		summary.String(),
		fieldRow(24, "Requested capacity", fmt.Sprint(pool.RequestedCapacity), "Running capacity", fmt.Sprint(pool.RunningCapacity), "Stopped capacity", fmt.Sprint(pool.StoppedCapacity)),
		fieldRow(24, "Running count", fmt.Sprint(pool.RunningCount), "Stopped count", fmt.Sprint(pool.StoppedCount)),
		renderYAML(pool.Config),
	)

	return Detail{
		Title:   fmt.Sprintf("Worker Pool :: %s", pool.WorkerPoolID),
		Body:    body,
		Actions: workerPoolActions(pool.WorkerPoolID, r.Name()),
	}, nil
}

// RefreshInterval is longer than most other list resources' (15s) — a
// refresh reloads the base rows and restarts Augment from scratch, and
// giving Augment more headroom before the next reload cuts down on how
// often a slow augmentation run gets cut off partway through.
func (r *WorkerPoolsResource) RefreshInterval() time.Duration {
	return 60 * time.Second
}

func (r *WorkerPoolsResource) ListWebURL(rootURL, scope string) string {
	return webUIPath(rootURL, "worker-manager")
}

func (r *WorkerPoolsResource) DetailWebURL(rootURL, id string) string {
	return webUIPath(rootURL, "worker-manager/"+pathSegment(id))
}
