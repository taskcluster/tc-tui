package shell

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/taskcluster/tc-tui/crash"
	"github.com/taskcluster/tc-tui/resource"
)

// runCommand executes one command-bar command: the shell's own builtins
// (`:help`, `:quit`/`:q` — see resource.BuiltinCommands) first, then a
// registry resource via switchResource. It's the single dispatch point for
// everything typeable after `:`, shared by the command bar, the command
// palette's NavCommand rows, and the CLI's positional arguments — which is
// what lets the palette list the builtins as ordinary rows.
func (s *Shell) runCommand(name, scope string) {
	if cmd, isBuiltin := resource.ResolveBuiltin(name); isBuiltin {
		switch cmd.Name {
		case "help":
			s.ensureBaseView()
			s.openHelp()
		case "quit":
			s.Stop()
		}
		return
	}

	s.switchResource(name, scope)
}

// ensureBaseView draws the app's initial view if the cold-start command hasn't
// — the CLI's first command can OVERLAY the screen (`tc-tui help`,
// `tc-tui createtask`) rather than navigate to a view of its own, and StartAt
// renders nothing itself. Without this, dismissing that overlay drops the user
// on the empty table page that was never filled in.
//
// Gated on baseRendered rather than on the stack being empty: RestoreState
// leaves last session's views on the stack without drawing any of them, so an
// unrendered session and a restored one look identical from the stack alone.
// renderRestoredTop then draws whichever case applies — the restored top, or
// restoreFallback — exactly as Start would have.
func (s *Shell) ensureBaseView() {
	if s.baseRendered {
		return
	}
	if _, restored := s.stack.Top(); !restored && s.restoreFallback == "" {
		return // never went through Start/StartAt: nothing to fall back to
	}

	s.renderRestoredTop()
}

// switchResource replaces the entire navigation stack with a fresh List
// view for the given resource name/alias — the `:` command bar's behavior.
// If the resolved resource is a ScopedResource and no scope was given, it
// redirects to that resource's EmptyScopeResource instead of attempting an
// unscoped fetch. A resource.PeekResource (history, the command palette) is
// the one exception: it's pushed instead (see below), so it doesn't discard
// the current screen.
func (s *Shell) switchResource(nameOrAlias, scope string) {
	res, ok := s.registry.Resolve(nameOrAlias)
	if !ok {
		s.showError(nameOrAlias, fmt.Errorf(
			"unknown resource %q (available: %s)", nameOrAlias, strings.Join(s.registry.Names(), ", "),
		), func() {})
		return
	}

	// A peek resource is a navigational aid, not a destination in its own
	// right. Pushed rather than reset so Esc returns to whatever screen was
	// open before it — unless it's already the top view, in which case
	// re-opening it (`:hist` from history, Ctrl-A from the palette) would
	// stack a second identical copy for Esc to pop through. An argument still
	// means what it means everywhere else (`:commands workers` describes that
	// one entry), via showDetail so that it's pushed too.
	if _, isPeek := res.(resource.PeekResource); isPeek {
		if scope != "" {
			s.showDetail(res.Name(), scope)
			return
		}

		view := View{ResourceName: res.Name(), Kind: ListKind}
		if top, hasTop := s.stack.Top(); !hasTop || top != view {
			s.stack.Push(view)
		}
		s.renderList(res, "", false)
		return
	}

	// Checked before DirectLookup: a DirectScopedResource's method set is a
	// superset of DirectLookup's (it also embeds ScopedResource), so it
	// would match that broader assertion too — the more specific check must
	// win, or a resource like TaskGroupResource would be routed into
	// switchToDetail (Describe) instead of a scoped List view.
	if dsr, isDirectScoped := res.(resource.DirectScopedResource); isDirectScoped {
		// A RootBrowsable is the exception: with no id it falls through to the
		// unscoped list, which is its root scope, rather than prompting.
		_, browsesRoot := res.(resource.RootBrowsable)
		if scope == "" && !browsesRoot {
			s.openIDPrompt(dsr.IDPromptLabel(), historyKeyIDPrompt, func(id string) {
				s.switchResource(dsr.Name(), id)
			})
			return
		}
		s.stack.ResetTo(View{ResourceName: res.Name(), Kind: ListKind, Scope: scope})
		s.renderList(res, scope, false)
		return
	}

	if direct, isDirect := res.(resource.DirectLookup); isDirect {
		if scope == "" {
			s.openIDPrompt(direct.IDPromptLabel(), historyKeyIDPrompt, func(id string) {
				s.switchToDetail(res, id)
			})
			return
		}
		s.switchToDetail(res, scope)
		return
	}

	if scoped, isScoped := res.(resource.ScopedResource); isScoped {
		if scope == "" {
			if sp, wantsPrompt := res.(resource.ScopePrompt); wantsPrompt {
				s.openScopePrompt(sp)
				return
			}
			s.switchResource(scoped.EmptyScopeResource(), "")
			return
		}
		s.stack.ResetTo(View{ResourceName: res.Name(), Kind: ListKind, Scope: scope})
		s.renderList(res, scope, false)
		return
	}

	// A command-only resource (:createtask) has no list/detail view — fire its
	// single action directly, overlaying whatever screen is showing. On a cold
	// start (CLI `tc-tui createtask`) there is no view underneath, so cancel or
	// an editor-launch failure needs a base view to return to — see
	// ensureBaseView.
	if ca, isCommand := res.(resource.CommandAction); isCommand {
		s.ensureBaseView()
		s.startAction(ca.CommandAction())
		return
	}

	// res is a plain Resource: no ScopedList, no DirectLookup. A second
	// argument here can only mean "open this id directly" (e.g. `:workerpools
	// proj-taskcluster/ci`) — res.List() takes no scope, so there's no scoped
	// list this could otherwise mean.
	if scope != "" {
		s.switchToDetail(res, scope)
		return
	}

	s.stack.ResetTo(View{ResourceName: res.Name(), Kind: ListKind})
	s.renderList(res, "", false)
}

// openScopePrompt asks for sp's scope, the way a bare `:workers` (or picking
// it from the command palette) is handled — see resource.ScopePrompt. An
// entered id opens the scoped list directly; an empty submit falls back to the
// parent-list redirect, which the label advertises.
//
// The blank-to-browse half is offered only where the fallback is somewhere you
// can actually browse (see browsableFallbackIn). Where it just prompts again —
// artifacts → task — the hint would be a lie, so those get an ordinary
// required prompt instead.
func (s *Shell) openScopePrompt(sp resource.ScopePrompt) {
	label := sp.ScopePromptLabel()

	fallback, canBrowse := browsableFallbackIn(s.registry, sp.EmptyScopeResource())
	if !canBrowse {
		s.openIDPrompt(label, historyKeyIDPrompt, func(id string) {
			s.switchResource(sp.Name(), id)
		})
		return
	}

	s.openPrompt(label+" (blank to browse)", historyKeyIDPrompt, true, func(id string) {
		if id == "" {
			s.switchResource(fallback, "")
			return
		}
		s.switchResource(sp.Name(), id)
	})
}

// browsableFallbackIn reports whether name resolves to a resource that renders
// a pick-from-it list with no argument — the only case where offering "submit
// blank to browse instead" leads anywhere useful. Shared by the prompt itself
// and by the help text that documents it, so the two can't disagree.
//
// Everything that would instead demand another id is excluded: a DirectLookup
// or DirectScopedResource (both prompt), a ScopedResource (prompts, or
// redirects onward to something that does), and a CommandAction (runs a
// mutation rather than showing anything). A RootBrowsable is the exception
// among those — it's a DirectScopedResource that opens on its root list, so
// it's checked first.
func browsableFallbackIn(registry *resource.Registry, name string) (string, bool) {
	if name == "" {
		return "", false
	}

	res, ok := registry.Resolve(name)
	if !ok {
		return "", false
	}

	switch res.(type) {
	case resource.RootBrowsable:
		return res.Name(), true
	case resource.DirectLookup, resource.ScopedResource, resource.CommandAction:
		return "", false
	}

	return res.Name(), true
}

// toggleCommandPalette opens the command palette over the current view — the
// Ctrl-A key — or dismisses it again when it's already the top view. The
// palette is a PeekResource, so switchResource pushes rather than resets and
// Esc works to close it too. A registry without it registered (a minimal test
// registry) makes the key a no-op rather than an error screen.
func (s *Shell) toggleCommandPalette() {
	res, ok := s.registry.Resolve(resource.CommandsResourceName)
	if !ok {
		return
	}

	if top, hasTop := s.stack.Top(); hasTop && top.Kind == ListKind && top.ResourceName == res.Name() {
		s.goBack()
		return
	}

	s.switchResource(res.Name(), "")
}

// switchToDetail resets the navigation stack to a Detail view for res/id —
// the DirectLookup counterpart of switchResource's List-view reset. Used by
// a `:name <id>` command and by the id-prompt handleFooterInputDone opens
// when <id> is omitted.
func (s *Shell) switchToDetail(res resource.Resource, id string) {
	s.stack.ResetTo(View{ResourceName: res.Name(), Kind: DetailKind, SelectedID: id})
	s.renderDetail(res, id, false)
}

// showDetail pushes a Detail view for id onto the stack.
func (s *Shell) showDetail(resourceName, id string) {
	res, ok := s.registry.Resolve(resourceName)
	if !ok {
		s.showError(resourceName, fmt.Errorf("unknown resource %q", resourceName), func() {})
		return
	}

	s.stack.Push(View{ResourceName: res.Name(), Kind: DetailKind, SelectedID: id})
	s.renderDetail(res, id, false)
}

// pushScopedList pushes a List view scoped to scope onto the stack — what a
// DetailAction with Kind: NavScopedList triggers. Unlike switchResource,
// this pushes rather than resets, so Esc returns to the view that launched
// it.
func (s *Shell) pushScopedList(resourceName, scope string) {
	res, ok := s.registry.Resolve(resourceName)
	if !ok {
		s.showError(resourceName, fmt.Errorf("unknown resource %q", resourceName), func() {})
		return
	}

	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind, Scope: scope})
	s.renderList(res, scope, false)
}

// navigateTo executes a NavTarget the same way regardless of where it came
// from — a Detail view's action keybinding or a list row's NavTarget
// override (e.g. HistoryResource's rows).
//
// If history is the view being navigated away from, it's popped first so
// the target replaces it on the stack instead of stacking on top of it —
// otherwise Esc from the target would land back on history (itself pushed,
// not reset, specifically so Esc could skip past it) rather than on
// whatever screen was open before `:history` was run.
func (s *Shell) navigateTo(target resource.NavTarget) {
	if top, ok := s.stack.Top(); ok && top.ResourceName == "history" {
		s.stack.Pop()
	}

	switch target.Kind {
	case resource.NavScopedList:
		s.pushScopedList(target.ResourceName, target.ID)
	case resource.NavCommand:
		// Routed through runCommand rather than resolved here, so a palette
		// row does exactly what typing the same text into `:` does.
		s.runCommand(target.ResourceName, target.ID)
	default:
		s.showDetail(target.ResourceName, target.ID)
	}
}

// renderRestoredTop renders the current stack's top view — called once from
// Start with a stack populated by RestoreState (if any). It renders with
// isRestore=true, so a resolve/fetch failure for that view pops it and
// retries the next one down instead of showing the error screen (see
// loadList/loadDetail's isRestore-failure branch), so a stale restored view
// (e.g. an entity deleted since the last session) falls back through the
// breadcrumb trail silently. Once the stack empties out, it falls back to
// s.restoreFallback exactly as a fresh launch would.
func (s *Shell) renderRestoredTop() {
	top, ok := s.stack.Top()
	if !ok {
		s.switchResource(s.restoreFallback, "")
		return
	}

	res, ok := s.registry.Resolve(top.ResourceName)
	if !ok {
		s.stack.Pop()
		s.renderRestoredTop()
		return
	}

	switch top.Kind {
	case ListKind:
		s.renderList(res, top.Scope, true)
	case DetailKind:
		s.renderDetail(res, top.SelectedID, true)
	}
}

// goBack pops the top view and re-renders the new top. Esc never quits —
// only q does — so this is a no-op once only the root view is left.
func (s *Shell) goBack() {
	if s.stack.Len() <= 1 {
		return
	}

	s.stack.Pop()

	top, ok := s.stack.Top()
	if !ok {
		return
	}

	res, ok := s.registry.Resolve(top.ResourceName)
	if !ok {
		s.showError(top.ResourceName, fmt.Errorf("unknown resource %q", top.ResourceName), func() {})
		return
	}

	switch top.Kind {
	case ListKind:
		s.renderList(res, top.Scope, false)
	case DetailKind:
		s.renderDetail(res, top.SelectedID, false)
	}
}

// cycleFacet moves the current list view to the next (direction 1) or
// previous (direction -1) facet tab, wrapping around. For a client-side
// Faceted resource this is instant (refreshTable() re-filters s.lastRows).
// For a ServerFaceted resource it triggers a fresh fetch, since a different
// tab means different rows entirely.
func (s *Shell) cycleFacet(direction int) {
	switch {
	case s.currentServerFaceted != nil:
		tabs := ServerFacetTabs(s.currentServerFaceted, s.currentFacetCounts)
		next := cycleFacetValue(tabs, s.currentFacetValue, direction)
		if next == s.currentFacetValue {
			return
		}

		s.currentFacetValue = next
		s.facetByResource[s.currentListResource] = next
		s.table.ResetSelection()

		top, ok := s.stack.Top()
		if !ok {
			return
		}

		// Use the already-held ServerFaceted value directly (it embeds
		// resource.Resource) rather than re-resolving via the registry — a
		// tab switch doesn't change which resource is active, just its
		// facet value, so there's no need to look it up again.
		res := s.currentServerFaceted
		s.setTitle("Loading " + res.Name() + "...")
		s.table.SetData(s.currentColumns, nil, s.currentSort)
		s.loadList(res, top.Scope, s.currentFacetValue, true, false, false)

	case s.currentFaceted != nil:
		rows := FilterRows(s.lastRows, s.filterQuery)
		tabs := ClientFacetTabs(s.currentFaceted, rows)
		next := cycleFacetValue(tabs, s.currentFacetValue, direction)

		s.currentFacetValue = next
		s.facetByResource[s.currentListResource] = next
		s.table.ResetSelection()
		s.refreshTable()
	}
}

// toggleSort updates the active sort for the current list view: pressing
// the same column again reverses direction; any other column starts fresh
// at ascending. The result is remembered per-resource in sortByResource so
// it's restored the next time this resource is switched to.
func (s *Shell) toggleSort(column int) {
	if column < 0 || column >= len(s.currentColumns) {
		return
	}

	if s.currentSort.Direction != SortNone && s.currentSort.Column == column {
		if s.currentSort.Direction == SortAsc {
			s.currentSort.Direction = SortDesc
		} else {
			s.currentSort.Direction = SortAsc
		}
	} else {
		s.currentSort = SortState{Column: column, Direction: SortAsc}
	}

	s.sortByResource[s.currentListResource] = s.currentSort
	s.table.ResetSelection()
	s.refreshTable()
}

// toggleExpandColumns flips whether the table's columns honor their Width
// cap — the 'x' key's behavior. Columns that no longer fit the terminal once
// expanded are reachable with the Left/Right arrow keys (tview.Table's
// built-in horizontal scrolling).
func (s *Shell) toggleExpandColumns() {
	s.table.SetExpandColumns(!s.table.ExpandColumns())
	s.refreshTable()
}

// canLoadAllRows reports whether the 'L' key applies right now: a list view
// is front and its rows were capped at the safe fetch limit with more left
// server-side.
func (s *Shell) canLoadAllRows() bool {
	name, _ := s.content.GetFrontPage()
	return name == pageTable && s.currentListTruncated
}

// loadAllRows re-fetches the current list without the safe fetch limit —
// the 'L' key's action on a truncated list. The choice is remembered per
// cache key (see Shell.loadAllKeys) so auto-refresh ticks and
// back-navigation keep the full row set rather than reverting to the capped
// first fetch.
func (s *Shell) loadAllRows() {
	top, ok := s.stack.Top()
	if !ok || top.Kind != ListKind {
		return
	}
	res, ok := s.registry.Resolve(top.ResourceName)
	if !ok {
		return
	}
	if _, ok := res.(resource.PartialLister); !ok {
		return
	}

	s.loadAllKeys[cacheKeyFor(res, top.Scope, s.currentFacetValue)] = true
	s.setTitle("Loading all " + res.Name() + "...")
	s.loadList(res, top.Scope, s.currentFacetValue, true, true, false)
}

// toggleDetailWrap flips whether the detail body word-wraps to fit the view
// (default) or runs lines out unbroken — the 'x' key's behavior on a detail
// page. Unbroken lines are reachable with Left/Right/h/l, tview.TextView's
// built-in horizontal scrolling.
func (s *Shell) toggleDetailWrap() {
	s.detail.SetWrapEnabled(!s.detail.WrapEnabled())
	s.refreshDetailTitle()
}

// toggleDetailLineNumbers flips the 'n' key's vim-like "set number" gutter on
// a detail body — most useful paired with a '/' filter, so a line's original
// position stays visible even once the query has hidden the lines around it.
func (s *Shell) toggleDetailLineNumbers() {
	s.detail.SetLineNumbersEnabled(!s.detail.LineNumbersEnabled())
	s.refreshDetailTitle()
}

// refreshDetailTitle rebuilds the detail page's title from
// currentDetailTitle, appending a "[no wrap]" suffix while word-wrap is
// toggled off — the detail-page counterpart of refreshTable's "[no
// truncation]" suffix — a "[revealed]" suffix while masked content is being
// shown in the clear, and a "(query)" suffix while a '/' filter is active,
// mirroring refreshTable's own filter suffix.
func (s *Shell) refreshDetailTitle() {
	title := s.currentDetailTitle
	if !s.detail.WrapEnabled() {
		title += " [no wrap]"
	}
	if s.detail.LineNumbersEnabled() {
		title += " [#]"
	}
	if s.detailRevealed {
		title += " [revealed]"
	}
	if query := s.detail.FilterQuery(); query != "" {
		title += " (" + query + ")"
	}
	s.setTitle(title)
}

// refreshTable recomputes the table's displayed rows from s.lastRows by
// applying the current filter, facet, then sort, and re-renders; it also
// updates the title to show the active filter, if any. This is the single
// place list-view rows get filtered/sorted — call it any time s.lastRows,
// s.filterQuery, s.currentFacetValue, or s.currentSort changes.
func (s *Shell) refreshTable() {
	title := s.currentListResource
	if s.currentListScope != "" {
		title += " (" + s.currentListScope + ")"
	}
	if s.currentScopeSubtitle != "" {
		title += " [" + s.currentScopeSubtitle + "]"
	}
	if s.filterQuery != "" {
		title += " (" + s.filterQuery + ")"
	}

	rows := FilterRows(s.lastRows, s.filterQuery)
	s.renderTabsBar(rows)

	// For a ServerFaceted resource, s.lastRows is already exactly the
	// selected tab's rows (server-filtered) — no client-side facet filter
	// applies. For a Faceted resource, filter client-side.
	if s.currentFaceted != nil {
		rows = FilterByFacet(rows, s.currentFaceted, s.currentFacetValue)
	}

	// rows is now exactly what's about to be shown (pre-sort — sorting
	// doesn't change WHICH rows are visible, only their order), so it's the
	// right basis for the visible/total header count, and (below) for driving
	// augmentation and publishing the visible-set snapshot Augment reads.
	// The truncation "+" is folded into this single count rather than shown
	// as a separate marker — see formatRowCount.
	title += " · " + formatRowCount(len(rows), len(s.lastRows), s.currentListTruncated)

	if s.augmentTotal > 0 && s.augmentCompleted < s.augmentTotal {
		title += fmt.Sprintf(" [%d/%d]", s.augmentCompleted, s.augmentTotal)
	}
	if s.table.ExpandColumns() {
		title += " [no truncation]"
	}
	s.setTitle(title)
	s.updateBorderColor()

	s.triggerAugmentForNewlyVisibleRows(rows)

	rows = SortRows(rows, s.currentSort)
	s.table.SetData(s.currentColumns, rows, s.currentSort)
}

// renderTabsBar recomputes and redraws the facet tab bar from rows (used
// only by the client-side Faceted case; ServerFaceted reads
// s.currentFacetCounts instead, ignoring rows), collapsing the bar (and its
// separator line) entirely when the current resource has no facets. Each
// tab is rendered as its own colored "pill" — a bright highlight for the
// active tab, a muted one for the rest — with a horizontal rule underneath
// to separate the bar from the table.
func (s *Shell) renderTabsBar(rows []resource.Row) {
	var tabs []FacetTab
	switch {
	case s.currentServerFaceted != nil:
		tabs = ServerFacetTabs(s.currentServerFaceted, s.currentFacetCounts)
	case s.currentFaceted != nil:
		tabs = ClientFacetTabs(s.currentFaceted, rows)
	default:
		s.tableContainer.ResizeItem(s.tabsBar, 0, 0)
		s.tableContainer.ResizeItem(s.tabsSeparator, 0, 0)
		s.tabsBar.SetText("")
		s.tabsSeparator.SetText("")
		return
	}

	s.tableContainer.ResizeItem(s.tabsBar, 1, 0)
	s.tableContainer.ResizeItem(s.tabsSeparator, 1, 0)
	s.tabsSeparator.SetText(strings.Repeat("─", 300)) // clipped to whatever width is available

	var b strings.Builder
	for i, tab := range tabs {
		if i > 0 {
			b.WriteString(" ")
		}

		label := tab.Value
		if label == "" {
			label = "All"
		}
		text := fmt.Sprintf("%s (%d)", label, tab.Count)

		if tab.Value == s.currentFacetValue {
			// White-on-blue rather than black-on-yellow: named colors like
			// "black"/"gray" get remapped unpredictably by terminal themes
			// and can end up low-contrast or oddly tinted; white text on a
			// solid blue fill reads reliably across terminals.
			b.WriteString(fmt.Sprintf("[white:blue:b] %s [-:-:-]", text))
		} else {
			b.WriteString(fmt.Sprintf("[white] %s ", text))
		}
	}

	s.tabsBar.SetText(" " + b.String())
}

func (s *Shell) renderList(res resource.Resource, scope string, isRestore bool) {
	s.baseRendered = true
	s.currentDetailActions = nil
	if sa, ok := res.(resource.ScopeActions); ok {
		s.currentDetailActions = sa.ScopeActions(scope)
	}
	// A list view can also carry mutating actions (resource.Actionable) — e.g.
	// "create task" on the tasks list. They don't act on any single row, so
	// they're resolved with an empty id purely to render their key hints;
	// dispatch still re-resolves against the highlighted row at press time.
	s.currentActions = nil
	if act, ok := res.(resource.Actionable); ok {
		s.currentActions = act.Actions("")
	}
	s.closeFooterInput()
	s.filterQuery = s.filterByResource[res.Name()] // "" if never set
	s.currentListResource = res.Name()
	s.currentListScope = scope
	s.currentScopeSubtitle = "" // repopulated by loadList if res implements ScopeSubtitle

	// s.lastRows belongs to whichever resource/view was on screen before —
	// it may have a completely different shape (fewer cells) than the one
	// about to load. Cleared here, synchronously, rather than left to
	// linger until the fetch resolves: any refreshTable trigger in that
	// window (a sort/filter/facet keypress fired before the new rows
	// arrive) would otherwise hand these stale rows to the NEW resource's
	// Augment — e.g. WorkerPoolsResource.Augment unconditionally indexes
	// Cells[4..6], which panics on a row with fewer columns than that.
	s.lastRows = nil
	s.currentListTruncated = false
	s.augmentedRowIDs = map[string]bool{}
	s.settledRowIDs = map[string]bool{}
	s.visibleRowIDs.Store(map[string]bool{})
	s.currentColumns = res.Columns()
	s.currentSort = s.sortByResource[res.Name()] // zero value (SortNone) if not yet sorted

	s.currentFaceted = nil
	s.currentServerFaceted = nil
	s.currentFacetValue = ""
	s.currentFacetCounts = nil

	if sf, ok := res.(resource.ServerFaceted); ok {
		s.currentServerFaceted = sf
		s.currentFacetValue = s.restoreFacetValue(sf, res.Name())
	} else if f, ok := res.(resource.Faceted); ok {
		s.currentFaceted = f
		s.currentFacetValue = s.facetByResource[res.Name()] // "" (All) if never set
	}

	s.renderHeaderHints()
	s.renderBreadcrumbs()
	s.setTitle("Loading " + res.Name() + "...")
	s.table.SetData(s.currentColumns, nil, s.currentSort)
	s.renderTabsBar(nil)
	s.content.SwitchToPage(pageTable)
	s.updateBorderColor()
	s.app.SetFocus(s.table)

	s.startRefreshLoop(View{ResourceName: res.Name(), Kind: ListKind, Scope: scope}, res.RefreshInterval())
	s.loadList(res, scope, s.currentFacetValue, true, false, isRestore)
}

// restoreFacetValue returns the remembered facet value for name if it's
// still a valid option for sf, otherwise sf's first (default) option.
func (s *Shell) restoreFacetValue(sf resource.ServerFaceted, name string) string {
	options := sf.FacetOptions()
	if len(options) == 0 {
		return ""
	}

	saved := s.facetByResource[name]
	for _, opt := range options {
		if opt == saved {
			return saved
		}
	}

	return options[0]
}

// applyListResult renders a successfully fetched (or cache-hit) list result:
// shared by loadList's synchronous cache-hit path and its async fetch-success
// path. subtitle is whatever a ScopeSubtitle resource returned alongside
// rows ("" if the resource doesn't implement it, or the scope is empty).
// settledIDs seeds both s.augmentedRowIDs and s.settledRowIDs — nil for a
// fresh fetch (nothing augmented yet), or a cache hit's
// cacheEntry.settledIDs, so the refreshTable call below only re-triggers
// Augment for whatever that cached snapshot hadn't actually finished yet,
// rather than either blindly re-requesting rows already done or wrongly
// treating still-unaugmented ones (e.g. hidden by a filter, or simply
// still in flight, at the time they were cached) as settled forever.
// truncated marks rows as capped at the safe fetch limit (see
// resource.PartialLister) — always false for a resource that isn't one.
func (s *Shell) applyListResult(res resource.Resource, rows []resource.Row, counts map[string]int, subtitle string, settledIDs map[string]bool, truncated bool) {
	s.lastRows = rows
	s.currentListTruncated = truncated
	s.currentScopeSubtitle = subtitle
	s.augmentCompleted, s.augmentTotal = 0, 0
	s.augmentEpoch++
	s.augmentedRowIDs = map[string]bool{}
	s.settledRowIDs = map[string]bool{}
	for id := range settledIDs {
		s.augmentedRowIDs[id] = true
		s.settledRowIDs[id] = true
	}
	if counts != nil {
		s.currentFacetCounts = counts
	}
	s.refreshTable()
	s.activeContent = s.table
	s.renderHeaderHints()
	s.renderBreadcrumbs()
}

// nextLoadGeneration returns the generation the caller's dispatch should
// capture. A genuine navigation dispatch (isInitial=true — a real
// navigation, a restore-replay, or a facet-tab switch) starts a NEW
// generation (increments first). A background refresh tick (isInitial=false,
// used exclusively by Invalidate) instead just returns whichever generation
// is already current, inheriting the epoch of the view it's refreshing
// rather than starting its own or being exempt from the mechanism entirely.
// This is the single place that decides capture behavior for both loadList
// and loadDetail — see loadGeneration's doc comment in shell.go for how the
// captured value is later used.
func (s *Shell) nextLoadGeneration(isInitial bool) int {
	if isInitial {
		s.loadGeneration++
	}
	return s.loadGeneration
}

// loadList fetches this view's rows: via ServerFaceted.FacetList(scope,
// facetValue) if the resource implements it, otherwise via ScopedList(scope)
// (if scope is non-empty) or List(). Passing facetValue explicitly (rather
// than reading s.currentFacetValue from the fetch goroutine) avoids a data
// race with a UI-thread tab switch, and re-checking it on completion
// discards a slow fetch that a newer tab switch has since superseded.
// isInitial distinguishes a first/navigation load (failure shows a
// full-screen error with retry) from a background refresh tick that hasn't
// (failure shows a transient warning and keeps the last-good render) — tab
// switches themselves pass isInitial=true, since they're a deliberate user
// action. isRestore is true only for the one call renderRestoredTop itself
// issues for the view it's replaying — every other caller passes false.
//
// Unless forceRefresh is set, this first checks the cache for this
// (resource, scope, facetValue) key and, on a hit, applies it synchronously
// — no goroutine, no network call. forceRefresh is set only by the
// auto-refresh ticker (Invalidate), whose whole purpose is to get a
// genuinely fresh result for the view currently on screen.
func (s *Shell) loadList(res resource.Resource, scope, facetValue string, isInitial, forceRefresh, isRestore bool) {
	gen := s.nextLoadGeneration(isInitial)

	key := cacheKeyFor(res, scope, facetValue)
	// Whether the user has asked this view for the full uncapped fetch ('L')
	// — read here, on the UI thread, and captured by the fetch goroutine.
	loadAll := s.loadAllKeys[key]

	recordVisit := func() {
		if isInitial && !isRestore && scope != "" && res.Name() != "history" && s.historyRecorder != nil {
			s.historyRecorder.Record(resource.HistoryEntry{
				ResourceName: res.Name(),
				Kind:         int(ListKind),
				Scope:        scope,
				VisitedAt:    time.Now(),
			})
		}
	}

	if !forceRefresh {
		// A capped snapshot can't satisfy a load once the user has asked for
		// everything — without this, navigating away and back within the TTL
		// after pressing 'L' (but before the uncapped fetch landed) would pin
		// the view to the truncated rows.
		if entry, ok := s.cache.get(key, res.RefreshInterval()); ok && !(loadAll && entry.truncated) {
			// entry.settledIDs restores exactly which rows this cached
			// snapshot had actually FINISHED augmenting — anything else
			// (e.g. hidden by a narrower filter at cache time, or simply
			// still in flight when it was cached) still gets picked up
			// fresh below, via refreshTable.
			s.applyListResult(res, entry.rows, entry.counts, entry.subtitle, entry.settledIDs, entry.truncated)
			recordVisit() // <-- new
			return
		}
	}

	crash.Go(func() {
		var rows []resource.Row
		var counts map[string]int
		var err error
		var subtitle string
		var truncated bool

		// A PartialLister takes precedence over every other fetch shape —
		// it's the same list, just capped at the safe limit unless the user
		// asked for everything. A ServerFaceted resource's FacetCounts is
		// still fetched either way, since the tab bar needs it regardless of
		// how the rows themselves were obtained.
		pl, isPartial := res.(resource.PartialLister)
		sf, isServerFaceted := res.(resource.ServerFaceted)

		switch {
		case isPartial:
			rows, truncated, err = pl.ListPartial(scope, facetValue, loadAll)
		case isServerFaceted:
			rows, err = sf.FacetList(scope, facetValue)
		case scope != "":
			scoped, ok := res.(resource.ScopedResource)
			if !ok {
				err = fmt.Errorf("%s does not support a scoped list", res.Name())
			} else {
				rows, err = scoped.ScopedList(scope)
			}
		default:
			rows, err = res.List()
		}

		if isServerFaceted && err == nil {
			counts, err = sf.FacetCounts(scope)
		}

		// A ScopeSubtitle failure is non-fatal — sealed/expiry-style context
		// is supplementary, not worth failing the whole list load over.
		if err == nil && scope != "" {
			if ss, ok := res.(resource.ScopeSubtitle); ok {
				subtitle, _ = ss.Subtitle(scope)
			}
		}

		s.app.QueueUpdateDraw(func() {
			if s.isStaleLoad(gen) {
				return // a newer navigation dispatch has started since — even for the same View
			}
			if !s.isTopView(View{ResourceName: res.Name(), Kind: ListKind, Scope: scope}) {
				return
			}
			if facetValue != s.currentFacetValue {
				return // a newer tab switch has already superseded this fetch
			}

			if err != nil {
				// isRestore must be checked before isInitial: renderRestoredTop
				// always dispatches with isInitial=true, so swapping this order
				// would route a restore-chain failure into showError instead of
				// the silent pop-and-retry.
				if isRestore {
					s.stack.Pop()
					s.renderRestoredTop()
					return
				}
				if isInitial {
					s.showError(res.Name(), err, func() { s.renderList(res, scope, false) })
				} else {
					s.showTransientWarning(fmt.Sprintf("refresh failed: %s", err))
				}
				return
			}

			s.cache.set(key, cacheEntry{rows: rows, counts: counts, subtitle: subtitle, fetchedAt: time.Now(), truncated: truncated})
			s.applyListResult(res, rows, counts, subtitle, nil, truncated) // bumps s.augmentEpoch, resets s.augmentedRowIDs
			recordVisit()                                                  // <-- new
			// refreshTable (called by applyListResult) already triggers
			// Augment for whatever's visible now — see
			// triggerAugmentForNewlyVisibleRows.
		})
	})
}

// markRowsAugmented records rows as already requested for the current
// augmentEpoch, so triggerAugmentForNewlyVisibleRows won't hand them to
// Augment a second time.
func (s *Shell) markRowsAugmented(rows []resource.Row) {
	for _, row := range rows {
		s.augmentedRowIDs[row.ID] = true
	}
}

// cloneIDSet copies an id set (s.settledRowIDs, when storing alongside a
// cache entry) — the cache must hold its own snapshot, not a reference to
// the live map, since the latter is reset wholesale (to a new map) by the
// very next applyListResult call.
func cloneIDSet(ids map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(ids))
	for id := range ids {
		clone[id] = true
	}
	return clone
}

// rowIDSet builds a plain lookup set from rows' IDs — used both for the
// live visibleRowIDs snapshot and wherever else a row-membership check by ID
// is all that's needed.
func rowIDSet(rows []resource.Row) map[string]bool {
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		set[row.ID] = true
	}
	return set
}

// triggerAugmentForNewlyVisibleRows publishes visible (exactly what
// refreshTable is about to render — post filter AND facet) as the new
// s.visibleRowIDs snapshot, then kicks off an Augmentable.Augment call for
// whichever of those rows haven't already been augmented (or requested)
// this augmentEpoch. Called from refreshTable, so it re-fires whenever the
// filter or facet changes, in particular when widening or clearing a filter
// reveals rows that were skipped by an earlier, narrower call. Rows still
// hidden are left alone, same as before — this only ever adds work for rows
// that just became visible, never repeats it for ones already covered.
//
// Publishing the snapshot unconditionally (even when there's nothing NEW to
// request) matters just as much as the augmenting itself: an
// Augmentable.Augment call already in flight from an EARLIER, wider visible
// set (e.g. Augment was requested for all 400 rows of an unfiltered list)
// reads this same snapshot via its wanted callback, so narrowing the filter
// while that batch is still draining tells it to stop spending further API
// calls on rows that are no longer visible — see Augmentable.Augment's doc.
func (s *Shell) triggerAugmentForNewlyVisibleRows(visible []resource.Row) {
	visibleSet := rowIDSet(visible)

	res, ok := s.registry.Resolve(s.currentListResource)
	if !ok {
		s.visibleRowIDs.Store(visibleSet)
		return
	}
	a, ok := res.(resource.Augmentable)
	if !ok {
		s.visibleRowIDs.Store(visibleSet)
		return
	}

	var toAugment []resource.Row
	for _, row := range visible {
		if !s.augmentedRowIDs[row.ID] {
			toAugment = append(toAugment, row)
		}
	}

	// The filter matches only against each row's CURRENT cells, so a query
	// aimed at an augmented column (e.g. worker pools' Pending/Claimed/
	// Errors) can't match a row still sitting on its loading placeholder.
	// Two ways that bites:
	//   - Every row in the base list is still on its placeholder, visible
	//     comes back empty, and nothing above gets requested — a filter
	//     restored from a prior session never even gets a keystroke to
	//     retry it, so it would find nothing, forever, with no way out.
	//   - SOME rows already matched via an ordinary (known) column, so
	//     visible is non-empty and augmentation looks "done" for this
	//     query — but another row that would ALSO match, only via an
	//     augmented column, never gets checked and silently never shows up.
	//     A non-empty visible set on its own says nothing about whether
	//     every possible match has been found.
	// Fall back to augmenting a small bounded batch from the full
	// (unfiltered) list whenever EITHER can be happening, so filtering
	// keeps making forward progress toward a complete result instead of
	// settling for whatever's already been found. Gated on
	// len(toAugment) never being used alone — that's also 0, and common,
	// whenever a non-empty visible set has simply already been fully
	// augmented, which must NOT re-trigger this fallback on its own.
	//
	// Gated on mightMatchAugmentedColumn ALONE, deliberately not "OR
	// len(visible) == 0": base columns (pool ID, provider, capacity) are
	// ALL already fully known from the very first render, never a
	// placeholder — so a purely-alphabetic query matching zero rows is a
	// definitive, complete answer already; augmenting more rows cannot
	// possibly turn up a NEW match for it, since alphabetic text can never
	// match a numeric augmented column either. Falling back anyway for
	// that case (an earlier version of this fix did, via a bare
	// len(visible) == 0 check) would walk the ENTIRE hidden list for a
	// plain typo, achieving nothing but the exact wasted-call cost wanted
	// exists to avoid. mightMatchAugmentedColumn checks for a digit, since
	// every augmented column across today's Augmentable resources renders
	// as a plain decimal integer (worker pools' Pending/Claimed/Errors) —
	// a digit-bearing query gets the benefit of the doubt and keeps
	// probing in small batches (regardless of whether visible is currently
	// empty or not) until nothing's left un-augmented, since it might
	// legitimately be aimed at one of those columns rather than a name
	// that happens to contain a digit. No separate emptiness check for
	// s.filterQuery is needed: mightMatchAugmentedColumn("") is false, so
	// an unfiltered view (where visible == s.lastRows already, leaving
	// nothing extra for the fallback to add anyway) never reaches here.
	if mightMatchAugmentedColumn(s.filterQuery) {
		// staged tracks rows already added to toAugment by the visible
		// loop above — s.augmentedRowIDs alone isn't enough, since
		// markRowsAugmented hasn't run yet at this point in the function;
		// without this, any row that's both currently visible AND still
		// unaugmented (the common case right after a fresh load) would be
		// iterated again here and added to toAugment a second time.
		staged := make(map[string]bool, len(toAugment))
		for _, row := range toAugment {
			staged[row.ID] = true
		}
		for _, row := range s.lastRows {
			if len(toAugment) >= augmentFilterMissFallbackBatch {
				break
			}
			if s.augmentedRowIDs[row.ID] || staged[row.ID] {
				continue
			}
			toAugment = append(toAugment, row)
			staged[row.ID] = true
			visibleSet[row.ID] = true // else wanted() would reject its own fallback batch immediately
		}
	}

	s.visibleRowIDs.Store(visibleSet)

	if len(toAugment) == 0 {
		return
	}
	s.markRowsAugmented(toAugment)

	view := View{ResourceName: s.currentListResource, Kind: ListKind, Scope: s.currentListScope}
	gen := s.loadGeneration
	epoch := s.augmentEpoch
	facetValue := s.currentFacetValue
	counts := s.currentFacetCounts
	subtitle := s.currentScopeSubtitle
	key := cacheKeyFor(res, s.currentListScope, facetValue)

	// rejected records every id wanted has EVER said no to during this
	// batch — not just its answer at the final tick. wanted may be called
	// concurrently (Augmentable.Augment's own per-row goroutines), so it's
	// guarded by rejectedMu; both are read/written only here and in the
	// final-tick handling below, never elsewhere.
	var (
		rejectedMu sync.Mutex
		rejected   = map[string]bool{}
	)
	wanted := func(id string) bool {
		ids, _ := s.visibleRowIDs.Load().(map[string]bool)
		if ids[id] {
			return true
		}
		rejectedMu.Lock()
		rejected[id] = true
		rejectedMu.Unlock()
		return false
	}

	crash.Go(func() {
		// lastRedraw throttles the expensive part of each tick (refreshTable
		// — full filter/facet/sort recompute plus a whole-table SetData —
		// and the cache write) to at most once per augmentRedrawInterval.
		// With hundreds of rows, Augment can tick once per row; a resource
		// that resolves most of those near-instantly (e.g. skipping ones
		// wanted rejects, with no network round-trip to pace them) would
		// otherwise fire that full redraw hundreds of times in a tight
		// burst, starving the UI thread — including the very keystroke that
		// just narrowed the filter. The cheap part (merging data into
		// s.lastRows and the completed/total counters) still happens on
		// every tick, so no data is ever lost — only how often it's actually
		// drawn is throttled. The final tick (completed >= total) always
		// redraws regardless, so the view never ends up stale.
		var lastRedraw time.Time

		a.Augment(toAugment, wanted, func(updated []resource.Row, completed, total int) {
			s.app.QueueUpdateDraw(func() {
				if s.isStaleLoad(gen) || !s.isTopView(view) || facetValue != s.currentFacetValue {
					return // the same staleness checks loadList's fetch-success handler uses
				}
				if s.augmentEpoch != epoch {
					return // a newer base-row render for this view has since started — drop this now-stale tick rather than clobbering fresher rows
				}
				merged := mergeRowsByID(s.lastRows, updated)
				s.lastRows = merged
				s.augmentCompleted, s.augmentTotal = completed, total

				// Once this batch is done — and only then, since a row's
				// wanted answer(s) can't be trusted as final until its
				// whole batch has finished — settle its rows: un-mark
				// every one wanted EVER rejected during it (not just
				// whichever ones are still invisible right now — a row can
				// be rejected while the filter is narrow and become
				// visible again before the batch finishes; re-checking
				// wanted() only at this final tick would see it as visible
				// and leave it marked "augmented" despite never actually
				// being fetched, so a later filter change would never
				// retry it either), and mark every row that WASN'T
				// rejected as settled — this is what makes it safe to
				// cache: s.settledRowIDs, unlike s.augmentedRowIDs, only
				// ever contains rows a batch has actually finished with.
				if completed >= total {
					rejectedMu.Lock()
					for _, row := range toAugment {
						if rejected[row.ID] {
							delete(s.augmentedRowIDs, row.ID)
						} else {
							s.settledRowIDs[row.ID] = true
						}
					}
					rejectedMu.Unlock()
				}

				now := time.Now()
				if !shouldRedrawAugmentTick(completed, total, lastRedraw, now) {
					return
				}
				lastRedraw = now
				if s.onAugmentRedrawForTest != nil {
					s.onAugmentRedrawForTest()
				}
				s.refreshTable() // may itself mark further rows augmented — snapshot afterward
				s.cache.set(key, cacheEntry{
					rows: merged, counts: counts, subtitle: subtitle, fetchedAt: time.Now(),
					truncated: s.currentListTruncated,
					// s.settledRowIDs, not s.augmentedRowIDs: refreshTable
					// just above may have dispatched a brand new batch
					// (newly-visible rows, or the augmented-column-filter
					// fallback) whose rows are now marked requested but
					// have had zero chance to actually fetch anything yet
					// — caching THAT set would let them be wrongly treated
					// as settled if the user navigates away and back
					// before that new batch's own final tick ever lands.
					settledIDs: cloneIDSet(s.settledRowIDs),
				})
			})
		})
	})
}

// augmentRedrawInterval caps how often a single Augment call's ticks
// actually trigger a redraw — see triggerAugmentForNewlyVisibleRows.
const augmentRedrawInterval = 150 * time.Millisecond

// augmentFilterMissFallbackBatch caps how many additional, currently-hidden
// rows triggerAugmentForNewlyVisibleRows will augment in one go when the
// active filter matches nothing yet — see its doc comment.
const augmentFilterMissFallbackBatch = 25

// mightMatchAugmentedColumn reports whether query could plausibly match one
// of today's Augmentable resources' augmented columns — see
// triggerAugmentForNewlyVisibleRows's doc comment for why this exists and
// its limits.
func mightMatchAugmentedColumn(query string) bool {
	return strings.ContainsAny(query, "0123456789")
}

// shouldRedrawAugmentTick decides whether an Augment tick should trigger the
// expensive refreshTable+cache-write path right now: always once the batch
// is done (completed >= total, so the view never ends up stale), otherwise
// only if augmentRedrawInterval has elapsed since lastRedraw.
func shouldRedrawAugmentTick(completed, total int, lastRedraw, now time.Time) bool {
	return completed >= total || now.Sub(lastRedraw) >= augmentRedrawInterval
}

func (s *Shell) renderDetail(res resource.Resource, id string, isRestore bool) {
	s.baseRendered = true
	s.currentDetailActions = nil
	s.currentActions = nil
	// A fresh render always starts masked — a reveal is scoped to one visit.
	// Routed through setDetailRevealed so the epoch advances even when the
	// view being left wasn't revealed, retiring any fetch dispatched under it.
	s.setDetailRevealed(false)
	s.closeFooterInput()

	s.renderHeaderHints()
	s.renderBreadcrumbs()

	// A cache hit redraws this view's last-known content immediately instead
	// of blanking to "Loading..." — most noticeable on Esc/back navigation,
	// where the exact same view was just on screen a moment ago. loadDetail
	// still re-fetches fresh in the background regardless (primed=true below
	// only changes how ITS OWN completion applies the result — see
	// detailCache's doc comment) — this never substitutes for a real
	// Describe.
	primed := false
	if entry, ok := s.detailCache.get(detailCacheKeyFor(res, id), res.RefreshInterval()); ok {
		s.detail.SetData(entry.detail)
		s.currentDetailActions = entry.detail.Actions
		s.activeContent = s.detail
		s.currentDetailTitle = entry.detail.Title
		s.refreshDetailTitle()
		s.renderHeaderHints()
		primed = true
	} else {
		s.setTitle("Loading " + res.Name() + "...")
		s.detail.SetData(resource.Detail{})
	}
	s.content.SwitchToPage(pageDetail)
	s.updateBorderColor()
	s.app.SetFocus(s.detail)

	s.startRefreshLoop(View{ResourceName: res.Name(), Kind: DetailKind, SelectedID: id}, res.RefreshInterval())
	s.loadDetail(res, id, true, isRestore, primed)
}

// primed is true only when renderDetail already redrew this view
// synchronously from detailCache — it makes this call's own completion
// behave like a background refresh (UpdateData, transient warning on
// failure) even though isInitial is true, since the screen isn't blank and
// shouldn't be treated as if this were the first content it's ever shown.
func (s *Shell) loadDetail(res resource.Resource, id string, isInitial, isRestore, primed bool) {
	gen := s.nextLoadGeneration(isInitial)
	revealed := s.detailRevealed
	revealEpoch := s.revealEpoch

	crash.Go(func() {
		// A currently-live id streams instead of Describe-ing — checked
		// here, off the UI thread, since IsLive may cost an API call.
		if ls, ok := res.(resource.LiveStreamer); ok && ls.IsLive(id) {
			s.runDetailStream(ls, id, gen, isInitial, isRestore)
			return
		}

		detail, err := describeDetail(res, id, revealed)
		s.app.QueueUpdateDraw(func() {
			if s.isStaleLoad(gen) {
				return // a newer navigation dispatch has started since — even for the same View
			}
			if !s.isTopView(View{ResourceName: res.Name(), Kind: DetailKind, SelectedID: id}) {
				return
			}
			if revealEpoch != s.revealEpoch {
				return // 'v' has been toggled since this fetch was dispatched — drop it rather than putting cleartext back on a screen just cleared of it
			}

			if err != nil {
				// isRestore must be checked before isInitial: renderRestoredTop
				// always dispatches with isInitial=true, so swapping this order
				// would route a restore-chain failure into showError instead of
				// the silent pop-and-retry.
				if isRestore {
					s.stack.Pop()
					s.renderRestoredTop()
					return
				}
				if isInitial && !primed {
					s.showError(fmt.Sprintf("%s %s", res.Name(), id), err, func() { s.renderDetail(res, id, false) })
				} else {
					s.showTransientWarning(fmt.Sprintf("refresh failed: %s", err))
				}
				return
			}

			// A revealed body must never be cached: renderDetail's primed path
			// redraws from the cache without anyone having asked for a reveal,
			// and every other reader assumes the masked rendering.
			if !revealed {
				s.detailCache.set(detailCacheKeyFor(res, id), detailCacheEntry{detail: detail, fetchedAt: time.Now()})
			}

			if isInitial && !primed {
				s.detail.SetData(detail)
			} else {
				s.detail.UpdateData(detail)
			}
			s.currentDetailActions = detail.Actions
			if act, ok := res.(resource.Actionable); ok {
				s.currentActions = act.Actions(id)
			} else {
				s.currentActions = nil
			}
			s.activeContent = s.detail
			s.currentDetailTitle = detail.Title
			s.refreshDetailTitle()
			s.renderHeaderHints()
			s.renderBreadcrumbs()

			if isInitial && !isRestore && res.Name() != "history" && s.historyRecorder != nil {
				s.historyRecorder.Record(resource.HistoryEntry{
					ResourceName: res.Name(),
					Kind:         int(DetailKind),
					SelectedID:   id,
					Title:        detail.Title,
					VisitedAt:    time.Now(),
				})
			}
		})
	})
}

func (s *Shell) showError(title string, err error, retry func()) {
	s.stopRefreshLoop()

	s.errorView.SetError(title, err)
	s.errorView.SetOnRetry(retry)
	s.activeContent = s.errorView

	s.setTitle(fmt.Sprintf("Error :: %s", title))
	s.content.SwitchToPage(pageError)
	s.updateBorderColor()
	s.app.SetFocus(s.errorView)
}

func (s *Shell) showTransientWarning(msg string) {
	s.footerBreadcrumb.SetText(fmt.Sprintf("[red]%s[white]", msg))
}

// showTransientInfo behaves like showTransientWarning but in green — for
// non-error feedback (e.g. a completed save) rather than something gone
// wrong.
func (s *Shell) showTransientInfo(msg string) {
	s.footerBreadcrumb.SetText(fmt.Sprintf("[green]%s[white]", msg))
}
