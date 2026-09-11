package resource

import (
	"errors"
	"strings"
	"testing"

	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"
)

func runningStatusFake() *fakeTaskcluster {
	return &fakeTaskcluster{
		taskStatus: &tcqueue.TaskStatusStructure{
			Runs: []tcqueue.RunInformation{
				{RunID: 0, State: "completed"},
				{RunID: 1, State: "running"},
			},
		},
	}
}

func TestTaskArtifactsIsLive(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"running run's live log", composeArtifactID("task-1", 1, "public/logs/live.log"), true},
		{"completed run's live log", composeArtifactID("task-1", 0, "public/logs/live.log"), false},
		{"backing log never streams", composeArtifactID("task-1", 1, "public/logs/live_backing.log"), false},
		{"other artifact never streams", composeArtifactID("task-1", 1, "public/build/target.zip"), false},
		{"unknown run", composeArtifactID("task-1", 7, "public/logs/live.log"), false},
		{"malformed id", "not-an-artifact-id", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := NewTaskArtifactsResource(runningStatusFake())
			if got := res.IsLive(tc.id); got != tc.want {
				t.Fatalf("IsLive(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// A status fetch failure must fall back to the one-shot path (not live),
// where the error surfaces through the normal Describe flow.
func TestTaskArtifactsIsLiveStatusError(t *testing.T) {
	fake := &fakeTaskcluster{taskStatusErr: errors.New("boom")}
	res := NewTaskArtifactsResource(fake)

	if res.IsLive(composeArtifactID("task-1", 1, "public/logs/live.log")) {
		t.Fatal("expected IsLive to be false when status can't be fetched")
	}
}

func TestTaskArtifactsStreamDetail(t *testing.T) {
	fake := runningStatusFake()
	fake.streamChunks = [][]byte{
		[]byte("line one\npar"),
		[]byte("tial line\n\x1b[31mred\x1b[0m [INFO] ok\n"),
		[]byte("trailing tail"),
	}
	res := NewTaskArtifactsResource(fake)

	var started []Detail
	var appended []string
	id := composeArtifactID("task-1", 1, "public/logs/live.log")
	truncated, err := res.StreamDetail(id, nil,
		func(d Detail) { started = append(started, d) },
		func(text string) { appended = append(appended, text) },
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}

	if len(started) != 1 {
		t.Fatalf("expected exactly one onStart, got %d", len(started))
	}
	if started[0].Title != "Task :: task-1 :: Run 1 :: public/logs/live.log" {
		t.Fatalf("unexpected title: %s", started[0].Title)
	}

	all := strings.Join(appended, "")
	// Only complete lines flow through until the final flush; the partial
	// line split across chunks arrives reassembled.
	if !strings.Contains(all, "line one\n") || !strings.Contains(all, "partial line\n") {
		t.Fatalf("expected reassembled lines, got %q", all)
	}
	// The trailing unterminated line is flushed at stream end.
	if !strings.HasSuffix(all, "trailing tail") {
		t.Fatalf("expected the trailing tail flushed last, got %q", all)
	}
	// Raw ANSI must be translated to tview markup (no ESC bytes reach the
	// view), and literal brackets must be escaped so they can't be misread
	// as tview tags.
	if strings.Contains(all, "\x1b") {
		t.Fatalf("raw ANSI escape leaked through: %q", all)
	}
	if !strings.Contains(all, "[maroon:]red") { // tview maps ANSI 31 to "maroon"
		t.Fatalf("expected ANSI red translated to a tview tag, got %q", all)
	}
	if !strings.Contains(all, "[INFO[]") {
		t.Fatalf("expected literal brackets escaped, got %q", all)
	}
}

func TestTaskArtifactsStreamDetailTruncated(t *testing.T) {
	fake := runningStatusFake()
	fake.streamChunks = [][]byte{[]byte("x\n")}
	fake.streamTruncated = true
	res := NewTaskArtifactsResource(fake)

	truncated, err := res.StreamDetail(composeArtifactID("task-1", 1, "public/logs/live.log"), nil,
		func(Detail) {}, func(string) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to propagate")
	}
}

func TestTaskArtifactsStreamDetailError(t *testing.T) {
	fake := runningStatusFake()
	fake.streamErr = errors.New("boom")
	res := NewTaskArtifactsResource(fake)

	_, err := res.StreamDetail(composeArtifactID("task-1", 1, "public/logs/live.log"), nil,
		func(Detail) {}, func(string) {})
	if !errors.Is(err, fake.streamErr) {
		t.Fatalf("expected stream error to propagate, got %v", err)
	}
}
