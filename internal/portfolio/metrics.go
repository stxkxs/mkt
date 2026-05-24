package portfolio

import "math"

// Sharpe computes the (un-annualized) Sharpe ratio:
//
//	(mean(returns) - rf) / stddev(returns)
//
// Caller is responsible for choosing return units (daily / weekly) and
// annualizing if desired. Returns NaN when len(returns) < 2 or stddev
// is zero.
func Sharpe(returns []float64, rf float64) float64 {
	if len(returns) < 2 {
		return math.NaN()
	}
	mean := mean(returns)
	sd := sampleStddev(returns, mean)
	if sd == 0 {
		return math.NaN()
	}
	return (mean - rf) / sd
}

// Sortino computes the Sortino ratio — like Sharpe but the denominator
// is the standard deviation of returns below rf (downside deviation).
// Returns NaN when fewer than two downside returns exist or downside
// stddev is zero.
func Sortino(returns []float64, rf float64) float64 {
	if len(returns) < 2 {
		return math.NaN()
	}
	mean := mean(returns)
	var downside []float64
	for _, r := range returns {
		if r < rf {
			downside = append(downside, r-rf)
		}
	}
	if len(downside) < 2 {
		return math.NaN()
	}
	sd := sampleStddev(downside, 0)
	if sd == 0 {
		return math.NaN()
	}
	return (mean - rf) / sd
}

// Beta computes the beta of asset returns vs benchmark returns:
//
//	cov(asset, benchmark) / var(benchmark)
//
// Returns NaN on length mismatch, fewer than two points, or zero
// benchmark variance.
func Beta(asset, benchmark []float64) float64 {
	if len(asset) != len(benchmark) || len(asset) < 2 {
		return math.NaN()
	}
	mA := mean(asset)
	mB := mean(benchmark)
	var cov, varB float64
	for i := range asset {
		da := asset[i] - mA
		db := benchmark[i] - mB
		cov += da * db
		varB += db * db
	}
	n := float64(len(asset) - 1)
	cov /= n
	varB /= n
	if varB == 0 {
		return math.NaN()
	}
	return cov / varB
}

// MaxDrawdown returns the largest peak-to-trough decline observed in
// an equity series as a positive fraction (0.20 = 20%). Empty or
// single-point input returns 0. A monotonically non-decreasing series
// has zero drawdown.
func MaxDrawdown(equity []float64) float64 {
	if len(equity) < 2 {
		return 0
	}
	peak := equity[0]
	var maxDD float64
	for _, v := range equity {
		if v > peak {
			peak = v
		}
		if peak <= 0 {
			continue
		}
		dd := (peak - v) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// sampleStddev computes the sample standard deviation around m. Caller
// passes 0 for m if values are already mean-zero (e.g., downside deltas).
func sampleStddev(xs []float64, m float64) float64 {
	var s float64
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)-1))
}
