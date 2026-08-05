package taskcluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/taskcluster/tc-tui/crash"

	tcurls "github.com/taskcluster/taskcluster-lib-urls"
	tcclient "github.com/taskcluster/taskcluster/v101/clients/client-go"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcauth"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcgithub"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tchooks"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcindex"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcpurgecache"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcqueue"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcsecrets"
	"github.com/taskcluster/taskcluster/v101/clients/client-go/tcworkermanager"
)

const PageSize = "150"

// DefaultListLimit is the safe row cap for list fetches that can otherwise
// run to thousands of items (a big task group's tasks, a pool's stopped
// workers, a deep task queue's backlog) — enough to be useful immediately
// without grinding through dozens of pages nobody asked for. Methods taking
// a `limit` parameter treat 0 as "no cap"; see paginateUpTo.
const DefaultListLimit = 1000

type RolesList []tcauth.GetRoleResponse
type WorkerPoolList []tcworkermanager.WorkerPoolFullDefinition
type WorkerList []tcworkermanager.WorkerFullDefinition
type TaskGroupTaskList []tcqueue.TaskDefinitionAndStatus          // ListTaskGroup's members
type PendingTaskList []tcqueue.Var3                               // ListPendingTasks' members
type ClaimedTaskList []tcqueue.Var4                               // ListClaimedTasks' members
type WorkerPoolLaunchConfigList []tcworkermanager.Var1            // ListWorkerPoolLaunchConfigs' members
type WorkerPoolErrorList []tcworkermanager.WorkerPoolError        // ListWorkerPoolErrors' members
type ArtifactList []tcqueue.Artifact                              // ListArtifacts' members
type ClientList []tcauth.GetClientResponse                        // ListClients' members
type PurgeCacheRequestList []tcpurgecache.PurgeCacheRequestsEntry // PurgeRequests' members
type IndexNamespaceList []tcindex.Namespace                       // ListNamespaces' members
type IndexTaskList []tcindex.Task                                 // ListTasks' members
type HookList []tchooks.HookDefinition                            // ListHooks' members, across all hook groups
type HookLastFireList []tchooks.Var                               // ListLastFires' members
type GithubBuildList []tcgithub.Build                             // Builds' members

// GithubBuildFilter's Organization and Repository are always required by
// GetGithubBuilds; exactly one of PullRequest/SHA is required too — see
// resource.GithubBuildsResource's scope grammar, the only caller.
// Organization and Repository must already have '.' translated to '%' by
// the caller (see resource.githubDotsToPercent) — GetGithubBuilds sends
// them to the API verbatim, it does not re-translate.
type GithubBuildFilter struct {
	Organization string
	Repository   string
	PullRequest  string // decimal PR number, as the API expects it
	SHA          string // exactly 40 lowercase hex characters
}

type Taskcluster interface {
	GetVersion() Version
	GetRoot() string
	GetClientID() string

	IsAuthenticated() bool

	GetRoles() (RolesList, error)
	GetRole(roleID string) (*tcauth.GetRoleResponse, error)
	GetWorkerPools() (WorkerPoolList, error)
	GetWorkerPool(workerPoolID string) (*tcworkermanager.WorkerPoolFullDefinition, error)
	GetTaskQueueCounts(workerPoolIDs []string, wanted func(workerPoolID string) bool, onEach func(workerPoolID string, counts TaskQueueCounts))
	GetWorkerPoolErrorCounts() (map[string]int, error)
	GetWorkersForWorkerPool(workerPoolID, launchConfigID, state string, limit int) (WorkerList, bool, error)
	GetWorkerPoolStateCounts(workerPoolID, launchConfigID string) (map[string]int, error)
	GetWorker(workerPoolID, workerGroup, workerID string) (*tcworkermanager.WorkerFullDefinition, error)
	GetWorkerRecentTasks(workerPoolID, workerGroup, workerID string) ([]tcqueue.TaskRun, error)
	GetWorkerPoolLaunchConfigs(workerPoolID string, includeArchived bool) (WorkerPoolLaunchConfigList, error)
	GetWorkerPoolErrors(workerPoolID, launchConfigID string) (WorkerPoolErrorList, error)
	GetWorkerPoolError(workerPoolID, errorID string) (*tcworkermanager.WorkerPoolError, error)
	GetWorkerPoolErrorCount(workerPoolID string) (int, error)

	GetTask(taskID string) (*tcqueue.TaskDefinitionResponse, error)
	CreateTask(taskID string, body json.RawMessage) (*tcqueue.TaskStatusResponse, error)
	CancelTask(taskID string) (*tcqueue.TaskStatusResponse, error)
	RerunTask(taskID string) (*tcqueue.TaskStatusResponse, error)
	ScheduleTask(taskID string) (*tcqueue.TaskStatusResponse, error)
	ChangeTaskPriority(taskID, newPriority string) (*tcqueue.TaskStatusResponse, error)
	GetTaskStatus(taskID string) (*tcqueue.TaskStatusStructure, error)
	GetTaskGroup(taskGroupID string) (*tcqueue.TaskGroupDefinitionResponse, error)
	GetTaskGroupTasks(taskGroupID string, limit int) (TaskGroupTaskList, bool, error)
	GetDependentTasks(taskID string) (TaskGroupTaskList, error)
	GetPendingTasks(taskQueueID string, limit int) (PendingTaskList, bool, error)
	GetClaimedTasks(taskQueueID string, limit int) (ClaimedTaskList, bool, error)
	GetArtifacts(taskID string, runID int64) (ArtifactList, error)
	GetArtifactContent(taskID string, runID int64, name string) (content string, contentType string, truncated bool, err error)
	StreamArtifactContent(taskID string, runID int64, name string, stop <-chan struct{}, onChunk func(chunk []byte)) (contentType string, truncated bool, err error)
	GetArtifactURL(taskID string, runID int64, name string) (string, error)

	GetClients() (ClientList, error)
	GetClient(clientID string) (*tcauth.GetClientResponse, error)

	GetSecrets() ([]string, error)
	GetSecret(name string) (*tcsecrets.Secret, error)

	GetPurgeCacheRequestsForPool(workerPoolID string) (PurgeCacheRequestList, error)

	GetHooks() (HookList, error)
	GetHook(hookGroupID, hookID string) (*tchooks.HookDefinition, error)
	GetHookLastFires(hookGroupID, hookID string) (HookLastFireList, error)

	GetIndexNamespaces(namespace string) (IndexNamespaceList, error)
	GetIndexTasks(namespace string) (IndexTaskList, error)
	FindIndexedTask(indexPath string) (*tcindex.IndexedTaskResponse, error)

	GetGithubBuilds(filter GithubBuildFilter) (GithubBuildList, error)
	GetGithubRepository(owner, repo string) (*tcgithub.RepositoryResponse, error)
}

type TC struct {
	auth       *tcauth.Auth
	wm         *tcworkermanager.WorkerManager
	queue      *tcqueue.Queue
	secrets    *tcsecrets.Secrets
	purgeCache *tcpurgecache.PurgeCache
	index      *tcindex.Index
	hooks      *tchooks.Hooks
	github     *tcgithub.Github

	tcRoot string
}

type Version struct {
	Source  string `json:"source"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Build   string `json:"build"`
}

func NewTaskcluster() Taskcluster {
	tc := &TC{
		auth:       tcauth.NewFromEnv(),
		wm:         tcworkermanager.NewFromEnv(),
		queue:      tcqueue.NewFromEnv(),
		secrets:    tcsecrets.NewFromEnv(),
		purgeCache: tcpurgecache.NewFromEnv(),
		index:      tcindex.NewFromEnv(),
		hooks:      tchooks.NewFromEnv(),
		github:     tcgithub.NewFromEnv(),
	}

	tc.tcRoot = tc.auth.RootURL
	if tc.tcRoot == "" {
		panic("Root URL not defined. export TASKCLUSTER_ROOT_URL=x")
	}

	return tc
}

func (tc *TC) GetClientID() string {
	if tc.auth.Credentials.ClientID != "" {
		return tc.auth.Credentials.ClientID
	}

	return "(anonymous)"
}

func (tc *TC) IsAuthenticated() bool {
	_, err := tc.auth.CurrentScopes()
	return err == nil
}

func (tc *TC) GetVersion() Version {
	versionJson, err := getHttpResponse(tc.tcRoot + "/__version__")
	if err != nil {
		panic(err)
	}
	ver := Version{}
	if err := json.Unmarshal([]byte(versionJson), &ver); err != nil {
		panic(err)
	}

	return ver
}

func (tc *TC) GetRoles() (RolesList, error) {
	roles, err := paginate(func(cont string) ([]tcauth.GetRoleResponse, string, error) {
		resp, err := tc.auth.ListRoles2(cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Roles, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return RolesList(roles), nil
}

func (tc *TC) GetRole(roleID string) (*tcauth.GetRoleResponse, error) {
	return tc.auth.Role(roleID)
}

func (tc *TC) GetWorkerPools() (WorkerPoolList, error) {
	pools, err := paginate(func(cont string) ([]tcworkermanager.WorkerPoolFullDefinition, string, error) {
		resp, err := tc.wm.ListWorkerPools(cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.WorkerPools, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	stats, err := paginate(func(cont string) ([]tcworkermanager.Var3, string, error) {
		resp, err := tc.wm.ListWorkerPoolsStats(cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.WorkerPoolsStats, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	statsByID := make(map[string]tcworkermanager.Var3, len(stats))
	for _, s := range stats {
		statsByID[s.WorkerPoolID] = s
	}

	for i, pool := range pools {
		s, ok := statsByID[pool.WorkerPoolID]
		if !ok {
			continue
		}
		pools[i].CurrentCapacity = s.CurrentCapacity
		pools[i].RequestedCapacity = s.RequestedCapacity
		pools[i].RequestedCount = s.RequestedCount
		pools[i].RunningCapacity = s.RunningCapacity
		pools[i].RunningCount = s.RunningCount
		pools[i].StoppedCapacity = s.StoppedCapacity
		pools[i].StoppedCount = s.StoppedCount
		pools[i].StoppingCapacity = s.StoppingCapacity
		pools[i].StoppingCount = s.StoppingCount
	}

	return WorkerPoolList(pools), nil
}

func (tc *TC) GetWorkerPool(workerPoolID string) (*tcworkermanager.WorkerPoolFullDefinition, error) {
	return tc.wm.WorkerPool(workerPoolID)
}

// TaskQueueCounts holds a task queue's approximate pending/claimed task
// counts. PendingKnown/ClaimedKnown are false when even GetTaskQueueCounts's
// fallback paths (see its doc comment) couldn't obtain that particular
// number — the zero value must not be mistaken for a genuine zero count.
type TaskQueueCounts struct {
	Pending      int64
	PendingKnown bool
	Claimed      int64
	ClaimedKnown bool
}

// GetTaskQueueCounts fetches pending/claimed counts for each of
// workerPoolIDs concurrently, calling onEach exactly once per ID as each
// pool's fetch completes (success, failure, or skipped — see wanted below) —
// a worker pool's ID doubles as its task queue's ID, and there is no bulk
// variant of this endpoint (Taskcluster's own web UI fetches it the same
// way: one call per pool, batched concurrently rather than sequentially).
// onEach is always called exactly once per id, so a caller counting ticks
// against len(workerPoolIDs) always reaches it.
//
// wanted is consulted twice per id: once before it's even queued (skipping
// it entirely, freeing that concurrency slot for one that IS wanted), and
// again right after a slot actually frees up — since with a large
// workerPoolIDs list most ids spend real time queued behind maxConcurrency,
// and wanted's answer may have changed by the time a slot opens (e.g. the
// caller applied a filter while a large batch was still draining). Pass
// `func(string) bool { return true }` to fetch every id unconditionally.
//
// TaskQueueCounts (the combined, ideal call) requires both
// queue:pending-count and queue:claimed-count scopes together, so a
// credential granted only one of the two (as observed with community-tc's
// anonymous role, which grants queue:claimed-list but apparently not
// queue:claimed-count) fails the combined call entirely. Each number then
// falls back independently to an older, more narrowly-scoped call: Pending
// to the deprecated pending-count-only endpoint, Claimed to counting
// GetClaimedTasks's result (the same queue:claimed-list-scoped call the
// existing "claimed" list view already uses successfully) — an approximate
// but perfectly serviceable substitute for a single summary column, given
// currently-claimed tasks are bounded by worker capacity rather than an
// unbounded backlog.
func (tc *TC) GetTaskQueueCounts(workerPoolIDs []string, wanted func(workerPoolID string) bool, onEach func(workerPoolID string, counts TaskQueueCounts)) {
	const maxConcurrency = 24

	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, maxConcurrency)
	)

	for _, id := range workerPoolIDs {
		if !wanted(id) {
			onEach(id, TaskQueueCounts{})
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		crash.Go(func() {
			defer wg.Done()
			defer func() { <-sem }()

			if !wanted(id) {
				onEach(id, TaskQueueCounts{})
				return
			}

			if counts, err := tc.queue.TaskQueueCounts(id); err == nil {
				onEach(id, TaskQueueCounts{
					Pending: counts.PendingTasks, PendingKnown: true,
					Claimed: counts.ClaimedTasks, ClaimedKnown: true,
				})
				return
			}

			var result TaskQueueCounts
			if pending, err := tc.queue.PendingTasks(id); err == nil {
				result.Pending, result.PendingKnown = pending.PendingTasks, true
			}
			if claimed, _, err := tc.GetClaimedTasks(id, 0); err == nil {
				result.Claimed, result.ClaimedKnown = int64(len(claimed)), true
			}
			onEach(id, result)
		})
	}

	wg.Wait()
}

// GetWorkerPoolErrorCounts returns each worker pool's error count over the
// last 7 days in one bulk call (workerPoolErrorStats with no workerPoolId
// filter), keyed by worker pool ID.
func (tc *TC) GetWorkerPoolErrorCounts() (map[string]int, error) {
	stats, err := tc.wm.WorkerPoolErrorStats("")
	if err != nil {
		return nil, err
	}

	var byPool map[string]float64
	if err := json.Unmarshal(stats.Totals.WorkerPool, &byPool); err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(byPool))
	for id, n := range byPool {
		counts[id] = int(n)
	}
	return counts, nil
}

// GetWorkersForWorkerPool lists a pool's workers in one state, fetching at
// most limit rows (0 = all; see paginateUpTo) — a pool can have tens of
// thousands of stopped workers, so callers pass DefaultListLimit unless the
// user explicitly asked for everything.
func (tc *TC) GetWorkersForWorkerPool(workerPoolID, launchConfigID, state string, limit int) (WorkerList, bool, error) {
	workers, truncated, err := paginateUpTo(limit, func(cont string) ([]tcworkermanager.WorkerFullDefinition, string, error) {
		resp, err := tc.wm.ListWorkersForWorkerPool(workerPoolID, cont, launchConfigID, PageSize, state)
		if err != nil {
			return nil, "", err
		}
		return resp.Workers, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, false, err
	}

	return WorkerList(workers), truncated, nil
}

// GetWorkerPoolStateCounts returns worker counts by state for one pool. With
// launchConfigID empty, counts are summed across every launch configuration;
// otherwise only the matching launch configuration's counts are returned. It
// calls the lightweight worker-pool stats endpoint — no individual worker
// rows are fetched either way.
func (tc *TC) GetWorkerPoolStateCounts(workerPoolID, launchConfigID string) (map[string]int, error) {
	stats, err := tc.wm.WorkerPoolStats(workerPoolID)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{"requested": 0, "running": 0, "stopping": 0, "stopped": 0}
	for _, lc := range stats.LaunchConfigStats {
		if launchConfigID != "" && lc.LaunchConfigID != launchConfigID {
			continue
		}
		counts["requested"] += int(lc.RequestedCount)
		counts["running"] += int(lc.RunningCount)
		counts["stopping"] += int(lc.StoppingCount)
		counts["stopped"] += int(lc.StoppedCount)
	}

	return counts, nil
}

func (tc *TC) GetWorker(workerPoolID, workerGroup, workerID string) (*tcworkermanager.WorkerFullDefinition, error) {
	return tc.wm.Worker(workerPoolID, workerGroup, workerID)
}

// GetWorkerRecentTasks returns the up-to-20 most recent tasks claimed by a
// worker, via the Queue service's own worker record (distinct from
// worker-manager's — this is the only place recentTasks is exposed).
// workerPoolID is split on "/" into the provisionerId/workerType pair the
// Queue API still expects.
func (tc *TC) GetWorkerRecentTasks(workerPoolID, workerGroup, workerID string) ([]tcqueue.TaskRun, error) {
	parts := strings.SplitN(workerPoolID, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid worker pool id %q", workerPoolID)
	}

	resp, err := tc.queue.GetWorker(parts[0], parts[1], workerGroup, workerID)
	if err != nil {
		return nil, err
	}

	return resp.RecentTasks, nil
}

// GetWorkerPoolLaunchConfigs lists a worker pool's launch configurations.
// With includeArchived false, only active (non-archived) configs are
// returned — matches the API's own default.
func (tc *TC) GetWorkerPoolLaunchConfigs(workerPoolID string, includeArchived bool) (WorkerPoolLaunchConfigList, error) {
	archived := "false"
	if includeArchived {
		archived = "true"
	}

	configs, err := paginate(func(cont string) ([]tcworkermanager.Var1, string, error) {
		resp, err := tc.wm.ListWorkerPoolLaunchConfigs(workerPoolID, cont, archived, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.WorkerPoolLaunchConfigs, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return WorkerPoolLaunchConfigList(configs), nil
}

// GetWorkerPoolErrors lists provisioning errors reported for a worker pool.
// With launchConfigID empty, every launch configuration's errors are
// returned; otherwise only errors reported against that launch configuration.
func (tc *TC) GetWorkerPoolErrors(workerPoolID, launchConfigID string) (WorkerPoolErrorList, error) {
	errs, err := paginate(func(cont string) ([]tcworkermanager.WorkerPoolError, string, error) {
		resp, err := tc.wm.ListWorkerPoolErrors(workerPoolID, cont, "" /* errorId */, launchConfigID, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.WorkerPoolErrors, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return WorkerPoolErrorList(errs), nil
}

// GetWorkerPoolError fetches a single worker pool error by ID, using the
// API's own errorId filter rather than listing and searching client-side.
func (tc *TC) GetWorkerPoolError(workerPoolID, errorID string) (*tcworkermanager.WorkerPoolError, error) {
	resp, err := tc.wm.ListWorkerPoolErrors(workerPoolID, "", errorID, "", "1")
	if err != nil {
		return nil, err
	}
	if len(resp.WorkerPoolErrors) == 0 {
		return nil, fmt.Errorf("worker pool error %q not found in worker pool %q", errorID, workerPoolID)
	}

	return &resp.WorkerPoolErrors[0], nil
}

// GetWorkerPoolErrorCount returns the total number of provisioning errors
// reported for a worker pool over worker-manager's fixed lookback window
// (roughly the last 7 days), via the dedicated stats endpoint — no error
// rows are fetched.
func (tc *TC) GetWorkerPoolErrorCount(workerPoolID string) (int, error) {
	stats, err := tc.wm.WorkerPoolErrorStats(workerPoolID)
	if err != nil {
		return 0, err
	}

	return int(stats.Totals.Total), nil
}

func (tc *TC) GetTask(taskID string) (*tcqueue.TaskDefinitionResponse, error) {
	return tc.queue.Task(taskID)
}

// CreateTask submits a new task definition under taskID (a slugid the caller
// generates). Queue.createTask is idempotent, so a retry with the same taskID
// and payload is safe.
//
// The body is sent verbatim rather than round-tripped through
// tcqueue.TaskDefinitionRequest: that struct marks fields like retries
// `omitempty`, which would silently drop an explicit `retries: 0` and let the
// queue apply its default of 5. A json.RawMessage marshals to itself, so
// APICall forwards exactly the JSON the user submitted.
func (tc *TC) CreateTask(taskID string, body json.RawMessage) (*tcqueue.TaskStatusResponse, error) {
	cd := tcclient.Client(*tc.queue)
	resp, _, err := (&cd).APICall(body, "PUT", "/task/"+url.PathEscape(taskID), new(tcqueue.TaskStatusResponse), nil)
	if err != nil {
		return nil, err
	}
	return resp.(*tcqueue.TaskStatusResponse), nil
}

// CancelTask cancels an unscheduled/pending/running task, resolving its current
// run as an exception with reasonResolved "canceled". Idempotent: cancelling an
// already-resolved task just returns its current status.
func (tc *TC) CancelTask(taskID string) (*tcqueue.TaskStatusResponse, error) {
	return tc.queue.CancelTask(taskID)
}

// RerunTask reruns a previously resolved task under the same taskID, resetting
// its retries. Idempotent for a pending/running task.
func (tc *TC) RerunTask(taskID string) (*tcqueue.TaskStatusResponse, error) {
	return tc.queue.RerunTask(taskID)
}

// ScheduleTask schedules an unscheduled task even if its dependencies are
// unresolved. Idempotent.
func (tc *TC) ScheduleTask(taskID string) (*tcqueue.TaskStatusResponse, error) {
	return tc.queue.ScheduleTask(taskID)
}

// ChangeTaskPriority updates an unresolved task's priority. A claimed/running
// run keeps its current priority until retried. The queue endpoint is marked
// experimental upstream.
func (tc *TC) ChangeTaskPriority(taskID, newPriority string) (*tcqueue.TaskStatusResponse, error) {
	return tc.queue.ChangeTaskPriority(taskID, &tcqueue.ChangeTaskPriorityRequest{NewPriority: newPriority})
}

func (tc *TC) GetTaskStatus(taskID string) (*tcqueue.TaskStatusStructure, error) {
	resp, err := tc.queue.Status(taskID)
	if err != nil {
		return nil, err
	}
	return &resp.Status, nil
}

func (tc *TC) GetTaskGroup(taskGroupID string) (*tcqueue.TaskGroupDefinitionResponse, error) {
	return tc.queue.GetTaskGroup(taskGroupID)
}

// GetTaskGroupTasks lists a task group's tasks, fetching at most limit rows
// (0 = all; see paginateUpTo) — a big group can hold thousands.
func (tc *TC) GetTaskGroupTasks(taskGroupID string, limit int) (TaskGroupTaskList, bool, error) {
	tasks, truncated, err := paginateUpTo(limit, func(cont string) ([]tcqueue.TaskDefinitionAndStatus, string, error) {
		resp, err := tc.queue.ListTaskGroup(taskGroupID, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Tasks, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, false, err
	}

	return TaskGroupTaskList(tasks), truncated, nil
}

// GetDependentTasks lists tasks that declare taskID as one of their
// dependencies — the reverse of a task's own Dependencies field.
func (tc *TC) GetDependentTasks(taskID string) (TaskGroupTaskList, error) {
	tasks, err := paginate(func(cont string) ([]tcqueue.TaskDefinitionAndStatus, string, error) {
		resp, err := tc.queue.ListDependentTasks(taskID, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Tasks, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return TaskGroupTaskList(tasks), nil
}

// GetPendingTasks lists a task queue's pending backlog, fetching at most
// limit rows (0 = all; see paginateUpTo) — a backed-up queue is unbounded.
func (tc *TC) GetPendingTasks(taskQueueID string, limit int) (PendingTaskList, bool, error) {
	tasks, truncated, err := paginateUpTo(limit, func(cont string) ([]tcqueue.Var3, string, error) {
		resp, err := tc.queue.ListPendingTasks(taskQueueID, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Tasks, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, false, err
	}

	return PendingTaskList(tasks), truncated, nil
}

// GetClaimedTasks lists a task queue's currently-claimed tasks, fetching at
// most limit rows (0 = all; see paginateUpTo).
func (tc *TC) GetClaimedTasks(taskQueueID string, limit int) (ClaimedTaskList, bool, error) {
	tasks, truncated, err := paginateUpTo(limit, func(cont string) ([]tcqueue.Var4, string, error) {
		resp, err := tc.queue.ListClaimedTasks(taskQueueID, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Tasks, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, false, err
	}

	return ClaimedTaskList(tasks), truncated, nil
}

// GetArtifacts lists the artifacts produced by one run of a task.
func (tc *TC) GetArtifacts(taskID string, runID int64) (ArtifactList, error) {
	runIDStr := strconv.FormatInt(runID, 10)

	artifacts, err := paginate(func(cont string) ([]tcqueue.Artifact, string, error) {
		resp, err := tc.queue.ListArtifacts(taskID, runIDStr, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Artifacts, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return ArtifactList(artifacts), nil
}

// MaxArtifactContentBytes caps how much of an artifact's content
// GetArtifactContent reads into memory — a build log can run to hundreds of
// MB, and reading it in full just to render a tail-truncated preview would
// both waste bandwidth and risk exhausting memory. GetArtifactContent
// reports via its truncated return value when this cap was hit.
const MaxArtifactContentBytes = 20 * 1024 * 1024 // 20 MiB

// artifactURL builds the URL to fetch or link to one artifact's content —
// signed when actual credentials are configured, matching the Taskcluster
// web UI's own getArtifactUrl (buildSignedUrlSync only when user.credentials
// is set, buildUrl otherwise). IsAuthenticated can't be used for this check:
// it tests whether currentScopes succeeds, which it does even anonymously
// (the anonymous role's own scopes), whereas signing with an empty client
// ID/access token produces a bewit missing its id/mac fields, which the
// queue rejects with "Missing bewit attributes" rather than falling back to
// anonymous access.
func (tc *TC) artifactURL(taskID string, runID int64, name string, duration time.Duration) (string, error) {
	runIDStr := strconv.FormatInt(runID, 10)

	if tc.auth.Credentials.ClientID != "" {
		signedURL, err := tc.queue.GetArtifact_SignedURL(taskID, runIDStr, name, duration)
		if err != nil {
			return "", err
		}
		return signedURL.String(), nil
	}

	route := fmt.Sprintf("task/%s/runs/%s/artifacts/%s", url.PathEscape(taskID), url.PathEscape(runIDStr), url.PathEscape(name))
	return tcurls.API(tc.tcRoot, "queue", "v1", route), nil
}

// GetArtifactContent fetches one artifact's content, capped at
// MaxArtifactContentBytes (see its doc comment) — truncated reports whether
// the cap was hit. The queue's "artifact" endpoint responds with either the
// content itself or a redirect to it depending on storage type; a plain
// http.Get follows that redirect automatically, so no response-body parsing
// is needed either way. contentType is the fetch response's own Content-Type
// header, letting callers render markdown/JSON/YAML artifacts specially
// without a second API call.
func (tc *TC) GetArtifactContent(taskID string, runID int64, name string) (content string, contentType string, truncated bool, err error) {
	fetchURL, err := tc.artifactURL(taskID, runID, name, 60*time.Second)
	if err != nil {
		return "", "", false, err
	}

	data, contentType, truncated, err := getHttpResponseCapped(fetchURL, MaxArtifactContentBytes)
	if err != nil {
		return "", "", false, err
	}

	return string(data), contentType, truncated, nil
}

// GetArtifactURL returns a URL suitable for opening or downloading one
// artifact directly (e.g. via a browser) — signed when authenticated, with a
// longer lifetime than GetArtifactContent's own fetch since there's a delay
// between generating it and the user actually loading it.
func (tc *TC) GetArtifactURL(taskID string, runID int64, name string) (string, error) {
	return tc.artifactURL(taskID, runID, name, 5*time.Minute)
}

// GetClients lists every auth client (credential) in the deployment.
func (tc *TC) GetClients() (ClientList, error) {
	clients, err := paginate(func(cont string) ([]tcauth.GetClientResponse, string, error) {
		resp, err := tc.auth.ListClients(cont, PageSize, "")
		if err != nil {
			return nil, "", err
		}
		return resp.Clients, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return ClientList(clients), nil
}

func (tc *TC) GetClient(clientID string) (*tcauth.GetClientResponse, error) {
	return tc.auth.Client(clientID)
}

// GetSecrets lists every secret's name — the bulk list API never returns
// values, only names, so there's no need for a wrapper type beyond []string.
func (tc *TC) GetSecrets() ([]string, error) {
	names, err := paginate(func(cont string) ([]string, string, error) {
		resp, err := tc.secrets.List(cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Secrets, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return names, nil
}

func (tc *TC) GetSecret(name string) (*tcsecrets.Secret, error) {
	return tc.secrets.Get(name)
}

// GetPurgeCacheRequestsForPool lists open purge-cache requests for one
// worker pool. Unlike AllPurgeRequests, this endpoint has no continuation
// token — a single call returns everything.
func (tc *TC) GetPurgeCacheRequestsForPool(workerPoolID string) (PurgeCacheRequestList, error) {
	resp, err := tc.purgeCache.PurgeRequests(workerPoolID, "")
	if err != nil {
		return nil, err
	}

	return PurgeCacheRequestList(resp.Requests), nil
}

// GetHooks lists every hook in the deployment, across all hook groups —
// there is no single list-everything endpoint, so this walks listHookGroups
// and then listHooks per group, matching how the web UI's own hooks page
// assembles its view. Neither endpoint paginates.
func (tc *TC) GetHooks() (HookList, error) {
	groups, err := tc.hooks.ListHookGroups()
	if err != nil {
		return nil, err
	}

	var all HookList
	for _, group := range groups.Groups {
		resp, err := tc.hooks.ListHooks(group)
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Hooks...)
	}

	return all, nil
}

func (tc *TC) GetHook(hookGroupID, hookID string) (*tchooks.HookDefinition, error) {
	return tc.hooks.Hook(hookGroupID, hookID)
}

// GetHookLastFires lists the most recent attempts to fire a hook (each with
// the task it created, or the error that prevented one). A hook that has
// never fired makes listLastFires respond 404 ("No such hook or never
// fired") rather than an empty list — reported here as an empty list, since
// the hook itself is known to exist (callers fetch it first) and "never
// fired" is an ordinary state, not a failure.
func (tc *TC) GetHookLastFires(hookGroupID, hookID string) (HookLastFireList, error) {
	fires, err := paginate(func(cont string) ([]tchooks.Var, string, error) {
		resp, err := tc.hooks.ListLastFires(hookGroupID, hookID, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.LastFires, resp.ContinuationToken, nil
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return HookLastFireList(fires), nil
}

func (tc *TC) GetIndexNamespaces(namespace string) (IndexNamespaceList, error) {
	namespaces, err := paginate(func(cont string) ([]tcindex.Namespace, string, error) {
		resp, err := tc.index.ListNamespaces(namespace, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Namespaces, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return IndexNamespaceList(namespaces), nil
}

func (tc *TC) GetIndexTasks(namespace string) (IndexTaskList, error) {
	tasks, err := paginate(func(cont string) ([]tcindex.Task, string, error) {
		resp, err := tc.index.ListTasks(namespace, cont, PageSize)
		if err != nil {
			return nil, "", err
		}
		return resp.Tasks, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return IndexTaskList(tasks), nil
}

// FindIndexedTask resolves an exact, full index path to its currently
// indexed task. A 404 (no task indexed at this exact path) is reported as
// (nil, nil) rather than an error — callers use this to distinguish "this
// looks like a namespace to browse, not a leaf path" from a real fetch
// failure.
func (tc *TC) FindIndexedTask(indexPath string) (*tcindex.IndexedTaskResponse, error) {
	task, err := tc.index.FindTask(indexPath)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return task, nil
}

// GetGithubBuilds returns every build matching filter — unbounded (see
// paginate, not paginateUpTo): scoping to one PR or one SHA keeps the
// result set naturally small (a handful of rows per push/sync event),
// unlike an org- or repo-wide fetch, which the Github service's
// oldest-updated-first-only ordering (no descending/keyset option) makes
// unsafe to page through with a cap — a capped fetch would silently return
// the OLDEST N builds, never recent ones. This resource deliberately never
// fetches unscoped.
func (tc *TC) GetGithubBuilds(filter GithubBuildFilter) (GithubBuildList, error) {
	builds, err := paginate(func(cont string) ([]tcgithub.Build, string, error) {
		resp, err := tc.github.Builds(cont, PageSize, filter.Organization, filter.PullRequest, filter.Repository, filter.SHA)
		if err != nil {
			return nil, "", err
		}
		return resp.Builds, resp.ContinuationToken, nil
	})
	if err != nil {
		return nil, err
	}

	return GithubBuildList(builds), nil
}

// GetGithubRepository reports a single repo's Taskcluster-Github install
// status. Stability: EXPERIMENTAL upstream (surfaced to the user as a
// caveat in resource.GithubRepositoryResource's Detail body, not hidden
// here). owner/repo are sent verbatim — unlike GetGithubBuilds, this
// endpoint takes the real Github name, not a sanitized/stored field.
func (tc *TC) GetGithubRepository(owner, repo string) (*tcgithub.RepositoryResponse, error) {
	return tc.github.Repository(owner, repo)
}

// isNotFound reports whether err is a Taskcluster API error with a 404
// response — used by FindIndexedTask to treat "not found" as an expected
// outcome rather than a failure.
func isNotFound(err error) bool {
	var apiErr *tcclient.APICallException
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.CallSummary != nil && apiErr.CallSummary.HTTPResponse != nil &&
		apiErr.CallSummary.HTTPResponse.StatusCode == 404
}

func (tc *TC) GetRoot() string {
	return tc.tcRoot
}

// getHttpResponseCapped fetches url, reading at most maxBytes of the
// response body — truncated reports whether the body was longer than that.
// contentType is the response's own Content-Type header.
func getHttpResponseCapped(url string, maxBytes int64) (content []byte, contentType string, truncated bool, err error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, "", false, err
	}
	defer response.Body.Close()

	data, err := ioutil.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, "", false, err
	}

	contentType = response.Header.Get("Content-Type")
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], contentType, true, nil
	}
	return data, contentType, false, nil
}

func getHttpResponse(url string) (string, error) {
	response, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	return string(contents), nil
}
