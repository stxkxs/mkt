package indicator

import "math"

// MACDResult holds the output of a MACD calculation.
type MACDResult struct {
	MACD      []float64
	Signal    []float64
	Histogram []float64
}

// MACD computes the Moving Average Convergence Divergence.
// Typical params: fast=12, slow=26, signal=9.
func MACD(closes []float64, fast, slow, signal int) MACDResult {
	n := len(closes)
	result := MACDResult{
		MACD:      make([]float64, n),
		Signal:    make([]float64, n),
		Histogram: make([]float64, n),
	}
	if n == 0 {
		return result
	}

	fastEMA := EMA(closes, fast)
	slowEMA := EMA(closes, slow)

	// MACD line = fast EMA - slow EMA
	for i := range n {
		if math.IsNaN(fastEMA[i]) || math.IsNaN(slowEMA[i]) {
			result.MACD[i] = math.NaN()
		} else {
			result.MACD[i] = fastEMA[i] - slowEMA[i]
		}
	}

	// Signal line = EMA of MACD line (only non-NaN portion)
	// Find start of valid MACD values
	validStart := -1
	for i, v := range result.MACD {
		if !math.IsNaN(v) {
			validStart = i
			break
		}
	}
	if validStart < 0 {
		for i := range n {
			result.Signal[i] = math.NaN()
			result.Histogram[i] = math.NaN()
		}
		return result
	}

	validMACD := result.MACD[validStart:]
	signalVals := EMA(validMACD, signal)

	for i := range n {
		if i < validStart {
			result.Signal[i] = math.NaN()
			result.Histogram[i] = math.NaN()
		} else {
			result.Signal[i] = signalVals[i-validStart]
			if math.IsNaN(result.Signal[i]) || math.IsNaN(result.MACD[i]) {
				result.Histogram[i] = math.NaN()
			} else {
				result.Histogram[i] = result.MACD[i] - result.Signal[i]
			}
		}
	}

	return result
}
