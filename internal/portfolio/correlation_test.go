package portfolio

import (
	"math"
	"testing"
	"time"
)

// fromLogReturns rebuilds a price series whose log returns are exactly xs.
func fromLogReturns(start float64, xs []float64) []float64 {
	out := make([]float64, len(xs)+1)
	out[0] = start
	acc := math.Log(start)
	for i, x := range xs {
		acc += x
		out[i+1] = math.Exp(acc)
	}
	return out
}

func TestCorrelationPerfect(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{2, 4, 6, 8, 10}
	got := Correlation(a, b)
	if math.Abs(got-1) > 1e-9 {
		t.Errorf("perfectly correlated: got %v want 1", got)
	}
}

func TestCorrelationInverse(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{5, 4, 3, 2, 1}
	got := Correlation(a, b)
	if math.Abs(got+1) > 1e-9 {
		t.Errorf("perfectly inverse: got %v want -1", got)
	}
}

func TestCorrelationFlatYieldsNaN(t *testing.T) {
	got := Correlation([]float64{1, 2, 3}, []float64{5, 5, 5})
	if !math.IsNaN(got) {
		t.Errorf("flat series should yield NaN, got %v", got)
	}
}

func TestCorrelationLengthMismatch(t *testing.T) {
	if !math.IsNaN(Correlation([]float64{1, 2, 3}, []float64{1, 2})) {
		t.Error("length mismatch should yield NaN")
	}
}

func TestPearsonCompleteSkipsUndefinedPeriods(t *testing.T) {
	a := []float64{math.NaN(), 1, 2, 3, 4}
	b := []float64{0, 2, 4, 6, 8}
	got := pearsonComplete(a, b)
	if math.Abs(got-1) > 1e-9 {
		t.Errorf("got %v, want 1 — one undefined period must not blank the whole coefficient", got)
	}
}

func TestLogReturns(t *testing.T) {
	t.Run("scale invariant", func(t *testing.T) {
		a := LogReturns([]float64{100, 110, 121})
		b := LogReturns([]float64{1, 1.1, 1.21})
		if len(a) != 2 || len(b) != 2 {
			t.Fatalf("len = %d/%d, want 2/2", len(a), len(b))
		}
		for i := range a {
			if math.Abs(a[i]-b[i]) > 1e-12 {
				t.Fatalf("log returns %v vs %v differ at %d", a, b, i)
			}
			if math.Abs(a[i]-math.Log(1.1)) > 1e-12 {
				t.Fatalf("a[%d] = %v, want ln(1.1)", i, a[i])
			}
		}
	})

	t.Run("undefined periods are NaN but keep alignment", func(t *testing.T) {
		got := LogReturns([]float64{10, 0, 20, 40})
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if !math.IsNaN(got[0]) || !math.IsNaN(got[1]) {
			t.Errorf("periods touching a zero price should be NaN, got %v", got)
		}
		if math.Abs(got[2]-math.Log(2)) > 1e-12 {
			t.Errorf("got[2] = %v, want ln(2)", got[2])
		}
	})

	t.Run("short input", func(t *testing.T) {
		if LogReturns(nil) != nil || LogReturns([]float64{1}) != nil {
			t.Error("fewer than two points should yield nil")
		}
	})
}

func TestReturns(t *testing.T) {
	got := Returns([]float64{100, 110, 99})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if math.Abs(got[0]-0.1) > 1e-12 || math.Abs(got[1]+0.1) > 1e-12 {
		t.Errorf("got %v, want [0.1 -0.1]", got)
	}
	if r := Returns([]float64{0, 5}); !math.IsNaN(r[0]) {
		t.Errorf("return off a zero base = %v, want NaN", r[0])
	}
}

// TestCorrelationMatrixUsesReturnsNotLevels is the regression for the reported
// bug: two assets that merely both drifted upward used to read above 0.95
// regardless of whether their moves had anything to do with each other.
func TestCorrelationMatrixUsesReturnsNotLevels(t *testing.T) {
	// Both trend up; their period-to-period moves are unrelated.
	a := fromLogReturns(100, []float64{0.03, 0.01, 0.04, 0.01, 0.03, 0.01, 0.04})
	b := fromLogReturns(50, []float64{0.01, 0.04, 0.01, 0.03, 0.01, 0.04, 0.01})

	levels := Correlation(a, b)
	if levels < 0.9 {
		t.Fatalf("precondition: price levels should look highly correlated, got %v", levels)
	}

	m := CorrelationMatrix([]string{"A", "B"}, [][]float64{a, b})
	if math.IsNaN(m[0][1]) {
		t.Fatal("expected a computable correlation")
	}
	if m[0][1] > 0 {
		t.Errorf("returns correlation = %v, want negative — the two series alternate", m[0][1])
	}
	if math.Abs(m[0][1]-levels) < 0.5 {
		t.Errorf("returns correlation %v is still tracking the level correlation %v", m[0][1], levels)
	}
}

func TestCorrelationMatrixDiagonalIsOne(t *testing.T) {
	xs := []float64{0.02, -0.01, 0.03, -0.02}
	a := fromLogReturns(100, xs)
	b := fromLogReturns(100, []float64{-0.02, 0.01, -0.03, 0.02}) // exactly -xs
	c := make([]float64, len(a))                                  // 2x A: same returns, different scale
	for i, v := range a {
		c[i] = 2 * v
	}

	m := CorrelationMatrix([]string{"A", "B", "C"}, [][]float64{a, b, c})
	for i := range 3 {
		if math.Abs(m[i][i]-1) > 1e-9 {
			t.Errorf("diagonal [%d][%d] = %v, want 1", i, i, m[i][i])
		}
	}
	// A vs C: identical returns at a different price scale.
	if math.Abs(m[0][2]-1) > 1e-9 {
		t.Errorf("A vs C: got %v want 1 (correlation must be scale invariant)", m[0][2])
	}
	// A vs B: mirrored returns.
	if math.Abs(m[0][1]+1) > 1e-9 {
		t.Errorf("A vs B: got %v want -1", m[0][1])
	}
	if m[1][0] != m[0][1] {
		t.Errorf("not symmetric")
	}
}

func TestCorrelationMatrixTrimsToShortest(t *testing.T) {
	syms := []string{"A", "B"}
	prices := [][]float64{
		{1, 2, 3, 4, 5},
		{6, 8, 10}, // 2x A's last three → identical returns over the trimmed window
	}
	m := CorrelationMatrix(syms, prices)
	if math.Abs(m[0][1]-1) > 1e-9 {
		t.Errorf("expected 1 on trimmed window, got %v", m[0][1])
	}
}

func TestCorrelationMatrixInsufficientData(t *testing.T) {
	syms := []string{"A", "B"}
	// Two prices give one return, which cannot be correlated.
	for _, prices := range [][][]float64{{{1}, {2}}, {{1, 2}, {2, 4}}} {
		m := CorrelationMatrix(syms, prices)
		if m[0][0] != 1 || !math.IsNaN(m[0][1]) {
			t.Errorf("insufficient data %v: want diagonal=1 off-diag=NaN, got [%v %v]", prices, m[0][0], m[0][1])
		}
	}
}

func TestCorrelationMatrixDegenerateInputs(t *testing.T) {
	if m := CorrelationMatrix(nil, nil); len(m) != 0 {
		t.Errorf("no symbols should yield an empty matrix, got %v", m)
	}
	// Fewer series than symbols must not panic.
	m := CorrelationMatrix([]string{"A", "B"}, [][]float64{{1, 2, 3}})
	if m[0][0] != 1 || !math.IsNaN(m[0][1]) {
		t.Errorf("missing series: got %v", m)
	}
}

func TestSamplesFromDropsUnplaceableObservations(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	prices := []float64{10, 11, 12, 13}
	times := []time.Time{base.Add(time.Second), {}, base, base.Add(2 * time.Second)}

	got := SamplesFrom(prices, times)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (the zero-timestamped price is dropped)", len(got))
	}
	// Sorted chronologically regardless of input order.
	if !got[0].Time.Equal(base) || got[0].Price != 12 {
		t.Errorf("first sample = %+v, want the base-time price 12", got[0])
	}
	if got[2].Price != 13 {
		t.Errorf("last sample = %+v, want 13", got[2])
	}

	// A non-positive price has no log return and is dropped too.
	if s := SamplesFrom([]float64{0, 1}, []time.Time{base, base.Add(time.Second)}); len(s) != 1 {
		t.Errorf("non-positive price should be dropped, got %v", s)
	}
}

// TestAlignComparesEqualSpansOfTime is the regression for correlating a 5s
// crypto stream against a 15s stock poll by slice index: the two windows cover
// wildly different spans, and only a time-aware alignment can compare them.
func TestAlignComparesEqualSpansOfTime(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	var btc, aapl []Sample
	for i := range 13 { // 5s ticks spanning 60s
		btc = append(btc, Sample{Time: base.Add(time.Duration(i*5) * time.Second), Price: 100 + float64(i)})
	}
	for i := range 5 { // 5 polls * 15s = 60s
		aapl = append(aapl, Sample{Time: base.Add(time.Duration(i*15) * time.Second), Price: 200 + float64(i)})
	}

	times, aligned := Align([][]Sample{btc, aapl}, 15*time.Second)
	if len(aligned) != 2 {
		t.Fatalf("aligned %d series, want 2", len(aligned))
	}
	if len(times) != len(aligned[0]) || len(aligned[0]) != len(aligned[1]) {
		t.Fatalf("ragged output: times=%d a=%d b=%d", len(times), len(aligned[0]), len(aligned[1]))
	}
	if len(times) != 5 {
		t.Fatalf("grid = %d buckets, want 5 (60s at 15s)", len(times))
	}
	for i := range times {
		want := base.Add(time.Duration(i*15) * time.Second)
		if !times[i].Equal(want) {
			t.Fatalf("times[%d] = %v, want %v", i, times[i], want)
		}
	}
	// The fast series is sampled, not truncated: bucket i takes the tick at
	// or before that instant.
	wantBTC := []float64{100, 103, 106, 109, 112}
	for i, w := range wantBTC {
		if aligned[0][i] != w {
			t.Fatalf("btc aligned = %v, want %v", aligned[0], wantBTC)
		}
	}
	// The slow series is carried forward unchanged.
	wantAAPL := []float64{200, 201, 202, 203, 204}
	for i, w := range wantAAPL {
		if aligned[1][i] != w {
			t.Fatalf("aapl aligned = %v, want %v", aligned[1], wantAAPL)
		}
	}
}

func TestAlignCarriesForwardAcrossGaps(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	slow := []Sample{
		{Time: base, Price: 10},
		{Time: base.Add(30 * time.Second), Price: 20},
	}
	fast := []Sample{}
	for i := range 4 {
		fast = append(fast, Sample{Time: base.Add(time.Duration(i*10) * time.Second), Price: float64(i + 1)})
	}
	_, aligned := Align([][]Sample{slow, fast}, 10*time.Second)
	want := []float64{10, 10, 10, 20}
	for i, w := range want {
		if aligned[0][i] != w {
			t.Fatalf("slow aligned = %v, want %v (last observation carried forward)", aligned[0], want)
		}
	}
}

func TestAlignNonOverlappingSeries(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	a := []Sample{{Time: base, Price: 1}, {Time: base.Add(time.Minute), Price: 2}}
	b := []Sample{{Time: base.Add(time.Hour), Price: 1}, {Time: base.Add(2 * time.Hour), Price: 2}}
	if times, aligned := Align([][]Sample{a, b}, time.Second); times != nil || aligned != nil {
		t.Errorf("disjoint windows should not align, got %d buckets", len(times))
	}
}

func TestAlignDegenerateInputs(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	ok := []Sample{{Time: base, Price: 1}, {Time: base.Add(time.Minute), Price: 2}}
	cases := []struct {
		name   string
		series [][]Sample
		bucket time.Duration
	}{
		{"no series", nil, time.Second},
		{"zero bucket", [][]Sample{ok, ok}, 0},
		{"negative bucket", [][]Sample{ok, ok}, -time.Second},
		{"empty member", [][]Sample{ok, {}}, time.Second},
		{"bucket wider than window", [][]Sample{ok, ok}, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if times, aligned := Align(c.series, c.bucket); times != nil || aligned != nil {
				t.Errorf("want nil, got %d buckets", len(times))
			}
		})
	}
}

func TestAlignCapsBucketCount(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	a := []Sample{{Time: base, Price: 1}, {Time: base.Add(10 * time.Hour), Price: 2}}
	b := []Sample{{Time: base, Price: 1}, {Time: base.Add(10 * time.Hour), Price: 2}}
	times, aligned := Align([][]Sample{a, b}, time.Second)
	if len(times) != maxAlignBuckets {
		t.Fatalf("grid = %d buckets, want the cap %d", len(times), maxAlignBuckets)
	}
	if len(aligned[0]) != maxAlignBuckets {
		t.Fatalf("aligned len = %d, want %d", len(aligned[0]), maxAlignBuckets)
	}
	// The cap keeps the most recent window.
	if !times[len(times)-1].After(base.Add(9 * time.Hour)) {
		t.Errorf("last bucket = %v, want the most recent window retained", times[len(times)-1])
	}
}

func TestAlignSortsUnorderedSamples(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	a := []Sample{
		{Time: base.Add(20 * time.Second), Price: 3},
		{Time: base, Price: 1},
		{Time: base.Add(10 * time.Second), Price: 2},
	}
	_, aligned := Align([][]Sample{a, a}, 10*time.Second)
	want := []float64{1, 2, 3}
	if len(aligned[0]) != 3 {
		t.Fatalf("aligned = %v, want 3 buckets", aligned[0])
	}
	for i, w := range want {
		if aligned[0][i] != w {
			t.Fatalf("aligned = %v, want %v", aligned[0], want)
		}
	}
}

func TestCorrelationSeriesAlignsOnTime(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	xs := []float64{0.02, -0.01, 0.03, -0.02, 0.01}

	// Slow feed: one observation per bucket.
	slowPrices := fromLogReturns(100, xs)
	var slow []Sample
	for i, p := range slowPrices {
		slow = append(slow, Sample{Time: base.Add(time.Duration(i*15) * time.Second), Price: p})
	}
	// Fast feed: three observations per bucket, mirroring the slow feed's move
	// exactly on the bucket boundaries.
	var fast []Sample
	for i, p := range slowPrices {
		for j := range 3 {
			fast = append(fast, Sample{
				Time:  base.Add(time.Duration(i*15+j*5) * time.Second),
				Price: 2 * p,
			})
		}
	}

	got := CorrelationSeries(slow, fast, 15*time.Second)
	if math.Abs(got-1) > 1e-9 {
		t.Errorf("CorrelationSeries = %v, want 1 — same moves at different sample rates", got)
	}
}

func TestCorrelationSeriesInsufficientOverlap(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	a := []Sample{{Time: base, Price: 1}, {Time: base.Add(time.Minute), Price: 2}}
	if got := CorrelationSeries(a, a, time.Minute); !math.IsNaN(got) {
		t.Errorf("got %v, want NaN — one bucket cannot produce two returns", got)
	}
}

func TestCorrelationMatrixSeries(t *testing.T) {
	base := time.Unix(1_699_999_980, 0).UTC()
	xs := []float64{0.02, -0.01, 0.03, -0.02}
	up := fromLogReturns(100, xs)
	down := fromLogReturns(100, []float64{-0.02, 0.01, -0.03, 0.02})

	mk := func(prices []float64, step time.Duration) []Sample {
		out := make([]Sample, len(prices))
		for i, p := range prices {
			out[i] = Sample{Time: base.Add(time.Duration(i) * step), Price: p}
		}
		return out
	}
	series := [][]Sample{mk(up, 10*time.Second), mk(down, 10*time.Second)}

	m := CorrelationMatrixSeries([]string{"A", "B"}, series, 10*time.Second)
	if math.Abs(m[0][0]-1) > 1e-9 || math.Abs(m[1][1]-1) > 1e-9 {
		t.Errorf("diagonal not 1: %v", m)
	}
	if math.Abs(m[0][1]+1) > 1e-9 {
		t.Errorf("A vs B = %v, want -1", m[0][1])
	}
	if m[0][1] != m[1][0] {
		t.Error("not symmetric")
	}
}

func TestCorrelationMatrixSeriesDegenerateInputs(t *testing.T) {
	if m := CorrelationMatrixSeries(nil, nil, time.Second); len(m) != 0 {
		t.Errorf("no symbols should yield an empty matrix, got %v", m)
	}
	m := CorrelationMatrixSeries([]string{"A", "B"}, [][]Sample{{{Time: time.Unix(1, 0), Price: 1}}}, time.Second)
	if m[0][0] != 1 || !math.IsNaN(m[0][1]) {
		t.Errorf("missing series: got %v", m)
	}
}

func TestCorrelationReturnsIndexAligned(t *testing.T) {
	a := fromLogReturns(100, []float64{0.01, -0.02, 0.03})
	b := fromLogReturns(7, []float64{0.01, -0.02, 0.03})
	if got := CorrelationReturns(a, b); math.Abs(got-1) > 1e-9 {
		t.Errorf("got %v, want 1", got)
	}
	if got := CorrelationReturns(a, b[:2]); !math.IsNaN(got) {
		t.Errorf("length mismatch = %v, want NaN", got)
	}
}
