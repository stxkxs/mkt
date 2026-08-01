package indicator

import "math"

// Stddev returns the rolling sample standard deviation over the given period.
//
// Reference definition: the Bessel-corrected sample estimator,
// sqrt(sum((x - mean)^2) / (period-1)) over the window — the two-pass form,
// which avoids the cancellation error of the sum-of-squares shortcut.
// Checked against that definition on the textbook series [2,4,4,4,5,5,7,9],
// whose sample standard deviation is 2.1380899…. Note that Bollinger bands
// deliberately use the *population* estimator instead.
//
// The first period-1 entries are NaN, as is any window holding a non-finite
// value; the series recovers as soon as that value leaves the window. Flat
// windows return 0.
func Stddev(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 1 || len(values) == 0 {
		return fillNaN(out)
	}
	for i := range values {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		start := i - period + 1
		var sum float64
		usable := true
		for j := start; j <= i; j++ {
			if !finite(values[j]) {
				usable = false
				break
			}
			sum += values[j]
		}
		if !usable {
			out[i] = math.NaN()
			continue
		}
		mean := sum / float64(period)
		var sq float64
		for j := start; j <= i; j++ {
			d := values[j] - mean
			sq += d * d
		}
		// Squaring can overflow on absurd magnitudes; an unrepresentable
		// spread is undefined, not infinite.
		if sd := math.Sqrt(sq / float64(period-1)); finite(sd) {
			out[i] = sd
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}
