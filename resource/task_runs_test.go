package resource

import (
	"errors"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
)

func TestTaskRunsResourceScopedListReturnsOneRowPerRun(t *testing.T) {
	fake := &fakeTaskcluster{
		taskStatus: &tcqueue.TaskStatusStructure{
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed", WorkerGroup: "us-west1", WorkerID: "i-1234"},
				{RunID: 1, State: "pending"},
			},
		},
	}
	res := NewTaskRunsResource(fake)

	rows, err := res.ScopedList("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0].ID != "task-1/0" || rows[0].Cells[0] != "0" || rows[0].Cells[1] != "completed" ||
		rows[0].Cells[2] != "us-west1/i-1234" {
		t.Fatalf("unexpected row 0: %+v", rows[0])
	}
	if rows[1].ID != "task-1/1" || rows[1].Cells[2] != "n/a" {
		t.Fatalf("unexpected row 1: %+v", rows[1])
	}

	// Selection should land on this resource's own Describe view (a rich
	// per-run detail), not jump straight to a worker.
	for i, row := range rows {
		if row.NavTarget != nil {
			t.Fatalf("expected no NavTarget override for row %d, got %+v", i, row.NavTarget)
		}
	}
}

func TestTaskRunsResourceScopedListPropagatesFetchError(t *testing.T) {
	fake := &fakeTaskcluster{taskStatusErr: errors.New("boom")}
	res := NewTaskRunsResource(fake)

	if _, err := res.ScopedList("task-1"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestTaskRunsResourceListRequiresScope(t *testing.T) {
	res := NewTaskRunsResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error for an unscoped List call")
	}
}

func TestTaskRunsResourceDescribeIncludesWorkerActionWhenAssigned(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{
			Metadata:      tcqueue.TaskMetadata{Name: "build-linux"},
			ProvisionerID: "gcp",
			WorkerType:    "pool-a",
		},
		taskStatus: &tcqueue.TaskStatusStructure{
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed", WorkerGroup: "us-west1", WorkerID: "i-1234"},
			},
		},
	}
	res := NewTaskRunsResource(fake)

	detail, err := res.Describe("task-1/0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: build-linux (task-1) :: Run 0" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if len(detail.Actions) != 2 {
		t.Fatalf("expected 2 actions (worker + artifacts), got %d: %+v", len(detail.Actions), detail.Actions)
	}
	action := detail.Actions[0]
	if action.Key != 'w' || action.Target.ResourceName != "workers" ||
		action.Target.ID != "gcp/pool-a::us-west1::i-1234" || action.Target.Kind != NavDetail {
		t.Fatalf("unexpected worker action: %+v", action)
	}
}

func TestTaskRunsResourceDescribeAlwaysIncludesArtifactsAction(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build-linux"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			Runs: []tcqueue.RunInformation{{RunID: 0, State: "pending"}},
		},
	}
	res := NewTaskRunsResource(fake)

	detail, err := res.Describe("task-1/0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(detail.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(detail.Actions), detail.Actions)
	}
	action := detail.Actions[0]
	if action.Key != 'a' || action.Target.ResourceName != "artifacts" ||
		action.Target.ID != "task-1" || action.Target.Kind != NavScopedList {
		t.Fatalf("unexpected artifacts action: %+v", action)
	}
}

func TestTaskRunsResourceDescribeOmitsWorkerActionWithoutOne(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build-linux"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			Runs: []tcqueue.RunInformation{{RunID: 0, State: "pending"}},
		},
	}
	res := NewTaskRunsResource(fake)

	detail, err := res.Describe("task-1/0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range detail.Actions {
		if a.Key == 'w' {
			t.Fatalf("expected no 'w' action without an assigned worker, got %+v", detail.Actions)
		}
	}
}

func TestTaskRunsResourceDescribeErrorsWhenRunNotFound(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{},
		taskStatus: &tcqueue.TaskStatusStructure{},
	}
	res := NewTaskRunsResource(fake)

	if _, err := res.Describe("task-1/0"); err == nil {
		t.Fatalf("expected an error for a run that doesn't exist")
	}
}

func TestTaskRunsResourceDescribeRejectsMalformedID(t *testing.T) {
	res := NewTaskRunsResource(&fakeTaskcluster{})

	if _, err := res.Describe("not-a-run-id"); err == nil {
		t.Fatalf("expected an error for a malformed run id")
	}
}

func TestTaskRunsResourceDescribePropagatesTaskFetchError(t *testing.T) {
	fake := &fakeTaskcluster{taskErr: errors.New("boom")}
	res := NewTaskRunsResource(fake)

	if _, err := res.Describe("task-1/0"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestTaskRunsResourceDetailWebURLLinksToTaskPage(t *testing.T) {
	res := NewTaskRunsResource(&fakeTaskcluster{})

	got := res.DetailWebURL("https://tc.example.com", "task-1/0")
	if got == "" {
		t.Fatalf("expected a non-empty URL")
	}
}
