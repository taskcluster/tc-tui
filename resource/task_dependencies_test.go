package resource

import (
	"errors"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
)

func TestTaskDependenciesResourceScopedListReturnsNavigableRows(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{
			Metadata:      tcqueue.TaskMetadata{Name: "dep-task"},
			ProvisionerID: "gcp",
			WorkerType:    "pool-a",
			Dependencies:  []string{"dep-1", "dep-2"},
		},
		taskStatus: &tcqueue.TaskStatusStructure{State: "completed"},
	}
	res := NewTaskDependenciesResource(fake, nil)

	rows, err := res.ScopedList("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// The fake returns the same task+status regardless of which ID was
	// requested, so both dependency rows resolve to the same name/state/pool
	// — what matters here is that each row still keys off its own dependency
	// ID and gets the richer shape, not a raw ID-only row.
	for i, depID := range []string{"dep-1", "dep-2"} {
		row := rows[i]
		if row.ID != depID || row.Cells[0] != depID || row.Cells[1] != "dep-task" ||
			row.Cells[2] != "[green]completed[white]" || row.Cells[3] != "gcp/pool-a" {
			t.Fatalf("unexpected row %d: %+v", i, row)
		}
		if row.NavTarget != nil {
			t.Fatalf("expected no NavTarget override — default selection should call Describe directly, got %+v", row.NavTarget)
		}
	}
}

func TestTaskDependenciesResourceScopedListPropagatesFetchError(t *testing.T) {
	fake := &fakeTaskcluster{taskErr: errors.New("boom")}
	res := NewTaskDependenciesResource(fake, nil)

	if _, err := res.ScopedList("task-1"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestTaskDependenciesResourceListRequiresScope(t *testing.T) {
	res := NewTaskDependenciesResource(&fakeTaskcluster{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error for an unscoped List call")
	}
}

func TestDependencyRowShowsFailureInlineOnFetchError(t *testing.T) {
	fake := &fakeTaskcluster{taskErr: errors.New("boom")}

	row := dependencyRow(fake, "dep-1")
	if row.ID != "dep-1" || row.Cells[0] != "dep-1" || row.Cells[1] != "(failed to load)" {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestDependencyRowShowsFailureInlineOnStatusFetchError(t *testing.T) {
	fake := &fakeTaskcluster{
		task:          &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "dep-task"}},
		taskStatusErr: errors.New("boom"),
	}

	row := dependencyRow(fake, "dep-1")
	if row.ID != "dep-1" || row.Cells[1] != "dep-task" || row.Cells[2] != "(failed to load)" {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestTaskDependenciesResourceDescribeDelegatesToTaskDetail(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "dep-task"}},
		taskStatus: &tcqueue.TaskStatusStructure{},
	}
	res := NewTaskDependenciesResource(fake, nil)

	detail, err := res.Describe("dep-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: dep-task (dep-1)" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
}

func TestTaskDependentsResourceScopedListReturnsTaskRows(t *testing.T) {
	fake := &fakeTaskcluster{
		dependentTasks: []tcqueue.TaskDefinitionAndStatus{
			{
				Task:   tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "dependent-1"}, WorkerType: "linux-b-large"},
				Status: tcqueue.TaskStatusStructure{TaskID: "dependent-task-1", State: "pending"},
			},
		},
	}
	res := NewTaskDependentsResource(fake, nil)

	rows, err := res.ScopedList("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.ID != "dependent-task-1" || row.Cells[0] != "dependent-task-1" ||
		row.Cells[1] != "dependent-1" || row.Cells[2] != "[white]pending[white]" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row.NavTarget != nil {
		t.Fatalf("expected no NavTarget override — default selection should call Describe directly, got %+v", row.NavTarget)
	}
}

func TestTaskDependentsResourceScopedListPropagatesFetchError(t *testing.T) {
	fake := &fakeTaskcluster{dependentTasksErr: errors.New("boom")}
	res := NewTaskDependentsResource(fake, nil)

	if _, err := res.ScopedList("task-1"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestTaskDependentsResourceListRequiresScope(t *testing.T) {
	res := NewTaskDependentsResource(&fakeTaskcluster{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error for an unscoped List call")
	}
}
