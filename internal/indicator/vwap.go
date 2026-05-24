package indicator

import "math"

// VWAP computes the running anchored Volume-Weighted Average Price using
// typical price (H+L+C)/3 per candle. The result has the same length as
// the input. Entries return NaN until cumulative volume becomes non-zero.
// For session VWAP, callers should pass exactly one session's data.
func VWAP(highs, lows, closes, volumes []float64) []float64 {
	out := make([]float64, len(closes))
	if len(closes) == 0 ||
		len(highs) != len(closes) ||
		len(lows) != len(closes) ||
		len(volumes) != len(closes) {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	var cumPV, cumV float64
	for i := range closes {
		typical := (highs[i] + lows[i] + closes[i]) / 3
		cumPV += typical * volumes[i]
		cumV += volumes[i]
		if cumV == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = cumPV / cumV
		}
	}
	return out
}
