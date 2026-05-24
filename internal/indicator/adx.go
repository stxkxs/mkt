package indicator

import "math"

// ADX computes the Average Directional Index along with +DI and -DI.
// All three series use Wilder smoothing. Warm-up bars are NaN. Values
// lie in [0, 100]. Period defaults to 14 in typical use.
//
// Inputs must be the same length; mismatched or insufficient inputs
// return all NaN.
func ADX(highs, lows, closes []float64, period int) (adx, plusDI, minusDI []float64) {
	n := len(closes)
	adx = make([]float64, n)
	plusDI = make([]float64, n)
	minusDI = make([]float64, n)
	if period <= 0 || n == 0 ||
		len(highs) != n || len(lows) != n ||
		n < 2*period+1 {
		for i := range adx {
			adx[i] = math.NaN()
			plusDI[i] = math.NaN()
			minusDI[i] = math.NaN()
		}
		return adx, plusDI, minusDI
	}

	// Per-bar TR, +DM, -DM (i=0 is undefined; treat as 0)
	tr := make([]float64, n)
	pdm := make([]float64, n)
	mdm := make([]float64, n)
	for i := 1; i < n; i++ {
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])
		tr[i] = max(hl, max(hc, lc))

		upMove := highs[i] - highs[i-1]
		downMove := lows[i-1] - lows[i]
		switch {
		case upMove > downMove && upMove > 0:
			pdm[i] = upMove
		case downMove > upMove && downMove > 0:
			mdm[i] = downMove
		}
	}

	// Wilder-smooth TR, +DM, -DM. Seed at index `period` with the simple
	// sum over indices 1..period.
	smTR := make([]float64, n)
	smPDM := make([]float64, n)
	smMDM := make([]float64, n)
	for i := 0; i < period; i++ {
		smTR[i] = math.NaN()
		smPDM[i] = math.NaN()
		smMDM[i] = math.NaN()
	}
	var sTR, sPDM, sMDM float64
	for i := 1; i <= period; i++ {
		sTR += tr[i]
		sPDM += pdm[i]
		sMDM += mdm[i]
	}
	smTR[period] = sTR
	smPDM[period] = sPDM
	smMDM[period] = sMDM
	for i := period + 1; i < n; i++ {
		smTR[i] = smTR[i-1] - smTR[i-1]/float64(period) + tr[i]
		smPDM[i] = smPDM[i-1] - smPDM[i-1]/float64(period) + pdm[i]
		smMDM[i] = smMDM[i-1] - smMDM[i-1]/float64(period) + mdm[i]
	}

	// +DI, -DI, DX
	dx := make([]float64, n)
	for i := 0; i < n; i++ {
		if i < period {
			plusDI[i] = math.NaN()
			minusDI[i] = math.NaN()
			dx[i] = math.NaN()
			continue
		}
		if smTR[i] == 0 {
			plusDI[i] = 0
			minusDI[i] = 0
			dx[i] = 0
			continue
		}
		plusDI[i] = 100 * smPDM[i] / smTR[i]
		minusDI[i] = 100 * smMDM[i] / smTR[i]
		sum := plusDI[i] + minusDI[i]
		if sum == 0 {
			dx[i] = 0
		} else {
			dx[i] = 100 * math.Abs(plusDI[i]-minusDI[i]) / sum
		}
	}

	// ADX: Wilder-smooth DX starting at 2*period. Warm-up is NaN.
	for i := 0; i < 2*period; i++ {
		adx[i] = math.NaN()
	}
	var seed float64
	for i := period; i < 2*period; i++ {
		seed += dx[i]
	}
	adx[2*period-1] = seed / float64(period)
	for i := 2 * period; i < n; i++ {
		adx[i] = (adx[i-1]*float64(period-1) + dx[i]) / float64(period)
	}
	return adx, plusDI, minusDI
}
