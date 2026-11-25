package portfolio

import (
	"math"
	"testing"
	"time"
)

var allMethods = []TaxMethod{TaxAverage, TaxFIFO, TaxLIFO, TaxHIFO}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Selling more than is held realizes only the held quantity, under every
// method, and leaves no position behind.
func TestOversellClampsToHeldQuantity(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 25, Price: 110, Fee: 2},
	}
	for _, m := range allMethods {
		if got := RealizedByMethod(txs, m); !near(got, 10*10-2) {
			t.Errorf("method %q: realized %v, want 98 (10 shares × $10 − $2 fee)", m, got)
		}
	}
	if h := DeriveHoldings(txs); len(h) != 0 {
		t.Errorf("holdings after oversell = %+v, want none", h)
	}
}

// A sell with nothing left to sell realizes nothing, fee included, and the
// next buy starts a fresh cost basis.
func TestSellAfterSellOutIsIgnored(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 10, Price: 120},
		{Type: TxSell, Symbol: "AAPL", Quantity: 5, Price: 130, Fee: 3},
		{Type: TxBuy, Symbol: "AAPL", Quantity: 4, Price: 50},
	}
	for _, m := range allMethods {
		if got := RealizedByMethod(txs, m); !near(got, 200) {
			t.Errorf("method %q: realized %v, want 200 (only the first sell)", m, got)
		}
	}
	h := DeriveHoldings(txs)
	if len(h) != 1 || !near(h[0].Quantity, 4) || !near(h[0].CostBasis, 50) {
		t.Errorf("holdings = %+v, want 4 @ 50 from the fresh buy", h)
	}
}

// Rows that move no shares are ignored by every fold, so the average and
// lot methods agree on them.
func TestNonPositiveQuantityRowsAreIgnored(t *testing.T) {
	real := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 5, Price: 120},
	}
	noise := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 0, Price: 999, Fee: 50},
		{Type: TxBuy, Symbol: "AAPL", Quantity: -3, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 0, Price: 120, Fee: 7},
		{Type: TxSell, Symbol: "AAPL", Quantity: -2, Price: 120},
	}
	mixed := []Transaction{noise[0], real[0], noise[1], noise[2], real[1], noise[3]}

	for _, m := range allMethods {
		want := RealizedByMethod(real, m)
		if got := RealizedByMethod(mixed, m); !near(got, want) {
			t.Errorf("method %q: realized %v with no-op rows, %v without", m, got, want)
		}
	}
	got, want := DeriveHoldings(mixed), DeriveHoldings(real)
	if len(got) != 1 || len(want) != 1 || !near(got[0].Quantity, want[0].Quantity) || !near(got[0].CostBasis, want[0].CostBasis) {
		t.Errorf("holdings %+v with no-op rows, %+v without", got, want)
	}
}

// The year boundary is UTC: a dividend paid late on 31 December in New
// York is already next year in UTC, and one paid early on 1 January in
// Tokyo is still the previous year.
func TestDividendsYTDYearBoundaryIsUTC(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	txs := []Transaction{
		{Type: TxDividend, Symbol: "A", Quantity: 1, Price: 10, Time: time.Date(2025, 12, 31, 22, 0, 0, 0, ny)},  // 2026-01-01 03:00 UTC
		{Type: TxDividend, Symbol: "B", Quantity: 1, Price: 100, Time: time.Date(2026, 1, 1, 5, 0, 0, 0, tokyo)}, // 2025-12-31 20:00 UTC
		{Type: TxDividend, Symbol: "C", Quantity: 1, Price: 1, Fee: 0.25, Time: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		{Type: TxBuy, Symbol: "C", Quantity: 1, Price: 1000, Time: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	}
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, tokyo)
	if got := DividendsYTD(txs, now); !near(got, 10+0.75) {
		t.Errorf("DividendsYTD = %v, want 10.75 (A and C, net of fee)", got)
	}
	if got := DividendsYTD(txs, time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)); !near(got, 100) {
		t.Errorf("DividendsYTD for 2025 = %v, want 100 (B only)", got)
	}
}
