package shell

import (
	"fmt"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/taskcluster/tc-tui/resource"
)

const (
	pageTable         = "table"
	pageDetail        = "detail"
	pageError         = "error"
	pageHelp          = "help"
	pageAction        = "action"
	pageEditorConfirm = "editorConfirm"

	pageFooterBreadcrumb = "breadcrumb"
	pageFooterInput      = "input"
)

type footerMode int

const (
	footerIdle footerMode = iota
	footerCommand
	footerFilter
	footerPrompt
)

// footerHistoryKey names a footer history bucket (see Shell.footerHistory).
// It's tracked separately from footerMode because footerMode's footerPrompt
// value covers two semantically unrelated prompts — a resource-id lookup
// (`:task`, `:hook`, ...) and the 's' save-as path — that must never share
// recall history (a task id showing up while typing a save path, or vice
// versa, would be actively confusing).
type footerHistoryKey string

const (
	historyKeyCommand  footerHistoryKey = "command"
	historyKeyFilter   footerHistoryKey = "filter"
	historyKeyIDPrompt footerHistoryKey = "id-prompt"
	historyKeySavePath footerHistoryKey = "save-path"
)

// Shell is the generic navigation engine: registry, view stack, table/detail
// views, command bar, filter, refresh loop. It knows nothing about roles or
// worker pools specifically — only the Resource interface.
type Shell struct {
	app      *tview.Application
	registry *resource.Registry
	stack    *ViewStack

	root       *tview.Grid
	headerLeft *tview.TextView
	headerHint *tview.TextView

	content       *tview.Pages
	table         *TableView
	detail        *DetailView
	errorView     *ErrorView
	helpView      *HelpView
	actionView    *ActionView
	editorConfirm *EditorConfirmView

	helpOpen    bool
	preHelpPage string

	// actionOpen is true while the authenticated-action dialog (ActionView)
	// is front and focused. Like footerMode/helpOpen it gates
	// globalInputCapture: every key passes straight through to the dialog's
	// form so the user can type/tab/confirm, rather than being intercepted as
	// a global shortcut.
	actionOpen bool
	// actionBusy is true while the current action's Perform is running, so a
	// second Confirm activation is ignored until it finishes.
	actionBusy bool
	// actionReturnPage is the content page to restore when the dialog closes.
	actionReturnPage string
	// currentAction is the action the open dialog is collecting input for.
	currentAction resource.Action

	// openInEditor is a seam over the package-level editInEditor so tests can
	// simulate an $EDITOR round trip (success, failure, or an unavailable
	// screen) without shelling out or suspending a real terminal.
	openInEditor func(seed string) (string, error)

	footer              *tview.Pages
	footerBreadcrumb    *tview.TextView
	footerInput         *tview.InputField
	footerMode          footerMode
	pendingLookupCommit func(id string) // set while footerMode == footerPrompt; called with the entered id
	// pendingLookupAllowsEmpty commits "" on Enter with nothing typed, instead
	// of ignoring it — only for a prompt where blank means something.
	pendingLookupAllowsEmpty bool

	// footerHistory remembers previously entered footer text, scoped per
	// footerHistoryKey (command bar, filter, id lookup, and save-as path each
	// get their own history) so Up/Down can recall it shell-style.
	// footerHistoryKey is which bucket is currently active, set by whichever
	// open* function opened the footer. footerHistoryIndex is a cursor into
	// that bucket's slice — len(history) means "not browsing", i.e. showing
	// footerHistoryDraft, the text that was being typed before the first Up
	// press of the current browsing session.
	footerHistory      map[footerHistoryKey][]string
	footerHistoryKey   footerHistoryKey
	footerHistoryIndex int
	footerHistoryDraft string

	currentListResource  string
	currentListScope     string // "" for an unscoped list
	currentScopeSubtitle string // set by a ScopeSubtitle resource alongside its rows; "" if none/not applicable
	currentColumns       []resource.Column
	lastRows             []resource.Row
	filterQuery          string
	filterByResource     map[string]string
	currentDetailActions []resource.DetailAction
	currentDetailTitle   string

	// currentActions holds the mutating actions the current Detail entity
	// exposes (via resource.Actionable), used to render their key hints in
	// the header. Dispatch resolves actions fresh at key-press time (see
	// resolveActionByKey) rather than from this slice, so it's purely for the
	// hint row; nil when the current view has none.
	currentActions []resource.Action

	// currentListTruncated reports whether the current list view's rows were
	// capped at the safe fetch limit with more left unfetched server-side
	// (see resource.PartialLister) — drives refreshTable's "N+" row-count title
	// suffix and the 'L' load-all key and hint.
	currentListTruncated bool

	// loadAllKeys remembers, per list cache key, that the user asked for the
	// full uncapped fetch ('L') — consulted by loadList so later loads of
	// the same view (auto-refresh ticks, navigating back within the cache
	// TTL) stay uncapped instead of silently reverting to the capped first
	// fetch.
	loadAllKeys map[cacheKey]bool

	currentSort    SortState
	sortByResource map[string]SortState

	tabsBar        *tview.TextView
	tabsSeparator  *tview.TextView
	tableContainer *tview.Flex

	currentFaceted       resource.Faceted
	currentServerFaceted resource.ServerFaceted
	currentFacetValue    string
	currentFacetCounts   map[string]int
	facetByResource      map[string]string

	// augmentCompleted/augmentTotal track an in-progress Augmentable.Augment
	// call for the current list view — augmentTotal == 0 means no
	// augmentation is active (or applicable), so refreshTable's title
	// suffix is hidden. Reset to 0,0 by applyListResult on every fresh
	// base-row render (navigation, refresh, or a cache hit — which never
	// gets a follow-up Augment call).
	augmentCompleted int
	augmentTotal     int

	// augmentEpoch is bumped by applyListResult every time base rows are
	// freshly applied for a list view. A refresh reuses the same View and
	// the same loadGeneration as the render it's refreshing (see
	// loadGeneration's doc comment) — isStaleLoad/isTopView alone can't
	// tell a slow, still-in-flight Augment run left over from a PRIOR
	// refresh cycle apart from one belonging to the CURRENT rows on
	// screen. loadList captures the epoch right after applying a given
	// set of base rows and threads it through to that Augment call's
	// onUpdate closure, which drops any tick where the epoch has since
	// moved on — otherwise a slow augmentation's late ticks would
	// overwrite a newer reload's fresh (placeholder) rows with stale
	// computed values.
	augmentEpoch int

	// augmentedRowIDs tracks which of the current view's rows have already
	// been REQUESTED from Augmentable.Augment (dispatched — not necessarily
	// finished yet) for the current augmentEpoch — refreshTable calls
	// triggerAugmentForNewlyVisibleRows on every filter/facet/sort change,
	// which uses this to avoid dispatching a duplicate request for a row
	// already in flight, and to fire Augment only for rows not in this set,
	// so widening or clearing a filter picks up augmentation for the
	// newly-revealed rows instead of leaving them stuck at their
	// placeholder forever. Reset (to an empty, non-nil map) by
	// applyListResult alongside augmentEpoch.
	augmentedRowIDs map[string]bool

	// settledRowIDs tracks which of augmentedRowIDs have actually FINISHED
	// — their owning batch reached its final tick and confirmed they were
	// never rejected by wanted — as opposed to merely requested. This is
	// the set that gets written to (and restored from) the list cache: a
	// row can be marked in augmentedRowIDs the instant its batch is
	// dispatched, well before it has any real data, so caching that set
	// directly would let a still-in-flight (or fallback-batch-dispatched)
	// row's placeholder get treated as settled forever if the user
	// navigates away and back within the cache TTL before that batch
	// finishes. Reset alongside augmentedRowIDs; always a subset of it.
	settledRowIDs map[string]bool

	// visibleRowIDs holds the current view's visible-row-ID set (map[string]bool),
	// updated by refreshTable every time it recomputes what's actually
	// shown (filter/facet/sort/base-row changes). It's an atomic.Value
	// rather than a plain map because Augmentable.Augment's wanted callback
	// reads it from arbitrary background goroutines — each Store swaps in a
	// brand new map, so a concurrent Load always sees a complete, never-
	// mutated-after-publish snapshot, no locking needed.
	visibleRowIDs atomic.Value

	// onAugmentRedrawForTest, if set, is called each time
	// triggerAugmentForNewlyVisibleRows actually performs the throttled
	// refreshTable+cache-write for an Augment tick (i.e. shouldRedrawAugmentTick
	// returned true) — a test-only seam for counting real redraws directly,
	// since wall-clock timing against a SimulationScreen doesn't reflect real
	// terminal draw cost. Always nil in production.
	onAugmentRedrawForTest func()

	// onStopForTest, if set, is called by Stop before app.Stop() — a test-only
	// seam for asserting a quit was triggered (e.g. via the `:quit` command),
	// since tview's Application doesn't expose whether it's running. Always nil
	// in production.
	onStopForTest func()

	activeContent tview.Primitive

	stopRefresh chan struct{}

	// stopStream, when non-nil, is the active detail stream's stop channel
	// (see runDetailStream) — closed by stopDetailStream whenever the
	// streamed view stops being the one on screen (navigation, error, a
	// newer load), which ends the underlying fetch. Like stopRefresh, only
	// ever touched on the UI goroutine.
	stopStream chan struct{}

	cache *listCache

	// detailCache mirrors cache but for Describe results, keyed by (resource,
	// id) — see detailCache's own doc comment for why this exists.
	detailCache *detailCache

	// historyRecorder is resolved once, in init(), from whatever resource is
	// registered under the name "history" (nil if none is — e.g. a minimal
	// test registry). Every recording call in loadList/loadDetail is a no-op
	// when this is nil.
	historyRecorder resource.HistoryRecorder

	// loadGeneration is incremented once per genuine navigation/render
	// dispatch of loadList/loadDetail (isInitial=true — a real navigation,
	// a restore-replay, or a facet-tab switch). A background refresh tick
	// (isInitial=false, used exclusively by Invalidate) does NOT increment
	// it — it captures whichever generation is already current instead,
	// inheriting the epoch of the view it's refreshing rather than
	// starting a new one. See nextLoadGeneration (navigation.go), the
	// single place that decides this capture behavior for both
	// loadList/loadDetail. Every dispatch's completion (refresh or
	// navigation) checks its captured generation against the current
	// value unconditionally: a captured value that no longer matches means
	// a newer navigation has started since — even one that later returns
	// to the identical View (isTopView would match again in that case, but
	// the generation correctly still doesn't) — and this completion must
	// no-op regardless of success or failure.
	//
	// Only ever mutated/read on tview's single event-loop goroutine (input
	// captures, Start's initial dispatch before app.Run(), and
	// QueueUpdateDraw callbacks are all serialized onto it) — never call
	// loadList/loadDetail from a raw `go` statement, or this increment
	// becomes a data race and two dispatches could capture the same value.
	loadGeneration int

	// restoreFallback is the resource Start falls back to once a restored
	// stack (if any) has been fully drained — either because it was empty to
	// begin with, or because every restored view turned out to be
	// unresolvable/stale. See renderRestoredTop/loadList/loadDetail's
	// isRestore argument for how an in-progress restore is now tracked (a
	// per-call argument, not a field).
	restoreFallback string

	// baseRendered reports whether any view has been drawn yet this session.
	// pageTable is the front page from construction and RestoreState fills the
	// stack without drawing it, so neither the content area nor the stack can
	// answer that on their own — see ensureBaseView.
	baseRendered bool

	// rootURL is set once via SetInfo and used to build web UI links for the
	// 'o' key (see openInBrowser).
	rootURL string

	// openBrowser is a seam over the package-level openBrowser func so tests
	// can capture what would be opened instead of shelling out for real.
	openBrowser func(url string) error
}

func New(registry *resource.Registry) *Shell {
	// Let the terminal supply the background instead of painting tview's
	// default ColorBlack over every primitive. Besides respecting the user's
	// terminal theme, ColorDefault allows terminal emulators such as Ghostty
	// to preserve their configured background transparency.
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault

	s := &Shell{
		app:              tview.NewApplication(),
		registry:         registry,
		stack:            NewViewStack(),
		sortByResource:   make(map[string]SortState),
		facetByResource:  make(map[string]string),
		filterByResource: make(map[string]string),
		loadAllKeys:      make(map[cacheKey]bool),
		footerHistory:    make(map[footerHistoryKey][]string),
		cache:            newListCache(),
		detailCache:      newDetailCache(),
		openBrowser:      openBrowser,
	}
	s.init()

	return s
}

func (s *Shell) init() {
	if hr, ok := s.registry.Resolve("history"); ok {
		s.historyRecorder, _ = hr.(resource.HistoryRecorder)
	}

	s.headerLeft = tview.NewTextView().SetDynamicColors(true).SetWordWrap(true).
		SetChangedFunc(func() { s.app.Draw() })
	s.headerHint = tview.NewTextView().SetDynamicColors(true).SetWordWrap(true).
		SetTextAlign(tview.AlignLeft).
		SetChangedFunc(func() { s.app.Draw() })

	s.table = NewTableView()
	s.table.SetOnSelect(func(row resource.Row) {
		if row.NavTarget != nil {
			s.navigateTo(*row.NavTarget)
			return
		}
		s.showDetail(s.currentListResource, row.ID)
	})

	s.tabsBar = tview.NewTextView().SetDynamicColors(true)
	s.tabsSeparator = tview.NewTextView().SetDynamicColors(true).
		SetTextColor(tview.Styles.SecondaryTextColor)
	s.tableContainer = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.tabsBar, 0, 0, false).
		AddItem(s.tabsSeparator, 0, 0, false).
		AddItem(s.table, 0, 1, true)

	s.detail = NewDetailView()

	s.errorView = NewErrorView()
	s.helpView = NewHelpView()
	s.actionView = NewActionView()
	s.editorConfirm = NewEditorConfirmView()
	s.openInEditor = func(seed string) (string, error) { return editInEditor(s.app, seed) }

	s.content = tview.NewPages().
		AddPage(pageTable, s.tableContainer, true, true).
		AddPage(pageDetail, s.detail, true, false).
		AddPage(pageError, s.errorView, true, false).
		AddPage(pageHelp, s.helpView, true, false).
		AddPage(pageAction, s.actionView, true, false).
		AddPage(pageEditorConfirm, s.editorConfirm, true, false)
	s.content.SetBorder(true)
	s.activeContent = s.table

	s.initFooter()

	s.root = tview.NewGrid().SetRows(3, 0, 1).SetColumns(-1, -1)
	s.root.AddItem(s.headerLeft, 0, 0, 1, 1, 0, 0, false)
	s.root.AddItem(s.headerHint, 0, 1, 1, 1, 0, 0, false)
	s.root.AddItem(s.content, 1, 0, 1, 2, 0, 0, true)
	s.root.AddItem(s.footer, 2, 0, 1, 2, 0, 0, false)

	s.app.SetRoot(s.root, true).SetFocus(s.content)
	s.app.SetInputCapture(s.globalInputCapture)
}

// globalInputCapture handles keys that apply regardless of which content
// view has focus: `q` quits from navigable views, `:` opens the command bar,
// `/` opens the filter (narrows a list's rows, or a detail body's lines,
// including a live-streaming one), `?` toggles the help overlay, and Esc
// pops the view stack (a no-op at the root, or closes help if open).
// While the footer input is active, Up/Down cycle that mode's footer
// history (see cycleFooterHistory) and every other key passes through
// untouched so it can be typed into the input field. While help is open,
// every key is swallowed except q, Esc/`?`, and the scroll keys.
func (s *Shell) globalInputCapture(event *tcell.EventKey) *tcell.EventKey {
	if s.helpOpen {
		if s.footerMode == footerIdle && isQuitKey(event) {
			s.Stop()
			return nil
		}

		switch event.Key() {
		case tcell.KeyEscape:
			s.closeHelp()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
			return event // let the HelpView TextView scroll
		}
		switch event.Rune() {
		case 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone) // vim-style scroll
		case 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone) // vim-style scroll
		case '?':
			s.closeHelp()
		}
		return nil
	}

	if s.footerMode != footerIdle {
		switch event.Key() {
		case tcell.KeyUp:
			s.cycleFooterHistory(-1)
			return nil
		case tcell.KeyDown:
			s.cycleFooterHistory(1)
			return nil
		}
		return event
	}

	// While the action dialog is open every key belongs to its form (typing
	// into a YAML/reason field, Tab between fields/buttons, Enter to confirm,
	// Esc to cancel) — never a global shortcut.
	if s.actionOpen {
		return event
	}

	switch {
	case isQuitKey(event):
		s.Stop()
		return nil
	case event.Key() == tcell.KeyEscape:
		if s.clearActiveFilter() {
			return nil
		}
		s.goBack()
		return nil
	case event.Rune() == ':':
		s.openCommandBar()
		return nil
	case event.Key() == tcell.KeyCtrlA:
		s.toggleCommandPalette()
		return nil
	case event.Rune() == '/':
		switch name, _ := s.content.GetFrontPage(); name {
		case pageTable, pageDetail:
			s.openFilter()
		}
		return nil
	case event.Rune() >= '1' && event.Rune() <= '9':
		if name, _ := s.content.GetFrontPage(); name == pageTable {
			s.toggleSort(int(event.Rune() - '1'))
		}
		return nil
	case event.Key() == tcell.KeyTab:
		if name, _ := s.content.GetFrontPage(); name == pageTable {
			s.cycleFacet(1)
		}
		return nil
	case event.Key() == tcell.KeyBacktab:
		if name, _ := s.content.GetFrontPage(); name == pageTable {
			s.cycleFacet(-1)
		}
		return nil
	case event.Rune() == '?':
		s.openHelp()
		return nil
	case event.Rune() == 'r':
		s.refreshCurrent()
		return nil
	case event.Rune() == 'o':
		s.openInBrowser()
		return nil
	case event.Rune() == 's':
		s.promptSaveToDisk()
		return nil
	case event.Rune() == 'x':
		switch name, _ := s.content.GetFrontPage(); name {
		case pageTable:
			s.toggleExpandColumns()
		case pageDetail:
			s.toggleDetailWrap()
		}
		return nil
	// 'n' only means something on a detail page — falling through to the
	// detail-action keys below (rather than swallowing the key) elsewhere,
	// same reasoning as the 'L' condition just below.
	case event.Rune() == 'n':
		if name, _ := s.content.GetFrontPage(); name != pageDetail {
			break
		}
		s.toggleDetailLineNumbers()
		return nil
	// The condition is part of the case so that an 'L' pressed anywhere a
	// truncated list ISN'T front falls through to the detail-action keys
	// below instead of being swallowed.
	case event.Rune() == 'L' && s.canLoadAllRows():
		s.loadAllRows()
		return nil
	}

	if event.Key() == tcell.KeyRune {
		for _, action := range s.currentDetailActions {
			if event.Rune() == action.Key {
				s.navigateTo(action.Target)
				return nil
			}
		}
		// Mutating actions (Actionable) are dispatched after navigation
		// actions so a resource can't accidentally shadow a navigation key,
		// and resolved against the current target at press time so a
		// list-row action reflects the highlighted row.
		if action, ok := s.resolveActionByKey(event.Rune()); ok {
			s.startAction(action)
			return nil
		}
	}

	return event
}

func isQuitKey(event *tcell.EventKey) bool {
	return event.Key() == tcell.KeyRune && event.Rune() == 'q'
}

// hasFacets reports whether the current list view has a facet tab bar —
// either client-side (Faceted) or server-side (ServerFaceted).
func (s *Shell) hasFacets() bool {
	return s.currentFaceted != nil || s.currentServerFaceted != nil
}

func (s *Shell) setTitle(title string) {
	formatted := "[ Taskcluster"
	if title != "" {
		formatted += " :: " + title
	}
	formatted += " ]"
	s.content.SetTitle(formatted)
}

// updateBorderColor tints s.content's border blue while a filter is active
// on the currently visible list or detail body, so a filtered view is
// distinguishable at a glance rather than only via the title-bar query
// suffix. Blue rather than yellow, since yellow is already used for
// shortcut/header highlights elsewhere and wouldn't stand out here. The
// border is shared by every page in s.content (table/detail/error/help all
// live in the same Pages Box), so this must be recomputed any time either
// the front page, s.filterQuery, or s.detail's own filter changes — not
// just from refreshTable.
func (s *Shell) updateBorderColor() {
	front, _ := s.content.GetFrontPage()
	switch {
	case front == pageTable && s.filterQuery != "":
		s.content.SetBorderColor(tcell.ColorBlue)
	case front == pageDetail && s.detail.FilterQuery() != "":
		s.content.SetBorderColor(tcell.ColorBlue)
	default:
		s.content.SetBorderColor(tview.Styles.BorderColor)
	}
}

// SetInfo renders the persistent header bar's left block (Taskcluster
// root/version/client info), replacing the old ui.UI.SetTaskclusterInfo.
func (s *Shell) SetInfo(root, version, clientID string, authenticated bool) {
	s.rootURL = root

	clientColor := "yellow"
	clientExtra := ""
	if !authenticated {
		clientColor = "red"
		clientExtra = " (not authenticated)"
	}

	s.headerLeft.SetText(fmt.Sprintf(
		" [green]%s[white]\n Taskcluster version: [yellow]%s[white]\n Client ID: [%s]%s[gray]%s[white]",
		root, version, clientColor, clientID, clientExtra,
	))
}

// Start renders the app's initial view — the top of a stack restored via
// RestoreState, if one was, otherwise rootResource — and runs the tview event
// loop. It blocks until Stop() is called.
func (s *Shell) Start(rootResource string) error {
	s.restoreFallback = rootResource
	s.renderRestoredTop()
	return s.app.Run()
}

// StartAt behaves like Start, but jumps straight to name/scope (resolved the
// same way as the `:` command bar — see switchResource) instead of restored
// state or a fallback root resource, discarding any restored navigation
// stack. Used when the CLI is given a positional resource argument. root
// seeds the fallback the command-action cold-start guard renders underneath a
// command-only target (see switchResource), so cancelling or a failed editor
// launch returns to a usable screen instead of a blank one.
//
// The initial dispatch is queued from a separate goroutine (via
// QueueUpdateDraw) rather than called synchronously here, so it lands on the
// event-loop goroutine once Run() is draining updates rather than racing
// ahead of it — required because a command-only resource (:createtask)
// suspends to $EDITOR, which no-ops before the screen exists. Calling
// QueueUpdate synchronously on this goroutine (the same one about to call
// Run()) would deadlock instead.
func (s *Shell) StartAt(root, name, scope string) error {
	s.restoreFallback = root
	go s.app.QueueUpdateDraw(func() { s.runCommand(name, scope) })
	return s.app.Run()
}

func (s *Shell) Stop() {
	s.stopRefreshLoop()
	if s.onStopForTest != nil {
		s.onStopForTest()
	}
	s.app.Stop()
}

// openHelp swaps the content area to the help overlay, remembering which
// page was showing so closeHelp can restore it exactly. It does not touch
// s.stack or the active refresh loop — help is not a navigable place.
func (s *Shell) openHelp() {
	if s.helpOpen {
		return
	}

	s.preHelpPage, _ = s.content.GetFrontPage()
	s.helpOpen = true
	s.helpView.SetData(buildHelpText(s.registry))
	s.content.SwitchToPage(pageHelp)
	s.updateBorderColor()
	s.app.SetFocus(s.helpView)
}

// closeHelp restores whatever content page was showing before openHelp.
func (s *Shell) closeHelp() {
	if !s.helpOpen {
		return
	}

	s.helpOpen = false
	s.content.SwitchToPage(s.preHelpPage)
	s.updateBorderColor()
	s.app.SetFocus(s.activeContent)
}
