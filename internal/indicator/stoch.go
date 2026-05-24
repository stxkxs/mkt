package indicator

import "math"

// Stochastic computes the Stochastic Oscillator (%K and %D).
// %K[i] = 100 * (C[i] - lowestLow(kPeriod)) / (highestHigh(kPeriod) - lowestLow(kPeriod))
// %D[i] = SMA(K, dPeriod). Warm-up entries are NaN. Output values are
// clamped to [0, 100]; a flat range (highestHigh == lowestLow) yields NaN
// to indicate the indicator is undefined.
func Stochastic(highs, lows, closes []float64, kPeriod, dPeriod int) (k, d []float64) {
	n := len(closes)
	k = make([]float64, n)
	d = make([]float64, n)
	if kPeriod <= 0 || dPeriod <= 0 || n == 0 ||
		len(highs) != n || len(lows) != n {
		for i := range k {
			k[i] = math.NaN()
			d[i] = math.NaN()
		}
		return k, d
	}

	for i := 0; i < n; i++ {
		if i < kPeriod-1 {
			k[i] = math.NaN()
			continue
		}
		hh, ll := highs[i-kPeriod+1], lows[i-kPeriod+1]
		for j := i - kPeriod + 2; j <= i; j++ {
			if highs[j] > hh {
				hh = highs[j]
			}
			if lows[j] < ll {
				ll = lows[j]
			}
		}
		rng := hh - ll
		if rng == 0 {
			k[i] = math.NaN()
			continue
		}
		v := 100 * (closes[i] - ll) / rng
		if v < 0 {
			v = 0
		} else if v > 100 {
			v = 100
		}
		k[i] = v
	}

	d = SMA(k, dPeriod)
	return k, d
}
