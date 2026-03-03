package portfolio

import (
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
}

func TestEvaluateNoQuote(t *testing.T) {
	holdings := []Holding{
		{Symbol: "AAPL", Quantity: 100, CostBasis: 150},
	}
	quotes := map[string]provider.Quote{} // no quote available

	s := Evaluate(holdings, quotes)
	// Should fall back to cost basis
	if s.Positions[0].PnL != 0 {
		t.Errorf("Expected 0 PnL with no quote, got %.2f", s.Positions[0].PnL)
	}
}
