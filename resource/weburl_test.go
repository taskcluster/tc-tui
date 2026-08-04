package resource

import "testing"

const testRootURL = "https://stage.taskcluster.nonprod.cloudops.mozgcp.net"

func TestRolesResourceWebURL(t *testing.T) {
	res := NewRolesResource(&fakeTaskcluster{})

	if got, want := res.ListWebURL(testRootURL, ""), testRootURL+"/auth/roles"; got != want {
		t.Errorf("ListWebURL = %q, want %q", got, want)
	}
	if got, want := res.DetailWebURL(testRootURL, "hook-id:foo"), testRootURL+"/auth/roles/hook-id:foo"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestWorkerPoolsResourceWebURL(t *testing.T) {
	res := NewWorkerPoolsResource(&fakeTaskcluster{})

	if got, want := res.ListWebURL(testRootURL, ""), testRootURL+"/worker-manager"; got != want {
		t.Errorf("ListWebURL = %q, want %q", got, want)
	}
	if got, want := res.DetailWebURL(testRootURL, "gecko-3/b-linux"), testRootURL+"/worker-manager/gecko-3%2Fb-linux"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestWorkersResourceWebURL(t *testing.T) {
	res := NewWorkersResource(&fakeTaskcluster{})

	if got, want := res.ListWebURL(testRootURL, "gecko-3/b-linux"), testRootURL+"/worker-manager/gecko-3%2Fb-linux/workers"; got != want {
		t.Errorf("ListWebURL(pool only) = %q, want %q", got, want)
	}
	if got, want := res.ListWebURL(testRootURL, composeScope("gecko-3/b-linux", "lc-1")),
		testRootURL+"/worker-manager/gecko-3%2Fb-linux/workers?launchConfigId=lc-1"; got != want {
		t.Errorf("ListWebURL(with launch config) = %q, want %q", got, want)
	}

	id := composeWorkerID("gecko-3/b-linux", "wg-1", "w-1")
	if got, want := res.DetailWebURL(testRootURL, id),
		testRootURL+"/provisioners/gecko-3/worker-types/b-linux/workers/wg-1/w-1"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}

	if got := res.DetailWebURL(testRootURL, "not-a-valid-id"); got != "" {
		t.Errorf("DetailWebURL(invalid id) = %q, want empty", got)
	}
}

func TestLaunchConfigsResourceWebURL(t *testing.T) {
	res := NewLaunchConfigsResource(&fakeTaskcluster{})

	if got, want := res.ListWebURL(testRootURL, "gecko-3/b-linux"),
		testRootURL+"/worker-manager/gecko-3%2Fb-linux/launch-configs"; got != want {
		t.Errorf("ListWebURL = %q, want %q", got, want)
	}

	id := composeScope("gecko-3/b-linux", "lc-1")
	if got, want := res.DetailWebURL(testRootURL, id),
		testRootURL+"/worker-manager/gecko-3%2Fb-linux/launch-configs?launchConfigId=lc-1"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestErrorsResourceWebURL(t *testing.T) {
	res := NewErrorsResource(&fakeTaskcluster{})

	if got, want := res.ListWebURL(testRootURL, "gecko-3/b-linux"),
		testRootURL+"/worker-manager/gecko-3%2Fb-linux/errors"; got != want {
		t.Errorf("ListWebURL(pool only) = %q, want %q", got, want)
	}
	if got, want := res.ListWebURL(testRootURL, composeScope("gecko-3/b-linux", "lc-1")),
		testRootURL+"/worker-manager/gecko-3%2Fb-linux/errors?launchConfigId=lc-1"; got != want {
		t.Errorf("ListWebURL(with launch config) = %q, want %q", got, want)
	}

	id := composeScope("gecko-3/b-linux", "err-1")
	if got, want := res.DetailWebURL(testRootURL, id),
		testRootURL+"/worker-manager/gecko-3%2Fb-linux/errors"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestPendingAndClaimedTasksResourceWebURL(t *testing.T) {
	pending := NewPendingTasksResource(&fakeTaskcluster{}, nil)
	if got, want := pending.ListWebURL(testRootURL, "gecko-3/b-linux"),
		testRootURL+"/provisioners/gecko-3/worker-types/b-linux/pending-tasks"; got != want {
		t.Errorf("pending ListWebURL = %q, want %q", got, want)
	}
	if got, want := pending.DetailWebURL(testRootURL, "TASK1"), testRootURL+"/tasks/TASK1"; got != want {
		t.Errorf("pending DetailWebURL = %q, want %q", got, want)
	}

	claimed := NewClaimedTasksResource(&fakeTaskcluster{}, nil)
	if got, want := claimed.ListWebURL(testRootURL, "gecko-3/b-linux"),
		testRootURL+"/provisioners/gecko-3/worker-types/b-linux/claimed-tasks"; got != want {
		t.Errorf("claimed ListWebURL = %q, want %q", got, want)
	}
}

func TestTaskResourceWebURL(t *testing.T) {
	res := NewTaskResource(&fakeTaskcluster{}, nil)

	if got, want := res.DetailWebURL(testRootURL, "TASK1"), testRootURL+"/tasks/TASK1"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
	if got := res.ListWebURL(testRootURL, ""); got != "" {
		t.Errorf("ListWebURL = %q, want empty (DirectLookup, never rendered)", got)
	}
}

func TestTasksResourceWebURL(t *testing.T) {
	res := NewTasksResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if got, want := res.ListWebURL(testRootURL, "GROUP1"), testRootURL+"/tasks/groups/GROUP1"; got != want {
		t.Errorf("ListWebURL = %q, want %q", got, want)
	}
	if got, want := res.DetailWebURL(testRootURL, "TASK1"), testRootURL+"/tasks/TASK1"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestTaskGroupResourceWebURL(t *testing.T) {
	res := NewTaskGroupResource(&fakeTaskcluster{}, &taskDefHistory{}, nil)

	if got, want := res.ListWebURL(testRootURL, "GROUP1"), testRootURL+"/tasks/groups/GROUP1"; got != want {
		t.Errorf("ListWebURL = %q, want %q", got, want)
	}
	if got, want := res.DetailWebURL(testRootURL, "TASK1"), testRootURL+"/tasks/TASK1"; got != want {
		t.Errorf("DetailWebURL = %q, want %q", got, want)
	}
}

func TestTaskDependenciesAndDependentsResourceWebURL(t *testing.T) {
	deps := NewTaskDependenciesResource(&fakeTaskcluster{}, nil)
	if got, want := deps.ListWebURL(testRootURL, "TASK1"), testRootURL+"/tasks/TASK1"; got != want {
		t.Errorf("dependencies ListWebURL = %q, want %q", got, want)
	}
	if got, want := deps.DetailWebURL(testRootURL, "TASK2"), testRootURL+"/tasks/TASK2"; got != want {
		t.Errorf("dependencies DetailWebURL = %q, want %q", got, want)
	}

	dependents := NewTaskDependentsResource(&fakeTaskcluster{}, nil)
	if got, want := dependents.ListWebURL(testRootURL, "TASK1"), testRootURL+"/tasks/TASK1"; got != want {
		t.Errorf("dependents ListWebURL = %q, want %q", got, want)
	}
	if got, want := dependents.DetailWebURL(testRootURL, "TASK2"), testRootURL+"/tasks/TASK2"; got != want {
		t.Errorf("dependents DetailWebURL = %q, want %q", got, want)
	}
}
