package portfolio

import (
	"sort"
	"time"
)

// TaxMethod identifies which lot-consumption strategy realized-P&L uses.
// The empty value is TaxAverage (weighted-average cost) for backward
// compatibility with existing configurations.
type TaxMethod string

const (
	TaxAverage TaxMethod = "" // default; matches portfolio.Realized
	TaxFIFO    TaxMethod = "fifo"
	TaxLIFO    TaxMethod = "lifo"
	TaxHIFO    TaxMethod = "hifo"
)

// lot is one buy-side bucket of inventory for a symbol.
type lot struct {
	Quantity float64
	Cost     float64 // per-unit, fee-adjusted
	Time     time.Time
}

// RealizedByMethod returns cumulative realized P&L computed with the
// given tax-lot method. TaxAverage delegates to Realized. Other methods
// track per-symbol lot queues and consume lots in method-specific order.
func RealizedByMethod(txs []Transaction, method TaxMethod) float64 {
	if method == TaxAverage {
		return Realized(txs)
	}
	state := make(map[string][]lot, len(txs))
	var total float64
	for _, t := range txs {
		switch t.Type {
		case TxBuy:
			if t.Quantity <= 0 {
				continue
			}
			perUnit := t.Price + t.Fee/t.Quantity
			state[t.Symbol] = append(state[t.Symbol], lot{
				Quantity: t.Quantity,
				Cost:     perUnit,
				Time:     t.Time,
			})
		case TxSell:
			lots := state[t.Symbol]
			if len(lots) == 0 {
				continue
			}
			realized, remaining := consumeLots(lots, t.Quantity, t.Price, t.Fee, method)
			total += realized
			state[t.Symbol] = remaining
		}
	}
	return total
}

// consumeLots removes `qty` units from `lots` in the order dictated by
// `method`, returning the realized P&L and the surviving lots.
func consumeLots(lots []lot, qty, sellPrice, fee float64, method TaxMethod) (float64, []lot) {
	order := consumeOrder(lots, method)
	var realized float64
	remaining := qty
	for _, i := range order {
		if remaining <= 0 {
			break
		}
		consume := lots[i].Quantity
		if consume > remaining {
			consume = remaining
		}
		realized += (sellPrice - lots[i].Cost) * consume
		lots[i].Quantity -= consume
		remaining -= consume
	}
	realized -= fee
	out := lots[:0]
	for _, l := range lots {
		if l.Quantity > 0 {
			out = append(out, l)
		}
	}
	return realized, out
}

// consumeOrder returns the indices into `lots` in the order the method
// wants them consumed. FIFO is identity; LIFO is reversed; HIFO sorts
// by descending Cost (stable on ties for predictability).
func consumeOrder(lots []lot, method TaxMethod) []int {
	idx := make([]int, len(lots))
	for i := range lots {
		idx[i] = i
	}
	switch method {
	case TaxLIFO:
		for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
			idx[i], idx[j] = idx[j], idx[i]
		}
	case TaxHIFO:
		sort.SliceStable(idx, func(a, b int) bool {
			return lots[idx[a]].Cost > lots[idx[b]].Cost
		})
	}
	return idx
}
