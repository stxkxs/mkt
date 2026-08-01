package indicator

import "math"

// Indicators in this package share one rule for bad samples: a value that is
// not a finite real number is missing data, never a number to compute with.
//
// Rolling-window indicators (SMA, Stddev, Bollinger) report NaN for any
// window that contains missing data and recover as soon as it leaves the
// window. Accumulating indicators (EMA, RSI, ATR, ADX, VWAP, OBV) leave the
// missing sample out of their running state instead of folding it in, so a
// single bad tick can never turn every later value into NaN. That guarantee
// is load-bearing: Stochastic %D is an SMA over a %K series whose warm-up
// bars are NaN by construction.

// finite reports whether v is a usable real number — neither NaN nor an
// infinity.
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// fillNaN sets every entry of s to NaN and returns it, for the shared
// "input is unusable" paths.
func fillNaN(s []float64) []float64 {
	for i := range s {
		s[i] = math.NaN()
	}
	return s
}
