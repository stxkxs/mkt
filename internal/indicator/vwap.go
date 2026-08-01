package indicator

import "math"

// VWAP computes the running anchored Volume-Weighted Average Price using
// typical price (H+L+C)/3 per candle.
//
// Reference definition: VWAP[i] = sum(TP*V) / sum(V) over bars 0..i, with
// TP = (H+L+C)/3. Checked against that definition — a constant price returns
// that price, and equal volumes reduce it to the running mean.
//
// The result has the same length as the input. Entries return NaN until the
// cumulative volume is strictly positive; a zero-volume prefix has no average
// to report, and negative volume is bad data rather than a short. A candle
// with a non-finite price or volume is skipped instead of poisoning every
// later value. For session VWAP, callers should pass exactly one session's
// data.
func VWAP(highs, lows, closes, volumes []float64) []float64 {
	out := make([]float64, len(closes))
	if len(closes) == 0 ||
		len(highs) != len(closes) ||
		len(lows) != len(closes) ||
		len(volumes) != len(closes) {
		return fillNaN(out)
	}

	var cumPV, cumV float64
	for i := range closes {
		typical := (highs[i] + lows[i] + closes[i]) / 3
		// Commit the bar only if the running totals survive it: the
		// price-volume product can overflow on absurd inputs, and an
		// infinite accumulator would never recover.
		pv, v := typical*volumes[i], volumes[i]
		if finite(typical) && finite(v) && v >= 0 &&
			finite(pv) && finite(cumPV+pv) && finite(cumV+v) {
			cumPV += pv
			cumV += v
		}
		if avg := cumPV / cumV; cumV > 0 && finite(avg) {
			out[i] = avg
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}
