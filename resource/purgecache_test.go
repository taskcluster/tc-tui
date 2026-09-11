package resource

import (
	"errors"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestPurgeCacheResourceScopedList(t *testing.T) {
	before := tcclient.Time(time.Now())
	fake := &fakeTaskcluster{
		purgeCacheRequestsForPool: taskcluster.PurgeCacheRequestList{
			{ProvisionerID: "gcp", WorkerType: "pool-a", CacheName: "cache-1", Before: before},
		},
	}
	res := NewPurgeCacheResource(fake)

	rows, err := res.ScopedList("gcp/pool-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Cells[1] != "gcp" || row.Cells[2] != "pool-a" || row.Cells[3] != "cache-1" {
		t.Fatalf("unexpected row: %+v", row)
	}
	wantID := "gcp" + idSeparator + "pool-a" + idSeparator + "cache-1" + idSeparator + before.String()
	if row.ID != wantID {
		t.Fatalf("expected id %q, got %q", wantID, row.ID)
	}
}

func TestPurgeCacheResourceScopedListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{purgeCacheRequestsForPoolErr: wantErr}
	res := NewPurgeCacheResource(fake)

	_, err := res.ScopedList("gcp/pool-a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestPurgeCacheResourceScopedListInvalidWorkerPoolID(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})

	if _, err := res.ScopedList("not-a-valid-pool-id"); err == nil {
		t.Fatalf("expected an error for a worker pool id with no '/'")
	}
}

func TestPurgeCacheResourceListRequiresScope(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error for an unscoped List call")
	}
}

func TestPurgeCacheResourceEmptyScopeResource(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})

	if got := res.EmptyScopeResource(); got != "workerpools" {
		t.Fatalf("expected %q, got %q", "workerpools", got)
	}
}

func TestPurgeCacheResourceScopeActionsExcludesItself(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})

	actions := res.ScopeActions("gcp/pool-a")
	for _, a := range actions {
		if a.Target.ResourceName == "purgecache" {
			t.Fatalf("expected purgecache's own action to be excluded, got: %+v", actions)
		}
	}
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions (7 minus itself), got %d: %+v", len(actions), actions)
	}
}

func TestPurgeCacheResourceDescribeDecodesID(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})
	id := "gcp" + idSeparator + "pool-a" + idSeparator + "cache-1" + idSeparator + "2026-01-01T00:00:00.000Z"

	detail, err := res.Describe(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Purge Cache Request :: cache-1" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	found := false
	for _, a := range detail.Actions {
		if a.Target.ResourceName == "workerpools" && a.Target.ID == "gcp/pool-a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a workerpools action pointing at gcp/pool-a, got: %+v", detail.Actions)
	}
}

func TestPurgeCacheResourceDescribeMalformedID(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})

	if _, err := res.Describe("not-enough-parts"); err == nil {
		t.Fatalf("expected an error for a malformed id")
	}
}

func TestPurgeCacheResourceWebURL(t *testing.T) {
	res := NewPurgeCacheResource(&fakeTaskcluster{})

	got := res.ListWebURL("https://tc.example.com", "gcp/pool-a")
	want := "https://tc.example.com/worker-manager/gcp%2Fpool-a/purge-cache"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
