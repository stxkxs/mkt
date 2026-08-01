package portfolio

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleTotal   = lipgloss.NewStyle().Foreground(theme.ColorYellow).Bold(true)
	styleLabel   = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleUnknown = lipgloss.NewStyle().Foreground(theme.ColorOrange)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleTotal = lipgloss.NewStyle().Foreground(theme.ColorYellow).Bold(true)
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleUnknown = lipgloss.NewStyle().Foreground(theme.ColorOrange)
}

// DefaultBenchmark is the symbol sampled alongside each equity mark to
// compute Beta. It only produces a series when the symbol is in the
// watchlist — that is what the tab subscribes quotes through.
const DefaultBenchmark = "SPY"

// betaSample pairs one equity mark with the benchmark price at the same
// instant. Beta needs two return series sampled on the same clock, and
// this is the only place both numbers are known at once.
type betaSample struct {
	equity float64
	bench  float64
}

// Model is the portfolio view.
type Model struct {
	portfolios []portfolio.Portfolio
	activeIdx  int
	quotes     map[string]provider.Quote
	cursor     int
	width      int
	height     int

	// Equity history per portfolio name, populated by dashboard at
	// startup and appended to on EquityMarkMsg.
	equity map[string][]portfolio.EquityMark

	// Benchmark price sampled alongside each equity mark, per portfolio.
	// Only this session's marks have one — a persisted curve carries no
	// benchmark — so Beta is reported over however many pairs exist.
	beta      map[string][]betaSample
	benchmark string
}

// New creates a portfolio model.
func New(portfolios []portfolio.Portfolio) Model {
	return Model{
		portfolios: portfolios,
		quotes:     make(map[string]provider.Quote),
		equity:     make(map[string][]portfolio.EquityMark),
		beta:       make(map[string][]betaSample),
		benchmark:  DefaultBenchmark,
	}
}

// SetBenchmark selects the symbol whose price is sampled next to each
// equity mark for Beta. Pass "" to drop the Beta readout entirely.
func (m *Model) SetBenchmark(sym string) {
	m.benchmark = sym
}

// LoadEquityHistory seeds the model with previously persisted marks.
// Should be called before the program runs.
func (m *Model) LoadEquityHistory(byName map[string][]portfolio.EquityMark) {
	if m.equity == nil {
		m.equity = make(map[string][]portfolio.EquityMark)
	}
	for k, v := range byName {
		m.equity[k] = v
	}
}

// AppendEquityMark records a new mark for its portfolio and, when the
// benchmark has a live quote, snapshots its price alongside so Beta has
// a series sampled on the same clock as the equity curve. Marks taken
// while the benchmark is unquoted are simply not paired, rather than
// padded with a guess.
func (m *Model) AppendEquityMark(mark portfolio.EquityMark) {
	if m.equity == nil {
		m.equity = make(map[string][]portfolio.EquityMark)
	}
	m.equity[mark.PortfolioName] = append(m.equity[mark.PortfolioName], mark)

	if m.benchmark == "" {
		return
	}
	q, ok := m.quotes[m.benchmark]
	if !ok || q.Price <= 0 {
		return
	}
	if m.beta == nil {
		m.beta = make(map[string][]betaSample)
	}
	m.beta[mark.PortfolioName] = append(m.beta[mark.PortfolioName],
		betaSample{equity: mark.Value, bench: q.Price})
}

// betaOf computes the portfolio's beta against the benchmark over the
// paired samples collected this session. NaN when there are too few.
func (m Model) betaOf(name string) float64 {
	pairs := m.beta[name]
	if len(pairs) < 3 {
		return math.NaN()
	}
	eq := make([]float64, len(pairs))
	bm := make([]float64, len(pairs))
	for i, p := range pairs {
		eq[i], bm[i] = p.equity, p.bench
	}
	return portfolio.Beta(portfolio.Returns(eq), portfolio.Returns(bm))
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
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
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
	case tea.MouseWheelMsg:
		holdings := m.activePortfolio().Holdings
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(holdings)-1 {
				m.cursor++
			}
		}
	case tea.MouseClickMsg:
		p := m.activePortfolio()
		if len(p.Holdings) == 0 {
			return m, nil
		}
		row := msg.Y - headerLines
		if row < 0 {
			return m, nil
		}
		summary := portfolio.Evaluate(p.Holdings, m.quotes)
		idx := m.viewportStart(p, summary) + row
		if idx >= 0 && idx < len(summary.Positions) {
			m.cursor = idx
		}
	}
	return m, nil
}

// headerLines is the portfolio name row, the column header and the
// separator above the first holding.
const headerLines = 3

// footerLines counts the rows of the totals block under the table. It is
// state-dependent — coverage, metrics, realized P&L and dividends each
// appear only when they have something to say — and the row budget has
// to match it exactly or the last holdings scroll off the bottom.
func (m Model) footerLines(p portfolio.Portfolio, s portfolio.Summary) int {
	n := 2 // blank separator + totals row
	if !s.FullyPriced() {
		n++
	}
	if len(m.equity[p.Name]) >= 2 {
		n++
	}
	if len(p.Transactions) > 0 {
		n++
		if portfolio.Dividends(p.Transactions) > 0 {
			n++
		}
	}
	return n
}

// visibleRows is how many holdings fit between the header and the
// totals block.
func (m Model) visibleRows(p portfolio.Portfolio, s portfolio.Summary) int {
	return format.VisibleRows(m.height, headerLines+m.footerLines(p, s), len(s.Positions))
}

// viewportStart returns the first visible holding index. Shared by View
// and the click handler so the two agree on what is on screen.
func (m Model) viewportStart(p portfolio.Portfolio, s portfolio.Summary) int {
	return format.ViewportStart(m.cursor, len(s.Positions), m.visibleRows(p, s))
}

// View renders the portfolio.
func (m Model) View() string {
	if m.width <= 0 {
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

	// Equity curve sparkline (the numeric risk stats live in the footer)
	curveHint := ""
	if marks := m.equity[p.Name]; len(marks) >= 2 {
		curveHint = "  " + theme.StyleDim.Render(sparkline(portfolio.MarkValues(marks), 24))
	}
	sb.WriteString(styleLabel.Render(fmt.Sprintf("  %s", p.Name)) + curveHint + navHint + "\n")

	if len(p.Holdings) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No holdings in this portfolio.\n"))
		return sb.String()
	}

	// Header
	header := fmt.Sprintf("  %-6s %-22s %10s %10s %12s %12s %10s",
		"SYMBOL", "NAME", "QTY", "COST", "PRICE", "VALUE", "P&L")
	sb.WriteString(theme.StyleHeader.Render(header))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(format.Repeat("─", m.width)))
	sb.WriteString("\n")

	summary := portfolio.Evaluate(p.Holdings, m.quotes)

	startIdx := m.viewportStart(p, summary)
	endIdx := startIdx + m.visibleRows(p, summary)
	if endIdx > len(summary.Positions) {
		endIdx = len(summary.Positions)
	}

	for i := startIdx; i < endIdx; i++ {
		m.renderPosition(&sb, summary.Positions[i], i == m.cursor)
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

	// Coverage: the totals above cover priced holdings only, so say so
	// rather than letting a fabricated break-even row pass for a real
	// position folded into the total.
	if !summary.FullyPriced() {
		sb.WriteString(fmt.Sprintf("  %s\n", styleUnknown.Render(fmt.Sprintf(
			"%d of %d holdings not quoted (%s) — totals cover %.0f%% of cost basis",
			len(summary.Unpriced), len(summary.Positions),
			format.Truncate(strings.Join(summary.Unpriced, ", "), 40),
			summary.Coverage()*100,
		))))
	}

	// Risk metrics over the recorded equity curve.
	if marks := m.equity[p.Name]; len(marks) >= 2 {
		st := portfolio.StatsFromMarks(marks, 0)
		sb.WriteString(fmt.Sprintf("  %s\n", theme.StyleDim.Render(fmt.Sprintf(
			"Sharpe %s   Sortino %s   Vol %s   MaxDD %.2f%%   %s   (%d marks)",
			ratio(st.Sharpe), ratio(st.Sortino), pct(st.Volatility*100),
			st.MaxDrawdown*100, m.betaLabel(p.Name), st.Marks,
		))))
	}

	if len(p.Transactions) > 0 {
		realized := portfolio.RealizedByMethod(p.Transactions, p.TaxMethod)
		realizedStyle := theme.StyleUp
		realizedSign := "+"
		if realized < 0 {
			realizedStyle = theme.StyleDown
			realizedSign = ""
		}
		label := "Realized"
		if p.TaxMethod != portfolio.TaxAverage {
			label = fmt.Sprintf("Realized (%s)", strings.ToUpper(string(p.TaxMethod)))
		}
		sb.WriteString(fmt.Sprintf("  %s\n",
			realizedStyle.Bold(true).Render(fmt.Sprintf("%s: %s$%.2f", label, realizedSign, realized)),
		))

		divTotal := portfolio.Dividends(p.Transactions)
		if divTotal > 0 {
			ytd := portfolio.DividendsYTD(p.Transactions, time.Now())
			sb.WriteString(fmt.Sprintf("  %s\n",
				theme.StyleUp.Bold(true).Render(fmt.Sprintf("Dividends: $%.2f  (YTD: $%.2f)", divTotal, ytd)),
			))
		}
	}

	return sb.String()
}

// renderPosition writes one holding row. An unpriced holding shows a
// dash for price and value and is marked "not quoted": its P&L is zero
// only because there was no quote, and rendering that as a break-even
// row is indistinguishable from a position that really has not moved.
func (m Model) renderPosition(sb *strings.Builder, pos portfolio.Position, selected bool) {
	cursor := "  "
	if selected {
		cursor = theme.StyleCursorGutter.Render("▎") + " "
	}

	name := format.Truncate(pos.Name, 22)
	head := fmt.Sprintf("%s%s %s %s %s",
		cursor,
		theme.StyleSymbol.Render(fmt.Sprintf("%-6s", pos.Symbol)),
		theme.StyleDim.Render(fmt.Sprintf("%-22s", name)),
		theme.StyleVal.Render(fmt.Sprintf("%10.4f", pos.Quantity)),
		theme.StyleVal.Render(fmt.Sprintf("%10.2f", pos.CostBasis)),
	)

	if !pos.Priced {
		sb.WriteString(fmt.Sprintf("%s %s %s %s\n", head,
			theme.StyleNeutral.Render(fmt.Sprintf("%12s", "—")),
			theme.StyleNeutral.Render(fmt.Sprintf("%12s", "—")),
			styleUnknown.Render("not quoted"),
		))
		return
	}

	pnlStyle := theme.StyleUp
	sign := "+"
	if pos.PnL < 0 {
		pnlStyle = theme.StyleDown
		sign = ""
	}
	sb.WriteString(fmt.Sprintf("%s %s %s %s\n", head,
		theme.StyleVal.Render(fmt.Sprintf("%12.2f", pos.CurrentPrice)),
		theme.StyleVal.Render(fmt.Sprintf("%12.2f", pos.MarketValue)),
		pnlStyle.Render(fmt.Sprintf("%s%.2f (%s%.1f%%)", sign, pos.PnL, sign, pos.PnLPct)),
	))
}

// betaLabel renders the Beta readout, naming the benchmark so an
// unavailable one is self-explanatory.
func (m Model) betaLabel(name string) string {
	if m.benchmark == "" {
		return "Beta —"
	}
	return fmt.Sprintf("Beta(%s) %s", m.benchmark, ratio(m.betaOf(name)))
}

// ratio formats a risk ratio, showing an em dash for the undefined case
// so "no downside observed yet" never reads as zero.
func ratio(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.2f", v)
}

// pct formats a percentage the same way.
func pct(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", v)
}
