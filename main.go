package main

import (
	"fmt"
	"os"

	"github.com/taskcluster/tc-tui/controller"
)

func main() {
	name, scope, showHelp, showVersion := parseArgs(os.Args[1:])

	if showVersion {
		printVersion()
		return
	}

	// Help is printed before any controller (and thus Taskcluster client)
	// exists, so it works without TASKCLUSTER_ROOT_URL set.
	if showHelp {
		printUsage(controller.HelpText())
		return
	}

	ctrl := controller.NewController()

	var err error
	if name != "" {
		err = ctrl.StartUIAt(name, scope)
	} else {
		err = ctrl.StartUI()
	}

	if err != nil {
		panic(err)
	}

	// A session ended by SIGTERM/SIGHUP unwinds through the same clean path as
	// `q`, so without this a killed tc-tui would report success.
	if reporter, ok := ctrl.(controller.ShutdownReporter); ok {
		if code, signalled := reporter.ShutdownExitCode(); signalled {
			os.Exit(code)
		}
	}
}

// parseArgs splits argv into the -h/--help and -v/--version flags plus up to
// two positional arguments: a resource name/alias and its scope/id, e.g.
// `tc-tui wp proj-taskcluster/ci` or `tc-tui pending proj-taskcluster/ci` —
// resolved the same way as `:name scope` in the command bar.
func parseArgs(args []string) (name, scope string, showHelp, showVersion bool) {
	var positional []string

	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			showHelp = true
		case "-v", "--version":
			showVersion = true
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) > 0 {
		name = positional[0]
	}
	if len(positional) > 1 {
		scope = positional[1]
	}

	return
}

func printUsage(resourceHelp string) {
	fmt.Print(`Usage: tc-tui [flags] [resource] [scope|id]

Flags:
  -h, --help     show this help text and exit
  -v, --version  show the client version and build hash and exit

Positional args (optional — jump straight to a view instead of the last
session's or the default root):
  resource       resource name or alias, e.g. wp, workers, task
  scope|id       scope or id for that resource, e.g. proj-taskcluster/ci

Examples:
  tc-tui                              resume the last session (or the command palette)
  tc-tui wp proj-taskcluster/ci       open that worker pool directly
  tc-tui pending proj-taskcluster/ci  open its pending tasks
  tc-tui task <taskId>                open a task directly

`)
	fmt.Print(resourceHelp)
}
