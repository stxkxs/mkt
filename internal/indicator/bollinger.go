package indicator

import "math"

// BollingerResult holds Bollinger Bands output.
type BollingerResult struct {
	Upper  []float64
	Middle []float64
	Lower  []float64
}

// Bollinger computes Bollinger Bands: a `period` simple moving average with
// bands at ±mult standard deviations of the same window.
//
// Reference definition (Bollinger): the band offset uses the *population*
// standard deviation of the window — divide the squared deviations by period,
// not by period-1 as the sample estimator in Stddev does. Checked against
// that definition: a constant series collapses both bands onto the average.
//
// Typical params: period=20, mult=2.0. Warm-up bars, any window holding a
// non-finite close (SMA reports those as NaN), and a non-finite mult all
// return NaN on all three bands, so nothing non-finite reaches a renderer.
func Bollinger(closes []float64, period int, mult float64) BollingerResult {
	n := len(closes)
	result := BollingerResult{
		Upper:  make([]float64, n),
		Middle: make([]float64, n),
		Lower:  make([]float64, n),
	}
	if period <= 0 || n == 0 || !finite(mult) {
		fillNaN(result.Upper)
		fillNaN(result.Middle)
		fillNaN(result.Lower)
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
		// Population standard deviation over the window
		var sumSq float64
		start := i - period + 1
		for j := start; j <= i; j++ {
			diff := closes[j] - sma[i]
			sumSq += diff * diff
		}
		sd := math.Sqrt(sumSq / float64(period))

		// Squaring can overflow on absurd magnitudes; unrepresentable
		// bands are undefined, not infinite.
		upper, lower := sma[i]+mult*sd, sma[i]-mult*sd
		if !finite(upper) || !finite(lower) {
			result.Upper[i] = math.NaN()
			result.Middle[i] = math.NaN()
			result.Lower[i] = math.NaN()
			continue
		}

		result.Middle[i] = sma[i]
		result.Upper[i] = upper
		result.Lower[i] = lower
	}

	return result
}
