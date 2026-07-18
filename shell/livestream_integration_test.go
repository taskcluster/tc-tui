package shell

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/taskcluster/tc-tui/resource"
)

// fakeLiveStreamerResource implements resource.LiveStreamer: StreamDetail
// relays whatever the test pushes into appendCh until it's closed (stream
// ended) or stop is closed (interrupted, recorded via stopped).
type fakeLiveStreamerResource struct {
	fakeResource
	live       bool
	staticBody string // Describe's body, for the non-live path
	appendCh   chan string
	stopped    chan struct{}
	truncated  bool
	streamErr  error
}

func (f *fakeLiveStreamerResource) Describe(id string) (resource.Detail, error) {
	return resource.Detail{Title: "Static :: " + id, Body: f.staticBody}, nil
}

func (f *fakeLiveStreamerResource) IsLive(id string) bool { return f.live }

func (f *fakeLiveStreamerResource) StreamDetail(id string, stop <-chan struct{}, onStart func(resource.Detail), onAppend func(string)) (bool, error) {
	if f.streamErr != nil {
		return false, f.streamErr
	}
	onStart(resource.Detail{Title: "Live :: " + id})
	for {
		select {
		case text, ok := <-f.appendCh:
			if !ok {
				return f.truncated, nil
			}
			onAppend(text)
		case <-stop:
			if f.stopped != nil {
				close(f.stopped)
			}
			return false, nil
		}
	}
}

// detailText reads the detail view's current text through the UI thread —
// tview widgets aren't safe to read while the app goroutine is mutating
// them.
func detailText(t *testing.T, s *Shell) string {
	t.Helper()
	var text string
	s.app.QueueUpdate(func() { text = s.detail.GetText(true) })
	return text
}

func contentTitle(t *testing.T, s *Shell) string {
	t.Helper()
	var title string
	s.app.QueueUpdate(func() { title = s.content.GetTitle() })
	return title
}

func newLiveStreamShell(t *testing.T, res *fakeLiveStreamerResource) *Shell {
	t.Helper()
	registry := resource.NewRegistry()
	registry.Register(res)
	s := newRunningTestShell(t, registry)
	s.stack.Push(View{ResourceName: res.Name(), Kind: DetailKind, SelectedID: "x"})
	return s
}

func TestLiveStreamAppendsProgressively(t *testing.T) {
	res := &fakeLiveStreamerResource{
		fakeResource: fakeResource{name: "livelog"},
		live:         true,
		appendCh:     make(chan string),
	}
	s := newLiveStreamShell(t, res)

	s.loadDetail(res, "x", true, false, false)

	res.appendCh <- "hello line\n"
	waitFor(t, func() bool { return strings.Contains(detailText(t, s), "hello line") })

	// Content arrives progressively — the second line lands while the
	// stream is still open, and the first line stays on screen.
	res.appendCh <- "second line\n"
	waitFor(t, func() bool { return strings.Contains(detailText(t, s), "second line") })
	if !strings.Contains(detailText(t, s), "hello line") {
		t.Fatalf("earlier content vanished: %q", detailText(t, s))
	}

	if !strings.Contains(contentTitle(t, s), "● LIVE") {
		t.Fatalf("expected a LIVE marker in the title, got %q", contentTitle(t, s))
	}

	close(res.appendCh)
	waitFor(t, func() bool { return strings.Contains(detailText(t, s), "stream ended") })
	waitFor(t, func() bool { return strings.Contains(contentTitle(t, s), "ended") })
}

func TestLiveStreamStopsOnNavigateBack(t *testing.T) {
	res := &fakeLiveStreamerResource{
		fakeResource: fakeResource{name: "livelog"},
		live:         true,
		appendCh:     make(chan string),
		stopped:      make(chan struct{}),
	}
	registry := resource.NewRegistry()
	registry.Register(res)
	s := newRunningTestShell(t, registry)
	s.stack.Push(View{ResourceName: res.Name(), Kind: ListKind})
	s.stack.Push(View{ResourceName: res.Name(), Kind: DetailKind, SelectedID: "x"})

	s.loadDetail(res, "x", true, false, false)
	res.appendCh <- "streaming\n"
	waitFor(t, func() bool { return strings.Contains(detailText(t, s), "streaming") })

	s.app.QueueUpdateDraw(func() { s.goBack() })

	select {
	case <-res.stopped:
	case <-time.After(time.Second):
		t.Fatal("stream was not stopped by navigating away")
	}
}

func TestLiveStreamTruncationBanner(t *testing.T) {
	res := &fakeLiveStreamerResource{
		fakeResource: fakeResource{name: "livelog"},
		live:         true,
		appendCh:     make(chan string),
		truncated:    true,
	}
	s := newLiveStreamShell(t, res)

	s.loadDetail(res, "x", true, false, false)
	res.appendCh <- "some content\n"
	close(res.appendCh)

	waitFor(t, func() bool { return strings.Contains(detailText(t, s), "size cap") })
}

// A stream that fails before producing anything routes to the normal error
// view (with retry), not a blank page with a banner.
func TestLiveStreamEarlyErrorShowsErrorView(t *testing.T) {
	res := &fakeLiveStreamerResource{
		fakeResource: fakeResource{name: "livelog"},
		live:         true,
		streamErr:    errors.New("boom"),
	}
	s := newLiveStreamShell(t, res)

	s.loadDetail(res, "x", true, false, false)

	waitFor(t, func() bool {
		var page string
		s.app.QueueUpdate(func() { page, _ = s.content.GetFrontPage() })
		return page == pageError
	})
}

// A LiveStreamer whose id is NOT currently live must take the ordinary
// one-shot Describe path.
func TestLiveStreamerNotLiveFallsBackToDescribe(t *testing.T) {
	res := &fakeLiveStreamerResource{
		fakeResource: fakeResource{name: "livelog"},
		live:         false,
		staticBody:   "STATIC CONTENT",
	}
	s := newLiveStreamShell(t, res)

	s.loadDetail(res, "x", true, false, false)

	waitFor(t, func() bool { return strings.Contains(detailText(t, s), "STATIC CONTENT") })
}
