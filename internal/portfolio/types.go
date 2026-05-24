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
}

// Position is a holding with live P&L calculated.
type Position struct {
	Holding
	CurrentPrice float64
	MarketValue  float64
	PnL          float64 // unrealized P&L
	PnLPct       float64 // unrealized P&L percentage
}

// Summary is the overall portfolio summary.
type Summary struct {
	Positions   []Position
	TotalCost   float64
	TotalValue  float64
	TotalPnL    float64
	TotalPnLPct float64
}
