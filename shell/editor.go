package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/rivo/tview"
)

// errEditorScreenUnavailable is returned by editInEditor when
// Application.Suspend reports the terminal wasn't ready to hand off (e.g. the
// event loop hasn't started yet) — never assume the editor ran.
var errEditorScreenUnavailable = errors.New("editor unavailable: terminal not ready")

// resolveEditor picks the editor command to run: $VISUAL, then $EDITOR, then
// "vi" as a last resort. getenv is a seam for tests.
func resolveEditor(getenv func(string) string) string {
	if v := getenv("VISUAL"); v != "" {
		return v
	}
	if e := getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// classifyEditorRun maps the result of running the editor via `sh -c` to a
// launch error. Shell exit 126/127 means the editor was not found or not
// executable — a launch failure; any other non-zero exit is the editor's own
// (an aborted/normal exit the confirm screen handles).
func classifyEditorRun(editor string, err error) error {
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code == 126 || code == 127 {
			return fmt.Errorf("editor %q not found or not executable (exit %d)", editor, code)
		}
		return nil // editor ran and exited non-zero; not a launch failure
	}
	return fmt.Errorf("launch %q: %w", editor, err)
}

// editInEditor seeds a temp file with seed, suspends the tview screen to hand
// the terminal to $EDITOR/$VISUAL, and returns the file's contents once the
// editor exits. It must be called on the event-loop goroutine (an input
// handler, or a queued initial dispatch) — never wrapped in another
// QueueUpdate/Draw, which would deadlock (see runEditorHandoff).
func editInEditor(app *tview.Application, seed string) (string, error) {
	f, err := os.CreateTemp("", "tc-tui-task-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.WriteString(seed); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	editor := resolveEditor(os.Getenv)
	var runErr error
	ok := app.Suspend(func() {
		cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path) // #nosec G204
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = classifyEditorRun(editor, cmd.Run())
	})
	if !ok {
		return "", errEditorScreenUnavailable
	}
	if runErr != nil {
		return "", runErr
	}
	out, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}
	return string(out), nil
}
