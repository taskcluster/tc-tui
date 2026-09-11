package taskcluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
)

func TestTaskQueueCountsBatches(t *testing.T) {
	var sizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/queue/v1/task-queues/counts" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			IDs []string `json:"taskQueueIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		sizes = append(sizes, len(body.IDs))
		counts := []map[string]any{}
		for i := len(body.IDs) - 1; i >= 0; i-- {
			counts = append(counts, map[string]any{"taskQueueId": body.IDs[i], "pendingTasks": 7, "claimedTasks": 0})
		}
		json.NewEncoder(w).Encode(map[string]any{"taskQueueCounts": counts})
	}))
	defer server.Close()
	tc := &TC{queue: tcqueue.New(nil, server.URL)}
	ids := make([]string, 1003)
	for i := range ids {
		ids[i] = fmt.Sprintf("prov/pool-%d", i)
	}
	ids[1] = ids[0] // Deduplicate the request, but preserve input callbacks.
	seen := map[string]int{}
	tc.GetTaskQueueCounts(ids, func(id string) bool { return id != ids[1000] }, func(id string, c TaskQueueCounts) {
		seen[id]++
		if id == ids[1000] {
			if c != (TaskQueueCounts{}) {
				t.Errorf("skipped counts: %+v", c)
			}
			return
		}
		if c != (TaskQueueCounts{Pending: 7, PendingKnown: true, ClaimedKnown: true}) {
			t.Errorf("counts for %s: %+v", id, c)
		}
	})
	if fmt.Sprint(sizes) != "[999 2]" {
		t.Fatalf("batch sizes: %v", sizes)
	}
	if len(seen) != 1002 || seen[ids[0]] != 2 {
		t.Fatalf("callbacks: %d unique, duplicate %d", len(seen), seen[ids[0]])
	}
}

// A deployment without the bulk endpoint (404) is the one failure worth
// retrying per-queue: the counts really are obtainable there, one call at a
// time, degrading to pending-count + claimed-list for a queue whose combined
// call is itself refused.
func TestTaskQueueCountsFallsBackForAnOlderDeployment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/queue/v1/task-queues/counts":
			w.WriteHeader(404)
		case "/api/queue/v1/task-queues/prov/good/counts":
			fmt.Fprint(w, `{"pendingTasks":5,"claimedTasks":2}`)
		case "/api/queue/v1/task-queues/prov/restricted/counts":
			w.WriteHeader(403)
		case "/api/queue/v1/pending/prov/restricted":
			fmt.Fprint(w, `{"pendingTasks":3}`)
		default:
			w.WriteHeader(403)
		}
	}))
	defer server.Close()
	tc := &TC{queue: tcqueue.New(nil, server.URL)}
	var mu sync.Mutex
	got := map[string]TaskQueueCounts{}
	err := tc.GetTaskQueueCounts([]string{"prov/good", "prov/restricted"}, func(string) bool { return true }, func(id string, c TaskQueueCounts) {
		mu.Lock()
		defer mu.Unlock()
		got[id] = c
	})
	if err != nil {
		t.Errorf("err = %v, want nil — the fallback obtained the counts", err)
	}
	if got["prov/good"] != (TaskQueueCounts{Pending: 5, PendingKnown: true, Claimed: 2, ClaimedKnown: true}) {
		t.Errorf("good: %+v", got)
	}
	if got["prov/restricted"] != (TaskQueueCounts{Pending: 3, PendingKnown: true}) {
		t.Errorf("restricted: %+v", got)
	}
}

// A scope denial is the failure NOT worth retrying per-queue: every per-queue
// call demands the same scope pair, so fanning out would spend one-to-two
// requests per pool to be refused all over again. Give up, report every id
// unknown, and hand the caller something to show the user.
func TestTaskQueueCountsGivesUpOnAScopeDenial(t *testing.T) {
	var mu sync.Mutex
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(403)
		fmt.Fprint(w, `{"code":"InsufficientScopes","message":"queue:claimed-count:prov/a"}`)
	}))
	defer server.Close()
	tc := &TC{queue: tcqueue.New(nil, server.URL)}

	// Two batches' worth, so the ids the give-up skips entirely (batch 2)
	// are covered as well as the ones its own batch abandoned.
	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = fmt.Sprintf("prov/pool-%d", i)
	}
	seen := map[string]int{}
	err := tc.GetTaskQueueCounts(ids, func(string) bool { return true }, func(id string, c TaskQueueCounts) {
		mu.Lock()
		defer mu.Unlock()
		seen[id]++
		if c.PendingKnown || c.ClaimedKnown {
			t.Errorf("counts for %s must be unknown: %+v", id, c)
		}
	})
	if !errors.Is(err, ErrTaskQueueCountScopes) {
		t.Errorf("err = %v, want ErrTaskQueueCountScopes", err)
	}
	if len(paths) != 1 || paths[0] != "/api/queue/v1/task-queues/counts" {
		t.Errorf("requests: %v, want the one bulk call and no per-pool fan-out", paths)
	}
	if len(seen) != len(ids) {
		t.Errorf("%d ids reported, want %d — onEach must still fire once per id", len(seen), len(ids))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("%s reported %d times, want 1", id, n)
		}
	}
}

func TestTaskQueueCountsRechecksWantedBetweenBatches(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `{"taskQueueCounts":[]}`)
	}))
	defer server.Close()
	tc := &TC{queue: tcqueue.New(nil, server.URL)}
	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = fmt.Sprintf("prov/pool-%d", i)
	}
	visible := true
	callbacks := 0
	tc.GetTaskQueueCounts(ids, func(string) bool { return visible }, func(_ string, c TaskQueueCounts) {
		callbacks++
		visible = false
		if c.PendingKnown || c.ClaimedKnown {
			t.Error("missing or skipped counts must remain unknown")
		}
	})
	if requests != 1 || callbacks != len(ids) {
		t.Fatalf("requests=%d callbacks=%d", requests, callbacks)
	}
	tc.GetTaskQueueCounts(nil, func(string) bool { return true }, func(string, TaskQueueCounts) { t.Error("unexpected callback") })
	if requests != 1 {
		t.Fatal("empty input sent a request")
	}
}

// A failed batch must not be retried for every remaining batch: one 1000-id
// request that fails the same way buys nothing over going straight to the
// per-queue fallback, which is where a scope failure can be isolated.
func TestTaskQueueCountsStopsBatchingAfterAFailure(t *testing.T) {
	var mu sync.Mutex
	batchRequests, individualRequests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/queue/v1/task-queues/counts" {
			batchRequests++
			w.WriteHeader(404) // a deployment without the bulk endpoint
			return
		}
		individualRequests++
		fmt.Fprint(w, `{"pendingTasks":5,"claimedTasks":2}`)
	}))
	defer server.Close()
	tc := &TC{queue: tcqueue.New(nil, server.URL)}
	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = fmt.Sprintf("prov/pool-%d", i)
	}
	got := 0
	tc.GetTaskQueueCounts(ids, func(string) bool { return true }, func(_ string, c TaskQueueCounts) {
		mu.Lock()
		defer mu.Unlock()
		got++
		if c != (TaskQueueCounts{Pending: 5, PendingKnown: true, Claimed: 2, ClaimedKnown: true}) {
			t.Errorf("counts: %+v", c)
		}
	})
	if batchRequests != 1 {
		t.Errorf("batch requests: %d, want 1", batchRequests)
	}
	if individualRequests != len(ids) || got != len(ids) {
		t.Errorf("individual requests=%d callbacks=%d, want %d each", individualRequests, got, len(ids))
	}
}
