package indicator

import "math"

// BollingerResult holds Bollinger Bands output.
type BollingerResult struct {
	Upper  []float64
	Middle []float64
	Lower  []float64
}

// Bollinger computes Bollinger Bands.
// Typical params: period=20, mult=2.0.
func Bollinger(closes []float64, period int, mult float64) BollingerResult {
	n := len(closes)
	result := BollingerResult{
		Upper:  make([]float64, n),
		Middle: make([]float64, n),
		Lower:  make([]float64, n),
	}
	if period <= 0 || n == 0 {
		for i := range n {
			result.Upper[i] = math.NaN()
			result.Middle[i] = math.NaN()
			result.Lower[i] = math.NaN()
		}
		return result
	}

	sma := SMA(closes, period)

	for i := range n {
		if math.IsNaN(sma[i]) {
			result.Upper[i] = math.NaN()
			result.Middle[i] = math.NaN()
			result.Lower[i] = math.NaN()
			continue
		}
		// Standard deviation over the window
		var sumSq float64
		start := i - period + 1
		for j := start; j <= i; j++ {
			diff := closes[j] - sma[i]
			sumSq += diff * diff
		}
		sd := math.Sqrt(sumSq / float64(period))

		result.Middle[i] = sma[i]
		result.Upper[i] = sma[i] + mult*sd
		result.Lower[i] = sma[i] - mult*sd
	}

	return result
}
