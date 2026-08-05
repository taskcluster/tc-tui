package shell

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/taskcluster/tc-tui/resource"
)

func TestWatchShutdownRetriesTheShutdownBeforeTheBackstopExit(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	sigs <- syscall.SIGTERM

	var order []string
	var handed []os.Signal
	exited := make(chan int, 1)

	watchShutdown(sigs,
		func(sig os.Signal) { order = append(order, "shutdown"); handed = append(handed, sig) },
		func(code int) { order = append(order, "exit"); exited <- code },
		time.Millisecond,
	)

	// Twice: the first attempt is a no-op if the signal beat Run to creating a
	// screen, so the backstop has to try again rather than exit on the
	// assumption that the first one landed.
	want := []string{"shutdown", "shutdown", "exit"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("teardown order = %v, want %v", order, want)
	}
	for _, sig := range handed {
		if sig != syscall.SIGTERM {
			t.Fatalf("handled signal = %v, want SIGTERM", sig)
		}
	}
	if code := <-exited; code != 128+int(syscall.SIGTERM) {
		t.Fatalf("backstop exit code = %d, want %d (128+SIGTERM)", code, 128+int(syscall.SIGTERM))
	}
}

func TestExitStatusFollowsTheShellConvention(t *testing.T) {
	if got := exitStatus(syscall.SIGHUP); got != 129 {
		t.Fatalf("exitStatus(SIGHUP) = %d, want 129", got)
	}
	if got := exitStatus(os.Interrupt); got != 130 {
		t.Fatalf("exitStatus(os.Interrupt) = %d, want 130", got)
	}
}

func TestRestoreTerminalStopsTheScreen(t *testing.T) {
	s := New(resource.NewRegistry())
	stopped := false
	s.stopScreen = func() { stopped = true }

	s.restoreTerminal()

	if !stopped {
		t.Fatal("restoreTerminal must stop the app — the only route to tcell's Fini")
	}
}

// The gate's whole point: tcell clears its running flag at the start of a
// transition but restores termios and leaves the alternate screen at the end,
// so a Fini landing in between does neither and closes the tty out from under
// the transition that would have.
func TestRestoreTerminalWaitsForAnInFlightScreenTransition(t *testing.T) {
	s := New(resource.NewRegistry())
	stopped := make(chan struct{})
	s.stopScreen = func() { close(stopped) }

	s.screen.enter() // a transition begins (tcell mid-disengage)

	go s.restoreTerminal()

	select {
	case <-stopped:
		t.Fatal("the screen was stopped in the middle of a transition")
	case <-time.After(50 * time.Millisecond):
	}

	s.screen.leave() // transition complete; now Fini is safe

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("restoreTerminal must proceed once the transition finishes")
	}
}

func TestRestoreTerminalGivesUpWaitingRatherThanSkippingTheRestore(t *testing.T) {
	s := New(resource.NewRegistry())
	stopped := make(chan struct{})
	s.stopScreen = func() { close(stopped) }

	s.screen.enter() // a transition that never finishes (a wedged event loop)
	defer s.screen.leave()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.screen.withSettledScreen(20*time.Millisecond, s.stopScreen)
	}()

	select {
	case <-done:
		<-stopped // must have restored anyway rather than skipped it
	case <-time.After(time.Second):
		t.Fatal("a stuck transition must not block the restore indefinitely")
	}
}

// suspendScreen must hold the gate across tcell's disengage/engage but release
// it for the editor itself, so a teardown mid-edit restores promptly instead of
// waiting for the user to quit their editor.
func TestSuspendScreenHoldsTheGateOnlyAcrossTheTransitions(t *testing.T) {
	s := New(resource.NewRegistry())
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	s.app.SetScreen(screen)

	gateFreeDuringEditor := false
	ok := s.suspendScreen(func() {
		gateFreeDuringEditor = s.gateIsFree()
	})

	if !ok {
		t.Fatal("Suspend reported no screen to suspend")
	}
	if !gateFreeDuringEditor {
		t.Fatal("the gate must be free while the editor runs, or a teardown waits for the editor")
	}
	if !s.gateIsFree() {
		t.Fatal("the gate must be released once Suspend returns")
	}
}

func TestSuspendScreenLeavesTheGateBalancedWithoutAScreen(t *testing.T) {
	s := New(resource.NewRegistry())

	if s.suspendScreen(func() { t.Error("f must not run when there is no screen to suspend") }) {
		t.Fatal("Suspend must report failure when no screen exists")
	}
	if !s.gateIsFree() {
		t.Fatal("the gate must be released even when Suspend skips the handoff")
	}
	if _, err := s.editInEditor("seed"); !errors.Is(err, errEditorScreenUnavailable) {
		t.Fatalf("editInEditor without a screen = %v, want errEditorScreenUnavailable", err)
	}
}

func TestBeginShutdownRecordsTheExitStatusBeforeRestoring(t *testing.T) {
	s := New(resource.NewRegistry())

	if code, signalled := s.ShutdownExitCode(); signalled || code != 0 {
		t.Fatalf("an unsignalled session must report no exit code; got %d, %v", code, signalled)
	}

	// Recording has to happen first: restoring the screen is what unblocks Run,
	// and main reads the status the moment Run returns.
	recorded := false
	s.stopScreen = func() {
		_, recorded = s.ShutdownExitCode()
	}

	s.beginShutdown(syscall.SIGHUP)

	if !recorded {
		t.Fatal("the exit status must be recorded before the screen is handed back")
	}
	code, signalled := s.ShutdownExitCode()
	if !signalled || code != 129 {
		t.Fatalf("ShutdownExitCode() = %d, %v; want 129, true (128+SIGHUP)", code, signalled)
	}
}

// A signal can arrive before Run has created a screen, where restoreTerminal
// has nothing to hand back. Engaging one afterwards would mean exiting with the
// terminal still in the alternate screen.
func TestRunUIRefusesToStartAScreenForAnAlreadySignalledSession(t *testing.T) {
	s := New(resource.NewRegistry())
	ran := false
	s.runScreen = func() error { ran = true; return nil }
	s.stopScreen = func() {}

	s.beginShutdown(syscall.SIGTERM)

	if err := s.runUI(); err != nil {
		t.Fatalf("runUI() = %v, want nil", err)
	}
	if ran {
		t.Fatal("the event loop must not start for a session that is already shutting down")
	}
}

func TestRunUIStartsTheEventLoopNormally(t *testing.T) {
	s := New(resource.NewRegistry())
	wantErr := errors.New("loop failed")
	s.runScreen = func() error { return wantErr }

	if err := s.runUI(); !errors.Is(err, wantErr) {
		t.Fatalf("runUI() = %v, want the event loop's own error", err)
	}
}

// gateIsFree reports whether the screen gate is currently unheld. Only
// meaningful in tests, where the observation is made from a goroutine that
// isn't itself contending for it.
func (s *Shell) gateIsFree() bool {
	select {
	case s.screen.busy <- struct{}{}:
		<-s.screen.busy
		return true
	default:
		return false
	}
}
