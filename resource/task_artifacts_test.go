package resource

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"
	"github.com/taskcluster/taskcluster/v109/clients/client-go/tcqueue"

	"github.com/taskcluster/tc-tui/taskcluster"
)

func TestTaskArtifactsResourceScopedListReturnsOneRowPerArtifactAcrossRuns(t *testing.T) {
	fake := &fakeTaskcluster{
		taskStatus: &tcqueue.TaskStatusStructure{
			Runs: []tcqueue.RunInformation{{RunID: 0}, {RunID: 1}},
		},
		// The fake returns the same artifact list regardless of which run
		// was requested, so both runs' rows carry the same artifact but a
		// different RUN cell — what matters here is that ScopedList fetches
		// per run and tags each row accordingly.
		artifacts: taskcluster.ArtifactList{
			{Name: "public/logs/live_backing.log", ContentType: "text/plain", ContentLength: 2048},
		},
	}
	res := NewTaskArtifactsResource(fake)

	rows, err := res.ScopedList("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	for i, runLabel := range []string{"0", "1"} {
		row := rows[i]
		if row.ID != "task-1/"+runLabel+"::public/logs/live_backing.log" ||
			row.Cells[0] != runLabel || row.Cells[1] != "public/logs/live_backing.log" || row.Cells[2] != "text/plain" {
			t.Fatalf("unexpected row %d: %+v", i, row)
		}
		if row.NavTarget != nil {
			t.Fatalf("expected no NavTarget override — default selection should call Describe directly, got %+v", row.NavTarget)
		}
	}
}

func TestTaskArtifactsResourceScopedListPropagatesTaskStatusFetchError(t *testing.T) {
	fake := &fakeTaskcluster{taskStatusErr: errors.New("boom")}
	res := NewTaskArtifactsResource(fake)

	if _, err := res.ScopedList("task-1"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestArtifactRowsForRunShowsFailureInlineOnFetchError(t *testing.T) {
	fake := &fakeTaskcluster{artifactsErr: errors.New("boom")}

	rows := artifactRowsForRun(fake, "task-1", 0)
	if len(rows) != 1 || rows[0].Cells[0] != "0" || rows[0].Cells[1] != "(failed to load)" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestTaskArtifactsResourceFacetOptionsDerivesDistinctRunsInOrder(t *testing.T) {
	res := NewTaskArtifactsResource(&fakeTaskcluster{})

	rows := []Row{
		{Cells: []string{"0", "a"}},
		{Cells: []string{"1", "b"}},
		{Cells: []string{"0", "c"}},
	}
	got := res.FacetOptions(rows)
	want := []string{"0", "1"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected facet options: %+v", got)
	}
	if res.FacetColumn() != 0 {
		t.Fatalf("expected FacetColumn 0, got %d", res.FacetColumn())
	}
}

func TestTaskArtifactsResourceListRequiresScope(t *testing.T) {
	res := NewTaskArtifactsResource(&fakeTaskcluster{})

	if _, err := res.List(); err == nil {
		t.Fatalf("expected an error for an unscoped List call")
	}
}

func TestTaskArtifactsResourceDescribeRendersContent(t *testing.T) {
	fake := &fakeTaskcluster{artifactContent: "line one\nline two\n"}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/logs/live_backing.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Task :: task-1 :: Run 0 :: public/logs/live_backing.log" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}
	if detail.Body != "line one\nline two\n" {
		t.Fatalf("unexpected body: %q", detail.Body)
	}
}

// TestTaskArtifactsResourceDescribeShowsFullContentForOrdinaryLogSize
// guards against re-introducing an arbitrary line-count cap: a normal-sized
// log (a few thousand short lines, well under maxArtifactRenderBytes) must
// render in full, not get cut to some fixed line count.
func TestTaskArtifactsResourceDescribeShowsFullContentForOrdinaryLogSize(t *testing.T) {
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = "log line"
	}
	fake := &fakeTaskcluster{artifactContent: strings.Join(lines, "\n")}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/logs/live_backing.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(detail.Body, "showing only part") || strings.Contains(detail.Body, "showing last") {
		t.Fatalf("expected no truncation notice for an ordinary-sized log, got: %s", detail.Body[:200])
	}
	if strings.Count(detail.Body, "log line") != len(lines) {
		t.Fatalf("expected all %d lines, got %d", len(lines), strings.Count(detail.Body, "log line"))
	}
}

// TestTaskArtifactsResourceDescribeKeepsTailWhenOverRenderCap verifies the
// byte-size safety net (maxArtifactRenderBytes) keeps the *tail* of oversized
// content, not the head — the interesting bit of a failing task's log is
// almost always the end.
func TestTaskArtifactsResourceDescribeKeepsTailWhenOverRenderCap(t *testing.T) {
	content := strings.Repeat("a", maxArtifactRenderBytes) + "TAIL-MARKER"
	fake := &fakeTaskcluster{artifactContent: content}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/logs/live_backing.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "TAIL-MARKER") {
		t.Fatalf("expected the tail to be preserved when content exceeds the render cap")
	}
	if !strings.Contains(detail.Body, "showing only part") {
		t.Fatalf("expected a truncation banner, got: %s", detail.Body[:200])
	}
}

func TestTaskArtifactsResourceDescribeEscapesLiteralBracketsAndTranslatesANSI(t *testing.T) {
	raw := "[INFO] step 1\n\x1b[32mok\x1b[0m\n"
	fake := &fakeTaskcluster{artifactContent: raw}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/logs/live_backing.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compare against tview's own escaping/translation rather than a
	// hand-written expectation, since the exact escaped form is an
	// implementation detail of tview's tag syntax.
	want := tview.TranslateANSI(tview.Escape(raw))
	if detail.Body != want {
		t.Fatalf("unexpected body: %q, want %q", detail.Body, want)
	}
	// A literal "[INFO]" must not survive unescaped, or tview would try to
	// interpret it as a color/region tag.
	if strings.Contains(detail.Body, "[INFO]") {
		t.Fatalf("expected \"[INFO]\" to be escaped, got: %q", detail.Body)
	}
}

func TestIsBinaryArtifactToleratesTruncatedMultiByteTail(t *testing.T) {
	// "café" ends in a 2-byte UTF-8 rune (0xC3 0xA9); slicing off its last
	// byte — as GetArtifactContent's size cap can do to an otherwise-valid
	// huge text artifact — must not be misread as binary.
	full := "café"
	cutMidRune := full[:len(full)-1]

	if isBinaryArtifact("", cutMidRune) {
		t.Fatalf("expected content truncated mid-rune not to be classified as binary")
	}
}

func TestIsBinaryArtifactDetectsRealBinaryContent(t *testing.T) {
	if !isBinaryArtifact("", "\x00\x01\x02\xff\xfe") {
		t.Fatalf("expected NUL/invalid-UTF-8 content to be classified as binary")
	}
}

func TestChromaLanguageForKnownAndUnknownTypes(t *testing.T) {
	cases := []struct {
		contentType string
		wantLang    string
		wantOK      bool
	}{
		{"application/json", "json", true},
		{"application/json; charset=utf-8", "json", true},
		{"text/yaml", "yaml", true},
		{"application/x-yaml", "yaml", true},
		{"text/markdown", "markdown", true},
		{"application/xml", "xml", true},
		{"application/octet-stream", "", false},
		{"text/plain", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		lang, ok := chromaLanguageFor(c.contentType)
		if lang != c.wantLang || ok != c.wantOK {
			t.Fatalf("chromaLanguageFor(%q) = (%q, %v), want (%q, %v)", c.contentType, lang, ok, c.wantLang, c.wantOK)
		}
	}
}

func TestRenderHighlightedOrPlainHighlightsSmallKnownContentType(t *testing.T) {
	content := `{"status":"ok"}`

	got := renderHighlightedOrPlain("application/json", content)
	plain := renderArtifactText(content)
	if got == plain {
		t.Fatalf("expected small JSON to be highlighted, not rendered identically to plain text")
	}
	if !strings.Contains(stripRegionTags(got), content) {
		t.Fatalf("expected the original JSON preserved verbatim in highlighted output, got: %s", stripRegionTags(got))
	}
}

func TestRenderHighlightedOrPlainSkipsHighlightingOverSizeThreshold(t *testing.T) {
	content := `{"data":"` + strings.Repeat("a", maxArtifactHighlightBytes) + `"}`

	got := renderHighlightedOrPlain("application/json", content)
	want := renderArtifactText(content)
	if got != want {
		t.Fatalf("expected oversized JSON to skip highlighting and match plain rendering exactly")
	}
}

func TestRenderHighlightedOrPlainFallsBackForUnknownContentType(t *testing.T) {
	content := "just some text"

	got := renderHighlightedOrPlain("text/plain", content)
	want := renderArtifactText(content)
	if got != want {
		t.Fatalf("expected an unhighlightable content-type to render exactly like plain text")
	}
}

// TestRenderHighlightedArtifactPreservesLiteralBracketsInJSONArrays exercises
// the escape-before-TranslateANSI ordering directly: JSON is full of literal
// "[" "]" (arrays), which must survive as literal characters in the
// highlighted output rather than being consumed as (or corrupting) a tview
// color/region tag.
func TestRenderHighlightedArtifactPreservesLiteralBracketsInJSONArrays(t *testing.T) {
	content := `{"items":["a","b"],"count":2}`

	out, err := renderHighlightedArtifact("json", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stripRegionTags(out), content) {
		t.Fatalf("expected original JSON (including its brackets) preserved verbatim, got: %s", stripRegionTags(out))
	}
}

// TestEscapeIgnoringANSIDoesNotConsumeBracketAdjacentToANSICode is a direct
// regression test for the bug renderHighlightedArtifact's array test above
// exercises end-to-end: a plain tview.Escape(fullANSIString) can match
// starting at an ANSI code's own "[", treat its parameter digits as tag
// content, and swallow an immediately-following literal "]" from real
// content as that "tag"'s close — inserting a spurious "[" and corrupting
// the text. escapeIgnoringANSI must leave the ANSI code AND the adjacent
// literal bracket both intact.
func TestEscapeIgnoringANSIDoesNotConsumeBracketAdjacentToANSICode(t *testing.T) {
	s := "\x1b[38;5;187m],\x1b[0m"

	got := escapeIgnoringANSI(s)
	translated := tview.TranslateANSI(got)

	if strings.Contains(translated, "[]") {
		t.Fatalf("expected the literal \"],\" not to gain a spurious \"[\", got: %q", translated)
	}
	if !strings.Contains(translated, "]") {
		t.Fatalf("expected the literal \"]\" to survive, got: %q", translated)
	}
}

func TestEscapeIgnoringANSIStillEscapesPlainTagLikeBrackets(t *testing.T) {
	got := escapeIgnoringANSI("[INFO] no ansi here")
	if strings.Contains(got, "[INFO]") {
		t.Fatalf("expected \"[INFO]\" to be escaped even with no ANSI codes present, got: %q", got)
	}
}

func TestTaskArtifactsResourceDescribeShowsMetadataForBinaryContent(t *testing.T) {
	fake := &fakeTaskcluster{
		artifactContent:     "\x00\x01\x02binary-ish\xff",
		artifactContentType: "application/octet-stream",
	}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/data.bin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "application/octet-stream") || !strings.Contains(detail.Body, "isn't rendered") {
		t.Fatalf("expected a binary-content notice, got: %s", detail.Body)
	}
	if strings.ContainsRune(detail.Body, '\x00') {
		t.Fatalf("expected raw binary bytes not to appear in the body: %q", detail.Body)
	}
}

func TestTaskArtifactsResourceDescribeShowsTruncationBanner(t *testing.T) {
	fake := &fakeTaskcluster{artifactContent: "partial content", artifactTruncated: true}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/logs/live_backing.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(detail.Body, "showing only part of this artifact") || !strings.Contains(detail.Body, "partial content") {
		t.Fatalf("expected a truncation banner alongside the fetched content, got: %s", detail.Body)
	}
}

// TestTaskArtifactsResourceDescribeHighlightsJSONPreservingOriginalForm
// verifies JSON is syntax-highlighted without being reformatted — e.g. into
// YAML — since debugging an artifact means seeing exactly what's in it, a
// different concern from a task's own description/payload getting a
// prettified render.
func TestTaskArtifactsResourceDescribeHighlightsJSONPreservingOriginalForm(t *testing.T) {
	content := `{"items":["a","b"],"count":2}`
	fake := &fakeTaskcluster{artifactContent: content, artifactContentType: "application/json"}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/result.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stripRegionTags(detail.Body), content) {
		t.Fatalf("expected the original JSON preserved verbatim, got: %s", stripRegionTags(detail.Body))
	}
}

// TestTaskArtifactsResourceDescribeRendersLargeJSONQuickly guards against a
// real regression: syntax-highlighting a ~227 KiB JSON artifact via
// glamour/chroma took long enough to block Describe — and since tview's
// key-input loop and the caller of Describe share one goroutine, that froze
// the whole UI, quitting included. renderHighlightedOrPlain now skips
// highlighting above maxArtifactHighlightBytes; this pins that down with a
// wall-clock budget using content sized to exceed the threshold.
func TestTaskArtifactsResourceDescribeRendersLargeJSONQuickly(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 5000; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":%d,"name":"item-%d","status":"ok"}`, i, i)
	}
	sb.WriteString("]")

	fake := &fakeTaskcluster{artifactContent: sb.String(), artifactContentType: "application/json"}
	res := NewTaskArtifactsResource(fake)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := res.Describe("task-1/0::public/result.json"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Describe did not return within 5s — likely stuck in a slow renderer")
	}
}

func TestTaskArtifactsResourceDescribeRendersMarkdownArtifact(t *testing.T) {
	fake := &fakeTaskcluster{artifactContent: "# Heading\n\nsome text", artifactContentType: "text/markdown"}
	res := NewTaskArtifactsResource(fake)

	detail, err := res.Describe("task-1/0::public/README.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stripRegionTags(detail.Body), "Heading") {
		t.Fatalf("expected the markdown content rendered, got: %s", detail.Body)
	}
}

func TestTaskArtifactsResourceDescribeRejectsMalformedID(t *testing.T) {
	res := NewTaskArtifactsResource(&fakeTaskcluster{})

	if _, err := res.Describe("not-an-artifact-id"); err == nil {
		t.Fatalf("expected an error for a malformed artifact id")
	}
}

func TestTaskArtifactsResourceDescribePropagatesFetchError(t *testing.T) {
	fake := &fakeTaskcluster{artifactContentErr: errors.New("boom")}
	res := NewTaskArtifactsResource(fake)

	if _, err := res.Describe("task-1/0::public/logs/live_backing.log"); err == nil {
		t.Fatalf("expected an error to propagate")
	}
}

func TestTaskArtifactsResourceDetailWebURLLinksDirectlyToArtifactContent(t *testing.T) {
	fake := &fakeTaskcluster{artifactURL: "https://tc.example.com/signed-artifact-url"}
	res := NewTaskArtifactsResource(fake)

	got := res.DetailWebURL("https://tc.example.com", "task-1/0::public/logs/live_backing.log")
	if got != "https://tc.example.com/signed-artifact-url" {
		t.Fatalf("unexpected URL: %q", got)
	}
}

func TestTaskArtifactsResourceDetailWebURLRejectsMalformedID(t *testing.T) {
	res := NewTaskArtifactsResource(&fakeTaskcluster{})

	if got := res.DetailWebURL("https://tc.example.com", "not-an-artifact-id"); got != "" {
		t.Fatalf("expected an empty URL for a malformed id, got %q", got)
	}
}

func TestTaskArtifactsResourceDetailWebURLPropagatesFetchError(t *testing.T) {
	fake := &fakeTaskcluster{artifactURLErr: errors.New("boom")}
	res := NewTaskArtifactsResource(fake)

	if got := res.DetailWebURL("https://tc.example.com", "task-1/0::public/logs/live_backing.log"); got != "" {
		t.Fatalf("expected an empty URL on fetch error, got %q", got)
	}
}
