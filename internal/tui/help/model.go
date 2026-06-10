// Package help renders a centered keybinding-reference overlay. The card
// shows the active tab's keys first, then the global keys, so `?` always
// answers "what can I press right here".
package help

import (
	"strings"

	tea "charm.land/bubbletea/v2"
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
		{"j/k, arrows", "navigate rules"},
		{"t", "toggle rule on/off"},
		{"d", "delete rule (asks to confirm)"},
	},
	"Chart": {
		{"c (on Watch tab)", "open chart for selected symbol"},
		{"[ / ]", "change interval (1m → 1w)"},
		{"+ / -", "zoom in / out"},
		{"m", "candlestick / line"},
		{"i", "indicator menu"},
		{"esc", "close chart"},
	},
	"Macro": {
		{"", "read-only: rates, FX, futures, DeFi, calendar"},
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
		{"", "read-only: correlation of watchlist symbols"},
	},
}

// Model is the help overlay.
type Model struct {
	active bool
	tab    string
	width  int
	height int
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

	var sb strings.Builder
	writeSection(&sb, m.tab, tabBindings[m.tab])
	sb.WriteString("\n")
	writeSection(&sb, "Global", globalBindings)
	sb.WriteString("\n ")
	sb.WriteString(theme.StyleDim.Render("press any key to close"))

	return theme.RenderPanel("Help — "+m.tab, sb.String(), panelWidth)
}

const panelWidth = 58
const keyColWidth = 18

func writeSection(sb *strings.Builder, title string, bindings []binding) {
	sb.WriteString(" ")
	sb.WriteString(theme.StylePanelTitle.Render(title))
	sb.WriteString("\n")
	for _, b := range bindings {
		sb.WriteString("  ")
		key := b.key
		for len(key) < keyColWidth {
			key += " "
		}
		sb.WriteString(theme.StyleSymbol.Render(key))
		sb.WriteString(theme.StyleVal.Render(b.desc))
		sb.WriteString("\n")
	}
}
