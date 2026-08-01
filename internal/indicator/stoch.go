package indicator

import "math"

// Stochastic computes the Stochastic Oscillator (%K and %D).
//
// Reference definition (Lane):
// %K[i] = 100 * (C[i] - lowestLow(kPeriod)) / (highestHigh(kPeriod) - lowestLow(kPeriod))
// and %D = a dPeriod simple moving average of %K. Checked against that
// definition — a close at the window high reads 100, a close at the window
// low reads 0, and %D is the plain mean of the last dPeriod %K readings.
//
// Warm-up entries are NaN: %K needs kPeriod bars, and %D needs a further
// dPeriod-1 bars on top of that before it has a full window of valid %K
// values. Output values are clamped to [0, 100]. A window whose range is not
// strictly positive — flat, or bad data with the high below the low — leaves
// %K undefined and yields NaN, which blanks only the dPeriod %D values whose
// window overlaps it; SMA is NaN-aware, so the rest of the %D series is
// unaffected.
func Stochastic(highs, lows, closes []float64, kPeriod, dPeriod int) (k, d []float64) {
	n := len(closes)
	k = make([]float64, n)
	d = make([]float64, n)
	if kPeriod <= 0 || dPeriod <= 0 || n == 0 ||
		len(highs) != n || len(lows) != n {
		return fillNaN(k), fillNaN(d)
	}

	for i := 0; i < n; i++ {
		if i < kPeriod-1 {
			k[i] = math.NaN()
			continue
		}
		hh, ll := highs[i-kPeriod+1], lows[i-kPeriod+1]
		for j := i - kPeriod + 2; j <= i; j++ {
			if highs[j] > hh {
				hh = highs[j]
			}
			if lows[j] < ll {
				ll = lows[j]
			}
		}
		// Negated so a NaN range (missing data) is rejected too.
		rng := hh - ll
		if !(rng > 0) {
			k[i] = math.NaN()
			continue
		}
		// Both comparisons are false for a NaN close, so an undefined
		// reading stays NaN rather than being clamped into range.
		v := 100 * (closes[i] - ll) / rng
		if v < 0 {
			v = 0
		} else if v > 100 {
			v = 100
		}
		k[i] = v
	}

	d = SMA(k, dPeriod)
	return k, d
}
