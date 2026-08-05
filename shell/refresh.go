package shell

import (
	"time"

	"github.com/taskcluster/tc-tui/crash"
)

// isTopView reports whether view is currently the topmost view on the
// stack.
func (s *Shell) isTopView(view View) bool {
	top, ok := s.stack.Top()
	return ok && top == view
}

// isStaleLoad reports whether gen (a value captured at dispatch time) no
// longer matches the shell's current loadGeneration — i.e. a newer
// loadList/loadDetail dispatch has started since this one, so this
// request's completion must no-op regardless of what isTopView would say.
func (s *Shell) isStaleLoad(gen int) bool {
	return gen != s.loadGeneration
}

// Invalidate re-fetches the given view if it is currently the topmost view,
// and redraws. Called by the refresh ticker today; a future push-event
// listener (e.g. Pulse) could call this directly instead of on a timer,
// without any view-layer changes.
func (s *Shell) Invalidate(view View) {
	if !s.isTopView(view) {
		return
	}

	res, ok := s.registry.Resolve(view.ResourceName)
	if !ok {
		return
	}

	switch view.Kind {
	case ListKind:
		s.loadList(res, view.Scope, s.currentFacetValue, false, true, false)
	case DetailKind:
		s.loadDetail(res, view.SelectedID, false, false, false)
	}
}

// refreshCurrent force re-fetches the topmost view, bypassing the list
// cache — the global `r` key's action. It reuses Invalidate rather than
// duplicating its fetch/error-handling logic, so a manual refresh behaves
// exactly like an auto-refresh tick (silent failure keeps the last-good
// render and shows a transient warning).
func (s *Shell) refreshCurrent() {
	top, ok := s.stack.Top()
	if !ok {
		return
	}

	s.Invalidate(top)
}

// startRefreshLoop stops any existing ticker and, if interval > 0, starts a
// new one that invalidates view for as long as it stays topmost on the
// stack.
func (s *Shell) startRefreshLoop(view View, interval time.Duration) {
	s.stopRefreshLoop()

	if interval <= 0 {
		return
	}

	stop := make(chan struct{})
	s.stopRefresh = stop

	crash.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.app.QueueUpdateDraw(func() {
					s.Invalidate(view)
				})
			case <-stop:
				return
			}
		}
	})
}

func (s *Shell) stopRefreshLoop() {
	s.stopDetailStream()

	if s.stopRefresh != nil {
		close(s.stopRefresh)
		s.stopRefresh = nil
	}
}

// stopDetailStream ends any in-flight detail stream (see runDetailStream).
// Folded into stopRefreshLoop — which every navigation, error view, and new
// refresh loop already goes through — so a live stream can never outlive the
// view it was rendering into.
func (s *Shell) stopDetailStream() {
	if s.stopStream != nil {
		close(s.stopStream)
		s.stopStream = nil
	}
}
