// Package help renders a centered keybinding-reference overlay. The card
// shows the active tab's keys first, then the global keys, so `?` always
// answers "what can I press right here".
package help

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	{"→ / ←", "next / previous tab, except on Heatmap and Correl"},
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
	"Compare": {
		{"[ / ]", "change interval (1m → 1w)"},
		{"+ / -", "zoom in / out"},
		{"f", "fit candles to the window width"},
		{"x", "remove the last symbol added"},
		{"esc", "close comparison"},
	},
	"Detail": {
		{"esc", "close the detail panel"},
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
		{"j/k/h/l, arrows", "navigate sectors"},
		{"enter", "drill into sector"},
		{"esc", "back to overview"},
	},
	"Options": {
		{"j/k, arrows", "scroll chain"},
		{"O (on Watch tab)", "load chain for selected symbol"},
	},
	"Correl": {
		{"h/l, arrows, [ / ]", "scroll the visible symbol window"},
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
	offset int  // first body row shown when the card is taller than the frame
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
	m.offset = 0
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

// Update scrolls a card taller than the frame with j/k, the arrows, the
// page keys and g/G, and closes the overlay on any other key. A card that
// fits closes on every key, as its footer says.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	body, rows := m.layout()
	maxOffset := len(body) - rows
	if rows <= 0 || maxOffset <= 0 {
		m.active = false
		return m, nil
	}
	switch key.String() {
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "pgdown", "space", "ctrl+d":
		m.offset += rows
	case "pgup", "ctrl+u":
		m.offset -= rows
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = maxOffset
	default:
		m.active = false
	}
	m.offset = min(max(m.offset, 0), maxOffset)
	return m, nil
}

// View renders the help card. Nothing on it is cut with an ellipsis: the
// card is the one place every binding is spelled out, so a description
// wraps onto as many rows as it needs, and a card taller than the frame
// scrolls.
func (m Model) View() string {
	if !m.active {
		return ""
	}
	w := m.panelWidth()
	if w == 0 {
		// Too narrow to render without overflowing the frame.
		return ""
	}
	body, rows := m.layout()
	if rows < 0 {
		return ""
	}
	inner := w - 2

	var sb strings.Builder
	if rows > 0 && len(body) > rows {
		offset := min(max(m.offset, 0), len(body)-rows)
		for _, l := range body[offset : offset+rows] {
			sb.WriteString(l + "\n")
		}
		sb.WriteString("\n")
		sb.WriteString(footer(scrollFooter(offset+1, offset+rows, len(body)), inner))
	} else {
		for _, l := range body {
			sb.WriteString(l + "\n")
		}
		sb.WriteString("\n")
		sb.WriteString(footer(closeFooter, inner))
	}
	return theme.RenderPanel(m.title(inner), sb.String(), w)
}

// title names the card for the border. The tab's name is dropped where the
// border cannot hold it, since the first section repeats it.
func (m Model) title(inner int) string {
	if t := "Help — " + m.tab; m.tab != "" && lipgloss.Width(t) <= inner-3 {
		return t
	}
	return "Help"
}

// closeFooter is the footer of a card that fits the frame.
const closeFooter = "press any key to close"

// scrollFooter is the footer of a card taller than the frame: the keys
// that scroll it, and which rows are showing.
func scrollFooter(first, last, total int) string {
	return fmt.Sprintf("j/k: scroll  other keys: close  %d-%d/%d", first, last, total)
}

// footer renders the card's last row inside inner cells, wrapped rather
// than cut when the card is narrow.
func footer(text string, inner int) string {
	lines := format.Wrap(text, inner-1)
	for i, l := range lines {
		lines[i] = " " + theme.StyleDim.Render(l)
	}
	return strings.Join(lines, "\n")
}

// Card geometry. The card is as wide as its widest unwrapped row, up to
// maxPanelWidth, and never wider than the frame.
const (
	maxPanelWidth = 80
	keyGap        = 2 // between the key column and the description
	indent        = 2 // before each key
	// minDescWidth is the narrowest description column worth keeping beside
	// the keys; below it each key takes its own row and its description
	// wraps on the rows under it.
	minDescWidth = 14
	// panelChrome is the rows RenderPanel adds around the content: the top
	// and bottom borders and the shadow row.
	panelChrome = 3
)

// minPanelWidth is the narrowest card worth drawing; below this the frame
// cannot hold the overlay and View renders nothing.
const minPanelWidth = 16

// sections returns the card's two blocks: the active tab's keys, then the
// global keys.
func (m Model) sections() []section {
	return []section{
		{m.tab, tabBindings[m.tab]},
		{"Global", globalBindingsFor(m.shared)},
	}
}

type section struct {
	title    string
	bindings []binding
}

// keyColumn is the width of the widest key on the card.
func keyColumn(secs []section) int {
	w := 0
	for _, s := range secs {
		for _, b := range s.bindings {
			w = max(w, lipgloss.Width(b.key))
		}
	}
	return w
}

// panelWidth returns the card width for the current frame, or 0 when the
// frame is too narrow to hold the card at all. It is the width that holds
// every row unwrapped, narrowed to the frame.
func (m Model) panelWidth() int {
	secs := m.sections()
	keyCol := keyColumn(secs)
	want := lipgloss.Width("Help — "+m.tab) + 6
	for _, s := range secs {
		want = max(want, lipgloss.Width(s.title)+3)
		for _, b := range s.bindings {
			// border + indent + key column + gap + description + pad + border
			want = max(want, 1+indent+keyCol+keyGap+lipgloss.Width(b.desc)+1+1)
		}
	}
	return theme.PanelWidth(min(want, maxPanelWidth), m.width, minPanelWidth)
}

// layout renders the card's body rows at the current width, and returns
// how many of them fit the frame beside the footer. A rows of 0 means the
// frame is unsized, and the whole body is shown; a rows below len(body)
// means the card scrolls; a rows below 0 means the frame is too short to
// hold one row beside the footer, and the card is not drawn.
func (m Model) layout() ([]string, int) {
	w := m.panelWidth()
	if w == 0 {
		return nil, 0
	}
	inner := w - 2
	secs := m.sections()
	var body []string
	for i, s := range secs {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, sectionRows(s, keyColumn(secs), inner)...)
	}
	if m.height <= 0 {
		return body, 0
	}
	// The body fits when it and the closing footer fit together. Otherwise
	// it scrolls, and the rows left for it are what remains beside the
	// scrolling footer, counted at its widest so a wrapped footer never
	// pushes the card past the frame.
	avail := m.height - panelChrome - 1 // the blank row above the footer
	if len(body)+len(format.Wrap(closeFooter, inner-1)) <= avail {
		return body, len(body)
	}
	n := len(body)
	rows := avail - len(format.Wrap(scrollFooter(n, n, n), inner-1))
	if rows < 1 {
		return nil, -1
	}
	return body, rows
}

// sectionRows renders one titled block of bindings inside inner cells. A
// description wraps under its own column; when the column would be too
// narrow to read, each key takes a row and its description wraps below it.
func sectionRows(s section, keyCol, inner int) []string {
	var rows []string
	for _, l := range format.Wrap(s.title, inner-1) {
		rows = append(rows, " "+theme.StylePanelTitle.Render(l))
	}
	descCol := inner - indent - keyCol - keyGap - 1
	pad := format.Spaces(indent)
	if descCol >= minDescWidth {
		hang := format.Spaces(indent + keyCol + keyGap)
		for _, b := range s.bindings {
			key := b.key + format.Spaces(keyCol-lipgloss.Width(b.key)+keyGap)
			for j, l := range format.Wrap(b.desc, descCol) {
				if j == 0 {
					rows = append(rows, pad+theme.StyleSymbol.Render(key)+theme.StyleVal.Render(l))
				} else {
					rows = append(rows, hang+theme.StyleVal.Render(l))
				}
			}
		}
		return rows
	}
	stack := format.Spaces(2 * indent)
	for _, b := range s.bindings {
		for _, l := range format.Wrap(b.key, inner-indent-1) {
			rows = append(rows, pad+theme.StyleSymbol.Render(l))
		}
		for _, l := range format.Wrap(b.desc, inner-2*indent-1) {
			rows = append(rows, stack+theme.StyleVal.Render(l))
		}
	}
	return rows
}
