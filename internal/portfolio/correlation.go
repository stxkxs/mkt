package portfolio

import (
	"math"
	"sort"
	"time"
)

// maxAlignBuckets bounds the grid Align builds so a tiny bucket over a long
// window cannot allocate without limit. When the request exceeds it, the most
// recent maxAlignBuckets buckets are returned.
const maxAlignBuckets = 4096

// Correlation returns Pearson's correlation coefficient between two
// equal-length series. Returns NaN when inputs are mismatched, shorter
// than 2, either series has zero variance, or either contains a
// non-finite value.
//
// This is the raw statistical primitive and it correlates whatever it is
// given. Do not hand it price levels: two assets that merely both drifted
// upward over the window will read above 0.9 no matter how unrelated their
// day-to-day moves were. Feed it LogReturns, or use CorrelationSeries, which
// does that for you.
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

// pearsonComplete is Correlation over the observations where both series are
// finite (pairwise-complete deletion). Return series legitimately carry NaN for
// periods with no defined return, and one such period must not blank the whole
// coefficient.
func pearsonComplete(a, b []float64) float64 {
	if len(a) != len(b) {
		return math.NaN()
	}
	xs := make([]float64, 0, len(a))
	ys := make([]float64, 0, len(b))
	for i := range a {
		if math.IsNaN(a[i]) || math.IsInf(a[i], 0) || math.IsNaN(b[i]) || math.IsInf(b[i], 0) {
			continue
		}
		xs = append(xs, a[i])
		ys = append(ys, b[i])
	}
	return Correlation(xs, ys)
}

// CorrelationReturns returns the correlation of two price series' log returns,
// aligned by index. Use it when the two series are known to be sampled on the
// same clock; when they are not, use CorrelationSeries, which aligns on time.
func CorrelationReturns(a, b []float64) float64 {
	if len(a) != len(b) || len(a) < 3 {
		return math.NaN()
	}
	return pearsonComplete(LogReturns(a), LogReturns(b))
}

// CorrelationMatrix returns the symmetric NxN matrix of correlations between
// the columns of `prices`, computed over each series' LOG RETURNS. prices[i] is
// the price series for symbols[i]. Series are truncated to the shortest length
// so their windows line up. The diagonal is 1; cells where either series has
// fewer than three prices (two returns) are NaN.
//
// The matrix is index-aligned, which is only meaningful when the caller's series
// are sampled on the same clock. mkt's quote cache is not: crypto ticks arrive
// several times a second and stocks are polled every fifteen, so a 60-slot
// crypto window covers minutes while a 60-slot stock window covers a quarter of
// an hour, and comparing them slot-by-slot compares different spans of time. Use
// CorrelationMatrixSeries with timestamped samples when correctness matters;
// this function remains for callers that only have bare price slices.
func CorrelationMatrix(symbols []string, prices [][]float64) [][]float64 {
	n := len(symbols)
	out := emptyMatrix(n)
	if n == 0 || len(prices) < n {
		return out
	}

	// Truncate to shortest series so windows align
	minLen := math.MaxInt
	for _, s := range prices[:n] {
		if len(s) < minLen {
			minLen = len(s)
		}
	}
	// Three prices give two returns, the minimum a correlation needs.
	if minLen < 3 {
		return out
	}
	returns := make([][]float64, n)
	for i := range n {
		s := prices[i]
		returns[i] = LogReturns(s[len(s)-minLen:])
	}
	fillMatrix(out, returns)
	return out
}

// Sample is one observation of a price at a point in time. It is what a
// timestamp-aware correlation needs and what market.Cache.Series provides.
type Sample struct {
	Time  time.Time
	Price float64
}

// SamplesFrom pairs a price series with the timestamps each price carried, as
// returned together by market.Cache.Series. Entries with a zero timestamp
// (a backfilled price that has no time) or a non-positive price are dropped:
// they cannot be placed on a time axis, and guessing where they belong is how
// misaligned windows get compared in the first place. The result is sorted
// chronologically.
func SamplesFrom(prices []float64, times []time.Time) []Sample {
	n := min(len(prices), len(times))
	out := make([]Sample, 0, n)
	for i := range n {
		if times[i].IsZero() || !(prices[i] > 0) || math.IsInf(prices[i], 0) {
			continue
		}
		out = append(out, Sample{Time: times[i], Price: prices[i]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// Align resamples timestamped series onto one shared time grid so they can be
// compared period for period regardless of how often each was sampled.
//
// The grid is spaced by bucket and spans the interval every series covers (from
// the latest first observation to the earliest last one), snapped to bucket
// boundaries. Each series is carried forward: a bucket takes the most recent
// observation at or before its timestamp, which is the standard treatment for a
// slower feed and never invents a value the series had not yet reported.
//
// It returns the grid timestamps and one aligned price series per input, in
// input order and all the same length. When the series do not overlap, or fewer
// than two buckets fit, it returns nil. When the window would need more than
// maxAlignBuckets buckets, the most recent maxAlignBuckets are returned.
func Align(series [][]Sample, bucket time.Duration) ([]time.Time, [][]float64) {
	if len(series) == 0 || bucket <= 0 {
		return nil, nil
	}

	sorted := make([][]Sample, len(series))
	var start, end time.Time
	for i, s := range series {
		s = sortedSamples(s)
		if len(s) == 0 {
			return nil, nil
		}
		sorted[i] = s
		if first := s[0].Time; start.IsZero() || first.After(start) {
			start = first
		}
		if last := s[len(s)-1].Time; end.IsZero() || last.Before(end) {
			end = last
		}
	}
	if !end.After(start) {
		return nil, nil
	}

	// Snap the first grid point up to a bucket boundary so every run over the
	// same data produces the same grid.
	first := start.Truncate(bucket)
	if first.Before(start) {
		first = first.Add(bucket)
	}
	if !end.After(first) {
		return nil, nil
	}
	count := int(end.Sub(first)/bucket) + 1
	if count < 2 {
		return nil, nil
	}
	if count > maxAlignBuckets {
		first = first.Add(time.Duration(count-maxAlignBuckets) * bucket)
		count = maxAlignBuckets
	}

	times := make([]time.Time, count)
	for i := range count {
		times[i] = first.Add(time.Duration(i) * bucket)
	}

	aligned := make([][]float64, len(sorted))
	for i, s := range sorted {
		vals := make([]float64, count)
		pos := 0
		last := math.NaN()
		for b, t := range times {
			for pos < len(s) && !s[pos].Time.After(t) {
				last = s[pos].Price
				pos++
			}
			vals[b] = last
		}
		aligned[i] = vals
	}
	return times, aligned
}

// CorrelationSeries returns the correlation of two timestamped price series'
// log returns after resampling both onto a shared bucket grid. This is the
// correct way to correlate a 5-second crypto stream against a 15-second stock
// poll: pick a bucket at least as coarse as the slower feed. Returns NaN when
// the series do not overlap enough to produce two comparable returns.
func CorrelationSeries(a, b []Sample, bucket time.Duration) float64 {
	_, aligned := Align([][]Sample{a, b}, bucket)
	if len(aligned) != 2 || len(aligned[0]) < 3 {
		return math.NaN()
	}
	return pearsonComplete(LogReturns(aligned[0]), LogReturns(aligned[1]))
}

// CorrelationMatrixSeries is CorrelationMatrix over timestamped samples: it
// aligns every series onto a shared bucket grid before correlating their log
// returns, so symbols sampled at different rates are compared over the same
// spans of time. series[i] belongs to symbols[i]. The diagonal is 1; cells that
// cannot be computed are NaN.
func CorrelationMatrixSeries(symbols []string, series [][]Sample, bucket time.Duration) [][]float64 {
	n := len(symbols)
	out := emptyMatrix(n)
	if n == 0 || len(series) < n {
		return out
	}
	_, aligned := Align(series[:n], bucket)
	if len(aligned) != n || len(aligned[0]) < 3 {
		return out
	}
	returns := make([][]float64, n)
	for i := range n {
		returns[i] = LogReturns(aligned[i])
	}
	fillMatrix(out, returns)
	return out
}

// emptyMatrix builds an NxN matrix with 1 on the diagonal and NaN elsewhere —
// the "nothing could be computed" answer every correlation entry point falls
// back to.
func emptyMatrix(n int) [][]float64 {
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
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

// fillMatrix writes the pairwise correlations of returns into out.
func fillMatrix(out [][]float64, returns [][]float64) {
	for i := range returns {
		out[i][i] = 1
		for j := i + 1; j < len(returns); j++ {
			c := pearsonComplete(returns[i], returns[j])
			out[i][j] = c
			out[j][i] = c
		}
	}
}

// sortedSamples returns samples chronologically, copying only when needed.
func sortedSamples(s []Sample) []Sample {
	if len(s) < 2 {
		return s
	}
	inOrder := true
	for i := 1; i < len(s); i++ {
		if s[i].Time.Before(s[i-1].Time) {
			inOrder = false
			break
		}
	}
	if inOrder {
		return s
	}
	out := append([]Sample(nil), s...)
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}
