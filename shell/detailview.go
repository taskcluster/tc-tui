package shell

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/taskcluster/tc-tui/resource"
)

// streamViewMaxLines bounds the TextView's buffer while a live stream is
// appending to it (see StartStream) — a long-running task's log grows
// without limit, and unlike a one-shot render there's no
// maxArtifactRenderBytes gate in front of the view. Oldest lines are
// dropped; 'o'/'s' remain the full-content escape hatches. streamLines is
// bounded the same way, so a '/' filter applied mid-stream (or after it
// ends) only ever has to search/rebuild from the same bounded window
// that's actually on screen, never an unbounded log.
const streamViewMaxLines = 10000

// DetailView renders a Resource.Describe(id) result. Any DetailActions the
// Detail carries are dispatched by Shell.globalInputCapture (shared with a
// List view's ScopeActions), not by DetailView itself.
type DetailView struct {
	*tview.TextView

	wrapEnabled bool

	// lineNumbersEnabled toggles a vim-like "set number" gutter (the 'n'
	// key) — like wrapEnabled, it's a display preference that survives
	// SetData/StartStream, not per-body state.
	lineNumbersEnabled bool

	// rawBody is the last SetData/UpdateData body, before any '/' filter is
	// applied — filtering always re-derives from this, never from a
	// previously-filtered rendering, so widening/clearing the query can
	// always recover lines it had hidden.
	rawBody string

	// filterQuery is the active '/' line filter — shared meaning for both a
	// one-shot body (rawBody) and a live stream (streamLines), but tracked
	// independently of the Shell's own list-filter state so filtering a list
	// can never leak into a detail view navigated to afterward, or vice
	// versa (list filters are already scoped per-resource for the same
	// reason).
	filterQuery string

	// streaming is set once StartStream has been called and never reset —
	// even after the stream itself ends, filtering should keep working
	// against the accumulated streamLines rather than switching back to the
	// (empty) rawBody path.
	streaming   bool
	streamLines []string

	// streamLineTotal counts every line ever appended to the stream, never
	// decremented by streamLines' own capping — it's what lets a numbered
	// gutter show a retained line's TRUE original position even once older
	// lines have been dropped from streamLines (see AppendStream).
	streamLineTotal int

	// streamBanner is the most recent AppendBanner text, if any — rebuildStreamView
	// re-appends it after redrawing from streamLines, since a banner is UI
	// chrome rather than a retained line and would otherwise be silently
	// dropped by any rebuild (a '/' filter change, or a line-numbers toggle)
	// that happens after the stream has already ended.
	streamBanner string

	// streamTailStyle is the tagStyle active right after the last line ever
	// appended (regardless of streamLines' own capping) — the seed AppendStream
	// advances forward through each new batch, so a color/attribute opened in
	// one AppendStream call and left open correctly carries into the next
	// call's own numbered rendering.
	streamTailStyle tagStyle

	// streamBoundaryStyle is the tagStyle active right before streamLines[0]
	// — advanced forward through whatever lines AppendStream drops from the
	// front once the retained window exceeds streamViewMaxLines, mirroring
	// how streamLineTotal keeps counting through drops. This is what lets
	// rebuildStreamContent recompute correct per-line restore styles across
	// the WHOLE retained window, not just from a wrongly-assumed default at
	// streamLines[0].
	streamBoundaryStyle tagStyle
}

func NewDetailView() *DetailView {
	d := &DetailView{
		TextView:    tview.NewTextView(),
		wrapEnabled: true,
	}
	d.SetDynamicColors(true).SetWordWrap(true)

	return d
}

// WrapEnabled reports whether the body wraps at the view's width (the
// default) or, once toggled off via the 'x' key, runs lines out unbroken —
// reachable with Left/Right/h/l, tview.TextView's built-in horizontal scroll.
func (d *DetailView) WrapEnabled() bool {
	return d.wrapEnabled
}

// SetWrapEnabled flips between word-wrapping the body (default) and leaving
// long lines unbroken so they can be scrolled horizontally instead — useful
// for content with long unbroken lines (URLs, wide JSON) that word-wrap would
// otherwise force onto multiple lines.
func (d *DetailView) SetWrapEnabled(enabled bool) {
	d.wrapEnabled = enabled
	d.SetWrap(enabled)
}

func (d *DetailView) SetData(detail resource.Detail) {
	d.rawBody = detail.Body
	d.filterQuery = ""
	d.streaming = false
	d.streamLines = nil
	d.SetMaxLines(0) // clear any StartStream bound — one-shot bodies are pre-capped
	d.Clear().SetText(d.filteredBody()).ScrollToBeginning()
}

// UpdateData replaces the body like SetData, but preserves the current
// scroll position — used when refreshing the view already on screen (as
// opposed to navigating to a new one), so a periodic auto-refresh or the
// manual `r` key doesn't yank the reader back to the top. Any active '/'
// filter carries over and is reapplied to the refreshed body, matching how a
// list view's filter survives its own auto-refresh.
func (d *DetailView) UpdateData(detail resource.Detail) {
	row, col := d.GetScrollOffset()
	d.rawBody = detail.Body
	d.SetMaxLines(0)
	d.Clear().SetText(d.filteredBody())
	d.ScrollTo(row, col)
}

// FilterQuery returns the active '/' line filter, if any — used both to
// prefill the footer input when '/' reopens it and to drive the detail
// title's "(query)" suffix and border tint, mirroring the list view's own
// filter affordances.
func (d *DetailView) FilterQuery() string {
	return d.filterQuery
}

// SetFilterQuery re-renders the body keeping only lines whose visible text
// contains query (case-insensitive substring) — the '/' key's behavior on a
// detail page, consistent with how it narrows rows in a list view. Matching
// is against each line's STRIPPED (tag-free) text, not its raw markup, so a
// query can't accidentally match inside a `[green]`-style color tag. An
// empty query restores every line.
func (d *DetailView) SetFilterQuery(query string) {
	d.filterQuery = query
	if d.streaming {
		d.rebuildStreamView()
		return
	}
	d.Clear().SetText(d.filteredBody()).ScrollToBeginning()
}

// filteredBody re-derives the displayed text from rawBody: splitLines
// discards the single empty trailing element strings.Split would otherwise
// produce for a body ending in "\n" (which would render as a numbered but
// content-less phantom last line — see renderLines) — that trailing newline
// is added back afterward instead, but only when the query is empty. An
// active query already drops that phantom element on its own (an empty
// string never contains a non-empty needle), so re-adding a trailing
// newline there would introduce one that was never in the filtered output.
func (d *DetailView) filteredBody() string {
	lines := splitLines(d.rawBody)
	lineStates, _ := lineStartStyles(defaultTagStyle(), lines)
	rendered := renderLines(lines, 1, d.filterQuery, d.lineNumbersEnabled, lineStates)
	if d.filterQuery == "" && strings.HasSuffix(d.rawBody, "\n") {
		rendered += "\n"
	}
	return rendered
}

// LineNumbersEnabled reports whether the 'n' key's vim-like "set number"
// gutter is showing each line's original position — see SetLineNumbersEnabled.
func (d *DetailView) LineNumbersEnabled() bool {
	return d.lineNumbersEnabled
}

// SetLineNumbersEnabled toggles a line-number gutter on the body, most useful
// alongside a '/' filter: a query that hides most of a log still leaves each
// surviving line's original position visible, rather than a plain sequential
// count of what's left on screen. Scroll position — AND, while streaming,
// tview's own tail-follow tracking — is left completely untouched: tview's
// Clear/SetText never touch either (only an explicit ScrollTo/ScrollToEnd
// call does), so simply not calling either here is what preserves both a
// detached scroll position and an active tail-follow rather than forcing one
// or the other. This is why streaming uses rebuildStreamContent (content
// only) rather than rebuildStreamView (which deliberately reattaches to the
// tail for a '/' filter change — not appropriate here, since a reader
// scrolled up to inspect older output shouldn't get yanked to the bottom
// just for toggling the gutter).
func (d *DetailView) SetLineNumbersEnabled(enabled bool) {
	d.lineNumbersEnabled = enabled
	if d.streaming {
		d.rebuildStreamContent()
		return
	}
	d.Clear().SetText(d.filteredBody())
}

// StartStream prepares the view for a live stream: empty content, a line
// bound on the buffer, and follow mode — tview's ScrollToEnd keeps the view
// pinned to the newest line as content is appended, detaching when the user
// scrolls up and re-attaching when they return to the bottom, i.e. `tail -f`
// semantics for free. Any filter left over from a previous view is dropped —
// this is a fresh log.
func (d *DetailView) StartStream() {
	d.filterQuery = ""
	d.streaming = true
	d.streamLines = nil
	d.streamLineTotal = 0
	d.streamBanner = ""
	d.streamTailStyle = defaultTagStyle()
	d.streamBoundaryStyle = defaultTagStyle()
	d.Clear()
	d.SetMaxLines(streamViewMaxLines)
	d.ScrollToEnd()
}

// AppendStream appends already-rendered text to the view — content is
// expected to consist of whole, newline-terminated lines (guaranteed by the
// LiveStreamer's own line assembly), except possibly the very last call at
// stream end, which may flush a final unterminated line. Every line is
// recorded into streamLines (capped at streamViewMaxLines, oldest dropped)
// so a '/' filter applied mid-stream or after it ends has the full retained
// window to search, even lines already scrolled out of the visible buffer.
// While a filter is active, only matching lines are actually written to the
// view; scroll/follow state is otherwise untouched.
func (d *DetailView) AppendStream(text string) {
	lines := splitLines(text)
	startNumber := d.streamLineTotal + 1
	d.streamLineTotal += len(lines)

	// Always advance streamTailStyle, even when numbering is currently off
	// — it may be toggled on later, at which point the numbered rendering
	// needs an accurate carried-over style from everything appended so far,
	// not just from whenever the toggle happened to flip.
	lineStates, tailStyle := lineStartStyles(d.streamTailStyle, lines)
	d.streamTailStyle = tailStyle

	d.streamLines = append(d.streamLines, lines...)
	if excess := len(d.streamLines) - streamViewMaxLines; excess > 0 {
		// Advance streamBoundaryStyle through the lines about to be
		// dropped, so it keeps reflecting the style active right before
		// the new streamLines[0] — mirrors streamLineTotal's own
		// keep-counting-through-drops approach.
		_, d.streamBoundaryStyle = lineStartStyles(d.streamBoundaryStyle, d.streamLines[:excess])
		d.streamLines = d.streamLines[excess:]
	}

	if d.filterQuery == "" && !d.lineNumbersEnabled {
		fmt.Fprint(d.TextView, text)
		return
	}
	if rendered := renderLines(lines, startNumber, d.filterQuery, d.lineNumbersEnabled, lineStates); rendered != "" {
		fmt.Fprint(d.TextView, rendered+"\n")
	}
}

// AppendBanner appends a status line (stream ended/failed/truncated) that
// bypasses filtering entirely — it's UI chrome, not log content, so it
// should never be silently hidden by a '/' query that happens not to match
// it, and it's deliberately not recorded into streamLines, so a filter
// change afterward won't reproduce or duplicate it. Recorded into
// streamBanner so a later rebuildStreamView (a filter change, or a
// line-numbers toggle) can restore it instead of silently dropping it.
func (d *DetailView) AppendBanner(text string) {
	d.streamBanner = text
	fmt.Fprint(d.TextView, text)
}

// rebuildStreamContent redraws the view's text from streamLines under the
// current filter/numbering settings and re-appends the end-of-stream banner
// if one was ever shown (see AppendBanner), WITHOUT touching scroll or
// tail-follow state — tview's Clear/SetText never touch either (see
// SetLineNumbersEnabled's doc comment), and neither does writing straight
// into the TextView the way AppendStream itself already does.
func (d *DetailView) rebuildStreamContent() {
	d.Clear()
	startNumber := d.streamLineTotal - len(d.streamLines) + 1
	lineStates, _ := lineStartStyles(d.streamBoundaryStyle, d.streamLines)
	if rendered := renderLines(d.streamLines, startNumber, d.filterQuery, d.lineNumbersEnabled, lineStates); rendered != "" {
		fmt.Fprint(d.TextView, rendered+"\n")
	}
	if d.streamBanner != "" {
		fmt.Fprint(d.TextView, d.streamBanner)
	}
}

// rebuildStreamView is rebuildStreamContent plus re-attaching to the tail —
// used whenever the '/' query changes while streaming (or after a stream has
// ended), since unlike AppendStream's incremental path, a query CHANGE can
// both re-reveal previously-hidden lines and hide previously-visible ones,
// so the whole buffer must be redrawn rather than patched. Re-attaching here
// mirrors StartStream's own tail-follow default; callers where that would be
// wrong (e.g. a line-numbers toggle, which must preserve whatever follow
// state was already in effect) call rebuildStreamContent directly instead.
func (d *DetailView) rebuildStreamView() {
	d.rebuildStreamContent()
	d.ScrollToEnd()
}

// filterLineIndices returns the indices into lines whose stripped (tag-free)
// text contains query (case-insensitive substring), preserving order —
// shared by the one-shot body filter and the live-stream filter. An empty
// query keeps every index, so callers can use the result uniformly whether
// or not a filter is active. Returning indices rather than the lines
// themselves is what lets a caller pair a kept line back up with its
// original line number once a query has hidden the lines around it.
func filterLineIndices(lines []string, query string) []int {
	indices := make([]int, len(lines))
	for i := range lines {
		indices[i] = i
	}
	if query == "" {
		return indices
	}

	stripped := strings.Split(stripDisplayTags(strings.Join(lines, "\n")), "\n")
	needle := strings.ToLower(query)

	var kept []int
	for _, i := range indices {
		if i < len(stripped) && strings.Contains(strings.ToLower(stripped[i]), needle) {
			kept = append(kept, i)
		}
	}
	return kept
}

// lineNumberWidth is how many columns the "set number" gutter's digits are
// padded to — wide enough that a stream's retained window (up to
// streamViewMaxLines lines) never shifts the gutter's alignment mid-scroll.
const lineNumberWidth = 4

// renderLines filters lines by query (see filterLineIndices) and joins the
// survivors with newlines, optionally prefixing each with its original
// 1-based line number — start+i, not its position among the survivors — so
// numbering stays meaningful once a filter has hidden the lines around it.
// start is the original line number of lines[0], letting a streamed batch or
// a truncated retained window number its lines by true absolute position
// rather than restarting from 1. lineStates[i] is the tagStyle active at the
// start of lines[i] (see lineStartStyles) — re-emitted right after the
// number instead of a hardcoded reset, so a color/attribute that legitimately
// spans multiple lines (e.g. ANSI/Chroma-highlighted output with no per-line
// reset) carries through a numbered line instead of being clobbered to white.
func renderLines(lines []string, start int, query string, numbered bool, lineStates []tagStyle) string {
	kept := filterLineIndices(lines, query)
	rendered := make([]string, len(kept))
	for j, i := range kept {
		line := lines[i]
		if query != "" {
			line = highlightMatches(line, query, lineStates[i])
		}
		if numbered {
			rendered[j] = fmt.Sprintf("[gray]%*d%s %s", lineNumberWidth, start+i, lineStates[i].tag(), line)
		} else {
			rendered[j] = line
		}
	}
	return strings.Join(rendered, "\n")
}

// highlightMatchMinLength is the minimum query length (in runes) a '/'
// filter query must reach before surviving lines get their matches visually
// highlighted, on top of just being kept — a 1-2 character query matches so
// much of ordinary text that highlighting every hit would be noise rather
// than a useful pointer to where the match actually is.
const highlightMatchMinLength = 2

// highlightMatches wraps every case-insensitive occurrence of query within
// line's literal (non-tag) text in a reverse-video tag, restoring style (the
// tagStyle active at the very start of line — see lineStartStyles)
// immediately after each match. Reverse video rather than a fixed color, so
// a match stays visible regardless of the line's own color (plain text,
// chroma-highlighted JSON, ANSI log output, ...) and the terminal's palette.
// Segments are split on style-tag boundaries first (mirroring
// advanceTagStyle) so a match can't be found straddling a real tag — the
// common case (plain surrounding text) is unaffected, and a match that
// happens to sit right at a color change simply isn't highlighted, same as
// filterLineIndices already can't tell such a line apart from any other
// match when deciding whether to keep it.
func highlightMatches(line, query string, style tagStyle) string {
	if utf8.RuneCountInString(query) <= highlightMatchMinLength {
		return line
	}

	var sb strings.Builder
	state := style
	pos := 0
	for _, m := range bracketPattern.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		sb.WriteString(highlightSegment(line[pos:start], query, state))
		tagText := line[start:end]
		sb.WriteString(tagText)
		if !isEscapedTag(tagText) {
			state = applyTag(state, tagText[1:len(tagText)-1])
		}
		pos = end
	}
	sb.WriteString(highlightSegment(line[pos:], query, state))
	return sb.String()
}

// highlightSegment wraps every case-insensitive occurrence of query within
// segment — a run of literal text with no style tags of its own — in a
// reverse-video tag, restoring style right after each one.
func highlightSegment(segment, query string, style tagStyle) string {
	lowerSeg := strings.ToLower(segment)
	lowerQuery := strings.ToLower(query)
	if segment == "" || !strings.Contains(lowerSeg, lowerQuery) {
		return segment
	}

	var sb strings.Builder
	i := 0
	for {
		idx := strings.Index(lowerSeg[i:], lowerQuery)
		if idx < 0 {
			sb.WriteString(segment[i:])
			break
		}
		idx += i
		sb.WriteString(segment[i:idx])
		sb.WriteString("[::r]")
		sb.WriteString(segment[idx : idx+len(lowerQuery)])
		sb.WriteString(style.tag())
		i = idx + len(lowerQuery)
	}
	return sb.String()
}

// tagStyle tracks the tview style-tag fields (foreground, background,
// attributes) active at some point in a body — used by the line-number
// gutter to reconstruct and re-emit the exact style that was active before
// the gutter's own "[gray]" tag, rather than resetting it to a hardcoded
// color. fg/bg are each either "-" (an explicit reset to the view's initial
// style, matching tview's own tag semantics for a literal "-") or a real
// value (a color name or "#rrggbb" hex) last set by a genuine tag — never ""
// ("unset"): re-emitting an empty field means "inherit whatever's currently
// active" in tview's own grammar, which right after our own "[gray]" tag
// would just mean "stay gray". attrs is the fully RESOLVED bitmask of
// currently-active attributes — tview's own attribute tag field is
// incremental (lowercase letters add a bit, uppercase remove exactly that
// bit, layered on whatever was already active — see applyAttrField), not a
// replace, so tracking it as raw tag text would lose earlier bits a later
// tag doesn't mention (e.g. "[::b]" then "[::i]" is bold+italic together,
// not just italic).
type tagStyle struct {
	fg, bg string
	attrs  tcell.AttrMask
}

// defaultTagStyle is the style active before any tag has been seen — a full
// reset to the view's own initial style, no attributes active.
func defaultTagStyle() tagStyle {
	return tagStyle{fg: "-", bg: "-", attrs: 0}
}

// attrLetterOrder fixes a deterministic emission order for tag, mirroring
// tview's own attribute-letter table (strings.go's tagStateStartAttributes).
var attrLetterOrder = []struct {
	letter byte
	bit    tcell.AttrMask
}{
	{'b', tcell.AttrBold},
	{'i', tcell.AttrItalic},
	{'l', tcell.AttrBlink},
	{'d', tcell.AttrDim},
	{'s', tcell.AttrStrikeThrough},
	{'r', tcell.AttrReverse},
	{'u', tcell.AttrUnderline},
}

// attrBit resolves an uppercase attribute-tag letter (e.g. 'B') to its tcell
// bit, mirroring tview's own attrs map in ansi.go/strings.go.
func attrBit(upper byte) (tcell.AttrMask, bool) {
	for _, e := range attrLetterOrder {
		if e.letter-('a'-'A') == upper {
			return e.bit, true
		}
	}
	return 0, false
}

// tag renders s as tag text that, re-emitted into the body, restores exactly
// this style. attrs can't be expressed as a single field alongside fg/bg —
// tview's grammar only allows an attrs field to be EITHER a bare "-" reset OR
// a run of add/remove letters, never both in one field — so a non-empty
// attrs set is emitted as a SECOND tag: one to reset fg/bg/attrs together,
// then one that adds back exactly the resolved bits, so the result doesn't
// depend on replaying the tag history that produced it.
func (s tagStyle) tag() string {
	reset := fmt.Sprintf("[%s:%s:-]", s.fg, s.bg)
	if s.attrs == 0 {
		return reset
	}
	var letters strings.Builder
	for _, e := range attrLetterOrder {
		if s.attrs&e.bit != 0 {
			letters.WriteByte(e.letter)
		}
	}
	return reset + "[::" + letters.String() + "]"
}

// bracketPattern matches a candidate "[...]" run with no nested "]" —
// covers both a genuine tview style tag and one of tview.Escape's own
// escaped-literal sequences (see isEscapedTag).
var bracketPattern = regexp.MustCompile(`\[[^\]]*\]`)

// colorFieldPattern matches a syntactically plausible foreground/background
// field: a color name (letter-led alnum, per tview's tagStateNameForeground/
// tagStateNameBackground) or a "#rrggbb" hex color. An empty field ("leave
// unchanged") and "-" ("reset") are checked separately.
var colorFieldPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$|^#[0-9a-fA-F]{6}$`)

// attrsFieldPattern matches tview's actual attribute-tag charset —
// strings.go's tagStateStartAttributes/tagStateAttributes recognize ONLY
// "buildsrBUILDSR"; any other character makes tview reject the WHOLE tag as
// invalid (rendered as literal text, no style change at all — not even the
// tag's own fg/bg fields), e.g. "[red::x]" is literal text to tview, not
// "red" with an ignored attribute.
var attrsFieldPattern = regexp.MustCompile(`^[biuldsrBIULDSR]*$`)

// isEscapedTag reports whether match (the full "[...]" text, brackets
// included) is one of tview.Escape's own escaped-literal sequences — e.g.
// "[INFO[]" for a literal "[INFO]" — rather than a real style tag.
// tview.Escape always appends one or more extra "[" immediately before the
// final "]", which no real tag field value can ever contain.
func isEscapedTag(match string) bool {
	inner := match[1 : len(match)-1]
	return strings.HasSuffix(inner, "[")
}

// validTagField reports whether s is a syntactically plausible value for a
// tag field, using field-specific grammar: fieldIndex 0/1 (foreground/
// background) accept a color name or hex; fieldIndex 2 (attributes) accepts
// only tview's own attribute-letter charset. An empty field ("leave
// unchanged") and "-" ("reset") are valid for either kind.
func validTagField(s string, fieldIndex int) bool {
	if s == "" || s == "-" {
		return true
	}
	if fieldIndex == 2 {
		return attrsFieldPattern.MatchString(s)
	}
	return colorFieldPattern.MatchString(s)
}

// applyAttrField updates mask per tview's own incremental attribute-tag
// grammar (strings.go's tagStateAttributes): "-" alone is a hard reset to no
// attributes; otherwise each letter adds (lowercase) or removes (uppercase)
// exactly that one attribute bit, layered on whatever was already active —
// never a wholesale replace of the set.
func applyAttrField(mask tcell.AttrMask, field string) tcell.AttrMask {
	if field == "-" {
		return 0
	}
	for i := 0; i < len(field); i++ {
		ch := field[i]
		switch {
		case ch >= 'a' && ch <= 'z':
			if bit, ok := attrBit(ch - ('a' - 'A')); ok {
				mask |= bit
			}
		case ch >= 'A' && ch <= 'Z':
			if bit, ok := attrBit(ch); ok {
				mask &^= bit
			}
		}
	}
	return mask
}

// applyTag updates state according to tagContent's up-to-four
// colon-separated fields (foreground:background:attributes:url), leaving a
// field untouched if it's absent or empty — mirroring tview's own "omitted
// field = unchanged" tag grammar. Returns state unchanged if tagContent
// doesn't look like a real, valid style tag (validated per-field, since
// tview's own grammar differs for colors vs. attributes — see
// validTagField), so a stray "[...]" that merely resembles one can't
// corrupt tracked style. The optional 4th field is a URL, whose content is
// unrestricted in tview's grammar (anything up to the tag's closing "]") and
// irrelevant to fg/bg/attrs tracking — skipped rather than validated, so its
// presence can't cause an otherwise-valid tag to be wrongly rejected.
func applyTag(state tagStyle, tagContent string) tagStyle {
	fields := strings.SplitN(tagContent, ":", 4)
	for i, f := range fields {
		if i == 3 {
			break // URL field — unrestricted content, not validated
		}
		if !validTagField(f, i) {
			return state
		}
	}
	if len(fields) >= 1 && fields[0] != "" {
		state.fg = fields[0]
	}
	if len(fields) >= 2 && fields[1] != "" {
		state.bg = fields[1]
	}
	if len(fields) >= 3 && fields[2] != "" {
		state.attrs = applyAttrField(state.attrs, fields[2])
	}
	return state
}

// advanceTagStyle scans text for real style tags (skipping tview.Escape's
// escaped-literal ones — see isEscapedTag) and returns the resulting style.
func advanceTagStyle(state tagStyle, text string) tagStyle {
	for _, m := range bracketPattern.FindAllString(text, -1) {
		if isEscapedTag(m) {
			continue
		}
		state = applyTag(state, m[1:len(m)-1])
	}
	return state
}

// lineStartStyles returns, for each of lines, the tagStyle active at the very
// start of that line — before any of ITS OWN tags take effect — given start
// as the style active immediately before lines[0]. end is the resulting
// style after the last line, letting a caller carry it forward across a
// later batch (AppendStream's streamTailStyle) or a dropped-lines boundary
// (AppendStream's streamBoundaryStyle). This is what lets the line-number
// gutter restore a color/attribute that legitimately spans multiple lines
// instead of clobbering it to a hardcoded default on every numbered line
// after the first.
func lineStartStyles(start tagStyle, lines []string) (perLine []tagStyle, end tagStyle) {
	perLine = make([]tagStyle, len(lines))
	state := start
	for i, line := range lines {
		perLine[i] = state
		state = advanceTagStyle(state, line)
	}
	return perLine, state
}

// splitLines splits already-assembled stream text into individual lines,
// tolerating both a trailing newline (the common case) and none (the final
// flush at stream end may hand back an unterminated last line). Empty text
// yields no lines. Trims at most ONE trailing newline — the batch's own line
// terminator — rather than every trailing newline: a batch ending in "\n\n"
// (e.g. a genuinely blank line followed by the terminator) must keep that
// blank line as a real entry, not have it collapsed away along with the
// terminator.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// stripDisplayTags strips tview's dynamic-color tags from text, using a
// scratch TextView (configured exactly like the real one) rather than
// reimplementing tview's own tag grammar — used so a '/' filter query is
// matched against what's actually on screen (e.g. "completed", not
// "[green]completed[white]"), not the raw markup.
func stripDisplayTags(text string) string {
	scratch := tview.NewTextView().SetDynamicColors(true)
	scratch.SetText(text)
	return scratch.GetText(true)
}
