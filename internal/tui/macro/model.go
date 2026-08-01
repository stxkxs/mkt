package macro

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/binance"
	"github.com/stxkxs/mkt/internal/provider/calendar"
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
	quotes   map[string]provider.Quote
	defi     []defillama.TVLSnapshot
	futures  []binance.FuturesSnapshot
	upcoming []calendar.Event
	width    int
	height   int
	scroll   int // first visible content row
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

// UpdateEvents replaces the upcoming-events list.
func (m *Model) UpdateEvents(events []calendar.Event) {
	m.upcoming = events
}

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleMacroVal = lipgloss.NewStyle().Foreground(theme.ColorFg)
}

// Update handles messages. The macro dashboard is read-only, but it
// stacks six sections that routinely run longer than the frame, so it
// scrolls: without an Update the rows past the fold were unreachable.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
	case tea.KeyPressMsg:
		// Rendering the body is the expensive part, so measure it once
		// per key rather than once per clamp.
		total := len(m.contentLines())
		switch msg.String() {
		case "j", "down":
			m.scroll++
		case "k", "up":
			m.scroll--
		case "g", "home":
			m.scroll = 0
		case "G", "end":
			m.scroll = total
		case "pgdown":
			m.scroll += m.visibleRows()
		case "pgup":
			m.scroll -= m.visibleRows()
		}
		m.scroll = clamp(m.scroll, total-m.visibleRows())
	case tea.MouseWheelMsg:
		total := len(m.contentLines())
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll = clamp(m.scroll-3, total-m.visibleRows())
		case tea.MouseWheelDown:
			m.scroll = clamp(m.scroll+3, total-m.visibleRows())
		}
	}
	return m, nil
}

// visibleRows is the content height, minus the tab's own section header
// and the blank row under it.
func (m Model) visibleRows() int {
	n := m.height - macroHeaderLines
	if n < 1 {
		n = 1
	}
	return n
}

// clampScroll keeps the offset inside [0, len(lines)-visible] so the last
// screenful always stays full.
func (m Model) clampScroll(v int) int {
	return clamp(v, len(m.contentLines())-m.visibleRows())
}

// clamp bounds v to [0, maxScroll], treating a negative maximum as zero.
func clamp(v, maxScroll int) int {
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v > maxScroll {
		v = maxScroll
	}
	if v < 0 {
		v = 0
	}
	return v
}

// macroHeaderLines is the "Macro Dashboard" header row plus the blank
// row beneath it, both of which stay pinned while the body scrolls.
const macroHeaderLines = 2

// View renders the macro dashboard: a pinned header over a scrolling
// body.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	lines := m.contentLines()
	visible := m.visibleRows()
	start := m.clampScroll(m.scroll)
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}

	hint := ""
	if len(lines) > visible {
		hint = fmt.Sprintf("j/k:scroll  %d-%d of %d", start+1, end, len(lines))
	}

	var sb strings.Builder
	sb.WriteString(theme.SectionHeaderHint("Macro Dashboard", hint, m.width))
	sb.WriteString("\n\n")
	sb.WriteString(strings.Join(lines[start:end], "\n"))
	return sb.String()
}

// contentLines renders the scrollable body as individual rows so View
// can window it. Every row is one terminal line.
func (m Model) contentLines() []string {
	if len(m.quotes) == 0 {
		return []string{theme.StyleDim.Render("  Loading macro data...")}
	}

	var sb strings.Builder
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

	// Yield-curve spread. ^IRX is the 13-week bill, not the 2-year note,
	// so ^TNX - ^IRX is the 10Y-3M spread — a different indicator from
	// the 2s10s, and the one this actually computes.
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
			theme.StyleDim.Render("10Y-3M Spread"),
			spreadStyle.Render(fmt.Sprintf("%.3f%%", spread)),
		))
	}

	// Crypto Futures (Binance)
	if len(m.futures) > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("Crypto Futures", m.width))
		sb.WriteString("\n")
		for _, s := range m.futures {
			// A snapshot with nothing in it means Binance refused or was
			// unreachable. Rendering it as "0.00 funding +0.0000% OI 0" would
			// be a fabricated flat market — say unavailable instead. Binance
			// answers 451 to US hosts, so this is the common case there.
			if s.Unavailable() {
				reason := "unavailable"
				if s.Restricted() {
					reason = "unavailable in this region"
				}
				sb.WriteString(fmt.Sprintf("    %-10s %s\n",
					theme.StyleDim.Render(s.Symbol),
					theme.StyleDim.Render("— "+reason),
				))
				continue
			}
			markStr, fundingStr, oiStr := "—", "funding —", "OI —"
			if s.HavePremium {
				markStr = format.FormatPrice(s.MarkPrice)
				fundingStr = fmt.Sprintf("funding %+.4f%%", s.FundingRate*100)
			}
			if s.HaveOI {
				oiStr = "OI " + format.FormatVolume(s.OpenInterest)
			}
			fundingStyle := theme.StyleDim
			if s.HavePremium {
				fundingStyle = theme.StyleUp
				if s.FundingRate < 0 {
					fundingStyle = theme.StyleDown
				}
			}
			sb.WriteString(fmt.Sprintf("    %-10s %12s   %s   %s\n",
				theme.StyleDim.Render(s.Symbol),
				styleMacroVal.Render(markStr),
				fundingStyle.Render(fundingStr),
				theme.StyleDim.Render(oiStr),
			))
		}
	}

	// Upcoming economic events (next 30 days)
	if len(m.upcoming) > 0 {
		sb.WriteString("\n")
		sb.WriteString(theme.SectionHeader("Upcoming Economic Events (30d)", m.width))
		sb.WriteString("\n")
		max := 8
		if max > len(m.upcoming) {
			max = len(m.upcoming)
		}
		now := time.Now().UTC()
		for _, e := range m.upcoming[:max] {
			delta := e.Time.Sub(now)
			when := fmt.Sprintf("in %dd", int(delta.Hours()/24))
			if delta < 24*time.Hour {
				when = fmt.Sprintf("in %dh", int(delta.Hours()))
			}
			sb.WriteString(fmt.Sprintf("    %-30s %s   %s\n",
				styleMacroVal.Render(e.Title),
				theme.StyleDim.Render(e.Time.Local().Format("Jan 02 15:04")),
				theme.StyleDim.Render(when),
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

	return strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
}
