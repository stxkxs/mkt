package alerts

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
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
}

// New creates an alerts model.
func New(engine *alert.Engine) Model {
	return Model{
		engine: engine,
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
			if len(rules) > 0 {
				m.engine.RemoveRule(m.cursor)
				if m.cursor >= len(rules)-1 && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	}
	return m, nil
}

// View renders the alerts view.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder
	rules := m.engine.Rules()

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
		sb.WriteString(theme.SectionHeader("Alert Rules", m.width))
		sb.WriteString("\n")
		header := fmt.Sprintf("  %-12s %-16s %12s %8s", "SYMBOL", "CONDITION", "VALUE", "STATUS")
		sb.WriteString(theme.StyleHeader.Render(header))
		sb.WriteString("\n")
		sb.WriteString(theme.StyleBorderChar.Render(strings.Repeat("─", m.width)))
		sb.WriteString("\n")

		for i, r := range rules {
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
		sb.WriteString(theme.StyleDim.Render("  t: toggle  d: delete  j/k: navigate"))
		sb.WriteString("\n")
	}

	// Recent alerts
	if len(m.history) > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("Recent Alerts", m.width))
		sb.WriteString("\n")
		// Show last 10
		start := len(m.history) - 10
		if start < 0 {
			start = 0
		}
		for _, a := range m.history[start:] {
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				theme.StyleDim.Render(a.Timestamp.Format("15:04:05")),
				styleAlert.Render(a.Message),
			))
		}
	}

	return sb.String()
}
