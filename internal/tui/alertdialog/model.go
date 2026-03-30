package alertdialog

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
	styleHint  = lipgloss.NewStyle().Foreground(theme.ColorDim)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
	styleHint = lipgloss.NewStyle().Foreground(theme.ColorDim)
}

type step int

const (
	stepCondition step = iota
	stepValue
	stepConfirm
)

// Model is the quick alert creation dialog.
type Model struct {
	symbol       string
	currentPrice float64
	conditions   []alert.Condition
	condIdx      int
	valueInput   string
	step         step
	active       bool
	engine       *alert.Engine
	width        int
	height       int
}

// New creates an alert dialog model.
func New(engine *alert.Engine) Model {
	return Model{
		engine:     engine,
		conditions: alert.AllConditions(),
	}
}

// Open activates the dialog for a given symbol.
func (m *Model) Open(symbol string, price float64) {
	m.symbol = symbol
	m.currentPrice = price
	m.condIdx = 0
	m.valueInput = fmt.Sprintf("%.2f", price)
	m.step = stepCondition
	m.active = true
}

// Active returns whether the dialog is visible.
func (m Model) Active() bool {
	return m.active
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		switch key {
		case "esc":
			m.active = false
			return m, nil
		case "enter":
			switch m.step {
			case stepCondition:
				cond := m.conditions[m.condIdx]
				if cond == alert.CondMACDCross {
					// Skip value step for MACD cross
					m.valueInput = "0"
					m.step = stepConfirm
				} else {
					m.step = stepValue
				}
			case stepValue:
				m.step = stepConfirm
			case stepConfirm:
				val, err := strconv.ParseFloat(m.valueInput, 64)
				if err != nil {
					return m, nil
				}
				cond := m.conditions[m.condIdx]
				period := 0
				if alert.IsIndicatorCondition(cond) {
					switch {
					case cond == alert.CondRSIAbove || cond == alert.CondRSIBelow:
						period = 14
					case cond == alert.CondSMACrossAbove || cond == alert.CondSMACrossBelow:
						period = 20
					}
				}
				m.engine.AddRule(alert.Rule{
					Symbol:    m.symbol,
					Condition: cond,
					Value:     val,
					Period:    period,
					Enabled:   true,
				})
				m.active = false
			}
			return m, nil
		case "left", "h":
			if m.step == stepCondition {
				if m.condIdx > 0 {
					m.condIdx--
				} else {
					m.condIdx = len(m.conditions) - 1
				}
			}
		case "right", "l":
			if m.step == stepCondition {
				m.condIdx = (m.condIdx + 1) % len(m.conditions)
			}
		case "backspace":
			if m.step == stepValue && len(m.valueInput) > 0 {
				m.valueInput = m.valueInput[:len(m.valueInput)-1]
			}
		default:
			if m.step == stepValue {
				for _, r := range key {
					if (r >= '0' && r <= '9') || r == '.' {
						m.valueInput += string(r)
					}
				}
			}
		}
	}
	return m, nil
}

// View renders the dialog.
func (m Model) View() string {
	if !m.active {
		return ""
	}

	cond := m.conditions[m.condIdx]
	condStr := fmt.Sprintf("◄ %s ►", cond)

	var lines []string
	lines = append(lines, "")

	if m.step == stepCondition {
		lines = append(lines, "  "+styleLabel.Render("Condition: ")+styleValue.Render(condStr))
		lines = append(lines, "")
		lines = append(lines, "  "+styleHint.Render("←/→: cycle  enter: next  esc: cancel"))
	} else if m.step == stepValue {
		lines = append(lines, "  "+styleLabel.Render("Condition: ")+styleValue.Render(string(cond)))
		lines = append(lines, "  "+styleLabel.Render("Value:     ")+styleValue.Render(m.valueInput+"_"))
		lines = append(lines, "")
		lines = append(lines, "  "+styleHint.Render("type value  enter: next  esc: cancel"))
	} else {
		val := m.valueInput
		if cond == alert.CondMACDCross {
			val = "—"
		}
		lines = append(lines, "  "+styleLabel.Render("Condition: ")+styleValue.Render(string(cond)))
		lines = append(lines, "  "+styleLabel.Render("Value:     ")+styleValue.Render(val))
		lines = append(lines, "")
		lines = append(lines, "  "+styleHint.Render("enter: save  esc: cancel"))
	}

	lines = append(lines, "")
	content := strings.Join(lines, "\n")
	return theme.RenderPanel(fmt.Sprintf("New Alert: %s", m.symbol), content, 42)
}
