package shell

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

func TestDetailViewSetDataResetsScrollToTop(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 20, 5)
	d.SetData(resource.Detail{Body: strings.Repeat("line\n", 50)})
	d.ScrollTo(10, 0)

	d.SetData(resource.Detail{Body: strings.Repeat("line\n", 50)})

	row, _ := d.GetScrollOffset()
	if row != 0 {
		t.Fatalf("expected SetData to reset scroll to top, got row %d", row)
	}
}

func TestDetailViewUpdateDataPreservesScroll(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 20, 5)
	d.SetData(resource.Detail{Body: strings.Repeat("line\n", 50)})
	d.ScrollTo(10, 0)

	d.UpdateData(resource.Detail{Body: strings.Repeat("line\n", 50)})

	row, _ := d.GetScrollOffset()
	if row != 10 {
		t.Fatalf("expected UpdateData to preserve scroll, got row %d, want 10", row)
	}
}

func TestDetailViewSetFilterQueryKeepsOnlyMatchingLines(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\ngamma\n"})

	d.SetFilterQuery("eta")

	got := d.GetText(true)
	if got != "beta" {
		t.Fatalf("unexpected filtered text: %q", got)
	}
	if d.FilterQuery() != "eta" {
		t.Fatalf("expected FilterQuery to report %q, got %q", "eta", d.FilterQuery())
	}
}

func TestDetailViewSetFilterQueryIsCaseInsensitive(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "Completed\nFailed\n"})

	d.SetFilterQuery("completed")

	if got := d.GetText(true); got != "Completed" {
		t.Fatalf("unexpected filtered text: %q", got)
	}
}

func TestDetailViewSetFilterQueryMatchesVisibleTextNotColorTags(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "[green]Completed[white]\n[red]Failed[white]\n"})

	// "reen" only appears inside the "[green]" tag markup, never in the
	// visible text of either line — matching raw markup would wrongly keep
	// the first line.
	d.SetFilterQuery("reen")

	if got := d.GetText(true); got != "" {
		t.Fatalf("expected no lines to match a query that only appears in tag markup, got %q", got)
	}
}

func TestDetailViewSetFilterQueryEmptyRestoresAllLines(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\ngamma\n"})
	d.SetFilterQuery("beta")

	d.SetFilterQuery("")

	if got := d.GetText(true); got != "alpha\nbeta\ngamma\n" {
		t.Fatalf("expected clearing the filter to restore every line, got %q", got)
	}
}

func TestDetailViewUpdateDataReappliesActiveFilter(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	d.SetFilterQuery("beta")

	d.UpdateData(resource.Detail{Body: "alpha\nbeta\ngamma\n"})

	if got := d.GetText(true); got != "beta" {
		t.Fatalf("expected the refreshed body to stay filtered, got %q", got)
	}
	if d.FilterQuery() != "beta" {
		t.Fatalf("expected FilterQuery to survive UpdateData, got %q", d.FilterQuery())
	}
}

func TestDetailViewSetDataResetsFilter(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\n"})
	d.SetFilterQuery("beta")

	d.SetData(resource.Detail{Body: "alpha\nbeta\n"})

	if d.FilterQuery() != "" {
		t.Fatalf("expected navigating to a new detail to clear the filter, got %q", d.FilterQuery())
	}
	if got := d.GetText(true); got != "alpha\nbeta\n" {
		t.Fatalf("expected the new body unfiltered, got %q", got)
	}
}

func TestDetailViewStreamFiltersLinesLive(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.SetFilterQuery("err")

	d.AppendStream("info: starting\n")
	d.AppendStream("err: boom\n")
	d.AppendStream("info: done\n")

	if got := d.GetText(true); got != "err: boom\n" {
		t.Fatalf("expected only the matching streamed line to be visible, got %q", got)
	}
}

func TestDetailViewStreamFilterChangeRebuildsFromRetainedLines(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()

	d.AppendStream("info: starting\n")
	d.AppendStream("err: boom\n")
	d.AppendStream("info: done\n")

	d.SetFilterQuery("err")
	if got := d.GetText(true); got != "err: boom\n" {
		t.Fatalf("expected the filter to hide lines appended before it was set, got %q", got)
	}

	d.SetFilterQuery("")
	if got := d.GetText(true); got != "info: starting\nerr: boom\ninfo: done\n" {
		t.Fatalf("expected clearing the filter to restore every retained line, got %q", got)
	}
}

func TestDetailViewStreamBannerBypassesFilter(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.SetFilterQuery("err")
	d.AppendStream("info: nothing matches\n")

	d.AppendBanner("(live stream ended)")

	got := d.GetText(true)
	if !strings.Contains(got, "(live stream ended)") {
		t.Fatalf("expected the banner to bypass the active filter, got %q", got)
	}
}

func TestDetailViewLineNumbersDisabledByDefault(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\n"})

	if d.LineNumbersEnabled() {
		t.Fatalf("expected line numbers disabled by default")
	}
	if got := d.GetText(true); got != "alpha\nbeta\n" {
		t.Fatalf("expected unnumbered text, got %q", got)
	}
}

func TestDetailViewSetLineNumbersEnabledPrependsLineNumbers(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\n"})

	d.SetLineNumbersEnabled(true)

	want := "   1 alpha\n   2 beta\n"
	if got := d.GetText(true); got != want {
		t.Fatalf("unexpected numbered text:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewLineNumbersReflectOriginalPositionWhenFiltered(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\nbeta\ngamma\n"})
	d.SetLineNumbersEnabled(true)

	d.SetFilterQuery("beta")

	want := "   2 beta"
	if got := d.GetText(true); got != want {
		t.Fatalf("expected the filtered line to keep its original line number:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewSetLineNumbersEnabledPreservesScroll(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 20, 5)
	d.SetData(resource.Detail{Body: strings.Repeat("line\n", 50)})
	d.ScrollTo(10, 0)

	d.SetLineNumbersEnabled(true)

	row, _ := d.GetScrollOffset()
	if row != 10 {
		t.Fatalf("expected toggling line numbers to preserve scroll, got row %d, want 10", row)
	}
}

func TestDetailViewLineNumbersPersistAcrossSetData(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\n"})
	d.SetLineNumbersEnabled(true)

	d.SetData(resource.Detail{Body: "gamma\ndelta\n"})

	if !d.LineNumbersEnabled() {
		t.Fatalf("expected the line-number preference to persist across navigating to a new detail")
	}
	want := "   1 gamma\n   2 delta\n"
	if got := d.GetText(true); got != want {
		t.Fatalf("unexpected numbered text after SetData:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewStreamLineNumbersReflectAbsolutePosition(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.SetLineNumbersEnabled(true)

	d.AppendStream("first\n")
	d.AppendStream("second\n")

	want := "   1 first\n   2 second\n"
	if got := d.GetText(true); got != want {
		t.Fatalf("unexpected numbered stream text:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewStreamLineNumbersSurviveTruncation(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()

	for i := 0; i < streamViewMaxLines+10; i++ {
		d.AppendStream(fmt.Sprintf("line-%d\n", i+1))
	}

	// Toggling numbering on triggers a full rebuild from the retained
	// (already-capped) streamLines buffer — the oldest 10 lines were
	// dropped, but the surviving first line's number must still reflect its
	// true original position (11), not 1 — otherwise numbering would
	// silently relabel every retained line once the buffer starts dropping.
	d.SetLineNumbersEnabled(true)

	want := fmt.Sprintf("%*d line-11", lineNumberWidth, 11)
	got := d.GetText(true)
	firstLine := strings.SplitN(got, "\n", 2)[0]
	if firstLine != want {
		t.Fatalf("unexpected first numbered line after truncation:\ngot:  %q\nwant: %q", firstLine, want)
	}
}

func TestDetailViewStreamLineNumbersWithFilterKeepOriginalPosition(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.SetLineNumbersEnabled(true)

	d.AppendStream("info: starting\n")
	d.AppendStream("err: boom\n")
	d.AppendStream("info: done\n")
	d.SetFilterQuery("err")

	want := "   2 err: boom\n"
	if got := d.GetText(true); got != want {
		t.Fatalf("unexpected numbered+filtered stream text:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSplitLinesPreservesEmbeddedBlankLine(t *testing.T) {
	got := splitLines("one\n\ntwo\n")
	want := []string{"one", "", "two"}
	if !equalStrings(got, want) {
		t.Fatalf("splitLines(%q) = %v, want %v", "one\n\ntwo\n", got, want)
	}
}

func TestSplitLinesTrimsOnlyTheFinalTerminator(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"one\n", []string{"one"}},
		{"one", []string{"one"}},
		{"", nil},
	}
	for _, c := range cases {
		if got := splitLines(c.text); !equalStrings(got, c.want) {
			t.Errorf("splitLines(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDetailViewFilteredBodyPreservesMultipleTrailingBlankLinesUnnumbered(t *testing.T) {
	d := NewDetailView()
	d.SetData(resource.Detail{Body: "alpha\n\n"})

	if got := d.GetText(true); got != "alpha\n\n" {
		t.Fatalf("expected the blank trailing line preserved when numbering is off, got %q", got)
	}
}

func TestDetailViewStreamLineNumbersCountBlankLinesInBatch(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.SetLineNumbersEnabled(true)

	d.AppendStream("one\n\ntwo\n")

	want := strings.Join([]string{
		fmt.Sprintf("%*d one", lineNumberWidth, 1),
		fmt.Sprintf("%*d ", lineNumberWidth, 2),
		fmt.Sprintf("%*d two", lineNumberWidth, 3),
	}, "\n") + "\n"
	if got := d.GetText(true); got != want {
		t.Fatalf("unexpected numbered text for a batch containing a blank line:\ngot:  %q\nwant: %q", got, want)
	}

	d.AppendStream("three\n")
	want += fmt.Sprintf("%*d three\n", lineNumberWidth, 4)
	if got := d.GetText(true); got != want {
		t.Fatalf("expected numbering to continue from the correct absolute count after the blank line:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewStreamPreservesTrailingBlankLineInBatch(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.SetLineNumbersEnabled(true)

	// A single chunk ending in a blank line ("one" followed by an empty
	// line) followed by a separate later chunk — the blank line must be
	// retained as its own line, and "two" must be numbered 3, not 2.
	d.AppendStream("one\n\n")
	d.AppendStream("two\n")

	want := strings.Join([]string{
		fmt.Sprintf("%*d one", lineNumberWidth, 1),
		fmt.Sprintf("%*d ", lineNumberWidth, 2),
		fmt.Sprintf("%*d two", lineNumberWidth, 3),
	}, "\n") + "\n"
	if got := d.GetText(true); got != want {
		t.Fatalf("unexpected numbered text across a batch boundary with a trailing blank line:\ngot:  %q\nwant: %q", got, want)
	}
	if len(d.streamLines) != 3 {
		t.Fatalf("expected 3 retained lines including the blank one, got %d: %v", len(d.streamLines), d.streamLines)
	}
}

func TestDetailViewSetLineNumbersEnabledPreservesTailFollow(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	screen.SetSize(80, 10)

	d := NewDetailView()
	d.SetRect(0, 0, 80, 10)
	d.StartStream() // ScrollToEnd() puts the view in tail-follow mode.

	for i := 0; i < 50; i++ {
		d.AppendStream(fmt.Sprintf("line-%d\n", i))
	}
	d.Draw(screen)

	d.SetLineNumbersEnabled(true)
	d.Draw(screen)
	rowAfterToggle, _ := d.GetScrollOffset()

	// If the toggle left tail-follow engaged, each further Draw keeps
	// pinning the offset to the (growing) bottom as more lines arrive. If it
	// silently detached instead, the offset freezes at whatever it was
	// right after the toggle, no matter how much more streams in.
	for i := 50; i < 100; i++ {
		d.AppendStream(fmt.Sprintf("line-%d\n", i))
	}
	d.Draw(screen)
	rowAfterMoreLines, _ := d.GetScrollOffset()

	if rowAfterMoreLines <= rowAfterToggle {
		t.Fatalf("expected tail-follow to still be engaged after toggling line numbers: row after toggle=%d, after 50 more lines=%d",
			rowAfterToggle, rowAfterMoreLines)
	}
}

func TestDetailViewSetLineNumbersEnabledPreservesDetachedScroll(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	screen.SetSize(80, 10)

	d := NewDetailView()
	d.SetRect(0, 0, 80, 10)
	d.StartStream()
	for i := 0; i < 50; i++ {
		d.AppendStream(fmt.Sprintf("line-%d\n", i))
	}
	d.Draw(screen)
	d.ScrollTo(5, 0) // user scrolls up to inspect older output — detaches.

	d.SetLineNumbersEnabled(true)
	d.Draw(screen)
	rowAfterToggle, _ := d.GetScrollOffset()

	// A detached position must stay put even as more lines arrive — it must
	// not have been silently reattached to the tail by the toggle.
	for i := 50; i < 100; i++ {
		d.AppendStream(fmt.Sprintf("line-%d\n", i))
	}
	d.Draw(screen)
	rowAfterMoreLines, _ := d.GetScrollOffset()

	if rowAfterMoreLines != rowAfterToggle {
		t.Fatalf("expected the detached scroll position to stay put while more lines arrived, got %d then %d",
			rowAfterToggle, rowAfterMoreLines)
	}
}

func TestDetailViewSetLineNumbersEnabledPreservesStreamScroll(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	for i := 0; i < 200; i++ {
		d.AppendStream(fmt.Sprintf("line-%d\n", i))
	}
	d.ScrollTo(10, 0)

	d.SetLineNumbersEnabled(true)

	row, _ := d.GetScrollOffset()
	if row != 10 {
		t.Fatalf("expected toggling line numbers mid-stream to preserve scroll, got row %d, want 10", row)
	}
}

func TestDetailViewSetLineNumbersEnabledPreservesStreamBanner(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()
	d.AppendStream("info: done\n")
	d.AppendBanner("(live stream ended)")

	d.SetLineNumbersEnabled(true)

	got := d.GetText(true)
	if !strings.Contains(got, "(live stream ended)") {
		t.Fatalf("expected the end-of-stream banner to survive toggling line numbers, got %q", got)
	}
}

func TestDetailViewStreamLinesCappedAtStreamViewMaxLines(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 40, 5)
	d.StartStream()

	for i := 0; i < streamViewMaxLines+10; i++ {
		d.AppendStream("line\n")
	}

	if len(d.streamLines) != streamViewMaxLines {
		t.Fatalf("expected retained lines capped at %d, got %d", streamViewMaxLines, len(d.streamLines))
	}
}

func TestApplyTagSetsOnlyGivenFields(t *testing.T) {
	state := defaultTagStyle()

	state = applyTag(state, "red")
	if state != (tagStyle{fg: "red", bg: "-", attrs: 0}) {
		t.Fatalf("unexpected state after single-field tag: %+v", state)
	}

	state = applyTag(state, ":blue:")
	if state != (tagStyle{fg: "red", bg: "blue", attrs: 0}) {
		t.Fatalf("expected an omitted foreground field to leave fg untouched, got %+v", state)
	}

	state = applyTag(state, "::b")
	if state != (tagStyle{fg: "red", bg: "blue", attrs: tcell.AttrBold}) {
		t.Fatalf("expected omitted fg/bg fields to leave them untouched, got %+v", state)
	}

	state = applyTag(state, "-:-:-")
	if state != defaultTagStyle() {
		t.Fatalf("expected a full \"-:-:-\" reset to restore the default style, got %+v", state)
	}
}

func TestApplyTagAccumulatesAttributeFlagsIncrementally(t *testing.T) {
	state := defaultTagStyle()

	state = applyTag(state, "::b")
	state = applyTag(state, "::i")

	if state.attrs&tcell.AttrBold == 0 {
		t.Fatalf("expected bold to remain active after a later, unrelated attribute tag, got %+v", state)
	}
	if state.attrs&tcell.AttrItalic == 0 {
		t.Fatalf("expected italic to also be active, got %+v", state)
	}
}

func TestApplyTagUppercaseRemovesOnlyThatAttribute(t *testing.T) {
	state := defaultTagStyle()
	state = applyTag(state, "::bi")

	state = applyTag(state, "::B") // remove bold only

	if state.attrs&tcell.AttrBold != 0 {
		t.Fatalf("expected bold removed, got %+v", state)
	}
	if state.attrs&tcell.AttrItalic == 0 {
		t.Fatalf("expected italic to remain active, got %+v", state)
	}
}

func TestApplyTagHardResetClearsAllAttributes(t *testing.T) {
	state := defaultTagStyle()
	state = applyTag(state, "::bi")

	state = applyTag(state, "::-")

	if state.attrs != 0 {
		t.Fatalf("expected a bare \"-\" attrs field to clear every attribute, got %+v", state)
	}
}

func TestApplyTagIgnoresUnrecognizedField(t *testing.T) {
	state := defaultTagStyle()

	got := applyTag(state, "not a real tag field")

	if got != state {
		t.Fatalf("expected an unrecognized field to leave state untouched, got %+v", got)
	}
}

func TestApplyTagRejectsWholeTagOnInvalidAttrsChar(t *testing.T) {
	// tview only recognizes buildsrBUILDSR in the attributes field; any
	// other character makes it reject the ENTIRE tag as invalid — "[red::x]"
	// renders as literal text with NO color change at all, not "red" with
	// an ignored attribute.
	state := defaultTagStyle()

	got := applyTag(state, "red::x")

	if got != state {
		t.Fatalf("expected the whole tag rejected (fg untouched) for an invalid attrs char, got %+v", got)
	}
}

func TestApplyTagAcceptsFourFieldURLTag(t *testing.T) {
	// tview's grammar allows an optional 4th field for a URL after
	// attributes — its content is unrestricted (anything up to the closing
	// "]") and must not cause fg/bg/attrs to be rejected.
	state := defaultTagStyle()

	got := applyTag(state, "red:blue:b:https://example.com")

	want := tagStyle{fg: "red", bg: "blue", attrs: tcell.AttrBold}
	if got != want {
		t.Fatalf("expected a valid 4-field URL tag to still apply fg/bg/attrs, got %+v, want %+v", got, want)
	}
}

func TestApplyTagAcceptsURLResetField(t *testing.T) {
	state := defaultTagStyle()

	got := applyTag(state, "red:blue:b:-")

	want := tagStyle{fg: "red", bg: "blue", attrs: tcell.AttrBold}
	if got != want {
		t.Fatalf("expected a \"-\" URL-reset field to still apply fg/bg/attrs, got %+v, want %+v", got, want)
	}
}

func TestAdvanceTagStyleSkipsEscapedLiteralBrackets(t *testing.T) {
	// tview.Escape turns a literal "[INFO]" into "[INFO[]" — our scanner
	// must recognize that trailing "[" as the escape marker and not treat
	// "INFO[" as a (nonsensical) color name.
	got := advanceTagStyle(defaultTagStyle(), "[red]actual color, then literal [INFO[]")

	if got.fg != "red" {
		t.Fatalf("expected the escaped literal to be skipped and the real tag still applied, got %+v", got)
	}
}

func TestLineStartStylesTracksColorSpanningMultipleLines(t *testing.T) {
	lines := []string{"before", "[red]red line one", "red line two[white]", "after"}

	states, end := lineStartStyles(defaultTagStyle(), lines)

	if states[0] != defaultTagStyle() {
		t.Fatalf("expected line 0 (before any tag) to start at the default style, got %+v", states[0])
	}
	if states[1] != defaultTagStyle() {
		t.Fatalf("expected line 1 to start at the default style — its own [red] tag hasn't taken effect yet, got %+v", states[1])
	}
	if states[2].fg != "red" {
		t.Fatalf("expected line 2 to start red — carried over from line 1's still-open [red], got %+v", states[2])
	}
	if states[3].fg != "white" {
		t.Fatalf("expected line 3 to start with fg=white — line 2's trailing [white] set it explicitly, got %+v", states[3])
	}
	if end.fg != "white" {
		t.Fatalf("expected the final resulting style to have fg=white, got %+v", end)
	}
}

func TestRenderLinesNumberedRestoresColorCarriedFromEarlierLine(t *testing.T) {
	lines := []string{"before", "[red]red line one", "red line two[white]", "after"}
	states, _ := lineStartStyles(defaultTagStyle(), lines)

	got := renderLines(lines, 1, "", true, states)

	want := fmt.Sprintf("[gray]%*d[-:-:-] before", lineNumberWidth, 1) + "\n" +
		fmt.Sprintf("[gray]%*d[-:-:-] [red]red line one", lineNumberWidth, 2) + "\n" +
		fmt.Sprintf("[gray]%*d[red:-:-] red line two[white]", lineNumberWidth, 3) + "\n" +
		fmt.Sprintf("[gray]%*d[white:-:-] after", lineNumberWidth, 4)
	if got != want {
		t.Fatalf("unexpected numbered rendering with a color spanning two lines:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewStreamLineNumbersRestoreColorSpanningAppendBatches(t *testing.T) {
	d := NewDetailView()
	d.SetRect(0, 0, 60, 10)
	d.StartStream()
	d.SetLineNumbersEnabled(true)

	d.AppendStream("[red]red line one\n")
	d.AppendStream("red line two[white]\n")

	got := d.GetText(false) // raw markup, tags NOT stripped
	wantSecondLine := fmt.Sprintf("[gray]%*d[red:-:-] red line two[white]", lineNumberWidth, 2)
	if !strings.Contains(got, wantSecondLine) {
		t.Fatalf("expected the second AppendStream batch to restore the color opened by the first batch:\ngot:  %q\nwant substring: %q", got, wantSecondLine)
	}
}

func TestDetailViewLineNumbersPreserveMultiLineSpanningStyleOnScreen(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	screen.SetSize(80, 10)

	d := NewDetailView()
	d.SetRect(0, 0, 80, 10)
	d.SetData(resource.Detail{Body: "before\n[red]red line one\nred line two[white]\nafter\n"})
	d.SetLineNumbersEnabled(true)
	d.Draw(screen)

	// "red line two" is the 3rd line (0-indexed row 2); its content starts
	// right after the "   3 " gutter, at column lineNumberWidth+1.
	_, _, style, _ := screen.GetContent(lineNumberWidth+1, 2)
	fg, _, _ := style.Decompose()
	if want := tcell.ColorNames["red"]; fg != want {
		t.Fatalf("expected the continuation line still rendered in red (%v), got %v", want, fg)
	}
}

// TestRenderLinesRestoresFullAccumulatedAttributesWhenOpenersAreFiltered
// covers "[::b]" then, later, "[::i]" — bold+italic together — if a filter
// hides both opener lines and keeps only a later continuation line, the
// restored style on that surviving line must still carry BOTH attributes,
// not just whichever one's tag came last.
func TestRenderLinesRestoresFullAccumulatedAttributesWhenOpenersAreFiltered(t *testing.T) {
	lines := []string{
		"[::b]bold starts",
		"[::i]bold and italic now",
		"still bold and italic",
	}
	states, _ := lineStartStyles(defaultTagStyle(), lines)

	// Matches only the third line's stripped text. The match itself
	// ("still bold", 10 runes) is long enough to also get highlighted (see
	// highlightMatchMinLength), restoring the same bold+italic style right
	// after it.
	got := renderLines(lines, 1, "still bold", true, states)

	want := fmt.Sprintf("[gray]%*d[-:-:-][::bi] [::r]still bold[-:-:-][::bi] and italic", lineNumberWidth, 3)
	if got != want {
		t.Fatalf("unexpected rendering with filtered-out attribute openers:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDetailViewFilteredNumberedLineRestoresFullAttributeSetOnScreen(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	screen.SetSize(80, 10)

	d := NewDetailView()
	d.SetRect(0, 0, 80, 10)
	d.SetData(resource.Detail{Body: "[::b]bold starts\n[::i]bold and italic now\nstill bold and italic\n"})
	d.SetLineNumbersEnabled(true)
	d.SetFilterQuery("still bold")
	d.Draw(screen)

	// The filter hides both opener lines, leaving the surviving line as the
	// only (and therefore first) row on screen.
	_, _, style, _ := screen.GetContent(lineNumberWidth+1, 0)
	_, _, attrs := style.Decompose()
	if attrs&tcell.AttrBold == 0 {
		t.Fatalf("expected the surviving line to still render bold (opened two filtered-out lines earlier), attrs=%v", attrs)
	}
	if attrs&tcell.AttrItalic == 0 {
		t.Fatalf("expected the surviving line to still render italic too, attrs=%v", attrs)
	}
}

func TestHighlightMatchesLeavesShortQueriesUnhighlighted(t *testing.T) {
	line := "an error occurred"
	got := highlightMatches(line, "er", defaultTagStyle()) // 2 runes — at highlightMatchMinLength, not over it
	if got != line {
		t.Fatalf("expected a 2-rune query to be left unhighlighted, got %q", got)
	}
}

func TestHighlightMatchesWrapsCaseInsensitiveMatchAndRestoresStyle(t *testing.T) {
	got := highlightMatches("an ERROR occurred", "error", defaultTagStyle())
	want := "an [::r]ERROR[-:-:-] occurred"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHighlightMatchesWrapsEveryOccurrence(t *testing.T) {
	got := highlightMatches("error: error again", "error", defaultTagStyle())
	want := "[::r]error[-:-:-]: [::r]error[-:-:-] again"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHighlightMatchesRestoresNonDefaultStyle(t *testing.T) {
	style := tagStyle{fg: "green", bg: "-", attrs: tcell.AttrBold}
	got := highlightMatches("still bold and italic", "still bold", style)
	want := "[::r]still bold[green:-:-][::b] and italic"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHighlightMatchesSkipsMatchesInsideStyleTags(t *testing.T) {
	// "green" only appears inside the color tag itself, never in the
	// line's literal text — highlightMatches must not touch tag content.
	line := "[green]completed[white]"
	got := highlightMatches(line, "green", defaultTagStyle())
	if got != line {
		t.Fatalf("expected a query matching only inside a style tag to leave the line untouched, got %q", got)
	}
}

func TestRenderLinesOnlyHighlightsWhenQueryIsActive(t *testing.T) {
	lines := []string{"an error occurred"}
	states, _ := lineStartStyles(defaultTagStyle(), lines)

	got := renderLines(lines, 1, "", false, states)
	if got != lines[0] {
		t.Fatalf("expected an empty query to leave lines unhighlighted, got %q", got)
	}
}

func TestDetailViewFilterHighlightsMatchOnScreen(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	screen.SetSize(80, 10)

	d := NewDetailView()
	d.SetRect(0, 0, 80, 10)
	d.SetData(resource.Detail{Body: "an error occurred\nsomething else\n"})
	d.SetFilterQuery("error")
	d.Draw(screen)

	// "an error occurred" — 'e' of "error" starts at column 3.
	_, _, style, _ := screen.GetContent(3, 0)
	_, _, attrs := style.Decompose()
	if attrs&tcell.AttrReverse == 0 {
		t.Fatalf("expected the matched substring to render reverse-video, attrs=%v", attrs)
	}

	// "an " (before the match) must NOT be reverse-video.
	_, _, style, _ = screen.GetContent(0, 0)
	_, _, attrs = style.Decompose()
	if attrs&tcell.AttrReverse != 0 {
		t.Fatalf("expected text before the match to render normally, attrs=%v", attrs)
	}
}
