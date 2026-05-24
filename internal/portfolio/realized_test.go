package portfolio

import (
	"math"
	"testing"
)

func TestRealizedBuyOnly(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
	})
	if got != 0 {
		t.Errorf("buy-only should yield 0 realized, got %v", got)
	}
}

func TestRealizedEmpty(t *testing.T) {
	if got := Realized(nil); got != 0 {
		t.Errorf("empty txs should yield 0, got %v", got)
	}
}

func TestRealizedSellAtCost(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxSell, Symbol: "AAPL", Quantity: 50, Price: 150},
	})
	if math.Abs(got) > 1e-9 {
		t.Errorf("sell at cost should yield 0, got %v", got)
	}
}

func TestRealizedGain(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxSell, Symbol: "AAPL", Quantity: 50, Price: 200},
	})
	// (200 - 150) * 50 = 2500
	if math.Abs(got-2500) > 1e-9 {
		t.Errorf("expected 2500 gain, got %v", got)
	}
}

func TestRealizedLoss(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 200},
		{Type: TxSell, Symbol: "AAPL", Quantity: 50, Price: 150},
	})
	if math.Abs(got+2500) > 1e-9 {
		t.Errorf("expected -2500 loss, got %v", got)
	}
}

func TestRealizedSellFeeReducesProfit(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 10, Price: 110, Fee: 5},
	})
	// (110 - 100) * 10 - 5 = 95
	if math.Abs(got-95) > 1e-9 {
		t.Errorf("expected 95, got %v", got)
	}
}

func TestRealizedRoundTripsAccumulate(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 10, Price: 120}, // +200
		{Type: TxBuy, Symbol: "AAPL", Quantity: 5, Price: 90},
		{Type: TxSell, Symbol: "AAPL", Quantity: 5, Price: 100}, // +50
	})
	if math.Abs(got-250) > 1e-9 {
		t.Errorf("expected 250 accumulated, got %v", got)
	}
}

func TestRealizedMultipleSymbolsIsolated(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxBuy, Symbol: "MSFT", Quantity: 10, Price: 200},
		{Type: TxSell, Symbol: "AAPL", Quantity: 10, Price: 110}, // +100
		{Type: TxSell, Symbol: "MSFT", Quantity: 10, Price: 180}, // -200
	})
	if math.Abs(got+100) > 1e-9 {
		t.Errorf("expected -100 net, got %v", got)
	}
}

func TestRealizedBuyWithFeeAffectsCostBasis(t *testing.T) {
	got := Realized([]Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100, Fee: 50}, // cost basis 105
		{Type: TxSell, Symbol: "AAPL", Quantity: 10, Price: 110},         // (110 - 105) * 10 = 50
	})
	if math.Abs(got-50) > 1e-9 {
		t.Errorf("expected 50, got %v", got)
	}
}
