package portfolio

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
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
	styleUp     = lipgloss.NewStyle().Foreground(colorGreen)
	styleDown   = lipgloss.NewStyle().Foreground(colorRed)
	styleVal    = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	styleTotal  = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(colorDim)
)

// Model is the portfolio view.
type Model struct {
	holdings []portfolio.Holding
	quotes   map[string]provider.Quote
	cursor   int
	width    int
	height   int
}

// New creates a portfolio model.
func New(holdings []portfolio.Holding) Model {
	return Model{
		holdings: holdings,
		quotes:   make(map[string]provider.Quote),
	}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// UpdateQuote processes a new quote.
func (m *Model) UpdateQuote(q provider.Quote) {
	m.quotes[q.Symbol] = q
}

// SetHoldings updates the holdings list.
func (m *Model) SetHoldings(h []portfolio.Holding) {
	m.holdings = h
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.holdings)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		}
	}
	return m, nil
}

// View renders the portfolio.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	if len(m.holdings) == 0 {
		return styleDim.Render("  No holdings configured.\n  Add holdings in ~/.config/mkt/config.yaml")
	}

	var sb strings.Builder

	// Header
	header := fmt.Sprintf("  %-12s %10s %10s %12s %12s %10s",
		"SYMBOL", "QTY", "COST", "PRICE", "VALUE", "P&L")
	sb.WriteString(styleHeader.Render(header))
	sb.WriteString("\n")

	summary := portfolio.Evaluate(m.holdings, m.quotes)

	for i, pos := range summary.Positions {
		cursor := "  "
		if i == m.cursor {
			cursor = styleCursor.Render("> ")
		}

		pnlStyle := styleUp
		sign := "+"
		if pos.PnL < 0 {
			pnlStyle = styleDown
			sign = ""
		}

		row := fmt.Sprintf("%s%s %s %s %s %s %s",
			cursor,
			styleSymbol.Render(fmt.Sprintf("%-12s", pos.Symbol)),
			styleVal.Render(fmt.Sprintf("%10.4f", pos.Quantity)),
			styleVal.Render(fmt.Sprintf("%10.2f", pos.CostBasis)),
			styleVal.Render(fmt.Sprintf("%12.2f", pos.CurrentPrice)),
			styleVal.Render(fmt.Sprintf("%12.2f", pos.MarketValue)),
			pnlStyle.Render(fmt.Sprintf("%s%.2f (%s%.1f%%)", sign, pos.PnL, sign, pos.PnLPct)),
		)
		sb.WriteString(row)
		sb.WriteString("\n")
	}

	// Total row
	sb.WriteString("\n")
	totalPnlStyle := styleUp
	totalSign := "+"
	if summary.TotalPnL < 0 {
		totalPnlStyle = styleDown
		totalSign = ""
	}
	sb.WriteString(fmt.Sprintf("  %s  %s  %s\n",
		styleTotal.Render(fmt.Sprintf("Total Cost: $%.2f", summary.TotalCost)),
		styleTotal.Render(fmt.Sprintf("Value: $%.2f", summary.TotalValue)),
		totalPnlStyle.Bold(true).Render(fmt.Sprintf("P&L: %s$%.2f (%s%.1f%%)",
			totalSign, summary.TotalPnL, totalSign, summary.TotalPnLPct)),
	))

	return sb.String()
}
