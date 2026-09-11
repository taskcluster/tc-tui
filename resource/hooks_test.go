package resource

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tchooks"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestHooksResourceList(t *testing.T) {
	fake := &fakeTaskcluster{
		hooks: taskcluster.HookList{
			{
				HookGroupID: "project-a",
				HookID:      "nightly",
				Metadata:    tchooks.HookMetadata{Name: "Nightly build"},
				Schedule:    []string{"0 0 * * *", "0 12 * * *"},
			},
			{
				HookGroupID: "project-b",
				HookID:      "ci/on-push",
				Metadata:    tchooks.HookMetadata{Name: "CI"},
			},
		},
	}
	res := NewHooksResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "project-a/nightly" || rows[0].Cells[0] != "project-a/nightly" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	if rows[0].Cells[1] != "Nightly build" || rows[0].Cells[2] != "0 0 * * *, 0 12 * * *" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	// A hook ID may itself contain "/" — the row ID must still round-trip.
	if rows[1].ID != "project-b/ci/on-push" {
		t.Fatalf("unexpected row: %+v", rows[1])
	}
	if rows[1].Cells[2] != "" {
		t.Fatalf("expected empty schedule cell, got %q", rows[1].Cells[2])
	}
}

func TestHooksResourceListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{hooksErr: wantErr}
	res := NewHooksResource(fake)

	_, err := res.List()
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestHooksResourceDescribe(t *testing.T) {
	older := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	fake := &fakeTaskcluster{
		hook: &tchooks.HookDefinition{
			HookGroupID: "project-a",
			HookID:      "ci/nightly",
			Metadata: tchooks.HookMetadata{
				Name:         "Nightly build",
				Description:  "builds things nightly",
				Owner:        "eng@example.com",
				EmailOnError: true,
			},
			Schedule: []string{"0 0 * * *"},
			Task:     json.RawMessage(`{"provisionerId":"proj"}`),
		},
		hookLastFires: taskcluster.HookLastFireList{
			// Deliberately oldest-first: Describe must sort newest-first
			// and point the 't' action at the newest fire's task.
			{TaskID: "OLDERTASKIDxxxxxxxxxxx", Result: "success", FiredBy: "schedule", TaskState: "completed", TaskCreateTime: tcclient.Time(older)},
			{TaskID: "NEWERTASKIDxxxxxxxxxxx", Result: "error", FiredBy: "triggerHook", TaskState: "unknown", TaskCreateTime: tcclient.Time(newer), Error: "task creation failed"},
		},
	}
	res := NewHooksResource(fake)

	detail, err := res.Describe("project-a/ci/nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Hook :: project-a/ci/nightly" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}

	body := stripRegionTags(detail.Body)
	for _, want := range []string{
		"Nightly build", "eng@example.com", "builds things nightly",
		"0 0 * * *", "provisionerId",
		"OLDERTASKIDxxxxxxxxxxx", "NEWERTASKIDxxxxxxxxxxx", "task creation failed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, detail.Body)
		}
	}
	// Newest fire listed before the older one.
	if strings.Index(body, "NEWERTASKIDxxxxxxxxxxx") > strings.Index(body, "OLDERTASKIDxxxxxxxxxxx") {
		t.Fatalf("fires not sorted newest-first: %s", detail.Body)
	}

	var taskAction *DetailAction
	for i := range detail.Actions {
		if detail.Actions[i].Key == 't' {
			taskAction = &detail.Actions[i]
		}
	}
	if taskAction == nil {
		t.Fatalf("expected a 't' action, got %+v", detail.Actions)
	}
	want := NavTarget{ResourceName: "task", ID: "NEWERTASKIDxxxxxxxxxxx", Kind: NavDetail}
	if taskAction.Target != want {
		t.Fatalf("unexpected 't' target: %+v", taskAction.Target)
	}
}

func TestHooksResourceDescribeNoFires(t *testing.T) {
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
	for _, a := range detail.Actions {
		if a.Key == 't' {
			t.Fatalf("expected no 't' action without fires, got %+v", detail.Actions)
		}
	}
}

// Last fires are best-effort enrichment: a hook whose fires can't be listed
// (e.g. missing scopes) must still show its definition rather than failing
// the whole Describe.
func TestHooksResourceDescribeLastFiresError(t *testing.T) {
	fake := &fakeTaskcluster{
		hook: &tchooks.HookDefinition{
			HookGroupID: "project-a",
			HookID:      "nightly",
			Metadata:    tchooks.HookMetadata{Name: "Nightly build"},
		},
		hookLastFiresErr: errors.New("insufficient scopes"),
	}
	res := NewHooksResource(fake)

	detail, err := res.Describe("project-a/nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := stripRegionTags(detail.Body)
	if !strings.Contains(body, "insufficient scopes") {
		t.Fatalf("body should surface the last-fires error: %s", detail.Body)
	}
}

func TestHooksResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{hookErr: wantErr}
	res := NewHooksResource(fake)

	_, err := res.Describe("project-a/nightly")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestHooksResourceDescribeMalformedID(t *testing.T) {
	res := NewHooksResource(&fakeTaskcluster{})

	if _, err := res.Describe("no-group-separator"); err == nil {
		t.Fatal("expected an error for an id with no group/hook separator")
	}
}

func TestHooksResourceWebURLs(t *testing.T) {
	res := NewHooksResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", ""); got != "https://tc.example.com/hooks" {
		t.Fatalf("unexpected list web url: %s", got)
	}
	// The hook ID's own "/" must be escaped — it's one path segment in the
	// web UI's /hooks/:hookGroupId/:hookId route — while the group/hook
	// separator stays a real "/".
	if got := res.DetailWebURL("https://tc.example.com", "project-a/ci/nightly"); got != "https://tc.example.com/hooks/project-a/ci%2Fnightly" {
		t.Fatalf("unexpected detail web url: %s", got)
	}
}
