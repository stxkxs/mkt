package portfolio

import (
	"math"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestEvaluate(t *testing.T) {
	holdings := []Holding{
		{Symbol: "BTCUSDT", Quantity: 0.5, CostBasis: 40000},
		{Symbol: "ETHUSDT", Quantity: 10, CostBasis: 2000},
	}

	quotes := map[string]provider.Quote{
		"BTCUSDT": {Symbol: "BTCUSDT", Price: 50000, Timestamp: time.Now()},
		"ETHUSDT": {Symbol: "ETHUSDT", Price: 2500, Timestamp: time.Now()},
	}

	s := Evaluate(holdings, quotes)

	if len(s.Positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(s.Positions))
	}

	// BTC: cost=20000, value=25000, pnl=5000 (25%)
	btc := s.Positions[0]
	if btc.PnL != 5000 {
		t.Errorf("BTC PnL: expected 5000, got %.2f", btc.PnL)
	}
	if btc.PnLPct != 25 {
		t.Errorf("BTC PnLPct: expected 25, got %.2f", btc.PnLPct)
	}
	if !btc.Priced {
		t.Error("BTC should be priced")
	}

	// ETH: cost=20000, value=25000, pnl=5000 (25%)
	eth := s.Positions[1]
	if eth.PnL != 5000 {
		t.Errorf("ETH PnL: expected 5000, got %.2f", eth.PnL)
	}

	// Total: cost=40000, value=50000, pnl=10000 (25%)
	if s.TotalPnL != 10000 {
		t.Errorf("Total PnL: expected 10000, got %.2f", s.TotalPnL)
	}
	if s.TotalPnLPct != 25 {
		t.Errorf("Total PnLPct: expected 25, got %.2f", s.TotalPnLPct)
	}
	if !s.FullyPriced() || len(s.Unpriced) != 0 {
		t.Errorf("expected fully priced, got Unpriced=%v", s.Unpriced)
	}
	if s.Coverage() != 1 {
		t.Errorf("Coverage = %v, want 1", s.Coverage())
	}
}

// TestEvaluateNoQuote pins the headline fix: a holding nobody is pricing must
// not be rendered as a flawless break-even position, and must not appear in the
// totals at all.
func TestEvaluateNoQuote(t *testing.T) {
	holdings := []Holding{
		{Symbol: "AAPL", Quantity: 100, CostBasis: 150},
	}
	quotes := map[string]provider.Quote{} // no quote available

	s := Evaluate(holdings, quotes)
	p := s.Positions[0]
	if p.Priced {
		t.Error("Priced = true with no quote")
	}
	if p.PnL != 0 || p.PnLPct != 0 {
		t.Errorf("unpriced position should report no P&L, got %.2f (%.2f%%)", p.PnL, p.PnLPct)
	}
	// CurrentPrice still falls back to cost so nothing divides by zero.
	if p.CurrentPrice != 150 || p.MarketValue != 15000 {
		t.Errorf("fallback price/value = %.2f/%.2f, want 150/15000", p.CurrentPrice, p.MarketValue)
	}
	if len(s.Unpriced) != 1 || s.Unpriced[0] != "AAPL" {
		t.Errorf("Unpriced = %v, want [AAPL]", s.Unpriced)
	}
	if s.TotalCost != 0 || s.TotalValue != 0 || s.TotalPnL != 0 || s.TotalPnLPct != 0 {
		t.Errorf("unpriced holding leaked into totals: %+v", s)
	}
	if s.FullyPriced() {
		t.Error("FullyPriced = true with an unpriced holding")
	}
	if got := s.UnpricedCost(); got != 15000 {
		t.Errorf("UnpricedCost = %.2f, want 15000", got)
	}
	if got := s.Coverage(); got != 0 {
		t.Errorf("Coverage = %v, want 0", got)
	}
}

// TestEvaluateMixedPricingKeepsTotalsHonest is the reported repro: AAPL priced,
// KO and JNJ never subscribed. The old code reported Value $11389.10 on Cost
// $9800.00 for +16.2%, silently averaging in two fabricated break-evens.
func TestEvaluateMixedPricingKeepsTotalsHonest(t *testing.T) {
	holdings := []Holding{
		{Symbol: "AAPL", Quantity: 10, CostBasis: 150},
		{Symbol: "KO", Quantity: 100, CostBasis: 55},
		{Symbol: "JNJ", Quantity: 20, CostBasis: 140},
	}
	quotes := map[string]provider.Quote{
		"AAPL": {Symbol: "AAPL", Price: 308.91},
	}

	s := Evaluate(holdings, quotes)

	if got := []string{s.Unpriced[0], s.Unpriced[1]}; len(s.Unpriced) != 2 || got[0] != "KO" || got[1] != "JNJ" {
		t.Fatalf("Unpriced = %v, want [KO JNJ] in Positions order", s.Unpriced)
	}
	// Totals cover AAPL alone: cost 1500, value 3089.10, +105.94%.
	if math.Abs(s.TotalCost-1500) > 1e-9 {
		t.Errorf("TotalCost = %.2f, want 1500 (priced subset only)", s.TotalCost)
	}
	if math.Abs(s.TotalValue-3089.10) > 1e-9 {
		t.Errorf("TotalValue = %.2f, want 3089.10", s.TotalValue)
	}
	if math.Abs(s.TotalPnLPct-105.94) > 0.01 {
		t.Errorf("TotalPnLPct = %.2f, want ~105.94 (not the diluted 16.2)", s.TotalPnLPct)
	}
	if got := s.UnpricedCost(); math.Abs(got-8300) > 1e-9 {
		t.Errorf("UnpricedCost = %.2f, want 8300", got)
	}
	if got := s.Coverage(); math.Abs(got-1500.0/9800.0) > 1e-9 {
		t.Errorf("Coverage = %v, want 1500/9800", got)
	}
}

func TestEvaluateRejectsUnusablePrices(t *testing.T) {
	cases := []struct {
		name  string
		price float64
	}{
		{"zero", 0},
		{"negative", -1},
		{"NaN", math.NaN()},
		{"infinite", math.Inf(1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Evaluate(
				[]Holding{{Symbol: "AAPL", Quantity: 10, CostBasis: 150}},
				map[string]provider.Quote{"AAPL": {Symbol: "AAPL", Price: c.price}},
			)
			if s.Positions[0].Priced {
				t.Errorf("price %v was accepted as live", c.price)
			}
			if s.TotalValue != 0 {
				t.Errorf("TotalValue = %v, want 0", s.TotalValue)
			}
		})
	}
}

func TestEvaluateMatchesCanonicalSymbol(t *testing.T) {
	// Config spells it "btc"; providers emit "BTC-USD".
	s := Evaluate(
		[]Holding{{Symbol: "btc", Quantity: 2, CostBasis: 40000}},
		map[string]provider.Quote{"BTC-USD": {Symbol: "BTC-USD", Price: 50000}},
	)
	if !s.Positions[0].Priced {
		t.Fatal("btc did not match the BTC-USD quote")
	}
	if s.TotalValue != 100000 {
		t.Errorf("TotalValue = %.2f, want 100000", s.TotalValue)
	}
}

func TestEvaluateEmptyHoldings(t *testing.T) {
	s := Evaluate(nil, nil)
	if len(s.Positions) != 0 || !s.FullyPriced() || s.Coverage() != 1 {
		t.Errorf("empty portfolio: %+v coverage=%v", s, s.Coverage())
	}
}

func TestEvaluateZeroCostBasisHolding(t *testing.T) {
	// An airdrop: no cost, a real price. P&L% is undefined so it stays 0, but
	// the value must still count.
	s := Evaluate(
		[]Holding{{Symbol: "AAPL", Quantity: 10, CostBasis: 0}},
		map[string]provider.Quote{"AAPL": {Symbol: "AAPL", Price: 100}},
	)
	if !s.Positions[0].Priced {
		t.Fatal("zero cost basis should not make a holding unpriced")
	}
	if s.TotalValue != 1000 || s.TotalPnL != 1000 {
		t.Errorf("TotalValue/PnL = %.2f/%.2f, want 1000/1000", s.TotalValue, s.TotalPnL)
	}
	if s.TotalPnLPct != 0 {
		t.Errorf("TotalPnLPct = %.2f, want 0 (undefined on zero cost)", s.TotalPnLPct)
	}
}
