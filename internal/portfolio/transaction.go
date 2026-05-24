package portfolio

import "time"

// TxType identifies the kind of transaction.
type TxType string

const (
	TxBuy  TxType = "buy"
	TxSell TxType = "sell"
)

// Transaction is a single recorded portfolio action. Time and Fee are
// optional; zero values are treated as "unset".
type Transaction struct {
	Type     TxType
	Symbol   string
	Quantity float64
	Price    float64 // per-unit
	Time     time.Time
	Fee      float64
	Note     string
}

// DeriveHoldings folds a transaction history into current holdings using
// weighted-average cost basis. SELL reduces quantity and proportionally
// reduces accumulated cost but does not record realized P&L (that is the
// concern of P1). Symbols whose net quantity reaches zero or below are
// dropped from the result. Insertion order is preserved.
func DeriveHoldings(txs []Transaction) []Holding {
	type acc struct {
		Quantity  float64
		TotalCost float64
	}
	state := make(map[string]*acc, len(txs))
	var order []string

	for _, t := range txs {
		a, ok := state[t.Symbol]
		if !ok {
			a = &acc{}
			state[t.Symbol] = a
			order = append(order, t.Symbol)
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
			a.Quantity -= sellQty
			a.TotalCost -= avgCost * sellQty
		}
	}

	out := make([]Holding, 0, len(order))
	for _, sym := range order {
		a := state[sym]
		if a.Quantity <= 0 {
			continue
		}
		out = append(out, Holding{
			Symbol:    sym,
			Quantity:  a.Quantity,
			CostBasis: a.TotalCost / a.Quantity,
		})
	}
	return out
}

// Materialize is the convenience used by the config loader. If txs is
// empty it returns existingHoldings unchanged for full backward
// compatibility with holdings-only configs. Otherwise each existing
// holding with positive quantity becomes a synthetic BUY transaction at
// its cost basis, prepended to txs before folding. Names from the
// original holdings are re-applied to the derived holdings.
func Materialize(existingHoldings []Holding, txs []Transaction) []Holding {
	if len(txs) == 0 {
		return existingHoldings
	}

	names := make(map[string]string, len(existingHoldings))
	all := make([]Transaction, 0, len(existingHoldings)+len(txs))
	for _, h := range existingHoldings {
		if h.Name != "" {
			names[h.Symbol] = h.Name
		}
		if h.Quantity > 0 {
			all = append(all, Transaction{
				Type:     TxBuy,
				Symbol:   h.Symbol,
				Quantity: h.Quantity,
				Price:    h.CostBasis,
			})
		}
	}
	all = append(all, txs...)

	holdings := DeriveHoldings(all)
	for i, h := range holdings {
		if h.Name == "" {
			if n, ok := names[h.Symbol]; ok {
				holdings[i].Name = n
			}
		}
	}
	return holdings
}
