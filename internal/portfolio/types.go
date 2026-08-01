package portfolio

// Holding represents a single portfolio position.
type Holding struct {
	Symbol    string
	Name      string
	Quantity  float64
	CostBasis float64 // per-unit cost
}

// Portfolio is a named collection of holdings, optionally accompanied by
// the transaction log that produced them. Transactions enable realized
// P&L (P1) and tax-lot accounting (P2) without re-reading config.
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
