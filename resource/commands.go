package resource

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// CommandsResource is the command palette: one searchable, navigable list of
// every command the `:` bar accepts. Selecting a row runs that command exactly
// as typing it would (NavCommand) — including a command-only resource such as
// `:createtask`, which fires its action directly rather than opening an empty
// list view.
//
// It reads the very Registry the shell resolves commands against, rather than
// its own hand-maintained table, so it can neither list something the command
// bar would reject nor miss something it would accept.
type CommandsResource struct {
	registry *Registry
}

// CommandsResourceName is the registry name the shell opens for its Ctrl-A
// palette key. Exported so that binding doesn't hardcode the string on the
// shell side.
const CommandsResourceName = "commands"

// BuiltinCommand is a command the shell answers itself rather than by
// resolving a Resource — `:help`, `:quit`. They're not in the Registry (there's
// nothing to list or describe), but they're still commands, so the palette has
// to show them. Single source of truth for both sides: the palette renders
// these as rows and the shell dispatches against the same list (see
// Shell.runCommand).
type BuiltinCommand struct {
	Name        string
	Aliases     []string
	Description string
}

// BuiltinCommands returns the shell's non-Resource commands, in palette order.
func BuiltinCommands() []BuiltinCommand {
	return []BuiltinCommand{
		{Name: "help", Description: "Show the help screen — every global key and every resource"},
		{Name: "quit", Aliases: []string{"q"}, Description: "Quit tc-tui (same as the global 'q' key)"},
	}
}

// ResolveBuiltin looks a builtin up by name or alias, case-insensitively —
// the same matching Registry.Resolve does for resources.
func ResolveBuiltin(nameOrAlias string) (BuiltinCommand, bool) {
	key := strings.ToLower(strings.TrimSpace(nameOrAlias))

	for _, cmd := range BuiltinCommands() {
		if key == cmd.Name {
			return cmd, true
		}
		for _, alias := range cmd.Aliases {
			if key == strings.ToLower(alias) {
				return cmd, true
			}
		}
	}

	return BuiltinCommand{}, false
}

func NewCommandsResource(registry *Registry) *CommandsResource {
	return &CommandsResource{registry: registry}
}

func (r *CommandsResource) Name() string { return CommandsResourceName }

func (r *CommandsResource) Aliases() []string {
	return []string{"cmd", "cmds", "alias", "aliases"}
}

func (r *CommandsResource) Description() string {
	return "Every available command and its aliases — select a row to run it"
}

func (r *CommandsResource) IsPeek() {}

func (r *CommandsResource) Columns() []Column {
	return []Column{
		{Title: "COMMAND", Width: 22},
		{Title: "ALIASES", Width: 26},
		{Title: "ARGUMENT", Width: 22},
		// Width is a truncation cap; Expand lets the description also soak up
		// whatever terminal width the fixed columns leave over.
		{Title: "DESCRIPTION", Width: 60, Expand: true},
	}
}

func (r *CommandsResource) List() ([]Row, error) {
	rows := make([]Row, 0, len(r.registry.Names())+len(BuiltinCommands()))

	for _, name := range r.registry.Names() {
		res, ok := r.registry.Resolve(name)
		if !ok {
			continue
		}
		rows = append(rows, commandRow(
			res.Name(), res.Aliases(), commandArgument(res), res.Description(),
		))
	}

	// The shell's own commands aren't Resources, but they're still things you
	// can type after `:` — see BuiltinCommand.
	for _, cmd := range BuiltinCommands() {
		rows = append(rows, commandRow(cmd.Name, cmd.Aliases, runsAtOnceArgument, cmd.Description))
	}

	// Alphabetical rather than registration order — a palette is for looking
	// things up, and it interleaves the builtins rather than stranding them
	// at the end.
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	return rows, nil
}

func commandRow(name string, aliases []string, argument, description string) Row {
	return Row{
		ID:        name,
		Cells:     []string{name, strings.Join(aliases, ", "), argument, description},
		NavTarget: &NavTarget{ResourceName: name, Kind: NavCommand},
	}
}

// Describe is unreachable by ordinary navigation — every row carries a
// NavTarget, so selecting one runs the command instead. It's here for the
// `:commands <name>` spelling, and to satisfy Resource.
func (r *CommandsResource) Describe(id string) (Detail, error) {
	var name, aliases, argument, description string

	if res, ok := r.registry.Resolve(id); ok {
		name, argument, description = res.Name(), commandArgument(res), res.Description()
		aliases = strings.Join(res.Aliases(), ", ")
	} else {
		cmd, isBuiltin := ResolveBuiltin(id)
		if !isBuiltin {
			return Detail{}, fmt.Errorf("unknown command %q", id)
		}
		name, argument, description = cmd.Name, runsAtOnceArgument, cmd.Description
		aliases = strings.Join(cmd.Aliases, ", ")
	}

	if aliases == "" {
		aliases = "none"
	}
	if argument == "" {
		argument = "none"
	}

	body := fmt.Sprintf(
		"[green]Command:[white]     :%s\n"+
			"[green]Aliases:[white]     %s\n"+
			"[green]Argument:[white]    %s\n\n"+
			"%s\n",
		name, aliases, argument, description,
	)

	return Detail{Title: name, Body: body}, nil
}

// RefreshInterval is 0: the registry is built once at startup and never
// mutated, so there is nothing to re-poll.
func (r *CommandsResource) RefreshInterval() time.Duration { return 0 }

// runsAtOnceArgument marks a command that takes no argument and fires the
// moment it's picked, rather than opening a list to browse.
const runsAtOnceArgument = "(runs at once)"

// commandArgument summarises what res expects after its name in the command
// bar, for the palette's ARGUMENT column. Order matters: each of these method
// sets is a superset of the ones below it, and a type switch takes the first
// match.
func commandArgument(res Resource) string {
	switch typed := res.(type) {
	case CommandAction:
		return runsAtOnceArgument
	case RootBrowsable:
		// Square brackets rather than angle: a RootBrowsable opens its root
		// list when the argument is omitted, so it's optional.
		return "[" + typed.IDPromptLabel() + "]"
	case DirectLookup:
		return "<" + typed.IDPromptLabel() + ">"
	case ScopePrompt:
		return "<" + typed.ScopePromptLabel() + ">"
	case ScopedResource:
		return "<id>"
	}
	return ""
}
