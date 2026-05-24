package indicator

import "math"

// Stddev returns the rolling sample standard deviation over the given
// period. The first period-1 entries are NaN. Flat windows return 0.
// Sample standard deviation divides by (period-1).
func Stddev(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 1 || len(values) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	for i := range values {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		start := i - period + 1
		var sum float64
		for j := start; j <= i; j++ {
			sum += values[j]
		}
		mean := sum / float64(period)
		var sq float64
		for j := start; j <= i; j++ {
			d := values[j] - mean
			sq += d * d
		}
		out[i] = math.Sqrt(sq / float64(period-1))
	}
	return out
}
