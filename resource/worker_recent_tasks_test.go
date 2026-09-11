package resource

import (
	"errors"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
)

func TestWorkerRecentTasksResourceScopedListReturnsNavigableRows(t *testing.T) {
	fake := &fakeTaskcluster{
		workerRecentTasks: []tcqueue.TaskRun{
			{TaskID: "task-1", RunID: 0},
			{TaskID: "task-2", RunID: 1},
		},
	}
	res := NewWorkerRecentTasksResource(fake)

	rows, err := res.ScopedList("gcp/pool-a::us-west1::i-1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	for i, want := range []struct {
		taskID string
		run    string
	}{{"task-1", "0"}, {"task-2", "1"}} {
		row := rows[i]
		if row.ID != want.taskID || row.Cells[0] != want.taskID || row.Cells[1] != want.run {
			t.Fatalf("unexpected row %d: %+v", i, row)
		}
		if row.NavTarget == nil || row.NavTarget.ResourceName != "task" ||
			row.NavTarget.ID != want.taskID || row.NavTarget.Kind != NavDetail {
			t.Fatalf("unexpected NavTarget for row %d: %+v", i, row.NavTarget)
		}
	}
}

func TestWorkerRecentTasksResourceScopedListPropagatesFetchError(t *testing.T) {
	fake := &fakeTaskcluster{workerRecentTasksErr: errors.New("boom")}
	res := NewWorkerRecentTasksResource(fake)

	if _, err := res.ScopedList("gcp/pool-a::us-west1::i-1234"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestWorkerRecentTasksResourceScopedListRejectsMalformedScope(t *testing.T) {
	res := NewWorkerRecentTasksResource(&fakeTaskcluster{})

	if _, err := res.ScopedList("not-a-worker-id"); err == nil {
		t.Fatalf("expected an error for a malformed worker id scope")
	}
}

func TestWorkerRecentTasksResourceListRequiresScope(t *testing.T) {
	res := NewWorkerRecentTasksResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error for an unscoped List call")
	}
}

func TestWorkerRecentTasksResourceDescribeIsUnreachable(t *testing.T) {
	res := NewWorkerRecentTasksResource(&fakeTaskcluster{})

	if _, err := res.Describe("task-1"); err == nil {
		t.Fatalf("expected Describe to always error")
	}
}
