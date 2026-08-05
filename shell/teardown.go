package shell

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/taskcluster/tc-tui/crash"
)

// shutdownSignals are the terminations tc-tui has to intercept to hand the
// terminal back. Nothing in the stack below does it: tcell installs a handler
// for SIGWINCH and nothing else, so `kill` (SIGTERM) or closing the terminal
// window (SIGHUP) ends the process with the alternate screen still entered and
// raw/keypad mode still on — the state where the mouse wheel stops scrolling
// scrollback and `reset` is the only cure.
//
// Deliberately not intercepted: SIGKILL (impossible); SIGQUIT, left to the
// runtime's goroutine dump, which is a debugging tool rather than an exit
// path; and SIGINT, which never reaches the process while the UI is up
// (tcell's raw mode turns Ctrl-C into a key event) and during an $EDITOR
// handoff belongs to the editor.
var shutdownSignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP}

const (
	// shutdownGrace is how long a signalled process gets to unwind on its own —
	// enough for app.Run to return and the controller to persist navigation
	// state, short enough not to look hung if the event loop is stuck in a slow
	// call.
	shutdownGrace = 2 * time.Second

	// screenSettleTimeout bounds how long a teardown waits for an in-flight
	// screen transition (see screenGate). Transitions take milliseconds; this
	// is generous enough to never expire on a working event loop and short
	// enough not to delay a shutdown behind a broken one.
	screenSettleTimeout = 500 * time.Millisecond
)

// screenGate serializes handing the terminal off (tcell's disengage, as
// Suspend runs it for an $EDITOR handoff) and taking it back (engage, as
// Resume runs it) against a teardown's Fini.
//
// The two cannot simply run concurrently. tcell's disengage clears its
// `running` flag up front but only afterwards waits for its input loops,
// writes ExitKeypad/ExitCA and restores termios; a Fini landing inside that
// window sees `running == false`, returns without doing any of that work
// itself, and closes the tty out from under the disengage that is still
// writing to it — leaving the wrecked terminal this file exists to prevent.
// engage has the mirror-image window.
//
// Outside those windows a Fini is safe in either resting state, engaged or
// fully suspended (verified against a real pty: Suspend followed by the Fini a
// teardown does, plus the second one tview's own Suspend performs on finding
// the app stopped, returns cleanly on tcell v2.13.10). So the gate is held
// only across the transitions themselves, never for the length of an editor
// session — a teardown mid-edit still restores promptly instead of waiting for
// the user to quit vim.
type screenGate struct {
	// busy holds a token for as long as a transition (or a teardown) is in
	// flight. Capacity 1: it's a mutex that can be acquired with a timeout.
	busy chan struct{}
}

func newScreenGate() *screenGate {
	return &screenGate{busy: make(chan struct{}, 1)}
}

// enter marks a screen transition as starting, blocking while a teardown holds
// the gate — so a handoff can't begin in the middle of a Fini either.
func (g *screenGate) enter() { g.busy <- struct{}{} }

// leave marks the transition complete.
func (g *screenGate) leave() { <-g.busy }

// withSettledScreen runs f once no transition is in flight, holding the gate so
// none can start while it runs.
//
// If a transition hasn't finished within settle, f runs anyway: nothing else
// can restore the terminal, and the only way to sit in a transition that long
// is an event loop already too wedged to finish it.
func (g *screenGate) withSettledScreen(settle time.Duration, f func()) {
	select {
	case g.busy <- struct{}{}:
		defer func() { <-g.busy }()
	case <-time.After(settle):
	}

	f()
}

// installTerminalGuards covers the two ways out of the process that skip
// tview's own screen teardown: a shutdown signal, and a panic on any goroutine
// other than the event loop (see package crash). Called by Start/StartAt
// rather than New, so merely constructing a Shell — as every test does —
// doesn't register process-wide handlers.
func (s *Shell) installTerminalGuards() {
	crash.SetCleanup(func() { s.restoreTerminal() })

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, shutdownSignals...)

	crash.Go(func() { watchShutdown(sigs, s.beginShutdown, os.Exit, shutdownGrace) })
}

// watchShutdown hands the first shutdown signal to shutdown, then ends the
// process. Its effects are parameters so the sequencing can be tested without
// signalling — and killing — the test binary.
func watchShutdown(sigs <-chan os.Signal, shutdown func(os.Signal), exit func(int), grace time.Duration) {
	sig := <-sigs

	shutdown(sig)

	// Restoring the screen makes app.Run return, so the process normally ends
	// itself here — main exits with the status recorded by beginShutdown once
	// the controller has saved navigation state. What follows is the backstop
	// for an event loop too wedged to get there, so a killed tc-tui can't sit
	// on the terminal indefinitely.
	time.Sleep(grace)

	// Shut down once more before giving up: if the signal arrived before Run
	// had created a screen, the first attempt had nothing to hand back and
	// Run may well have engaged one since (runUI closes the common case, but
	// not the sliver between its check and Run taking the app lock).
	shutdown(sig)

	exit(exitStatus(sig))
}

// beginShutdown records the status the process must end with and hands the
// terminal back. The record comes first so that it is already in place by the
// time restoring the screen unblocks Run — main reads it as soon as Run
// returns (see ShutdownExitCode) — and so that runUI can refuse to start a
// screen for a session that is already ending. It is idempotent: watchShutdown
// calls it twice.
func (s *Shell) beginShutdown(sig os.Signal) {
	s.shutdownExit.Store(int32(exitStatus(sig)))
	s.restoreTerminal()
}

// ShutdownExitCode reports the status the process must exit with because a
// signal ended the session rather than the user quitting — the conventional
// 128+signo, which returning normally from main (status 0) would misreport to
// whatever launched tc-tui. ok is false for an ordinary quit.
func (s *Shell) ShutdownExitCode() (int, bool) {
	code := s.shutdownExit.Load()

	return int(code), code != 0
}

// exitStatus reports a signalled process's status the way a shell does.
func exitStatus(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}

	return 1
}

// restoreTerminal hands the terminal back to the shell that owns it: leaves
// the alternate screen, drops raw mode, re-enables scrollback under the mouse
// wheel. Application.Stop is the only route to tcell's Fini, and is safe to
// call from any goroutine (it takes the app lock, and its screen-replacement
// channel is buffered) and safe to call twice (a no-op once the screen is
// gone) — but not safe mid-transition, hence the gate.
func (s *Shell) restoreTerminal() {
	s.screen.withSettledScreen(screenSettleTimeout, s.stopScreen)
}

// runUI starts the event loop, unless a shutdown signal has already been
// handled — in which case there is nothing to gain from engaging a screen the
// process is about to abandon, and something to lose: before Run creates one,
// restoreTerminal has nothing to hand back, so the terminal would be left in
// the alternate screen when the backstop exit fires.
func (s *Shell) runUI() error {
	if _, signalled := s.ShutdownExitCode(); signalled {
		return nil
	}

	return s.runScreen()
}
