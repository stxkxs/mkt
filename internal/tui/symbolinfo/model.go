package symbolinfo

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorAccent).
			Padding(1, 2)
	styleTitle = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim).Width(16)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
	styleHint  = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleError = lipgloss.NewStyle().Foreground(theme.ColorRed)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorAccent).
		Padding(1, 2)
	styleTitle = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim).Width(16)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
	styleHint = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleError = lipgloss.NewStyle().Foreground(theme.ColorRed)
}

type symbolInfoLoadedMsg struct {
	summary yahoo.SymbolSummary
}

type symbolInfoErrorMsg struct {
	err error
}

// Model is the symbol info overlay.
type Model struct {
	summary  yahoo.SymbolSummary
	active   bool
	loading  bool
	errMsg   string
	width    int
	height   int
	provider *yahoo.Provider
}

// New creates a symbol info model.
func New(prov *yahoo.Provider) Model {
	return Model{provider: prov}
}

// Active returns whether the overlay is visible.
func (m Model) Active() bool {
	return m.active
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Open starts fetching summary data for a symbol.
func (m *Model) Open(symbol string) tea.Cmd {
	m.active = true
	m.loading = true
	m.errMsg = ""
	m.summary = yahoo.SymbolSummary{Symbol: symbol}
	prov := m.provider
	return func() tea.Msg {
		s, err := prov.FetchSummary(context.Background(), symbol)
		if err != nil {
			return symbolInfoErrorMsg{err: err}
		}
		return symbolInfoLoadedMsg{summary: s}
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "esc" || msg.String() == "?" {
			m.active = false
		}
	case symbolInfoLoadedMsg:
		m.summary = msg.summary
		m.loading = false
	case symbolInfoErrorMsg:
		m.errMsg = msg.err.Error()
		m.loading = false
	}
	return m, nil
}

// View renders the overlay.
func (m Model) View() string {
	if !m.active {
		return ""
	}

	var lines []string
	lines = append(lines, styleTitle.Render(m.summary.Symbol))
	lines = append(lines, "")

	if m.loading {
		lines = append(lines, styleHint.Render("Loading..."))
	} else if m.errMsg != "" {
		lines = append(lines, styleError.Render("Error: "+m.errMsg))
	} else {
		s := m.summary
		lines = append(lines, row("Market Cap", formatLargeNum(s.MarketCap)))
		lines = append(lines, row("P/E", formatFloat(s.PE)))
		lines = append(lines, row("Forward P/E", formatFloat(s.ForwardPE)))
		lines = append(lines, row("EPS", "$"+formatFloat(s.EPS)))
		lines = append(lines, row("Div Yield", formatPct(s.DivYield)))
		lines = append(lines, row("52W High", "$"+formatFloat(s.Week52High)))
		lines = append(lines, row("52W Low", "$"+formatFloat(s.Week52Low)))
		if s.Sector != "" {
			lines = append(lines, row("Sector", s.Sector))
		}
		if s.Industry != "" {
			lines = append(lines, row("Industry", s.Industry))
		}
	}

	lines = append(lines, "")
	lines = append(lines, styleHint.Render("esc: close"))

	content := strings.Join(lines, "\n")
	return styleBorder.Width(38).Render(content)
}

func row(label, value string) string {
	return styleLabel.Render(label) + styleValue.Render(value)
}

func formatFloat(f float64) string {
	if f == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", f)
}

func formatPct(f float64) string {
	if f == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", f)
}

func formatLargeNum(f float64) string {
	switch {
	case f >= 1e12:
		return fmt.Sprintf("$%.2fT", f/1e12)
	case f >= 1e9:
		return fmt.Sprintf("$%.2fB", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("$%.2fM", f/1e6)
	case f == 0:
		return "—"
	default:
		return fmt.Sprintf("$%.0f", f)
	}
}
