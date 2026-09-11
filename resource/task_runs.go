package resource

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// TaskRunsResource lists a task's runs, scoped by that task's own ID.
// Selecting a run opens its own Describe view (state, timestamps,
// artifacts) rather than jumping straight to a worker — Taskcluster runs
// carry enough of their own detail to be worth a dedicated view, and a
// direct jump on selection was surprising (see Describe's 'w' action for
// the actual worker jump).
type TaskRunsResource struct {
	tc taskcluster.Taskcluster
}

func NewTaskRunsResource(tc taskcluster.Taskcluster) *TaskRunsResource {
	return &TaskRunsResource{tc: tc}
}

func (r *TaskRunsResource) Name() string      { return "runs" }
func (r *TaskRunsResource) Aliases() []string { return nil }
func (r *TaskRunsResource) Description() string {
	return "A task's runs (scoped list) — select one for its detail, then 'w' for its worker"
}

func (r *TaskRunsResource) Columns() []Column {
	return []Column{
		{Title: "RUN", Width: 6},
		{Title: "STATE", Width: 12},
		{Title: "WORKER", Width: 30},
		{Title: "RESOLVED"},
	}
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *TaskRunsResource) List() ([]Row, error) {
	return nil, fmt.Errorf("runs requires a task scope")
}

func (r *TaskRunsResource) ScopedList(taskID string) ([]Row, error) {
	status, err := r.tc.GetTaskStatus(taskID)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(status.Runs))
	for _, run := range status.Runs {
		rows = append(rows, Row{
			ID: composeRunID(taskID, run.RunID),
			Cells: []string{
				fmt.Sprintf("%d", run.RunID),
				run.State,
				formatWorker(run.WorkerGroup, run.WorkerID),
				fmt.Sprint(run.Resolved),
			},
		})
	}

	return rows, nil
}

func (r *TaskRunsResource) ScopePromptLabel() string { return "task id" }

func (r *TaskRunsResource) EmptyScopeResource() string {
	return "task"
}

// Describe renders a single run's own detail — the same state/timestamps/
// artifacts rendering describeTask nests inline for every run, but for just
// this one — plus a 'w' action to jump to its worker when one is assigned.
func (r *TaskRunsResource) Describe(id string) (Detail, error) {
	taskID, runID, err := parseRunID(id)
	if err != nil {
		return Detail{}, err
	}

	task, err := r.tc.GetTask(taskID)
	if err != nil {
		return Detail{}, err
	}

	status, err := r.tc.GetTaskStatus(taskID)
	if err != nil {
		return Detail{}, err
	}

	run, ok := findRun(status.Runs, runID)
	if !ok {
		return Detail{}, fmt.Errorf("run %d not found for task %s", runID, taskID)
	}

	var actions []DetailAction
	if run.WorkerGroup != "" && run.WorkerID != "" {
		workerPoolID := task.ProvisionerID + "/" + task.WorkerType
		actions = append(actions, DetailAction{
			Key:   'w',
			Label: "worker",
			Target: NavTarget{
				ResourceName: "workers",
				ID:           composeWorkerID(workerPoolID, run.WorkerGroup, run.WorkerID),
				Kind:         NavDetail,
			},
		})
	}
	if name, ok := findLiveLogArtifact(r.tc, taskID, runID); ok {
		actions = append(actions, DetailAction{
			Key:   'l',
			Label: "live log",
			Target: NavTarget{
				ResourceName: "artifacts",
				ID:           composeArtifactID(taskID, runID, name),
				Kind:         NavDetail,
			},
		})
	}
	// Always shown, like describeTask's own 'a' action — artifacts is
	// scoped by task (tabbing over whichever runs exist), not by this one
	// run, so there's nothing to gate on here either.
	actions = append(actions, DetailAction{
		Key:   'a',
		Label: "artifacts",
		Target: NavTarget{
			ResourceName: "artifacts",
			ID:           taskID,
			Kind:         NavScopedList,
		},
	})

	return Detail{
		Title:   fmt.Sprintf("Task :: %s (%s) :: Run %d", task.Metadata.Name, taskID, runID),
		Body:    renderRunBody(r.tc, taskID, run, ""),
		Actions: actions,
	}, nil
}

func (r *TaskRunsResource) RefreshInterval() time.Duration {
	return 15 * time.Second
}

// ListWebURL links to the scoped task's own page — there's no dedicated
// runs page in the web UI (runs are shown inline on a task's own page), and
// scope is that task's ID.
func (r *TaskRunsResource) ListWebURL(rootURL, scope string) string {
	return taskWebURL(rootURL, scope)
}

// DetailWebURL links to the same task page as ListWebURL — the web UI has
// no per-run anchor of its own.
func (r *TaskRunsResource) DetailWebURL(rootURL, id string) string {
	taskID, _, err := parseRunID(id)
	if err != nil {
		return ""
	}
	return taskWebURL(rootURL, taskID)
}

// findRun returns the run with the given RunID, if present.
func findRun(runs []tcqueue.RunInformation, runID int64) (tcqueue.RunInformation, bool) {
	for _, run := range runs {
		if run.RunID == runID {
			return run, true
		}
	}
	return tcqueue.RunInformation{}, false
}

// composeRunID and parseRunID identify a single run as "<taskID>/<runID>" —
// safe because Taskcluster task IDs are slugids (never contain '/').
func composeRunID(taskID string, runID int64) string {
	return fmt.Sprintf("%s/%d", taskID, runID)
}

func parseRunID(id string) (taskID string, runID int64, err error) {
	idx := strings.LastIndex(id, "/")
	if idx < 0 {
		return "", 0, fmt.Errorf("invalid run id %q", id)
	}

	runID, err = strconv.ParseInt(id[idx+1:], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid run id %q: %w", id, err)
	}

	return id[:idx], runID, nil
}
