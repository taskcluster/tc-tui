package resource

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/taskcluster/slugid-go/slugid"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// taskStateTTL bounds how long a recorded task-state snapshot is trusted. The
// detail refresh loop rewrites it well inside this window; the TTL only guards
// against acting on a snapshot left behind long ago.
const taskStateTTL = 60 * time.Second

// taskStateSnapshot is the minimum a task-detail render already knows and that
// lifecycleActions needs synchronously (on the UI goroutine) to decide which
// mutating actions are valid and to prefill the change-priority input.
type taskStateSnapshot struct {
	state    string // unscheduled|pending|running|completed|failed|exception
	priority string
	at       time.Time
}

// taskStateCache is a small shared map of the last-described state/priority per
// task id, written by describeTask (off the UI thread) and read by
// lifecycleActions (on the UI thread). It exists because Actions(id) runs on the
// event loop and must not make an API call to learn a task's state.
type taskStateCache struct {
	mu   sync.Mutex
	data map[string]taskStateSnapshot
}

func NewTaskStateCache() *taskStateCache {
	return &taskStateCache{data: make(map[string]taskStateSnapshot)}
}

// record stores the state/priority for taskID as of now. Nil-safe.
func (c *taskStateCache) record(taskID, state, priority string, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[taskID] = taskStateSnapshot{state: state, priority: priority, at: now}
}

// lookup returns the snapshot for taskID if one exists and is within the TTL.
// Nil-safe: a nil cache reports not-found rather than panicking.
func (c *taskStateCache) lookup(taskID string, now time.Time) (taskStateSnapshot, bool) {
	if c == nil {
		return taskStateSnapshot{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, ok := c.data[taskID]
	if !ok || now.Sub(snap.at) > taskStateTTL {
		return taskStateSnapshot{}, false
	}
	return snap, true
}

// taskLifecycleInvalidates names the list views whose cached rows a lifecycle
// mutation can change, so they re-fetch on next load. The launching view is
// always refreshed by the shell regardless; this covers the others.
var taskLifecycleInvalidates = []string{"tasks", "taskgroup", "pending", "claimed"}

// lifecycleActions returns the mutating actions valid for taskID given its
// last-recorded state. Retrigger is always available (it clones the definition
// under a new id and needs no state); the rest are gated on the snapshot. When
// no fresh snapshot exists, only retrigger is offered.
func lifecycleActions(tc taskcluster.Taskcluster, cache *taskStateCache, taskID string) []Action {
	var actions []Action

	if snap, ok := cache.lookup(taskID, time.Now()); ok {
		switch snap.state {
		case "unscheduled":
			actions = append(actions, scheduleTaskAction(tc, taskID))
			actions = append(actions, cancelTaskAction(tc, taskID))
			actions = append(actions, changePriorityAction(tc, taskID, snap.priority))
		case "pending", "running":
			actions = append(actions, cancelTaskAction(tc, taskID))
			actions = append(actions, changePriorityAction(tc, taskID, snap.priority))
		case "completed", "failed", "exception":
			actions = append(actions, rerunTaskAction(tc, taskID))
		}
	}

	actions = append(actions, retriggerTaskAction(tc, taskID))
	return actions
}

// taskPriorities is the change-task-priority enum, from the queue schema.
var taskPriorities = []string{
	"highest", "very-high", "high", "medium", "low", "very-low", "lowest", "normal",
}

func joinPriorities() string { return strings.Join(taskPriorities, ", ") }

// buildRetriggerDefinition fetches taskID's definition and produces a fresh
// (id, body) to submit: timestamps rebased to now, retries reset to zero, a
// newly minted slugid. It reuses buildTaskDefinition for the rebase +
// create-task schema validation, so a definition the queue would reject is
// caught before submission ("this may not work with all tasks").
func buildRetriggerDefinition(tc taskcluster.Taskcluster, taskID string, now time.Time) (string, json.RawMessage, error) {
	def, err := tc.GetTask(taskID)
	if err != nil {
		return "", nil, err
	}

	encoded, err := json.Marshal(def)
	if err != nil {
		return "", nil, fmt.Errorf("encode task definition: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &top); err != nil {
		return "", nil, fmt.Errorf("decode task definition: %w", err)
	}

	// The response type marshals fields the queue omitted as JSON null (nil
	// slices, an absent extra). buildTaskDefinition rejects a present null, and
	// an omittable identifier defaults correctly only when the key is absent —
	// so drop every null-valued key rather than resubmit it.
	for k, raw := range top {
		if isJSONNull(raw) {
			delete(top, k)
		}
	}
	top["retries"] = json.RawMessage("0")

	raw, err := json.Marshal(top)
	if err != nil {
		return "", nil, fmt.Errorf("encode task definition: %w", err)
	}

	bt, err := buildTaskDefinition(string(raw), now, true)
	if err != nil {
		return "", nil, fmt.Errorf("cannot retrigger this task: %w", err)
	}
	return slugid.Nice(), bt.body, nil
}

// validateTaskPriority accepts exactly one of the enum values (case-sensitive,
// no surrounding whitespace — the queue matches the schema enum verbatim).
func validateTaskPriority(v string) error {
	for _, p := range taskPriorities {
		if v == p {
			return nil
		}
	}
	return fmt.Errorf("priority must be one of: %s", joinPriorities())
}

// retriggerTaskAction clones taskID under a new id (rebased timestamps,
// retries reset to zero) and navigates to the created task. The built (id,
// body) is memoized so a retry after a transport error resubmits the identical
// pair rather than minting a fresh slugid — keeping CreateTask idempotent,
// mirroring createTaskAction.
func retriggerTaskAction(tc taskcluster.Taskcluster, taskID string) Action {
	var (
		builtID   string
		builtBody json.RawMessage
	)
	return Action{
		Key:   'T',
		Label: "retrigger task",
		Prompt: "This will duplicate the task and create it under a different taskId. " +
			"The new task will be altered to update deadlines and other timestamps " +
			"for the current time and set the number of retries to zero. Note: this " +
			"may not work with all tasks.",
		Perform: func(ActionInput) error {
			if builtID == "" {
				id, body, err := buildRetriggerDefinition(tc, taskID, time.Now())
				if err != nil {
					return err
				}
				builtID, builtBody = id, body
			}
			_, err := tc.CreateTask(builtID, builtBody)
			return err
		},
		Next: func() (NavTarget, bool) {
			if builtID == "" {
				return NavTarget{}, false
			}
			return NavTarget{ResourceName: "task", ID: builtID, Kind: NavDetail}, true
		},
		Invalidates: taskLifecycleInvalidates,
	}
}

func cancelTaskAction(tc taskcluster.Taskcluster, taskID string) Action {
	return Action{
		Key:         'K',
		Label:       "cancel task",
		Destructive: true,
		// Cancel does not delete the task — it resolves the current run as an
		// exception — so the detail should refresh to show the new state and
		// re-gate its actions, unlike a true delete which the shell skips.
		RefreshAfter: true,
		Prompt: fmt.Sprintf("Cancel task %s? Its current run will be resolved as an "+
			"exception with reason \"canceled\".", taskID),
		Perform: func(ActionInput) error {
			_, err := tc.CancelTask(taskID)
			return err
		},
		Invalidates: taskLifecycleInvalidates,
	}
}

func rerunTaskAction(tc taskcluster.Taskcluster, taskID string) Action {
	return Action{
		Key:   'E',
		Label: "rerun task",
		Prompt: "This will cause a new run of the task to be created with the same " +
			"taskId. It will only succeed if the task hasn't passed its deadline. " +
			"Notice that this may interfere with listeners who only expect this " +
			"task to be resolved once.",
		Perform: func(ActionInput) error {
			_, err := tc.RerunTask(taskID)
			return err
		},
		Invalidates: taskLifecycleInvalidates,
	}
}

func scheduleTaskAction(tc taskcluster.Taskcluster, taskID string) Action {
	return Action{
		Key:   'S',
		Label: "schedule task",
		Prompt: fmt.Sprintf("Schedule task %s? It will be announced as pending and "+
			"claimable even if its dependencies are unresolved.", taskID),
		Perform: func(ActionInput) error {
			_, err := tc.ScheduleTask(taskID)
			return err
		},
		Invalidates: taskLifecycleInvalidates,
	}
}

func changePriorityAction(tc taskcluster.Taskcluster, taskID, current string) Action {
	return Action{
		Key:         'P',
		Label:       "change priority",
		Input:       InputLine,
		InputLabel:  "priority",
		InitialText: current,
		Prompt: fmt.Sprintf("Change priority of task %s — one of: %s. (Experimental: a "+
			"claimed or running run keeps its priority until it is retried.)", taskID, joinPriorities()),
		Validate: func(in ActionInput) error {
			return validateTaskPriority(in.Raw)
		},
		Perform: func(in ActionInput) error {
			_, err := tc.ChangeTaskPriority(taskID, in.Raw)
			return err
		},
		Invalidates: taskLifecycleInvalidates,
	}
}
