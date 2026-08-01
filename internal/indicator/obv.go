package indicator

// OBV computes On-Balance Volume: a running signed-volume total where
// each candle's volume is added when the close rises and subtracted when
// it falls, leaving the total unchanged on a flat close.
//
// Reference definition (Granville): OBV[i] = OBV[i-1] ± V[i] by the sign of
// C[i] - C[i-1]. Checked against that definition — an unbroken advance sums
// every volume after the first bar and an unbroken decline negates it.
//
// The first entry is 0 (canonical baseline; no prior close to compare). A
// candle with a non-finite close or volume leaves the total unchanged, so one
// bad tick cannot turn the rest of the series into NaN. The result has the
// same length as the input.
func OBV(closes, volumes []float64) []float64 {
	out := make([]float64, len(closes))
	if len(closes) == 0 || len(volumes) != len(closes) {
		return out
	}
	for i := 1; i < len(closes); i++ {
		prev := out[i-1]
		if !finite(closes[i]) || !finite(closes[i-1]) || !finite(volumes[i]) {
			out[i] = prev
			continue
		}
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
