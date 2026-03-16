package indicator

import "math"

// SMA computes a Simple Moving Average over the given period.
// Returns NaN for entries before the period is filled.
func SMA(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 0 || len(closes) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	var sum float64
	for i := range closes {
		sum += closes[i]
		if i < period-1 {
			out[i] = math.NaN()
		} else {
			if i >= period {
				sum -= closes[i-period]
			}
			out[i] = sum / float64(period)
		}
	}
	return out
}

// EMA computes an Exponential Moving Average over the given period.
// Returns NaN for entries before the period is filled.
func EMA(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 0 || len(closes) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	k := 2.0 / float64(period+1)
	var sum float64
	for i := range closes {
		if i < period-1 {
			sum += closes[i]
			out[i] = math.NaN()
		} else if i == period-1 {
			sum += closes[i]
			out[i] = sum / float64(period)
		} else {
			out[i] = closes[i]*k + out[i-1]*(1-k)
		}
	}
	return out
}
