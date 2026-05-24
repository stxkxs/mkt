package portfolio

import (
	"math"
	"testing"
	"time"
)

func TestRealizedByMethodAverageMatchesLegacy(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100, Fee: 5},
		{Type: TxBuy, Symbol: "AAPL", Quantity: 5, Price: 200},
		{Type: TxSell, Symbol: "AAPL", Quantity: 8, Price: 180, Fee: 3},
	}
	avg := Realized(txs)
	got := RealizedByMethod(txs, TaxAverage)
	if math.Abs(got-avg) > 1e-9 {
		t.Errorf("TaxAverage diverged from Realized: %v vs %v", got, avg)
	}
}

func TestRealizedByMethodDivergent(t *testing.T) {
	// Three lots at distinct prices; one sell. FIFO, LIFO, HIFO all differ.
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	txs := []Transaction{
		{Type: TxBuy, Symbol: "X", Quantity: 10, Price: 100, Time: t0},
		{Type: TxBuy, Symbol: "X", Quantity: 10, Price: 200, Time: t1},
		{Type: TxBuy, Symbol: "X", Quantity: 10, Price: 150, Time: t2},
		{Type: TxSell, Symbol: "X", Quantity: 10, Price: 180},
	}
	fifo := RealizedByMethod(txs, TaxFIFO) // consume 100 lot → (180-100)*10 = 800
	lifo := RealizedByMethod(txs, TaxLIFO) // consume 150 lot → (180-150)*10 = 300
	hifo := RealizedByMethod(txs, TaxHIFO) // consume 200 lot → (180-200)*10 = -200
	if math.Abs(fifo-800) > 1e-9 {
		t.Errorf("FIFO: got %v want 800", fifo)
	}
	if math.Abs(lifo-300) > 1e-9 {
		t.Errorf("LIFO: got %v want 300", lifo)
	}
	if math.Abs(hifo+200) > 1e-9 {
		t.Errorf("HIFO: got %v want -200", hifo)
	}
}

func TestRealizedByMethodOversellClamps(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "X", Quantity: 5, Price: 100},
		{Type: TxSell, Symbol: "X", Quantity: 50, Price: 150}, // oversell
	}
	got := RealizedByMethod(txs, TaxFIFO)
	// Only 5 units can be consumed: (150 - 100) * 5 = 250
	if math.Abs(got-250) > 1e-9 {
		t.Errorf("oversell should clamp: got %v want 250", got)
	}
}

func TestRealizedByMethodPerSymbolIsolated(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "A", Quantity: 10, Price: 100},
		{Type: TxBuy, Symbol: "B", Quantity: 10, Price: 200},
		{Type: TxSell, Symbol: "A", Quantity: 10, Price: 110}, // +100
		{Type: TxSell, Symbol: "B", Quantity: 10, Price: 180}, // -200
	}
	got := RealizedByMethod(txs, TaxFIFO)
	if math.Abs(got+100) > 1e-9 {
		t.Errorf("expected -100, got %v", got)
	}
}

func TestRealizedByMethodSellFeeDeducted(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "X", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "X", Quantity: 10, Price: 110, Fee: 10},
	}
	// (110-100)*10 - 10 = 90
	got := RealizedByMethod(txs, TaxFIFO)
	if math.Abs(got-90) > 1e-9 {
		t.Errorf("expected 90, got %v", got)
	}
}

func TestRealizedByMethodBuyFeeRaisesBasis(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "X", Quantity: 10, Price: 100, Fee: 50}, // per-unit basis 105
		{Type: TxSell, Symbol: "X", Quantity: 10, Price: 110},         // (110-105)*10 = 50
	}
	got := RealizedByMethod(txs, TaxFIFO)
	if math.Abs(got-50) > 1e-9 {
		t.Errorf("expected 50, got %v", got)
	}
}

func TestRealizedByMethodEmpty(t *testing.T) {
	for _, m := range []TaxMethod{TaxAverage, TaxFIFO, TaxLIFO, TaxHIFO} {
		if got := RealizedByMethod(nil, m); got != 0 {
			t.Errorf("method %q: empty → %v want 0", m, got)
		}
	}
}
