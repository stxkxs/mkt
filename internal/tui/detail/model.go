package detail

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
)

var (
	colorGreen  = lipgloss.Color("#9ece6a")
	colorRed    = lipgloss.Color("#f7768e")
	colorDim    = lipgloss.Color("#565f89")
	colorAccent = lipgloss.Color("#7aa2f7")
	colorCyan   = lipgloss.Color("#7dcfff")
	colorYellow = lipgloss.Color("#e0af68")

	styleLabel = lipgloss.NewStyle().Foreground(colorDim)
	styleValue = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	styleUp    = lipgloss.NewStyle().Foreground(colorGreen)
	styleDown  = lipgloss.NewStyle().Foreground(colorRed)
)

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
		Foreground(colorAccent).
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
	changeStyle := styleUp
	arrow := "▲"
	if q.ChangePct < 0 {
		changeStyle = styleDown
		arrow = "▼"
	}
	sb.WriteString(fmt.Sprintf("  %s  %s\n\n",
		styleValue.Bold(true).Render(formatPrice(q.Price)),
		changeStyle.Render(fmt.Sprintf("%s %.2f (%.2f%%)", arrow, q.Change, q.ChangePct)),
	))

	// Details grid
	details := []struct{ label, value string }{
		{"24h High", formatPrice(q.High24h)},
		{"24h Low", formatPrice(q.Low24h)},
		{"Volume", formatVolume(q.Volume)},
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
		sb.WriteString(lipgloss.NewStyle().Foreground(colorCyan).Render(
			"  " + renderSparkline(prices, chartWidth),
		))
		sb.WriteString("\n")
	}

	return sb.String()
}

func renderSparkline(prices []float64, width int) string {
	if len(prices) > width {
		prices = prices[len(prices)-width:]
	}
	minP, maxP := prices[0], prices[0]
	for _, p := range prices {
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	rng := maxP - minP
	if rng == 0 {
		rng = 1
	}
	var sb strings.Builder
	for _, p := range prices {
		idx := int((p - minP) / rng * float64(len(blocks)-1))
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	return sb.String()
}

func formatPrice(price float64) string {
	switch {
	case price >= 100:
		return fmt.Sprintf("%.2f", price)
	case price >= 1:
		return fmt.Sprintf("%.4f", price)
	default:
		return fmt.Sprintf("%.6f", price)
	}
}

func formatVolume(vol float64) string {
	switch {
	case vol >= 1e9:
		return fmt.Sprintf("%.1fB", vol/1e9)
	case vol >= 1e6:
		return fmt.Sprintf("%.1fM", vol/1e6)
	case vol >= 1e3:
		return fmt.Sprintf("%.1fK", vol/1e3)
	default:
		return fmt.Sprintf("%.0f", vol)
	}
}
