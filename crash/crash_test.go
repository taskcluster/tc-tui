package crash

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestHandleCleansUpBeforeReportingAndExits(t *testing.T) {
	var out bytes.Buffer
	var order []string
	code := 0

	handle("boom",
		func() { order = append(order, "cleanup") },
		func(c int) { order = append(order, "exit"); code = c },
		&out,
	)

	if len(order) != 2 || order[0] != "cleanup" || order[1] != "exit" {
		t.Fatalf("want cleanup then exit, got %v", order)
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (what an unguarded panic would have been)", code)
	}
	if got := out.String(); !strings.Contains(got, "boom") || !strings.Contains(got, "goroutine") {
		t.Fatalf("report must carry the panic value and a stack; got %q", got)
	}
}

func TestHandleToleratesNoCleanup(t *testing.T) {
	var out bytes.Buffer
	code := 0

	handle("boom", nil, func(c int) { code = c }, &out)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2 even with no cleanup registered", code)
	}
}

func TestGoRunsTheFunc(t *testing.T) {
	done := make(chan struct{})

	Go(func() { close(done) })

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Go never ran fn")
	}
}

func TestGoCleansUpWhenTheFuncPanics(t *testing.T) {
	var out bytes.Buffer
	exited := make(chan int, 1)

	restore := swapEffects(func(c int) { exited <- c }, &out)
	defer restore()

	SetCleanup(func() { out.WriteString("cleaned:") })
	defer SetCleanup(nil)

	Go(func() { panic("background boom") })

	select {
	case code := <-exited:
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	case <-time.After(time.Second):
		t.Fatal("a panicking goroutine must reach the guard")
	}

	if got := out.String(); !strings.HasPrefix(got, "cleaned:") || !strings.Contains(got, "background boom") {
		t.Fatalf("want cleanup before the panic report; got %q", got)
	}
}

// swapEffects points the package's exit/stderr at test doubles and returns a
// func that puts the real ones back. It takes the package lock the guard reads
// them under, so a still-unwinding goroutine from an earlier test can't race
// with the swap.
func swapEffects(fakeExit func(int), out *bytes.Buffer) func() {
	mu.Lock()
	defer mu.Unlock()

	realExit, realOut := exit, errOut
	exit, errOut = fakeExit, out

	return func() {
		mu.Lock()
		defer mu.Unlock()

		exit, errOut = realExit, realOut
	}
}
