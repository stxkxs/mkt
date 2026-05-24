package macro

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/binance"
	"github.com/stxkxs/mkt/internal/provider/defillama"
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
	quotes  map[string]provider.Quote
	defi    []defillama.TVLSnapshot
	futures []binance.FuturesSnapshot
	width   int
	height  int
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

// UpdateDeFi replaces the DeFi TVL snapshot list.
func (m *Model) UpdateDeFi(chains []defillama.TVLSnapshot) {
	m.defi = chains
}

// UpdateFutures replaces the Binance futures snapshot list.
func (m *Model) UpdateFutures(snaps []binance.FuturesSnapshot) {
	m.futures = snaps
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

	// Crypto Futures (Binance)
	if len(m.futures) > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("Crypto Futures", m.width))
		sb.WriteString("\n")
		for _, s := range m.futures {
			fundingStyle := theme.StyleUp
			if s.FundingRate < 0 {
				fundingStyle = theme.StyleDown
			}
			pct := s.FundingRate * 100
			sb.WriteString(fmt.Sprintf("    %-10s %12s   %s   %s\n",
				theme.StyleDim.Render(s.Symbol),
				styleMacroVal.Render(format.FormatPrice(s.MarkPrice)),
				fundingStyle.Render(fmt.Sprintf("funding %+.4f%%", pct)),
				theme.StyleDim.Render("OI "+format.FormatVolume(s.OpenInterest)),
			))
		}
	}

	// DeFi TVL (top 8 chains)
	if len(m.defi) > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("DeFi TVL (top 8 chains)", m.width))
		sb.WriteString("\n")
		max := 8
		if max > len(m.defi) {
			max = len(m.defi)
		}
		for _, c := range m.defi[:max] {
			oneDay := theme.StyleUp
			if c.Change1d < 0 {
				oneDay = theme.StyleDown
			}
			sevenDay := theme.StyleUp
			if c.Change7d < 0 {
				sevenDay = theme.StyleDown
			}
			sb.WriteString(fmt.Sprintf("    %-18s %12s   %s   %s\n",
				theme.StyleDim.Render(c.Chain),
				styleMacroVal.Render("$"+format.FormatVolume(c.TVL)),
				oneDay.Render(fmt.Sprintf("1d %+.2f%%", c.Change1d)),
				sevenDay.Render(fmt.Sprintf("7d %+.2f%%", c.Change7d)),
			))
		}
	}

	return sb.String()
}
