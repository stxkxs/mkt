package alerts

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleOn    = lipgloss.NewStyle().Foreground(theme.ColorGreen)
	styleOff   = lipgloss.NewStyle().Foreground(theme.ColorRed)
	styleAlert = lipgloss.NewStyle().Foreground(theme.ColorYellow).Bold(true)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleOn = lipgloss.NewStyle().Foreground(theme.ColorGreen)
	styleOff = lipgloss.NewStyle().Foreground(theme.ColorRed)
	styleAlert = lipgloss.NewStyle().Foreground(theme.ColorYellow).Bold(true)
}

// Model is the alerts management view.
type Model struct {
	engine  *alert.Engine
	cursor  int
	width   int
	height  int
	history []alert.TriggeredAlert

	// confirmDelete is the rule index awaiting y/n confirmation, or -1.
	confirmDelete int
}

// New creates an alerts model.
func New(engine *alert.Engine) Model {
	return Model{
		engine:        engine,
		confirmDelete: -1,
	}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// AddTriggered records a triggered alert for the history view.
func (m *Model) AddTriggered(a alert.TriggeredAlert) {
	m.history = append(m.history, a)
	// Keep last 50
	if len(m.history) > 50 {
		m.history = m.history[len(m.history)-50:]
	}
}

// TriggeredCount returns the number of triggered alerts in history.
func (m Model) TriggeredCount() int {
	return len(m.history)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
	case tea.KeyPressMsg:
		rules := m.engine.Rules()
		// Pending delete: y confirms, anything else cancels.
		if m.confirmDelete >= 0 {
			if msg.String() == "y" && m.confirmDelete < len(rules) {
				m.engine.RemoveRule(m.confirmDelete)
				if m.cursor >= len(rules)-1 && m.cursor > 0 {
					m.cursor--
				}
			}
			m.confirmDelete = -1
			return m, nil
		}
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(rules)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "t":
			m.engine.ToggleRule(m.cursor)
		case "d", "delete":
			if len(rules) > 0 && m.cursor < len(rules) {
				m.confirmDelete = m.cursor
			}
		}
	case tea.MouseWheelMsg:
		rules := m.engine.Rules()
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(rules)-1 {
				m.cursor++
			}
		}
	case tea.MouseClickMsg:
		rules := m.engine.Rules()
		// The hit-test maps a mouse row straight to a data-row index, so
		// it answers only for a frame that paints rows at all, and only
		// for the rows it paints. The hint line, the blank line under the
		// table and the whole recent-alerts block sit below the last rule
		// on screen; without the upper bound a click on any of them lands
		// on a rule the viewport is not showing.
		if len(rules) == 0 || m.fit() == nil {
			return m, nil
		}
		ruleRows, _ := m.layout(len(rules))
		row := msg.Y - rulesHeaderLines
		if row < 0 || row >= ruleRows {
			return m, nil
		}
		idx := format.ViewportStart(m.cursor, len(rules), ruleRows) + row
		if idx >= 0 && idx < len(rules) {
			m.cursor = idx
		}
	}
	return m, nil
}

// Fixed rows the rules block spends on chrome: section header, column
// header and separator above; a blank line and the hint/confirm line
// below.
const (
	rulesHeaderLines = 3
	rulesFooterLines = 2
	historyChrome    = 2  // blank line + section header
	historyMax       = 10 // most recent triggers worth showing
)

// Column keys of the rules table. The header, every row and the fit all
// switch on these, so the geometry lives in exactly one place.
const (
	colSymbol    = "symbol"
	colCondition = "condition"
	colValue     = "value"
	colStatus    = "status"
)

// rulesGutter is the cursor column each row leads with, spent before the
// first cell.
const rulesGutter = 2

// tooNarrow stands in for the table on a frame that cannot hold even the
// symbol column.
const tooNarrow = "  Terminal too small for alert rules."

// rulesColumns is the table's one geometry. Prio is the shedding order on
// a narrow frame: status goes first, since a rule's row is legible
// without ON/OFF; symbol survives longest, since a row that does not name
// its rule names nothing. Condition is flexible because a compound or
// period-qualified condition is the longest text in the table.
func rulesColumns() []format.Col {
	return []format.Col{
		{Key: colSymbol, Width: 12, Min: 6, Prio: 4},
		{Key: colCondition, Width: 16, Min: 8, Prio: 2, Flex: true},
		{Key: colValue, Width: 12, Min: 8, Prio: 3},
		{Key: colStatus, Width: 8, Min: 3, Prio: 1},
	}
}

// fit resolves the columns against the frame. A nil layout means not even
// the symbol fits, and the tab prints its too-narrow line instead of a
// partial table.
func (m Model) fit() format.Layout {
	return format.Fit(rulesColumns(), m.width, rulesGutter)
}

// layout splits the available height between the rules table and the
// recent-alerts list. Both blocks scroll rather than overflowing the
// frame: rules the cursor cannot reach are as good as missing, and a
// list that paints past the bottom of the tab pushes the status bar off
// screen. Rules get first claim on the space, but never more than half
// when there is history to show.
func (m Model) layout(ruleCount int) (ruleRows, histRows int) {
	histTotal := len(m.history)
	if histTotal > historyMax {
		histTotal = historyMax
	}
	if ruleCount == 0 {
		return 0, format.VisibleRows(m.height, historyChrome, histTotal)
	}

	reserved := rulesHeaderLines + rulesFooterLines
	if histTotal > 0 {
		want := histTotal + historyChrome
		if half := (m.height - reserved) / 2; want > half {
			want = half
		}
		if want > 0 {
			reserved += want
		}
	}
	ruleRows = format.VisibleRows(m.height, reserved, ruleCount)

	histRows = m.height - rulesHeaderLines - rulesFooterLines - ruleRows - historyChrome
	if histRows < 0 {
		histRows = 0
	}
	if histRows > histTotal {
		histRows = histTotal
	}
	return ruleRows, histRows
}

// emptyState is the placeholder for a config with no alerts at all.
var emptyState = []string{
	"  No alerts configured.",
	"  Add alerts in ~/.config/mkt/config.yaml",
	"",
	"  Example:",
	"  alerts:",
	"    - symbol: BTCUSDT",
	"      condition: above",
	"      value: 100000",
	"      enabled: true",
}

// View renders the alerts view.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var sb strings.Builder
	rules := m.engine.Rules()
	ruleRows, histRows := m.layout(len(rules))

	if len(rules) == 0 && len(m.history) == 0 {
		for _, line := range emptyState {
			sb.WriteString(theme.StyleDim.Render(format.Truncate(line, m.width)))
			sb.WriteString("\n")
		}
		return sb.String()
	}

	if len(rules) > 0 {
		sb.WriteString(theme.SectionHeader("Alert Rules", m.width))
		sb.WriteString("\n")

		cols := m.fit()
		if cols == nil {
			sb.WriteString(theme.StyleDim.Render(format.Truncate(tooNarrow, m.width)))
			sb.WriteString("\n")
		} else {
			start := format.ViewportStart(m.cursor, len(rules), ruleRows)
			end := start + ruleRows
			if end > len(rules) {
				end = len(rules)
			}

			sb.WriteString(theme.StyleHeader.Render(headerLine(cols)))
			sb.WriteString("\n")
			sb.WriteString(theme.StyleBorderChar.Render(format.Repeat("─", m.width)))
			sb.WriteString("\n")

			for i, r := range rules[start:end] {
				sb.WriteString(ruleLine(cols, r, i+start == m.cursor))
				sb.WriteString("\n")
			}

			sb.WriteString("\n")
			sb.WriteString(m.footerLine(rules, start, end, ruleRows))
			sb.WriteString("\n")
		}
	}

	// Recent alerts — most recent last, clipped to whatever height the
	// rules table left over.
	if histRows > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("Recent Alerts", m.width))
		sb.WriteString("\n")
		for _, a := range m.history[len(m.history)-histRows:] {
			lead := "  " + a.Timestamp.Format("15:04:05") + "  "
			sb.WriteString(theme.StyleDim.Render(format.Truncate(lead, m.width)))
			sb.WriteString(styleAlert.Render(
				format.Truncate(a.Message, m.width-lipgloss.Width(lead))))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// headerLine labels the columns that survived the fit, aligned the way
// their cells are.
func headerLine(cols format.Layout) string {
	var b strings.Builder
	b.WriteString(format.Spaces(rulesGutter))
	for i, c := range cols {
		if i > 0 {
			b.WriteString(" ")
		}
		switch c.Key {
		case colSymbol:
			b.WriteString(pad("SYMBOL", c.Width, false))
		case colCondition:
			b.WriteString(pad("CONDITION", c.Width, false))
		case colValue:
			b.WriteString(pad("VALUE", c.Width, true))
		case colStatus:
			b.WriteString(pad("STATUS", c.Width, true))
		}
	}
	return b.String()
}

// ruleLine renders one rule across the columns that survived the fit.
func ruleLine(cols format.Layout, r alert.Rule, selected bool) string {
	var b strings.Builder
	if selected {
		b.WriteString(theme.StyleCursorGutter.Render("▎") + " ")
	} else {
		b.WriteString(format.Spaces(rulesGutter))
	}
	for i, c := range cols {
		if i > 0 {
			b.WriteString(" ")
		}
		switch c.Key {
		case colSymbol:
			b.WriteString(theme.StyleSymbol.Render(pad(r.Symbol, c.Width, false)))
		case colCondition:
			b.WriteString(theme.StyleVal.Render(pad(conditionText(r), c.Width, false)))
		case colValue:
			b.WriteString(theme.StyleVal.Render(pad(valueText(r, c.Width), c.Width, true)))
		case colStatus:
			if r.Enabled {
				b.WriteString(styleOn.Render(pad("ON", c.Width, true)))
			} else {
				b.WriteString(styleOff.Render(pad("OFF", c.Width, true)))
			}
		}
	}
	return b.String()
}

// footerLine is the row under the table: the delete prompt when one is
// pending, otherwise the key hints and the scroll position.
func (m Model) footerLine(rules []alert.Rule, start, end, ruleRows int) string {
	if m.confirmDelete >= 0 && m.confirmDelete < len(rules) {
		r := rules[m.confirmDelete]
		what := string(r.Condition)
		if r.IsCompound() {
			what = "compound rule"
		}
		// The prompt and its keys share one screen line, so they are
		// budgeted against the frame together.
		prompt := format.Truncate(fmt.Sprintf("  Delete %s %s?", r.Symbol, what), m.width)
		line := styleOff.Render(prompt)
		if left := m.width - lipgloss.Width(prompt); left > 0 {
			line += theme.StyleDim.Render(
				format.Truncate("  y: confirm  any other key: cancel", left))
		}
		return line
	}

	hint := "  t: toggle  d: delete  j/k: navigate"
	if ruleRows < len(rules) {
		hint += fmt.Sprintf("   showing %d-%d of %d", start+1, end, len(rules))
	}
	return theme.StyleDim.Render(format.Truncate(hint, m.width))
}

// conditionText names the condition, qualified by its lookback when the
// rule sets one.
func conditionText(r alert.Rule) string {
	if r.Period > 0 {
		return fmt.Sprintf("%s(%d)", r.Condition, r.Period)
	}
	return string(r.Condition)
}

// valueText renders a rule's threshold in at most cells display cells.
// Thresholds span price levels, RSI points and volume bounds in the
// billions, and four decimals of the last is fifteen cells: precision is
// spent first, then magnitude folds into a suffix, so the column is a
// maximum as well as a minimum.
func valueText(r alert.Rule, cells int) string {
	if r.Condition == alert.CondMACDCross {
		return "—"
	}
	for _, dec := range []int{4, 2, 0} {
		if s := strconv.FormatFloat(r.Value, 'f', dec, 64); lipgloss.Width(s) <= cells {
			return s
		}
	}
	if s := format.FormatVolume(r.Value); lipgloss.Width(s) <= cells {
		return s
	}
	return format.Truncate(strconv.FormatFloat(r.Value, 'g', 2, 64), cells)
}

// pad fits s to exactly cells display cells, right-aligned when right is
// set. Content is truncated and padded before it is styled: fmt's width
// verbs count runes, so padding a string that already carries ANSI
// escapes counts the escapes and the cell silently grows.
func pad(s string, cells int, right bool) string {
	s = format.Truncate(s, cells)
	gap := format.Spaces(cells - lipgloss.Width(s))
	if right {
		return gap + s
	}
	return s + gap
}
