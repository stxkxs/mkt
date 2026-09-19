// Package help renders a centered keybinding-reference overlay. The card
// shows the active tab's keys first, then the global keys, so `?` always
// answers "what can I press right here".
package help

import (
	"charm.land/lipgloss/v2"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// binding is one key → action row on the card.
type binding struct {
	key  string
	desc string
}

var globalBindings = []binding{
	{"1-9", "jump to tab"},
	{"tab / shift+tab", "next / previous tab"},
	{":", "command palette (tab name, theme <name>, q)"},
	{"T", "cycle color theme"},
	{"?", "toggle this help"},
	{"q / ctrl+c", "quit"},
}

// themeBindingKeys are the bindings that mutate process-wide theme state.
// A shared process withholds them from guests, so the reference must not
// advertise them there.
var themeBindingKeys = map[string]bool{"T": true}

// globalBindingsFor returns the global reference for one session. A shared
// process drops the theme controls, since listing a key that does nothing
// is worse than listing one fewer key.
func globalBindingsFor(shared bool) []binding {
	if !shared {
		return globalBindings
	}
	out := make([]binding, 0, len(globalBindings))
	for _, b := range globalBindings {
		if themeBindingKeys[b.key] {
			continue
		}
		if b.key == ":" {
			b.desc = "command palette (tab name, q)"
		}
		out = append(out, b)
	}
	return out
}

// tabBindings maps a tab name (as shown in the tab bar) to its keys.
var tabBindings = map[string][]binding{
	"Watch": {
		{"j/k, arrows", "navigate"},
		{"g / G", "top / bottom"},
		{"/", "fuzzy search symbols"},
		{"s", "cycle sort (config / change / volume / price)"},
		{"[ / ]", "switch watchlist group"},
		{"enter", "detail panel"},
		{"c", "full-screen chart"},
		{"a", "add to comparison set"},
		{"C", "open comparison chart"},
		{"A", "create alert"},
		{"i", "symbol info"},
		{"O", "options chain"},
	},
	"Portfolio": {
		{"j/k, arrows", "navigate holdings"},
		{"[ / ]", "switch portfolio"},
	},
	"Alerts": {
		{"j/k, arrows", "navigate rules (list scrolls)"},
		{"t", "toggle rule on/off"},
		{"d", "delete rule (asks to confirm)"},
	},
	"Chart": {
		{"c (on Watch tab)", "open chart for selected symbol"},
		{"[ / ]", "change interval (1m → 1w)"},
		{"+ / -", "zoom in / out"},
		{"f", "fit candles to the window width"},
		{"m", "candlestick / line"},
		{"i", "indicator menu"},
		{"esc", "close chart"},
	},
	"Macro": {
		{"j/k, arrows", "scroll"},
		{"pgup / pgdn", "scroll by a page"},
		{"g / G", "top / bottom"},
	},
	"News": {
		{"j/k, arrows", "navigate"},
		{"g / G", "top / bottom"},
		{"f", "filter: all / news / filings"},
		{"enter", "open link in browser"},
	},
	"Heatmap": {
		{"j/k/h/l", "navigate sectors"},
		{"enter", "drill into sector"},
		{"esc", "back to overview"},
	},
	"Options": {
		{"j/k, arrows", "scroll chain"},
		{"O (on Watch tab)", "load chain for selected symbol"},
	},
	"Correl": {
		{"h/l or [ / ]", "scroll the visible symbol window"},
		{"g / G", "first / last symbols"},
		{"b", "cycle resampling bucket (30s → 15m)"},
	},
}

// Model is the help overlay.
type Model struct {
	active bool
	tab    string
	width  int
	height int
	shared bool // several sessions in one process; theme controls withheld
}

// New creates an inactive help model.
func New() Model {
	return Model{}
}

// Active reports whether the overlay is showing.
func (m Model) Active() bool {
	return m.active
}

// Open shows the card for the named tab.
func (m *Model) Open(tab string) {
	m.active = true
	m.tab = tab
}

// SetSize updates dimensions for centering and clamping.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetShared drops the bindings a shared process withholds, so the reference
// never names a key that does nothing.
func (m *Model) SetShared(shared bool) {
	m.shared = shared
}

// Update closes the overlay on any key press.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.active = false
	}
	return m, nil
}

// View renders the help card.
func (m Model) View() string {
	if !m.active {
		return ""
	}

	w := m.panelWidth()
	if w == 0 {
		// Too narrow to render without overflowing the frame.
		return ""
	}

	var sb strings.Builder
	writeSection(&sb, m.tab, tabBindings[m.tab], w)
	sb.WriteString("\n")
	writeSection(&sb, "Global", globalBindingsFor(m.shared), w)
	sb.WriteString("\n ")
	sb.WriteString(theme.StyleDim.Render(format.Truncate("press any key to close", w-1)))

	return theme.RenderPanel("Help — "+m.tab, sb.String(), w)
}

// panelWidth is the card's preferred width; panelWidth() narrows it to fit
// the frame. SetSize feeds m.width, and a card wider than the terminal is
// composited at x=0 and overflows the right edge.
const panelWidth = 58
const keyColWidth = 18

// panelChrome is what RenderPanel adds around the width it is given: a
// border column on each side plus the shadow column.
const panelChrome = 3

// minPanelWidth is the narrowest card worth drawing; below this the frame
// cannot hold the overlay and View returns nothing.
const minPanelWidth = 16

// panelWidth returns the card width for the current frame, or 0 when the
// frame is too narrow to hold the card at all.
func (m Model) panelWidth() int {
	if m.width <= 0 {
		return panelWidth
	}
	w := min(panelWidth, m.width-panelChrome)
	if w < minPanelWidth {
		return 0
	}
	return w
}

// writeSection renders one titled block of bindings inside width cells.
// RenderPanel sizes the card to its longest content line rather than
// truncating, so a row that overruns width widens the whole card past the
// frame — the key column and the description are both budgeted here.
func writeSection(sb *strings.Builder, title string, bindings []binding, width int) {
	sb.WriteString(" ")
	sb.WriteString(theme.StylePanelTitle.Render(format.Truncate(title, width-1)))
	sb.WriteString("\n")

	keyCol := min(keyColWidth, max(width-6, 1))
	for _, b := range bindings {
		sb.WriteString("  ")
		key := format.Truncate(b.key, keyCol)
		key += format.Spaces(keyCol - lipgloss.Width(key))
		sb.WriteString(theme.StyleSymbol.Render(key))
		// 2 leading spaces + the key column is already spent.
		sb.WriteString(theme.StyleVal.Render(format.Truncate(b.desc, width-2-keyCol)))
		sb.WriteString("\n")
	}
}
