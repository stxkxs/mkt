package portfolio

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
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
