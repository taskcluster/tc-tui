package shell

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/tview"

	"github.com/taskcluster/tc-tui/resource"
)

// HelpView renders the help overlay's body (global keys + registry-derived
// resource list). It has no state beyond what SetData is given each time
// help opens, since buildHelpText regenerates the full body from the live
// registry on every open.
type HelpView struct {
	*tview.TextView
}

func NewHelpView() *HelpView {
	h := &HelpView{TextView: tview.NewTextView()}
	h.SetDynamicColors(true).SetWordWrap(true)

	return h
}

func (h *HelpView) SetData(text string) {
	h.Clear().SetText(text).ScrollToBeginning()
}

// buildHelpText renders the full help body from the live registry, so it
// can never drift out of sync with which resources are actually registered.
func buildHelpText(registry *resource.Registry) string {
	var b strings.Builder

	b.WriteString("[green]Global keys[white]\n\n")
	b.WriteString("  [yellow]q[white]     quit from any view (also `:quit` / `:q` from the command bar)\n")
	b.WriteString("  [yellow]:[white]     open the command bar (switch resource, e.g. `:workerpools`, `:wp`, `:workers <poolId>`, `:help`, `:quit`)\n")
	b.WriteString("  [yellow]/[white]     filter the current list's rows, or a detail body's lines " +
		"(including a live-streaming log) — narrows to lines/rows containing the query\n")
	b.WriteString("  [yellow]1-9[white]   sort the current list by that column, numbered left to right " +
		"(list views only) — press the same digit again to reverse direction\n")
	b.WriteString("  [yellow]Tab[white]/[yellow]Shift+Tab[white]  cycle the facet tab bar, for resources that have one " +
		"(see below)\n")
	b.WriteString("  [yellow]r[white]     refresh the current view, bypassing the cache\n")
	b.WriteString("  [yellow]x[white]     on a list, toggle column truncation — use Left/Right to scroll columns " +
		"that no longer fit; on a detail view, toggle word-wrap — use Left/Right/h/l to scroll horizontally " +
		"once it's off\n")
	b.WriteString("  [yellow]n[white]     on a detail view, toggle a vim-like line-number gutter — a filtered line keeps " +
		"its original number, so you can still tell where it sat among the lines the query hid\n")
	b.WriteString("  [yellow]L[white]     load ALL rows of a truncated list — very large lists (big task groups, " +
		"stopped workers, deep task queues) fetch only their first ~1000 rows up front, shown as a [yellow]N+[white] " +
		"row count in the title\n")
	b.WriteString("  [yellow]o[white]     open the current view in Taskcluster's web UI, if that resource has one\n")
	b.WriteString("  [yellow]s[white]     save the current view's content to a local file, if that resource supports it (e.g. an artifact)\n")
	b.WriteString("  [yellow]Esc[white]   go back\n")
	b.WriteString("  [yellow]?[white]     toggle this help screen\n\n")

	b.WriteString("[green]Footer input (command bar, filter, id prompt)[white]\n\n")
	b.WriteString("  [yellow]Up[white]/[yellow]Down[white]  cycle through previously entered values, scoped separately per input " +
		"kind (command bar vs. filter vs. id prompt)\n\n")

	b.WriteString("[green]Resources[white]\n\n")
	for _, name := range registry.Names() {
		res, ok := registry.Resolve(name)
		if !ok {
			continue
		}

		aliases := "none"
		if len(res.Aliases()) > 0 {
			aliases = strings.Join(res.Aliases(), ", ")
		}

		if direct, isDirect := res.(resource.DirectLookup); isDirect {
			b.WriteString(fmt.Sprintf(
				"  [yellow]%s[white] (aliases: %s)\n      %s\n"+
					"      requires an id, e.g. `:%s <id>` — no id opens a prompt asking for a %s\n\n",
				res.Name(), aliases, res.Description(), direct.Name(), direct.IDPromptLabel(),
			))
			continue
		}

		columns := make([]string, len(res.Columns()))
		for i, col := range res.Columns() {
			columns[i] = col.Title
		}

		b.WriteString(fmt.Sprintf(
			"  [yellow]%s[white] (aliases: %s)\n      %s\n      columns: %s\n",
			res.Name(), aliases, res.Description(), strings.Join(columns, ", "),
		))

		if scoped, isScoped := res.(resource.ScopedResource); isScoped {
			b.WriteString(fmt.Sprintf(
				"      requires a scope, e.g. `:%s <id>` — no scope redirects to `%s`\n",
				res.Name(), scoped.EmptyScopeResource(),
			))
		} else {
			b.WriteString(fmt.Sprintf(
				"      can also be opened directly by id, e.g. `:%s <id>`\n", res.Name(),
			))
		}

		switch faceted := res.(type) {
		case resource.ServerFaceted:
			b.WriteString(fmt.Sprintf(
				"      tabs: %s\n", strings.Join(faceted.FacetOptions(), ", "),
			))
		case resource.Faceted:
			b.WriteString("      tabs by provider\n")
		}

		b.WriteString("\n")
	}

	b.WriteString("[green]Context actions[white]\n\n")
	b.WriteString("  Some detail screens expose extra keys (e.g. [yellow]w[white] workers) shown in that screen's own header hint bar when available.\n")

	return b.String()
}

// colorTagRegexp matches buildHelpText's tview [color] region tags — always
// a single lowercase color word, never dynamic content — so it's safe to
// strip wholesale for plain-terminal output.
var colorTagRegexp = regexp.MustCompile(`\[[a-z]+\]`)

// PlainHelpText is the same content shown by the in-app '?' help overlay
// (buildHelpText), with tview's [color] region tags stripped — for printing
// to a plain terminal, e.g. the CLI's --help.
func PlainHelpText(registry *resource.Registry) string {
	return colorTagRegexp.ReplaceAllString(buildHelpText(registry), "")
}
