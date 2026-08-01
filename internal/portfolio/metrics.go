package portfolio

import (
	"math"
	"sort"
	"time"
)

// Sharpe computes the (un-annualized) Sharpe ratio:
//
//	(mean(returns) - rf) / stddev(returns)
//
// Caller is responsible for choosing return units (daily / weekly) and
// annualizing if desired. Returns NaN when len(returns) < 2 or stddev
// is zero. Returns and rf must be in the same units — see Returns and
// MarkReturns for turning an equity curve into a returns series.
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

// Sortino computes the Sortino ratio — like Sharpe but the denominator is
// downside deviation rather than total volatility:
//
//	(mean(returns) - rf) / DD
//	DD = sqrt( (1/N) * Σ min(r_i - rf, 0)² )
//
// This is the Sortino & Price definition: the sum runs over every observation,
// and it is divided by the total observation count N, not by the number of
// downside observations. Dividing by the downside count instead (the common
// mistake) inflates the ratio whenever most periods are above target, and makes
// it undefined for a portfolio with a single bad month in twenty — which is
// exactly the portfolio the ratio is meant to describe.
//
// Returns NaN when len(returns) < 2 or when no observation falls below rf, in
// which case downside deviation is zero and the ratio is unbounded.
func Sortino(returns []float64, rf float64) float64 {
	if len(returns) < 2 {
		return math.NaN()
	}
	dd := downsideDeviation(returns, rf)
	if dd == 0 || math.IsNaN(dd) {
		return math.NaN()
	}
	return (mean(returns) - rf) / dd
}

// downsideDeviation is the root-mean-square of the shortfalls below target,
// averaged over every observation (periods at or above target contribute zero).
func downsideDeviation(returns []float64, target float64) float64 {
	if len(returns) == 0 {
		return math.NaN()
	}
	var s float64
	for _, r := range returns {
		d := r - target
		if d < 0 {
			s += d * d
		}
	}
	return math.Sqrt(s / float64(len(returns)))
}

// Beta computes the beta of asset returns vs benchmark returns:
//
//	cov(asset, benchmark) / var(benchmark)
//
// Returns NaN on length mismatch, fewer than two points, or zero
// benchmark variance. Both series must be returns (not price levels) and
// must be aligned period-for-period.
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

// Returns converts a value series (an equity curve, a price series) into simple
// period-over-period returns: r_i = v_i/v_{i-1} - 1. The result is one shorter
// than the input; a nil or single-point input yields nil.
//
// A period whose predecessor is non-positive or non-finite has no defined
// return and is reported as NaN rather than dropped, so the result stays index-
// aligned with the input. Sharpe, Sortino and Beta all want this, not levels.
func Returns(values []float64) []float64 {
	if len(values) < 2 {
		return nil
	}
	out := make([]float64, len(values)-1)
	for i := 1; i < len(values); i++ {
		prev, cur := values[i-1], values[i]
		if !(prev > 0) || math.IsInf(prev, 0) || math.IsNaN(cur) || math.IsInf(cur, 0) {
			out[i-1] = math.NaN()
			continue
		}
		out[i-1] = cur/prev - 1
	}
	return out
}

// LogReturns converts a value series into continuously-compounded returns:
// r_i = ln(v_i/v_{i-1}). Same shape and NaN rules as Returns.
//
// Log returns are the right input for correlation and volatility work: they are
// invariant to the scale of the asset (a $60,000 coin and a $180 stock become
// comparable), they are additive across periods, and unlike price levels they do
// not make any two trending assets look 0.95 correlated.
func LogReturns(values []float64) []float64 {
	if len(values) < 2 {
		return nil
	}
	out := make([]float64, len(values)-1)
	for i := 1; i < len(values); i++ {
		prev, cur := values[i-1], values[i]
		if !(prev > 0) || !(cur > 0) || math.IsInf(prev, 0) || math.IsInf(cur, 0) {
			out[i-1] = math.NaN()
			continue
		}
		out[i-1] = math.Log(cur / prev)
	}
	return out
}

// MarkValues extracts the equity values from recorded marks, chronologically.
// Marks are sorted by time if they are not already.
func MarkValues(marks []EquityMark) []float64 {
	sorted := sortedMarks(marks)
	out := make([]float64, len(sorted))
	for i, m := range sorted {
		out[i] = m.Value
	}
	return out
}

// MarkReturns converts recorded equity marks into simple per-mark returns,
// ready for Sharpe, Sortino or Beta. The returns are per *mark interval*, so rf
// must be quoted in the same units — see StatsFromMarks, which does the
// bookkeeping.
func MarkReturns(marks []EquityMark) []float64 {
	return Returns(MarkValues(marks))
}

// sortedMarks returns marks in chronological order, copying only when the input
// is not already sorted.
func sortedMarks(marks []EquityMark) []EquityMark {
	if len(marks) < 2 {
		return marks
	}
	inOrder := true
	for i := 1; i < len(marks); i++ {
		if marks[i].Time.Before(marks[i-1].Time) {
			inOrder = false
			break
		}
	}
	if inOrder {
		return marks
	}
	out := append([]EquityMark(nil), marks...)
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

const yearDuration = 365 * 24 * time.Hour

// Stats is the risk/return picture of one recorded equity curve. Every ratio is
// reported both per period (the interval between marks) and annualized; a value
// that is not defined for the input is NaN rather than zero, so a caller can
// tell "no downside yet" from "flat".
type Stats struct {
	Marks   int           // equity marks the stats were computed from
	Periods int           // per-period returns (Marks-1)
	First   time.Time     // time of the first mark
	Last    time.Time     // time of the last mark
	Elapsed time.Duration // Last - First

	StartValue  float64
	EndValue    float64
	TotalReturn float64 // fraction over the whole window (0.12 = +12%)

	MeanReturn  float64 // mean per-period simple return
	Volatility  float64 // sample stddev of per-period returns
	Sharpe      float64 // per period
	Sortino     float64 // per period
	MaxDrawdown float64 // positive fraction

	Period         time.Duration // median spacing between marks; 0 when unknown
	PeriodsPerYear float64       // 0 when Period is unknown

	AnnualizedReturn     float64 // geometric, from Elapsed; NaN when undefined
	AnnualizedVolatility float64 // Volatility * sqrt(PeriodsPerYear)
	AnnualizedSharpe     float64 // Sharpe * sqrt(PeriodsPerYear)
}

// StatsFromMarks computes Stats over an equity curve. rf is the risk-free rate
// expressed per mark interval (pass 0 for the excess-return-over-zero
// convention mkt uses by default).
//
// Annualization is derived from the median observed spacing between marks and
// from the wall-clock window the marks span. mkt records a mark every five
// minutes, so a curve that has only been running an afternoon will annualize to
// an absurd number: Elapsed and Period are reported so a caller can decide
// whether the annualized figures are worth showing at all.
func StatsFromMarks(marks []EquityMark, rf float64) Stats {
	sorted := sortedMarks(marks)
	s := Stats{
		Marks:                len(sorted),
		MeanReturn:           math.NaN(),
		Volatility:           math.NaN(),
		Sharpe:               math.NaN(),
		Sortino:              math.NaN(),
		AnnualizedReturn:     math.NaN(),
		AnnualizedVolatility: math.NaN(),
		AnnualizedSharpe:     math.NaN(),
	}
	if len(sorted) == 0 {
		return s
	}

	s.First = sorted[0].Time
	s.Last = sorted[len(sorted)-1].Time
	s.Elapsed = s.Last.Sub(s.First)
	s.StartValue = sorted[0].Value
	s.EndValue = sorted[len(sorted)-1].Value
	s.TotalReturn = math.NaN()
	if s.StartValue > 0 {
		s.TotalReturn = s.EndValue/s.StartValue - 1
	}

	values := MarkValues(sorted)
	s.MaxDrawdown = MaxDrawdown(values)

	rets := Returns(values)
	s.Periods = len(rets)
	if len(rets) >= 2 {
		s.MeanReturn = mean(rets)
		s.Volatility = sampleStddev(rets, s.MeanReturn)
		s.Sharpe = Sharpe(rets, rf)
		s.Sortino = Sortino(rets, rf)
	}

	s.Period = medianSpacing(sorted)
	if s.Period > 0 {
		s.PeriodsPerYear = float64(yearDuration) / float64(s.Period)
		root := math.Sqrt(s.PeriodsPerYear)
		s.AnnualizedVolatility = s.Volatility * root
		s.AnnualizedSharpe = s.Sharpe * root
	}
	if s.Elapsed > 0 && s.StartValue > 0 && s.EndValue > 0 {
		s.AnnualizedReturn = math.Pow(s.EndValue/s.StartValue, float64(yearDuration)/float64(s.Elapsed)) - 1
	}
	return s
}

// medianSpacing is the median gap between consecutive marks. The median rather
// than the mean because a restarted process leaves one enormous gap in the file
// and that must not redefine the sampling interval.
func medianSpacing(marks []EquityMark) time.Duration {
	if len(marks) < 2 {
		return 0
	}
	gaps := make([]time.Duration, 0, len(marks)-1)
	for i := 1; i < len(marks); i++ {
		if d := marks[i].Time.Sub(marks[i-1].Time); d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	return gaps[len(gaps)/2]
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
