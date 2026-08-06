package shell

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

// fakeRevealableResource is a Detail-only fake standing in for secrets: its
// Describe masks the value and DescribeRevealed doesn't, so a test can tell
// which one the shell actually called just by looking at the body.
type fakeRevealableResource struct {
	fakeResource
	// revealedCalls is bumped from the shell's fetch goroutines, several of
	// which can be in flight at once (that's the point of blockReveal), so
	// it's atomic rather than a plain int.
	revealedCalls atomic.Int32
	// blockReveal, when non-nil, holds the FIRST DescribeRevealed call until
	// it's closed — letting a test land a slow reveal fetch after the user has
	// already toggled 'v' again. Later calls return immediately, so a test can
	// interleave a fast reveal ahead of a slow one.
	blockReveal chan struct{}
	// revealEntered, when non-nil, is closed once that first call has actually
	// entered DescribeRevealed. A keypress only queues work onto the event
	// loop, which then spawns the fetch goroutine — so "the press was handled"
	// is NOT "the fetch is in flight", and a test that toggles again on the
	// former can have the later reveal win the race to be call #1 (and thus
	// the blocked one), inverting exactly the ordering it meant to set up.
	revealEntered chan struct{}
}

func (f *fakeRevealableResource) Describe(id string) (resource.Detail, error) {
	return resource.Detail{Title: "Secret :: " + id, Body: "token: ******"}, nil
}

// DescribeRevealed tags its body with the call number, so a test can tell
// WHICH reveal's response is on screen, not just that some reveal's is.
func (f *fakeRevealableResource) DescribeRevealed(id string) (resource.Detail, error) {
	call := f.revealedCalls.Add(1)
	if call == 1 {
		// Only call 1 ever reaches here, so closing unguarded is safe.
		if f.revealEntered != nil {
			close(f.revealEntered)
		}
		if f.blockReveal != nil {
			<-f.blockReveal
		}
	}
	return resource.Detail{Title: "Secret :: " + id, Body: fmt.Sprintf("token: s3cr3t #%d", call)}, nil
}

func (f *fakeRevealableResource) RefreshInterval() time.Duration { return time.Minute }

func newRevealTestShell(t *testing.T) (*Shell, *fakeRevealableResource) {
	t.Helper()
	return newRevealTestShellFor(t, &fakeRevealableResource{fakeResource: fakeResource{name: "secrets"}})
}

func newRevealTestShellFor(t *testing.T, res *fakeRevealableResource) (*Shell, *fakeRevealableResource) {
	t.Helper()

	registry := resource.NewRegistry()
	registry.Register(res)
	s := newRunningTestShell(t, registry)

	s.app.QueueUpdateDraw(func() { s.showDetail("secrets", "proj/foo") })
	waitFor(t, func() bool { return readDetailBody(s) == "token: ******" })

	return s, res
}

func readDetailBody(s *Shell) string {
	var body string
	s.app.QueueUpdateDraw(func() { body = strings.TrimSpace(s.detail.GetText(true)) })
	return body
}

// waitForClose blocks until ch is closed, failing rather than hanging the
// package's whole test binary if whatever was supposed to close it never
// does. The channel counterpart of waitFor.
func waitForClose(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("channel was never closed")
	}
}

func pressRune(s *Shell, r rune) {
	s.app.QueueUpdateDraw(func() {
		s.globalInputCapture(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	})
}

func TestRevealKeyShowsClearValueAndTogglesBack(t *testing.T) {
	s, res := newRevealTestShell(t)

	pressRune(s, 'v')
	waitFor(t, func() bool { return strings.HasPrefix(readDetailBody(s), "token: s3cr3t") })

	var revealedTitle string
	s.app.QueueUpdateDraw(func() { revealedTitle = s.content.GetTitle() })
	if !strings.Contains(revealedTitle, "[revealed]") {
		t.Fatalf("expected the title to flag the reveal, got %q", revealedTitle)
	}

	pressRune(s, 'v')
	waitFor(t, func() bool { return readDetailBody(s) == "token: ******" })

	if calls := res.revealedCalls.Load(); calls != 1 {
		t.Fatalf("expected exactly one DescribeRevealed call, got %d", calls)
	}
}

// Hiding is the whole point of the key — it can't wait on a network fetch to
// land before the value comes off the screen.
func TestRevealKeyHidesSynchronously(t *testing.T) {
	s, _ := newRevealTestShell(t)

	pressRune(s, 'v')
	waitFor(t, func() bool { return strings.HasPrefix(readDetailBody(s), "token: s3cr3t") })

	var bodyRightAfterHide string
	s.app.QueueUpdateDraw(func() {
		s.globalInputCapture(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone))
		bodyRightAfterHide = s.detail.GetText(true)
	})
	if strings.Contains(bodyRightAfterHide, "s3cr3t") {
		t.Fatalf("expected the cleartext value to be off screen immediately, got %q", bodyRightAfterHide)
	}
}

// The dangerous ordering: a reveal fetch that only lands after the user has
// already pressed 'v' again to hide. Nothing else drops it — same view, same
// load generation — so its result must be discarded on the reveal state
// alone.
func TestSlowRevealLandingAfterHideIsDiscarded(t *testing.T) {
	release := make(chan struct{})
	s, _ := newRevealTestShellFor(t, &fakeRevealableResource{
		fakeResource: fakeResource{name: "secrets"},
		blockReveal:  release,
	})

	// No entry synchronization needed here, unlike the ABA test below: only
	// one reveal is ever dispatched, so there's no second fetch that could
	// race it into becoming the blocked one.
	pressRune(s, 'v') // dispatches the reveal fetch, which blocks
	pressRune(s, 'v') // hide again before it can land
	close(release)

	// Give the now-unblocked fetch a moment to (wrongly) apply its result,
	// since there's no positive event to wait for here.
	time.Sleep(50 * time.Millisecond)
	if body := readDetailBody(s); strings.Contains(body, "s3cr3t") {
		t.Fatalf("a reveal that landed after the hide put cleartext back on screen: %q", body)
	}
}

// The ABA case: reveal → hide → reveal. Both the first and the third state
// are "revealed", so comparing the boolean alone would let the first,
// still-in-flight reveal land on top of the third one's fresher content.
func TestStaleRevealLandingAfterAReRevealIsDiscarded(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	s, res := newRevealTestShellFor(t, &fakeRevealableResource{
		fakeResource:  fakeResource{name: "secrets"},
		blockReveal:   release,
		revealEntered: entered,
	})

	pressRune(s, 'v') // reveal #1 — blocks
	// The press has been handled, but its fetch goroutine may not have run
	// yet. Toggling before it does lets the LATER reveal become call #1 (the
	// blocked one), which is the opposite of the ordering under test.
	waitForClose(t, entered)
	pressRune(s, 'v') // hide
	pressRune(s, 'v') // reveal #2 — returns immediately
	waitFor(t, func() bool { return readDetailBody(s) == "token: s3cr3t #2" })

	close(release) // #1 finally lands, against a view showing #2

	time.Sleep(50 * time.Millisecond)
	if body := readDetailBody(s); body != "token: s3cr3t #2" {
		t.Fatalf("expected the newest reveal to stay on screen, got %q", body)
	}

	if calls := res.revealedCalls.Load(); calls != 2 {
		t.Fatalf("expected two DescribeRevealed calls, got %d", calls)
	}
}

// The detail cache is redrawn from on back-navigation without anyone asking
// for a reveal, so a revealed body must never reach it.
func TestRevealedDetailIsNeverCached(t *testing.T) {
	s, res := newRevealTestShell(t)

	pressRune(s, 'v')
	waitFor(t, func() bool { return strings.HasPrefix(readDetailBody(s), "token: s3cr3t") })

	var cached string
	s.app.QueueUpdateDraw(func() {
		entry, ok := s.detailCache.get(detailCacheKeyFor(res, "proj/foo"), res.RefreshInterval())
		if ok {
			cached = entry.detail.Body
		}
	})
	if cached != "token: ******" {
		t.Fatalf("expected only the masked body to be cached, got %q", cached)
	}
}

// A reveal is scoped to one visit: coming back to the same secret has to ask
// again.
func TestNavigatingBackToARevealedDetailStartsMaskedAgain(t *testing.T) {
	s, _ := newRevealTestShell(t)

	pressRune(s, 'v')
	waitFor(t, func() bool { return strings.HasPrefix(readDetailBody(s), "token: s3cr3t") })

	s.app.QueueUpdateDraw(func() { s.showDetail("secrets", "proj/bar") })
	waitFor(t, func() bool { return readDetailBody(s) == "token: ******" })

	s.app.QueueUpdateDraw(func() { s.goBack() })
	waitFor(t, func() bool { return readDetailBody(s) == "token: ******" })

	var revealed bool
	s.app.QueueUpdateDraw(func() { revealed = s.detailRevealed })
	if revealed {
		t.Fatalf("expected the reveal state to reset on navigation")
	}
}

// An auto-refresh tick (or the 'r' key) of a revealed view keeps the reveal
// rather than silently re-masking under the reader.
func TestRefreshKeepsAnActiveReveal(t *testing.T) {
	s, _ := newRevealTestShell(t)

	pressRune(s, 'v')
	waitFor(t, func() bool { return strings.HasPrefix(readDetailBody(s), "token: s3cr3t") })

	pressRune(s, 'r')
	waitFor(t, func() bool { return strings.HasPrefix(readDetailBody(s), "token: s3cr3t") })
}

func TestRevealHintReflectsCurrentState(t *testing.T) {
	s, _ := newRevealTestShell(t)

	readHints := func() string {
		var hints string
		s.app.QueueUpdateDraw(func() { hints = s.headerHint.GetText(true) })
		return hints
	}

	if hints := readHints(); !strings.Contains(hints, "v reveal values") {
		t.Fatalf("expected a reveal hint on a maskable detail, got %q", hints)
	}

	pressRune(s, 'v')
	waitFor(t, func() bool { return strings.Contains(readHints(), "v hide values") })
}

// fakeVBindingResource binds 'v' itself and isn't Revealable — the global key
// must not swallow it.
type fakeVBindingResource struct {
	fakeResource
}

func (f fakeVBindingResource) Describe(id string) (resource.Detail, error) {
	return resource.Detail{
		Title:   "Widget " + id,
		Body:    "body",
		Actions: []resource.DetailAction{{Key: 'v', Label: "versions", Target: resource.NavTarget{ResourceName: "widgets", ID: "other"}}},
	}, nil
}

func TestRevealKeyFallsThroughToAResourcesOwnVAction(t *testing.T) {
	registry := resource.NewRegistry()
	registry.Register(fakeVBindingResource{fakeResource: fakeResource{name: "widgets"}})
	s := newRunningTestShell(t, registry)

	s.app.QueueUpdateDraw(func() { s.showDetail("widgets", "a") })
	waitFor(t, func() bool {
		var title string
		s.app.QueueUpdateDraw(func() { title = s.currentDetailTitle })
		return title == "Widget a"
	})

	pressRune(s, 'v')
	waitFor(t, func() bool {
		var title string
		s.app.QueueUpdateDraw(func() { title = s.currentDetailTitle })
		return title == "Widget other"
	})
}
