package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/tui/format"
)

// RenderPanel wraps content in a bordered panel with an embedded title and shadow.
//
//	╭─── Title ──────────────────╮
//	│ content                    │
//	╰────────────────────────────╯░
//	 ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
//
// A width narrower than the two border columns renders nothing rather
// than reaching for a negative-length fill.
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
