package indicator

import "math"

// RSI computes the Relative Strength Index using Wilder's smoothing method.
// Returns values in [0, 100]. NaN for entries before the period is filled.
func RSI(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 0 || len(closes) < period+1 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	// Initialize
	for i := 0; i < period; i++ {
		out[i] = math.NaN()
	}

	// First average gain/loss
	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		delta := closes[i] - closes[i-1]
		if delta > 0 {
			avgGain += delta
		} else {
			avgLoss -= delta
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		out[period] = 100
	} else {
		rs := avgGain / avgLoss
		out[period] = 100 - 100/(1+rs)
	}

	// Subsequent values using Wilder's smoothing
	for i := period + 1; i < len(closes); i++ {
		delta := closes[i] - closes[i-1]
		var gain, loss float64
		if delta > 0 {
			gain = delta
		} else {
			loss = -delta
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)

		if avgLoss == 0 {
			out[i] = 100
		} else {
			rs := avgGain / avgLoss
			out[i] = 100 - 100/(1+rs)
		}
	}

	return out
}
