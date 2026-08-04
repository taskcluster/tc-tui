package resource

import (
	"fmt"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// CreateTaskResource is a CommandAction-only virtual resource: it has no
// list/detail view of its own, existing purely so `:createtask`/`:newtask`
// is reachable globally rather than only from the tasks/taskgroup lists.
type CreateTaskResource struct {
	tc      taskcluster.Taskcluster
	history *taskDefHistory
}

func NewCreateTaskResource(tc taskcluster.Taskcluster, history *taskDefHistory) *CreateTaskResource {
	return &CreateTaskResource{tc: tc, history: history}
}

func (r *CreateTaskResource) CommandAction() Action {
	return createTaskAction(r.tc, r.history)
}

func (r *CreateTaskResource) Name() string      { return "createtask" }
func (r *CreateTaskResource) Aliases() []string { return []string{"newtask"} }
func (r *CreateTaskResource) Description() string {
	return "Create a task, edited in $EDITOR"
}
func (r *CreateTaskResource) Columns() []Column { return nil }
func (r *CreateTaskResource) List() ([]Row, error) {
	return nil, fmt.Errorf("createtask has no list view")
}
func (r *CreateTaskResource) Describe(id string) (Detail, error) {
	return Detail{}, fmt.Errorf("createtask has no detail view")
}
func (r *CreateTaskResource) RefreshInterval() time.Duration { return 0 }
