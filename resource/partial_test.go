package resource

import (
	"errors"
	"testing"

	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcqueue"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// Every PartialLister must translate loadAll=false into the safe fetch cap
// and loadAll=true into an uncapped fetch (limit 0), and pass the client's
// truncated report through as `more`.

func TestTasksResourceListPartialCapsFetchAndReportsMore(t *testing.T) {
	fake := &fakeTaskcluster{
		taskGroupTasks: taskcluster.TaskGroupTaskList{
			{Status: tcqueue.TaskStatusStructure{TaskID: "task-1"}},
		},
		taskGroupTasksTruncated: true,
	}
	res := NewTasksResource(fake, &taskDefHistory{}, nil)

	rows, more, err := res.ListPartial("group-1", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.taskGroupTasksLimit != taskcluster.DefaultListLimit {
		t.Fatalf("expected limit %d, got %d", taskcluster.DefaultListLimit, fake.taskGroupTasksLimit)
	}
	if !more {
		t.Fatalf("expected more=true when the fetch was truncated")
	}
	if len(rows) != 1 || rows[0].ID != "task-1" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestTasksResourceListPartialLoadAllLiftsCap(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewTasksResource(fake, &taskDefHistory{}, nil)

	_, more, err := res.ListPartial("group-1", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.taskGroupTasksLimit != 0 {
		t.Fatalf("expected loadAll to fetch with limit 0, got %d", fake.taskGroupTasksLimit)
	}
	if more {
		t.Fatalf("expected more=false when nothing was truncated")
	}
}

func TestTaskGroupResourceListPartialCapsFetch(t *testing.T) {
	fake := &fakeTaskcluster{taskGroupTasksTruncated: true}
	res := NewTaskGroupResource(fake, &taskDefHistory{}, nil)

	_, more, err := res.ListPartial("group-1", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.taskGroupTasksLimit != taskcluster.DefaultListLimit {
		t.Fatalf("expected limit %d, got %d", taskcluster.DefaultListLimit, fake.taskGroupTasksLimit)
	}
	if !more {
		t.Fatalf("expected more=true when the fetch was truncated")
	}
}

func TestWorkersResourceListPartialCapsFetchAndKeepsFacetState(t *testing.T) {
	fake := &fakeTaskcluster{
		workers: taskcluster.WorkerList{
			{WorkerPoolID: "gcp/pool-a", WorkerGroup: "us-west1", WorkerID: "i-1", State: "stopped"},
		},
		workersTruncated: true,
	}
	res := NewWorkersResource(fake)

	rows, more, err := res.ListPartial("gcp/pool-a", "stopped", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workersLimit != taskcluster.DefaultListLimit {
		t.Fatalf("expected limit %d, got %d", taskcluster.DefaultListLimit, fake.workersLimit)
	}
	if fake.workersState != "stopped" {
		t.Fatalf("expected the facet value to be passed as state, got %q", fake.workersState)
	}
	if !more {
		t.Fatalf("expected more=true when the fetch was truncated")
	}
	if len(rows) != 1 || rows[0].ID != "gcp/pool-a::us-west1::i-1" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestWorkersResourceListPartialLoadAllLiftsCap(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewWorkersResource(fake)

	_, _, err := res.ListPartial("gcp/pool-a::lc-1", "running", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workersLimit != 0 {
		t.Fatalf("expected loadAll to fetch with limit 0, got %d", fake.workersLimit)
	}
	if fake.workersLaunchConfigID != "lc-1" {
		t.Fatalf("expected the compound scope's launch config to be passed, got %q", fake.workersLaunchConfigID)
	}
}

func TestPendingTasksResourceListPartialCapsFetch(t *testing.T) {
	fake := &fakeTaskcluster{pendingTasksTruncated: true}
	res := NewPendingTasksResource(fake, nil)

	_, more, err := res.ListPartial("gcp/pool-a", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.pendingTasksLimit != taskcluster.DefaultListLimit {
		t.Fatalf("expected limit %d, got %d", taskcluster.DefaultListLimit, fake.pendingTasksLimit)
	}
	if !more {
		t.Fatalf("expected more=true when the fetch was truncated")
	}
}

func TestClaimedTasksResourceListPartialCapsFetch(t *testing.T) {
	fake := &fakeTaskcluster{claimedTasksTruncated: true}
	res := NewClaimedTasksResource(fake, nil)

	_, more, err := res.ListPartial("gcp/pool-a", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.claimedTasksLimit != taskcluster.DefaultListLimit {
		t.Fatalf("expected limit %d, got %d", taskcluster.DefaultListLimit, fake.claimedTasksLimit)
	}
	if !more {
		t.Fatalf("expected more=true when the fetch was truncated")
	}
}

func TestListPartialPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{taskGroupTasksErr: wantErr}
	res := NewTasksResource(fake, &taskDefHistory{}, nil)

	_, _, err := res.ListPartial("group-1", "", false)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

// The interface assertions the shell's dispatch depends on: each of these
// resources must actually satisfy PartialLister.
var (
	_ PartialLister = (*TasksResource)(nil)
	_ PartialLister = (*TaskGroupResource)(nil)
	_ PartialLister = (*WorkersResource)(nil)
	_ PartialLister = (*PendingTasksResource)(nil)
	_ PartialLister = (*ClaimedTasksResource)(nil)
)
