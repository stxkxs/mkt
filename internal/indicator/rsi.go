package indicator

import "math"

// RSI computes the Relative Strength Index using Wilder's smoothing method.
//
// Reference definition (Wilder, New Concepts in Technical Trading Systems):
// the seed average gain/loss is the simple mean of the gains/losses over the
// first `period` deltas, every later bar smooths with
// avg = (avg*(period-1) + current) / period, and RSI = 100 - 100/(1 + avgGain/avgLoss).
// Checked against that definition: an unbroken run of gains reads 100 and an
// unbroken run of losses reads 0.
//
// A window with no movement at all — avgGain == 0 and avgLoss == 0 — carries
// no directional information, so it returns the neutral midpoint 50. The bare
// division-by-zero branch would return 100 there, which makes an `rsi_above`
// rule fire every night and every weekend on any symbol whose polled price
// stops changing. 100 is reserved for the genuine all-gains case.
//
// Returns values in [0, 100]. NaN for entries before the period is filled and
// for any bar whose close is not a finite number; a non-finite close counts as
// neither a gain nor a loss, so one bad tick cannot poison the rest of the
// series.
func RSI(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 0 || len(closes) < period+1 {
		return fillNaN(out)
	}

	// Initialize
	for i := 0; i < period; i++ {
		out[i] = math.NaN()
	}

	// First average gain/loss
	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		gain, loss := splitDelta(closes[i-1], closes[i])
		avgGain += gain
		avgLoss += loss
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	out[period] = wilderRSI(avgGain, avgLoss, closes[period])

	// Subsequent values using Wilder's smoothing
	for i := period + 1; i < len(closes); i++ {
		gain, loss := splitDelta(closes[i-1], closes[i])
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		out[i] = wilderRSI(avgGain, avgLoss, closes[i])
	}

	return out
}

// splitDelta splits the move from prev to curr into its gain and loss parts,
// both non-negative. A non-finite endpoint leaves the move undefined, which
// counts as neither.
func splitDelta(prev, curr float64) (gain, loss float64) {
	if !finite(prev) || !finite(curr) {
		return 0, 0
	}
	delta := curr - prev
	if delta > 0 {
		return delta, 0
	}
	return 0, -delta
}

// wilderRSI turns a smoothed gain/loss pair into the reading for a bar
// closing at c, clamped to the [0, 100] range the index is defined on.
func wilderRSI(avgGain, avgLoss, c float64) float64 {
	if !finite(c) {
		return math.NaN()
	}
	switch {
	case avgGain == 0 && avgLoss == 0:
		// Nothing moved either way: neutral, not maximally overbought.
		return 50
	case avgLoss == 0:
		return 100
	}
	return min(100, max(0, 100-100/(1+avgGain/avgLoss)))
}
