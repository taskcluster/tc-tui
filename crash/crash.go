// Package crash keeps a panic on a background goroutine from wrecking the
// terminal on its way out.
//
// tview recovers panics raised on its own event-loop goroutine only (see
// Application.Run), where it Fini-s the screen before re-panicking. Every
// other goroutine — the shell's list/detail fetches and refresh tickers, a
// resource's per-row enrichment calls — takes the process straight down,
// leaving the terminal in the alternate screen with raw and keypad mode still
// on: the state where the mouse wheel no longer scrolls scrollback and
// `reset` is the only way back. Go has no process-wide panic hook to fix that
// centrally, so background goroutines are started via Go and the program
// registers a cleanup via SetCleanup at startup.
//
// The panic is deliberately not swallowed: cleanup runs, then the panic and
// its stack are printed and the process exits 2 — exactly what would have
// happened unguarded, only with a usable terminal to read it in.
package crash

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
)

// mu guards all three of the guard's collaborators below. cleanup is set once
// at startup but read from arbitrary goroutines; exit and errOut are seams so
// tests can observe the guard's side effects instead of being killed by the
// binary they're testing. Production never reassigns them.
var (
	mu      sync.Mutex
	cleanup func()

	exit             = os.Exit
	errOut io.Writer = os.Stderr
)

// SetCleanup registers the func to run before a guarded panic ends the
// process — here, restoring the terminal. A later call replaces the earlier
// one; with none registered a guarded panic still prints and exits, just with
// nothing to clean up first.
func SetCleanup(f func()) {
	mu.Lock()
	defer mu.Unlock()

	cleanup = f
}

func effects() (func(), func(int), io.Writer) {
	mu.Lock()
	defer mu.Unlock()

	return cleanup, exit, errOut
}

// Go runs fn on a new goroutine with the guard installed — a drop-in
// replacement for `go fn()` anywhere the process would otherwise die with the
// screen still engaged.
func Go(fn func()) {
	go func() {
		defer guard()

		fn()
	}()
}

// guard has to be deferred directly by the goroutine's top-level func, since
// recover only reports a panic when called from a function that func deferred.
func guard() {
	p := recover()
	if p == nil {
		return
	}

	cleanup, exit, out := effects()
	handle(p, cleanup, exit, out)
}

func handle(p any, cleanup func(), exit func(int), out io.Writer) {
	// Captured before cleanup, which would otherwise bury the panicking
	// frames under its own.
	stack := debug.Stack()

	if cleanup != nil {
		cleanup()
	}

	fmt.Fprintf(out, "panic: %v\n\n%s", p, stack)
	exit(2)
}
