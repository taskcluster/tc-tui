package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestLaunchConfigsResourceFacetOptions(t *testing.T) {
	res := NewLaunchConfigsResource(&fakeTaskcluster{})

	got := res.FacetOptions()
	want := []string{"active", "all"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestLaunchConfigsResourceFacetListActive(t *testing.T) {
	fake := &fakeTaskcluster{
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{WorkerPoolID: "gcp/pool-a", LaunchConfigID: "lc-1", IsArchived: false},
		},
	}
	res := NewLaunchConfigsResource(fake)

	rows, err := res.FacetList("gcp/pool-a", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.launchConfigsIncludeArchived {
		t.Fatalf("expected includeArchived=false for the %q tab", "active")
	}
	if len(rows) != 1 || rows[0].ID != "gcp/pool-a::lc-1" || rows[0].Cells[0] != "lc-1" || rows[0].Cells[2] != "no" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestLaunchConfigsResourceFacetListAll(t *testing.T) {
	fake := &fakeTaskcluster{
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{WorkerPoolID: "gcp/pool-a", LaunchConfigID: "lc-1", IsArchived: true},
		},
	}
	res := NewLaunchConfigsResource(fake)

	rows, err := res.FacetList("gcp/pool-a", "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.launchConfigsIncludeArchived {
		t.Fatalf("expected includeArchived=true for the %q tab", "all")
	}
	if len(rows) != 1 || rows[0].Cells[2] != "yes" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestLaunchConfigsResourceFacetListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{launchConfigsErr: wantErr}
	res := NewLaunchConfigsResource(fake)

	_, err := res.FacetList("gcp/pool-a", "active")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestLaunchConfigsResourceFacetCounts(t *testing.T) {
	fake := &fakeTaskcluster{
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{WorkerPoolID: "gcp/pool-a", LaunchConfigID: "lc-1", IsArchived: false},
			{WorkerPoolID: "gcp/pool-a", LaunchConfigID: "lc-2", IsArchived: true},
		},
	}
	res := NewLaunchConfigsResource(fake)

	counts, err := res.FacetCounts("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts["active"] != 1 || counts["all"] != 2 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}

func TestLaunchConfigsResourceFacetCountsError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{launchConfigsErr: wantErr}
	res := NewLaunchConfigsResource(fake)

	_, err := res.FacetCounts("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestLaunchConfigsResourceScopedListDelegatesToDefaultFacet(t *testing.T) {
	fake := &fakeTaskcluster{}
	res := NewLaunchConfigsResource(fake)

	if _, err := res.ScopedList("gcp/pool-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.launchConfigsIncludeArchived {
		t.Fatalf("expected ScopedList to default to the first facet option %q (includeArchived=false)", "active")
	}
}

func TestLaunchConfigsResourceListReturnsScopeRequiredError(t *testing.T) {
	res := NewLaunchConfigsResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestLaunchConfigsResourceEmptyScopeResource(t *testing.T) {
	res := NewLaunchConfigsResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "workerpools" {
		t.Fatalf("expected %q, got %q", "workerpools", got)
	}
}

func TestLaunchConfigsResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{
				WorkerPoolID:   "gcp/pool-a",
				LaunchConfigID: "lc-1",
				IsArchived:     false,
				Created:        tcclient.Time(time.Now()),
				LastModified:   tcclient.Time(time.Now()),
				Configuration:  []byte(`{"minCapacity":1}`),
			},
		},
	}
	res := NewLaunchConfigsResource(fake)

	detail, err := res.Describe("gcp/pool-a::lc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Launch Config :: lc-1" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if len(detail.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(detail.Actions))
	}
	wantWorkersTarget := NavTarget{ResourceName: "workers", ID: "gcp/pool-a::lc-1", Kind: NavScopedList}
	if detail.Actions[0].Key != 'w' || detail.Actions[0].Target != wantWorkersTarget {
		t.Fatalf("unexpected workers action: %+v", detail.Actions[0])
	}
	wantErrorsTarget := NavTarget{ResourceName: "errors", ID: "gcp/pool-a::lc-1", Kind: NavScopedList}
	if detail.Actions[1].Key != 'e' || detail.Actions[1].Target != wantErrorsTarget {
		t.Fatalf("unexpected errors action: %+v", detail.Actions[1])
	}
	if strings.Contains(detail.Body, `{"minCapacity":1}`) {
		t.Fatalf("expected the raw single-line JSON blob to be gone, got: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "minCapacity") {
		t.Fatalf("expected the rendered configuration in the body, got: %s", detail.Body)
	}
}

func TestLaunchConfigsResourceDescribeGroupsCreatedModifiedArchivedOnOneLine(t *testing.T) {
	fake := &fakeTaskcluster{
		launchConfigs: taskcluster.WorkerPoolLaunchConfigList{
			{WorkerPoolID: "gcp/pool-a", LaunchConfigID: "lc-1", IsArchived: true},
		},
	}
	res := NewLaunchConfigsResource(fake)

	detail, err := res.Describe("gcp/pool-a::lc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := stripRegionTags(detail.Body)
	found := false
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Archived") {
			found = true
			if !strings.Contains(line, "Created") || !strings.Contains(line, "Last Modified") {
				t.Fatalf("expected Created/Last Modified/Archived on the same line, got: %q", line)
			}
		}
	}
	if !found {
		t.Fatalf("Archived not found in body: %s", body)
	}
}

func TestLaunchConfigsResourceDescribeNotFound(t *testing.T) {
	fake := &fakeTaskcluster{launchConfigs: taskcluster.WorkerPoolLaunchConfigList{}}
	res := NewLaunchConfigsResource(fake)

	if _, err := res.Describe("gcp/pool-a::lc-missing"); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestLaunchConfigsResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{launchConfigsErr: wantErr}
	res := NewLaunchConfigsResource(fake)

	_, err := res.Describe("gcp/pool-a::lc-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestLaunchConfigsResourceScopeActionsExcludesLaunchConfigs(t *testing.T) {
	res := NewLaunchConfigsResource(&fakeTaskcluster{})

	actions := res.ScopeActions("gcp/pool-a")
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Target.ResourceName == "launchconfigs" {
			t.Fatalf("expected \"launchconfigs\" excluded from its own sibling actions, got %+v", actions)
		}
		if a.Target.ID != "gcp/pool-a" {
			t.Fatalf("expected actions scoped pool-wide to %q, got %+v", "gcp/pool-a", a)
		}
	}
}
