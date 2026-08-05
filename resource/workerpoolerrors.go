package resource

import (
	"fmt"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

type ErrorsResource struct {
	tc taskcluster.Taskcluster
}

func NewErrorsResource(tc taskcluster.Taskcluster) *ErrorsResource {
	return &ErrorsResource{tc: tc}
}

func (r *ErrorsResource) Name() string      { return "errors" }
func (r *ErrorsResource) Aliases() []string { return []string{"err"} }
func (r *ErrorsResource) Description() string {
	return "Provisioning errors reported for a worker pool (scoped list, optionally narrowed to one launch config)"
}

func (r *ErrorsResource) Columns() []Column {
	return []Column{
		{Title: "REPORTED", Width: 24},
		{Title: "TITLE", Width: 40},
		{Title: "KIND", Width: 24},
		{Title: "LAUNCH CONFIG ID"},
	}
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *ErrorsResource) List() ([]Row, error) {
	return nil, fmt.Errorf("errors requires a worker pool scope")
}

// ScopedList's scope is either a bare workerPoolId (pool-wide) or a
// workerPoolId::launchConfigId compound (narrowed to one launch config).
func (r *ErrorsResource) ScopedList(scope string) ([]Row, error) {
	workerPoolID, launchConfigID := parseScope(scope)

	errs, err := r.tc.GetWorkerPoolErrors(workerPoolID, launchConfigID)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(errs))
	for _, e := range errs {
		rows = append(rows, Row{
			ID: composeScope(e.WorkerPoolID, e.ErrorID),
			Cells: []string{
				e.Reported.String(),
				e.Title,
				e.Kind,
				e.LaunchConfigID,
			},
		})
	}

	return rows, nil
}

func (r *ErrorsResource) ScopePromptLabel() string { return "worker pool id" }

func (r *ErrorsResource) EmptyScopeResource() string {
	return "workerpools"
}

// ScopeActions returns the worker-pool sibling jump keys (minus "errors"
// itself) for scope — see resource.ScopeActions.
func (r *ErrorsResource) ScopeActions(scope string) []DetailAction {
	workerPoolID, _ := parseScope(scope)
	return workerPoolActions(workerPoolID, r.Name())
}

func (r *ErrorsResource) Describe(id string) (Detail, error) {
	workerPoolID, errorID := parseScope(id)

	e, err := r.tc.GetWorkerPoolError(workerPoolID, errorID)
	if err != nil {
		return Detail{}, err
	}

	body := fmt.Sprintf(
		"[green]Reported:[white] %s\n\n"+
			"[green]Kind:[white] %s\n"+
			"[green]Title:[white] %s\n"+
			"[green]Launch Config ID:[white] %s\n\n"+
			"[green]Description:[white]\n%s\n\n"+
			"[green]Extra:[white]\n%s\n\n",
		e.Reported,
		e.Kind,
		e.Title,
		e.LaunchConfigID,
		renderMarkdown(e.Description),
		renderYAML(e.Extra),
	)

	return Detail{
		Title:   fmt.Sprintf("Worker Pool Error :: %s", e.ErrorID),
		Body:    body,
		Actions: workerPoolActions(workerPoolID, r.Name()),
	}, nil
}

func (r *ErrorsResource) RefreshInterval() time.Duration {
	return 15 * time.Second
}

// ListWebURL links to the worker-manager pool's errors page, narrowed to a
// launch config via query param when scope carries one (see ScopedList's
// doc comment for the scope format).
func (r *ErrorsResource) ListWebURL(rootURL, scope string) string {
	workerPoolID, launchConfigID := parseScope(scope)
	path := "worker-manager/" + pathSegment(workerPoolID) + "/errors"
	return webUIPath(rootURL, withQuery(path, "launchConfigId", launchConfigID))
}

// DetailWebURL links to the same errors page as ListWebURL — there's no
// per-error route in the web UI, so the specific error ID is dropped.
func (r *ErrorsResource) DetailWebURL(rootURL, id string) string {
	workerPoolID, _ := parseScope(id)
	return webUIPath(rootURL, "worker-manager/"+pathSegment(workerPoolID)+"/errors")
}
