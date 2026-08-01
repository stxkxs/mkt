package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/tui/format"
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

// ChangedMsg is broadcast when the active theme changes.
//
// There is exactly one restyle mechanism: a view that caches lipgloss
// styles in package-level vars handles ChangedMsg in its Update and
// calls its own RebuildStyles. Views that read the theme's exported
// colors at render time need neither, and deliberately do not define an
// empty RebuildStyles stub.
type ChangedMsg struct {
	Name string
}

// StyleAccentText renders text in accent color.
func StyleAccentText(s string) string {
	return lipgloss.NewStyle().Foreground(ColorAccent).Render(s)
}

// Section header geometry: "  ── " lead-in, one space after the title,
// two spaces between the rule and a trailing hint.
const (
	headerLead    = 5
	headerGap     = 1
	headerHintGap = 2
	headerMinRule = 2 // keep the divider visible or drop the hint entirely
)

// SectionHeader renders a styled section divider: "  ── Title ─────────"
func SectionHeader(title string, width int) string {
	return SectionHeaderHint(title, "", width)
}

// SectionHeaderHint renders a section divider that carries a trailing
// hint on the same row: "  ── Title ─────────  j/k:nav".
//
// The rule absorbs whatever width is left after the title and the hint,
// so the header occupies exactly one row of `width` cells. Callers that
// appended their own hint after SectionHeader used to overrun the frame
// — the rule had already padded to full width — and orphan the hint onto
// the next line; pass it here instead. Too narrow for both drops the
// hint, then truncates the title.
func SectionHeaderHint(title, hint string, width int) string {
	if width <= headerLead+headerGap {
		return StyleBorderChar.Render(format.Repeat("─", width))
	}

	titleStr := StylePanelTitle.Render(format.Truncate(title, width-headerLead-headerGap))
	prefix := StyleBorderChar.Render("  ── ") + titleStr + " "
	rule := width - headerLead - headerGap - lipgloss.Width(titleStr)

	if hint != "" {
		if r := rule - headerHintGap - lipgloss.Width(hint); r >= headerMinRule {
			return prefix + StyleBorderChar.Render(format.Repeat("─", r)) +
				StyleDim.Render(format.Spaces(headerHintGap)+hint)
		}
	}
	return prefix + StyleBorderChar.Render(format.Repeat("─", rule))
}
