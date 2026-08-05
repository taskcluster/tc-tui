package shell

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/taskcluster/tc-tui/crash"
	"github.com/taskcluster/tc-tui/resource"
)

// streamFlushInterval paces how often buffered stream content is pushed to
// the detail view. Every flush is a QueueUpdateDraw onto the single UI/input
// goroutine, so flushing per-line would let a chatty log starve keyboard
// handling — the same pressure that led to the augment redraw throttle.
const streamFlushInterval = 200 * time.Millisecond

// liveTitleSuffix marks a detail title as currently streaming. Deliberately
// tag-free: tview renders Box titles through the same color-tag parser as
// body text, and a plain marker can't interact with it.
const liveTitleSuffix = " ● LIVE"

// runDetailStream is loadDetail's streaming counterpart for a currently-live
// id (see resource.LiveStreamer): it renders content progressively as the
// resource appends it, instead of blocking on a full Describe. Runs on
// loadDetail's fetch goroutine and blocks until the stream ends or is
// stopped; all UI mutation happens via QueueUpdateDraw callbacks, each
// re-checking the same generation/topmost-view guards loadDetail's one-shot
// completion uses.
func (s *Shell) runDetailStream(ls resource.LiveStreamer, id string, gen int, isInitial, isRestore bool) {
	view := View{ResourceName: ls.Name(), Kind: DetailKind, SelectedID: id}
	stop := make(chan struct{})

	// Register stop as the active stream's channel before any content
	// flows, replacing (and ending) whatever stream might still be active —
	// e.g. the one this same view's 'r' refresh is superseding. If this
	// load already lost (a newer navigation started, or the view moved on
	// while IsLive was in flight), bail without ever opening the stream.
	registered := false
	s.app.QueueUpdate(func() {
		if s.isStaleLoad(gen) || !s.isTopView(view) {
			return
		}
		s.stopDetailStream()
		s.stopStream = stop
		registered = true
	})
	if !registered {
		return
	}

	var (
		mu      sync.Mutex
		pending strings.Builder
	)
	flush := func() {
		mu.Lock()
		text := pending.String()
		pending.Reset()
		mu.Unlock()
		if text == "" {
			return
		}
		s.app.QueueUpdateDraw(func() {
			if s.isStaleLoad(gen) || !s.isTopView(view) {
				return
			}
			s.detail.AppendStream(text)
		})
	}

	flusherDone := make(chan struct{})
	crash.Go(func() {
		ticker := time.NewTicker(streamFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				flush()
			case <-flusherDone:
				return
			}
		}
	})

	onStart := func(detail resource.Detail) {
		s.app.QueueUpdateDraw(func() {
			if s.isStaleLoad(gen) || !s.isTopView(view) {
				return
			}

			s.detail.StartStream()
			s.currentDetailActions = detail.Actions
			s.activeContent = s.detail
			s.currentDetailTitle = detail.Title + liveTitleSuffix
			s.refreshDetailTitle()
			s.renderHeaderHints()
			s.renderBreadcrumbs()

			if isInitial && !isRestore && ls.Name() != "history" && s.historyRecorder != nil {
				s.historyRecorder.Record(resource.HistoryEntry{
					ResourceName: ls.Name(),
					Kind:         int(DetailKind),
					SelectedID:   id,
					Title:        detail.Title,
					VisitedAt:    time.Now(),
				})
			}
		})
	}

	appended := false
	truncated, err := ls.StreamDetail(id, stop, onStart, func(text string) {
		appended = true
		mu.Lock()
		pending.WriteString(text)
		mu.Unlock()
	})

	close(flusherDone)
	flush() // whatever's still buffered lands before the end banner

	s.app.QueueUpdateDraw(func() {
		if s.isStaleLoad(gen) || !s.isTopView(view) {
			// Interrupted streams (stop closed by navigation) always land
			// here — the view is no longer on screen, nothing to update.
			return
		}

		if err != nil && !appended {
			// Failed before producing anything — a normal load failure,
			// with the normal retry affordance.
			s.showError(fmt.Sprintf("%s %s", ls.Name(), id), err, func() { s.renderDetail(ls, id, false) })
			return
		}

		banner := "\n[yellow](live stream ended)[white]"
		switch {
		case err != nil:
			banner = fmt.Sprintf("\n[red](live stream failed: %s)[white]", err)
		case truncated:
			banner = "\n[yellow](live stream hit the size cap — press 'o' to open the full log)[white]"
		}
		s.detail.AppendBanner(banner)

		s.currentDetailTitle = strings.TrimSuffix(s.currentDetailTitle, liveTitleSuffix) + " (ended)"
		s.refreshDetailTitle()
	})
}
