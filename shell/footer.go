package shell

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (s *Shell) initFooter() {
	s.footerBreadcrumb = tview.NewTextView().SetDynamicColors(true)
	s.footerInput = tview.NewInputField().SetFieldWidth(0)
	s.footerInput.SetDoneFunc(s.handleFooterInputDone)
	s.footerInput.SetChangedFunc(s.handleFooterInputChanged)

	s.footer = tview.NewPages().
		AddPage(pageFooterBreadcrumb, s.footerBreadcrumb, true, true).
		AddPage(pageFooterInput, s.footerInput, true, false)

	s.renderHeaderHints()
	s.renderBreadcrumbs()
}

// hintColumns caps how many hints share a line in the header's center
// column, laid out as a left-aligned grid (k9s-style) rather than relying
// on the terminal's own wrapping, which would break a hint mid-string (e.g.
// splitting "Esc" from "back/quit") on a narrow terminal.
const hintColumns = 3

// headerHintGap is the minimum number of spaces separating one hint column
// from the next.
const headerHintGap = 3

// hint pairs a hint's plain-text form (used to measure column width, since
// tview's [color] region tags aren't rendered) with its colored form
// (what's actually shown).
type hint struct {
	plain   string
	colored string
}

// renderHeaderHints rebuilds the header's center hint column: global keys,
// the filter hint (`/` narrows a list's rows, or a detail body's lines), the
// facet-switch hint when the current list has facets, and any
// per-detail-action keys the current detail screen exposes. Hints are laid
// out as a left-aligned grid of hintColumns columns, each padded to the
// width of the longest hint, rather than centered free text.
func (s *Shell) renderHeaderHints() {
	hints := []hint{
		{"q quit", "[yellow]q[white] quit"},
		{": command", "[yellow]:[white] command"},
		{"Ctrl-A commands", "[yellow]Ctrl-A[white] commands"},
		{"r refresh", "[yellow]r[white] refresh"},
		{"Esc back", "[yellow]Esc[white] back"},
		{"? help", "[yellow]?[white] help"},
	}
	if top, ok := s.stack.Top(); !ok || top.Kind == ListKind {
		hints = append(hints, hint{"/ filter", "[yellow]/[white] filter"})
		hints = append(hints, hint{"x truncate", "[yellow]x[white] truncate"})
		if s.currentListTruncated {
			hints = append(hints, hint{"L load all", "[yellow]L[white] load all"})
		}
	} else if top.Kind == DetailKind {
		hints = append(hints, hint{"/ filter", "[yellow]/[white] filter"})
		hints = append(hints, hint{"x wrap", "[yellow]x[white] wrap"})
		hints = append(hints, hint{"n line numbers", "[yellow]n[white] line numbers"})
	}
	if s.hasFacets() {
		hints = append(hints, hint{"Tab/Shift+Tab switch state", "[yellow]Tab[white]/[yellow]Shift+Tab[white] switch state"})
	}
	if url, ok := s.currentWebURL(); ok && url != "" {
		hints = append(hints, hint{"o open in browser", "[yellow]o[white] open in browser"})
	}
	if _, _, _, ok := s.currentDownloadable(); ok {
		hints = append(hints, hint{"s save", "[yellow]s[white] save"})
	}
	for _, action := range s.currentDetailActions {
		hints = append(hints, hint{
			plain:   fmt.Sprintf("%c %s", action.Key, action.Label),
			colored: fmt.Sprintf("[yellow]%c[white] %s", action.Key, action.Label),
		})
	}
	// Mutating actions get a red key so an authenticated/destructive
	// operation reads differently at a glance from a plain navigation hint.
	for _, action := range s.currentActions {
		hints = append(hints, hint{
			plain:   fmt.Sprintf("%c %s", action.Key, action.Label),
			colored: fmt.Sprintf("[red]%c[white] %s", action.Key, action.Label),
		})
	}

	// Each column is only as wide as the widest hint that actually falls in
	// it, not the widest hint overall — otherwise one long hint (e.g. the
	// facet-switch hint) in one column would pad every column to its width.
	colWidth := make([]int, hintColumns)
	for i, h := range hints {
		col := i % hintColumns
		if len(h.plain) > colWidth[col] {
			colWidth[col] = len(h.plain)
		}
	}

	var b strings.Builder
	for i, h := range hints {
		col := i % hintColumns
		if col == 0 {
			b.WriteString(" ")
		}
		b.WriteString(h.colored)

		lastInRow := col == hintColumns-1 || i == len(hints)-1
		if !lastInRow {
			b.WriteString(strings.Repeat(" ", colWidth[col]-len(h.plain)+headerHintGap))
		} else if i != len(hints)-1 {
			b.WriteString("\n")
		}
	}

	s.headerHint.SetText(b.String())

	// The header row's height is fixed at grid-construction time (see
	// s.root.SetRows in init) to fit headerLeft's constant 3 lines. A detail
	// view can expose enough DetailActions to need more hint rows than that
	// — grow the header row to fit rather than silently clipping hints below
	// the visible area.
	//
	// s.root doesn't exist yet on the very first call, made from initFooter
	// before the grid is constructed — nothing to resize yet in that case.
	if s.root != nil {
		s.root.SetRows(headerRowsNeeded(len(hints)), 0, 1)
	}
}

// headerRowsNeeded returns how tall the header grid row must be to fit
// hintCount hints laid out hintColumns-wide, never shrinking below 3 —
// headerLeft always renders exactly 3 lines (root URL, version, client ID)
// regardless of how few hints there are.
func headerRowsNeeded(hintCount int) int {
	rows := (hintCount + hintColumns - 1) / hintColumns
	if rows < 3 {
		return 3
	}
	return rows
}

// renderBreadcrumbs rebuilds the footer's navigation trail from the current
// view stack, e.g. "workerpools › gecko-3/b-linux (workers)".
func (s *Shell) renderBreadcrumbs() {
	views := s.stack.views
	parts := make([]string, len(views))
	for i, v := range views {
		switch v.Kind {
		case DetailKind:
			parts[i] = fmt.Sprintf("%s:%s", v.ResourceName, v.SelectedID)
		default:
			if v.Scope != "" {
				parts[i] = fmt.Sprintf("%s (%s)", v.ResourceName, v.Scope)
			} else {
				parts[i] = v.ResourceName
			}
		}
	}

	s.footerBreadcrumb.SetText(" " + strings.Join(parts, " › "))
}

func (s *Shell) openCommandBar() {
	s.footerMode = footerCommand
	s.footerHistoryKey = historyKeyCommand
	s.resetFooterHistoryNav()
	s.footerInput.SetLabel("[yellow]:[white] ").SetText("")
	s.footer.SwitchToPage(pageFooterInput)
	s.app.SetFocus(s.footerInput)
}

func (s *Shell) openFilter() {
	s.footerMode = footerFilter
	s.footerHistoryKey = historyKeyFilter
	s.resetFooterHistoryNav()
	query := s.filterQuery
	if name, _ := s.content.GetFrontPage(); name == pageDetail {
		query = s.detail.FilterQuery()
	}
	s.footerInput.SetLabel("[yellow]/[white] ").SetText(query)
	s.footer.SwitchToPage(pageFooterInput)
	s.app.SetFocus(s.footerInput)
}

// clearActiveFilter clears whichever filter (list or detail) is active on
// the front page, if any, and reports whether it actually cleared one — so a
// caller like the global Esc handler can fall through to goBack() only once
// there's no more filter left to clear.
func (s *Shell) clearActiveFilter() bool {
	if name, _ := s.content.GetFrontPage(); name == pageDetail {
		if s.detail.FilterQuery() == "" {
			return false
		}
		s.detail.SetFilterQuery("")
		s.refreshDetailTitle()
		s.updateBorderColor()
		return true
	}
	if s.filterQuery == "" {
		return false
	}
	s.filterQuery = ""
	s.filterByResource[s.currentListResource] = ""
	s.refreshTable()
	return true
}

// openIDPrompt switches the footer to an inline id-entry field for a
// DirectLookup or DirectScopedResource reached with no id (e.g. bare
// `:task`), or for the 's' save-as path — rather than erroring or
// redirecting, since there's no browsable list to redirect to. commit is
// called with the entered text once Enter is pressed; label is what's shown
// before the input field, e.g. "task id" or "save as". historyKey scopes
// Up/Down recall to the right bucket — callers must pass a distinct key per
// semantically different prompt (id lookup vs. save path) so one never
// pollutes the other's history.
func (s *Shell) openIDPrompt(label string, historyKey footerHistoryKey, commit func(id string)) {
	s.openPrompt(label, historyKey, false, commit)
}

// openPrompt is openIDPrompt with control over whether an empty submit is
// meaningful. allowEmpty=false keeps the prompt open on Enter with nothing
// typed; allowEmpty=true calls commit("") instead, for a prompt where blank
// means something specific — see openScopePrompt.
func (s *Shell) openPrompt(label string, historyKey footerHistoryKey, allowEmpty bool, commit func(id string)) {
	s.footerMode = footerPrompt
	s.footerHistoryKey = historyKey
	s.resetFooterHistoryNav()
	s.pendingLookupCommit = commit
	s.pendingLookupAllowsEmpty = allowEmpty
	s.footerInput.SetLabel(fmt.Sprintf("[yellow]%s:[white] ", label)).SetText("")
	s.footer.SwitchToPage(pageFooterInput)
	s.app.SetFocus(s.footerInput)
}

func (s *Shell) closeFooterInput() {
	s.footerMode = footerIdle
	s.footer.SwitchToPage(pageFooterBreadcrumb)
	s.app.SetFocus(s.activeContent)
}

// resetFooterHistoryNav points the history cursor at "newest" for whichever
// bucket the footer was just opened in, so a fresh Up press always recalls
// the most recent entry rather than resuming wherever a previous session's
// browsing left off.
func (s *Shell) resetFooterHistoryNav() {
	s.footerHistoryIndex = len(s.footerHistory[s.footerHistoryKey])
	s.footerHistoryDraft = ""
}

// recordFooterHistory appends text to key's history, skipping a blank entry
// or one identical to the immediately preceding entry (so repeatedly
// re-running the same command doesn't pad the history with duplicates).
func (s *Shell) recordFooterHistory(key footerHistoryKey, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	history := s.footerHistory[key]
	if len(history) > 0 && history[len(history)-1] == text {
		return
	}
	s.footerHistory[key] = append(history, text)
}

// cycleFooterHistory moves the history cursor by delta (-1 for Up/older, +1
// for Down/newer) and writes the resulting entry into the footer input.
// Moving up for the first time in a browsing session stashes whatever was
// already typed as the draft, restored once Down cycles back past the
// newest entry. Both directions are no-ops at their respective ends (oldest
// entry stays put on Up; nothing to do on Down with no history browsed).
func (s *Shell) cycleFooterHistory(delta int) {
	history := s.footerHistory[s.footerHistoryKey]
	if len(history) == 0 {
		return
	}

	if delta < 0 {
		if s.footerHistoryIndex == len(history) {
			s.footerHistoryDraft = s.footerInput.GetText()
		}
		if s.footerHistoryIndex > 0 {
			s.footerHistoryIndex--
		}
	} else {
		if s.footerHistoryIndex >= len(history) {
			return // already at the draft; nothing newer to cycle to
		}
		s.footerHistoryIndex++
	}

	if s.footerHistoryIndex >= len(history) {
		s.footerInput.SetText(s.footerHistoryDraft)
		return
	}
	s.footerInput.SetText(history[s.footerHistoryIndex])
}

func (s *Shell) handleFooterInputChanged(text string) {
	if s.footerMode != footerFilter {
		return
	}

	if name, _ := s.content.GetFrontPage(); name == pageDetail {
		s.detail.SetFilterQuery(text)
		s.refreshDetailTitle()
		s.updateBorderColor()
		return
	}

	s.filterQuery = text
	s.refreshTable()
}

func (s *Shell) handleFooterInputDone(key tcell.Key) {
	switch key {
	case tcell.KeyEnter:
		switch s.footerMode {
		case footerCommand:
			s.recordFooterHistory(s.footerHistoryKey, s.footerInput.GetText())
			name, scope := splitCommand(s.footerInput.GetText())
			s.closeFooterInput()
			s.runCommand(name, scope)
		case footerFilter:
			s.recordFooterHistory(s.footerHistoryKey, s.footerInput.GetText())
			// The detail-page filter is already applied live by
			// handleFooterInputChanged and isn't resource-scoped the way a
			// list filter is — Enter there just leaves the input.
			if name, _ := s.content.GetFrontPage(); name != pageDetail {
				s.filterQuery = s.footerInput.GetText()
				s.filterByResource[s.currentListResource] = s.filterQuery
			}
			s.closeFooterInput()
		case footerPrompt:
			id := strings.TrimSpace(s.footerInput.GetText())
			if id == "" && !s.pendingLookupAllowsEmpty {
				return // keep the prompt open; nothing to look up yet
			}
			s.recordFooterHistory(s.footerHistoryKey, id) // no-op for the empty submit
			commit := s.pendingLookupCommit
			s.pendingLookupCommit = nil
			s.closeFooterInput()
			commit(id)
		}
	case tcell.KeyEscape:
		if s.footerMode == footerFilter {
			s.clearActiveFilter()
		}
		s.pendingLookupCommit = nil
		s.closeFooterInput()
	}
}

// splitCommand splits a command-bar input into a resource name and an
// optional scope argument: the remaining whitespace-separated fields,
// re-joined with a single space each (surrounding/repeated whitespace is
// normalized, not preserved literally).
func splitCommand(input string) (name, scope string) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", ""
	}

	return fields[0], strings.Join(fields[1:], " ")
}
