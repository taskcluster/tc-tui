package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcworkermanager"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestErrorsResourceScopedListPoolWide(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPoolErrors: taskcluster.WorkerPoolErrorList{
			{
				WorkerPoolID:   "gcp/pool-a",
				ErrorID:        "err-1",
				Title:          "launch failed",
				Kind:           "provider-error",
				LaunchConfigID: "lc-1",
				Reported:       tcclient.Time(time.Now()),
			},
		},
	}
	res := NewErrorsResource(fake)

	rows, err := res.ScopedList("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workerPoolErrorsLaunchConfigID != "" {
		t.Fatalf("expected no launch config filter, got %q", fake.workerPoolErrorsLaunchConfigID)
	}
	if len(rows) != 1 || rows[0].ID != "gcp/pool-a::err-1" || rows[0].Cells[1] != "launch failed" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestErrorsResourceScopedListLaunchConfigScoped(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewErrorsResource(fake)

	if _, err := res.ScopedList("gcp/pool-a::lc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.workerPoolErrorsLaunchConfigID != "lc-1" {
		t.Fatalf("expected launch config filter %q, got %q", "lc-1", fake.workerPoolErrorsLaunchConfigID)
	}
}

func TestErrorsResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workerPoolErrorsErr: wantErr}
	res := NewErrorsResource(fake)

	_, err := res.ScopedList("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestErrorsResourceListReturnsScopeRequiredError(t *testing.T) {
	res := NewErrorsResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestErrorsResourceEmptyScopeResource(t *testing.T) {
	res := NewErrorsResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "workerpools" {
		t.Fatalf("expected %q, got %q", "workerpools", got)
	}
}

func TestErrorsResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPoolError: &tcworkermanager.WorkerPoolError{
			WorkerPoolID:   "gcp/pool-a",
			ErrorID:        "err-1",
			Title:          "launch failed",
			Kind:           "provider-error",
			Description:    "something went wrong",
			LaunchConfigID: "lc-1",
			Reported:       tcclient.Time(time.Now()),
		},
	}
	res := NewErrorsResource(fake)

	detail, err := res.Describe("gcp/pool-a::err-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Worker Pool Error :: err-1" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	body := stripRegionTags(detail.Body)
	if !strings.Contains(body, "launch failed") || !strings.Contains(body, "something went wrong") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
	if len(detail.Actions) != 6 {
		t.Fatalf("expected 6 sibling actions, got %d: %+v", len(detail.Actions), detail.Actions)
	}
}

func TestErrorsResourceDescribeRendersExtraAsYAML(t *testing.T) {
	fake := &fakeTaskcluster{
		workerPoolError: &tcworkermanager.WorkerPoolError{
			WorkerPoolID: "gcp/pool-a",
			ErrorID:      "err-1",
			Title:        "launch failed",
			Extra:        []byte(`{"code":"QUOTA_EXCEEDED"}`),
		},
	}
	res := NewErrorsResource(fake)

	detail, err := res.Describe("gcp/pool-a::err-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "QUOTA_EXCEEDED") {
		t.Fatalf("expected the rendered extra field in the body, got: %s", detail.Body)
	}
	if strings.Contains(detail.Body, `{"code":"QUOTA_EXCEEDED"}`) {
		t.Fatalf("expected the raw single-line JSON blob to be gone, got: %s", detail.Body)
	}
}

func TestErrorsResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{workerPoolErrorErr: wantErr}
	res := NewErrorsResource(fake)

	_, err := res.Describe("gcp/pool-a::err-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestErrorsResourceScopeActionsExcludesErrors(t *testing.T) {
	res := NewErrorsResource(&fakeTaskcluster{})

	actions := res.ScopeActions("gcp/pool-a")
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ResourceName == "errors" {
			t.Fatalf("expected \"errors\" excluded from its own sibling actions, got %+v", actions)
		}
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped pool-wide to %q, got %+v", "gcp/pool-a", a)
		}
	}
}

func TestErrorsResourceScopeActionsWithLaunchConfigScope(t *testing.T) {
	res := NewErrorsResource(&fakeTaskcluster{})

	actions := res.ScopeActions("gcp/pool-a::lc-1")
	for _, a := range actions {
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped to the bare pool id %q even from a launch-config-scoped scope, got %+v", "gcp/pool-a", a)
		}
	}
}
