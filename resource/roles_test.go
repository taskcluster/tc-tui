package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcauth"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestRolesResourceList(t *testing.T) {
	fake := &fakeTaskcluster{
		roles: taskcluster.RolesList{
			{RoleID: "hook-id:foo"},
			{RoleID: "hook-id:bar"},
		},
	}
	res := NewRolesResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "hook-id:foo" || rows[0].Cells[0] != "hook-id:foo" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func TestRolesResourceListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{rolesErr: wantErr}
	res := NewRolesResource(fake)

	_, err := res.List()
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestRolesResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		role: &tcauth.GetRoleResponse{
			RoleID:         "hook-id:foo",
			Description:    "a role",
			Created:        tcclient.Time(time.Now()),
			LastModified:   tcclient.Time(time.Now()),
			Scopes:         []string{"scope:a"},
			ExpandedScopes: []string{"scope:a", "scope:b"},
		},
	}
	res := NewRolesResource(fake)

	detail, err := res.Describe("hook-id:foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Role :: hook-id:foo" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if !strings.Contains(detail.Body, "a role") || !strings.Contains(detail.Body, "scope:a") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
}

func TestRolesResourceDescribeGroupsCreatedAndLastModifiedOnOneLine(t *testing.T) {
	fake := &fakeTaskcluster{role: &tcauth.GetRoleResponse{RoleID: "role-1"}}
	res := NewRolesResource(fake)

	detail, err := res.Describe("role-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := stripRegionTags(detail.Body)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Created") {
			if !strings.Contains(line, "Last Modified") {
				t.Fatalf("expected Created and Last Modified on the same line, got: %q", line)
			}
			return
		}
	}
	t.Fatalf("Created not found in body: %s", body)
}

func TestRolesResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{roleErr: wantErr}
	res := NewRolesResource(fake)

	_, err := res.Describe("hook-id:foo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}
