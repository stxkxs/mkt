package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/tui/format"
)

// PanelShadow is what RenderPanel draws beyond the width it is given: the
// shadow column down the right edge. The two border columns are inside
// width, not beyond it.
const PanelShadow = 1

// PanelWidth fits an overlay's preferred width to the frame it is drawn
// in, returning 0 when the frame cannot hold it at minimum — the caller's
// signal to render nothing rather than overflow.
//
// A frame of 0 means the overlay has not been sized yet, and the preferred
// width is returned unchanged.
func PanelWidth(preferred, frame, minimum int) int {
	if frame <= 0 {
		return preferred
	}
	w := min(preferred, frame-PanelShadow)
	if w < minimum {
		return 0
	}
	return w
}

// RenderPanel wraps content in a bordered panel with an embedded title and shadow.
//
//	╭─── Title ──────────────────╮
//	│ content                    │
//	╰────────────────────────────╯░
//	 ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
//
// A width narrower than the two border columns renders nothing rather
// than reaching for a negative-length fill.
//
// The rendered block is PanelShadow cells wider than width, and content
// lines are cut to fit rather than allowed to widen it: an overlay is
// composited at a fixed origin, so a panel wider than its frame overflows
// the right edge instead of being clipped.
func RenderPanel(title, content string, width int) string {
	if width < 3 {
		return ""
	}

	borderStyle := StyleBorderChar
	titleStyle := StylePanelTitle
	shadowStyle := lipgloss.NewStyle().Foreground(ColorShadow)

	innerWidth := width - 2 // subtract left + right border chars

	// Top border: ╭─ Title ────╮
	titleRendered := titleStyle.Render(format.Truncate(title, innerWidth-3))
	titleVisualWidth := lipgloss.Width(titleRendered)
	topFill := innerWidth - 2 - titleVisualWidth - 1 // "─ " + title + " " + fill + last dash before ╮
	top := borderStyle.Render("╭─ ") + titleRendered + borderStyle.Render(" "+format.Repeat("─", topFill)+"╮")

	// Bottom border: ╰────────╯
	bottom := borderStyle.Render("╰" + format.Repeat("─", innerWidth) + "╯")

	// Content lines with side borders
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	sb.WriteString(top)
	sb.WriteString("\n")
	for _, line := range lines {
		line = format.Truncate(line, innerWidth)
		sb.WriteString(borderStyle.Render("│"))
		sb.WriteString(line)
		sb.WriteString(format.Spaces(innerWidth - lipgloss.Width(line)))
		sb.WriteString(borderStyle.Render("│"))
		sb.WriteString(shadowStyle.Render("░"))
		sb.WriteString("\n")
	}
	sb.WriteString(bottom)
	sb.WriteString(shadowStyle.Render("░"))
	sb.WriteString("\n")
	// Bottom shadow row
	sb.WriteString(" ")
	sb.WriteString(shadowStyle.Render(format.Repeat("░", width)))

	return sb.String()
}
