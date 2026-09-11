package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tchooks"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestHookFiresResourceScopedList(t *testing.T) {
	older := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	fake := &fakeTaskcluster{
		hookLastFires: taskcluster.HookLastFireList{
			// Deliberately oldest-first: ScopedList must sort newest-first.
			{TaskID: "OLDERTASKIDxxxxxxxxxxx", Result: "success", FiredBy: "schedule", TaskState: "completed", TaskCreateTime: tcclient.Time(older)},
			{TaskID: "NEWERTASKIDxxxxxxxxxxx", Result: "error", FiredBy: "triggerHook", TaskState: "unknown", TaskCreateTime: tcclient.Time(newer)},
		},
	}
	res := NewHookFiresResource(fake)

	rows, err := res.ScopedList("project-a/ci/nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "NEWERTASKIDxxxxxxxxxxx" {
		t.Fatalf("expected newest fire first, got %+v", rows[0])
	}
	if rows[0].Cells[0] != "NEWERTASKIDxxxxxxxxxxx" {
		t.Fatalf("unexpected cells: %+v", rows[0].Cells)
	}
	if !strings.Contains(rows[0].Cells[1], "error") || !strings.Contains(rows[1].Cells[1], "success") {
		t.Fatalf("unexpected result cells: %+v, %+v", rows[0].Cells, rows[1].Cells)
	}
	if !strings.Contains(rows[1].Cells[2], "completed") {
		t.Fatalf("unexpected state cell: %+v", rows[1].Cells)
	}
	if rows[0].Cells[3] != "triggerHook" {
		t.Fatalf("unexpected fired-by cell: %+v", rows[0].Cells)
	}
	// Every row jumps straight to its fire's task.
	want := NavTarget{ResourceName: "task", ID: "NEWERTASKIDxxxxxxxxxxx", Kind: NavDetail}
	if rows[0].NavTarget == nil || *rows[0].NavTarget != want {
		t.Fatalf("unexpected nav target: %+v", rows[0].NavTarget)
	}
}

func TestHookFiresResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{hookLastFiresErr: wantErr}
	res := NewHookFiresResource(fake)

	_, err := res.ScopedList("project-a/nightly")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestHookFiresResourceScopedListMalformedScope(t *testing.T) {
	res := NewHookFiresResource(&fakeTaskcluster{})

	if _, err := res.ScopedList("no-separator"); err == nil {
		t.Fatal("expected an error for a scope with no group/hook separator")
	}
}

func TestHookFiresResourceEmptyScope(t *testing.T) {
	res := NewHookFiresResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "hooks" {
		t.Fatalf("unexpected empty scope resource: %s", got)
	}
	if _, err := res.List(); err == nil {
		t.Fatal("expected List without a scope to error")
	}
}

func TestHookFiresResourceWebURLs(t *testing.T) {
	res := NewHookFiresResource(&fakeTaskcluster{})

	// No dedicated fires page in the web UI — link to the hook itself.
	if got := res.ListWebURL("https://tc.example.com", "project-a/ci/nightly"); got != "https://tc.example.com/hooks/project-a/ci%2Fnightly" {
		t.Fatalf("unexpected list web url: %s", got)
	}
}

func TestHooksResourceDescribeHasFiresAction(t *testing.T) {
	fake := &fakeTaskcluster{
		hook: &tchooks.HookDefinition{
			HookGroupID: "project-a",
			HookID:      "nightly",
			Metadata:    tchooks.HookMetadata{Name: "Nightly build"},
		},
	}
	res := NewHooksResource(fake)

	detail, err := res.Describe("project-a/nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var firesAction *DetailAction
	for i := range detail.Actions {
		if detail.Actions[i].Key == 'f' {
			firesAction = &detail.Actions[i]
		}
	}
	if firesAction == nil {
		t.Fatalf("expected an 'f' action, got %+v", detail.Actions)
	}
	want := NavTarget{ResourceName: "hookfires", ID: "project-a/nightly", Kind: NavScopedList}
	if firesAction.Target != want {
		t.Fatalf("unexpected 'f' target: %+v", firesAction.Target)
	}
}
