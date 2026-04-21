package detail

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
}

// Model is the detail panel for a selected symbol.
type Model struct {
	symbol string
	quote  provider.Quote
	cache  *market.Cache
	width  int
	height int
	active bool
}

// New creates a detail model.
func New(cache *market.Cache) Model {
	return Model{cache: cache}
}

// SetSymbol updates the displayed symbol.
func (m *Model) SetSymbol(sym string) {
	m.symbol = sym
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetActive sets whether this panel is active.
func (m *Model) SetActive(a bool) {
	m.active = a
}

// Active returns whether the panel is active.
func (m Model) Active() bool {
	return m.active
}

// Symbol returns the current symbol.
func (m Model) Symbol() string {
	return m.symbol
}

// UpdateQuote processes a new quote.
func (m *Model) UpdateQuote(q provider.Quote) {
	if q.Symbol == m.symbol {
		m.quote = q
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.active = false
		}
	}
	return m, nil
}

// View renders the detail panel.
func (m Model) View() string {
	if m.symbol == "" || m.width == 0 {
		return ""
	}

	var sb strings.Builder

	// Header
	header := lipgloss.NewStyle().
		Foreground(theme.ColorAccent).
		Bold(true).
		Render(fmt.Sprintf("  %s Detail", m.symbol))
	sb.WriteString(header)
	sb.WriteString("\n\n")

	if m.quote.Price == 0 {
		sb.WriteString(styleLabel.Render("  Waiting for data..."))
		return sb.String()
	}

	q := m.quote

	// Price + change
	changeStyle := theme.StyleUp
	arrow := "▲"
	if q.ChangePct < 0 {
		changeStyle = theme.StyleDown
		arrow = "▼"
	}
	sb.WriteString(fmt.Sprintf("  %s  %s\n\n",
		styleValue.Bold(true).Render(format.FormatPrice(q.Price)),
		changeStyle.Render(fmt.Sprintf("%s %.2f (%.2f%%)", arrow, q.Change, q.ChangePct)),
	))

	// Details grid
	details := []struct{ label, value string }{
		{"24h High", format.FormatPrice(q.High24h)},
		{"24h Low", format.FormatPrice(q.Low24h)},
		{"Volume", format.FormatVolume(q.Volume)},
		{"Provider", q.Provider},
		{"Type", q.Asset.String()},
	}
	for _, d := range details {
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			styleLabel.Render(fmt.Sprintf("%-12s", d.label)),
			styleValue.Render(d.value),
		))
	}

	// Sparkline chart
	sb.WriteString("\n")
	prices := m.cache.Prices(m.symbol)
	if len(prices) > 0 {
		chartWidth := m.width - 4
		if chartWidth > 80 {
			chartWidth = 80
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorCyan).Render(
			"  " + format.BrailleSparkline(prices, chartWidth),
		))
		sb.WriteString("\n")
	}

	return sb.String()
}
