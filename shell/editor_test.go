package shell

import (
	"os/exec"
	"testing"
)

func TestResolveEditorPrefersVisualThenEditorThenVi(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	if got := resolveEditor(getenv); got != "vi" {
		t.Fatalf("resolveEditor with nothing set = %q, want vi", got)
	}

	env["EDITOR"] = "nano"
	if got := resolveEditor(getenv); got != "nano" {
		t.Fatalf("resolveEditor with only EDITOR set = %q, want nano", got)
	}

	env["VISUAL"] = "code -w"
	if got := resolveEditor(getenv); got != "code -w" {
		t.Fatalf("resolveEditor with VISUAL set = %q, want it to win over EDITOR", got)
	}
}

func TestClassifyEditorRun(t *testing.T) {
	// A nonexistent editor: sh -c launches, command-not-found, exit 127.
	if err := exec.Command("sh", "-c", "definitely-not-an-editor-xyz").Run(); classifyEditorRun("definitely-not-an-editor-xyz", err) == nil {
		t.Fatal("missing editor (exit 127) must be a launch failure")
	}
	// A real editor that exits non-zero (user aborted) is NOT a launch failure.
	if err := exec.Command("sh", "-c", "exit 1").Run(); classifyEditorRun("ed", err) != nil {
		t.Fatal("a normal non-zero editor exit must not be a launch failure")
	}
	// Success.
	if classifyEditorRun("ed", nil) != nil {
		t.Fatal("a clean exit must classify as no error")
	}
}
