package resource

import (
	"fmt"
	"strings"
	"time"

	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcpurgecache"

	"github.com/taskcluster/tc-tui/taskcluster"
)

type PurgeCacheResource struct {
	tc taskcluster.Taskcluster
}

func NewPurgeCacheResource(tc taskcluster.Taskcluster) *PurgeCacheResource {
	return &PurgeCacheResource{tc: tc}
}

func (r *PurgeCacheResource) Name() string      { return "purgecache" }
func (r *PurgeCacheResource) Aliases() []string { return []string{"purge", "cache"} }
func (r *PurgeCacheResource) Description() string {
	return "Open cache-purge requests, scoped to a worker pool"
}

func (r *PurgeCacheResource) Columns() []Column {
	return []Column{
		{Title: "BEFORE", Width: 24},
		{Title: "PROVISIONER", Width: 24},
		{Title: "WORKER TYPE", Width: 24},
		{Title: "CACHE NAME", Expand: true},
	}
}

// List is never expected to be called via normal navigation — the shell
// always either has a scope, or redirects to EmptyScopeResource() first.
func (r *PurgeCacheResource) List() ([]Row, error) {
	return nil, fmt.Errorf("purgecache requires a worker pool scope")
}

func (r *PurgeCacheResource) ScopedList(workerPoolID string) ([]Row, error) {
	if !strings.Contains(workerPoolID, "/") {
		return nil, fmt.Errorf("invalid worker pool id %q", workerPoolID)
	}

	reqs, err := r.tc.GetPurgeCacheRequestsForPool(workerPoolID)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(reqs))
	for _, req := range reqs {
		rows = append(rows, purgeCacheRow(req))
	}
	return rows, nil
}

func (r *PurgeCacheResource) ScopePromptLabel() string { return "worker pool id" }

func (r *PurgeCacheResource) EmptyScopeResource() string { return "workerpools" }

func (r *PurgeCacheResource) ScopeActions(scope string) []DetailAction {
	return workerPoolActions(scope, r.Name())
}

// purgeCacheRow encodes every displayed field into the row ID — there is no
// single-item purge-cache-request fetch API for Describe to refetch from.
func purgeCacheRow(req tcpurgecache.PurgeCacheRequestsEntry) Row {
	before := req.Before.String()
	id := strings.Join([]string{req.ProvisionerID, req.WorkerType, req.CacheName, before}, idSeparator)
	return Row{
		ID:    id,
		Cells: []string{before, req.ProvisionerID, req.WorkerType, req.CacheName},
	}
}

// Describe decodes id (see purgeCacheRow) instead of fetching — there is no
// per-request lookup endpoint to refetch fresh from.
func (r *PurgeCacheResource) Describe(id string) (Detail, error) {
	parts := strings.Split(id, idSeparator)
	if len(parts) != 4 {
		return Detail{}, fmt.Errorf("malformed purge cache request id %q", id)
	}
	provisionerID, workerType, cacheName, before := parts[0], parts[1], parts[2], parts[3]

	body := fmt.Sprintf(
		"[green]Before:[white] %s\n\n[green]Provisioner:[white] %s\n[green]Worker Type:[white] %s\n[green]Cache Name:[white] %s\n",
		before, provisionerID, workerType, cacheName,
	)

	return Detail{
		Title:   fmt.Sprintf("Purge Cache Request :: %s", cacheName),
		Body:    body,
		Actions: workerPoolActions(provisionerID+"/"+workerType, r.Name()),
	}, nil
}

func (r *PurgeCacheResource) RefreshInterval() time.Duration { return 15 * time.Second }

func (r *PurgeCacheResource) ListWebURL(rootURL, scope string) string {
	return webUIPath(rootURL, "worker-manager/"+pathSegment(scope)+"/purge-cache")
}

func (r *PurgeCacheResource) DetailWebURL(rootURL, id string) string { return "" }
