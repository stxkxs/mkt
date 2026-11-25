package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// sgrResets are the full resets lipgloss closes every styled span with.
var sgrResets = []string{"\x1b[0m", "\x1b[m"}

// HighlightRow paints the selection band behind a finished row and pads it
// to width, so the band spans the frame rather than stopping at the last
// cell.
//
// Wrapping the row in StyleCursorRow is not enough: every cell closes its
// own style with a full SGR reset, which clears the band's background too,
// leaving it on the first glyph only. The band is re-opened after each
// reset instead.
func HighlightRow(row string, width int) string {
	open := sgrOpen(StyleCursorRow)
	if open == "" {
		return row
	}
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	pairs := make([]string, 0, 2*len(sgrResets))
	for _, reset := range sgrResets {
		pairs = append(pairs, reset, "\x1b[m"+open)
	}
	return open + strings.NewReplacer(pairs...).Replace(row) + "\x1b[m"
}

// sgrOpen returns the escape sequence a style opens a span with, or "" for
// a style that renders text unchanged.
func sgrOpen(s lipgloss.Style) string {
	const probe = "\x00"
	r := s.Render(probe)
	i := strings.Index(r, probe)
	if i <= 0 {
		return ""
	}
	return r[:i]
}
