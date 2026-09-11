package resource

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcworkermanager"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestWorkerPoolsResourceList(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPools: taskcluster.WorkerPoolList{
			{
				WorkerPoolID:      "proj/pool-a",
				ProviderID:        "gcp",
				CurrentCapacity:   3,
				RequestedCapacity: 5,
			},
		},
	}
	res := NewWorkerPoolsResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ID != "proj/pool-a" {
		t.Fatalf("unexpected id: %s", rows[0].ID)
	}
	if rows[0].Cells[0] != "proj/pool-a" || rows[0].Cells[1] != "gcp" ||
		strings.TrimSpace(rows[0].Cells[2]) != "3" || strings.TrimSpace(rows[0].Cells[3]) != "5" {
		t.Fatalf("unexpected cells: %+v", rows[0].Cells)
	}
}

func TestWorkerPoolsResourceListShowsLoadingPlaceholdersForSlowColumns(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPools: taskcluster.WorkerPoolList{
			{WorkerPoolID: "proj/pool-a", ProviderID: "gcp"},
		},
	}
	res := NewWorkerPoolsResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows[0].Cells) != 7 {
		t.Fatalf("expected 7 cells, got %d: %+v", len(rows[0].Cells), rows[0].Cells)
	}
	for _, i := range []int{4, 5, 6} {
		if rows[0].Cells[i] != loadingPlaceholder {
			t.Fatalf("expected cell %d to be the loading placeholder, got %q", i, rows[0].Cells[i])
		}
	}
}

// alwaysWanted is the wanted callback for tests that don't exercise
// Augment's skip-unwanted-rows behavior — every row is fetched
// unconditionally, matching Augment's old (pre-wanted) behavior.
func alwaysWanted(id string) bool { return true }

// collectAugmentUpdates runs Augment to completion (it blocks until done)
// and returns every onUpdate call it made, in the order received. Augment
// runs its two enrichment paths concurrently, so only the LAST call's
// snapshot (the fully-merged state) and the total call count are safe to
// assert on — never intermediate ordering between the two paths.
func collectAugmentUpdates(res *WorkerPoolsResource, rows []Row) [][]Row {
	return collectAugmentUpdatesWithWanted(res, rows, alwaysWanted)
}

func collectAugmentUpdatesWithWanted(res *WorkerPoolsResource, rows []Row, wanted func(id string) bool) [][]Row {
	var (
		mu      sync.Mutex
		updates [][]Row
	)
	res.Augment(rows, wanted, func(updated []Row, completed, total int) {
		mu.Lock()
		updates = append(updates, updated)
		mu.Unlock()
	}, func(string) {})
	return updates
}

func TestWorkerPoolsResourceAugmentFillsInAllThreeColumns(t *testing.T) {
	fake := &fakeTaskcluster{
		taskQueueCounts: map[string]taskcluster.TaskQueueCounts{
			"proj/pool-a": {Pending: 7, PendingKnown: true, Claimed: 3, ClaimedKnown: true},
		},
		workerPoolErrorCounts: map[string]int{"proj/pool-a": 2},
	}
	res := NewWorkerPoolsResource(fake)
	rows := []Row{{ID: "proj/pool-a", Cells: []string{"proj/pool-a", "gcp", "0", "0", loadingPlaceholder, loadingPlaceholder, loadingPlaceholder}}}

	updates := collectAugmentUpdates(res, rows)

	if len(updates) != 2 { // 1 pool tick + 1 bulk-errors tick
		t.Fatalf("expected 2 onUpdate calls, got %d", len(updates))
	}
	last := updates[len(updates)-1][0]
	if strings.TrimSpace(last.Cells[4]) != "7" || strings.TrimSpace(last.Cells[5]) != "3" || strings.TrimSpace(last.Cells[6]) != "2" {
		t.Fatalf("unexpected final cells: %+v", last.Cells)
	}
}

func TestWorkerPoolsResourceAugmentRendersZeroErrorsForPoolAbsentFromSuccessfulBulkResult(t *testing.T) {
	fake := &fakeTaskcluster{workerPoolErrorCounts: map[string]int{}} // succeeds, but omits pool-a
	res := NewWorkerPoolsResource(fake)
	rows := []Row{{ID: "proj/pool-a", Cells: []string{"proj/pool-a", "gcp", "0", "0", loadingPlaceholder, loadingPlaceholder, loadingPlaceholder}}}

	updates := collectAugmentUpdates(res, rows)

	last := updates[len(updates)-1][0]
	if strings.TrimSpace(last.Cells[6]) != "0" {
		t.Fatalf("expected 0 errors for a pool absent from a successful bulk result, got %q", last.Cells[6])
	}
}

func TestWorkerPoolsResourceAugmentLeavesColumnsBlankWhenNothingObtained(t *testing.T) {
	fake := &fakeTaskcluster{workerPoolErrorCountsErr: errors.New("boom")} // taskQueueCounts left nil too
	res := NewWorkerPoolsResource(fake)
	rows := []Row{{ID: "proj/pool-a", Cells: []string{"proj/pool-a", "gcp", "0", "0", loadingPlaceholder, loadingPlaceholder, loadingPlaceholder}}}

	updates := collectAugmentUpdates(res, rows)

	last := updates[len(updates)-1][0]
	for _, i := range []int{4, 5, 6} {
		if strings.TrimSpace(last.Cells[i]) != "" {
			t.Fatalf("expected cell %d blank when nothing was obtained, got %q", i, last.Cells[i])
		}
	}
}

// A row wanted rejects must not get real data written in for its
// Pending/Claimed/Errors columns — it should be left exactly as it came in,
// since the point of wanted is to skip spending API calls (and progress) on
// rows that are no longer visible (e.g. a filter narrowed after a big
// unfiltered batch was already dispatched).
func TestWorkerPoolsResourceAugmentSkipsRowsWantedRejects(t *testing.T) {
	fake := &fakeTaskcluster{
		taskQueueCounts: map[string]taskcluster.TaskQueueCounts{
			"proj/pool-a": {Pending: 7, PendingKnown: true, Claimed: 3, ClaimedKnown: true},
			"proj/pool-b": {Pending: 9, PendingKnown: true, Claimed: 1, ClaimedKnown: true},
		},
		workerPoolErrorCounts: map[string]int{"proj/pool-a": 2, "proj/pool-b": 5},
	}
	res := NewWorkerPoolsResource(fake)
	rows := []Row{
		{ID: "proj/pool-a", Cells: []string{"proj/pool-a", "gcp", "0", "0", loadingPlaceholder, loadingPlaceholder, loadingPlaceholder}},
		{ID: "proj/pool-b", Cells: []string{"proj/pool-b", "gcp", "0", "0", loadingPlaceholder, loadingPlaceholder, loadingPlaceholder}},
	}

	wanted := func(id string) bool { return id == "proj/pool-a" } // pool-b is no longer visible
	updates := collectAugmentUpdatesWithWanted(res, rows, wanted)

	last := updates[len(updates)-1]
	var a, b Row
	for _, row := range last {
		switch row.ID {
		case "proj/pool-a":
			a = row
		case "proj/pool-b":
			b = row
		}
	}
	if strings.TrimSpace(a.Cells[4]) != "7" || strings.TrimSpace(a.Cells[5]) != "3" || strings.TrimSpace(a.Cells[6]) != "2" {
		t.Fatalf("expected wanted pool-a to be fully augmented, got %+v", a.Cells)
	}
	if b.Cells[4] != loadingPlaceholder || b.Cells[5] != loadingPlaceholder || b.Cells[6] != loadingPlaceholder {
		t.Fatalf("expected unwanted pool-b to be left untouched, got %+v", b.Cells)
	}
}

func TestWorkerPoolsResourceListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workerPoolsErr: wantErr}
	res := NewWorkerPoolsResource(fake)

	_, err := res.List()
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWorkerPoolsResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPool: &tcworkermanager.WorkerPoolFullDefinition{
			WorkerPoolID:      "proj/pool-a",
			ProviderID:        "gcp",
			Description:       "a pool",
			Owner:             "owner@example.com",
			Created:           tcclient.Time(time.Now()),
			RequestedCapacity: 5,
			RunningCapacity:   3,
			StoppedCapacity:   2,
			RunningCount:      3,
			StoppedCount:      1,
		},
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{LaunchConfigID: "lc-1", IsArchived: false},
			{LaunchConfigID: "lc-2", IsArchived: true},
		},
		errorCount: 4,
	}
	res := NewWorkerPoolsResource(fake)

	detail, err := res.Describe("proj/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Worker Pool :: proj/pool-a" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if len(detail.Actions) != 6 {
		t.Fatalf("expected 6 actions, got %d", len(detail.Actions))
	}
	if a := detail.Actions[0]; a.Key != 'w' || a.Target.ResourceName != "workers" ||
		a.Target.ID != "proj/pool-a" || a.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action[0]: %+v", a)
	}
	if a := detail.Actions[1]; a.Key != 'p' || a.Target.ResourceName != "pending" ||
		a.Target.ID != "proj/pool-a" || a.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action[1]: %+v", a)
	}
	if a := detail.Actions[2]; a.Key != 'c' || a.Target.ResourceName != "claimed" ||
		a.Target.ID != "proj/pool-a" || a.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action[2]: %+v", a)
	}
	if a := detail.Actions[3]; a.Key != 'l' || a.Target.ResourceName != "launchconfigs" ||
		a.Target.ID != "proj/pool-a" || a.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action[3]: %+v", a)
	}
	if a := detail.Actions[4]; a.Key != 'e' || a.Target.ResourceName != "errors" ||
		a.Target.ID != "proj/pool-a" || a.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action[4]: %+v", a)
	}
	if a := detail.Actions[5]; a.Key != 'P' || a.Target.ResourceName != "purgecache" ||
		a.Target.ID != "proj/pool-a" || a.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action[5]: %+v", a)
	}
	body := stripRegionTags(detail.Body)
	if !strings.Contains(body, "a pool") || !strings.Contains(body, "owner@example.com") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "Launch configs:[blue] 2[white] (1 archived)") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "Errors (last 7d):[blue] 4[white]") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
}

func TestWorkerPoolsResourceDescribeSummaryLinesComeBeforeConfig(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPool: &tcworkermanager.WorkerPoolFullDefinition{
			WorkerPoolID: "proj/pool-a",
			Config:       []byte(`{"minCapacity":1}`),
		},
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{LaunchConfigID: "lc-1", IsArchived: false},
		},
		errorCount: 2,
	}
	res := NewWorkerPoolsResource(fake)

	detail, err := res.Describe("proj/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	launchConfigsIdx := strings.Index(detail.Body, "Launch configs:")
	errorsIdx := strings.Index(detail.Body, "Errors (last 7d):")
	configIdx := strings.Index(detail.Body, "Config:")
	if launchConfigsIdx == -1 || errorsIdx == -1 || configIdx == -1 {
		t.Fatalf("expected all three sections present, got body: %s", detail.Body)
	}
	if !(launchConfigsIdx < configIdx && errorsIdx < configIdx) {
		t.Fatalf("expected Launch configs/Errors summary before Config, got body: %s", detail.Body)
	}
}

func TestWorkerPoolsResourceDescribeRendersConfigAsYAML(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPool: &tcworkermanager.WorkerPoolFullDefinition{
			WorkerPoolID: "proj/pool-a",
			Description:  "a pool",
			Config:       []byte(`{"minCapacity":1,"maxCapacity":5}`),
		},
	}
	res := NewWorkerPoolsResource(fake)

	detail, err := res.Describe("proj/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "minCapacity") {
		t.Fatalf("expected the rendered config in the body, got: %s", detail.Body)
	}
	if strings.Contains(detail.Body, `{"minCapacity":1,"maxCapacity":5}`) {
		t.Fatalf("expected the raw single-line JSON blob to be gone, got: %s", detail.Body)
	}
}

func TestWorkerPoolsResourceDescribeOmitsSummaryLinesOnError(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPool: &tcworkermanager.WorkerPoolFullDefinition{
			WorkerPoolID: "proj/pool-a",
			Description:  "a pool",
		},
		launchConfigsErr: errors.New("boom"),
		errorCountErr:    errors.New("boom"),
	}
	res := NewWorkerPoolsResource(fake)

	detail, err := res.Describe("proj/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "Launch configs:") || strings.Contains(detail.Body, "Errors (last 7d):") {
		t.Fatalf("expected summary lines to be omitted on error, got body: %s", detail.Body)
	}
}

func TestWorkerPoolsResourceDescribePartialSummaryFailure(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPool: &tcworkermanager.WorkerPoolFullDefinition{
			WorkerPoolID: "proj/pool-a",
			Description:  "a pool",
		},
		launchConfigsErr: errors.New("boom"),
		errorCount:       7,
	}
	res := NewWorkerPoolsResource(fake)

	detail, err := res.Describe("proj/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "Launch configs:") {
		t.Fatalf("expected launch configs summary line to be omitted, got body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "Errors (last 7d):[blue] 7[white]") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
}

func TestWorkerPoolsResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workerPoolErr: wantErr}
	res := NewWorkerPoolsResource(fake)

	_, err := res.Describe("proj/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWorkerPoolsResourceFacetColumn(t *testing.T) {
	res := NewWorkerPoolsResource(&fakeTaskcluster{})

	if got := res.FacetColumn(); got != 1 {
		t.Fatalf("expected column 1 (PROVIDER), got %d", got)
	}
}

func TestWorkerPoolsResourceFacetOptionsDedupsAndSorts(t *testing.T) {
	res := NewWorkerPoolsResource(&fakeTaskcluster{})
	rows := []Row{
		{ID: "a", Cells: []string{"proj/pool-a", "gcp", "1", "1"}},
		{ID: "b", Cells: []string{"proj/pool-b", "aws", "1", "1"}},
		{ID: "c", Cells: []string{"proj/pool-c", "gcp", "1", "1"}},
	}

	got := res.FacetOptions(rows)
	want := []string{"aws", "gcp"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestWorkerPoolsResourceFacetOptionsEmptyRows(t *testing.T) {
	res := NewWorkerPoolsResource(&fakeTaskcluster{})

	got := res.FacetOptions(nil)
	if len(got) != 0 {
		t.Fatalf("expected no options, got %v", got)
	}
}

func TestWorkerPoolsResourceRefreshInterval(t *testing.T) {
	res := NewWorkerPoolsResource(&fakeTaskcluster{})

	if got, want := res.RefreshInterval(), 60*time.Second; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWorkerPoolActionsWithNoExclusionReturnsAllSeven(t *testing.T) {
	actions := workerPoolActions("proj/pool-a", "")

	if len(actions) != 7 {
		t.Fatalf("expected all 7 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ID != "proj/pool-a" {
			t.Fatalf("unexpected action: %+v", a)
		}
		if a.Target.ResourceName == "workerpools" {
			if a.Target.Kind != NavDetail {
				t.Fatalf("expected the \"worker pool\" action to be a Detail nav, got %+v", a)
			}
		} else if a.Target.Kind != NavScopedList {
			t.Fatalf("unexpected action: %+v", a)
		}
	}
}

func TestWorkerPoolActionsExcludesGivenResource(t *testing.T) {
	actions := workerPoolActions("proj/pool-a", "workers")

	if len(actions) != 6 {
		t.Fatalf("expected 6 actions (7 minus excluded), got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ResourceName == "workers" {
			t.Fatalf("expected the \"workers\" action to be excluded, got %+v", actions)
		}
	}
}

func TestWorkerPoolActionsWithUnmatchedExcludeReturnsAllSeven(t *testing.T) {
	actions := workerPoolActions("proj/pool-a", "not-a-real-resource")

	if len(actions) != 7 {
		t.Fatalf("expected an unmatched exclude to leave all 7 actions, got %d: %+v", len(actions), actions)
	}
}

func TestWorkerPoolActionsIncludesPurgeCache(t *testing.T) {
	actions := workerPoolActions("proj/pool-a", "")

	for _, a := range actions {
		if a.Key == 'P' && a.Target.ResourceName == "purgecache" && a.Target.Kind == NavScopedList {
			return
		}
	}
	t.Fatalf("expected a 'P' purgecache action, got: %+v", actions)
}

// Augment must hand a give-up-entirely counts failure (a scope denial) to
// onWarn — the count columns are blank either way, so the reason is the only
// thing distinguishing "your credential can't read this" from "still
// loading".
func TestWorkerPoolsResourceAugmentWarnsWhenCountsAreDenied(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPools:           taskcluster.WorkerPoolList{{WorkerPoolID: "proj/pool", ProviderID: "gcp"}},
		workerPoolErrorCounts: map[string]int{"proj/pool": 0},
		taskQueueCountsErr:    taskcluster.ErrTaskQueueCountScopes,
	}
	res := NewWorkerPoolsResource(fake)
	rows, err := res.List()
	if err != nil {
		t.Fatal(err)
	}

	var warnings []string
	res.Augment(rows, alwaysWanted, func([]Row, int, int) {}, func(msg string) {
		warnings = append(warnings, msg)
	})

	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if !strings.Contains(warnings[0], "queue:claimed-count") || !strings.HasPrefix(warnings[0], "pending/claimed") {
		t.Errorf("warning %q should name the column and the missing scope", warnings[0])
	}
}

// The ordinary path must stay quiet — a warning on every refresh would
// train the user to ignore the line it appears on.
func TestWorkerPoolsResourceAugmentDoesNotWarnOnSuccess(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPools:           taskcluster.WorkerPoolList{{WorkerPoolID: "proj/pool", ProviderID: "gcp"}},
		workerPoolErrorCounts: map[string]int{"proj/pool": 0},
		taskQueueCounts: map[string]taskcluster.TaskQueueCounts{
			"proj/pool": {Pending: 1, PendingKnown: true, Claimed: 2, ClaimedKnown: true},
		},
	}
	res := NewWorkerPoolsResource(fake)
	rows, err := res.List()
	if err != nil {
		t.Fatal(err)
	}

	res.Augment(rows, alwaysWanted, func([]Row, int, int) {}, func(msg string) {
		t.Errorf("unexpected warning: %q", msg)
	})
}
