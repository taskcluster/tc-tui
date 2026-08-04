package resource

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/taskcluster/slugid-go/slugid"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcqueue"
)

// sampleTaskDef is a minimal, valid-shaped definition with stale timestamps,
// used across the create-task tests.
const sampleTaskDef = `
provisionerId: proj-taskcluster
workerType: gw-ci
created: "2020-01-01T00:00:00.000Z"
deadline: "2020-01-01T01:00:00.000Z"
expires: "2020-02-01T00:00:00.000Z"
payload:
  command: [echo, hi]
  maxRunTime: 600
metadata:
  name: hello
  description: a hello task
  owner: me@example.com
  source: https://example.com
`

// freshTaskDef returns a sampleTaskDef-shaped definition anchored to now —
// used wherever a test exercises createTaskAction.Validate, which (unlike
// buildTaskDefinition's own structural checks) also enforces the queue's
// clock-drift constraints via validateTaskTiming. sampleTaskDef's fixed 2020
// dates stay fixed everywhere else: they're what the many schema/ordering/
// rebase tests below are actually about, independent of wall-clock time.
func freshTaskDef(now time.Time) string {
	return "\n" +
		"provisionerId: proj-taskcluster\n" +
		"workerType: gw-ci\n" +
		"created: \"" + now.UTC().Format(taskTimeLayout) + "\"\n" +
		"deadline: \"" + now.Add(time.Hour).UTC().Format(taskTimeLayout) + "\"\n" +
		"expires: \"" + now.AddDate(0, 0, 31).UTC().Format(taskTimeLayout) + "\"\n" +
		"payload:\n" +
		"  command: [echo, hi]\n" +
		"  maxRunTime: 600\n" +
		"metadata:\n" +
		"  name: hello\n" +
		"  description: a hello task\n" +
		"  owner: me@example.com\n" +
		"  source: https://example.com\n"
}

// build is a test helper that returns the supplied/parsed taskId and the error.
func build(t *testing.T, raw string, now time.Time, rebase bool) (string, error) {
	t.Helper()
	bt, err := buildTaskDefinition(raw, now, rebase)
	if bt == nil {
		return "", err
	}
	return bt.id, err
}

func mustBuild(t *testing.T, raw string, now time.Time, rebase bool) (string, *tcqueue.TaskDefinitionRequest) {
	t.Helper()
	bt, err := buildTaskDefinition(raw, now, rebase)
	if err != nil {
		t.Fatalf("buildTaskDefinition: %v", err)
	}
	return bt.id, bt.def
}

func TestBuildTaskDefinitionRebasesRelativeTimestamps(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	_, def := mustBuild(t, sampleTaskDef, now, true)

	if got := time.Time(def.Created).UTC(); !got.Equal(now) {
		t.Errorf("created = %s, want rebased to now %s", got, now)
	}
	// deadline was created+1h; expires was created+31d. Both offsets must be
	// preserved from the new created (now).
	if got, want := time.Time(def.Deadline).UTC(), now.Add(time.Hour); !got.Equal(want) {
		t.Errorf("deadline = %s, want %s (created+1h)", got, want)
	}
	if got, want := time.Time(def.Expires).UTC(), now.AddDate(0, 0, 31); !got.Equal(want) {
		t.Errorf("expires = %s, want %s (created+31d)", got, want)
	}
}

func TestBuildTaskDefinitionPreservesTimestampsWhenNotRebasing(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	_, def := mustBuild(t, sampleTaskDef, now, false)

	created, _ := time.Parse(taskTimeLayout, "2020-01-01T00:00:00.000Z")
	deadline, _ := time.Parse(taskTimeLayout, "2020-01-01T01:00:00.000Z")
	if got := time.Time(def.Created).UTC(); !got.Equal(created) {
		t.Errorf("created = %s, want preserved %s", got, created)
	}
	if got := time.Time(def.Deadline).UTC(); !got.Equal(deadline) {
		t.Errorf("deadline = %s, want preserved %s", got, deadline)
	}
}

func TestBuildTaskDefinitionSetsCreatedWhenAbsent(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	deadline := "2030-01-01T00:00:00.000Z"
	raw := `
taskQueueId: proj/gw
deadline: "` + deadline + `"
payload: {command: [echo]}
metadata: {name: nm, description: d, owner: ow, source: https://example.com}
`
	_, def := mustBuild(t, raw, now, true)
	if got := time.Time(def.Created).UTC(); !got.Equal(now) {
		t.Errorf("created = %s, want now %s", got, now)
	}
	// With no original created there is no delta, so an absolute future
	// deadline the user supplied is left untouched.
	want, _ := time.Parse(taskTimeLayout, deadline)
	if got := time.Time(def.Deadline).UTC(); !got.Equal(want) {
		t.Errorf("deadline = %s, want unchanged %s", got, want)
	}
}

func TestBuildTaskDefinitionRejectsDeadlineBeforeCreated(t *testing.T) {
	// The queue rejects created > deadline; catch it up front. rebase preserves
	// the created->deadline offset, so a negative one survives the shift and must
	// still be rejected — this is the concrete bug the check guards against.
	raw := `
taskQueueId: proj/gw
created: "2020-01-01T02:00:00.000Z"
deadline: "2020-01-01T01:00:00.000Z"
payload: {command: [echo]}
metadata: {name: nm, description: d, owner: ow, source: https://example.com}
`
	for _, rebase := range []bool{false, true} {
		if _, err := build(t, raw, time.Now(), rebase); err == nil || !strings.Contains(err.Error(), "deadline") {
			t.Fatalf("rebase=%v: want a deadline-before-created error, got %v", rebase, err)
		}
	}
}

func TestBuildTaskDefinitionRejectsExpiresBeforeDeadline(t *testing.T) {
	// The queue rejects deadline > expires. The offset is preserved through a
	// rebase, so an expires that precedes deadline stays invalid after shifting.
	raw := `
taskQueueId: proj/gw
created: "2020-01-01T00:00:00.000Z"
deadline: "2020-01-01T02:00:00.000Z"
expires: "2020-01-01T01:00:00.000Z"
payload: {command: [echo]}
metadata: {name: nm, description: d, owner: ow, source: https://example.com}
`
	for _, rebase := range []bool{false, true} {
		if _, err := build(t, raw, time.Now(), rebase); err == nil || !strings.Contains(err.Error(), "expires") {
			t.Fatalf("rebase=%v: want an expires-before-deadline error, got %v", rebase, err)
		}
	}
}

func TestBuildTaskDefinitionAcceptsEqualCreatedDeadline(t *testing.T) {
	// The queue's checks are non-strict (created > deadline / deadline > expires),
	// so equal boundaries are valid and must not be rejected.
	raw := `
taskQueueId: proj/gw
created: "2020-01-01T01:00:00.000Z"
deadline: "2020-01-01T01:00:00.000Z"
expires: "2020-01-01T01:00:00.000Z"
payload: {command: [echo]}
metadata: {name: nm, description: d, owner: ow, source: https://example.com}
`
	if _, err := build(t, raw, time.Now(), false); err != nil {
		t.Fatalf("equal created/deadline/expires should be accepted, got %v", err)
	}
}

func TestBuildTaskDefinitionRejectsNullExpiresUnderRebase(t *testing.T) {
	// expires: null is the sneaky case: rebase's timestamp reader treats a null
	// as an absent value and leaves it unshifted, so without an explicit null
	// check the null survives into the (verbatim-submitted) body and the queue —
	// not the dialog — rejects it. It must be caught up front, before rebasing.
	raw := `
taskQueueId: proj/gw
created: "2020-01-01T00:00:00.000Z"
deadline: "2020-01-01T01:00:00.000Z"
expires: null
payload: {command: [echo]}
metadata: {name: nm, description: d, owner: ow, source: https://example.com}
`
	if _, err := build(t, raw, time.Now(), true); err == nil || !strings.Contains(err.Error(), "expires may not be null") {
		t.Fatalf("want an expires-null error, got %v", err)
	}
}

func TestBuildTaskDefinitionAllowsNullInsidePayloadAndExtra(t *testing.T) {
	// payload and extra carry arbitrary worker-defined content that may
	// legitimately contain null, so the null check must not descend into them —
	// only reject the field itself being null.
	m := validDefMap()
	m["payload"] = map[string]interface{}{"command": []string{"echo"}, "env": map[string]interface{}{"OPT": nil}}
	m["extra"] = map[string]interface{}{"notes": nil}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err != nil {
		t.Fatalf("null inside payload/extra should be accepted, got %v", err)
	}
}

func TestBuildTaskDefinitionRejectsUnparseableTimestamp(t *testing.T) {
	raw := `
taskQueueId: proj/gw
created: "not-a-time"
deadline: "2030-01-01T00:00:00.000Z"
payload: {command: [echo]}
metadata: {name: nm}
`
	if _, err := build(t, raw, time.Now(), true); err == nil || !strings.Contains(err.Error(), "created") {
		t.Fatalf("want a created-timestamp error, got %v", err)
	}
}

func TestBuildTaskDefinitionAcceptsJSON(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	raw := `{"taskQueueId":"proj/gw","deadline":"2030-01-01T00:00:00.000Z",` +
		`"payload":{"command":["echo"]},"metadata":{"name":"nm","description":"d","owner":"ow","source":"https://example.com"}}`
	if _, err := build(t, raw, now, true); err != nil {
		t.Fatalf("expected JSON to parse, got %v", err)
	}
}

func TestBuildTaskDefinitionAcceptsSuppliedSlugid(t *testing.T) {
	id := slugid.Nice()
	raw := "taskId: " + id + "\n" + sampleTaskDef
	gotID, _ := mustBuild(t, raw, time.Now(), true)
	if gotID != id {
		t.Fatalf("returned taskId = %q, want the supplied %q", gotID, id)
	}
}

func TestBuildTaskDefinitionRejectsInvalidSlugid(t *testing.T) {
	raw := "taskId: not a slugid!\n" + sampleTaskDef
	if _, err := build(t, raw, time.Now(), true); err == nil || !strings.Contains(err.Error(), "slugid") {
		t.Fatalf("want an invalid-slugid error, got %v", err)
	}
}

func TestBuildTaskDefinitionGeneratesTaskIDWhenAbsent(t *testing.T) {
	// buildTaskDefinition itself returns "" when no taskId is supplied; the
	// action is responsible for generating one.
	gotID, _ := mustBuild(t, sampleTaskDef, time.Now(), true)
	if gotID != "" {
		t.Fatalf("expected an empty taskId when none supplied, got %q", gotID)
	}
}

func TestBuildTaskDefinitionRejectsUnknownField(t *testing.T) {
	// A misspelled top-level field must be rejected rather than silently
	// dropped into a default (or, for a case variant, submitted verbatim for
	// the queue to reject — see the "case-variant key" schema case).
	raw := `
taskQueueId: proj/gw
deadline: "2030-01-01T00:00:00.000Z"
provisionerr: typo-should-fail
payload: {command: [echo]}
metadata: {name: nm}
`
	if _, err := build(t, raw, time.Now(), true); err == nil || !strings.Contains(err.Error(), "provisionerr") {
		t.Fatalf("want an unknown-field error mentioning provisionerr, got %v", err)
	}
}

func TestBuildTaskDefinitionPreservesLargeIntegers(t *testing.T) {
	// A 64-bit integer beyond float64's exact range must survive intact
	// (would round to 9007199254740992 if routed through float64).
	const big = "9007199254740993"
	raw := `
taskQueueId: proj/gw
deadline: "2030-01-01T00:00:00.000Z"
payload:
  bigNumber: ` + big + `
  command: [echo]
metadata: {name: nm, description: d, owner: ow, source: https://example.com}
`
	_, def := mustBuild(t, raw, time.Now(), true)
	if got := string(def.Payload); !strings.Contains(got, big) {
		t.Fatalf("payload lost integer precision: %s", got)
	}
}

// validDefMap returns a schema-complete definition (taskQueueId form) as a map
// so individual tests can mutate exactly one field before submitting it as
// JSON. Values are JSON-quoted, so they are not subject to YAML scalar
// coercion.
func validDefMap() map[string]interface{} {
	return map[string]interface{}{
		"taskQueueId": "proj/gw",
		"created":     "2020-01-01T00:00:00.000Z",
		"deadline":    "2020-01-01T01:00:00.000Z",
		"payload":     map[string]interface{}{"command": []string{"echo"}},
		"metadata": map[string]interface{}{
			"name": "n", "description": "d", "owner": "o", "source": "https://example.com",
		},
	}
}

func TestBuildTaskDefinitionAcceptsCompleteDefinition(t *testing.T) {
	raw, err := json.Marshal(validDefMap())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err != nil {
		t.Fatalf("a schema-complete definition should validate, got %v", err)
	}
}

func TestBuildTaskDefinitionAcceptsTransitionalTriple(t *testing.T) {
	// A definition fetched from the queue carries taskQueueId AND matching
	// provisionerId/workerType; the create flow must accept that, since users
	// copy fetched definitions to resubmit them.
	m := validDefMap()
	m["taskQueueId"] = "proj/gw"
	m["provisionerId"] = "proj"
	m["workerType"] = "gw"
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err != nil {
		t.Fatalf("taskQueueId + matching provisionerId/workerType should be accepted, got %v", err)
	}
}

func TestBuildTaskDefinitionPreservesExplicitZeroRetries(t *testing.T) {
	// retries: 0 is a valid choice (never retry) and must reach the queue,
	// not be dropped by the typed struct's omitempty so the queue applies its
	// default of 5. The submit body is the raw JSON, so it keeps the zero.
	m := validDefMap()
	m["retries"] = 0
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	bt, err := buildTaskDefinition(string(raw), time.Now(), false)
	if err != nil {
		t.Fatalf("buildTaskDefinition: %v", err)
	}
	if !strings.Contains(string(bt.body), `"retries":0`) {
		t.Fatalf("submit body dropped explicit retries:0: %s", bt.body)
	}
}

func TestBuildTaskDefinitionEnforcesSchema(t *testing.T) {
	meta := func(m map[string]interface{}) map[string]interface{} {
		return m["metadata"].(map[string]interface{})
	}
	cases := map[string]struct {
		rebase bool
		mutate func(m map[string]interface{})
		want   string
	}{
		"not an object":   {true, nil, "object"}, // handled specially below
		"missing queue":   {true, func(m map[string]interface{}) { delete(m, "taskQueueId") }, "taskQueueId"},
		"malformed queue": {true, func(m map[string]interface{}) { m["taskQueueId"] = "proj/UPPER" }, "taskQueueId"},
		"inconsistent triple": {true, func(m map[string]interface{}) {
			m["provisionerId"] = "other"
			m["workerType"] = "gw"
		}, "must equal"},
		"lone provisioner": {true, func(m map[string]interface{}) {
			delete(m, "taskQueueId")
			m["provisionerId"] = "proj"
		}, "taskQueueId"},
		"malformed worker": {true, func(m map[string]interface{}) {
			delete(m, "taskQueueId")
			m["provisionerId"] = "proj"
			m["workerType"] = "Bad"
		}, "workerType"},
		"missing created":     {false, func(m map[string]interface{}) { delete(m, "created") }, "created"},
		"missing deadline":    {false, func(m map[string]interface{}) { delete(m, "deadline") }, "deadline"},
		"missing name":        {true, func(m map[string]interface{}) { delete(meta(m), "name") }, "metadata.name"},
		"missing description": {true, func(m map[string]interface{}) { delete(meta(m), "description") }, "metadata.description"},
		"missing owner":       {true, func(m map[string]interface{}) { delete(meta(m), "owner") }, "metadata.owner"},
		"missing source":      {true, func(m map[string]interface{}) { delete(meta(m), "source") }, "metadata.source"},
		"missing payload":     {true, func(m map[string]interface{}) { delete(m, "payload") }, "payload"},
		"non-object payload":  {true, func(m map[string]interface{}) { m["payload"] = "not-an-object" }, "payload must be an object"},
		"invalid priority":    {true, func(m map[string]interface{}) { m["priority"] = "turbo" }, "priority"},
		"retries too high":    {true, func(m map[string]interface{}) { m["retries"] = 50 }, "retries"},
		"retries negative":    {true, func(m map[string]interface{}) { m["retries"] = -1 }, "retries"},
		"malformed group":     {true, func(m map[string]interface{}) { m["taskGroupId"] = "not-a-slugid!" }, "taskGroupId"},
		"malformed dep":       {true, func(m map[string]interface{}) { m["dependencies"] = []string{"bad!"} }, "dependency"},
		"duplicate dep": {true, func(m map[string]interface{}) {
			dep := slugid.Nice()
			m["dependencies"] = []string{dep, dep}
		}, "duplicate dependency"},
		"case-variant key": {true, func(m map[string]interface{}) { m["provisionerID"] = "proj" }, "provisionerID"},
		"unknown metadata key": {true, func(m map[string]interface{}) {
			meta(m)["Source"] = "https://example.com" // case variant of source
		}, "metadata field"},
		"invalid requires": {true, func(m map[string]interface{}) { m["requires"] = "bogus" }, "requires"},
		// Explicit empty/null values collapse to the same Go zero value as an
		// omitted field, but the queue rejects them (they are not enum members /
		// violate the field's pattern), so presence-gated checks must catch them.
		"explicit empty priority":    {true, func(m map[string]interface{}) { m["priority"] = "" }, "priority"},
		"null priority":              {true, func(m map[string]interface{}) { m["priority"] = nil }, "priority"},
		"explicit empty requires":    {true, func(m map[string]interface{}) { m["requires"] = "" }, "requires"},
		"explicit empty schedulerId": {true, func(m map[string]interface{}) { m["schedulerId"] = "" }, "schedulerId"},
		"null schedulerId":           {true, func(m map[string]interface{}) { m["schedulerId"] = nil }, "schedulerId"},
		"explicit empty taskGroupId": {true, func(m map[string]interface{}) { m["taskGroupId"] = "" }, "taskGroupId"},
		// Explicit null for an array/object field is a type violation the queue
		// rejects; it decodes to the same nil as omission, so it is caught on the
		// raw definition.
		"null dependencies":               {true, func(m map[string]interface{}) { m["dependencies"] = nil }, "dependencies may not be null"},
		"null routes":                     {true, func(m map[string]interface{}) { m["routes"] = nil }, "routes may not be null"},
		"null scopes":                     {true, func(m map[string]interface{}) { m["scopes"] = nil }, "scopes may not be null"},
		"null tags":                       {true, func(m map[string]interface{}) { m["tags"] = nil }, "tags may not be null"},
		"null extra":                      {true, func(m map[string]interface{}) { m["extra"] = nil }, "extra may not be null"},
		"null retries":                    {true, func(m map[string]interface{}) { m["retries"] = nil }, "retries may not be null"},
		"null provisionerId":              {true, func(m map[string]interface{}) { m["provisionerId"] = nil }, "provisionerId may not be null"},
		"null taskQueueId":                {true, func(m map[string]interface{}) { m["taskQueueId"] = nil }, "taskQueueId may not be null"},
		"null tag value":                  {true, func(m map[string]interface{}) { m["tags"] = map[string]interface{}{"x": nil} }, "tags.x may not be null"},
		"null scope item":                 {true, func(m map[string]interface{}) { m["scopes"] = []interface{}{nil} }, "scopes[0] may not be null"},
		"null metadata name":              {true, func(m map[string]interface{}) { meta(m)["name"] = nil }, "metadata.name must be a string"},
		"null metadata owner":             {true, func(m map[string]interface{}) { meta(m)["owner"] = nil }, "metadata.owner must be a string"},
		"non-string metadata description": {true, func(m map[string]interface{}) { meta(m)["description"] = 5 }, "metadata.description must be a string"},
		"non-object extra":                {true, func(m map[string]interface{}) { m["extra"] = "nope" }, "extra must be an object"},
		"empty projectId":                 {true, func(m map[string]interface{}) { m["projectId"] = "" }, "projectId"},
		"malformed projectId":             {true, func(m map[string]interface{}) { m["projectId"] = "bad project!" }, "projectId"},
		"malformed source":                {true, func(m map[string]interface{}) { meta(m)["source"] = "not-a-url" }, "metadata.source"},
		"oversized name":                  {true, func(m map[string]interface{}) { meta(m)["name"] = strings.Repeat("x", 256) }, "metadata.name"},
		"empty route":                     {true, func(m map[string]interface{}) { m["routes"] = []string{""} }, "route"},
		"non-ascii scope":                 {true, func(m map[string]interface{}) { m["scopes"] = []string{"scope:\x01"} }, "scope"},
		"trailing double star":            {true, func(m map[string]interface{}) { m["scopes"] = []string{"foo:**"} }, "may not end"},
		"malformed source url":            {true, func(m map[string]interface{}) { meta(m)["source"] = "https://[" }, "not a valid URL"},
		"too many routes": {true, func(m map[string]interface{}) {
			routes := make([]string, 65)
			for i := range routes {
				routes[i] = fmt.Sprintf("route.%d", i)
			}
			m["routes"] = routes
		}, "routes exceeds"},
		"duplicate route": {true, func(m map[string]interface{}) { m["routes"] = []string{"a.b", "a.b"} }, "duplicate route"},
		"too many deps": {true, func(m map[string]interface{}) {
			deps := make([]string, 10001)
			id := slugid.Nice()
			for i := range deps {
				deps[i] = id
			}
			m["dependencies"] = deps
		}, "dependencies exceeds"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var raw string
			if tc.mutate == nil { // the non-object document case
				raw = "- a\n- b\n"
			} else {
				m := validDefMap()
				tc.mutate(m)
				encoded, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				raw = string(encoded)
			}
			if _, err := build(t, raw, time.Now(), tc.rebase); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestBuildTaskDefinitionAcceptsValidOptionalFields(t *testing.T) {
	// Guard against false positives: a definition exercising every optional
	// field with valid values must be accepted.
	m := validDefMap()
	m["requires"] = "all-resolved"
	m["projectId"] = "my/project-1"
	m["schedulerId"] = "tc-tui"
	m["extra"] = map[string]interface{}{"dashboard": map[string]interface{}{"n": 1}}
	m["routes"] = []string{"index.project.example.latest"}
	m["scopes"] = []string{"queue:create-task:highest:proj/gw", "assume:repo:x:*"} // single trailing * is allowed
	m["dependencies"] = []string{slugid.Nice(), slugid.Nice()}
	m["tags"] = map[string]interface{}{"purpose": "test"}
	m["priority"] = "high"
	m["retries"] = 3
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err != nil {
		t.Fatalf("valid optional fields should be accepted, got %v", err)
	}
}

func TestBuildTaskDefinitionAcceptsEmptyMetadataStrings(t *testing.T) {
	// name/description/owner are required to be present but have no minLength,
	// so empty values are valid — only their absence is an error. (source has a
	// URL pattern, so it can't be empty.)
	m := validDefMap()
	meta := m["metadata"].(map[string]interface{})
	meta["name"] = ""
	meta["description"] = ""
	meta["owner"] = ""
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err != nil {
		t.Fatalf("empty metadata strings should be accepted, got %v", err)
	}
}

func TestBuildTaskDefinitionCountsMaxLengthInCodePoints(t *testing.T) {
	// maxLength is counted in Unicode code points, not bytes: a 255-character
	// name that is 510 UTF-8 bytes must be accepted, and 256 characters
	// rejected.
	m := validDefMap()
	meta := m["metadata"].(map[string]interface{})

	meta["name"] = strings.Repeat("é", 255) // 255 runes, 510 bytes
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err != nil {
		t.Fatalf("a 255-code-point name should be accepted, got %v", err)
	}

	meta["name"] = strings.Repeat("é", 256)
	raw, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, string(raw), time.Now(), false); err == nil || !strings.Contains(err.Error(), "metadata.name") {
		t.Fatalf("a 256-code-point name should be rejected, got %v", err)
	}
}

func TestBuildTaskDefinitionRejectsNullDocuments(t *testing.T) {
	// Valid YAML that decodes to a nil map must be reported as an object-type
	// error, not crash on a nil-map assignment during rebasing.
	for _, raw := range []string{"null", "~", "# only a comment\n"} {
		if _, err := build(t, raw, time.Now(), true); err == nil || !strings.Contains(err.Error(), "object") {
			t.Fatalf("raw %q: want an object-type error, got %v", raw, err)
		}
	}
}

func TestUpdateTaskTimestampsPreservesTaskIdCommentsAndShiftsOffsets(t *testing.T) {
	id := slugid.Nice()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	// Build from the known-valid sampleTaskDef (complete: provisionerId,
	// workerType, payload, metadata) so buildTaskDefinition passes; prepend a
	// taskId and a standalone comment, and add an inline comment on created.
	raw := "taskId: " + id + "\n# keep me\n" +
		strings.Replace(sampleTaskDef,
			`created: "2020-01-01T00:00:00.000Z"`,
			`created: "2020-01-01T00:00:00.000Z" # birth`, 1)

	out, err := updateTaskTimestamps(raw, now)
	if err != nil {
		t.Fatalf("updateTaskTimestamps: %v", err)
	}
	if !strings.Contains(out, id) {
		t.Fatal("dropped the supplied taskId")
	}
	if !strings.Contains(out, "keep me") || !strings.Contains(out, "birth") {
		t.Fatalf("dropped a comment:\n%s", out)
	}
	// created -> now; deadline keeps its +1h offset (from sampleTaskDef).
	bt, err := buildTaskDefinition(out, now, false)
	if err != nil {
		t.Fatalf("rebased def should validate: %v", err)
	}
	if !time.Time(bt.def.Created).Equal(now) {
		t.Fatalf("created = %s, want now", time.Time(bt.def.Created))
	}
	if !time.Time(bt.def.Deadline).Equal(now.Add(time.Hour)) {
		t.Fatalf("deadline offset not preserved: %s", time.Time(bt.def.Deadline))
	}
}

func TestUpdateTaskTimestampsSetsCreatedWhenAbsent(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	raw := "deadline: \"2030-01-01T00:00:00.000Z\"\npayload: {}\n"
	out, err := updateTaskTimestamps(raw, now)
	if err != nil || !strings.Contains(out, "created:") {
		t.Fatalf("want created added, got %q err=%v", out, err)
	}
}

func TestUpdateTaskTimestampsPreservesTrailingDocumentComment(t *testing.T) {
	// A comment with nothing after it (the last line of the file) is
	// attached by the parser as the DOCUMENT's FootComment, not any field's —
	// a distinct case from the taskId/inline comments the other test covers.
	raw := sampleTaskDef + "\n# trailing comment\n"
	out, err := updateTaskTimestamps(raw, time.Now())
	if err != nil {
		t.Fatalf("updateTaskTimestamps: %v", err)
	}
	if !strings.Contains(out, "trailing comment") {
		t.Fatalf("dropped the trailing document comment:\n%s", out)
	}
}

func TestUpdateTaskTimestampsErrorsOnPresentInvalidTimestamp(t *testing.T) {
	// A present-but-unparseable timestamp is a hard error, not silently
	// treated as absent (which would leave deadline/expires unshifted). Matches
	// rebaseTaskTimestamps.
	raw := strings.Replace(sampleTaskDef,
		`created: "2020-01-01T00:00:00.000Z"`, `created: nonsense`, 1)
	if _, err := updateTaskTimestamps(raw, time.Now()); err == nil || !strings.Contains(err.Error(), "created") {
		t.Fatalf("want a created-timestamp error, got %v", err)
	}
}

func TestUpdateTaskTimestampsErrorsOnNonScalarTimestamp(t *testing.T) {
	// A mapping/sequence node's own Value field is always "", the same as a
	// genuinely absent field — without an explicit Kind check this silently
	// overwrites the malformed node instead of erroring as promised.
	raw := strings.Replace(sampleTaskDef,
		`created: "2020-01-01T00:00:00.000Z"`, "created:\n  bad: value", 1)
	if _, err := updateTaskTimestamps(raw, time.Now()); err == nil || !strings.Contains(err.Error(), "created") {
		t.Fatalf("want a created-timestamp error for a non-scalar node, got %v", err)
	}
}

func TestTasksAndTaskGroupShareHistory(t *testing.T) {
	h := &taskDefHistory{}
	tasks := NewTasksResource(&fakeTaskcluster{}, h, nil)
	group := NewTaskGroupResource(&fakeTaskcluster{}, h, nil)
	h.add("shared def")
	if a := tasks.Actions(""); a[0].InitialText != "shared def" {
		t.Fatalf("tasks action did not seed from shared history: %q", a[0].InitialText)
	}
	if a := group.Actions(""); a[0].InitialText != "shared def" {
		t.Fatalf("taskgroup action did not seed from shared history: %q", a[0].InitialText)
	}
}

func TestCreateTaskActionRetryIsIdempotent(t *testing.T) {
	tc := &fakeTaskcluster{createTaskErr: fmt.Errorf("transport blip")}
	action := createTaskAction(tc, &taskDefHistory{})
	in, err := ParseActionInput(action.Input, sampleTaskDef, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// First attempt: Queue may have accepted it, but the client saw an error.
	if err := action.Perform(in); err == nil {
		t.Fatal("expected the seeded transport error on the first attempt")
	}
	firstID := tc.createTaskID
	if firstID == "" || tc.createTaskBody == nil {
		t.Fatal("CreateTask should have been called on the first attempt")
	}
	firstBody := string(tc.createTaskBody)

	// A retry with the same input must reuse the same id and re-submit the
	// body byte-for-byte, not mint a new task id or re-rebase timestamps to a
	// later "now".
	tc.createTaskErr = nil
	if err := action.Perform(in); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if tc.createTaskID != firstID {
		t.Fatalf("retry used a different taskId: %q vs %q", tc.createTaskID, firstID)
	}
	if got := string(tc.createTaskBody); got != firstBody {
		t.Fatalf("retry re-built the body:\n first: %s\n retry: %s", firstBody, got)
	}
}

func TestCreateTaskActionRebuildsWhenInputChanges(t *testing.T) {
	tc := &fakeTaskcluster{}
	action := createTaskAction(tc, &taskDefHistory{})

	in1, _ := ParseActionInput(action.Input, sampleTaskDef, true)
	if err := action.Perform(in1); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	firstID := tc.createTaskID

	// Editing the definition invalidates the memo — it is a genuinely new task.
	edited := strings.Replace(sampleTaskDef, "name: hello", "name: edited", 1)
	in2, _ := ParseActionInput(action.Input, edited, true)
	if err := action.Perform(in2); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if tc.createTaskID == firstID {
		t.Fatal("editing the definition should produce a new taskId")
	}
}

func TestBuildTaskDefinitionAcceptsProvisionerWorkerType(t *testing.T) {
	// The sample uses provisionerId + workerType rather than taskQueueId.
	if _, err := build(t, sampleTaskDef, time.Now(), true); err != nil {
		t.Fatalf("provisionerId + workerType should satisfy validation, got %v", err)
	}
}

func TestBuildTaskDefinitionDoesNotMutateAcrossCalls(t *testing.T) {
	now1 := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	now2 := now1.Add(3 * time.Hour)

	_, d1 := mustBuild(t, sampleTaskDef, now1, true)
	_, d2 := mustBuild(t, sampleTaskDef, now2, true)
	// Each call re-parses raw, so the second rebase must be relative to now2,
	// unaffected by the first — proving no shared state leaks between the
	// Validate and Perform invocations that both call this.
	if got := time.Time(d1.Created).UTC(); !got.Equal(now1) {
		t.Errorf("first created = %s, want %s", got, now1)
	}
	if got := time.Time(d2.Created).UTC(); !got.Equal(now2) {
		t.Errorf("second created = %s, want %s", got, now2)
	}
}

func TestTaskDefHistoryRetainsRecentAndDedupes(t *testing.T) {
	h := &taskDefHistory{}
	if _, ok := h.latest(); ok {
		t.Fatal("empty history should report no latest")
	}

	h.add("a")
	h.add("b")
	if got, _ := h.latest(); got != "b" {
		t.Fatalf("latest = %q, want most-recent b", got)
	}

	// Re-adding an earlier entry moves it to the front without duplicating.
	h.add("a")
	if got, _ := h.latest(); got != "a" {
		t.Fatalf("latest = %q, want a after re-add", got)
	}
	if len(h.recent) != 2 {
		t.Fatalf("history len = %d, want 2 (deduped)", len(h.recent))
	}

	// Blank definitions are ignored.
	h.add("   \n  ")
	if len(h.recent) != 2 {
		t.Fatalf("blank add should be ignored, len = %d", len(h.recent))
	}
}

func TestTaskDefHistoryAllReturnsNewestFirstCopy(t *testing.T) {
	h := &taskDefHistory{}
	h.add("a")
	h.add("b")
	h.add("c")
	got := h.all()
	want := []string{"c", "b", "a"}
	if len(got) != len(want) {
		t.Fatalf("all() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("all()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Mutating the returned slice must not corrupt the store.
	got[0] = "mutated"
	if latest, _ := h.latest(); latest != "c" {
		t.Fatalf("all() leaked its backing store: latest=%q", latest)
	}
}

func TestTaskDefHistoryCaps(t *testing.T) {
	h := &taskDefHistory{}
	for i := 0; i < maxRetainedTaskDefs+5; i++ {
		h.add(strings.Repeat("x", i+1)) // each distinct
	}
	if len(h.recent) != maxRetainedTaskDefs {
		t.Fatalf("history len = %d, want cap %d", len(h.recent), maxRetainedTaskDefs)
	}
}

func TestCreateTaskActionSubmitsAndNavigates(t *testing.T) {
	tc := &fakeTaskcluster{
		createTaskResp: nil, // a nil response with nil error is success
	}
	history := &taskDefHistory{}
	action := createTaskAction(tc, history)

	if action.Key != createTaskKey {
		t.Fatalf("action key = %q, want %q", action.Key, createTaskKey)
	}

	// Validation passes for a good, timely definition (validateTaskTiming
	// requires created within 15min of now — sampleTaskDef's fixed 2020 dates
	// would fail it; see freshTaskDef).
	raw := freshTaskDef(time.Now())
	in, err := ParseActionInput(action.Input, raw, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := action.Validate(in); err != nil {
		t.Fatalf("Validate rejected a valid definition: %v", err)
	}

	// Before Perform, there is nothing to navigate to.
	if _, ok := action.Next(); ok {
		t.Fatal("Next should report false before a successful Perform")
	}

	if err := action.Perform(in); err != nil {
		t.Fatalf("Perform: %v", err)
	}

	if tc.createTaskID == "" {
		t.Fatal("CreateTask was not called with a generated taskId")
	}
	if !slugidRe.MatchString(tc.createTaskID) {
		t.Fatalf("generated taskId %q is not a slugid", tc.createTaskID)
	}
	if tc.createTaskBody == nil || !strings.Contains(string(tc.createTaskBody), `"name":"hello"`) {
		t.Fatalf("CreateTask got unexpected definition body: %s", tc.createTaskBody)
	}

	target, ok := action.Next()
	if !ok {
		t.Fatal("Next should navigate after a successful Perform")
	}
	if target.ResourceName != "task" || target.ID != tc.createTaskID || target.Kind != NavDetail {
		t.Fatalf("Next target = %+v, want task detail for %q", target, tc.createTaskID)
	}

	// The submitted definition is retained for reuse.
	if got, ok := history.latest(); !ok || got != raw {
		t.Fatalf("submitted definition was not retained, latest=%q ok=%v", got, ok)
	}
}

func TestCreateTaskActionValidateRejectsStaleTimestampsUntilRebased(t *testing.T) {
	// The starter template (and any reused stale definition) must not show as
	// "valid" until its timestamps are actually current — the real queue
	// enforces a 15min created drift and rejects a past deadline. Confirms the
	// exact review scenario: sampleTaskDef's fixed 2020 dates fail Validate,
	// and applying the "update timestamps" transform fixes it.
	action := createTaskAction(&fakeTaskcluster{}, &taskDefHistory{})
	in, err := ParseActionInput(action.Input, sampleTaskDef, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := action.Validate(in); err == nil {
		t.Fatal("Validate should reject a definition with a 2020 created/deadline")
	}

	rebased, err := updateTaskTimestamps(sampleTaskDef, time.Now())
	if err != nil {
		t.Fatalf("updateTaskTimestamps: %v", err)
	}
	in2, err := ParseActionInput(action.Input, rebased, true)
	if err != nil {
		t.Fatalf("parse rebased: %v", err)
	}
	if err := action.Validate(in2); err != nil {
		t.Fatalf("Validate rejected a rebased (now-current) definition: %v", err)
	}
}

func TestCreateTaskActionInvalidatesTaskgroupToo(t *testing.T) {
	action := createTaskAction(&fakeTaskcluster{}, &taskDefHistory{})
	want := map[string]bool{"tasks": true, "taskgroup": true}
	if len(action.Invalidates) != len(want) {
		t.Fatalf("Invalidates = %v, want %v", action.Invalidates, want)
	}
	for _, name := range action.Invalidates {
		if !want[name] {
			t.Fatalf("Invalidates = %v, unexpected entry %q", action.Invalidates, name)
		}
	}
}

func TestCreateTaskActionUsesSuppliedTaskID(t *testing.T) {
	id := slugid.Nice()
	tc := &fakeTaskcluster{}
	action := createTaskAction(tc, &taskDefHistory{})

	in, err := ParseActionInput(action.Input, "taskId: "+id+"\n"+sampleTaskDef, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := action.Perform(in); err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if tc.createTaskID != id {
		t.Fatalf("CreateTask taskId = %q, want the supplied %q", tc.createTaskID, id)
	}
}

func TestCreateTaskActionInitialTextAndHistory(t *testing.T) {
	history := &taskDefHistory{}
	// With no history the template is offered; the editor's Edit Again covers
	// reuse, so InputHistory is never populated.
	a := createTaskAction(&fakeTaskcluster{}, history)
	if a.Input != InputExternalEditor {
		t.Fatalf("Input = %v, want InputExternalEditor", a.Input)
	}
	if a.InitialText != createTaskTemplate {
		t.Fatalf("InitialText = %q, want template", a.InitialText)
	}
	if len(a.InputHistory) != 0 {
		t.Fatalf("InputHistory = %v, want empty", a.InputHistory)
	}

	// Once a definition is retained, the newest seeds InitialText.
	history.add("older")
	history.add(sampleTaskDef)
	a = createTaskAction(&fakeTaskcluster{}, history)
	if a.InitialText != sampleTaskDef {
		t.Fatalf("InitialText = %q, want newest retained definition", a.InitialText)
	}
	if len(a.InputHistory) != 0 {
		t.Fatalf("InputHistory = %v, want empty even with retained history", a.InputHistory)
	}
}

func TestCreateTaskTemplateIsValid(t *testing.T) {
	if _, err := build(t, createTaskTemplate, time.Now(), true); err != nil {
		t.Fatalf("the starter template must be a valid definition, got %v", err)
	}
}

func TestTasksResourceExposesCreateActions(t *testing.T) {
	r := NewTasksResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)
	actions := r.Actions("")
	if len(actions) != 1 {
		t.Fatalf("Actions = %+v, want a single create-task action", actions)
	}
	if actions[0].Key != createTaskKey {
		t.Fatalf("action key = %q, want %q", actions[0].Key, createTaskKey)
	}
}

func TestTaskGroupResourceExposesCreateActions(t *testing.T) {
	// The taskgroup list (`:g <id>`, or a task's 'g' jump) is the natural,
	// directly reachable place to create a task, so it exposes the same action.
	r := NewTaskGroupResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)
	actions := r.Actions("")
	if len(actions) != 1 {
		t.Fatalf("Actions = %+v, want a single create-task action", actions)
	}
	if actions[0].Key != createTaskKey {
		t.Fatalf("action key = %q, want %q", actions[0].Key, createTaskKey)
	}
}

// TestBuildTaskDefinitionPayloadRoundTrips guards that a nested payload
// survives the raw-JSON -> TaskDefinitionRequest conversion intact.
func TestBuildTaskDefinitionPayloadRoundTrips(t *testing.T) {
	_, def := mustBuild(t, sampleTaskDef, time.Now(), true)
	var payload struct {
		Command    []string `json:"command"`
		MaxRunTime int      `json:"maxRunTime"`
	}
	if err := json.Unmarshal(def.Payload, &payload); err != nil {
		t.Fatalf("payload did not round-trip: %v", err)
	}
	if payload.MaxRunTime != 600 || len(payload.Command) != 2 {
		t.Fatalf("payload lost data: %+v", payload)
	}
}
