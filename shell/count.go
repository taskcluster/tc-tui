package shell

import "fmt"

// formatRowCount renders the count segment for a list-view title (without the
// leading separator). visible is the number of rows shown after filter+facet;
// total is the number of rows fetched; truncated reports that more rows exist
// server-side (a capped fetch — see resource.PartialLister and the 'L' key).
//
// It distinguishes the four cases the header count is meant to convey:
//   - exact:                visible == total, not truncated → "42"
//   - truncated:            visible == total, truncated     → "1000+"
//   - filtered:             visible <  total, not truncated → "7 of 213"
//   - filtered + truncated: visible <  total, truncated     → "7 of 213+"
//
// The non-bracketed form is deliberate: augment progress already renders as a
// bracketed fraction (e.g. "[2/5]"), so a bracketed "[7/213]" here would be
// indistinguishable from it. Callers join this with a leading " · ".
func formatRowCount(visible, total int, truncated bool) string {
	plus := ""
	if truncated {
		plus = "+"
	}
	if visible == total {
		return fmt.Sprintf("%d%s", total, plus)
	}
	return fmt.Sprintf("%d of %d%s", visible, total, plus)
}
