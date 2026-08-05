package resource

import (
	"fmt"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

type LaunchConfigsResource struct {
	tc taskcluster.Taskcluster
}

func NewLaunchConfigsResource(tc taskcluster.Taskcluster) *LaunchConfigsResource {
	return &LaunchConfigsResource{tc: tc}
}

func (r *LaunchConfigsResource) Name() string      { return "launchconfigs" }
func (r *LaunchConfigsResource) Aliases() []string { return []string{"lc", "configs"} }
func (r *LaunchConfigsResource) Description() string {
	return "Launch configurations for a worker pool (scoped list)"
}

func (r *LaunchConfigsResource) Columns() []Column {
	return []Column{
		{Title: "LAUNCH CONFIG ID"},
		{Title: "CREATED", Width: 24},
		{Title: "ARCHIVED", Width: 12},
	}
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *LaunchConfigsResource) List() ([]Row, error) {
	return nil, fmt.Errorf("launchconfigs requires a worker pool scope")
}

// FacetOptions: "active" is the default tab, "all" includes archived configs too.
func (r *LaunchConfigsResource) FacetOptions() []string {
	return []string{"active", "all"}
}

// FacetList's first parameter, unlike WorkersResource's and ErrorsResource's,
// is always a bare worker pool ID — there's no path that narrows a launch
// config list to another launch config, so parseScope is deliberately unused
// here.
func (r *LaunchConfigsResource) FacetList(workerPoolID, value string) ([]Row, error) {
	configs, err := r.tc.GetWorkerPoolLaunchConfigs(workerPoolID, value == "all")
	if err != nil {
		return nil, err
	}

	return launchConfigRows(configs), nil
}

// FacetCounts fetches the full (includeArchived=true) list once and counts
// active vs. all client-side — worker pools have at most a handful of launch
// configs, so this is cheap despite not being a dedicated stats call (there
// isn't one for archived-vs-active).
func (r *LaunchConfigsResource) FacetCounts(workerPoolID string) (map[string]int, error) {
	total, active, err := launchConfigCounts(r.tc, workerPoolID)
	if err != nil {
		return nil, err
	}

	return map[string]int{"active": active, "all": total}, nil
}

// launchConfigCounts fetches every launch config (active and archived) for
// workerPoolID and returns the total count and the active-only count. Shared
// by FacetCounts and WorkerPoolsResource.Describe's summary line.
func launchConfigCounts(tc taskcluster.Taskcluster, workerPoolID string) (total, active int, err error) {
	configs, err := tc.GetWorkerPoolLaunchConfigs(workerPoolID, true)
	if err != nil {
		return 0, 0, err
	}

	for _, c := range configs {
		if !c.IsArchived {
			active++
		}
	}

	return len(configs), active, nil
}

// ScopeActions returns the worker-pool sibling jump keys (minus
// "launchconfigs" itself) for scope — see resource.ScopeActions. This is
// deliberately not applied to Describe, which already narrows its own w/e
// actions to one specific launch config.
func (r *LaunchConfigsResource) ScopeActions(scope string) []DetailAction {
	workerPoolID, _ := parseScope(scope)
	return workerPoolActions(workerPoolID, r.Name())
}

// ScopedList exists only so LaunchConfigsResource still satisfies
// ScopedResource (needed for the EmptyScopeResource redirect when no scope is
// given); the shell always prefers FacetList via the ServerFaceted branch.
func (r *LaunchConfigsResource) ScopedList(workerPoolID string) ([]Row, error) {
	return r.FacetList(workerPoolID, r.FacetOptions()[0])
}

// launchConfigRows converts a WorkerPoolLaunchConfigList into the shell's
// generic Row shape.
func launchConfigRows(configs taskcluster.WorkerPoolLaunchConfigList) []Row {
	rows := make([]Row, 0, len(configs))
	for _, c := range configs {
		archived := "no"
		if c.IsArchived {
			archived = "yes"
		}
		rows = append(rows, Row{
			ID: composeScope(c.WorkerPoolID, c.LaunchConfigID),
			Cells: []string{
				c.LaunchConfigID,
				c.Created.String(),
				archived,
			},
		})
	}

	return rows
}

func (r *LaunchConfigsResource) ScopePromptLabel() string { return "worker pool id" }

func (r *LaunchConfigsResource) EmptyScopeResource() string {
	return "workerpools"
}

// Describe fetches the full launch config list for the pool (there is no
// per-ID fetch endpoint) and finds the matching entry client-side.
func (r *LaunchConfigsResource) Describe(id string) (Detail, error) {
	workerPoolID, launchConfigID := parseScope(id)

	configs, err := r.tc.GetWorkerPoolLaunchConfigs(workerPoolID, true)
	if err != nil {
		return Detail{}, err
	}

	for _, c := range configs {
		if c.LaunchConfigID != launchConfigID {
			continue
		}

		body := fmt.Sprintf(
			"%s\n"+
				"[green]Configuration:[white]\n%s\n\n",
			fieldRow(30, "Created", fmt.Sprint(c.Created), "Last Modified", fmt.Sprint(c.LastModified), "Archived", fmt.Sprintf("%t", c.IsArchived)),
			renderYAML(c.Configuration),
		)

		return Detail{
			Title: fmt.Sprintf("Launch Config :: %s", c.LaunchConfigID),
			Body:  body,
			Actions: []DetailAction{
				{
					Key:   'w',
					Label: "workers",
					Target: NavTarget{
						ResourceName: "workers",
						ID:           composeScope(workerPoolID, launchConfigID),
						Kind:         NavScopedList,
					},
				},
				{
					Key:   'e',
					Label: "errors",
					Target: NavTarget{
						ResourceName: "errors",
						ID:           composeScope(workerPoolID, launchConfigID),
						Kind:         NavScopedList,
					},
				},
			},
		}, nil
	}

	return Detail{}, fmt.Errorf("launch config %q not found in worker pool %q", launchConfigID, workerPoolID)
}

func (r *LaunchConfigsResource) RefreshInterval() time.Duration {
	return 15 * time.Second
}

// ListWebURL links to the worker-manager pool's launch-configs page. scope
// here is always a bare worker pool ID (see FacetList's doc comment).
func (r *LaunchConfigsResource) ListWebURL(rootURL, scope string) string {
	return webUIPath(rootURL, "worker-manager/"+pathSegment(scope)+"/launch-configs")
}

// DetailWebURL links to the same launch-configs page as ListWebURL, narrowed
// to this one launch config via query param — there's no per-config route in
// the web UI.
func (r *LaunchConfigsResource) DetailWebURL(rootURL, id string) string {
	workerPoolID, launchConfigID := parseScope(id)
	path := "worker-manager/" + pathSegment(workerPoolID) + "/launch-configs"
	return webUIPath(rootURL, withQuery(path, "launchConfigId", launchConfigID))
}
