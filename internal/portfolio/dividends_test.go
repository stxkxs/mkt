package portfolio

import (
	"math"
	"testing"
	"time"
)

func TestDividendsIgnoredByDeriveHoldings(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.25},
	}
	got := DeriveHoldings(txs)
	if len(got) != 1 {
		t.Fatalf("want 1 holding, got %d", len(got))
	}
	if got[0].Quantity != 100 || got[0].CostBasis != 150 {
		t.Errorf("dividend should not change holdings: got %+v", got[0])
	}
}

func TestDividendsIgnoredByRealized(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.25},
		{Type: TxSell, Symbol: "AAPL", Quantity: 100, Price: 160},
	}
	got := Realized(txs)
	want := 100 * (160.0 - 150.0)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("realized should not include dividend: got %v want %v", got, want)
	}
	gotFIFO := RealizedByMethod(txs, TaxFIFO)
	if math.Abs(gotFIFO-want) > 1e-9 {
		t.Errorf("FIFO should not include dividend: got %v want %v", gotFIFO, want)
	}
}

func TestDividendsCumulative(t *testing.T) {
	txs := []Transaction{
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.25},
		{Type: TxDividend, Symbol: "MSFT", Quantity: 50, Price: 0.75},
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.30, Fee: 1},
	}
	// 25 + 37.5 + (30 - 1) = 91.5
	got := Dividends(txs)
	if math.Abs(got-91.5) > 1e-9 {
		t.Errorf("got %v want 91.5", got)
	}
}

func TestDividendsYTD(t *testing.T) {
	txs := []Transaction{
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.25, Time: time.Date(2025, 11, 15, 0, 0, 0, 0, time.UTC)},
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.25, Time: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)},
		{Type: TxDividend, Symbol: "AAPL", Quantity: 100, Price: 0.25, Time: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)},
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ytd := DividendsYTD(txs, now)
	// 2026 entries only: 25 + 25 = 50
	if math.Abs(ytd-50) > 1e-9 {
		t.Errorf("YTD got %v want 50", ytd)
	}
}

func TestDividendsEmpty(t *testing.T) {
	if got := Dividends(nil); got != 0 {
		t.Errorf("empty got %v want 0", got)
	}
	if got := DividendsYTD(nil, time.Now()); got != 0 {
		t.Errorf("empty YTD got %v want 0", got)
	}
}

func TestDividendsZeroForBuyOnly(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxSell, Symbol: "AAPL", Quantity: 50, Price: 200},
	}
	if got := Dividends(txs); got != 0 {
		t.Errorf("buy/sell-only got %v want 0", got)
	}
}
