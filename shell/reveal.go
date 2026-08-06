package shell

import "github.com/taskcluster/tc-tui/resource"

// currentRevealable resolves the resource.Revealable and entity id for the
// Detail view currently on top of the stack — what the 'v' key acts on, and
// what renderHeaderHints checks before advertising it.
//
// Resolved from the view stack rather than the front content page (unlike
// e.g. toggleDetailLineNumbers' own gate) because renderDetail calls
// renderHeaderHints before it switches pages: keying the hint off the front
// page would drop it from the first render of every secret. The 'v' key's
// own dispatch still checks the front page, so the hint and the key can't
// disagree about whether a detail view is actually showing.
func (s *Shell) currentRevealable() (res resource.Revealable, id string, ok bool) {
	top, hasTop := s.stack.Top()
	if !hasTop || top.Kind != DetailKind {
		return nil, "", false
	}

	resolved, found := s.registry.Resolve(top.ResourceName)
	if !found {
		return nil, "", false
	}

	revealable, isRevealable := resolved.(resource.Revealable)
	if !isRevealable {
		return nil, "", false
	}

	return revealable, top.SelectedID, true
}

// canToggleReveal reports whether the 'v' key applies right now: a Detail
// view of a Revealable resource is the one actually on screen.
func (s *Shell) canToggleReveal() bool {
	if name, _ := s.content.GetFrontPage(); name != pageDetail {
		return false
	}

	_, _, ok := s.currentRevealable()
	return ok
}

// setDetailRevealed is the only writer of s.detailRevealed and s.revealEpoch,
// so the epoch can't drift from the state it describes. It advances on every
// call, including one setting the value it already held (renderDetail's
// per-navigation reset), so it stays a monotonic counter rather than a mirror
// of the boolean — see the fields' doc comment.
func (s *Shell) setDetailRevealed(revealed bool) {
	s.detailRevealed = revealed
	s.revealEpoch++
}

// toggleDetailReveal flips the 'v' key's reveal state for the Detail view on
// screen and re-fetches it through the other Describe (see describeDetail).
//
// Hiding has to take effect on screen immediately: that re-fetch is
// asynchronous, and leaving the revealed body up until it lands is exactly
// what the user just asked to stop. The cached body redraws without a gap
// when it's still fresh — loadDetail only ever caches masked ones — and
// otherwise the body is blanked until the fetch replaces it.
func (s *Shell) toggleDetailReveal() {
	res, id, ok := s.currentRevealable()
	if !ok {
		return
	}

	s.setDetailRevealed(!s.detailRevealed)

	if !s.detailRevealed {
		masked := resource.Detail{Title: s.currentDetailTitle}
		if entry, hit := s.detailCache.get(detailCacheKeyFor(res, id), res.RefreshInterval()); hit {
			masked = entry.detail
		}
		s.detail.UpdateData(masked)
	}

	s.refreshDetailTitle()
	s.renderHeaderHints()

	// isInitial=false: this is a re-fetch of the view already on screen, so
	// its result should land like a refresh (UpdateData, keeping scroll; a
	// transient warning on failure) rather than blanking to an error page.
	s.loadDetail(res, id, false, false, false)
}

// describeDetail fetches id's Detail, taking the unmasked path only for a
// Revealable resource the user has actually revealed; everything else gets
// the ordinary Describe.
func describeDetail(res resource.Resource, id string, revealed bool) (resource.Detail, error) {
	if revealed {
		if revealable, ok := res.(resource.Revealable); ok {
			return revealable.DescribeRevealed(id)
		}
	}

	return res.Describe(id)
}
