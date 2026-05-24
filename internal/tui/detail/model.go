package detail

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
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
	cb     *coinbase.Provider
	book   coinbase.OrderBook
	width  int
	height int
	active bool
}

// New creates a detail model. The coinbase provider is used to fetch
// order books for crypto symbols when shown; pass nil to disable.
func New(cache *market.Cache, cb *coinbase.Provider) Model {
	return Model{cache: cache, cb: cb}
}

// SetSymbol updates the displayed symbol and returns a tea.Cmd that
// fetches an order book if the symbol is crypto. The Cmd is nil for
// non-crypto symbols or when no coinbase provider is configured.
func (m *Model) SetSymbol(sym string) tea.Cmd {
	m.symbol = sym
	m.book = coinbase.OrderBook{}
	if m.cb == nil || !isCryptoSymbol(sym) {
		return nil
	}
	cb := m.cb
	return func() tea.Msg {
		b, err := cb.FetchOrderBook(context.Background(), sym)
		if err != nil {
			return nil
		}
		return orderBookLoadedMsg{book: b}
	}
}

type orderBookLoadedMsg struct{ book coinbase.OrderBook }

func isCryptoSymbol(s string) bool {
	up := strings.ToUpper(s)
	return strings.Contains(up, "-USD") || strings.HasSuffix(up, "USDT")
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
	case orderBookLoadedMsg:
		// Only keep the book if it still matches the displayed symbol.
		if msg.book.ProductID == "" || strings.EqualFold(msg.book.ProductID, m.symbol) {
			m.book = msg.book
		}
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

	// Order book (top 5 per side) for crypto symbols
	if len(m.book.Bids) > 0 || len(m.book.Asks) > 0 {
		sb.WriteString("\n  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("Order Book (top 5)"))
		sb.WriteString("\n")
		n := 5
		if n > len(m.book.Bids) {
			n = len(m.book.Bids)
		}
		na := 5
		if na > len(m.book.Asks) {
			na = len(m.book.Asks)
		}
		rows := n
		if na > rows {
			rows = na
		}
		for i := 0; i < rows; i++ {
			var bidStr, askStr string
			if i < n {
				bidStr = fmt.Sprintf("%10.2f x %.4f", m.book.Bids[i].Price, m.book.Bids[i].Size)
			} else {
				bidStr = strings.Repeat(" ", 21)
			}
			if i < na {
				askStr = fmt.Sprintf("%10.2f x %.4f", m.book.Asks[i].Price, m.book.Asks[i].Size)
			}
			sb.WriteString(fmt.Sprintf("  %s    %s\n",
				theme.StyleUp.Render(bidStr),
				theme.StyleDown.Render(askStr),
			))
		}
	}

	return sb.String()
}
