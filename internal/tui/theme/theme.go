package theme

import "charm.land/lipgloss/v2"

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

	StyleTabActive = lipgloss.NewStyle().
			Foreground(ColorBg).
			Background(ColorTabActive).
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColorDim).
				Background(ColorTabBg).
				PaddingLeft(1).
				PaddingRight(1)

	StyleTabBar = lipgloss.NewStyle().
			Background(ColorTabBg).
			PaddingLeft(1).
			PaddingRight(1)

	StyleStatusBar = lipgloss.NewStyle().
			Background(ColorTabBg).
			Foreground(ColorDim).
			PaddingLeft(1).
			PaddingRight(1)
)
