package resource

import (
	"errors"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v101/clients/client-go"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcindex"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestTaskIndexResourceScopedListResolvesExactPath(t *testing.T) {
	fake := &fakeTaskcluster{
		findIndexedTask: &tcindex.IndexedTaskResponse{
			TaskID:  "task-1",
			Expires: tcclient.Time(time.Now()),
		},
	}
	res := NewTaskIndexResource(fake)

	rows, err := res.ScopedList("gecko.v2.mozilla-central.latest.firefox.linux64-opt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.ID != "task-1" || row.NavTarget == nil ||
		row.NavTarget.ResourceName != "task" || row.NavTarget.ID != "task-1" || row.NavTarget.Kind != NavDetail {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestTaskIndexResourceScopedListFallsThroughToBrowse(t *testing.T) {
	fake := &fakeTaskcluster{
		findIndexedTask: nil, // not found — the fake's zero value; ScopedList must fall through
		indexNamespaces: taskcluster.IndexNamespaceList{
			{Namespace: "gecko.v2.mozilla-central.latest.firefox", Name: "firefox", Expires: tcclient.Time(time.Now())},
		},
		indexTasks: taskcluster.IndexTaskList{
			{Namespace: "gecko.v2.mozilla-central.latest.geckoview", TaskID: "task-2", Expires: tcclient.Time(time.Now())},
		},
	}
	res := NewTaskIndexResource(fake)

	rows, err := res.ScopedList("gecko.v2.mozilla-central.latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	nsRow := rows[0]
	if nsRow.Cells[0] != "namespace" || nsRow.NavTarget == nil ||
		nsRow.NavTarget.ResourceName != "index" || nsRow.NavTarget.Kind != NavScopedList ||
		nsRow.NavTarget.ID != "gecko.v2.mozilla-central.latest.firefox" {
		t.Fatalf("unexpected namespace row: %+v", nsRow)
	}

	taskRow := rows[1]
	if taskRow.Cells[0] != "task" || taskRow.Cells[1] != "geckoview" || taskRow.Cells[2] != "task-2" || taskRow.NavTarget == nil ||
		taskRow.NavTarget.ResourceName != "task" || taskRow.NavTarget.ID != "task-2" || taskRow.NavTarget.Kind != NavDetail {
		t.Fatalf("unexpected task row: %+v", taskRow)
	}
}

func TestTaskIndexResourceScopedListPropagatesFindError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{findIndexedTaskErr: wantErr}
	res := NewTaskIndexResource(fake)

	_, err := res.ScopedList("some.path")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskIndexResourceScopedListPropagatesNamespacesError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{indexNamespacesErr: wantErr}
	res := NewTaskIndexResource(fake)

	_, err := res.ScopedList("some.path")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskIndexResourceScopedListPropagatesTasksError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{indexTasksErr: wantErr}
	res := NewTaskIndexResource(fake)

	_, err := res.ScopedList("some.path")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestTaskIndexResourceListBrowsesTheRootNamespace(t *testing.T) {
	fake := &fakeTaskcluster{
		indexNamespaces: taskcluster.IndexNamespaceList{
			{Namespace: "gecko", Name: "gecko", Expires: tcclient.Time(time.Now())},
		},
		indexTasks: taskcluster.IndexTaskList{
			{Namespace: "toplevel", TaskID: "task-3", Expires: tcclient.Time(time.Now())},
		},
	}
	res := NewTaskIndexResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.indexNamespacesArg != "" || fake.indexTasksArg != "" {
		t.Fatalf("expected the root namespace to be listed as %q, got namespaces=%q tasks=%q",
			"", fake.indexNamespacesArg, fake.indexTasksArg)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	nsRow := rows[0]
	if nsRow.Cells[0] != "namespace" || nsRow.Cells[1] != "gecko" || nsRow.NavTarget == nil ||
		nsRow.NavTarget.ResourceName != "index" || nsRow.NavTarget.Kind != NavScopedList ||
		nsRow.NavTarget.ID != "gecko" {
		t.Fatalf("unexpected namespace row: %+v", nsRow)
	}

	// Nothing to trim at the root: a task's name is its full namespace.
	taskRow := rows[1]
	if taskRow.Cells[0] != "task" || taskRow.Cells[1] != "toplevel" || taskRow.Cells[2] != "task-3" {
		t.Fatalf("unexpected task row: %+v", taskRow)
	}
}

func TestTaskIndexResourceDescribeIsUnreachable(t *testing.T) {
	res := NewTaskIndexResource(&fakeTaskcluster{})

	if _, err := res.Describe("task-1"); err == nil {
		t.Fatalf("expected an error — every row overrides navigation via NavTarget")
	}
}

func TestTaskIndexResourceIDPromptLabel(t *testing.T) {
	res := NewTaskIndexResource(&fakeTaskcluster{})

	if got := res.IDPromptLabel(); got != "namespace or full index path" {
		t.Fatalf("unexpected label: %q", got)
	}
}

func TestTaskIndexResourceEmptyScopeResource(t *testing.T) {
	res := NewTaskIndexResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "" {
		t.Fatalf("expected no empty-scope redirect target, got %q", got)
	}
}
