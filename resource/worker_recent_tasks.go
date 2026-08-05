package resource

import (
	"fmt"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// WorkerRecentTasksResource lists a worker's recent tasks, scoped by that
// worker's own ID (see composeWorkerID/parseWorkerID). Every row overrides
// navigation via NavTarget straight to the task's own Detail view (see
// Row.NavTarget), so Describe is never called and this resource is never
// reached other than via the worker detail's 't' action — the Queue API's
// getWorker endpoint returns only taskId+runId pairs, not full task+status
// rows, so there's nothing richer to show per row.
type WorkerRecentTasksResource struct {
	tc taskcluster.Taskcluster
}

func NewWorkerRecentTasksResource(tc taskcluster.Taskcluster) *WorkerRecentTasksResource {
	return &WorkerRecentTasksResource{tc: tc}
}

func (r *WorkerRecentTasksResource) Name() string      { return "recenttasks" }
func (r *WorkerRecentTasksResource) Aliases() []string { return nil }
func (r *WorkerRecentTasksResource) Description() string {
	return "A worker's recent tasks (scoped list) — select one to jump to it"
}

func (r *WorkerRecentTasksResource) Columns() []Column {
	return []Column{{Title: "TASK ID"}, {Title: "RUN", Width: 8}}
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *WorkerRecentTasksResource) List() ([]Row, error) {
	return nil, fmt.Errorf("recenttasks requires a worker scope")
}

func (r *WorkerRecentTasksResource) ScopedList(workerID string) ([]Row, error) {
	workerPoolID, workerGroup, wID, err := parseWorkerID(workerID)
	if err != nil {
		return nil, err
	}

	tasks, err := r.tc.GetWorkerRecentTasks(workerPoolID, workerGroup, wID)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, Row{
			ID:        t.TaskID,
			Cells:     []string{t.TaskID, fmt.Sprintf("%d", t.RunID)},
			NavTarget: &NavTarget{ResourceName: "task", ID: t.TaskID, Kind: NavDetail},
		})
	}

	return rows, nil
}

func (r *WorkerRecentTasksResource) ScopePromptLabel() string { return "worker id (pool::group::id)" }

func (r *WorkerRecentTasksResource) EmptyScopeResource() string {
	return "workerpools"
}

// Describe is unreachable in normal use — see the type doc comment.
// Implemented only to satisfy the Resource interface.
func (r *WorkerRecentTasksResource) Describe(id string) (Detail, error) {
	return Detail{}, fmt.Errorf("recent task entries are not viewable directly")
}

func (r *WorkerRecentTasksResource) RefreshInterval() time.Duration { return 0 }

// ListWebURL links to the scoped worker's own page — there's no dedicated
// recent-tasks page in the web UI, and scope is that worker's ID.
func (r *WorkerRecentTasksResource) ListWebURL(rootURL, scope string) string {
	return workerDetailWebURL(rootURL, scope)
}

// DetailWebURL is never expected to be called — see Describe's doc comment.
func (r *WorkerRecentTasksResource) DetailWebURL(rootURL, id string) string { return "" }
