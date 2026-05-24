package portfolio

// Realized walks transactions in declaration order maintaining a
// per-symbol weighted-average cost and returns the cumulative realized
// P&L. Each SELL contributes (price - avgCost) * sold_qty - fee to the
// running total. Buy fees are folded into cost basis (matching
// DeriveHoldings). Returns 0 for buy-only or empty histories.
func Realized(txs []Transaction) float64 {
	type acc struct {
		Quantity  float64
		TotalCost float64
	}
	state := make(map[string]*acc, len(txs))
	var total float64
	for _, t := range txs {
		a, ok := state[t.Symbol]
		if !ok {
			a = &acc{}
			state[t.Symbol] = a
		}
		switch t.Type {
		case TxBuy:
			a.Quantity += t.Quantity
			a.TotalCost += t.Quantity*t.Price + t.Fee
		case TxSell:
			if a.Quantity <= 0 {
				continue
			}
			avgCost := a.TotalCost / a.Quantity
			sellQty := t.Quantity
			if sellQty > a.Quantity {
				sellQty = a.Quantity
			}
			proceeds := t.Price * sellQty
			cost := avgCost * sellQty
			total += proceeds - cost - t.Fee
			a.Quantity -= sellQty
			a.TotalCost -= cost
		}
	}
	return total
}
