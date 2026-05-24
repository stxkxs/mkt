package indicator

// PivotLevels holds the seven classic floor-trader pivot levels.
type PivotLevels struct {
	P  float64 // central pivot
	R1 float64
	R2 float64
	R3 float64
	S1 float64
	S2 float64
	S3 float64
}

// PivotsClassic computes the classic floor-trader pivot levels from a
// prior session's high, low, and close. All seven levels are returned
// from a single HLC tuple; pivots are constant within the current session.
func PivotsClassic(high, low, close float64) PivotLevels {
	p := (high + low + close) / 3
	rng := high - low
	return PivotLevels{
		P:  p,
		R1: 2*p - low,
		S1: 2*p - high,
		R2: p + rng,
		S2: p - rng,
		R3: high + 2*(p-low),
		S3: low - 2*(high-p),
	}
}
