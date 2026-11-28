package theme

import (
	"strings"
	"time"

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
// A view that caches lipgloss styles in package-level vars handles
// ChangedMsg in its Update and calls its own RebuildStyles. Views that read
// the theme's exported colors at render time need neither, and define no
// empty RebuildStyles stub.
//
// statusbar is the one exception: it exposes no Update, so the root model
// calls statusbar.RebuildStyles directly. A view with both an Update case
// and a direct call from the root rebuilds its styles twice per change.
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
// so the header occupies exactly one row of `width` cells. A caller that
// appends its own hint after SectionHeader overruns the frame — the rule
// has already padded to full width — and orphans the hint onto
// the next line; pass it here instead. Too narrow for both drops the
// hint, then truncates the title.
func SectionHeaderHint(title, hint string, width int) string {
	return SectionHeaderTitles([]string{title}, hint, width)
}

// SectionHeaderTitles is SectionHeaderHint with shorter wordings of the
// title, longest first. A narrow frame takes the longest title that fits
// beside the hint, then the longest that fits alone, and truncates the
// shortest only when none does: a hint can carry state the frame shows
// nowhere else, and a shorter title reads better than a cut one.
func SectionHeaderTitles(titles []string, hint string, width int) string {
	if width <= headerLead+headerGap || len(titles) == 0 {
		return StyleBorderChar.Render(format.Repeat("─", width))
	}
	room := width - headerLead - headerGap
	if hint != "" {
		for _, t := range titles {
			if r := room - lipgloss.Width(t) - headerHintGap - lipgloss.Width(hint); r >= headerMinRule {
				return sectionPrefix(t) + StyleBorderChar.Render(format.Repeat("─", r)) +
					StyleDim.Render(format.Spaces(headerHintGap)+hint)
			}
		}
	}
	title := format.Truncate(titles[len(titles)-1], room)
	for _, t := range titles {
		if lipgloss.Width(t) <= room {
			title = t
			break
		}
	}
	return sectionPrefix(title) + StyleBorderChar.Render(format.Repeat("─", room-lipgloss.Width(title)))
}

// sectionPrefix is a section header's lead-in and title.
func sectionPrefix(title string) string {
	return StyleBorderChar.Render("  ── ") + StylePanelTitle.Render(title) + " "
}

// Note renders dim instructional text inside width cells, wrapped rather
// than cut: an instruction on an empty or idle view is the only place the
// reader learns what to press. The text's own leading spaces are kept on
// its first row, and its continuation rows hang two cells further in.
func Note(text string, width int) string {
	return strings.Join(NoteLines(text, width), "\n")
}

// NoteLines is Note's rows, for a view that counts them against a height
// budget. Every row is wrapped at the continuation width, so the first row
// may end two cells short of the frame; it never ends past it.
func NoteLines(text string, width int) []string {
	return WrapStyled(text, width, StyleDim)
}

// WrapStyled is NoteLines in any style: an error that stands in for a
// view's content is wrapped the same way, in the error color.
func WrapStyled(text string, width int, style lipgloss.Style) []string {
	body := strings.TrimLeft(text, " ")
	lead := len(text) - len(body)
	hang := lead + 2
	if width-hang < 8 {
		lead, hang = 0, 0
	}
	var rows []string
	for i, l := range format.Wrap(body, width-hang) {
		pad := hang
		if i == 0 {
			pad = lead
		}
		rows = append(rows, format.Spaces(pad)+style.Render(l))
	}
	return rows
}

// SpinnerClock is the time the loading spinner reads its frame from. A
// frame drawn from the wall clock keeps one pace however many sessions
// share the process; tests pin it.
var SpinnerClock = time.Now

// spinnerFrameEvery is how long each spinner frame shows.
const spinnerFrameEvery = 100 * time.Millisecond

// Loading renders a view's loading line: the spinner and what is loading,
// wrapped like a Note. Every view that waits on a fetch shows this line,
// so the wait reads the same everywhere and visibly moves.
func Loading(what string, width int) string {
	frame := format.SpinnerFrame(int(SpinnerClock().UnixMilli() / spinnerFrameEvery.Milliseconds()))
	// The text is laid out four cells in, where "  ⠋ " ends, and the
	// spinner is drawn into those cells; a frame too narrow for that
	// indent gets the text alone.
	rows := NoteLines("    "+what+"…", width)
	if len(rows) > 0 && strings.HasPrefix(rows[0], "    ") {
		rows[0] = "  " + StyleAccentText(frame) + " " + rows[0][4:]
	}
	return strings.Join(rows, "\n")
}
