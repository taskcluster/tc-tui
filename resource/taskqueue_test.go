package resource

import (
	"errors"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestPendingTasksResourceScopedList(t *testing.T) {
	fake := &fakeTaskcluster{
		pendingTasks: taskcluster.PendingTaskList{
			{
				TaskID:   "task-1",
				Task:     tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}, ProvisionerID: "gcp", WorkerType: "linux-b-large", Priority: "high"},
				Inserted: tcclient.Time(time.Now().Add(-time.Minute)),
			},
		},
	}
	res := NewPendingTasksResource(fake, nil)

	rows, err := res.ScopedList("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ID != "task-1" || rows[0].Cells[0] != "task-1" ||
		rows[0].Cells[1] != "build" || rows[0].Cells[2] != "gcp/linux-b-large" || rows[0].Cells[3] != "high" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	if rows[0].Cells[5] == "" {
		t.Fatalf("expected a non-empty AGE cell, got %+v", rows[0])
	}
}

func TestPendingTasksResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{pendingTasksErr: wantErr}
	res := NewPendingTasksResource(fake, nil)

	_, err := res.ScopedList("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestPendingTasksResourceListReturnsError(t *testing.T) {
	res := NewPendingTasksResource(&fakeTaskcluster{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestPendingTasksResourceEmptyScopeResource(t *testing.T) {
	res := NewPendingTasksResource(&fakeTaskcluster{}, nil)

	if got := res.EmptyScopeResource(); got != "workerpools" {
		t.Fatalf("expected %q, got %q", "workerpools", got)
	}
}

func TestPendingTasksResourceDescribeDelegatesToDescribeTask(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{State: "pending"},
	}
	res := NewPendingTasksResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: [white]pending[white] build (task-1)" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
}

func TestClaimedTasksResourceScopedList(t *testing.T) {
	fake := &fakeTaskcluster{
		claimedTasks: taskcluster.ClaimedTaskList{
			{
				TaskID:      "task-1",
				Task:        tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
				WorkerGroup: "us-west1",
				WorkerID:    "i-1234",
				Claimed:     tcclient.Time(time.Now().Add(-time.Minute)),
			},
		},
	}
	res := NewClaimedTasksResource(fake, nil)

	rows, err := res.ScopedList("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ID != "task-1" || rows[0].Cells[0] != "task-1" ||
		rows[0].Cells[1] != "build" || rows[0].Cells[2] != "us-west1/i-1234" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	if rows[0].Cells[4] == "" {
		t.Fatalf("expected a non-empty AGE cell, got %+v", rows[0])
	}
}

func TestClaimedTasksResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{claimedTasksErr: wantErr}
	res := NewClaimedTasksResource(fake, nil)

	_, err := res.ScopedList("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestClaimedTasksResourceListReturnsError(t *testing.T) {
	res := NewClaimedTasksResource(&fakeTaskcluster{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestClaimedTasksResourceEmptyScopeResource(t *testing.T) {
	res := NewClaimedTasksResource(&fakeTaskcluster{}, nil)

	if got := res.EmptyScopeResource(); got != "workerpools" {
		t.Fatalf("expected %q, got %q", "workerpools", got)
	}
}

func TestClaimedTasksResourceDescribeDelegatesToDescribeTask(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{State: "running"},
	}
	res := NewClaimedTasksResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: [yellow]running[white] build (task-1)" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
}

func TestPendingTasksResourceScopeActionsExcludesPending(t *testing.T) {
	res := NewPendingTasksResource(&fakeTaskcluster{}, nil)

	actions := res.ScopeActions("gcp/pool-a")
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ResourceName == "pending" {
			t.Fatalf("expected \"pending\" excluded from its own sibling actions, got %+v", actions)
		}
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped pool-wide to %q, got %+v", "gcp/pool-a", a)
		}
	}
}

func TestClaimedTasksResourceScopeActionsExcludesClaimed(t *testing.T) {
	res := NewClaimedTasksResource(&fakeTaskcluster{}, nil)

	actions := res.ScopeActions("gcp/pool-a")
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ResourceName == "claimed" {
			t.Fatalf("expected \"claimed\" excluded from its own sibling actions, got %+v", actions)
		}
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped pool-wide to %q, got %+v", "gcp/pool-a", a)
		}
	}
}
