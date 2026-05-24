package indicator

import "math"

// ATR computes the Average True Range using Wilder's smoothing. True Range
// for bar i is max(H[i]-L[i], |H[i]-C[i-1]|, |L[i]-C[i-1]|). The first
// period entries are NaN.
func ATR(highs, lows, closes []float64, period int) []float64 {
	n := len(closes)
	out := make([]float64, n)
	if period <= 0 || n == 0 ||
		len(highs) != n || len(lows) != n ||
		n < period+1 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	// True Range slice (skip i=0 since we need a previous close)
	tr := make([]float64, n)
	tr[0] = math.NaN()
	for i := 1; i < n; i++ {
		h, l, prevC := highs[i], lows[i], closes[i-1]
		hl := h - l
		hc := math.Abs(h - prevC)
		lc := math.Abs(l - prevC)
		tr[i] = max(hl, max(hc, lc))
	}

	// Warm-up: first period TR values are NaN in the output
	for i := 0; i < period; i++ {
		out[i] = math.NaN()
	}

	// Seed: simple average of the first period TR values (indices 1..period)
	var sum float64
	for i := 1; i <= period; i++ {
		sum += tr[i]
	}
	out[period] = sum / float64(period)

	// Wilder smoothing: ATR_t = (ATR_{t-1}*(period-1) + TR_t) / period
	for i := period + 1; i < n; i++ {
		out[i] = (out[i-1]*float64(period-1) + tr[i]) / float64(period)
	}
	return out
}
