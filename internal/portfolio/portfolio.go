package portfolio

import "github.com/stxkxs/mkt/internal/provider"

// Evaluate computes P&L for holdings using current quotes.
func Evaluate(holdings []Holding, quotes map[string]provider.Quote) Summary {
	var s Summary
	for _, h := range holdings {
		q, ok := quotes[h.Symbol]
		price := q.Price
		if !ok || price == 0 {
			price = h.CostBasis // fallback to cost if no quote
		}

		cost := h.Quantity * h.CostBasis
		value := h.Quantity * price
		pnl := value - cost
		var pnlPct float64
		if cost > 0 {
			pnlPct = (pnl / cost) * 100
		}

		s.Positions = append(s.Positions, Position{
			Holding:      h,
			CurrentPrice: price,
			MarketValue:  value,
			PnL:          pnl,
			PnLPct:       pnlPct,
		})

		s.TotalCost += cost
		s.TotalValue += value
	}

	s.TotalPnL = s.TotalValue - s.TotalCost
	if s.TotalCost > 0 {
		s.TotalPnLPct = (s.TotalPnL / s.TotalCost) * 100
	}
	return s
}
