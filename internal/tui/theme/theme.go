package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Shared colors used across all TUI sub-packages.
var (
	ColorBg        = lipgloss.Color("#1a1b26")
	ColorFg        = lipgloss.Color("#c0caf5")
	ColorDim       = lipgloss.Color("#565f89")
	ColorAccent    = lipgloss.Color("#7aa2f7")
	ColorGreen     = lipgloss.Color("#9ece6a")
	ColorRed       = lipgloss.Color("#f7768e")
	ColorYellow    = lipgloss.Color("#e0af68")
	ColorCyan      = lipgloss.Color("#7dcfff")
	ColorMagenta   = lipgloss.Color("#bb9af7")
	ColorOrange    = lipgloss.Color("#ff9e64")
	ColorBorder    = lipgloss.Color("#3b4261")
	ColorTabActive = lipgloss.Color("#7aa2f7")
	ColorTabBg     = lipgloss.Color("#24283b")
	ColorShadow    = lipgloss.Color("#13141d")
)

// Shared styles used across multiple TUI sub-packages.
var (
	StyleUp      = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleDown    = lipgloss.NewStyle().Foreground(ColorRed)
	StyleDim     = lipgloss.NewStyle().Foreground(ColorDim)
	StyleNeutral = lipgloss.NewStyle().Foreground(ColorDim)

	StyleHeader = lipgloss.NewStyle().Foreground(ColorDim).Bold(true)
	StyleCursor = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleSymbol = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleVal    = lipgloss.NewStyle().Foreground(ColorFg)

	// Tab bar
	StyleTabActive = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Background(ColorTabBg).
			Bold(true).
			Underline(true)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColorDim).
				Background(ColorTabBg)

	StyleTabBar = lipgloss.NewStyle().
			Background(ColorTabBg)

	StyleTabSeparator = lipgloss.NewStyle().
				Foreground(ColorDim).
				Background(ColorTabBg)

	StyleBranding = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Background(ColorTabBg).
			Bold(true)

	// Cursor / selection
	StyleCursorGutter = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleCursorRow    = lipgloss.NewStyle().Background(ColorTabBg)

	// Panels & borders
	StyleBorderChar = lipgloss.NewStyle().Foreground(ColorBorder)
	StylePanelTitle = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
			Background(ColorTabBg).
			Foreground(ColorDim).
			PaddingLeft(1).
			PaddingRight(1)
)

// ChangedMsg is broadcast when the active theme changes so each view can rebuild its cached styles.
type ChangedMsg struct {
	Name string
}

// StyleAccentText renders text in accent color.
func StyleAccentText(s string) string {
	return lipgloss.NewStyle().Foreground(ColorAccent).Render(s)
}

// SectionHeader renders a styled section divider: "  ── Title ─────────"
func SectionHeader(title string, width int) string {
	prefix := StyleBorderChar.Render("  ── ")
	titleStr := StylePanelTitle.Render(title)
	suffix := " "
	used := 5 + lipgloss.Width(titleStr) + 1 // "  ── " + title + " "
	remaining := width - used
	if remaining < 0 {
		remaining = 0
	}
	line := StyleBorderChar.Render(strings.Repeat("─", remaining))
	return prefix + titleStr + suffix + line
}
