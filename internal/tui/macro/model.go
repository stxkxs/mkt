package macro

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleMacroVal = lipgloss.NewStyle().Foreground(theme.ColorFg)
)

type category struct {
	name    string
	symbols []yahoo.MacroSymbol
}

var categories = []category{
	{
		name: "Rates",
		symbols: []yahoo.MacroSymbol{
			{Symbol: "^TNX", Label: "10Y Treasury"},
			{Symbol: "^IRX", Label: "13W T-Bill"},
		},
	},
	{
		name: "Volatility",
		symbols: []yahoo.MacroSymbol{
			{Symbol: "^VIX", Label: "VIX"},
		},
	},
	{
		name: "Currency & Commodities",
		symbols: []yahoo.MacroSymbol{
			{Symbol: "DX-Y.NYB", Label: "Dollar (DXY)"},
			{Symbol: "GC=F", Label: "Gold"},
			{Symbol: "CL=F", Label: "WTI Crude"},
		},
	},
	{
		name: "Benchmarks",
		symbols: []yahoo.MacroSymbol{
			{Symbol: "^GSPC", Label: "S&P 500"},
			{Symbol: "BTC-USD", Label: "Bitcoin"},
		},
	},
}

// Model is the macro dashboard tab.
type Model struct {
	quotes map[string]provider.Quote
	width  int
	height int
}

// New creates a macro model.
func New() Model {
	return Model{
		quotes: make(map[string]provider.Quote),
	}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// UpdateQuotes replaces all macro quotes.
func (m *Model) UpdateQuotes(quotes []provider.Quote) {
	for _, q := range quotes {
		m.quotes[q.Symbol] = q
	}
}

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleMacroVal = lipgloss.NewStyle().Foreground(theme.ColorFg)
}

// View renders the macro dashboard.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(theme.SectionHeader("Macro Dashboard", m.width))
	sb.WriteString("\n\n")

	if len(m.quotes) == 0 {
		sb.WriteString(theme.StyleDim.Render("  Loading macro data..."))
		return sb.String()
	}

	for _, cat := range categories {
		sb.WriteString(theme.SectionHeader(cat.name, m.width))
		sb.WriteString("\n")

		for _, ms := range cat.symbols {
			q, ok := m.quotes[ms.Symbol]
			if !ok {
				sb.WriteString(fmt.Sprintf("    %-18s %s\n",
					theme.StyleDim.Render(ms.Label),
					theme.StyleDim.Render("—"),
				))
				continue
			}

			changeStyle := theme.StyleUp
			sign := "+"
			arrow := "▲"
			if q.ChangePct < 0 {
				changeStyle = theme.StyleDown
				sign = ""
				arrow = "▼"
			}

			priceStr := format.FormatPrice(q.Price)
			changeStr := fmt.Sprintf("%s%s%.2f%%", arrow, sign, q.ChangePct)

			sb.WriteString(fmt.Sprintf("    %-18s %12s  %s\n",
				theme.StyleDim.Render(ms.Label),
				styleMacroVal.Render(priceStr),
				changeStyle.Render(fmt.Sprintf("%-10s", changeStr)),
			))
		}
		sb.WriteString("\n")
	}

	// 2s10s spread
	tnx, hasTNX := m.quotes["^TNX"]
	irx, hasIRX := m.quotes["^IRX"]
	if hasTNX && hasIRX {
		spread := tnx.Price - irx.Price
		spreadStyle := theme.StyleUp
		if spread < 0 {
			spreadStyle = theme.StyleDown
		}
		sb.WriteString(theme.SectionHeader("Computed", m.width))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("    %-18s %12s\n",
			theme.StyleDim.Render("2s10s Spread"),
			spreadStyle.Render(fmt.Sprintf("%.3f%%", spread)),
		))
	}

	return sb.String()
}
