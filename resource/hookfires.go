package resource

import (
	"fmt"
	"sort"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// HookFiresResource lists a hook's recent fires, scoped by the hook's own
// "hookGroupId/hookId" (the same row ID HooksResource uses). Every row
// overrides navigation via NavTarget straight to the fired task's Detail
// view — even an error-result fire carries the task ID that would have been
// used, so selecting one of those simply lands on the task's own "not found"
// error screen, matching the web UI's behavior of linking every fire's task.
type HookFiresResource struct {
	tc taskcluster.Taskcluster
}

func NewHookFiresResource(tc taskcluster.Taskcluster) *HookFiresResource {
	return &HookFiresResource{tc: tc}
}

func (r *HookFiresResource) Name() string      { return "hookfires" }
func (r *HookFiresResource) Aliases() []string { return []string{"fires"} }
func (r *HookFiresResource) Description() string {
	return "A hook's recent fires (scoped list) — select one to jump to its task"
}

func (r *HookFiresResource) Columns() []Column {
	return []Column{
		{Title: "TASK ID", Width: taskIDColumnWidth},
		{Title: "RESULT", Width: 10},
		{Title: "STATE", Width: 12},
		{Title: "FIRED BY", Width: 18},
		{Title: "FIRED AT", Expand: true},
	}
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *HookFiresResource) List() ([]Row, error) {
	return nil, fmt.Errorf("hookfires requires a hookGroupId/hookId scope")
}

func (r *HookFiresResource) ScopedList(scope string) ([]Row, error) {
	group, hookID, err := splitHookID(scope)
	if err != nil {
		return nil, err
	}

	fires, err := r.tc.GetHookLastFires(group, hookID)
	if err != nil {
		return nil, err
	}
	sortHookFiresNewestFirst(fires)

	rows := make([]Row, 0, len(fires))
	for _, f := range fires {
		rows = append(rows, Row{
			ID: f.TaskID,
			Cells: []string{
				f.TaskID,
				renderHookFireResult(f.Result),
				renderTaskState(f.TaskState),
				f.FiredBy,
				fmt.Sprint(f.TaskCreateTime),
			},
			NavTarget: &NavTarget{ResourceName: "task", ID: f.TaskID, Kind: NavDetail},
		})
	}

	return rows, nil
}

func (r *HookFiresResource) ScopePromptLabel() string { return "hook id (group/hookId)" }

func (r *HookFiresResource) EmptyScopeResource() string { return "hooks" }

// Describe is unreachable in normal use — see the type doc comment.
// Implemented only to satisfy the Resource interface.
func (r *HookFiresResource) Describe(id string) (Detail, error) {
	return Detail{}, fmt.Errorf("hook fires are not viewable directly — select one to open its task")
}

func (r *HookFiresResource) RefreshInterval() time.Duration { return 15 * time.Second }

// ListWebURL links to the scoped hook's own page — the web UI shows its
// fires there, with no dedicated last-fires page.
func (r *HookFiresResource) ListWebURL(rootURL, scope string) string {
	return hookWebURL(rootURL, scope)
}

// DetailWebURL is never expected to be called — see Describe's doc comment.
func (r *HookFiresResource) DetailWebURL(rootURL, id string) string { return "" }

// sortHookFiresNewestFirst orders fires by task creation time, newest first
// — the order both the hook Detail's inline fires section and the hookfires
// list render in.
func sortHookFiresNewestFirst(fires taskcluster.HookLastFireList) {
	sort.SliceStable(fires, func(i, j int) bool {
		return time.Time(fires[i].TaskCreateTime).After(time.Time(fires[j].TaskCreateTime))
	})
}

// renderHookFireResult colors a fire's result — green for success, red for
// anything else (the API's only other value today is "error").
func renderHookFireResult(result string) string {
	color := "green"
	if result != "success" {
		color = "red"
	}
	return fmt.Sprintf("[%s]%s[white]", color, result)
}

// hookWebURL builds the web UI page for one hook (id is
// "hookGroupId/hookId"), shared by HooksResource's Detail view and
// HookFiresResource's scoped list.
func hookWebURL(rootURL, id string) string {
	group, hookID, err := splitHookID(id)
	if err != nil {
		return ""
	}
	return webUIPath(rootURL, "hooks/"+pathSegment(group)+"/"+pathSegment(hookID))
}
