package resource

import (
	"encoding/json"
	"regexp"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcauth"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcgithub"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tchooks"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcindex"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcsecrets"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcworkermanager"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// regionTagRe matches tview's `[tag]` region/color markup (e.g. "[green]",
// "[#dddddd:]", "[-:-:-]").
var regionTagRe = regexp.MustCompile(`\[[^][]*\]`)

// stripRegionTags removes tview region/color tags from s. Markdown-rendered
// body text (see renderMarkdown/renderGlamour in render.go) can interleave
// style tags mid-phrase — e.g. glamour periodically re-asserts the same
// color with no visible effect — which breaks naive multi-word
// strings.Contains checks against the raw body. Tests that need to assert on
// rendered prose should strip tags first rather than relying on markup-free
// contiguous substrings.
func stripRegionTags(s string) string {
	return regionTagRe.ReplaceAllString(s, "")
}

type fakeTaskcluster struct {
	roles    taskcluster.RolesList
	rolesErr error
	role     *tcauth.GetRoleResponse
	roleErr  error

	workerPools    taskcluster.WorkerPoolList
	workerPoolsErr error
	workerPool     *tcworkermanager.WorkerPoolFullDefinition
	workerPoolErr  error

	taskQueueCounts    map[string]taskcluster.TaskQueueCounts
	taskQueueCountsErr error

	workerPoolErrorCounts    map[string]int
	workerPoolErrorCountsErr error

	workers               taskcluster.WorkerList
	workersErr            error
	workersState          string // last `state` param GetWorkersForWorkerPool was called with
	workersLaunchConfigID string // last `launchConfigId` param GetWorkersForWorkerPool was called with
	workersLimit          int    // last `limit` param GetWorkersForWorkerPool was called with
	workersTruncated      bool   // truncated value GetWorkersForWorkerPool returns

	stateCounts               map[string]int
	stateCountsErr            error
	stateCountsLaunchConfigID string // last `launchConfigId` param GetWorkerPoolStateCounts was called with

	worker    *tcworkermanager.WorkerFullDefinition
	workerErr error

	workerRecentTasks    []tcqueue.TaskRun
	workerRecentTasksErr error

	launchConfigs                taskcluster.WorkerPoolLaunchConfigList
	launchConfigsErr             error
	launchConfigsIncludeArchived bool // last `includeArchived` param GetWorkerPoolLaunchConfigs was called with

	workerPoolErrors               taskcluster.WorkerPoolErrorList
	workerPoolErrorsErr            error
	workerPoolErrorsLaunchConfigID string // last `launchConfigId` param GetWorkerPoolErrors was called with

	workerPoolError    *tcworkermanager.WorkerPoolError
	workerPoolErrorErr error

	errorCount    int
	errorCountErr error

	task    *tcqueue.TaskDefinitionResponse
	taskErr error

	createTaskResp *tcqueue.TaskStatusResponse
	createTaskErr  error
	createTaskID   string          // last taskID CreateTask was called with
	createTaskBody json.RawMessage // last raw definition body CreateTask was called with
	createTaskN    int             // number of times CreateTask was called

	cancelTaskID   string // last taskID CancelTask was called with
	cancelTaskResp *tcqueue.TaskStatusResponse
	cancelTaskErr  error

	rerunTaskID   string // last taskID RerunTask was called with
	rerunTaskResp *tcqueue.TaskStatusResponse
	rerunTaskErr  error

	scheduleTaskID   string // last taskID ScheduleTask was called with
	scheduleTaskResp *tcqueue.TaskStatusResponse
	scheduleTaskErr  error

	changePriorityID    string // last taskID ChangeTaskPriority was called with
	changePriorityValue string // last newPriority ChangeTaskPriority was called with
	changePriorityResp  *tcqueue.TaskStatusResponse
	changePriorityErr   error

	taskStatus    *tcqueue.TaskStatusStructure
	taskStatusErr error

	taskGroup    *tcqueue.TaskGroupDefinitionResponse
	taskGroupErr error

	taskGroupTasks          taskcluster.TaskGroupTaskList
	taskGroupTasksErr       error
	taskGroupTasksLimit     int  // last `limit` param GetTaskGroupTasks was called with
	taskGroupTasksTruncated bool // truncated value GetTaskGroupTasks returns

	dependentTasks    taskcluster.TaskGroupTaskList
	dependentTasksErr error

	pendingTasks          taskcluster.PendingTaskList
	pendingTasksErr       error
	pendingTasksLimit     int  // last `limit` param GetPendingTasks was called with
	pendingTasksTruncated bool // truncated value GetPendingTasks returns

	claimedTasks          taskcluster.ClaimedTaskList
	claimedTasksErr       error
	claimedTasksLimit     int  // last `limit` param GetClaimedTasks was called with
	claimedTasksTruncated bool // truncated value GetClaimedTasks returns

	artifacts    taskcluster.ArtifactList
	artifactsErr error

	artifactContent     string
	artifactContentType string
	artifactTruncated   bool
	artifactContentErr  error

	artifactURL    string
	artifactURLErr error

	streamChunks      [][]byte
	streamContentType string
	streamTruncated   bool
	streamErr         error

	clients    taskcluster.ClientList
	clientsErr error
	client     *tcauth.GetClientResponse
	clientErr  error

	secrets    []string
	secretsErr error
	secret     *tcsecrets.Secret
	secretErr  error

	purgeCacheRequestsForPool    taskcluster.PurgeCacheRequestList
	purgeCacheRequestsForPoolErr error

	hooks            taskcluster.HookList
	hooksErr         error
	hook             *tchooks.HookDefinition
	hookErr          error
	hookLastFires    taskcluster.HookLastFireList
	hookLastFiresErr error

	indexNamespaces    taskcluster.IndexNamespaceList
	indexNamespacesErr error
	indexNamespacesArg string // last namespace param GetIndexNamespaces was called with
	indexTasks         taskcluster.IndexTaskList
	indexTasksErr      error
	indexTasksArg      string // last namespace param GetIndexTasks was called with
	findIndexedTask    *tcindex.IndexedTaskResponse
	findIndexedTaskErr error

	githubBuilds       taskcluster.GithubBuildList
	githubBuildsErr    error
	githubBuildsFilter taskcluster.GithubBuildFilter // last filter GetGithubBuilds was called with

	githubRepository      *tcgithub.RepositoryResponse
	githubRepositoryErr   error
	githubRepositoryOwner string // last owner param GetGithubRepository was called with
	githubRepositoryRepo  string // last repo param GetGithubRepository was called with
}

func (f *fakeTaskcluster) GetVersion() taskcluster.Version { return taskcluster.Version{} }
func (f *fakeTaskcluster) GetRoot() string                 { return "" }
func (f *fakeTaskcluster) GetClientID() string             { return "" }
func (f *fakeTaskcluster) IsAuthenticated() bool           { return false }

func (f *fakeTaskcluster) GetRoles() (taskcluster.RolesList, error) {
	return f.roles, f.rolesErr
}

func (f *fakeTaskcluster) GetRole(roleID string) (*tcauth.GetRoleResponse, error) {
	return f.role, f.roleErr
}

func (f *fakeTaskcluster) GetWorkerPools() (taskcluster.WorkerPoolList, error) {
	return f.workerPools, f.workerPoolsErr
}

func (f *fakeTaskcluster) GetWorkerPool(workerPoolID string) (*tcworkermanager.WorkerPoolFullDefinition, error) {
	return f.workerPool, f.workerPoolErr
}

// GetTaskQueueCounts calls onEach once per ID, in order, with whatever
// f.taskQueueCounts holds for that ID (the zero value if absent or if
// wanted rejects it) — no goroutines, so tests calling it don't need to
// synchronize — and returns f.taskQueueCountsErr, standing in for the real
// client's give-up-entirely case (a scope denial).
func (f *fakeTaskcluster) GetTaskQueueCounts(workerPoolIDs []string, wanted func(workerPoolID string) bool, onEach func(workerPoolID string, counts taskcluster.TaskQueueCounts)) error {
	for _, id := range workerPoolIDs {
		if !wanted(id) {
			onEach(id, taskcluster.TaskQueueCounts{})
			continue
		}
		onEach(id, f.taskQueueCounts[id])
	}
	return f.taskQueueCountsErr
}

func (f *fakeTaskcluster) GetWorkerPoolErrorCounts() (map[string]int, error) {
	return f.workerPoolErrorCounts, f.workerPoolErrorCountsErr
}

func (f *fakeTaskcluster) GetWorkersForWorkerPool(workerPoolID, launchConfigID, state string, limit int) (taskcluster.WorkerList, bool, error) {
	f.workersState = state
	f.workersLaunchConfigID = launchConfigID
	f.workersLimit = limit
	return f.workers, f.workersTruncated, f.workersErr
}

func (f *fakeTaskcluster) GetWorkerPoolStateCounts(workerPoolID, launchConfigID string) (map[string]int, error) {
	f.stateCountsLaunchConfigID = launchConfigID
	return f.stateCounts, f.stateCountsErr
}

func (f *fakeTaskcluster) GetWorker(workerPoolID, workerGroup, workerID string) (*tcworkermanager.WorkerFullDefinition, error) {
	return f.worker, f.workerErr
}

func (f *fakeTaskcluster) GetWorkerRecentTasks(workerPoolID, workerGroup, workerID string) ([]tcqueue.TaskRun, error) {
	return f.workerRecentTasks, f.workerRecentTasksErr
}

func (f *fakeTaskcluster) GetWorkerPoolLaunchConfigs(workerPoolID string, includeArchived bool) (taskcluster.WorkerPoolLaunchConfigList, error) {
	f.launchConfigsIncludeArchived = includeArchived
	return f.launchConfigs, f.launchConfigsErr
}

func (f *fakeTaskcluster) GetWorkerPoolErrors(workerPoolID, launchConfigID string) (taskcluster.WorkerPoolErrorList, error) {
	f.workerPoolErrorsLaunchConfigID = launchConfigID
	return f.workerPoolErrors, f.workerPoolErrorsErr
}

func (f *fakeTaskcluster) GetWorkerPoolError(workerPoolID, errorID string) (*tcworkermanager.WorkerPoolError, error) {
	return f.workerPoolError, f.workerPoolErrorErr
}

func (f *fakeTaskcluster) GetWorkerPoolErrorCount(workerPoolID string) (int, error) {
	return f.errorCount, f.errorCountErr
}

func (f *fakeTaskcluster) GetTask(taskID string) (*tcqueue.TaskDefinitionResponse, error) {
	return f.task, f.taskErr
}

func (f *fakeTaskcluster) CreateTask(taskID string, body json.RawMessage) (*tcqueue.TaskStatusResponse, error) {
	f.createTaskID = taskID
	f.createTaskBody = body
	f.createTaskN++
	return f.createTaskResp, f.createTaskErr
}

func (f *fakeTaskcluster) CancelTask(taskID string) (*tcqueue.TaskStatusResponse, error) {
	f.cancelTaskID = taskID
	return f.cancelTaskResp, f.cancelTaskErr
}

func (f *fakeTaskcluster) RerunTask(taskID string) (*tcqueue.TaskStatusResponse, error) {
	f.rerunTaskID = taskID
	return f.rerunTaskResp, f.rerunTaskErr
}

func (f *fakeTaskcluster) ScheduleTask(taskID string) (*tcqueue.TaskStatusResponse, error) {
	f.scheduleTaskID = taskID
	return f.scheduleTaskResp, f.scheduleTaskErr
}

func (f *fakeTaskcluster) ChangeTaskPriority(taskID, newPriority string) (*tcqueue.TaskStatusResponse, error) {
	f.changePriorityID = taskID
	f.changePriorityValue = newPriority
	return f.changePriorityResp, f.changePriorityErr
}

func (f *fakeTaskcluster) GetTaskStatus(taskID string) (*tcqueue.TaskStatusStructure, error) {
	return f.taskStatus, f.taskStatusErr
}

func (f *fakeTaskcluster) GetTaskGroup(taskGroupID string) (*tcqueue.TaskGroupDefinitionResponse, error) {
	return f.taskGroup, f.taskGroupErr
}

func (f *fakeTaskcluster) GetTaskGroupTasks(taskGroupID string, limit int) (taskcluster.TaskGroupTaskList, bool, error) {
	f.taskGroupTasksLimit = limit
	return f.taskGroupTasks, f.taskGroupTasksTruncated, f.taskGroupTasksErr
}

func (f *fakeTaskcluster) GetDependentTasks(taskID string) (taskcluster.TaskGroupTaskList, error) {
	return f.dependentTasks, f.dependentTasksErr
}

func (f *fakeTaskcluster) GetPendingTasks(taskQueueID string, limit int) (taskcluster.PendingTaskList, bool, error) {
	f.pendingTasksLimit = limit
	return f.pendingTasks, f.pendingTasksTruncated, f.pendingTasksErr
}

func (f *fakeTaskcluster) GetClaimedTasks(taskQueueID string, limit int) (taskcluster.ClaimedTaskList, bool, error) {
	f.claimedTasksLimit = limit
	return f.claimedTasks, f.claimedTasksTruncated, f.claimedTasksErr
}

func (f *fakeTaskcluster) GetArtifacts(taskID string, runID int64) (taskcluster.ArtifactList, error) {
	return f.artifacts, f.artifactsErr
}

func (f *fakeTaskcluster) GetArtifactContent(taskID string, runID int64, name string) (string, string, bool, error) {
	return f.artifactContent, f.artifactContentType, f.artifactTruncated, f.artifactContentErr
}

// StreamArtifactContent delivers f.streamChunks one onChunk call each, then
// returns — synchronously, so tests need no coordination. stop is ignored;
// tests exercising interruption drive it at the shell layer instead.
func (f *fakeTaskcluster) StreamArtifactContent(taskID string, runID int64, name string, stop <-chan struct{}, onChunk func(chunk []byte)) (string, bool, error) {
	if f.streamErr != nil {
		return "", false, f.streamErr
	}
	for _, chunk := range f.streamChunks {
		onChunk(chunk)
	}
	return f.streamContentType, f.streamTruncated, nil
}

func (f *fakeTaskcluster) GetArtifactURL(taskID string, runID int64, name string) (string, error) {
	return f.artifactURL, f.artifactURLErr
}

func (f *fakeTaskcluster) GetClients() (taskcluster.ClientList, error) {
	return f.clients, f.clientsErr
}

func (f *fakeTaskcluster) GetClient(clientID string) (*tcauth.GetClientResponse, error) {
	return f.client, f.clientErr
}

func (f *fakeTaskcluster) GetSecrets() ([]string, error) {
	return f.secrets, f.secretsErr
}

func (f *fakeTaskcluster) GetSecret(name string) (*tcsecrets.Secret, error) {
	return f.secret, f.secretErr
}

func (f *fakeTaskcluster) GetPurgeCacheRequestsForPool(workerPoolID string) (taskcluster.PurgeCacheRequestList, error) {
	return f.purgeCacheRequestsForPool, f.purgeCacheRequestsForPoolErr
}

func (f *fakeTaskcluster) GetHooks() (taskcluster.HookList, error) {
	return f.hooks, f.hooksErr
}

func (f *fakeTaskcluster) GetHook(hookGroupID, hookID string) (*tchooks.HookDefinition, error) {
	return f.hook, f.hookErr
}

func (f *fakeTaskcluster) GetHookLastFires(hookGroupID, hookID string) (taskcluster.HookLastFireList, error) {
	return f.hookLastFires, f.hookLastFiresErr
}

func (f *fakeTaskcluster) GetIndexNamespaces(namespace string) (taskcluster.IndexNamespaceList, error) {
	f.indexNamespacesArg = namespace
	return f.indexNamespaces, f.indexNamespacesErr
}

func (f *fakeTaskcluster) GetIndexTasks(namespace string) (taskcluster.IndexTaskList, error) {
	f.indexTasksArg = namespace
	return f.indexTasks, f.indexTasksErr
}

func (f *fakeTaskcluster) FindIndexedTask(indexPath string) (*tcindex.IndexedTaskResponse, error) {
	return f.findIndexedTask, f.findIndexedTaskErr
}

func (f *fakeTaskcluster) GetGithubBuilds(filter taskcluster.GithubBuildFilter) (taskcluster.GithubBuildList, error) {
	f.githubBuildsFilter = filter
	return f.githubBuilds, f.githubBuildsErr
}

func (f *fakeTaskcluster) GetGithubRepository(owner, repo string) (*tcgithub.RepositoryResponse, error) {
	f.githubRepositoryOwner = owner
	f.githubRepositoryRepo = repo
	return f.githubRepository, f.githubRepositoryErr
}
