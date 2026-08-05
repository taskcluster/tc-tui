package resource

import (
	"fmt"
	"sync"
	"time"
)

// HistoryEntry is one row of the history log. Kind mirrors shell.ViewKind (0
// = list, 1 = detail) as a plain int — the same pattern state.ViewState
// already uses — so this package has no dependency on shell.
type HistoryEntry struct {
	ResourceName string
	Kind         int
	SelectedID   string
	Scope        string
	Title        string // Detail.Title for a detail visit; "" for a scoped-list visit
	VisitedAt    time.Time
}

// HistoryRecorder is implemented by HistoryResource. The shell records
// visits and round-trips persisted state through this interface rather than
// depending on HistoryResource's concrete type.
type HistoryRecorder interface {
	Record(entry HistoryEntry)
	Entries() []HistoryEntry
	Restore(entries []HistoryEntry)
}

const historyMaxEntries = 500

type HistoryResource struct {
	mu      sync.Mutex
	entries []HistoryEntry // most-recent-first
}

func NewHistoryResource() *HistoryResource { return &HistoryResource{} }

func (r *HistoryResource) Name() string      { return "history" }
func (r *HistoryResource) Aliases() []string { return []string{"hist"} }
func (r *HistoryResource) Description() string {
	return "Chronological log of visited resources — select a row to jump back to it"
}

func (r *HistoryResource) IsPeek() {}

func (r *HistoryResource) Columns() []Column {
	return []Column{
		{Title: "RESOURCE TYPE", Width: 20},
		// Capped, unlike most identity columns: a worker recent-task/detail
		// visit's SelectedID is "workerPoolId::workerGroup::workerId", which
		// can run well past 80 characters and, left flexible (Width: 0),
		// crowded WHEN/TITLE down to a sliver. Press 'x' to see the full id.
		{Title: "RESOURCE ID", Width: 55},
		{Title: "WHEN", Width: 25},
		// Width is a baseline/truncation cap (like TITLE elsewhere — see
		// workerpoolerrors.go); Expand lets it still grow into whatever
		// terminal width is left over once every other column has claimed
		// its own share, rather than being stuck at exactly 40 on a wide
		// terminal. Left fully flexible (Width: 0) it was exempt from the
		// 'x' truncation toggle entirely (uncapped columns always render
		// uncapped) and its rendered width swung unpredictably with
		// whatever space RESOURCE ID left over.
		{Title: "TITLE", Width: 40, Expand: true},
	}
}

func (r *HistoryResource) List() ([]Row, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows := make([]Row, 0, len(r.entries))
	for _, e := range r.entries {
		id := e.SelectedID
		kind := NavDetail
		if e.Kind == 0 { // list visit — id column shows the scope instead
			id = e.Scope
			kind = NavScopedList
		}

		rows = append(rows, Row{
			ID: historyKey(e),
			Cells: []string{
				e.ResourceName,
				id,
				e.VisitedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
				e.Title,
			},
			NavTarget: &NavTarget{ResourceName: e.ResourceName, ID: id, Kind: kind},
		})
	}
	return rows, nil
}

// Describe is unreachable in normal use: every row's NavTarget overrides
// selection (see resource.Row.NavTarget), so the shell never calls
// Describe("history", ...). Implemented only to satisfy the Resource
// interface.
func (r *HistoryResource) Describe(id string) (Detail, error) {
	return Detail{}, fmt.Errorf("history entries are not viewable directly")
}

func (r *HistoryResource) RefreshInterval() time.Duration { return 0 }

func historyKey(e HistoryEntry) string {
	return fmt.Sprintf("%s|%d|%s|%s", e.ResourceName, e.Kind, e.SelectedID, e.Scope)
}

// Record removes any existing entry with the same identity and re-inserts e
// at the front (most-recent-first), then truncates to historyMaxEntries.
func (r *HistoryResource) Record(e HistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := historyKey(e)
	for i, existing := range r.entries {
		if historyKey(existing) == key {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			break
		}
	}
	r.entries = append([]HistoryEntry{e}, r.entries...)
	if len(r.entries) > historyMaxEntries {
		r.entries = r.entries[:historyMaxEntries]
	}
}

func (r *HistoryResource) Entries() []HistoryEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]HistoryEntry(nil), r.entries...)
}

// Restore replaces the in-memory log wholesale (used once, before Start),
// trusting the persisted order but still defensively truncating to
// historyMaxEntries.
func (r *HistoryResource) Restore(entries []HistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(entries) > historyMaxEntries {
		entries = entries[:historyMaxEntries]
	}
	r.entries = append([]HistoryEntry(nil), entries...)
}
