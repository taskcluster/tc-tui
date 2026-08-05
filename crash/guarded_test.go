package crash

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryProductionGoroutineIsGuarded enforces the guarantee the rest of this
// package only offers: that no production goroutine is started with a bare `go`
// statement, since a panic on one takes the process down with the terminal
// still in the alternate screen. That includes goroutines spawned from inside a
// guarded one — panics don't propagate between goroutines, so being called from
// inside crash.Go buys a nested `go func()` nothing.
func TestEveryProductionGoroutineIsGuarded(t *testing.T) {
	fset := token.NewFileSet()
	var bare []string

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "crash" { // crash.Go's own `go func()`
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if stmt, ok := n.(*ast.GoStmt); ok {
				bare = append(bare, fset.Position(stmt.Go).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repo: %v", err)
	}

	if len(bare) > 0 {
		t.Fatalf("these goroutines are started with a bare `go` and would crash with an unrestored terminal — use crash.Go:\n\t%s",
			strings.Join(bare, "\n\t"))
	}
}
