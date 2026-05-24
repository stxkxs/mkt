package portfolio

import "math"

// Correlation returns Pearson's correlation coefficient between two
// equal-length return / price series. Returns NaN when inputs are
// mismatched, shorter than 2, or either series has zero variance.
func Correlation(a, b []float64) float64 {
	if len(a) != len(b) || len(a) < 2 {
		return math.NaN()
	}
	mA, mB := mean(a), mean(b)
	var cov, varA, varB float64
	for i := range a {
		da := a[i] - mA
		db := b[i] - mB
		cov += da * db
		varA += da * da
		varB += db * db
	}
	if varA == 0 || varB == 0 {
		return math.NaN()
	}
	return cov / math.Sqrt(varA*varB)
}

// CorrelationMatrix returns the symmetric NxN matrix of Pearson
// correlations between the columns of `prices`. prices[i] is the
// series for symbols[i]. Series are truncated to the shortest
// length so they align. The diagonal is 1; cells where either series
// has insufficient data are NaN.
func CorrelationMatrix(symbols []string, prices [][]float64) [][]float64 {
	n := len(symbols)
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
	}
	// Truncate to shortest series so windows align
	minLen := math.MaxInt
	for _, s := range prices {
		if len(s) < minLen {
			minLen = len(s)
		}
	}
	if minLen < 2 {
		for i := range out {
			for j := range out[i] {
				if i == j {
					out[i][j] = 1
				} else {
					out[i][j] = math.NaN()
				}
			}
		}
		return out
	}
	trimmed := make([][]float64, n)
	for i, s := range prices {
		trimmed[i] = s[len(s)-minLen:]
	}
	for i := 0; i < n; i++ {
		out[i][i] = 1
		for j := i + 1; j < n; j++ {
			c := Correlation(trimmed[i], trimmed[j])
			out[i][j] = c
			out[j][i] = c
		}
	}
	return out
}
