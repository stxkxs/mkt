package portfolio

import "math"

// PositionSize returns the share count and dollar risk for a trade
// sized to risk no more than riskPct of equity between entry and stop.
// riskPct is in percent (1 = 1%). Returns (0, 0) on degenerate input
// (non-positive equity/riskPct, or entry == stop).
func PositionSize(equity, riskPct, entry, stop float64) (shares, dollarRisk float64) {
	if equity <= 0 || riskPct <= 0 {
		return 0, 0
	}
	shareRisk := math.Abs(entry - stop)
	if shareRisk == 0 {
		return 0, 0
	}
	dollarRisk = equity * (riskPct / 100)
	shares = dollarRisk / shareRisk
	return shares, dollarRisk
}

// ATRStop returns an ATR-implied stop price: entry - mult*atr for a long
// trade, entry + mult*atr for a short. Caller is responsible for choosing
// a meaningful ATR value (typically ATR(14) of the chart's candles).
func ATRStop(entry, atr, mult float64, long bool) float64 {
	if long {
		return entry - mult*atr
	}
	return entry + mult*atr
}
