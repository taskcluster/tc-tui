package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestTaskResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{
			Metadata: tcqueue.TaskMetadata{
				Name:        "build-linux",
				Description: "builds linux",
				Owner:       "owner@example.com",
				Source:      "https://example.com/source",
			},
			ProvisionerID: "gcp",
			WorkerType:    "linux-b-large",
			Priority:      "high",
			TaskGroupID:   "grp-1",
			Dependencies:  []string{"dep-1"},
			Scopes:        []string{"queue:get-task:*"},
			Created:       tcclient.Time(time.Now()),
			Deadline:      tcclient.Time(time.Now()),
			Expires:       tcclient.Time(time.Now()),
		},
		taskStatus: &tcqueue.TaskStatusStructure{
			State:       "completed",
			RetriesLeft: 3,
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed", ReasonResolved: "completed", WorkerGroup: "us-west1", WorkerID: "i-1"},
			},
		},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: [green]completed[white] build-linux (task-1)" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if !strings.Contains(detail.Body, "build-linux") || !strings.Contains(detail.Body, "completed") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
	if len(detail.Actions) != 6 {
		t.Fatalf("expected 6 actions, got %d", len(detail.Actions))
	}
	workerPoolAction := detail.Actions[0]
	if workerPoolAction.Key != 'W' || workerPoolAction.Target.ResourceName != "workerpools" ||
		workerPoolAction.Target.ID != "gcp/linux-b-large" || workerPoolAction.Target.Kind != NavDetail {
		t.Fatalf("unexpected action: %+v", workerPoolAction)
	}
	groupAction := detail.Actions[1]
	if groupAction.Key != 'g' || groupAction.Target.ResourceName != "taskgroup" ||
		groupAction.Target.ID != "grp-1" || groupAction.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action: %+v", groupAction)
	}
	depsAction := detail.Actions[2]
	if depsAction.Key != 'd' || depsAction.Target.ResourceName != "dependencies" ||
		depsAction.Target.ID != "task-1" || depsAction.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action: %+v", depsAction)
	}
	dependentsAction := detail.Actions[3]
	if dependentsAction.Key != 'D' || dependentsAction.Target.ResourceName != "dependents" ||
		dependentsAction.Target.ID != "task-1" || dependentsAction.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action: %+v", dependentsAction)
	}
	runsAction := detail.Actions[4]
	if runsAction.Key != 'R' || runsAction.Target.ResourceName != "runs" ||
		runsAction.Target.ID != "task-1" || runsAction.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action: %+v", runsAction)
	}
	artifactsAction := detail.Actions[5]
	if artifactsAction.Key != 'a' || artifactsAction.Target.ResourceName != "artifacts" ||
		artifactsAction.Target.ID != "task-1" || artifactsAction.Target.Kind != NavScopedList {
		t.Fatalf("unexpected action: %+v", artifactsAction)
	}
}

func TestTaskResourceDescribeGroupsOwnerAndSourceOnOneLine(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{
			Metadata: tcqueue.TaskMetadata{Name: "t", Owner: "owner@example.com", Source: "https://example.com/src"},
		},
		taskStatus: &tcqueue.TaskStatusStructure{State: "completed"},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := stripRegionTags(detail.Body)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "owner@example.com") {
			if !strings.Contains(line, "https://example.com/src") {
				t.Fatalf("expected Owner and Source on the same line, got: %q", line)
			}
			return
		}
	}
	t.Fatalf("owner not found in body: %s", body)
}

// TestTaskResourceDescribeAlwaysShowsDependentsAction confirms the
// dependents action appears even for a task with no dependencies of its own
// — the two directions are independent (see describeTask's comment on why
// dependents can't be conditionally hidden the way dependencies is).
func TestTaskResourceDescribeAlwaysShowsDependentsAction(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "leaf"}},
		taskStatus: &tcqueue.TaskStatusStructure{State: "completed"},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(detail.Actions) != 4 {
		t.Fatalf("expected 4 actions (worker pool + task group + dependents + artifacts, no dependencies), got %d", len(detail.Actions))
	}
	dependentsAction := detail.Actions[2]
	if dependentsAction.Key != 'D' || dependentsAction.Target.ResourceName != "dependents" {
		t.Fatalf("unexpected action: %+v", dependentsAction)
	}
	artifactsAction := detail.Actions[3]
	if artifactsAction.Key != 'a' || artifactsAction.Target.ResourceName != "artifacts" {
		t.Fatalf("unexpected action: %+v", artifactsAction)
	}
}

// TestTaskResourceDescribeOmitsRunsActionWhenNoRuns confirms the 'R' runs
// action is hidden for a task with no runs yet — unlike dependents, there's
// nothing useful to browse.
func TestTaskResourceDescribeOmitsRunsActionWhenNoRuns(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "leaf"}},
		taskStatus: &tcqueue.TaskStatusStructure{State: "unscheduled"},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range detail.Actions {
		if a.Key == 'R' {
			t.Fatalf("expected no 'R' action when there are no runs, got %+v", detail.Actions)
		}
	}
}

func TestTaskResourceDescribeIncludesPayload(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{
			Metadata: tcqueue.TaskMetadata{Name: "build", Description: "builds the thing"},
			Payload:  []byte(`{"command":["echo","hi"]}`),
		},
		taskStatus: &tcqueue.TaskStatusStructure{State: "completed"},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "Payload:") || !strings.Contains(detail.Body, "command") {
		t.Fatalf("expected the rendered payload in the body, got: %s", detail.Body)
	}
	if !strings.Contains(stripRegionTags(detail.Body), "builds the thing") {
		t.Fatalf("expected the rendered description in the body, got: %s", detail.Body)
	}
}

func TestTaskResourceDescribeTaskError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{taskErr: wantErr}
	res := NewTaskResource(fake, nil)

	_, err := res.Describe("task-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskResourceDescribeStatusError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{
		task:          &tcqueue.TaskDefinitionResponse{},
		taskStatusErr: wantErr,
	}
	res := NewTaskResource(fake, nil)

	_, err := res.Describe("task-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskResourceListReturnsError(t *testing.T) {
	res := NewTaskResource(&fakeTaskcluster{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestTaskResourceIDPromptLabel(t *testing.T) {
	res := NewTaskResource(&fakeTaskcluster{}, nil)

	if got := res.IDPromptLabel(); got != "task id" {
		t.Fatalf("expected %q, got %q", "task id", got)
	}
}

func TestTasksResourceScopedList(t *testing.T) {
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
	res := NewTasksResource(fake, &taskDefHistory{}, nil)

	rows, err := res.ScopedList("grp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ID != "task-1" {
		t.Fatalf("unexpected id: %s", rows[0].ID)
	}
	if rows[0].Cells[0] != "task-1" || rows[0].Cells[1] != "build" ||
		rows[0].Cells[2] != "[white]pending[white]" || rows[0].Cells[3] != "gcp/linux-b-large" {
		t.Fatalf("unexpected cells: %+v", rows[0].Cells)
	}
	if rows[0].Cells[4] == "" {
		t.Fatalf("expected a non-empty AGE cell, got %+v", rows[0].Cells)
	}
}

func TestTasksResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{taskGroupTasksErr: wantErr}
	res := NewTasksResource(fake, &taskDefHistory{}, nil)

	_, err := res.ScopedList("grp-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTasksResourceListReturnsError(t *testing.T) {
	res := NewTasksResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestTasksResourceEmptyScopeResource(t *testing.T) {
	res := NewTasksResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if got := res.EmptyScopeResource(); got != "taskgroup" {
		t.Fatalf("expected %q, got %q", "taskgroup", got)
	}
}

func TestDescribeTaskRunsIncludeTimestamps(t *testing.T) {
	scheduled := tcclient.Time(time.Now().Add(-time.Hour))
	started := tcclient.Time(time.Now().Add(-50 * time.Minute))
	resolved := tcclient.Time(time.Now().Add(-10 * time.Minute))
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "completed",
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed", Scheduled: scheduled, Started: started, Resolved: resolved},
			},
		},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "scheduled:") || !strings.Contains(detail.Body, "started:") ||
		!strings.Contains(detail.Body, "resolved:") {
		t.Fatalf("expected scheduled/started/resolved timestamps in the run info, got: %s", detail.Body)
	}
}

func TestDescribeTaskRunsIncludeElapsedTimeBetweenEvents(t *testing.T) {
	scheduled := tcclient.Time(time.Now().Add(-time.Hour))
	started := tcclient.Time(time.Now().Add(-50 * time.Minute))
	resolved := tcclient.Time(time.Now().Add(-10 * time.Minute))
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "completed",
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed", Scheduled: scheduled, Started: started, Resolved: resolved},
			},
		},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "10m0s after scheduled") {
		t.Fatalf("expected started timestamp annotated with elapsed time since scheduled, got: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "40m0s after started") {
		t.Fatalf("expected resolved timestamp annotated with elapsed time since started, got: %s", detail.Body)
	}
}

func TestDescribeTaskRunOmitsElapsedTimeWhenPriorEventIsUnset(t *testing.T) {
	resolved := tcclient.Time(time.Now().Add(-10 * time.Minute))
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "completed",
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed", Resolved: resolved},
			},
		},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "after started") {
		t.Fatalf("expected no elapsed annotation when started is unset, got: %s", detail.Body)
	}
}

func TestDescribeTaskRunListsArtifactsForStartedRuns(t *testing.T) {
	started := tcclient.Time(time.Now().Add(-10 * time.Minute))
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "completed",
			Runs:  []tcqueue.RunInformation{{RunID: 0, State: "completed", Started: started}},
		},
		artifacts: taskcluster.ArtifactList{
			{Name: "public/logs/live_backing.log", ContentType: "text/plain", ContentLength: 2048},
			{Name: "public/build.tar.gz", ContentType: "application/gzip", ContentLength: 5 * 1024 * 1024},
		},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "public/logs/live_backing.log") ||
		!strings.Contains(detail.Body, "public/build.tar.gz") {
		t.Fatalf("expected artifact names in the body, got: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "2.0 KiB") || !strings.Contains(detail.Body, "5.0 MiB") {
		t.Fatalf("expected human-readable artifact sizes in the body, got: %s", detail.Body)
	}
}

func TestDescribeTaskRunSkipsArtifactFetchForUnstartedRuns(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "pending",
			Runs:  []tcqueue.RunInformation{{RunID: 0, State: "pending"}},
		},
		artifactsErr: errors.New("should not be called"),
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "artifacts:") {
		t.Fatalf("expected no artifacts section for an unstarted run, got: %s", detail.Body)
	}
}

func TestDescribeTaskRunShowsArtifactLoadFailureInline(t *testing.T) {
	started := tcclient.Time(time.Now().Add(-10 * time.Minute))
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "completed",
			Runs:  []tcqueue.RunInformation{{RunID: 0, State: "completed", Started: started}},
		},
		artifactsErr: errors.New("boom"),
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "artifacts: (failed to load: boom)") {
		t.Fatalf("expected inline artifact load failure, got: %s", detail.Body)
	}
}

func TestDescribeTaskRunOmitsUnsetTimestamps(t *testing.T) {
	fake := &fakeTaskcluster{
		task: &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{
			State: "pending",
			Runs:  []tcqueue.RunInformation{{RunID: 0, State: "pending"}},
		},
	}
	res := NewTaskResource(fake, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "started:") || strings.Contains(detail.Body, "resolved:") ||
		strings.Contains(detail.Body, "takenUntil:") {
		t.Fatalf("expected unset run timestamps to be omitted, got: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "scheduled:") {
		t.Fatalf("expected scheduled: to be present even when unset, got: %s", detail.Body)
	}
}

func TestTasksResourceDescribeDelegatesToDescribeTask(t *testing.T) {
	fake := &fakeTaskcluster{
		task:       &tcqueue.TaskDefinitionResponse{Metadata: tcqueue.TaskMetadata{Name: "build"}},
		taskStatus: &tcqueue.TaskStatusStructure{State: "completed"},
	}
	res := NewTasksResource(fake, &taskDefHistory{}, nil)

	detail, err := res.Describe("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: [green]completed[white] build (task-1)" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if !strings.Contains(detail.Body, "build") || !strings.Contains(detail.Body, "completed") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "(no runs yet)") {
		t.Fatalf("expected no-runs sentinel in body: %s", detail.Body)
	}
}
