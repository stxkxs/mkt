package portfolio

import (
	"math"

	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/symbol"
)

// Evaluate computes P&L for holdings using current quotes.
//
// A holding is priced when quotes carries a usable price for it: present, finite
// and greater than zero. Anything else — a symbol no provider ever subscribed
// to, a typo, a quote that has not arrived yet — is unpriced. An unpriced
// position keeps a CurrentPrice of its cost basis so downstream formatting and
// division stay defined, but it reports Priced false, zero P&L, and its symbol
// is collected in Summary.Unpriced.
//
// Unpriced positions are EXCLUDED from TotalCost, TotalValue, TotalPnL and
// TotalPnLPct. This is the important part. Folding them in at cost renders a
// holding nobody is pricing as a flawless break-even position and drags the
// portfolio's total P&L percentage toward zero — a wrong number shaped exactly
// like a right one, with nothing on screen to distinguish them. The totals here
// describe the priced subset and nothing else; Unpriced, FullyPriced, Coverage
// and UnpricedCost tell a caller how much of the portfolio that subset is, so a
// partial total cannot be read as a complete one.
//
// Symbols are matched exactly first and then by their canonical spelling, so a
// portfolio configured as "btc" still matches the "BTC-USD" key providers emit.
func Evaluate(holdings []Holding, quotes map[string]provider.Quote) Summary {
	var s Summary
	for _, h := range holdings {
		price, priced := priceFor(quotes, h.Symbol)
		if !priced {
			price = h.CostBasis // keeps formatting and division defined; not a price
		}

		cost := h.Quantity * h.CostBasis
		value := h.Quantity * price
		var pnl, pnlPct float64
		if priced {
			pnl = value - cost
			if cost > 0 {
				pnlPct = (pnl / cost) * 100
			}
		}

		s.Positions = append(s.Positions, Position{
			Holding:      h,
			CurrentPrice: price,
			MarketValue:  value,
			PnL:          pnl,
			PnLPct:       pnlPct,
			Priced:       priced,
		})

		if !priced {
			s.Unpriced = append(s.Unpriced, h.Symbol)
			continue
		}
		s.TotalCost += cost
		s.TotalValue += value
	}

	s.TotalPnL = s.TotalValue - s.TotalCost
	if s.TotalCost > 0 {
		s.TotalPnLPct = (s.TotalPnL / s.TotalCost) * 100
	}
	return s
}

// priceFor resolves a holding's symbol against the quote map and reports
// whether the result is a usable live price. The canonical fallback covers
// config written in a spelling providers do not emit ("btc" vs "BTC-USD").
func priceFor(quotes map[string]provider.Quote, sym string) (float64, bool) {
	q, ok := quotes[sym]
	if !ok {
		if c := symbol.Canonical(sym); c != sym {
			q, ok = quotes[c]
		}
	}
	if !ok {
		return 0, false
	}
	// A zero, negative or NaN price is a provider that has nothing to say, not
	// an asset worth nothing.
	if !(q.Price > 0) || math.IsInf(q.Price, 0) {
		return 0, false
	}
	return q.Price, true
}
