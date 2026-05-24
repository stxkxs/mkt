package portfolio

import "time"

// Dividends returns the cumulative net dividend total from a transaction
// log. For each TxDividend row, the contribution is
// (Quantity * Price) - Fee — so users can record per-share amounts with
// the share count, or a total amount with Quantity=1.
func Dividends(txs []Transaction) float64 {
	var total float64
	for _, t := range txs {
		if t.Type != TxDividend {
			continue
		}
		total += t.Quantity*t.Price - t.Fee
	}
	return total
}

// DividendsYTD returns the dividend total for the calendar year of
// `now` (UTC).
func DividendsYTD(txs []Transaction, now time.Time) float64 {
	year := now.UTC().Year()
	var total float64
	for _, t := range txs {
		if t.Type != TxDividend {
			continue
		}
		if t.Time.UTC().Year() != year {
			continue
		}
		total += t.Quantity*t.Price - t.Fee
	}
	return total
}
