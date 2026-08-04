package resource

import (
	"fmt"
	"sync"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// TaskDependenciesResource lists a task's dependencies, scoped by that
// task's own ID. Taskcluster's Queue API exposes only dependency IDs
// (task.Dependencies), not full task+status rows the way listDependentTasks
// does for TaskDependentsResource — so ScopedList fetches each dependency's
// task+status individually (bounded concurrency) to build the same
// name/state/worker-pool/age row shape dependents already gets. A single
// dependency's fetch failing doesn't fail the whole list — its row just
// shows the failure inline — since the rest of the dependencies are still
// useful to see.
type TaskDependenciesResource struct {
	tc         taskcluster.Taskcluster
	stateCache *taskStateCache
}

func NewTaskDependenciesResource(tc taskcluster.Taskcluster, stateCache *taskStateCache) *TaskDependenciesResource {
	return &TaskDependenciesResource{tc: tc, stateCache: stateCache}
}

func (r *TaskDependenciesResource) Name() string      { return "dependencies" }
func (r *TaskDependenciesResource) Aliases() []string { return nil }
func (r *TaskDependenciesResource) Description() string {
	return "A task's dependencies (scoped list) — select one to jump to it"
}

func (r *TaskDependenciesResource) Columns() []Column {
	return taskListColumns()
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *TaskDependenciesResource) List() ([]Row, error) {
	return nil, fmt.Errorf("dependencies requires a task scope")
}

const maxDependencyFetchConcurrency = 16

func (r *TaskDependenciesResource) ScopedList(taskID string) ([]Row, error) {
	task, err := r.tc.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, len(task.Dependencies))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxDependencyFetchConcurrency)

	for i, depID := range task.Dependencies {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, depID string) {
			defer wg.Done()
			defer func() { <-sem }()
			rows[i] = dependencyRow(r.tc, depID)
		}(i, depID)
	}
	wg.Wait()

	return rows, nil
}

// dependencyRow fetches one dependency's task+status and renders it in the
// same shape as taskListRows — but per-row rather than in bulk, since
// dependencies must be fetched one ID at a time (see the type doc comment).
// A fetch failure shows inline as "(failed to load)" for whichever fields
// couldn't be fetched, rather than dropping the row or failing the list.
func dependencyRow(tc taskcluster.Taskcluster, depID string) Row {
	task, err := tc.GetTask(depID)
	if err != nil {
		return Row{ID: depID, Cells: []string{depID, "(failed to load)", "", "", ""}}
	}

	name := task.Metadata.Name
	workerPool := task.ProvisionerID + "/" + task.WorkerType
	age := formatAge(time.Time(task.Created))

	status, err := tc.GetTaskStatus(depID)
	if err != nil {
		return Row{ID: depID, Cells: []string{depID, name, "(failed to load)", workerPool, age}}
	}

	return Row{ID: depID, Cells: []string{depID, name, renderTaskState(status.State), workerPool, age}}
}

func (r *TaskDependenciesResource) EmptyScopeResource() string {
	return "task"
}

func (r *TaskDependenciesResource) Describe(id string) (Detail, error) {
	return describeTask(r.tc, r.stateCache, id)
}

// Actions offers lifecycle actions on a selected dependency's task detail
// (id != ""); the dependencies list itself carries no mutating action. Selecting
// a row opens the task detail through this resource (its rows set no NavTarget),
// so it must expose the actions like any other task detail.
func (r *TaskDependenciesResource) Actions(id string) []Action {
	if id == "" {
		return nil
	}
	return lifecycleActions(r.tc, r.stateCache, id)
}

func (r *TaskDependenciesResource) RefreshInterval() time.Duration {
	return 15 * time.Second
}

// ListWebURL links to the scoped task's own page — there's no dedicated
// dependencies page in the web UI (dependencies are shown inline on a task's
// own page), and scope is that task's ID.
func (r *TaskDependenciesResource) ListWebURL(rootURL, scope string) string {
	return taskWebURL(rootURL, scope)
}

func (r *TaskDependenciesResource) DetailWebURL(rootURL, id string) string {
	return taskWebURL(rootURL, id)
}

// TaskDependentsResource lists tasks that declare the scoped task as one of
// their own dependencies — the reverse direction of
// TaskDependenciesResource. Unlike dependencies, the Queue API's
// listDependentTasks returns full task+status rows in one call, so this
// reuses the same row shape (and Describe) as TasksResource rather than
// overriding navigation via NavTarget.
type TaskDependentsResource struct {
	tc         taskcluster.Taskcluster
	stateCache *taskStateCache
}

func NewTaskDependentsResource(tc taskcluster.Taskcluster, stateCache *taskStateCache) *TaskDependentsResource {
	return &TaskDependentsResource{tc: tc, stateCache: stateCache}
}

func (r *TaskDependentsResource) Name() string      { return "dependents" }
func (r *TaskDependentsResource) Aliases() []string { return nil }
func (r *TaskDependentsResource) Description() string {
	return "Tasks that declare this task as one of their own dependencies (scoped list)"
}

func (r *TaskDependentsResource) Columns() []Column {
	return taskListColumns()
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *TaskDependentsResource) List() ([]Row, error) {
	return nil, fmt.Errorf("dependents requires a task scope")
}

func (r *TaskDependentsResource) ScopedList(taskID string) ([]Row, error) {
	tasks, err := r.tc.GetDependentTasks(taskID)
	if err != nil {
		return nil, err
	}

	return taskListRows(tasks), nil
}

func (r *TaskDependentsResource) EmptyScopeResource() string {
	return "task"
}

func (r *TaskDependentsResource) Describe(id string) (Detail, error) {
	return describeTask(r.tc, r.stateCache, id)
}

// Actions offers lifecycle actions on a selected dependent's task detail
// (id != ""); the dependents list itself carries no mutating action.
func (r *TaskDependentsResource) Actions(id string) []Action {
	if id == "" {
		return nil
	}
	return lifecycleActions(r.tc, r.stateCache, id)
}

func (r *TaskDependentsResource) RefreshInterval() time.Duration {
	return 15 * time.Second
}

// ListWebURL links to the scoped task's own page — there's no dedicated
// dependents page in the web UI, and scope is that task's ID.
func (r *TaskDependentsResource) ListWebURL(rootURL, scope string) string {
	return taskWebURL(rootURL, scope)
}

func (r *TaskDependentsResource) DetailWebURL(rootURL, id string) string {
	return taskWebURL(rootURL, id)
}
