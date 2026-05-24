package portfolio

import (
	"math"
	"testing"
)

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestDeriveHoldingsBuyOnly(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "BTC-USD", Quantity: 0.3, Price: 35000},
		{Type: TxBuy, Symbol: "BTC-USD", Quantity: 0.2, Price: 50000},
		{Type: TxBuy, Symbol: "ETH-USD", Quantity: 5, Price: 2000},
	}
	got := DeriveHoldings(txs)
	if len(got) != 2 {
		t.Fatalf("want 2 holdings, got %d", len(got))
	}

	btc := got[0]
	if btc.Symbol != "BTC-USD" {
		t.Errorf("ordering broken: first symbol = %q", btc.Symbol)
	}
	if !approxEqual(btc.Quantity, 0.5, 1e-9) {
		t.Errorf("BTC quantity: got %v want 0.5", btc.Quantity)
	}
	// Weighted average: (0.3*35000 + 0.2*50000) / 0.5 = 41000
	if !approxEqual(btc.CostBasis, 41000, 1e-6) {
		t.Errorf("BTC cost basis: got %v want 41000", btc.CostBasis)
	}

	eth := got[1]
	if !approxEqual(eth.CostBasis, 2000, 1e-9) {
		t.Errorf("ETH cost basis: got %v want 2000", eth.CostBasis)
	}
}

func TestDeriveHoldingsSellPreservesCostBasis(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 200},
		{Type: TxSell, Symbol: "AAPL", Quantity: 50, Price: 180},
	}
	got := DeriveHoldings(txs)
	if len(got) != 1 {
		t.Fatalf("want 1 holding, got %d", len(got))
	}
	h := got[0]
	if !approxEqual(h.Quantity, 150, 1e-9) {
		t.Errorf("quantity: got %v want 150", h.Quantity)
	}
	// Avg cost before sell = (100*150 + 100*200) / 200 = 175. SELL reduces
	// quantity and cost proportionally — the per-unit cost basis stays at 175.
	if !approxEqual(h.CostBasis, 175, 1e-9) {
		t.Errorf("cost basis: got %v want 175 (unchanged by sell)", h.CostBasis)
	}
}

func TestDeriveHoldingsSellToZeroDropsSymbol(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 100, Price: 150},
		{Type: TxSell, Symbol: "AAPL", Quantity: 100, Price: 180},
	}
	got := DeriveHoldings(txs)
	if len(got) != 0 {
		t.Fatalf("symbol with zero quantity should be dropped, got %d holdings", len(got))
	}
}

func TestDeriveHoldingsBuyWithFee(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "BTC-USD", Quantity: 1, Price: 50000, Fee: 50},
	}
	got := DeriveHoldings(txs)
	if !approxEqual(got[0].CostBasis, 50050, 1e-9) {
		t.Errorf("fee should be folded into cost: got %v want 50050", got[0].CostBasis)
	}
}

func TestDeriveHoldingsOversellClampsToHolding(t *testing.T) {
	txs := []Transaction{
		{Type: TxBuy, Symbol: "AAPL", Quantity: 10, Price: 100},
		{Type: TxSell, Symbol: "AAPL", Quantity: 100, Price: 150}, // attempts to sell more than held
	}
	got := DeriveHoldings(txs)
	if len(got) != 0 {
		t.Fatalf("oversell should bring quantity to zero, got %d", len(got))
	}
}

func TestMaterializePassthroughWithoutTransactions(t *testing.T) {
	in := []Holding{
		{Symbol: "BTC-USD", Name: "Bitcoin", Quantity: 0.5, CostBasis: 40000},
		{Symbol: "ETH-USD", Quantity: 10, CostBasis: 2000},
	}
	got := Materialize(in, nil)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	// passthrough should preserve the slice unchanged
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("[%d] passthrough mismatch: got %+v want %+v", i, got[i], in[i])
		}
	}
}

func TestMaterializeSynthesizesAndPreservesNames(t *testing.T) {
	in := []Holding{
		{Symbol: "BTC-USD", Name: "Bitcoin", Quantity: 0.3, CostBasis: 35000},
	}
	txs := []Transaction{
		{Type: TxBuy, Symbol: "BTC-USD", Quantity: 0.2, Price: 50000},
		{Type: TxBuy, Symbol: "ETH-USD", Quantity: 5, Price: 2000},
	}
	got := Materialize(in, txs)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}

	btc := got[0]
	if btc.Symbol != "BTC-USD" {
		t.Errorf("BTC ordering broken: %q first", btc.Symbol)
	}
	if btc.Name != "Bitcoin" {
		t.Errorf("name not preserved: got %q want Bitcoin", btc.Name)
	}
	if !approxEqual(btc.Quantity, 0.5, 1e-9) {
		t.Errorf("BTC quantity: got %v want 0.5", btc.Quantity)
	}
	if !approxEqual(btc.CostBasis, 41000, 1e-6) {
		t.Errorf("BTC cost basis: got %v want 41000", btc.CostBasis)
	}

	eth := got[1]
	if eth.Symbol != "ETH-USD" {
		t.Errorf("ETH should be second: got %q", eth.Symbol)
	}
}

func TestMaterializeIgnoresZeroQuantityHolding(t *testing.T) {
	in := []Holding{
		{Symbol: "BTC-USD", Quantity: 0, CostBasis: 999999}, // pre-sold or seeded
	}
	txs := []Transaction{
		{Type: TxBuy, Symbol: "BTC-USD", Quantity: 1, Price: 50000},
	}
	got := Materialize(in, txs)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if !approxEqual(got[0].CostBasis, 50000, 1e-9) {
		t.Errorf("zero-quantity holding should not contribute cost: got %v want 50000", got[0].CostBasis)
	}
}
