package alerts

import (
	"fmt"
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
		if len(rules) == 0 {
			return m, nil
		}
		row := msg.Y - rulesHeaderLines
		if row < 0 {
			return m, nil
		}
		ruleRows, _ := m.layout(len(rules))
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

// View renders the alerts view.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder
	rules := m.engine.Rules()
	ruleRows, histRows := m.layout(len(rules))

	if len(rules) == 0 && len(m.history) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No alerts configured.\n"))
		sb.WriteString(theme.StyleDim.Render("  Add alerts in ~/.config/mkt/config.yaml\n\n"))
		sb.WriteString(theme.StyleDim.Render("  Example:\n"))
		sb.WriteString(theme.StyleDim.Render("  alerts:\n"))
		sb.WriteString(theme.StyleDim.Render("    - symbol: BTCUSDT\n"))
		sb.WriteString(theme.StyleDim.Render("      condition: above\n"))
		sb.WriteString(theme.StyleDim.Render("      value: 100000\n"))
		sb.WriteString(theme.StyleDim.Render("      enabled: true\n"))
		return sb.String()
	}

	// Rules table
	if len(rules) > 0 {
		start := format.ViewportStart(m.cursor, len(rules), ruleRows)
		end := start + ruleRows
		if end > len(rules) {
			end = len(rules)
		}

		sb.WriteString(theme.SectionHeader("Alert Rules", m.width))
		sb.WriteString("\n")
		header := fmt.Sprintf("  %-12s %-16s %12s %8s", "SYMBOL", "CONDITION", "VALUE", "STATUS")
		sb.WriteString(theme.StyleHeader.Render(header))
		sb.WriteString("\n")
		sb.WriteString(theme.StyleBorderChar.Render(format.Repeat("─", m.width)))
		sb.WriteString("\n")

		for i, r := range rules[start:end] {
			i += start
			cursor := "  "
			if i == m.cursor {
				cursor = theme.StyleCursorGutter.Render("▎") + " "
			}

			status := styleOn.Render("ON")
			if !r.Enabled {
				status = styleOff.Render("OFF")
			}

			condStr := string(r.Condition)
			if r.Period > 0 {
				condStr = fmt.Sprintf("%s(%d)", r.Condition, r.Period)
			}

			valStr := fmt.Sprintf("%12.4f", r.Value)
			if r.Condition == alert.CondMACDCross {
				valStr = fmt.Sprintf("%12s", "—")
			}

			row := fmt.Sprintf("%s%s %s %s %s",
				cursor,
				theme.StyleSymbol.Render(fmt.Sprintf("%-12s", r.Symbol)),
				theme.StyleVal.Render(fmt.Sprintf("%-16s", condStr)),
				theme.StyleVal.Render(valStr),
				status,
			)
			sb.WriteString(row)
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
		if m.confirmDelete >= 0 && m.confirmDelete < len(rules) {
			r := rules[m.confirmDelete]
			what := string(r.Condition)
			if r.IsCompound() {
				what = "compound rule"
			}
			sb.WriteString(styleOff.Render(fmt.Sprintf("  Delete %s %s?", r.Symbol, what)))
			sb.WriteString(theme.StyleDim.Render("  y: confirm  any other key: cancel"))
		} else {
			hint := "  t: toggle  d: delete  j/k: navigate"
			if ruleRows < len(rules) {
				hint += fmt.Sprintf("   showing %d-%d of %d", start+1, end, len(rules))
			}
			sb.WriteString(theme.StyleDim.Render(hint))
		}
		sb.WriteString("\n")
	}

	// Recent alerts — most recent last, clipped to whatever height the
	// rules table left over.
	if histRows > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("Recent Alerts", m.width))
		sb.WriteString("\n")
		for _, a := range m.history[len(m.history)-histRows:] {
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				theme.StyleDim.Render(a.Timestamp.Format("15:04:05")),
				styleAlert.Render(a.Message),
			))
		}
	}

	return sb.String()
}
