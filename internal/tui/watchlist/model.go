package watchlist

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

	styleHeader = lipgloss.NewStyle().
			Foreground(colorDim).
			Bold(true)

	styleCursor = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleSymbol = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	styleUp = lipgloss.NewStyle().
		Foreground(colorGreen)

	styleDown = lipgloss.NewStyle().
			Foreground(colorRed)

	styleNeutral = lipgloss.NewStyle().
			Foreground(colorDim)

	styleVol = lipgloss.NewStyle().
			Foreground(colorYellow)

	styleSparkUp = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleSparkDown = lipgloss.NewStyle().
			Foreground(colorRed)
)

// Model is the watchlist view.
type Model struct {
	symbols []string
	quotes  map[string]provider.Quote
	cache   *market.Cache
	cursor  int
	width   int
	height  int
}

// New creates a watchlist model.
func New(symbols []string, cache *market.Cache) Model {
	return Model{
		symbols: symbols,
		quotes:  make(map[string]provider.Quote),
		cache:   cache,
	}
}

// Symbols returns the current symbol list.
func (m Model) Symbols() []string {
	return m.symbols
}

// SelectedSymbol returns the currently selected symbol.
func (m Model) SelectedSymbol() string {
	if m.cursor < len(m.symbols) {
		return m.symbols[m.cursor]
	}
	return ""
}

// SetSize updates the viewport dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles messages for the watchlist.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.symbols)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.symbols) > 0 {
				m.cursor = len(m.symbols) - 1
			}
		}
	}
	return m, nil
}

// UpdateQuote processes a new quote.
func (m *Model) UpdateQuote(q provider.Quote) {
	m.quotes[q.Symbol] = q
}

// View renders the watchlist.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	sparkWidth := 20
	var sb strings.Builder

	// Header
	header := fmt.Sprintf("  %-12s %12s %10s %8s  %-*s",
		"SYMBOL", "PRICE", "CHANGE", "VOL", sparkWidth, "TREND")
	sb.WriteString(styleHeader.Render(header))
	sb.WriteString("\n")

	// Rows
	for i, sym := range m.symbols {
		q, hasQuote := m.quotes[sym]

		// Cursor indicator
		cursor := "  "
		if i == m.cursor {
			cursor = styleCursor.Render("> ")
		}

		// Symbol
		symStr := styleSymbol.Render(fmt.Sprintf("%-12s", sym))

		// Price
		var priceStr, changeStr string
		var changeStyle lipgloss.Style
		if hasQuote {
			priceStr = fmt.Sprintf("%12s", formatPrice(q.Price))
			sign := "+"
			if q.ChangePct < 0 {
				sign = ""
			}
			changeStr = fmt.Sprintf("%s%.2f%%", sign, q.ChangePct)
			if q.ChangePct > 0 {
				changeStyle = styleUp
			} else if q.ChangePct < 0 {
				changeStyle = styleDown
			} else {
				changeStyle = styleNeutral
			}
		} else {
			priceStr = fmt.Sprintf("%12s", "—")
			changeStr = fmt.Sprintf("%10s", "—")
			changeStyle = styleNeutral
		}

		// Volume
		var volStr string
		if hasQuote && q.Volume > 0 {
			volStr = styleVol.Render(fmt.Sprintf("%8s", formatVolume(q.Volume)))
		} else {
			volStr = styleNeutral.Render(fmt.Sprintf("%8s", "—"))
		}

		// Sparkline
		prices := m.cache.Prices(sym)
		spark := sparkline(prices, sparkWidth)
		var sparkStyled string
		if hasQuote && q.ChangePct >= 0 {
			sparkStyled = styleSparkUp.Render(spark)
		} else {
			sparkStyled = styleSparkDown.Render(spark)
		}

		row := fmt.Sprintf("%s%s %s %s %s  %s",
			cursor, symStr, priceStr,
			changeStyle.Render(fmt.Sprintf("%10s", changeStr)),
			volStr, sparkStyled)

		if i == m.cursor {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Render(row))
		} else {
			sb.WriteString(row)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
