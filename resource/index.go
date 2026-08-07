package resource

import (
	"fmt"
	"strings"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// TaskIndexResource is a DirectScopedResource: typing a namespace browses
// its children (sub-namespaces and immediate child tasks); typing a full,
// exact index path resolves straight to that path's currently indexed task
// instead (tried first in ScopedList, since there's no way to tell the two
// apart from the string alone).
//
// It's also RootBrowsable: the index is a tree whose root ("") is a namespace
// like any other, so a bare `:index` lists the top-level namespaces rather
// than demanding a path the user may not know yet.
type TaskIndexResource struct {
	tc taskcluster.Taskcluster
}

func NewTaskIndexResource(tc taskcluster.Taskcluster) *TaskIndexResource {
	return &TaskIndexResource{tc: tc}
}

func (r *TaskIndexResource) Name() string      { return "index" }
func (r *TaskIndexResource) Aliases() []string { return []string{"idx"} }
func (r *TaskIndexResource) Description() string {
	return "Browse the task index from the top or from a namespace, or resolve a full index path directly to its task"
}
func (r *TaskIndexResource) IDPromptLabel() string { return "namespace or full index path" }

func (r *TaskIndexResource) Columns() []Column {
	return []Column{
		{Title: "TYPE", Width: 10},
		{Title: "NAME", Expand: true},
		{Title: "TASK ID", Width: taskIDColumnWidth},
		{Title: "EXPIRES", Width: 24},
	}
}

func (r *TaskIndexResource) BrowsesRoot() {}

// List browses the root namespace: the top-level index namespaces. No
// FindIndexedTask probe here, unlike ScopedList — the root is never itself an
// indexed task's path.
func (r *TaskIndexResource) List() ([]Row, error) {
	return r.browse("")
}

func (r *TaskIndexResource) ScopedList(namespace string) ([]Row, error) {
	task, err := r.tc.FindIndexedTask(namespace)
	if err != nil {
		return nil, err
	}
	if task != nil {
		return []Row{{
			ID:        task.TaskID,
			Cells:     []string{"task", namespace, task.TaskID, fmt.Sprint(task.Expires)},
			NavTarget: &NavTarget{ResourceName: "task", ID: task.TaskID, Kind: NavDetail},
		}}, nil
	}

	return r.browse(namespace)
}

// browse lists namespace's children: its sub-namespaces, then the tasks
// indexed directly beneath it. Task names are shown relative to namespace,
// which for the root ("") leaves them full — there's nothing to trim.
func (r *TaskIndexResource) browse(namespace string) ([]Row, error) {
	namespaces, err := r.tc.GetIndexNamespaces(namespace)
	if err != nil {
		return nil, err
	}
	tasks, err := r.tc.GetIndexTasks(namespace)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(namespaces)+len(tasks))
	for _, ns := range namespaces {
		rows = append(rows, Row{
			ID:        ns.Namespace,
			Cells:     []string{"namespace", ns.Name, "", fmt.Sprint(ns.Expires)},
			NavTarget: &NavTarget{ResourceName: "index", ID: ns.Namespace, Kind: NavScopedList},
		})
	}
	for _, t := range tasks {
		name := strings.TrimPrefix(strings.TrimPrefix(t.Namespace, namespace), ".")
		rows = append(rows, Row{
			ID:        t.TaskID,
			Cells:     []string{"task", name, t.TaskID, fmt.Sprint(t.Expires)},
			NavTarget: &NavTarget{ResourceName: "task", ID: t.TaskID, Kind: NavDetail},
		})
	}
	return rows, nil
}

// EmptyScopeResource is unreachable — an empty scope browses the root (see
// List) rather than redirecting — and empty anyway: there is nothing above
// the root to redirect to.
func (r *TaskIndexResource) EmptyScopeResource() string { return "" }

// Describe is unreachable — every row overrides navigation via NavTarget,
// straight into either a deeper index ScopedList or a task's own Detail.
func (r *TaskIndexResource) Describe(id string) (Detail, error) {
	return Detail{}, fmt.Errorf("index entries are not viewable directly")
}

func (r *TaskIndexResource) RefreshInterval() time.Duration { return 15 * time.Second }
