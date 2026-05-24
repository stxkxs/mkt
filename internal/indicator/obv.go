package indicator

// OBV computes On-Balance Volume: a running signed-volume total where
// each candle's volume is added when the close rises and subtracted when
// it falls, leaving the total unchanged on a flat close. The first entry
// is 0 (canonical baseline; no prior close to compare). The result has
// the same length as the input.
func OBV(closes, volumes []float64) []float64 {
	out := make([]float64, len(closes))
	if len(closes) == 0 || len(volumes) != len(closes) {
		return out
	}
	for i := 1; i < len(closes); i++ {
		prev := out[i-1]
		switch {
		case closes[i] > closes[i-1]:
			out[i] = prev + volumes[i]
		case closes[i] < closes[i-1]:
			out[i] = prev - volumes[i]
		default:
			out[i] = prev
		}
	}
	return out
}
