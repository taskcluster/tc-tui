package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcworkermanager"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestWorkersResourceScopedList(t *testing.T) {
	fake := &fakeTaskcluster{
		workers: taskcluster.WorkerList{
			{
				WorkerPoolID: "gcp/pool-a",
				WorkerGroup:  "us-west1",
				WorkerID:     "i-1234",
				State:        "running",
				Capacity:     1,
			},
		},
	}
	res := NewWorkersResource(fake)

	rows, err := res.ScopedList("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ID != "gcp/pool-a::us-west1::i-1234" {
		t.Fatalf("unexpected id: %s", rows[0].ID)
	}
	if rows[0].Cells[0] != "running" || rows[0].Cells[1] != "us-west1" || rows[0].Cells[2] != "i-1234" || rows[0].Cells[3] != "1" {
		t.Fatalf("unexpected cells: %+v", rows[0].Cells)
	}
}

func TestWorkersResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workersErr: wantErr}
	res := NewWorkersResource(fake)

	_, err := res.ScopedList("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWorkersResourceFacetOptions(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	got := res.FacetOptions()
	want := []string{"running", "requested", "stopping", "stopped"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestWorkersResourceFacetListPassesStateThrough(t *testing.T) {
	fake := &fakeTaskcluster{
		workers: taskcluster.WorkerList{
			{WorkerPoolID: "gcp/pool-a", WorkerGroup: "us-west1", WorkerID: "i-1234", State: "stopped", Capacity: 1},
		},
	}
	res := NewWorkersResource(fake)

	rows, err := res.FacetList("gcp/pool-a", "stopped")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workersState != "stopped" {
		t.Fatalf("expected state %q passed to the API, got %q", "stopped", fake.workersState)
	}
	if len(rows) != 1 || rows[0].Cells[0] != "stopped" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestWorkersResourceFacetListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workersErr: wantErr}
	res := NewWorkersResource(fake)

	_, err := res.FacetList("gcp/pool-a", "running")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWorkersResourceFacetCounts(t *testing.T) {
	fake := &fakeTaskcluster{
		stateCounts: map[string]int{"running": 12, "stopped": 14302, "stopping": 1, "requested": 0},
	}
	res := NewWorkersResource(fake)

	counts, err := res.FacetCounts("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts["running"] != 12 || counts["stopped"] != 14302 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}

func TestWorkersResourceFacetCountsError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{stateCountsErr: wantErr}
	res := NewWorkersResource(fake)

	_, err := res.FacetCounts("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWorkersResourceScopedListDelegatesToDefaultFacet(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewWorkersResource(fake)

	if _, err := res.ScopedList("gcp/pool-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workersState != "running" {
		t.Fatalf("expected ScopedList to default to the first facet option %q, got %q", "running", fake.workersState)
	}
}

func TestWorkersResourceListReturnsScopeRequiredError(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestWorkersResourceEmptyScopeResource(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "workerpools" {
		t.Fatalf("expected %q, got %q", "workerpools", got)
	}
}

func TestWorkersResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		worker: &tcworkermanager.WorkerFullDefinition{
			WorkerPoolID:   "gcp/pool-a",
			WorkerGroup:    "us-west1",
			WorkerID:       "i-1234",
			State:          "running",
			Capacity:       1,
			LaunchConfigID: "lc-1",
			Created:        tcclient.Time(time.Now()),
			LastModified:   tcclient.Time(time.Now()),
			LastChecked:    tcclient.Time(time.Now()),
			Expires:        tcclient.Time(time.Now()),
		},
	}
	res := NewWorkersResource(fake)

	detail, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Worker :: i-1234" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if !strings.Contains(detail.Body, "running") || !strings.Contains(detail.Body, "lc-1") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
	if len(detail.Actions) != 6 {
		t.Fatalf("expected 6 sibling actions, got %d: %+v", len(detail.Actions), detail.Actions)
	}
}

func TestWorkersResourceDescribeIncludesRecentTasks(t *testing.T) {
	fake := &fakeTaskcluster{
		worker: &tcworkermanager.WorkerFullDefinition{
			WorkerPoolID: "gcp/pool-a",
			WorkerGroup:  "us-west1",
			WorkerID:     "i-1234",
		},
		workerRecentTasks: []tcqueue.TaskRun{
			{TaskID: "task-1", RunID: 0},
			{TaskID: "task-2", RunID: 1},
		},
	}
	res := NewWorkersResource(fake)

	detail, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "Recent Tasks (2):") || !strings.Contains(detail.Body, "task-1 (run 0)") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
}

func TestWorkersResourceDescribeShowsRecentTasksUnavailableOnError(t *testing.T) {
	fake := &fakeTaskcluster{
		worker: &tcworkermanager.WorkerFullDefinition{
			WorkerPoolID: "gcp/pool-a",
			WorkerGroup:  "us-west1",
			WorkerID:     "i-1234",
		},
		workerRecentTasksErr: errors.New("boom"),
	}
	res := NewWorkersResource(fake)

	detail, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "Recent Tasks:") || !strings.Contains(detail.Body, "unavailable") ||
		!strings.Contains(detail.Body, "boom") {
		t.Fatalf("expected an unavailable recent-tasks section naming the error, got body: %s", detail.Body)
	}
}

func TestWorkersResourceDescribeShowsRecentTasksActionWhenTasksExist(t *testing.T) {
	fake := &fakeTaskcluster{
		worker: &tcworkermanager.WorkerFullDefinition{
			WorkerPoolID: "gcp/pool-a",
			WorkerGroup:  "us-west1",
			WorkerID:     "i-1234",
		},
		workerRecentTasks: []tcqueue.TaskRun{{TaskID: "task-1", RunID: 0}},
	}
	res := NewWorkersResource(fake)

	detail, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *DetailAction
	for i := range detail.Actions {
		if detail.Actions[i].Key == 't' {
			found = &detail.Actions[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a 't' recent-tasks action, got %+v", detail.Actions)
	}
	if found.Target.ResourceName != "recenttasks" || found.Target.ID != "gcp/pool-a::us-west1::i-1234" ||
		found.Target.Kind != NavScopedList {
		t.Fatalf("unexpected recent-tasks action target: %+v", found.Target)
	}
}

func TestWorkersResourceDescribeOmitsRecentTasksActionWhenNoTasks(t *testing.T) {
	fake := &fakeTaskcluster{
		worker: &tcworkermanager.WorkerFullDefinition{
			WorkerPoolID: "gcp/pool-a",
			WorkerGroup:  "us-west1",
			WorkerID:     "i-1234",
		},
	}
	res := NewWorkersResource(fake)

	detail, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range detail.Actions {
		if a.Key == 't' {
			t.Fatalf("expected no 't' action when there are no recent tasks, got %+v", detail.Actions)
		}
	}
}

func TestWorkersResourceDescribeOmitsRecentTasksSectionWhenGenuinelyEmpty(t *testing.T) {
	fake := &fakeTaskcluster{
		worker: &tcworkermanager.WorkerFullDefinition{
			WorkerPoolID: "gcp/pool-a",
			WorkerGroup:  "us-west1",
			WorkerID:     "i-1234",
		},
	}
	res := NewWorkersResource(fake)

	detail, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "Recent Tasks") {
		t.Fatalf("expected no recent-tasks section when there's no error and no tasks, got body: %s", detail.Body)
	}
}

func TestWorkersResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workerErr: wantErr}
	res := NewWorkersResource(fake)

	_, err := res.Describe("gcp/pool-a::us-west1::i-1234")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWorkersResourceDescribeMalformedID(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	if _, err := res.Describe("not-a-valid-id"); err == nil {
		t.Fatalf("expected an error for a malformed id, got nil")
	}
}

func TestComposeAndParseWorkerID(t *testing.T) {
	id := composeWorkerID("gcp/pool-a", "us-west1", "i-1234")

	workerPoolID, workerGroup, workerID, err := parseWorkerID(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workerPoolID != "gcp/pool-a" || workerGroup != "us-west1" || workerID != "i-1234" {
		t.Fatalf("unexpected round trip: %q %q %q", workerPoolID, workerGroup, workerID)
	}
}

func TestComposeAndParseWorkerIDWithEmptyComponent(t *testing.T) {
	id := composeWorkerID("gcp/pool-a", "", "i-1234")

	workerPoolID, workerGroup, workerID, err := parseWorkerID(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workerPoolID != "gcp/pool-a" || workerGroup != "" || workerID != "i-1234" {
		t.Fatalf("unexpected round trip: %q %q %q", workerPoolID, workerGroup, workerID)
	}
}

func TestParseWorkerIDRejectsMalformedInput(t *testing.T) {
	if _, _, _, err := parseWorkerID("only-one-part"); err == nil {
		t.Fatalf("expected an error for an id with the wrong number of parts")
	}
	if _, _, _, err := parseWorkerID("a::b::c::d"); err == nil {
		t.Fatalf("expected an error for an id with too many parts")
	}
}

func TestWorkersResourceFacetListWithLaunchConfigScope(t *testing.T) {
	fake := &fakeTaskcluster{
		workers: taskcluster.WorkerList{
			{WorkerPoolID: "gcp/pool-a", WorkerGroup: "us-west1", WorkerID: "i-1234", State: "running", Capacity: 1},
		},
	}
	res := NewWorkersResource(fake)

	if _, err := res.FacetList("gcp/pool-a::lc-1", "running"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workersLaunchConfigID != "lc-1" {
		t.Fatalf("expected launch config filter %q, got %q", "lc-1", fake.workersLaunchConfigID)
	}
}

func TestWorkersResourceFacetListWithoutLaunchConfigScope(t *testing.T) {
	fake := &fakeTaskcluster{workers: taskcluster.WorkerList{}}
	res := NewWorkersResource(fake)

	if _, err := res.FacetList("gcp/pool-a", "running"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workersLaunchConfigID != "" {
		t.Fatalf("expected no launch config filter, got %q", fake.workersLaunchConfigID)
	}
}

func TestWorkersResourceFacetCountsWithLaunchConfigScope(t *testing.T) {
	fake := &fakeTaskcluster{stateCounts: map[string]int{"running": 1}}
	res := NewWorkersResource(fake)

	if _, err := res.FacetCounts("gcp/pool-a::lc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.stateCountsLaunchConfigID != "lc-1" {
		t.Fatalf("expected launch config filter %q, got %q", "lc-1", fake.stateCountsLaunchConfigID)
	}
}

func TestComposeAndParseScope(t *testing.T) {
	scope := composeScope("gcp/pool-a", "lc-1")
	if scope != "gcp/pool-a::lc-1" {
		t.Fatalf("unexpected scope: %s", scope)
	}

	workerPoolID, secondary := parseScope(scope)
	if workerPoolID != "gcp/pool-a" || secondary != "lc-1" {
		t.Fatalf("unexpected round trip: %q %q", workerPoolID, secondary)
	}
}

func TestParseScopeWithNoSeparator(t *testing.T) {
	workerPoolID, secondary := parseScope("gcp/pool-a")
	if workerPoolID != "gcp/pool-a" || secondary != "" {
		t.Fatalf("unexpected result: %q %q", workerPoolID, secondary)
	}
}

func TestWorkersResourceScopeActionsExcludesWorkers(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	actions := res.ScopeActions("gcp/pool-a")
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ResourceName == "workers" {
			t.Fatalf("expected \"workers\" excluded from its own sibling actions, got %+v", actions)
		}
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped pool-wide to %q, got %+v", "gcp/pool-a", a)
		}
	}
}

func TestWorkersResourceScopeActionsWithLaunchConfigScope(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	actions := res.ScopeActions("gcp/pool-a::lc-1")
	for _, a := range actions {
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped to the bare pool id %q even from a launch-config-scoped list, got %+v", "gcp/pool-a", a)
		}
	}
}
