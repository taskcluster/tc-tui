package resource

import (
	"fmt"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// TaskGroupResource is a DirectScopedResource: Taskcluster's Queue API has no
// "list all task groups" endpoint, so a task group is only ever reached
// directly by its own ID — but once given one, there's little value in a
// separate metadata-only page, so it renders the same task list TasksResource
// does (see taskListRows/taskListColumns), with the group's sealed status
// surfaced via Subtitle rather than a page of its own.
type TaskGroupResource struct {
	tc         taskcluster.Taskcluster
	history    *taskDefHistory
	stateCache *taskStateCache
}

func NewTaskGroupResource(tc taskcluster.Taskcluster, history *taskDefHistory, stateCache *taskStateCache) *TaskGroupResource {
	return &TaskGroupResource{tc: tc, history: history, stateCache: stateCache}
}

// Actions splits by context on the empty-id convention.
// The taskgroup *list* (id == "", reached via `:g <id>` or a task's 'g' jump)
// offers create-task — the natural place to create a task, without needing the
// `:tasks <id>` alias. Selecting a task row opens that task's detail rendered by
// this same resource, so a non-empty id offers that task's lifecycle actions.
func (r *TaskGroupResource) Actions(id string) []Action {
	if id == "" {
		return []Action{createTaskAction(r.tc, r.history)}
	}
	return lifecycleActions(r.tc, r.stateCache, id)
}

func (r *TaskGroupResource) Name() string      { return "taskgroup" }
func (r *TaskGroupResource) Aliases() []string { return []string{"g"} }
func (r *TaskGroupResource) Description() string {
	return "Tasks belonging to one task group, looked up directly by task group ID"
}
func (r *TaskGroupResource) IDPromptLabel() string { return "task group id" }

func (r *TaskGroupResource) Columns() []Column {
	return taskListColumns()
}

// List is never expected to be called via normal navigation — a
// DirectScopedResource always either has a scope, or opens an id prompt
// first.
func (r *TaskGroupResource) List() ([]Row, error) {
	return nil, fmt.Errorf("taskgroup requires a task group id")
}

func (r *TaskGroupResource) ScopedList(taskGroupID string) ([]Row, error) {
	rows, _, err := r.ListPartial(taskGroupID, "", false)
	return rows, err
}

// ListPartial fetches the group's tasks capped at the safe limit unless
// loadAll is set — see resource.PartialLister.
func (r *TaskGroupResource) ListPartial(taskGroupID, _ string, loadAll bool) ([]Row, bool, error) {
	tasks, more, err := r.tc.GetTaskGroupTasks(taskGroupID, partialListLimit(loadAll))
	if err != nil {
		return nil, false, err
	}

	return taskListRows(tasks), more, nil
}

// EmptyScopeResource is unreachable — this is a DirectScopedResource, so the
// shell prompts for a task group id first — but it names tasks rather than an
// unrelated resource in case that routing ever changes.
func (r *TaskGroupResource) EmptyScopeResource() string {
	return "tasks"
}

// Subtitle reports whether the task group is sealed, shown in the list
// view's title bar alongside its scope.
func (r *TaskGroupResource) Subtitle(taskGroupID string) (string, error) {
	group, err := r.tc.GetTaskGroup(taskGroupID)
	if err != nil {
		return "", err
	}

	if time.Time(group.Sealed).IsZero() {
		return "not sealed", nil
	}
	return fmt.Sprintf("sealed %s", group.Sealed), nil
}

func (r *TaskGroupResource) Describe(id string) (Detail, error) {
	return describeTask(r.tc, r.stateCache, id)
}

func (r *TaskGroupResource) RefreshInterval() time.Duration {
	return 15 * time.Second
}

// ListWebURL links to the task group's own page — same page TasksResource
// links to, since both list the same task group's tasks.
func (r *TaskGroupResource) ListWebURL(rootURL, scope string) string {
	return taskGroupWebURL(rootURL, scope)
}

func (r *TaskGroupResource) DetailWebURL(rootURL, id string) string {
	return taskWebURL(rootURL, id)
}

// taskGroupWebURL links to a task group's own page — the web UI's task
// group page also lists that group's tasks, so this doubles as the link for
// TasksResource's List view. Shared by TaskGroupResource and TasksResource.
func taskGroupWebURL(rootURL, taskGroupID string) string {
	return webUIPath(rootURL, "tasks/groups/"+pathSegment(taskGroupID))
}
