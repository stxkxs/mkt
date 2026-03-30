package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// RenderPanel wraps content in a bordered panel with an embedded title and shadow.
//
//	╭─── Title ──────────────────╮
//	│ content                    │
//	╰────────────────────────────╯░
//	 ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
func RenderPanel(title, content string, width int) string {
	borderStyle := StyleBorderChar
	titleStyle := StylePanelTitle
	shadowStyle := lipgloss.NewStyle().Foreground(ColorShadow)

	innerWidth := width - 2 // subtract left + right border chars

	// Top border: ╭─ Title ────╮
	titleRendered := titleStyle.Render(title)
	titleVisualWidth := lipgloss.Width(titleRendered)
	topFill := innerWidth - 2 - titleVisualWidth - 1 // "─ " + title + " " + fill + last dash before ╮
	if topFill < 0 {
		topFill = 0
	}
	top := borderStyle.Render("╭─ ") + titleRendered + borderStyle.Render(" "+strings.Repeat("─", topFill)+"╮")

	// Bottom border: ╰────────╯
	bottom := borderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")

	// Content lines with side borders
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	sb.WriteString(top)
	sb.WriteString("\n")
	for _, line := range lines {
		lineWidth := lipgloss.Width(line)
		pad := innerWidth - lineWidth
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(borderStyle.Render("│"))
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(borderStyle.Render("│"))
		sb.WriteString(shadowStyle.Render("░"))
		sb.WriteString("\n")
	}
	sb.WriteString(bottom)
	sb.WriteString(shadowStyle.Render("░"))
	sb.WriteString("\n")
	// Bottom shadow row
	sb.WriteString(" ")
	sb.WriteString(shadowStyle.Render(strings.Repeat("░", width)))

	return sb.String()
}
