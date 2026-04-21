package theme

import "charm.land/lipgloss/v2"

// Theme defines a complete color palette using hex strings.
type Theme struct {
	Name      string
	Bg        string
	Fg        string
	Dim       string
	Accent    string
	Green     string
	Red       string
	Yellow    string
	Cyan      string
	Magenta   string
	Orange    string
	Border    string
	TabActive string
	TabBg     string
	Shadow    string
}

// ThemeNames is the ordered list of available themes.
var ThemeNames = []string{
	"tokyonight",
	"catppuccin-mocha",
	"gruvbox-dark",
	"nord",
	"dracula",
	"solarized-dark",
	"catppuccin-latte",
}

var themes = map[string]Theme{
	"tokyonight": {
		Name:      "tokyonight",
		Bg:        "#1a1b26",
		Fg:        "#c0caf5",
		Dim:       "#565f89",
		Accent:    "#7aa2f7",
		Green:     "#9ece6a",
		Red:       "#f7768e",
		Yellow:    "#e0af68",
		Cyan:      "#7dcfff",
		Magenta:   "#bb9af7",
		Orange:    "#ff9e64",
		Border:    "#3b4261",
		TabActive: "#7aa2f7",
		TabBg:     "#24283b",
		Shadow:    "#13141d",
	},
	"catppuccin-mocha": {
		Name:      "catppuccin-mocha",
		Bg:        "#1e1e2e",
		Fg:        "#cdd6f4",
		Dim:       "#6c7086",
		Accent:    "#89b4fa",
		Green:     "#a6e3a1",
		Red:       "#f38ba8",
		Yellow:    "#f9e2af",
		Cyan:      "#89dceb",
		Magenta:   "#cba6f7",
		Orange:    "#fab387",
		Border:    "#45475a",
		TabActive: "#89b4fa",
		TabBg:     "#181825",
		Shadow:    "#151520",
	},
	"gruvbox-dark": {
		Name:      "gruvbox-dark",
		Bg:        "#282828",
		Fg:        "#ebdbb2",
		Dim:       "#928374",
		Accent:    "#83a598",
		Green:     "#b8bb26",
		Red:       "#fb4934",
		Yellow:    "#fabd2f",
		Cyan:      "#8ec07c",
		Magenta:   "#d3869b",
		Orange:    "#fe8019",
		Border:    "#504945",
		TabActive: "#83a598",
		TabBg:     "#1d2021",
		Shadow:    "#1d1d1d",
	},
	"nord": {
		Name:      "nord",
		Bg:        "#2e3440",
		Fg:        "#eceff4",
		Dim:       "#4c566a",
		Accent:    "#88c0d0",
		Green:     "#a3be8c",
		Red:       "#bf616a",
		Yellow:    "#ebcb8b",
		Cyan:      "#8fbcbb",
		Magenta:   "#b48ead",
		Orange:    "#d08770",
		Border:    "#434c5e",
		TabActive: "#88c0d0",
		TabBg:     "#3b4252",
		Shadow:    "#242830",
	},
	"dracula": {
		Name:      "dracula",
		Bg:        "#282a36",
		Fg:        "#f8f8f2",
		Dim:       "#6272a4",
		Accent:    "#bd93f9",
		Green:     "#50fa7b",
		Red:       "#ff5555",
		Yellow:    "#f1fa8c",
		Cyan:      "#8be9fd",
		Magenta:   "#ff79c6",
		Orange:    "#ffb86c",
		Border:    "#44475a",
		TabActive: "#bd93f9",
		TabBg:     "#21222c",
		Shadow:    "#1e1f29",
	},
	"solarized-dark": {
		Name:      "solarized-dark",
		Bg:        "#002b36",
		Fg:        "#839496",
		Dim:       "#586e75",
		Accent:    "#268bd2",
		Green:     "#859900",
		Red:       "#dc322f",
		Yellow:    "#b58900",
		Cyan:      "#2aa198",
		Magenta:   "#d33682",
		Orange:    "#cb4b16",
		Border:    "#073642",
		TabActive: "#268bd2",
		TabBg:     "#073642",
		Shadow:    "#001f28",
	},
	"catppuccin-latte": {
		Name:      "catppuccin-latte",
		Bg:        "#eff1f5",
		Fg:        "#4c4f69",
		Dim:       "#9ca0b0",
		Accent:    "#1e66f5",
		Green:     "#40a02b",
		Red:       "#d20f39",
		Yellow:    "#df8e1d",
		Cyan:      "#04a5e5",
		Magenta:   "#8839ef",
		Orange:    "#fe640b",
		Border:    "#ccd0da",
		TabActive: "#1e66f5",
		TabBg:     "#e6e9ef",
		Shadow:    "#d5d8df",
	},
}

// CurrentName returns the name of the active theme.
var CurrentName string

var themeIdx int

// Apply activates a theme by name, updating all package-level colors and styles.
func Apply(name string) {
	// Alias "dark" to "tokyonight" for backwards compat
	if name == "dark" {
		name = "tokyonight"
	}
	t, ok := themes[name]
	if !ok {
		t = themes["tokyonight"]
		name = "tokyonight"
	}
	CurrentName = name

	// Update the index
	for i, n := range ThemeNames {
		if n == name {
			themeIdx = i
			break
		}
	}

	// Overwrite package-level color vars
	ColorBg = lipgloss.Color(t.Bg)
	ColorFg = lipgloss.Color(t.Fg)
	ColorDim = lipgloss.Color(t.Dim)
	ColorAccent = lipgloss.Color(t.Accent)
	ColorGreen = lipgloss.Color(t.Green)
	ColorRed = lipgloss.Color(t.Red)
	ColorYellow = lipgloss.Color(t.Yellow)
	ColorCyan = lipgloss.Color(t.Cyan)
	ColorMagenta = lipgloss.Color(t.Magenta)
	ColorOrange = lipgloss.Color(t.Orange)
	ColorBorder = lipgloss.Color(t.Border)
	ColorTabActive = lipgloss.Color(t.TabActive)
	ColorTabBg = lipgloss.Color(t.TabBg)
	ColorShadow = lipgloss.Color(t.Shadow)

	rebuildStyles()
	rebuildHeatmapGradient(t.Red, t.Dim, t.Green)
}

// NextTheme cycles to the next theme and returns its name.
func NextTheme() string {
	themeIdx = (themeIdx + 1) % len(ThemeNames)
	return ThemeNames[themeIdx]
}

// rebuildStyles re-creates all shared styles from current colors.
func rebuildStyles() {
	StyleUp = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleDown = lipgloss.NewStyle().Foreground(ColorRed)
	StyleDim = lipgloss.NewStyle().Foreground(ColorDim)
	StyleNeutral = lipgloss.NewStyle().Foreground(ColorDim)

	StyleHeader = lipgloss.NewStyle().Foreground(ColorDim).Bold(true)
	StyleCursor = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleSymbol = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StyleVal = lipgloss.NewStyle().Foreground(ColorFg)

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

	StyleCursorGutter = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleCursorRow = lipgloss.NewStyle().Background(ColorTabBg)

	StyleBorderChar = lipgloss.NewStyle().Foreground(ColorBorder)
	StylePanelTitle = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
		Background(ColorTabBg).
		Foreground(ColorDim).
		PaddingLeft(1).
		PaddingRight(1)
}

func init() {
	Apply("tokyonight")
}
