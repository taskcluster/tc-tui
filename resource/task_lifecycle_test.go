package resource

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/taskcluster/slugid-go/slugid"
	tcclient "github.com/taskcluster/taskcluster/v101/clients/client-go"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcqueue"
)

var errTest = errors.New("boom")

// sampleTaskResponse builds a valid, stale (2020-dated) task definition
// response, used to exercise retrigger. Slices/extra are left nil on purpose so
// they marshal to JSON null and exercise the null-stripping in
// buildRetriggerDefinition.
func sampleTaskResponse() *tcqueue.TaskDefinitionResponse {
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	return &tcqueue.TaskDefinitionResponse{
		ProvisionerID: "proj-taskcluster",
		WorkerType:    "gw-ci",
		TaskQueueID:   "proj-taskcluster/gw-ci",
		SchedulerID:   "-",
		TaskGroupID:   slugid.Nice(),
		Priority:      "lowest",
		Requires:      "all-completed",
		Retries:       5,
		Created:       tcclient.Time(created),
		Deadline:      tcclient.Time(created.Add(time.Hour)),
		Expires:       tcclient.Time(created.AddDate(0, 0, 31)),
		Payload:       json.RawMessage(`{"command":["echo","hi"],"maxRunTime":600}`),
		Metadata: tcqueue.TaskMetadata{
			Name:        "hello",
			Description: "a hello task",
			Owner:       "me@example.com",
			Source:      "https://example.com",
		},
	}
}

// lifecycleKeys returns the action keys lifecycleActions offers for a task
// whose state/priority are seeded into a fresh cache, sorted for comparison.
func lifecycleKeys(t *testing.T, tc *fakeTaskcluster, state string) []rune {
	t.Helper()
	cache := NewTaskStateCache()
	cache.record("T1", state, "lowest", time.Now())
	acts := lifecycleActions(tc, cache, "T1")
	keys := make([]rune, 0, len(acts))
	for _, a := range acts {
		keys = append(keys, a.Key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortedRunes(rs ...rune) []rune {
	out := append([]rune(nil), rs...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestLifecycleActionsStateMapping(t *testing.T) {
	cases := []struct {
		state string
		keys  []rune
	}{
		// unscheduled: schedule + cancel + priority + retrigger
		{"unscheduled", sortedRunes('S', 'K', 'P', 'T')},
		// pending/running: cancel + priority + retrigger (no schedule)
		{"pending", sortedRunes('K', 'P', 'T')},
		{"running", sortedRunes('K', 'P', 'T')},
		// resolved states: rerun + retrigger only
		{"completed", sortedRunes('E', 'T')},
		{"failed", sortedRunes('E', 'T')},
		{"exception", sortedRunes('E', 'T')},
	}

	tc := &fakeTaskcluster{}
	for _, c := range cases {
		got := lifecycleKeys(t, tc, c.state)
		if len(got) != len(c.keys) {
			t.Fatalf("state %q: got keys %q, want %q", c.state, string(got), string(c.keys))
		}
		for i := range got {
			if got[i] != c.keys[i] {
				t.Fatalf("state %q: got keys %q, want %q", c.state, string(got), string(c.keys))
			}
		}
	}
}

// A missing/stale snapshot leaves only retrigger, the one action needing no
// state.
func TestLifecycleActionsWithoutSnapshotOffersRetriggerOnly(t *testing.T) {
	tc := &fakeTaskcluster{}
	cache := NewTaskStateCache()
	acts := lifecycleActions(tc, cache, "unknown")
	if len(acts) != 1 || acts[0].Key != 'T' {
		keys := make([]rune, len(acts))
		for i, a := range acts {
			keys[i] = a.Key
		}
		t.Fatalf("missing snapshot: got keys %q, want just 'T'", string(keys))
	}
}

// An expired snapshot (older than the TTL) is treated as missing.
func TestTaskStateCacheTTL(t *testing.T) {
	cache := NewTaskStateCache()
	now := time.Now()
	cache.record("T1", "running", "high", now.Add(-2*taskStateTTL))
	if _, ok := cache.lookup("T1", now); ok {
		t.Fatal("expected expired snapshot to be treated as missing")
	}
	cache.record("T2", "running", "high", now)
	if snap, ok := cache.lookup("T2", now); !ok || snap.state != "running" || snap.priority != "high" {
		t.Fatalf("fresh snapshot lookup = %+v, ok=%v", snap, ok)
	}
}

// A nil cache is safe (returns not-found), so lifecycleActions never panics
// when no cache is wired.
func TestTaskStateCacheNilSafe(t *testing.T) {
	var cache *taskStateCache
	if _, ok := cache.lookup("x", time.Now()); ok {
		t.Fatal("nil cache should report not-found")
	}
}

func TestBuildRetriggerDefinition(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	tc := &fakeTaskcluster{task: sampleTaskResponse()}

	id, body, err := buildRetriggerDefinition(tc, "SRC", now)
	if err != nil {
		t.Fatalf("buildRetriggerDefinition: %v", err)
	}
	if !slugidRe.MatchString(id) {
		t.Fatalf("id %q is not a valid slugid", id)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("body is not a JSON object: %v", err)
	}

	// retries reset to zero, submitted verbatim (not dropped as an omitempty 0)
	var retries int
	if err := json.Unmarshal(top["retries"], &retries); err != nil || retries != 0 {
		t.Fatalf("retries = %v (err %v), want 0", string(top["retries"]), err)
	}

	// timestamps rebased to now, preserving the original 1h created->deadline gap
	var created, deadline string
	json.Unmarshal(top["created"], &created)
	json.Unmarshal(top["deadline"], &deadline)
	gotCreated, _ := time.Parse(time.RFC3339, created)
	gotDeadline, _ := time.Parse(time.RFC3339, deadline)
	if !gotCreated.Equal(now) {
		t.Errorf("created = %s, want %s", gotCreated, now)
	}
	if got := gotDeadline.Sub(gotCreated); got != time.Hour {
		t.Errorf("deadline-created gap = %s, want 1h", got)
	}

	// null-valued fields (nil slices/extra) were stripped, not submitted as null
	for _, k := range []string{"extra", "dependencies", "scopes", "routes", "tags"} {
		if raw, present := top[k]; present && isJSONNull(raw) {
			t.Errorf("field %q submitted as null; should have been stripped", k)
		}
	}
}

func TestBuildRetriggerDefinitionPropagatesFetchError(t *testing.T) {
	tc := &fakeTaskcluster{taskErr: errTest}
	if _, _, err := buildRetriggerDefinition(tc, "SRC", time.Now()); err == nil {
		t.Fatal("expected GetTask error to propagate")
	}
}

// A retry after a failed CreateTask must resubmit the identical (taskId, body)
// so Queue.createTask stays idempotent — not mint a new slugid.
func TestRetriggerActionMemoizesIdOnRetry(t *testing.T) {
	tc := &fakeTaskcluster{task: sampleTaskResponse(), createTaskErr: errTest}
	action := retriggerTaskAction(tc, "SRC")

	if err := action.Perform(ActionInput{}); err == nil {
		t.Fatal("expected first Perform to fail (createTaskErr set)")
	}
	firstID, firstBody := tc.createTaskID, string(tc.createTaskBody)
	if !slugidRe.MatchString(firstID) {
		t.Fatalf("first submit id %q not a slugid", firstID)
	}

	tc.createTaskErr = nil
	if err := action.Perform(ActionInput{}); err != nil {
		t.Fatalf("retry Perform: %v", err)
	}
	if tc.createTaskID != firstID {
		t.Errorf("retry minted a new id %q, want memoized %q", tc.createTaskID, firstID)
	}
	if string(tc.createTaskBody) != firstBody {
		t.Error("retry resubmitted a different body")
	}
	if tc.createTaskN != 2 {
		t.Errorf("CreateTask called %d times, want 2", tc.createTaskN)
	}

	// Next targets the created task by its memoized id.
	target, ok := action.Next()
	if !ok || target.ResourceName != "task" || target.ID != firstID {
		t.Fatalf("Next() = %+v, %v; want task/%s", target, ok, firstID)
	}
}

// hasKey reports whether any action uses key k.
func hasKey(actions []Action, k rune) bool {
	for _, a := range actions {
		if a.Key == k {
			return true
		}
	}
	return false
}

// TestTaskResourcesActionsListVsDetail verifies every task-detail-bearing
// resource offers lifecycle actions on a detail (id != "") and only its
// list-level action (create, or none) on a list (id == "").
func TestTaskResourcesActionsListVsDetail(t *testing.T) {
	tc := &fakeTaskcluster{}
	cache := NewTaskStateCache()

	taskRes := NewTaskResource(tc, cache)
	if a, ok := interface{}(taskRes).(Actionable); !ok {
		t.Fatal("TaskResource should implement Actionable")
	} else {
		if !hasKey(a.Actions("SOMETASK"), 'T') {
			t.Error("TaskResource detail should offer lifecycle actions")
		}
		if len(a.Actions("")) != 0 {
			t.Error("TaskResource with empty id should offer nothing")
		}
	}

	tasks := NewTasksResource(tc, NewTaskDefHistory(), cache)
	if !hasKey(tasks.Actions(""), 'c') {
		t.Error("tasks list should offer create-task ('c')")
	}
	if hasKey(tasks.Actions("SOMETASK"), 'c') {
		t.Error("tasks detail should not offer create-task")
	}
	if !hasKey(tasks.Actions("SOMETASK"), 'T') {
		t.Error("tasks detail should offer lifecycle actions")
	}

	group := NewTaskGroupResource(tc, NewTaskDefHistory(), cache)
	if !hasKey(group.Actions(""), 'c') {
		t.Error("taskgroup list should offer create-task ('c')")
	}
	if !hasKey(group.Actions("SOMETASK"), 'T') {
		t.Error("taskgroup task detail should offer lifecycle actions")
	}
	if hasKey(group.Actions("SOMETASK"), 'c') {
		t.Error("taskgroup task detail should not offer create-task")
	}

	for _, r := range []Actionable{
		NewPendingTasksResource(tc, cache),
		NewClaimedTasksResource(tc, cache),
		NewTaskDependenciesResource(tc, cache),
		NewTaskDependentsResource(tc, cache),
	} {
		if len(r.Actions("")) != 0 {
			t.Errorf("%s list should offer no actions", r.Name())
		}
		if !hasKey(r.Actions("SOMETASK"), 'T') {
			t.Errorf("%s task detail should offer lifecycle actions", r.Name())
		}
	}
}

// describeTask records the fetched state/priority so a later Actions(id) can
// gate correctly without its own API call.
func TestDescribeTaskRecordsStateSnapshot(t *testing.T) {
	tc := &fakeTaskcluster{
		task:       sampleTaskResponse(),
		taskStatus: &tcqueue.TaskStatusStructure{State: "running"},
	}
	cache := NewTaskStateCache()
	if _, err := describeTask(tc, cache, "SRC"); err != nil {
		t.Fatalf("describeTask: %v", err)
	}
	snap, ok := cache.lookup("SRC", time.Now())
	if !ok || snap.state != "running" || snap.priority != "lowest" {
		t.Fatalf("snapshot = %+v ok=%v, want running/lowest", snap, ok)
	}
}

func TestValidateTaskPriority(t *testing.T) {
	for _, p := range []string{"highest", "very-high", "high", "medium", "low", "very-low", "lowest", "normal"} {
		if err := validateTaskPriority(p); err != nil {
			t.Errorf("valid priority %q rejected: %v", p, err)
		}
	}
	for _, bad := range []string{"", "  ", "urgent", "Highest", "very high", "0"} {
		if err := validateTaskPriority(bad); err == nil {
			t.Errorf("invalid priority %q accepted", bad)
		}
	}
}
