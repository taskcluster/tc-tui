package controller

import (
	"github.com/taskcluster/tc-tui/resource"
	"github.com/taskcluster/tc-tui/shell"
	"github.com/taskcluster/tc-tui/state"
	"github.com/taskcluster/tc-tui/taskcluster"
)

const rootResource = "workerpools"

type TcController interface {
	StartUI() error
	// StartUIAt behaves like StartUI, but jumps straight to a resource
	// name/alias (and optional scope/id), the way `:name scope` in the
	// command bar would — used for the CLI's positional arguments.
	StartUIAt(name, scope string) error
}

type Controller struct {
	tc       taskcluster.Taskcluster
	shell    *shell.Shell
	registry *resource.Registry

	// statePath is "" if persisting navigation state is unavailable (e.g. the
	// OS user cache directory can't be resolved), in which case load/save are
	// both skipped and the app behaves as if this feature didn't exist.
	statePath string
}

// buildRegistry registers every resource against tc. tc may be nil for a
// registry used only for its resource names/descriptions/columns (see
// HelpText) — resources dereference their client in List/Describe only,
// never at construction.
func buildRegistry(tc taskcluster.Taskcluster) *resource.Registry {
	registry := resource.NewRegistry()
	registry.Register(resource.NewRolesResource(tc))
	registry.Register(resource.NewClientsResource(tc))
	registry.Register(resource.NewSecretsResource(tc))
	registry.Register(resource.NewHooksResource(tc))
	registry.Register(resource.NewHookFiresResource(tc))
	registry.Register(resource.NewPurgeCacheResource(tc))
	registry.Register(resource.NewTaskIndexResource(tc))
	registry.Register(resource.NewWorkerPoolsResource(tc))
	registry.Register(resource.NewWorkersResource(tc))
	registry.Register(resource.NewWorkerRecentTasksResource(tc))
	registry.Register(resource.NewLaunchConfigsResource(tc))
	registry.Register(resource.NewErrorsResource(tc))
	taskHistory := resource.NewTaskDefHistory()
	taskStateCache := resource.NewTaskStateCache()
	registry.Register(resource.NewTaskResource(tc, taskStateCache))
	registry.Register(resource.NewTaskGroupResource(tc, taskHistory, taskStateCache))
	registry.Register(resource.NewTasksResource(tc, taskHistory, taskStateCache))
	registry.Register(resource.NewCreateTaskResource(tc, taskHistory))
	registry.Register(resource.NewTaskDependenciesResource(tc, taskStateCache))
	registry.Register(resource.NewTaskDependentsResource(tc, taskStateCache))
	registry.Register(resource.NewTaskRunsResource(tc))
	registry.Register(resource.NewTaskArtifactsResource(tc))
	registry.Register(resource.NewPendingTasksResource(tc, taskStateCache))
	registry.Register(resource.NewClaimedTasksResource(tc, taskStateCache))
	registry.Register(resource.NewGithubBuildsResource(tc))
	registry.Register(resource.NewGithubRepositoryResource(tc))
	registry.Register(resource.NewHistoryResource())
	return registry
}

// HelpText returns the same resource-derived help content shown by the
// in-app '?' overlay, formatted for a plain terminal — used by the CLI's
// --help. It deliberately never constructs a Taskcluster client, so `tc-tui
// --help` works without TASKCLUSTER_ROOT_URL set (a real client panics
// without it).
func HelpText() string {
	return shell.PlainHelpText(buildRegistry(nil))
}

func NewController() TcController {
	tc := taskcluster.NewTaskcluster()

	registry := buildRegistry(tc)

	sh := shell.New(registry)

	statePath, err := state.Path(tc.GetRoot())
	if err == nil {
		sh.RestoreState(state.Load(statePath))
	}

	return &Controller{
		tc:        tc,
		shell:     sh,
		registry:  registry,
		statePath: statePath,
	}
}

func (c *Controller) StartUI() error {
	return c.run(func() error { return c.shell.Start(rootResource) })
}

func (c *Controller) StartUIAt(name, scope string) error {
	return c.run(func() error { return c.shell.StartAt(rootResource, name, scope) })
}

// run wires up header info and persists navigation state around whichever
// of Shell's blocking entry points (Start/StartAt) start renders the app.
func (c *Controller) run(start func() error) error {
	c.shell.SetInfo(c.tc.GetRoot(), "..", "..", false)

	go func() {
		c.shell.SetInfo(
			c.tc.GetRoot(),
			c.tc.GetVersion().Version,
			c.tc.GetClientID(),
			c.tc.IsAuthenticated(),
		)
	}()

	err := start()

	if c.statePath != "" {
		_ = state.Save(c.statePath, c.shell.ExportState())
	}

	return err
}
