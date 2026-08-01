package indicator

import "math"

// ATR computes the Average True Range using Wilder's smoothing.
//
// Reference definition (Wilder): True Range for bar i is
// max(H[i]-L[i], |H[i]-C[i-1]|, |L[i]-C[i-1]|); the series is seeded with the
// simple mean of the first `period` true ranges and then smoothed as
// ATR_t = (ATR_{t-1}*(period-1) + TR_t) / period. Checked against that
// definition — a constant bar range returns that range.
//
// The first period entries are NaN: bar 0 has no previous close, so the seed
// window is bars 1..period. A bar whose true range is not finite is missing
// data — it reports NaN and is left out of both the seed and the smoothing,
// rather than turning every later value into NaN.
func ATR(highs, lows, closes []float64, period int) []float64 {
	n := len(closes)
	out := make([]float64, n)
	if period <= 0 || n == 0 ||
		len(highs) != n || len(lows) != n ||
		n < period+1 {
		return fillNaN(out)
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

	// Seed: simple average of the usable TR values in indices 1..period.
	// Accumulated incrementally — true ranges can be large enough that a
	// plain running sum overflows to +Inf, while the incremental mean stays
	// bounded by the largest sample.
	var atr float64
	var seen int
	for i := 1; i <= period; i++ {
		if !finite(tr[i]) {
			continue
		}
		seen++
		atr += (tr[i] - atr) / float64(seen)
	}
	if seen == 0 {
		return fillNaN(out)
	}
	out[period] = atr

	// Wilder smoothing: ATR_t = (ATR_{t-1}*(period-1) + TR_t) / period,
	// written in the algebraically identical increment form for the same
	// overflow reason.
	for i := period + 1; i < n; i++ {
		if !finite(tr[i]) {
			out[i] = math.NaN()
			continue
		}
		atr += (tr[i] - atr) / float64(period)
		out[i] = atr
	}
	return out
}
