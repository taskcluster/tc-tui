package resource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/taskcluster/slugid-go/slugid"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcqueue"
	"gopkg.in/yaml.v3"
	yamlconv "sigs.k8s.io/yaml"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// createTaskKey triggers the create-task action from the tasks/task-group
// lists. It avoids the shell's global keys (q, r, o, s, x, n, L, :, /, ?) —
// and, on worker pools, 'c' is taken for the "claimed" nav action, but the
// tasks/taskgroup lists have no other action keys, so it's free there.
const createTaskKey = 'c'

// maxRetainedTaskDefs caps how many recently submitted task definitions the
// create-task action keeps around for reuse.
const maxRetainedTaskDefs = 10

// taskTimeLayout is the millisecond RFC3339 form Taskcluster uses for task
// timestamps; rebased created/deadline/expires values are written back in it.
const taskTimeLayout = "2006-01-02T15:04:05.000Z07:00"

// slugidRe matches a Taskcluster slugid (the taskId syntax from the queue
// schema), used to validate a taskId supplied in the definition before it is
// accepted in place of a generated one.
var slugidRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8}[Q-T][A-Za-z0-9_-][CGKOSWaeimquy26-][A-Za-z0-9_-]{10}[AQgw]$`)

// createTaskTemplate prefills the $EDITOR buffer the first time it is opened
// (before any definition has been submitted), giving the user a valid-shaped
// starting point rather than an empty file. taskId is omitted so one is
// generated; the timestamps are stale on purpose — press 'u' on the confirm
// screen to rebase created/deadline/expires to now before submitting.
const createTaskTemplate = `# Task definition (YAML or JSON), edited in $EDITOR. Omit taskId to have one
# generated, or supply a slugid to reuse. Press 'u' on the confirm screen to
# rebase created/deadline/expires to now.
provisionerId: proj-taskcluster
workerType: gw-ci-ubuntu-24-04
schedulerId: tc-tui
created: "2024-01-01T00:00:00.000Z"
deadline: "2024-01-01T01:00:00.000Z"
expires: "2024-02-01T00:00:00.000Z"
payload:
  command:
    - - /bin/bash
      - -c
      - echo hello world
  maxRunTime: 600
metadata:
  name: tc-tui task
  description: Created from tc-tui
  owner: name@example.com
  source: https://github.com/taskcluster/tc-tui
`

// taskDefHistory retains the most recently submitted task definitions so a
// user can reopen the create-task dialog and cycle back through them (Ctrl-P /
// Ctrl-N) to reuse or tweak one instead of re-pasting it. It is safe for
// concurrent use: Perform appends off the UI thread while the action's
// InitialText/InputHistory are read on it.
type taskDefHistory struct {
	mu     sync.Mutex
	recent []string
}

// add records raw as the most recent definition, moving an identical earlier
// entry to the front rather than duplicating it, and caps the list.
func (h *taskDefHistory) add(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	kept := make([]string, 0, len(h.recent)+1)
	kept = append(kept, raw)
	for _, d := range h.recent {
		if d != raw {
			kept = append(kept, d)
		}
	}
	if len(kept) > maxRetainedTaskDefs {
		kept = kept[:maxRetainedTaskDefs]
	}
	h.recent = kept
}

// all returns every retained definition, newest first, as a copy safe for the
// caller to hold.
func (h *taskDefHistory) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recent) == 0 {
		return nil
	}
	out := make([]string, len(h.recent))
	copy(out, h.recent)
	return out
}

// latest returns the most recently submitted definition, if any.
func (h *taskDefHistory) latest() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recent) == 0 {
		return "", false
	}
	return h.recent[0], true
}

// NewTaskDefHistory returns an empty shared create-task history for injection
// into the tasks, task-group, and createtask resources so a definition
// submitted from any entry point seeds the others.
func NewTaskDefHistory() *taskDefHistory { return &taskDefHistory{} }

// createTaskAction builds the single "create task" action exposed on the
// tasks/task-group lists and the global `:createtask` command. The
// definition is edited in $EDITOR (InputExternalEditor) rather than an
// in-TUI field; timestamps are submitted exactly as written unless the user
// applies the "update timestamps" BufferTransform on the confirm screen. A
// fresh Action is returned per call so the createdTaskID that Perform stores
// and Next reads is scoped to a single dialog. InitialText seeds from the
// shared history's latest submission, if any.
func createTaskAction(tc taskcluster.Taskcluster, history *taskDefHistory) Action {
	initial := createTaskTemplate
	if latest, ok := history.latest(); ok {
		initial = latest
	}

	prompt := "Edit the task definition (YAML or JSON) in $EDITOR. A taskId is generated " +
		"for you (or supply a slugid to reuse). Timestamps are submitted exactly as " +
		"written unless you apply \"update timestamps\" on the confirm screen."

	var createdTaskID string

	// A single dialog can Perform more than once — the shell keeps it open so
	// the user can retry after a transport error. Queue.createTask is
	// idempotent only for an identical (taskId, definition) pair, so the
	// resolved id and finalized definition are memoized and reused as long as
	// the input text is unchanged: a retry re-submits byte-for-byte rather
	// than minting a new id. Editing the definition invalidates the memo,
	// making it a genuinely new task.
	var (
		submittedRaw  string
		submittedID   string
		submittedBody json.RawMessage
	)

	return Action{
		Key:         createTaskKey,
		Label:       "create task",
		Prompt:      prompt,
		Input:       InputExternalEditor,
		InputLabel:  "task definition",
		InitialText: initial,
		Transforms: []BufferTransform{{
			Key:   'u',
			Label: "update timestamps",
			Apply: func(raw string) (string, error) { return updateTaskTimestamps(raw, time.Now()) },
		}},
		Validate: func(in ActionInput) error {
			bt, err := buildTaskDefinition(in.Raw, time.Now(), false)
			if err != nil {
				return err
			}
			return validateTaskTiming(bt.def, time.Now())
		},
		Perform: func(in ActionInput) error {
			if submittedBody == nil || in.Raw != submittedRaw {
				bt, err := buildTaskDefinition(in.Raw, time.Now(), false)
				if err != nil {
					return err
				}
				taskID := bt.id
				if taskID == "" {
					taskID = slugid.Nice()
				}
				submittedRaw, submittedID, submittedBody = in.Raw, taskID, bt.body
			}
			if _, err := tc.CreateTask(submittedID, submittedBody); err != nil {
				return err
			}
			createdTaskID = submittedID
			history.add(in.Raw)
			return nil
		},
		Next: func() (NavTarget, bool) {
			if createdTaskID == "" {
				return NavTarget{}, false
			}
			return NavTarget{ResourceName: "task", ID: createdTaskID, Kind: NavDetail}, true
		},
		// The new task belongs to its own group; any tasks/taskgroup list
		// already cached (e.g. the group it landed in, if launched from a
		// different view) should re-fetch to pick it up.
		Invalidates: []string{"tasks", "taskgroup"},
	}
}

// validateTaskTiming enforces the queue's clock-drift constraints (queue
// createTask's patchAndValidateTaskDef): created must be within 15 minutes of
// now (either direction), and deadline may not already be in the past. This
// is deliberately NOT part of buildTaskDefinition/validateTaskDefinition —
// those are pure structural/schema checks exercised by many tests against
// fixed historical timestamps (rebasing, ordering, preservation), independent
// of wall-clock time — but it IS what the create-task action's Validate needs
// so the confirm screen doesn't show a stale definition (e.g. the starter
// template, whose timestamps are intentionally old) as green "valid" only for
// the real queue to reject it: pressing 'u' (updateTaskTimestamps) is what
// makes it valid again.
func validateTaskTiming(def *tcqueue.TaskDefinitionRequest, now time.Time) error {
	const driftAllowance = 15 * time.Minute

	created := time.Time(def.Created)
	if created.Before(now.Add(-driftAllowance)) {
		return fmt.Errorf("created %s is too far in the past (max 15min drift) — press 'u' to update timestamps",
			created.UTC().Format(taskTimeLayout))
	}
	if created.After(now.Add(driftAllowance)) {
		return fmt.Errorf("created %s is too far in the future (max 15min drift) — press 'u' to update timestamps",
			created.UTC().Format(taskTimeLayout))
	}

	if deadline := time.Time(def.Deadline); deadline.Before(now) {
		return fmt.Errorf("deadline %s is in the past — press 'u' to update timestamps",
			deadline.UTC().Format(taskTimeLayout))
	}
	return nil
}

// builtTask is a validated, ready-to-submit task definition. body is the exact
// JSON to PUT to the queue (taskId removed, timestamps rebased if requested);
// it is submitted verbatim rather than re-marshaled from def, so fields the
// user set explicitly that the typed struct marks omitempty (e.g. retries: 0)
// survive. def is the typed view used for validation and test inspection. id
// is any taskId supplied in the definition ("" when the caller must generate).
type builtTask struct {
	id   string
	def  *tcqueue.TaskDefinitionRequest
	body json.RawMessage
}

// buildTaskDefinition parses raw (YAML or JSON), optionally rebases its
// relative timestamps to now, and validates it against the queue's create-task
// schema, returning the exact JSON to submit alongside a typed view.
//
// It keeps every field's original JSON via a map of json.RawMessage rather
// than round-tripping through interface{}, so large integers in payload/extra
// keep full precision, and it decodes into the typed request with
// DisallowUnknownFields so a misspelled top-level field is rejected instead of
// silently dropped into a default. It never mutates shared state — it
// re-parses raw each call — so Validate and Perform can both call it safely.
func buildTaskDefinition(raw string, now time.Time, rebase bool) (*builtTask, error) {
	// YAMLToJSONStrict preserves 64-bit integers (unlike a YAML->interface{}
	// decode, which would widen them to float64) and rejects duplicate keys.
	jsonBytes, err := yamlconv.YAMLToJSONStrict([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid task definition: %w", err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(jsonBytes, &top); err != nil {
		return nil, fmt.Errorf("task definition must be a YAML/JSON object")
	}
	// A YAML document that is null, "~", empty, or comment-only decodes to a
	// nil map without error; treat it as the object-type error rather than
	// letting a later nil-map assignment panic and take down the TUI.
	if top == nil {
		return nil, fmt.Errorf("task definition must be a YAML/JSON object")
	}

	// A taskId may be supplied (must be a slugid) or generated by the caller
	// when absent. It is not a field of TaskDefinitionRequest — CreateTask
	// takes it separately — so it must be removed before the strict decode.
	var taskID string
	if rawID, ok := top["taskId"]; ok {
		if err := json.Unmarshal(rawID, &taskID); err != nil {
			return nil, fmt.Errorf("taskId must be a string")
		}
		if !slugidRe.MatchString(taskID) {
			return nil, fmt.Errorf("taskId %q is not a valid slugid", taskID)
		}
		delete(top, "taskId")
	}

	// No field of the create-task schema is nullable, yet Go decodes a JSON null
	// into a type's zero value (e.g. retries: null -> 0, extra: null -> an
	// absent-looking object), so a null passes the typed checks below and — since
	// the body is submitted verbatim — remains in the request for the queue to
	// reject. Reject any null up front, consistently for every field. payload and
	// extra carry arbitrary worker-defined content that may legitimately contain
	// null, so only the field itself is checked, not its contents; metadata's
	// string fields are type-checked below.
	for k, raw := range top {
		switch k {
		case "payload", "extra":
			if isJSONNull(raw) {
				return nil, fmt.Errorf("%s may not be null", k)
			}
		case "metadata":
			// validated below (presence + string type of each property)
		default:
			if err := rejectNullValues(raw, k); err != nil {
				return nil, err
			}
		}
	}

	if rebase {
		if err := rebaseTaskTimestamps(top, now); err != nil {
			return nil, err
		}
	}

	// Reject case-variant/misspelled keys the case-insensitive struct decode
	// below would otherwise accept and then submit verbatim (see
	// allowedTaskFields). Metadata is the other object with a fixed key set;
	// payload/extra/tags allow arbitrary keys, so they are not checked here.
	if err := rejectUnknownKeys(top, allowedTaskFields, ""); err != nil {
		return nil, err
	}
	// metadata is required, must be an object with no unknown keys, and must
	// carry all four required properties as strings. Presence and type are
	// checked here (on the raw map) rather than by value: the schema requires the
	// keys to exist and be strings but sets no minLength, so an empty
	// name/description/owner is valid — while a null/number/object (all of which
	// the typed struct would collapse to an empty string) is not.
	rawMeta, hasMeta := top["metadata"]
	if !hasMeta {
		return nil, fmt.Errorf("needs metadata")
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(rawMeta, &meta); err != nil || meta == nil {
		return nil, fmt.Errorf("metadata must be an object")
	}
	if err := rejectUnknownKeys(meta, allowedMetadataFields, "metadata "); err != nil {
		return nil, err
	}
	for _, k := range []string{"name", "description", "owner", "source"} {
		raw, ok := meta[k]
		if !ok {
			return nil, fmt.Errorf("needs metadata.%s", k)
		}
		// Unmarshal into interface{} and require a string: decoding JSON null
		// into a string is a silent no-op in Go (leaves it empty, no error), so a
		// plain string decode would let name/owner: null through.
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("metadata.%s is malformed", k)
		}
		if _, isString := v.(string); !isString {
			return nil, fmt.Errorf("metadata.%s must be a string", k)
		}
	}

	encoded, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("invalid task definition: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.DisallowUnknownFields()
	var out tcqueue.TaskDefinitionRequest
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid task definition: %w", err)
	}
	if err := validateTaskDefinition(top, &out); err != nil {
		return nil, err
	}
	return &builtTask{id: taskID, def: &out, body: encoded}, nil
}

// rebaseTaskTimestamps shifts a definition's created/deadline/expires to now.
// created is always set to now (a task the queue accepts must have one). When
// the definition already carried a created value, deadline and expires are
// shifted by the same delta so their offsets from created are preserved —
// this is what makes an old, reused definition (whose deadline has long since
// passed) submittable again. When there was no original created, only created
// is added and any absolute deadline/expires the user provided are left as-is.
// A present-but-unparseable timestamp is a hard error rather than silently
// ignored.
func rebaseTaskTimestamps(top map[string]json.RawMessage, now time.Time) error {
	created, hadCreated, err := readTaskTime(top, "created")
	if err != nil {
		return err
	}
	setTaskTime(top, "created", now)
	if !hadCreated {
		return nil
	}

	delta := now.Sub(created)
	for _, key := range []string{"deadline", "expires"} {
		t, ok, err := readTaskTime(top, key)
		if err != nil {
			return err
		}
		if ok {
			setTaskTime(top, key, t.Add(delta))
		}
	}
	return nil
}

// readTaskTime reads an RFC3339 timestamp field from the raw-JSON map,
// tolerating both millisecond and plain RFC3339. A missing or empty value
// reports ok=false; a present value that is not a parseable timestamp string
// is an error.
func readTaskTime(top map[string]json.RawMessage, key string) (time.Time, bool, error) {
	raw, ok := top[key]
	if !ok {
		return time.Time{}, false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return time.Time{}, false, fmt.Errorf("%s must be an RFC3339 timestamp string", key)
	}
	if s == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range []string{taskTimeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("%s is not a valid RFC3339 timestamp: %q", key, s)
}

// setTaskTime writes t back into the raw-JSON map in Taskcluster's UTC
// millisecond form.
func setTaskTime(top map[string]json.RawMessage, key string, t time.Time) {
	// Marshalling a string can't fail.
	encoded, _ := json.Marshal(t.UTC().Format(taskTimeLayout))
	top[key] = encoded
}

// updateTaskTimestamps rebases created/deadline/expires to now. It preserves
// comments and key order and leaves every other field untouched (including a
// supplied taskId) — but it is NOT fully format-preserving: the YAML encoder
// normalizes styling, and JSON input (being valid YAML) is re-emitted as YAML,
// data intact. created is set to now; if the source had a created value,
// deadline and expires are shifted by the same delta so their offsets are
// preserved; otherwise only created is set. A present-but-unparseable
// created/deadline/expires is a hard error (matching rebaseTaskTimestamps),
// not silently ignored.
func updateTaskTimestamps(raw string, now time.Time) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return "", fmt.Errorf("invalid task definition: %w", err)
	}
	root := &doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return "", fmt.Errorf("task definition must be a YAML/JSON object")
		}
		root = doc.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return "", fmt.Errorf("task definition must be a YAML/JSON object")
	}

	valueNode := func(key string) *yaml.Node {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == key {
				return root.Content[i+1]
			}
		}
		return nil
	}
	// setStr mutates an existing value node in place (retaining its Head/Line/
	// Foot comments) rather than replacing it, or appends the key when absent.
	setStr := func(key, val string) {
		if n := valueNode(key); n != nil {
			n.Kind, n.Tag, n.Value, n.Style = yaml.ScalarNode, "!!str", val, yaml.DoubleQuotedStyle
			return
		}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val, Style: yaml.DoubleQuotedStyle})
	}
	// readTime distinguishes absent (ok=false, no error) from present-invalid
	// (ok=true, error). Empty string counts as absent, matching readTaskTime.
	// A non-scalar node (e.g. created: {bad: value}) is present-and-invalid,
	// not absent — a mapping/sequence node's own Value field is always "",
	// which would otherwise be indistinguishable from a genuinely absent or
	// empty-string field and let setStr silently overwrite it instead of
	// erroring.
	readTime := func(key string) (time.Time, bool, error) {
		n := valueNode(key)
		if n == nil {
			return time.Time{}, false, nil
		}
		if n.Kind != yaml.ScalarNode {
			return time.Time{}, true, fmt.Errorf("%s is not a valid RFC3339 timestamp", key)
		}
		if n.Value == "" {
			return time.Time{}, false, nil
		}
		for _, l := range []string{taskTimeLayout, time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(l, n.Value); err == nil {
				return t, true, nil
			}
		}
		return time.Time{}, true, fmt.Errorf("%s is not a valid RFC3339 timestamp: %q", key, n.Value)
	}

	// Read (and validate) all three up front, so a present-invalid deadline or
	// expires errors even when there is no created to key a shift off.
	created, hadCreated, err := readTime("created")
	if err != nil {
		return "", err
	}
	deadline, hasDeadline, err := readTime("deadline")
	if err != nil {
		return "", err
	}
	expires, hasExpires, err := readTime("expires")
	if err != nil {
		return "", err
	}

	setStr("created", now.UTC().Format(taskTimeLayout))
	if hadCreated {
		delta := now.Sub(created)
		if hasDeadline {
			setStr("deadline", deadline.Add(delta).UTC().Format(taskTimeLayout))
		}
		if hasExpires {
			setStr("expires", expires.Add(delta).UTC().Format(taskTimeLayout))
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	// Encode &doc (the original document node), not root: a comment with
	// nothing following it (the last line of the file) is attached by the
	// parser as the DOCUMENT's FootComment, not the mapping node's — encoding
	// root alone would silently drop it. root's Content is doc.Content[0]
	// itself (mutated in place above), so this still emits every rewritten
	// field.
	if err := enc.Encode(&doc); err != nil {
		return "", fmt.Errorf("re-encode task definition: %w", err)
	}
	enc.Close()
	return buf.String(), nil
}

// Field syntaxes from the queue's create-task schema, used to reject a
// malformed definition in the dialog rather than after a round-trip.
var (
	provisionerIDRe = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,38}$`)
	workerTypeRe    = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,36}[a-z0-9])?$`)
	taskQueueIDRe   = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,38}/[a-z]([-a-z0-9]{0,36}[a-z0-9])?$`)
	schedulerIDRe   = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,38}$`)
	projectIDRe     = regexp.MustCompile(`^[a-zA-Z0-9._/-]{1,500}$`)
	scopeRe         = regexp.MustCompile(`^[ -~]*$`)
	sourceRe        = regexp.MustCompile(`^(https?://|ssh://|git@)`)
)

// validTaskPriorities is the create-task priority enum; validTaskRequires is
// the `requires` enum. Neither includes the empty string: omitting the field
// lets the queue apply its default (lowest / all-completed), but an explicit
// empty (or null) value is not a member of the enum and the queue rejects it,
// so callers gate these lookups on the field's raw presence.
var (
	validTaskPriorities = map[string]bool{
		"highest": true, "very-high": true, "high": true,
		"medium": true, "low": true, "very-low": true, "lowest": true, "normal": true,
	}
	validTaskRequires = map[string]bool{
		"all-completed": true, "all-resolved": true,
	}
)

// allowedTaskFields / allowedMetadataFields are the exact top-level and
// metadata key names the queue's create-task schema defines. Go's JSON
// decoding matches struct fields case-insensitively, so DisallowUnknownFields
// does NOT catch a case-variant or otherwise-misspelled key (e.g.
// `provisionerID`): it maps to the right field, passes validation, and — since
// the raw body is submitted verbatim — is then sent unchanged for the queue to
// reject. Checking the raw keys against these sets, case-sensitively, catches
// that in the dialog instead.
var (
	allowedTaskFields = map[string]bool{
		"created": true, "deadline": true, "dependencies": true, "expires": true,
		"extra": true, "metadata": true, "payload": true, "priority": true,
		"projectId": true, "provisionerId": true, "requires": true, "retries": true,
		"routes": true, "schedulerId": true, "scopes": true, "tags": true,
		"taskGroupId": true, "taskQueueId": true, "workerType": true,
	}
	allowedMetadataFields = map[string]bool{
		"name": true, "description": true, "owner": true, "source": true,
	}
)

// isJSONNull reports whether raw is the JSON null literal.
func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// rejectNullValues walks raw and returns an error naming the first JSON null it
// finds. Objects and arrays are descended recursively (path names the location
// for the message, e.g. "tags.x" or "scopes[0]"); a scalar null is reported as
// "<path> may not be null". It is used on the create-task fields that have a
// fixed type, where a null is never schema-valid — see buildTaskDefinition.
func rejectNullValues(raw json.RawMessage, path string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if string(trimmed) == "null" {
		return fmt.Errorf("%s may not be null", path)
	}
	switch trimmed[0] {
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil // a malformed/mistyped object is caught by the typed decode
		}
		for k, v := range obj {
			if err := rejectNullValues(v, path+"."+k); err != nil {
				return err
			}
		}
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil
		}
		for i, v := range arr {
			if err := rejectNullValues(v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectUnknownKeys returns an error naming a key in obj that is not in
// allowed. label prefixes the message (e.g. "" for a top-level field,
// "metadata " for a metadata field).
func rejectUnknownKeys(obj map[string]json.RawMessage, allowed map[string]bool, label string) error {
	for k := range obj {
		if !allowed[k] {
			return fmt.Errorf("unknown %sfield %q", label, k)
		}
	}
	return nil
}

// validateTaskDefinition enforces the queue's create-task schema constraints
// up front, so a schema-invalid definition is rejected in the dialog (with a
// fixable message) rather than after a round-trip to the server. top is the
// raw definition (used for presence-sensitive checks the omitempty typed
// struct can't distinguish, e.g. an explicit empty projectId); def is its
// typed view.
func validateTaskDefinition(top map[string]json.RawMessage, def *tcqueue.TaskDefinitionRequest) error {
	// Task queue identity: taskQueueId, or provisionerId + workerType. The
	// queue accepts the transitional form where all three are present (a task
	// fetched from the queue carries taskQueueId AND provisionerId/workerType),
	// so their coexistence is allowed rather than rejected — only a lone
	// provisionerId or workerType, with no taskQueueId, is incomplete. When
	// all three are present they must be consistent, matching the queue.
	if def.TaskQueueID == "" && (def.ProvisionerID == "" || def.WorkerType == "") {
		return fmt.Errorf("needs taskQueueId (or provisionerId + workerType)")
	}
	if def.TaskQueueID != "" && !taskQueueIDRe.MatchString(def.TaskQueueID) {
		return fmt.Errorf("taskQueueId %q is malformed", def.TaskQueueID)
	}
	if def.ProvisionerID != "" && !provisionerIDRe.MatchString(def.ProvisionerID) {
		return fmt.Errorf("provisionerId %q is malformed", def.ProvisionerID)
	}
	if def.WorkerType != "" && !workerTypeRe.MatchString(def.WorkerType) {
		return fmt.Errorf("workerType %q is malformed", def.WorkerType)
	}
	if def.TaskQueueID != "" && def.ProvisionerID != "" && def.WorkerType != "" &&
		def.TaskQueueID != def.ProvisionerID+"/"+def.WorkerType {
		return fmt.Errorf("taskQueueId %q must equal provisionerId/workerType %q",
			def.TaskQueueID, def.ProvisionerID+"/"+def.WorkerType)
	}

	// Timestamps: both required (the keep-timestamps variant does not set
	// created, so an omitted one is caught here rather than defaulting).
	if time.Time(def.Created).IsZero() {
		return fmt.Errorf("needs a created timestamp")
	}
	if time.Time(def.Deadline).IsZero() {
		return fmt.Errorf("needs a deadline")
	}
	// Ordering invariants the queue enforces (queue createTask): deadline may
	// not precede created, and expires (when present) may not precede deadline.
	// A reused definition whose deadline sat before its created keeps that
	// negative offset through a rebase (the offset is preserved), so catch it
	// here rather than after a round-trip. Equal values are allowed, matching
	// the queue's `created > deadline` / `deadline > expires` rejections.
	created, deadline := time.Time(def.Created), time.Time(def.Deadline)
	if deadline.Before(created) {
		return fmt.Errorf("deadline %s is before created %s",
			deadline.UTC().Format(taskTimeLayout), created.UTC().Format(taskTimeLayout))
	}
	if expires := time.Time(def.Expires); !expires.IsZero() && expires.Before(deadline) {
		return fmt.Errorf("expires %s is before deadline %s",
			expires.UTC().Format(taskTimeLayout), deadline.UTC().Format(taskTimeLayout))
	}

	// Metadata: presence of the four required properties (and rejection of
	// unknown keys) is enforced in buildTaskDefinition on the raw map. The
	// schema sets no minLength, so an empty name/description/owner is valid;
	// here enforce only the maxLength bounds — counted in Unicode code points,
	// as JSON Schema does, not bytes — and the source URL constraints.
	if utf8.RuneCountInString(def.Metadata.Name) > 255 {
		return fmt.Errorf("metadata.name exceeds 255 characters")
	}
	if utf8.RuneCountInString(def.Metadata.Description) > 32768 {
		return fmt.Errorf("metadata.description exceeds 32768 characters")
	}
	if utf8.RuneCountInString(def.Metadata.Owner) > 255 {
		return fmt.Errorf("metadata.owner exceeds 255 characters")
	}
	if utf8.RuneCountInString(def.Metadata.Source) > 4096 {
		return fmt.Errorf("metadata.source exceeds 4096 characters")
	}
	if !sourceRe.MatchString(def.Metadata.Source) {
		return fmt.Errorf("metadata.source %q must start with http(s)://, ssh://, or git@", def.Metadata.Source)
	}
	if err := validateSourceFormat(def.Metadata.Source); err != nil {
		return err
	}

	// Payload and extra must be JSON objects (extra only when present).
	if err := validateTaskObject(def.Payload, "payload", true); err != nil {
		return err
	}
	if err := validateTaskObject(def.Extra, "extra", false); err != nil {
		return err
	}

	// priority/requires: an explicit value (including an empty string or null,
	// both of which decode to "") must be a member of the enum; only omission is
	// allowed to fall back to the queue's default, so gate on raw presence.
	if _, present := top["priority"]; present && !validTaskPriorities[def.Priority] {
		return fmt.Errorf("priority %q is not a valid priority", def.Priority)
	}
	if _, present := top["requires"]; present && !validTaskRequires[def.Requires] {
		return fmt.Errorf("requires %q must be all-completed or all-resolved", def.Requires)
	}
	if def.Retries < 0 || def.Retries > 49 {
		return fmt.Errorf("retries must be between 0 and 49, got %d", def.Retries)
	}

	// Optional identifiers. Each defaults to a valid value when omitted, but an
	// explicit empty (or null) value — which the omitempty typed struct can't
	// distinguish from absent — violates the schema's pattern/minimum length, so
	// gate on the field's raw presence rather than on the zero-collapsed value.
	if _, present := top["schedulerId"]; present && !schedulerIDRe.MatchString(def.SchedulerID) {
		return fmt.Errorf("schedulerId %q is malformed", def.SchedulerID)
	}
	if _, present := top["projectId"]; present && !projectIDRe.MatchString(def.ProjectID) {
		return fmt.Errorf("projectId %q is malformed", def.ProjectID)
	}
	if _, present := top["taskGroupId"]; present && !slugidRe.MatchString(def.TaskGroupID) {
		return fmt.Errorf("taskGroupId %q is not a valid slugid", def.TaskGroupID)
	}

	// Dependencies: each a slugid, unique (schema uniqueItems), and within the
	// deployment's max-task-dependencies. That cap is deployment-configurable
	// but can only be lowered from the default of 10000, so >10000 is always
	// invalid regardless of deployment.
	if len(def.Dependencies) > 10000 {
		return fmt.Errorf("dependencies exceeds the maximum of 10000")
	}
	seenDep := make(map[string]bool, len(def.Dependencies))
	for _, dep := range def.Dependencies {
		if !slugidRe.MatchString(dep) {
			return fmt.Errorf("dependency %q is not a valid slugid", dep)
		}
		if seenDep[dep] {
			return fmt.Errorf("duplicate dependency %q", dep)
		}
		seenDep[dep] = true
	}

	// Routes: at most 64 (schema maxItems), unique (schema uniqueItems), each
	// 1-249 characters.
	if len(def.Routes) > 64 {
		return fmt.Errorf("routes exceeds the maximum of 64")
	}
	seenRoute := make(map[string]bool, len(def.Routes))
	for _, route := range def.Routes {
		if n := utf8.RuneCountInString(route); n < 1 || n > 249 {
			return fmt.Errorf("route %q must be 1-249 characters", route)
		}
		if seenRoute[route] {
			return fmt.Errorf("duplicate route %q", route)
		}
		seenRoute[route] = true
	}

	// Scopes: printable ASCII, not ending in more than one '*'. Duplicates are
	// allowed (schema uniqueItems: false).
	for _, scope := range def.Scopes {
		if !scopeRe.MatchString(scope) {
			return fmt.Errorf("scope %q must be printable ASCII", scope)
		}
		if strings.HasSuffix(scope, "**") {
			return fmt.Errorf("scope %q may not end in more than one '*'", scope)
		}
	}
	for k, v := range def.Tags {
		if utf8.RuneCountInString(v) > 4096 {
			return fmt.Errorf("tag %q value exceeds 4096 characters", k)
		}
	}
	return nil
}

// validateSourceFormat enforces metadata.source's anyOf[format:uri,
// format:regex] constraint (in addition to the prefix pattern checked by
// sourceRe): the value must parse as a URL or compile as a regex. A git@…
// source satisfies the regex branch; a value like "https://[" satisfies
// neither and is rejected here rather than by the server.
func validateSourceFormat(src string) error {
	if _, err := url.Parse(src); err == nil {
		return nil
	}
	if _, err := regexp.Compile(src); err == nil {
		return nil
	}
	return fmt.Errorf("metadata.source %q is not a valid URL", src)
}

// validateTaskObject checks that raw is a JSON object. When required, an
// absent/null value is an error ("needs a <name>"); otherwise absent is fine
// and only a present non-object is rejected. Inner contents (worker-specific
// payload, arbitrary extra) are left to the server to validate.
func validateTaskObject(raw json.RawMessage, name string, required bool) error {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return fmt.Errorf("needs a %s", name)
		}
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("%s must be an object", name)
	}
	return nil
}
