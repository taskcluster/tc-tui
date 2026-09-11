package resource

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcsecrets"
)

func TestSecretsResourceList(t *testing.T) {
	fake := &fakeTaskcluster{secrets: []string{"proj/foo", "proj/bar"}}
	res := NewSecretsResource(fake)

	rows, err := res.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "proj/foo" || rows[0].Cells[0] != "proj/foo" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func TestSecretsResourceListError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{secretsErr: wantErr}
	res := NewSecretsResource(fake)

	_, err := res.List()
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func secretsResourceWith(t *testing.T, value any) *SecretsResource {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal test secret: %v", err)
	}

	return NewSecretsResource(&fakeTaskcluster{
		secret: &tcsecrets.Secret{Expires: tcclient.Time(time.Now()), Secret: raw},
	})
}

func TestSecretsResourceDescribeMasksValues(t *testing.T) {
	res := secretsResourceWith(t, map[string]string{"token": "s3cr3t"})

	detail, err := res.Describe("proj/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Secret :: proj/foo" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if strings.Contains(detail.Body, "s3cr3t") {
		t.Fatalf("expected the value to be masked, got body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "token") {
		t.Fatalf("expected the key to survive masking, got body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, secretMask) {
		t.Fatalf("expected a mask placeholder, got body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "values hidden") {
		t.Fatalf("expected the body to say its values are hidden, got body: %s", detail.Body)
	}
}

func TestSecretsResourceDescribeRevealed(t *testing.T) {
	res := secretsResourceWith(t, map[string]string{"token": "s3cr3t"})

	detail, err := res.DescribeRevealed("proj/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "s3cr3t") {
		t.Fatalf("expected the value to be rendered in the clear, got body: %s", detail.Body)
	}
	if !strings.Contains(detail.Body, "revealed") {
		t.Fatalf("expected the body to flag itself as revealed, got body: %s", detail.Body)
	}
}

func TestSecretsResourceDescribeRevealedError(t *testing.T) {
	wantErr := errors.New("boom")
	res := NewSecretsResource(&fakeTaskcluster{secretErr: wantErr})

	if _, err := res.DescribeRevealed("proj/foo"); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

// A secret's shape (its keys, and whether a value is a list) is safe to
// show and useful to see; nothing below a key is.
func TestMaskSecretValuesKeepsStructureAndMasksEveryLeaf(t *testing.T) {
	raw := json.RawMessage(`{
		"token": "s3cr3t",
		"count": 42,
		"enabled": true,
		"missing": null,
		"nested": {"inner": "hunter2"},
		"list": ["one", {"deep": "two"}]
	}`)

	masked := maskSecretValues(raw)

	var got map[string]any
	if err := json.Unmarshal(masked, &got); err != nil {
		t.Fatalf("masked output is not valid JSON: %v (%s)", err, masked)
	}

	want := map[string]any{
		"token":   secretMask,
		"count":   secretMask,
		"enabled": secretMask,
		"missing": secretMask,
		"nested":  map[string]any{"inner": secretMask},
		"list":    []any{secretMask, map[string]any{"deep": secretMask}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected masking:\n got %#v\nwant %#v", got, want)
	}

	for _, leaked := range []string{"s3cr3t", "hunter2", "42", "true", "one", "two"} {
		if strings.Contains(string(masked), leaked) {
			t.Fatalf("masked output leaked %q: %s", leaked, masked)
		}
	}
}

// Masking has to fail closed: content it can't parse is content it can't
// prove is safe to print.
func TestMaskSecretValuesFailsClosedOnUnparseableContent(t *testing.T) {
	masked := maskSecretValues(json.RawMessage(`not json: s3cr3t`))

	if strings.Contains(string(masked), "s3cr3t") {
		t.Fatalf("expected unparseable content to be masked wholesale, got %s", masked)
	}
	if string(masked) != `"`+secretMask+`"` {
		t.Fatalf("expected a single mask, got %s", masked)
	}
}

// An absent value must not render as a mask — that would imply a secret
// holds something it doesn't.
func TestMaskSecretValuesLeavesEmptyContentAlone(t *testing.T) {
	for _, raw := range []string{"", "null", "  "} {
		if got := maskSecretValues(json.RawMessage(raw)); string(got) != raw {
			t.Fatalf("expected %q to be left alone, got %q", raw, got)
		}
	}
}

func TestSecretsResourceDescribeError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeTaskcluster{secretErr: wantErr}
	res := NewSecretsResource(fake)

	_, err := res.Describe("proj/foo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestSecretsResourceWebURLs(t *testing.T) {
	res := NewSecretsResource(&fakeTaskcluster{})

	if got := res.ListWebURL("https://tc.example.com", ""); got != "https://tc.example.com/secrets" {
		t.Fatalf("unexpected list web url: %s", got)
	}
	if got := res.DetailWebURL("https://tc.example.com", "proj/foo"); got != "https://tc.example.com/secrets/proj%2Ffoo" {
		t.Fatalf("unexpected detail web url: %s", got)
	}
}
