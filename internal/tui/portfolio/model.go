package portfolio

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleTotal = lipgloss.NewStyle().Foreground(theme.ColorYellow).Bold(true)
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
)

// Model is the portfolio view.
type Model struct {
	portfolios []portfolio.Portfolio
	activeIdx  int
	quotes     map[string]provider.Quote
	cursor     int
	width      int
	height     int
}

// New creates a portfolio model.
func New(portfolios []portfolio.Portfolio) Model {
	return Model{
		portfolios: portfolios,
		quotes:     make(map[string]provider.Quote),
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

func (m Model) activePortfolio() portfolio.Portfolio {
	if m.activeIdx < len(m.portfolios) {
		return m.portfolios[m.activeIdx]
	}
	return portfolio.Portfolio{}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		holdings := m.activePortfolio().Holdings
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(holdings)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "[":
			if len(m.portfolios) > 1 {
				m.activeIdx = (m.activeIdx - 1 + len(m.portfolios)) % len(m.portfolios)
				m.cursor = 0
			}
		case "]":
			if len(m.portfolios) > 1 {
				m.activeIdx = (m.activeIdx + 1) % len(m.portfolios)
				m.cursor = 0
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

	if len(m.portfolios) == 0 {
		return theme.StyleDim.Render("  No portfolios configured.\n  Add portfolios in ~/.config/mkt/config.yaml")
	}

	p := m.activePortfolio()
	var sb strings.Builder

	// Portfolio selector
	navHint := ""
	if len(m.portfolios) > 1 {
		navHint = theme.StyleDim.Render(fmt.Sprintf("  [/]: switch  (%d/%d)", m.activeIdx+1, len(m.portfolios)))
	}
	sb.WriteString(styleLabel.Render(fmt.Sprintf("  %s", p.Name)) + navHint + "\n")

	if len(p.Holdings) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No holdings in this portfolio.\n"))
		return sb.String()
	}

	// Header
	header := fmt.Sprintf("  %-6s %-22s %10s %10s %12s %12s %10s",
		"SYMBOL", "NAME", "QTY", "COST", "PRICE", "VALUE", "P&L")
	sb.WriteString(theme.StyleHeader.Render(header))
	sb.WriteString("\n")

	summary := portfolio.Evaluate(p.Holdings, m.quotes)

	// Compute visible window (2 for portfolio name + header, 3 for total/blank/summary)
	maxRows := m.height - 5
	if maxRows < 1 || maxRows >= len(summary.Positions) {
		maxRows = len(summary.Positions)
	}
	startIdx := 0
	if len(summary.Positions) > maxRows {
		startIdx = m.cursor - maxRows + 1
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx+maxRows > len(summary.Positions) {
			startIdx = len(summary.Positions) - maxRows
		}
	}
	endIdx := startIdx + maxRows
	if endIdx > len(summary.Positions) {
		endIdx = len(summary.Positions)
	}

	for i := startIdx; i < endIdx; i++ {
		pos := summary.Positions[i]
		cursor := "  "
		if i == m.cursor {
			cursor = theme.StyleCursor.Render("> ")
		}

		pnlStyle := theme.StyleUp
		sign := "+"
		if pos.PnL < 0 {
			pnlStyle = theme.StyleDown
			sign = ""
		}

		name := pos.Name
		if len(name) > 22 {
			name = name[:21] + "…"
		}

		row := fmt.Sprintf("%s%s %s %s %s %s %s %s",
			cursor,
			theme.StyleSymbol.Render(fmt.Sprintf("%-6s", pos.Symbol)),
			theme.StyleDim.Render(fmt.Sprintf("%-22s", name)),
			theme.StyleVal.Render(fmt.Sprintf("%10.4f", pos.Quantity)),
			theme.StyleVal.Render(fmt.Sprintf("%10.2f", pos.CostBasis)),
			theme.StyleVal.Render(fmt.Sprintf("%12.2f", pos.CurrentPrice)),
			theme.StyleVal.Render(fmt.Sprintf("%12.2f", pos.MarketValue)),
			pnlStyle.Render(fmt.Sprintf("%s%.2f (%s%.1f%%)", sign, pos.PnL, sign, pos.PnLPct)),
		)
		sb.WriteString(row)
		sb.WriteString("\n")
	}

	// Total row
	sb.WriteString("\n")
	totalPnlStyle := theme.StyleUp
	totalSign := "+"
	if summary.TotalPnL < 0 {
		totalPnlStyle = theme.StyleDown
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
