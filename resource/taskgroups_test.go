package resource

import (
	"errors"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestTaskGroupResourceScopedList(t *testing.T) {
	fake := &fakeTaskcluster{
		taskGroupTasks: taskcluster.TaskGroupTaskList{
			{
				Status: tcqueue.TaskStatusStructure{TaskID: "task-1", State: "pending"},
				Task: tcqueue.TaskDefinitionResponse{
					Metadata:      tcqueue.TaskMetadata{Name: "build"},
					ProvisionerID: "gcp",
					WorkerType:    "linux-b-large",
					Created:       tcclient.Time(time.Now().Add(-time.Hour)),
				},
			},
		},
	}
	res := NewTaskGroupResource(fake, &taskDefHistory{}, nil)

	rows, err := res.ScopedList("grp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "task-1" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestTaskGroupResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{taskGroupTasksErr: wantErr}
	res := NewTaskGroupResource(fake, &taskDefHistory{}, nil)

	_, err := res.ScopedList("grp-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskGroupResourceSubtitleNotSealed(t *testing.T) {
	fake := &fakeTaskcluster{
		taskGroup: &tcqueue.TaskGroupDefinitionResponse{TaskGroupID: "grp-1"},
	}
	res := NewTaskGroupResource(fake, &taskDefHistory{}, nil)

	subtitle, err := res.Subtitle("grp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subtitle != "not sealed" {
		t.Fatalf("unexpected subtitle: %q", subtitle)
	}
}

func TestTaskGroupResourceSubtitleSealed(t *testing.T) {
	fake := &fakeTaskcluster{
		taskGroup: &tcqueue.TaskGroupDefinitionResponse{
			TaskGroupID: "grp-1",
			Sealed:      tcclient.Time(time.Now()),
		},
	}
	res := NewTaskGroupResource(fake, &taskDefHistory{}, nil)

	subtitle, err := res.Subtitle("grp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subtitle == "not sealed" || subtitle == "" {
		t.Fatalf("expected a sealed subtitle, got %q", subtitle)
	}
}

func TestTaskGroupResourceSubtitleError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{taskGroupErr: wantErr}
	res := NewTaskGroupResource(fake, &taskDefHistory{}, nil)

	_, err := res.Subtitle("grp-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskGroupResourceListReturnsError(t *testing.T) {
	res := NewTaskGroupResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestTaskGroupResourceIDPromptLabel(t *testing.T) {
	res := NewTaskGroupResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if got := res.IDPromptLabel(); got != "task group id" {
		t.Fatalf("expected %q, got %q", "task group id", got)
	}
}

func TestTaskGroupResourceEmptyScopeResource(t *testing.T) {
	res := NewTaskGroupResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if got := res.EmptyScopeResource(); got != "tasks" {
		t.Fatalf("expected %q, got %q", "tasks", got)
	}
}
