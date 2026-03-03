package tui

import (
	"charm.land/lipgloss/v2"
)

var (
	// Base colors
	colorBg        = lipgloss.Color("#1a1b26")
	colorFg        = lipgloss.Color("#c0caf5")
	colorDim       = lipgloss.Color("#565f89")
	colorAccent    = lipgloss.Color("#7aa2f7")
	colorGreen     = lipgloss.Color("#9ece6a")
	colorRed       = lipgloss.Color("#f7768e")
	colorYellow    = lipgloss.Color("#e0af68")
	colorCyan      = lipgloss.Color("#7dcfff")
	colorBorder    = lipgloss.Color("#3b4261")
	colorTabActive = lipgloss.Color("#7aa2f7")
	colorTabBg     = lipgloss.Color("#24283b")

	// Styles
	styleTabActive = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorTabActive).
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	styleTabInactive = lipgloss.NewStyle().
				Foreground(colorDim).
				Background(colorTabBg).
				PaddingLeft(1).
				PaddingRight(1)

	styleTabBar = lipgloss.NewStyle().
			Background(colorTabBg).
			PaddingLeft(1).
			PaddingRight(1)

	styleStatusBar = lipgloss.NewStyle().
			Background(colorTabBg).
			Foreground(colorDim).
			PaddingLeft(1).
			PaddingRight(1)

	styleGreen = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleRed = lipgloss.NewStyle().
			Foreground(colorRed)

	styleDim = lipgloss.NewStyle().
			Foreground(colorDim)
)
