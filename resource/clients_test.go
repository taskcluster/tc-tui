package resource

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcauth"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestClientsResourceList(t *testing.T) {
	fake := &fakeTaskcluster{
		clients: taskcluster.ClientList{
			{ClientID: "project/foo", Disabled: false, Expires: tcclient.Time(time.Now())},
			{ClientID: "project/bar", Disabled: true, Expires: tcclient.Time(time.Now())},
		},
	}
	res := NewClientsResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "project/foo" || rows[0].Cells[0] != "project/foo" || rows[0].Cells[1] != strconv.FormatBool(false) {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	if rows[1].Cells[1] != strconv.FormatBool(true) {
		t.Fatalf("unexpected row: %+v", rows[1])
	}
}

func TestClientsResourceListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{clientsErr: wantErr}
	res := NewClientsResource(fake)

	_, err := res.List()
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestClientsResourceDescribe(t *testing.T) {
	fake := &fakeTaskcluster{
		client: &tcauth.GetClientResponse{
			ClientID:       "project/foo",
			Description:    "a client",
			Scopes:         []string{"scope:a"},
			ExpandedScopes: []string{"scope:a", "scope:b"},
			Created:        tcclient.Time(time.Now()),
			LastModified:   tcclient.Time(time.Now()),
			LastRotated:    tcclient.Time(time.Now()),
			LastDateUsed:   tcclient.Time(time.Now()),
		},
	}
	res := NewClientsResource(fake)

	detail, err := res.Describe("project/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Client :: project/foo" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	body := stripRegionTags(detail.Body)
	if !strings.Contains(body, "a client") || !strings.Contains(body, "scope:a") {
		t.Fatalf("unexpected body: %s", detail.Body)
	}
}

func TestClientsResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{clientErr: wantErr}
	res := NewClientsResource(fake)

	_, err := res.Describe("project/foo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestClientsResourceWebURLs(t *testing.T) {
	res := NewClientsResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", ""); got != "https://tc.example.com/auth/clients" {
		t.Fatalf("unexpected list web url: %s", got)
	}
	if got := res.DetailWebURL("https://tc.example.com", "project/foo"); got != "https://tc.example.com/auth/clients/project%2Ffoo" {
		t.Fatalf("unexpected detail web url: %s", got)
	}
}
