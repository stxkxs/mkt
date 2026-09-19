package portfolio

import (
	"fmt"
	"math"
	"strconv"
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
		// A frame too narrow for the table draws no rows, so a click
		// there names nothing: the row arithmetic below would map it to
		// a holding that is not on screen.
		if len(p.Holdings) == 0 || m.layout() == nil {
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

// tableGutter is the cursor column every row of the table draws before
// its first data column.
const tableGutter = 2

// columnPlan is the holdings table's geometry and the only place it is
// written down: the header, both row shapes and the too-narrow check all
// read one fit of this plan, so a column can not be one width in the
// header and another in the rows it labels.
//
// symbol holds eight cells because a Coinbase pair is eight (LINK-USD),
// and a ticker with a cell cut off names a different instrument. name
// carries the table's only prose, so it both absorbs a wide frame's
// slack and is the first column a narrow frame sheds.
//
// pnl wants the cells a whole figure takes with its percentage attached
// (-1234567.89 (-1234.5%)). A narrower want is not a narrower cell: name
// is the only Flex column, so every cell a wide frame does not give pnl
// is spent padding prose, while the P&L cell compresses a figure it had
// the room to print.
func columnPlan() []format.Col {
	return []format.Col{
		{Key: "symbol", Width: 8, Min: 6, Prio: 7},
		{Key: "name", Width: 22, Min: 8, Prio: 1, Flex: true},
		{Key: "qty", Width: 10, Min: 8, Prio: 3},
		{Key: "cost", Width: 10, Min: 8, Prio: 2},
		{Key: "price", Width: 12, Min: 8, Prio: 4},
		{Key: "value", Width: 12, Min: 10, Prio: 5},
		{Key: "pnl", Width: 22, Min: 10, Prio: 6},
	}
}

// headerLabels names each column of columnPlan. A label is budgeted
// against its column like any other cell.
var headerLabels = map[string]string{
	"symbol": "SYMBOL",
	"name":   "NAME",
	"qty":    "QTY",
	"cost":   "COST",
	"price":  "PRICE",
	"value":  "VALUE",
	"pnl":    "P&L",
}

// layout fits the plan to the frame. A nil layout means not even the
// symbol column fits, which is the signal to print the too-narrow line
// instead of a partial table.
func (m Model) layout() format.Layout {
	return format.Fit(columnPlan(), m.width, tableGutter)
}

// cell renders text into exactly w display cells: truncated to the
// column first, so a value can never be wider than the column it sits
// in, then padded. Numeric columns are flushed right so digits line up
// down the table; symbol and name are flushed left.
//
// The padding is measured with lipgloss.Width and applied before the
// caller styles the cell. fmt's width verbs count runes, so they
// under-budget a wide glyph and count an ANSI escape as content.
func cell(key, text string, w int) string {
	text = format.Truncate(text, w)
	pad := format.Spaces(w - lipgloss.Width(text))
	switch key {
	case "symbol", "name":
		return text + pad
	default:
		return pad + text
	}
}

// compactUnits scale a figure too large for its column, largest first.
var compactUnits = []struct {
	suffix string
	div    float64
}{{"T", 1e12}, {"B", 1e9}, {"M", 1e6}, {"K", 1e3}}

// fitNumber renders v with the most precision that fits w cells: the
// decimals asked for, then fewer, then a scaled suffix, then an
// exponent. A number is a value rather than prose — an ellipsis in a
// price reads as a different price — so a cramped cell spends precision
// instead of digits, and the result is never wider than w.
func fitNumber(v float64, decimals, w int) string {
	if w <= 0 {
		return ""
	}
	for d := decimals; d >= 0; d-- {
		if s := strconv.FormatFloat(v, 'f', d, 64); lipgloss.Width(s) <= w {
			return s
		}
	}
	abs := math.Abs(v)
	for _, u := range compactUnits {
		if abs < u.div {
			continue
		}
		for d := 1; d >= 0; d-- {
			if s := strconv.FormatFloat(v/u.div, 'f', d, 64) + u.suffix; lipgloss.Width(s) <= w {
				return s
			}
		}
	}
	// Past the largest suffix, an exponent is the only form left that
	// fits a column at all: 1e+30 is five cells where the digits are
	// thirty-one. Handing those digits back would put the caller's
	// truncation in the middle of a figure, and a number with its tail
	// cut off reads as a smaller number rather than as one that did not
	// fit.
	for d := 2; d >= 0; d-- {
		if s := strconv.FormatFloat(v, 'e', d, 64); lipgloss.Width(s) <= w {
			return s
		}
	}
	return format.Truncate(strconv.FormatFloat(v, 'e', 0, 64), w)
}

// pnlText renders unrealized P&L into w cells. The percentage is what
// makes one position's P&L comparable with another's, so the dollar
// figure spends its precision before the percentage is dropped.
func pnlText(pnl, pct float64, w int) string {
	sign := "+"
	if pnl < 0 {
		sign = "" // the formatted figure carries its own minus
	}
	tail := fmt.Sprintf(" (%s%.1f%%)", sign, pct)
	if budget := w - lipgloss.Width(tail) - lipgloss.Width(sign); budget >= 3 {
		if head := fitNumber(pnl, 2, budget); head != "" && lipgloss.Width(sign+head+tail) <= w {
			return sign + head + tail
		}
	}
	return sign + fitNumber(pnl, 2, w-lipgloss.Width(sign))
}

// seg is one styled run of a footer line.
type seg struct {
	text  string
	style lipgloss.Style
}

// footerLine composes a footer row from styled segments within the
// frame, indented and separated like the table above it. Each segment
// carries its own color, which rules out truncating the finished line —
// the escapes would count as content — so the line is measured as plain
// text and a segment that does not fit whole is dropped along with the
// ones after it, rather than showing half a figure. The first segment is
// truncated instead of dropped, so the line always says something.
func footerLine(width int, segs ...seg) string {
	var b strings.Builder
	used := min(tableGutter, max(width, 0))
	b.WriteString(format.Spaces(used))
	for i, s := range segs {
		gap := 0
		if i > 0 {
			gap = tableGutter
		}
		if used+gap+lipgloss.Width(s.text) > width {
			if i == 0 {
				b.WriteString(s.style.Render(format.Truncate(s.text, width-used)))
			}
			break
		}
		b.WriteString(format.Spaces(gap))
		b.WriteString(s.style.Render(s.text))
		used += gap + lipgloss.Width(s.text)
	}
	b.WriteString("\n")
	return b.String()
}

// View renders the portfolio.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	if len(m.portfolios) == 0 {
		return theme.StyleDim.Render(format.Truncate("  No portfolios configured.", m.width)) + "\n" +
			theme.StyleDim.Render(format.Truncate("  Add portfolios in ~/.config/mkt/config.yaml", m.width))
	}

	p := m.activePortfolio()
	var sb strings.Builder

	// Title. The name always renders; the switch hint and the equity
	// sparkline join it only while the frame holds them whole, the hint
	// first because it names a key binding.
	title := format.Truncate("  "+p.Name, m.width)
	used := lipgloss.Width(title)
	navHint := ""
	if len(m.portfolios) > 1 {
		hint := fmt.Sprintf("  [/]: switch  (%d/%d)", m.activeIdx+1, len(m.portfolios))
		if used+lipgloss.Width(hint) <= m.width {
			navHint = theme.StyleDim.Render(hint)
			used += lipgloss.Width(hint)
		}
	}
	curveHint := ""
	if marks := m.equity[p.Name]; len(marks) >= 2 {
		curve := sparkline(portfolio.MarkValues(marks), 24)
		if used+tableGutter+lipgloss.Width(curve) <= m.width {
			curveHint = format.Spaces(tableGutter) + theme.StyleDim.Render(curve)
		}
	}
	sb.WriteString(styleLabel.Render(title) + curveHint + navHint + "\n")

	if len(p.Holdings) == 0 {
		sb.WriteString(theme.StyleDim.Render(format.Truncate("  No holdings in this portfolio.", m.width)) + "\n")
		return sb.String()
	}

	cols := m.layout()
	if cols == nil {
		sb.WriteString(theme.StyleDim.Render(format.Truncate("  Terminal too small for holdings", m.width)))
		return sb.String()
	}

	// Header
	labels := make([]string, 0, len(cols))
	for _, c := range cols {
		labels = append(labels, cell(c.Key, headerLabels[c.Key], c.Width))
	}
	sb.WriteString(theme.StyleHeader.Render(format.Spaces(tableGutter) + strings.Join(labels, " ")))
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
		m.renderPosition(&sb, cols, summary.Positions[i], i == m.cursor)
	}

	// Total row
	sb.WriteString("\n")
	totalPnlStyle := theme.StyleUp
	totalSign := "+"
	if summary.TotalPnL < 0 {
		totalPnlStyle = theme.StyleDown
		totalSign = ""
	}
	sb.WriteString(footerLine(m.width,
		seg{fmt.Sprintf("Total Cost: $%.2f", summary.TotalCost), styleTotal},
		seg{fmt.Sprintf("Value: $%.2f", summary.TotalValue), styleTotal},
		seg{fmt.Sprintf("P&L: %s$%.2f (%s%.1f%%)",
			totalSign, summary.TotalPnL, totalSign, summary.TotalPnLPct), totalPnlStyle.Bold(true)},
	))

	// Coverage: the totals above cover priced holdings only, so say so
	// rather than letting a fabricated break-even row pass for a real
	// position folded into the total.
	if !summary.FullyPriced() {
		sb.WriteString(footerLine(m.width, seg{fmt.Sprintf(
			"%d of %d holdings not quoted (%s) — totals cover %.0f%% of cost basis",
			len(summary.Unpriced), len(summary.Positions),
			format.Truncate(strings.Join(summary.Unpriced, ", "), 40),
			summary.Coverage()*100,
		), styleUnknown}))
	}

	// Risk metrics over the recorded equity curve.
	if marks := m.equity[p.Name]; len(marks) >= 2 {
		st := portfolio.StatsFromMarks(marks, 0)
		sb.WriteString(footerLine(m.width, seg{fmt.Sprintf(
			"Sharpe %s   Sortino %s   Vol %s   MaxDD %.2f%%   %s   (%d marks)",
			ratio(st.Sharpe), ratio(st.Sortino), pct(st.Volatility*100),
			st.MaxDrawdown*100, m.betaLabel(p.Name), st.Marks,
		), theme.StyleDim}))
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
		sb.WriteString(footerLine(m.width, seg{
			fmt.Sprintf("%s: %s$%.2f", label, realizedSign, realized), realizedStyle.Bold(true),
		}))

		divTotal := portfolio.Dividends(p.Transactions)
		if divTotal > 0 {
			ytd := portfolio.DividendsYTD(p.Transactions, time.Now())
			sb.WriteString(footerLine(m.width, seg{
				fmt.Sprintf("Dividends: $%.2f  (YTD: $%.2f)", divTotal, ytd), theme.StyleUp.Bold(true),
			}))
		}
	}

	return sb.String()
}

// renderPosition writes one holding row. An unpriced holding shows a
// dash for price and value and is marked "not quoted": its P&L is zero
// only because there was no quote, and rendering that as a break-even
// row is indistinguishable from a position that really has not moved.
func (m Model) renderPosition(sb *strings.Builder, cols format.Layout, pos portfolio.Position, selected bool) {
	cursor := format.Spaces(tableGutter)
	if selected {
		cursor = theme.StyleCursorGutter.Render("▎") + " "
	}

	cells := make([]string, 0, len(cols))
	for _, c := range cols {
		var text string
		style := theme.StyleVal
		switch c.Key {
		case "symbol":
			text, style = pos.Symbol, theme.StyleSymbol
		case "name":
			text, style = pos.Name, theme.StyleDim
		case "qty":
			text = fitNumber(pos.Quantity, 4, c.Width)
		case "cost":
			text = fitNumber(pos.CostBasis, 2, c.Width)
		case "price":
			text, style = "—", theme.StyleNeutral
			if pos.Priced {
				text, style = fitNumber(pos.CurrentPrice, 2, c.Width), theme.StyleVal
			}
		case "value":
			text, style = "—", theme.StyleNeutral
			if pos.Priced {
				text, style = fitNumber(pos.MarketValue, 2, c.Width), theme.StyleVal
			}
		case "pnl":
			text, style = "not quoted", styleUnknown
			if pos.Priced {
				text, style = pnlText(pos.PnL, pos.PnLPct, c.Width), theme.StyleUp
				if pos.PnL < 0 {
					style = theme.StyleDown
				}
			}
		}
		cells = append(cells, style.Render(cell(c.Key, text, c.Width)))
	}
	sb.WriteString(cursor + strings.Join(cells, " ") + "\n")
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
