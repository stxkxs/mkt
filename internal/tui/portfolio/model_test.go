package portfolio

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

func mixedModel() Model {
	m := New([]portfolio.Portfolio{{
		Name: "Mixed",
		Holdings: []portfolio.Holding{
			{Symbol: "AAA", Name: "Alpha", Quantity: 10, CostBasis: 100},
			{Symbol: "BBB", Name: "Beta", Quantity: 5, CostBasis: 200},
			{Symbol: "CCC", Name: "Gamma", Quantity: 2, CostBasis: 400},
		},
	}})
	m.SetSize(140, 30)
	// Only AAA is quoted; BBB and CCC are outside the watchlist.
	m.UpdateQuote(provider.Quote{Symbol: "AAA", Price: 120})
	return m
}

// An unpriced holding used to render as a break-even row at cost basis
// and fold into the total as if it were real.
func TestUnpricedRowsAreMarked(t *testing.T) {
	out := plain(mixedModel().View())
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "BBB") || strings.Contains(line, "CCC") {
			if !strings.Contains(line, "not quoted") {
				t.Errorf("unpriced row not marked:\n%s", line)
			}
			if !strings.Contains(line, "—") {
				t.Errorf("unpriced row shows a fabricated price:\n%s", line)
			}
		}
		if strings.Contains(line, "AAA ") && strings.Contains(line, "not quoted") {
			t.Errorf("priced row marked as unquoted:\n%s", line)
		}
	}
}

func TestCoverageFooter(t *testing.T) {
	out := plain(mixedModel().View())
	if !strings.Contains(out, "2 of 3 holdings not quoted") {
		t.Errorf("coverage line missing:\n%s", out)
	}
	if !strings.Contains(out, "BBB, CCC") {
		t.Errorf("unpriced symbols not named:\n%s", out)
	}
	// Priced cost 1000; unpriced 5*200 + 2*400 = 1800, so 1000/2800.
	if !strings.Contains(out, "36% of cost basis") {
		t.Errorf("coverage percentage missing or wrong:\n%s", out)
	}
}

func TestCoverageFooterAbsentWhenFullyPriced(t *testing.T) {
	m := mixedModel()
	m.UpdateQuote(provider.Quote{Symbol: "BBB", Price: 210})
	m.UpdateQuote(provider.Quote{Symbol: "CCC", Price: 390})
	if out := plain(m.View()); strings.Contains(out, "not quoted") {
		t.Errorf("coverage line shown for a fully priced portfolio:\n%s", out)
	}
}

func withEquity(m Model, n int) Model {
	base := time.Now().Add(-time.Duration(n) * 5 * time.Minute)
	for i := range n {
		// A wobbling curve so Sharpe/Sortino/MaxDD are all defined.
		v := 10000 + float64(i)*40 + float64((i%3)-1)*250
		m.AppendEquityMark(portfolio.EquityMark{
			PortfolioName: "Mixed",
			Time:          base.Add(time.Duration(i) * 5 * time.Minute),
			Value:         v,
		})
	}
	return m
}

// Sharpe / Sortino / Beta were implemented and tested but had no caller.
func TestRiskMetricsAreSurfaced(t *testing.T) {
	m := withEquity(mixedModel(), 20)
	out := plain(m.View())
	for _, want := range []string{"Sharpe", "Sortino", "Vol", "MaxDD", "Beta(SPY)", "20 marks"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from the metrics row:\n%s", want, out)
		}
	}
	// No SPY quotes, so Beta has nothing to compute from and must say so
	// rather than print a zero.
	if !strings.Contains(out, "Beta(SPY) —") {
		t.Errorf("Beta without a benchmark should render an em dash:\n%s", out)
	}
}

func TestMetricsRowAbsentWithoutHistory(t *testing.T) {
	if out := plain(mixedModel().View()); strings.Contains(out, "Sharpe") {
		t.Errorf("metrics row shown with no equity history:\n%s", out)
	}
}

func TestBetaUsesBenchmarkSampledWithEachMark(t *testing.T) {
	m := mixedModel()
	base := time.Now().Add(-time.Hour)
	// The portfolio moves twice as hard as the benchmark every period,
	// so beta is exactly 2. The moves have to vary — a constant return
	// series has zero variance and no defined beta.
	moves := []float64{0, 0.01, -0.005, 0.02, 0, -0.01, 0.015, -0.02, 0.008, 0.012, -0.003, 0.005}
	bench, equity := 100.0, 10000.0
	for i, r := range moves {
		bench *= 1 + r
		equity *= 1 + 2*r
		m.UpdateQuote(provider.Quote{Symbol: "SPY", Price: bench})
		m.AppendEquityMark(portfolio.EquityMark{
			PortfolioName: "Mixed",
			Time:          base.Add(time.Duration(i) * 5 * time.Minute),
			Value:         equity,
		})
	}
	got := m.betaOf("Mixed")
	if math.IsNaN(got) {
		t.Fatal("beta is NaN with a fully sampled benchmark")
	}
	if got < 1.8 || got > 2.2 {
		t.Errorf("beta = %v, want ~2", got)
	}
	if !strings.Contains(plain(m.View()), "Beta(SPY) 2.") {
		t.Errorf("beta not rendered:\n%s", plain(m.View()))
	}
}

func TestSetBenchmarkDisablesBeta(t *testing.T) {
	m := mixedModel()
	m.SetBenchmark("")
	m = withEquity(m, 6)
	out := plain(m.View())
	if !strings.Contains(out, "Beta —") {
		t.Errorf("disabled benchmark not reflected:\n%s", out)
	}
	if strings.Contains(out, "Beta(") {
		t.Errorf("benchmark named after being disabled:\n%s", out)
	}
}

func bigPortfolio(n int) Model {
	holdings := make([]portfolio.Holding, n)
	for i := range holdings {
		holdings[i] = portfolio.Holding{
			Symbol:    fmt.Sprintf("S%03d", i),
			Name:      fmt.Sprintf("Holding %d", i),
			Quantity:  float64(i + 1),
			CostBasis: 100,
		}
	}
	m := New([]portfolio.Portfolio{{Name: "Big", Holdings: holdings}})
	for _, h := range holdings {
		m.UpdateQuote(provider.Quote{Symbol: h.Symbol, Price: 110})
	}
	return m
}

func TestTableIsWindowed(t *testing.T) {
	m := bigPortfolio(80)
	m.SetSize(140, 20)
	if lines := strings.Count(m.View(), "\n"); lines > 20 {
		t.Errorf("view is %d lines tall in a 20-line frame", lines)
	}
	m = press(m, "G")
	for range 79 {
		m = press(m, "j")
	}
	out := plain(m.View())
	if !strings.Contains(out, "S079") {
		t.Error("cursor row not visible after scrolling to the end")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	keys := []string{"j", "k", "[", "]", "g", "G", "esc"}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range keys {
				m := withEquity(mixedModel(), 6)
				m.SetSize(w, h)
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseClickMsg{X: 1, Y: h})
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
			empty := New(nil)
			empty.SetSize(w, h)
			empty = press(empty, "j", "]")
			_ = empty.View()

			noHoldings := New([]portfolio.Portfolio{{Name: "Empty"}})
			noHoldings.SetSize(w, h)
			noHoldings = press(noHoldings, "j")
			_ = noHoldings.View()
		}
	}
}

// sweepPortfolio is a portfolio shaped to stress every cell budget at
// once: eight-cell Coinbase pairs, a name longer than its column, a name
// of double-width glyphs, figures that outgrow their columns in both
// directions, a negative P&L, an unquoted holding, and a transaction log
// carrying both realized P&L and dividends. A second portfolio puts the
// switch hint on the title line.
func sweepPortfolio() Model {
	day := time.Now().Add(-24 * time.Hour)
	m := New([]portfolio.Portfolio{
		{
			Name:      "Crypto Majors and Long Equity Sleeve",
			TaxMethod: portfolio.TaxFIFO,
			Holdings: []portfolio.Holding{
				{Symbol: "LINK-USD", Name: "Chainlink", Quantity: 100, CostBasis: 15},
				{Symbol: "AVAX-USD", Name: "Avalanche", Quantity: 50, CostBasis: 35},
				{Symbol: "BRK-B", Name: "Berkshire Hathaway Inc. Class B Common Stock", Quantity: 1234.56789, CostBasis: 412345.67},
				{Symbol: "7203.T", Name: "トヨタ自動車株式会社", Quantity: 9876543.21, CostBasis: 0.00000123},
				{Symbol: "NOQUOTE", Name: "Unquoted Holding", Quantity: 3, CostBasis: 10},
			},
			Transactions: []portfolio.Transaction{
				{Type: portfolio.TxBuy, Symbol: "LINK-USD", Quantity: 100, Price: 15, Time: day},
				{Type: portfolio.TxSell, Symbol: "LINK-USD", Quantity: 40, Price: 9, Time: day.Add(time.Hour)},
				{Type: portfolio.TxDividend, Symbol: "BRK-B", Quantity: 1, Price: 250.50, Time: day.Add(2 * time.Hour)},
			},
		},
		{Name: "Second"},
	})
	m.UpdateQuote(provider.Quote{Symbol: "LINK-USD", Price: 9.1234})
	m.UpdateQuote(provider.Quote{Symbol: "AVAX-USD", Price: 41.5})
	m.UpdateQuote(provider.Quote{Symbol: "BRK-B", Price: 987654.321})
	m.UpdateQuote(provider.Quote{Symbol: "7203.T", Price: 0.00000987})
	// Marks under the active portfolio's own name, so the sparkline and
	// the metrics footer both render.
	base := time.Now().Add(-40 * 5 * time.Minute)
	for i := range 40 {
		m.AppendEquityMark(portfolio.EquityMark{
			PortfolioName: m.portfolios[0].Name,
			Time:          base.Add(time.Duration(i) * 5 * time.Minute),
			Value:         1e6 + float64(i)*4000 + float64((i%3)-1)*25000,
		})
	}
	return m
}

// sweepModels covers each branch of View: the table, both empty states,
// and the footer blocks that only appear with history or transactions.
func sweepModels() map[string]func() Model {
	return map[string]func() Model{
		"stress":        sweepPortfolio,
		"mixed":         func() Model { return withEquity(mixedModel(), 6) },
		"no portfolios": func() Model { return New(nil) },
		"no holdings":   func() Model { return New([]portfolio.Portfolio{{Name: "Empty"}}) },
	}
}

// TestEveryLineFitsItsFrame is the gate on the column plan: the panel
// word-wraps a line it cannot fit, and a row spilling onto a second
// screen line pushes the last holding out of the panel and offsets every
// click from the row it lands on.
func TestEveryLineFitsItsFrame(t *testing.T) {
	for name, build := range sweepModels() {
		for w := 1; w <= 200; w++ {
			for _, h := range []int{6, 30} {
				m := build()
				m.SetSize(w, h)
				m = press(m, "j")
				for i, line := range strings.Split(m.View(), "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Fatalf("%s: frame %dx%d: line %d measures %d cells\n%q",
							name, w, h, i+1, got, plain(line))
					}
				}
			}
		}
	}
}

// tableMinWidth is the narrowest frame that still renders a table: the
// cursor gutter plus the symbol column squeezed to its floor.
const tableMinWidth = tableGutter + 6

func TestTooNarrowFrameDropsTheTable(t *testing.T) {
	for w := 1; w < tableMinWidth; w++ {
		m := sweepPortfolio()
		m.SetSize(w, 30)
		out := plain(m.View())
		if strings.Contains(out, "SYMBOL") {
			t.Errorf("width %d renders a table header that cannot fit:\n%s", w, out)
		}
		// The notice is chrome like any other line, so it is budgeted
		// against the frame rather than allowed to wrap.
		if want := format.Truncate("  Terminal too small for holdings", w); !strings.Contains(out, want) {
			t.Errorf("width %d renders neither a table nor the too-narrow line:\n%s", w, out)
		}
	}
	m := sweepPortfolio()
	m.SetSize(tableMinWidth, 30)
	if out := plain(m.View()); !strings.Contains(out, "SYMBOL") {
		t.Errorf("width %d should still render the symbol column:\n%s", tableMinWidth, out)
	}
}

// Coinbase pairs are eight cells wide, and a ticker with a cell cut off
// names a different instrument.
func TestEightCellSymbolsRenderWhole(t *testing.T) {
	m := sweepPortfolio()
	m.SetSize(140, 30)
	out := plain(m.View())
	for _, sym := range []string{"LINK-USD", "AVAX-USD"} {
		if !strings.Contains(out, sym) {
			t.Errorf("%s truncated in a wide frame:\n%s", sym, out)
		}
	}
}

// hostilePortfolio is the data a config file can hold once a person is
// free to type into it: a symbol longer than any exchange emits, a name
// in prose that is not ASCII, figures that outrun float64's decimal
// precision, and a holding with neither symbol nor name.
func hostilePortfolio() portfolio.Portfolio {
	return portfolio.Portfolio{
		Name:      "1️⃣ 日本電信電話 — a portfolio name with no end in sight",
		TaxMethod: portfolio.TaxFIFO,
		Holdings: []portfolio.Holding{
			{Symbol: "BERKSHIRE-HATHAWAY-CLASS-A-USD", Name: "Berkshire Hathaway Inc. Class A", Quantity: 1, CostBasis: 628000},
			{Symbol: "", Name: "", Quantity: 0, CostBasis: 0},
			{Symbol: "日本電信", Name: "日本電信電話株式会社", Quantity: 123456789012345, CostBasis: 987654321098765},
			{Symbol: "\U0001f680\U0001f680\U0001f680", Name: "⚠️ Rocket Corp \U0001f315", Quantity: -1234.56789, CostBasis: -9999.99},
			{Symbol: "AAA", Name: "Alpha", Quantity: 10, CostBasis: 100},
			{Symbol: "LINK-USD", Name: "Chainlink", Quantity: 999999999, CostBasis: 12.345678},
		},
		Transactions: []portfolio.Transaction{
			{Type: portfolio.TxBuy, Symbol: "AAA", Quantity: 100, Price: 1e12, Time: time.Unix(1700000000, 0)},
			{Type: portfolio.TxSell, Symbol: "AAA", Quantity: 50, Price: 2e12, Time: time.Unix(1700100000, 0)},
			{Type: portfolio.TxDividend, Symbol: "AAA", Quantity: 1e9, Price: 12345.6789, Time: time.Unix(1700200000, 0)},
		},
	}
}

func hostileModel() Model {
	p := hostilePortfolio()
	m := New([]portfolio.Portfolio{p, {Name: "Second", Holdings: []portfolio.Holding{{Symbol: "X", Quantity: 1, CostBasis: 1}}}})
	for sym, price := range map[string]float64{
		"BERKSHIRE-HATHAWAY-CLASS-A-USD": 700123.45,
		"日本電信":                           1.2345678901234e14,
		"\U0001f680\U0001f680\U0001f680": 1e-9,
		"AAA":                            120,
		"LINK-USD":                       25.5,
		"SPY":                            500,
	} {
		m.UpdateQuote(provider.Quote{Symbol: sym, Price: price})
	}
	marks := make([]portfolio.EquityMark, 0, 40)
	for i := range 40 {
		marks = append(marks, portfolio.EquityMark{
			PortfolioName: p.Name,
			Time:          time.Unix(1700000000, 0).Add(time.Duration(i) * time.Hour),
			Value:         1e13 * (1 + 0.37*math.Sin(float64(i))),
		})
	}
	m.LoadEquityHistory(map[string][]portfolio.EquityMark{p.Name: marks})
	return m
}

// The tidy fixture exercises the column plan; this one exercises the
// measurement. A double-width glyph, a grapheme cluster that spans three
// runes and a figure wider than its column each break a different part
// of the budget, and all three reach the table from a config file.
func TestEveryLineFitsItsFrameOnHostileData(t *testing.T) {
	for w := 1; w <= 200; w++ {
		for _, h := range []int{6, 30} {
			m := hostileModel()
			m.SetSize(w, h)
			m = press(m, "j", "j", "j")
			for i, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("frame %dx%d: line %d measures %d cells\n%q",
						w, h, i+1, got, plain(line))
				}
			}
		}
	}
}

// cell's contract is exactness, not a ceiling: a short cell leaves the
// columns to its right unaligned, and a long one carries the overflow
// into the next column rather than off the end of the row, where a
// row-width assertion would find it.
func TestCellIsExactlyItsColumn(t *testing.T) {
	texts := []string{
		"", "A", "AAPL", "BERKSHIRE-HATHAWAY-CLASS-A-USD",
		"日本電信電話株式会社",
		"\U0001f680\U0001f680\U0001f680\U0001f680",
		"1️⃣ keycap", "⚠️ warning", "☂️",
		"\U0001f1fa\U0001f1f8 flag", "é́́ combining",
		"—", "not quoted",
	}
	for _, c := range columnPlan() {
		for _, s := range texts {
			for w := range 40 {
				if got := cell(c.Key, s, w); lipgloss.Width(got) != w {
					t.Fatalf("cell(%q, %q, %d) measures %d cells, want %d: %q",
						c.Key, s, w, lipgloss.Width(got), w, got)
				}
			}
		}
	}
}

// A figure wider than its column would be truncated by cell, and a
// number with its tail cut off reads as a smaller number. Every form
// fitNumber and pnlText can reach has to fit on its own.
func TestNumbersNeverExceedTheirColumn(t *testing.T) {
	values := []float64{
		0, 1, -1, 1e-9, -1e-9, 12.345678, 1e6, -1e6, 1e9, 1e12,
		1e15, -1e15, 1e19, -1e20, 1e30, 1e300, 123456789012345,
		math.MaxFloat64, -math.MaxFloat64,
		math.Inf(1), math.Inf(-1), math.NaN(),
	}
	pcts := []float64{0, 12.3, -99.9, 1e12, math.NaN(), math.Inf(1)}
	for _, v := range values {
		for w := 1; w <= 24; w++ {
			for _, d := range []int{0, 2, 4} {
				if s := fitNumber(v, d, w); lipgloss.Width(s) > w {
					t.Errorf("fitNumber(%g, %d, %d) = %q measures %d cells",
						v, d, w, s, lipgloss.Width(s))
				}
			}
			for _, pct := range pcts {
				if s := pnlText(v, pct, w); lipgloss.Width(s) > w {
					t.Errorf("pnlText(%g, %g, %d) = %q measures %d cells",
						v, pct, w, s, lipgloss.Width(s))
				}
			}
		}
	}
}

// A footer segment carries its own color, so the finished line can not
// be truncated after the fact. Each one is budgeted as plain text before
// it is styled.
func TestFooterLineFitsItsFrame(t *testing.T) {
	long := strings.Repeat("日本語", 40)
	cases := [][]seg{
		{{long, styleTotal}},
		{{"short", styleTotal}, {long, styleTotal}},
		{{long, styleTotal}, {long, styleTotal}, {long, styleTotal}},
		{{"\U0001f680\U0001f680\U0001f680\U0001f680", styleTotal}, {"x", styleTotal}},
		{{"", styleTotal}},
		{},
	}
	for w := range 121 {
		for i, segs := range cases {
			got := strings.TrimSuffix(footerLine(w, segs...), "\n")
			if lipgloss.Width(got) > w {
				t.Errorf("footerLine(%d) case %d measures %d cells: %q",
					w, i, lipgloss.Width(got), got)
			}
		}
	}
}

// The click hit-test maps a mouse row straight to a holding index, so it
// is only correct while the rows on screen are the rows the viewport
// arithmetic counted.
func TestRenderedLineCountMatchesTheViewport(t *testing.T) {
	for w := 1; w <= 200; w++ {
		for _, h := range []int{5, 10, 20, 40} {
			m := hostileModel()
			m.SetSize(w, h)
			if m.layout() == nil {
				continue
			}
			p := m.activePortfolio()
			s := portfolio.Evaluate(p.Holdings, m.quotes)
			lines := strings.Split(strings.TrimSuffix(m.View(), "\n"), "\n")
			if want := headerLines + m.visibleRows(p, s) + m.footerLines(p, s); len(lines) != want {
				t.Fatalf("frame %dx%d renders %d lines, the viewport counted %d\n%s",
					w, h, len(lines), want, plain(strings.Join(lines, "\n")))
			}
		}
	}
}

// A wide frame has the room to print a whole figure with its percentage,
// and spending that room on name padding instead is the column plan
// failing at the size it is least constrained.
func TestWideFrameShowsWholePnL(t *testing.T) {
	m := New([]portfolio.Portfolio{{
		Name:     "Core",
		Holdings: []portfolio.Holding{{Symbol: "AAPL", Name: "Apple Inc.", Quantity: 100, CostBasis: 150.25}},
	}})
	m.UpdateQuote(provider.Quote{Symbol: "AAPL", Price: 189.4321})
	for _, w := range []int{80, 100, 120, 160, 200} {
		m.SetSize(w, 30)
		out := plain(m.View())
		if !strings.Contains(out, "+3918.21 (+26.1%)") {
			t.Errorf("width %d compresses a P&L figure the frame has room for:\n%s", w, out)
		}
	}
}
