package portfolio

import (
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func aggregateFixture(method TaxMethod) Portfolio {
	base := time.Date(2024, 3, 11, 14, 30, 0, 0, time.UTC)
	txs := []Transaction{
		{Type: TxBuy, Symbol: "NVDA", Quantity: 10, Price: 100, Time: base},
		{Type: TxBuy, Symbol: "NVDA", Quantity: 10, Price: 300, Time: base.Add(24 * time.Hour)},
		{Type: TxSell, Symbol: "NVDA", Quantity: 10, Price: 400, Time: base.Add(48 * time.Hour)},
	}
	return Portfolio{
		Name:         "Aggregate",
		Holdings:     Materialize(nil, txs),
		Transactions: txs,
		TaxMethod:    method,
	}
}

// The method form must be the free function over the aggregate's own
// holdings and nothing else, so a call site can move between them without
// changing a number.
func TestPortfolioEvaluateMatchesFreeFunction(t *testing.T) {
	t.Parallel()
	p := aggregateFixture(TaxAverage)
	p.Holdings = append(p.Holdings, Holding{Symbol: "NOQUOTE", Quantity: 3, CostBasis: 5})
	quotes := map[string]provider.Quote{"NVDA": {Symbol: "NVDA", Price: 500}}

	got := p.Evaluate(quotes)
	want := Evaluate(p.Holdings, quotes)

	if got.TotalCost != want.TotalCost || got.TotalValue != want.TotalValue ||
		got.TotalPnL != want.TotalPnL || got.TotalPnLPct != want.TotalPnLPct {
		t.Errorf("Portfolio.Evaluate totals = %+v, want %+v", got, want)
	}
	if len(got.Positions) != len(want.Positions) || len(got.Unpriced) != len(want.Unpriced) {
		t.Fatalf("Portfolio.Evaluate positions = %d/%d unpriced, want %d/%d",
			len(got.Positions), len(got.Unpriced), len(want.Positions), len(want.Unpriced))
	}
	for i := range got.Positions {
		if got.Positions[i] != want.Positions[i] {
			t.Errorf("position %d = %+v, want %+v", i, got.Positions[i], want.Positions[i])
		}
	}
}

// Realized settles the aggregate's own log under the aggregate's own
// TaxMethod. Each method must produce its own number here, or the method is
// a facade over a fixed strategy rather than over the field.
func TestPortfolioRealizedUsesItsOwnTaxMethod(t *testing.T) {
	t.Parallel()
	for _, method := range []TaxMethod{TaxAverage, TaxFIFO, TaxLIFO, TaxHIFO} {
		p := aggregateFixture(method)
		if got, want := p.Realized(), RealizedByMethod(p.Transactions, method); got != want {
			t.Errorf("TaxMethod %q: Realized() = %v, want %v", method, got, want)
		}
	}

	// FIFO consumes the 100 lot and LIFO the 300 lot, so a Realized that
	// ignored TaxMethod would report one number for both.
	fifo := aggregateFixture(TaxFIFO).Realized()
	lifo := aggregateFixture(TaxLIFO).Realized()
	if fifo == lifo {
		t.Fatalf("FIFO and LIFO both realized %v; TaxMethod is not reaching the calculation", fifo)
	}
	if want := 3000.0; fifo != want {
		t.Errorf("FIFO realized = %v, want %v", fifo, want)
	}
	if want := 1000.0; lifo != want {
		t.Errorf("LIFO realized = %v, want %v", lifo, want)
	}
}

// A portfolio with no log realizes nothing, whatever method it declares.
func TestPortfolioRealizedEmptyLog(t *testing.T) {
	t.Parallel()
	for _, method := range []TaxMethod{TaxAverage, TaxFIFO, TaxLIFO, TaxHIFO} {
		p := Portfolio{Name: "Empty", TaxMethod: method}
		if got := p.Realized(); got != 0 {
			t.Errorf("TaxMethod %q: Realized() = %v, want 0", method, got)
		}
	}
}
