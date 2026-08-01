package indicator

import "math"

// SMA computes a Simple Moving Average over the given period.
//
// Reference definition: SMA[i] = mean(closes[i-period+1 .. i]). Checked
// against that definition — the running sum below adds the entering sample
// and evicts exactly the one leaving the window.
//
// Returns NaN for entries before the period is filled. A window holding a
// non-finite sample is undefined and returns NaN, and the average recovers
// the moment that sample leaves the window: non-finite values are counted,
// never folded into the running sum, so one NaN cannot poison every later
// value. Stochastic %D depends on this — it is an SMA over a %K series whose
// warm-up bars are NaN by construction.
func SMA(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 0 || len(closes) == 0 {
		return fillNaN(out)
	}
	var sum float64
	var missing int // non-finite samples currently inside the window
	for i := range closes {
		if finite(closes[i]) {
			sum += closes[i]
		} else {
			missing++
		}
		if i >= period {
			if left := closes[i-period]; finite(left) {
				sum -= left
			} else {
				missing--
			}
		}
		switch {
		case i < period-1, missing > 0:
			out[i] = math.NaN()
		default:
			out[i] = sum / float64(period)
		}
	}
	return out
}

// EMA computes an Exponential Moving Average over the given period.
//
// Reference definition: seeded with the simple mean of the first `period`
// samples, then EMA[i] = close[i]*k + EMA[i-1]*(1-k) with k = 2/(period+1).
// Checked against that definition — a constant series stays on the constant
// and the first emitted value equals the SMA of the seed window.
//
// Returns NaN for entries before the period is filled. A non-finite sample is
// missing data: it returns NaN and restarts the warm-up, so the recursion
// re-seeds from the samples that follow instead of carrying NaN forever.
func EMA(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 0 || len(closes) == 0 {
		return fillNaN(out)
	}
	k := 2.0 / float64(period+1)
	var sum float64  // seed accumulator for the current warm-up
	var seen int     // finite samples since the last gap
	var prev float64 // last emitted EMA value
	seeded := false
	for i := range closes {
		c := closes[i]
		if !finite(c) {
			out[i] = math.NaN()
			sum, seen, seeded = 0, 0, false
			continue
		}
		if seeded {
			prev = c*k + prev*(1-k)
			out[i] = prev
			continue
		}
		sum += c
		seen++
		if seen < period {
			out[i] = math.NaN()
			continue
		}
		prev = sum / float64(period)
		out[i] = prev
		seeded = true
	}
	return out
}
