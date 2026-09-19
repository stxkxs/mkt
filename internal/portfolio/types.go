package portfolio

import "github.com/stxkxs/mkt/internal/provider"

// Holding represents a single portfolio position.
type Holding struct {
	Symbol    string
	Name      string
	Quantity  float64
	CostBasis float64 // per-unit cost
}

// Portfolio is a named collection of holdings, optionally accompanied by
// the transaction log that produced them. Holdings answer what is held;
// the log answers at what cost and what has already been realized, so
// realized P&L and tax-lot accounting need no second read of config.
// TaxMethod governs how Realized consumes that log.
type Portfolio struct {
	Name         string
	Holdings     []Holding
	Transactions []Transaction
	TaxMethod    TaxMethod
}

// Position is a holding with live P&L calculated.
type Position struct {
	Holding
	CurrentPrice float64
	MarketValue  float64
	PnL          float64 // unrealized P&L; always 0 when Priced is false
	PnLPct       float64 // unrealized P&L percentage; always 0 when Priced is false
	Priced       bool    // false when no live quote was available (price fell back to cost basis)
}

// Summary is the overall portfolio summary.
//
// The totals cover the priced positions only. A holding with no live quote is
// still present in Positions (with Priced false) but contributes nothing to
// TotalCost, TotalValue, TotalPnL or TotalPnLPct, and its symbol is listed in
// Unpriced. Read Unpriced — or FullyPriced/Coverage — before presenting the
// totals as the whole portfolio.
type Summary struct {
	Positions   []Position
	TotalCost   float64
	TotalValue  float64
	TotalPnL    float64
	TotalPnLPct float64
	Unpriced    []string // symbols with no live quote, in Positions order
}

// FullyPriced reports whether every holding had a live quote, i.e. whether the
// totals describe the entire portfolio.
func (s Summary) FullyPriced() bool { return len(s.Unpriced) == 0 }

// UnpricedCost returns the cost basis of the holdings excluded from the totals.
// It is what a caller shows next to a partial total: "excludes $2,800 across 2
// positions".
func (s Summary) UnpricedCost() float64 {
	var c float64
	for _, p := range s.Positions {
		if !p.Priced {
			c += p.Quantity * p.CostBasis
		}
	}
	return c
}

// Coverage returns the share of the portfolio's cost basis that the totals
// account for, from 0 to 1. A portfolio with no cost basis at all returns 1 —
// there is nothing left uncovered.
func (s Summary) Coverage() float64 {
	unpriced := s.UnpricedCost()
	total := s.TotalCost + unpriced
	if total <= 0 {
		return 1
	}
	return s.TotalCost / total
}

// Evaluate prices the portfolio's own holdings against quotes and returns
// the same Summary as the package-level Evaluate over a loose slice. Prefer
// it wherever a whole Portfolio is in hand: the aggregate supplies its own
// holdings, so no call site can pair one portfolio's positions with
// another's.
func (p Portfolio) Evaluate(quotes map[string]provider.Quote) Summary {
	return Evaluate(p.Holdings, quotes)
}

// Realized returns cumulative realized P&L over the portfolio's own
// transaction log, settled under its own TaxMethod. The pairing is the
// point: TaxMethod describes how this log is to be consumed, and a log
// settled under some other portfolio's method is not a number anyone can
// act on.
func (p Portfolio) Realized() float64 {
	return RealizedByMethod(p.Transactions, p.TaxMethod)
}
