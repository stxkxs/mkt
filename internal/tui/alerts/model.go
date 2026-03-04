package alerts

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
)

var (
	colorGreen  = lipgloss.Color("#9ece6a")
	colorRed    = lipgloss.Color("#f7768e")
	colorDim    = lipgloss.Color("#565f89")
	colorAccent = lipgloss.Color("#7aa2f7")
	colorCyan   = lipgloss.Color("#7dcfff")
	colorYellow = lipgloss.Color("#e0af68")

	styleHeader = lipgloss.NewStyle().Foreground(colorDim).Bold(true)
	styleCursor = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleSymbol = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	styleOn     = lipgloss.NewStyle().Foreground(colorGreen)
	styleOff    = lipgloss.NewStyle().Foreground(colorRed)
	styleVal    = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	styleDim    = lipgloss.NewStyle().Foreground(colorDim)
	styleAlert  = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
)

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

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		sb.WriteString(styleDim.Render("  No alerts configured.\n"))
		sb.WriteString(styleDim.Render("  Add alerts in ~/.config/mkt/config.yaml\n\n"))
		sb.WriteString(styleDim.Render("  Example:\n"))
		sb.WriteString(styleDim.Render("  alerts:\n"))
		sb.WriteString(styleDim.Render("    - symbol: BTCUSDT\n"))
		sb.WriteString(styleDim.Render("      condition: above\n"))
		sb.WriteString(styleDim.Render("      value: 100000\n"))
		sb.WriteString(styleDim.Render("      enabled: true\n"))
		return sb.String()
	}

	// Rules table
	if len(rules) > 0 {
		sb.WriteString(styleHeader.Render("  ALERT RULES"))
		sb.WriteString("\n")
		header := fmt.Sprintf("  %-12s %-10s %12s %8s", "SYMBOL", "CONDITION", "VALUE", "STATUS")
		sb.WriteString(styleHeader.Render(header))
		sb.WriteString("\n")

		for i, r := range rules {
			cursor := "  "
			if i == m.cursor {
				cursor = styleCursor.Render("> ")
			}

			status := styleOn.Render("ON")
			if !r.Enabled {
				status = styleOff.Render("OFF")
			}

			row := fmt.Sprintf("%s%s %s %s %s",
				cursor,
				styleSymbol.Render(fmt.Sprintf("%-12s", r.Symbol)),
				styleVal.Render(fmt.Sprintf("%-10s", r.Condition)),
				styleVal.Render(fmt.Sprintf("%12.4f", r.Value)),
				status,
			)
			sb.WriteString(row)
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
		sb.WriteString(styleDim.Render("  t: toggle  d: delete  j/k: navigate"))
		sb.WriteString("\n")
	}

	// Recent alerts
	if len(m.history) > 0 {
		sb.WriteString("\n")
		sb.WriteString(styleHeader.Render("  RECENT ALERTS"))
		sb.WriteString("\n")
		// Show last 10
		start := len(m.history) - 10
		if start < 0 {
			start = 0
		}
		for _, a := range m.history[start:] {
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				styleDim.Render(a.Timestamp.Format("15:04:05")),
				styleAlert.Render(a.Message),
			))
		}
	}

	return sb.String()
}
