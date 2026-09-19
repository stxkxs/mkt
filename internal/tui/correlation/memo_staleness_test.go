package correlation

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
)

// ---------------------------------------------------------------------------
// Independent harness.
//
// The oracle for "what should this tab be showing" is a Model built from New
// and driven through the same model-level transitions, rendered once. Its memo
// is cold, so its render is always a fresh computation off the live cache. Any
// divergence between the long-lived model's render and the oracle's render is
// a stale matrix.
// ---------------------------------------------------------------------------

type modelOp struct {
	name string
	// on mutates the model. Replayable: it must not touch the cache.
	on func(Model) Model
}

func sizeOp(w, h int) modelOp {
	return modelOp{fmt.Sprintf("size(%d,%d)", w, h), func(m Model) Model { m.SetSize(w, h); return m }}
}

func symbolsOp(s []string) modelOp {
	return modelOp{fmt.Sprintf("symbols(%v)", s), func(m Model) Model { m.SetSymbols(s); return m }}
}

func keyOp(k string) modelOp {
	return modelOp{"key(" + k + ")", func(m Model) Model { return press(m, k) }}
}

// coldModel rebuilds the tab from scratch and replays every model op, so the
// returned model has never memoized anything.
func coldModel(cache *market.Cache, start []string, ops []modelOp) Model {
	m := New(start, cache)
	for _, op := range ops {
		m = op.on(m)
	}
	return m
}

// writeSeries writes a deterministic but symbol-specific series into the cache.
func writeSeries(c *market.Cache, sym string, base time.Time, n, salt int) {
	for j := range n {
		p := 100 + float64(salt)*7 + 9*math.Sin(float64(j*(salt+1))/3.0) + float64(j)*0.3
		c.Push(provider.Quote{Symbol: sym, Price: p, Timestamp: base.Add(time.Duration(j) * time.Minute)})
	}
}

var universe = []string{"AAA", "BBB", "CCC", "DDD", "EEE", "FFF", "GGG", "HHH"}

func filledCache() (*market.Cache, time.Time) {
	c := market.NewCache(240)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, s := range universe {
		writeSeries(c, s, base, 45, i)
	}
	return c, base.Add(45 * time.Minute)
}

// assertMatchesCold renders the live model and a cold oracle and fails on any
// difference in the painted grid.
func assertMatchesCold(t *testing.T, step string, cache *market.Cache, start []string, ops []modelOp, live Model) {
	t.Helper()
	got := plain(live.View())
	want := plain(coldModel(cache, start, ops).View())
	if got != want {
		t.Errorf("step %q: rendered matrix is stale\n--- got ---\n%s\n--- want ---\n%s", step, got, want)
	}
}

// ---------------------------------------------------------------------------
// The named scenarios.
// ---------------------------------------------------------------------------

func TestRenderedMatrixTracksEveryInput(t *testing.T) {
	cache, next := filledCache()
	start := append([]string(nil), universe[:6]...)

	live := New(start, cache)
	var ops []modelOp

	apply := func(op modelOp) {
		ops = append(ops, op)
		live = op.on(live)
		assertMatchesCold(t, op.name, cache, start, ops, live)
	}
	// A cache mutation is not replayable, so it is not an op: it lands on the
	// shared cache and both sides see it.
	touch := func(name string, f func()) {
		f()
		assertMatchesCold(t, name, cache, start, ops, live)
	}

	apply(sizeOp(120, 40)) // everything on screen
	apply(sizeOp(30, 9))   // window shrinks to a few symbols

	// A tick for a symbol already in the visible window.
	touch("tick for visible AAA", func() {
		cache.Push(provider.Quote{Symbol: "AAA", Price: 400, Timestamp: next})
		next = next.Add(time.Minute)
	})

	// A tick for a symbol in the universe but off screen.
	touch("tick for off-screen FFF", func() {
		cache.Push(provider.Quote{Symbol: "FFF", Price: 400, Timestamp: next})
		next = next.Add(time.Minute)
	})

	// A tick for a symbol the tab does not know about at all.
	touch("tick for unknown ZZZ", func() {
		cache.Push(provider.Quote{Symbol: "ZZZ", Price: 400, Timestamp: next})
		next = next.Add(time.Minute)
	})

	// Two ticks carrying the same instant.
	touch("two ticks, one instant", func() {
		cache.Push(provider.Quote{Symbol: "AAA", Price: 133, Timestamp: next})
		cache.Push(provider.Quote{Symbol: "BBB", Price: 177, Timestamp: next})
		next = next.Add(time.Minute)
	})

	// A tick that repeats the previous price and instant exactly.
	touch("duplicate tick", func() {
		cache.Push(provider.Quote{Symbol: "AAA", Price: 133, Timestamp: next.Add(-time.Minute)})
	})

	// A tick with no timestamp: the sample is dropped by the aligner, but it
	// still displaces a real sample out of the ring.
	touch("zero-time tick", func() {
		cache.Push(provider.Quote{Symbol: "BBB", Price: 210})
	})

	// A backfill that installs a ring for a symbol the window shows.
	touch("seed a visible symbol", func() {
		cache.SeedCandles("CCC", []provider.OHLCV{
			{Close: 90, Time: next.Add(-90 * time.Minute)},
			{Close: 95, Time: next.Add(-89 * time.Minute)},
			{Close: 88, Time: next.Add(-88 * time.Minute)},
		})
	})

	// Resizes: one that moves the window, one that does not.
	apply(sizeOp(30, 12)) // taller: more rows fit, window grows
	apply(sizeOp(40, 12)) // wider but height-bound: same window
	apply(sizeOp(120, 12))
	apply(sizeOp(30, 9))

	// Scroll.
	apply(keyOp("j"))
	apply(keyOp("j"))
	apply(keyOp("k"))
	apply(keyOp("G"))
	apply(keyOp("g"))

	// Bucket cycle, all the way round and past.
	for i := range 6 {
		apply(modelOp{fmt.Sprintf("bucket #%d", i), func(m Model) Model { return press(m, "b") }})
	}

	// SetSymbols to a set of the same length, different contents.
	apply(symbolsOp([]string{"HHH", "GGG", "FFF", "EEE", "DDD", "CCC"}))
	// SetSymbols to the same length again, with the visible window unchanged
	// but the tail different.
	apply(symbolsOp([]string{"HHH", "GGG", "FFF", "AAA", "BBB", "CCC"}))
	// Same length, same members, different order.
	apply(symbolsOp([]string{"GGG", "HHH", "FFF", "AAA", "BBB", "CCC"}))
	// Shorter, then longer.
	apply(symbolsOp([]string{"AAA", "BBB", "CCC"}))
	apply(symbolsOp(append([]string(nil), universe...)))
	// Empty and back.
	apply(symbolsOp(nil))
	apply(symbolsOp([]string{"AAA", "BBB", "CCC", "DDD"}))

	// A tick after all of that, to be sure the key still moves.
	touch("final tick", func() {
		cache.Push(provider.Quote{Symbol: "DDD", Price: 500, Timestamp: next})
	})
}

// A random walk over every input the matrix depends on, checked against a
// cold model at every step. This is the broad hunt for a key that misses a
// real change; the named scenarios above are the specific ones.
func TestRandomWalkNeverGoesStale(t *testing.T) {
	for _, seed := range []int64{1, 20260919, 424242} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { randomWalk(t, seed) })
	}
}

func randomWalk(t *testing.T, seed int64) {
	cache, next := filledCache()
	start := append([]string(nil), universe[:5]...)
	live := New(start, cache)
	var ops []modelOp

	rng := rand.New(rand.NewSource(seed))
	keys := []string{"j", "k", "h", "l", "[", "]", "g", "G", "pgdown", "pgup", "b"}
	widths := []int{0, 9, 16, 24, 31, 44, 60, 90, 130}
	heights := []int{0, 3, 6, 8, 10, 14, 19, 26, 41}

	for step := range 900 {
		switch rng.Intn(6) {
		case 0, 1: // a tick
			sym := universe[rng.Intn(len(universe))]
			q := provider.Quote{Symbol: sym, Price: 50 + rng.Float64()*400, Timestamp: next}
			if rng.Intn(8) == 0 {
				q.Timestamp = time.Time{} // untimed tick
			}
			if rng.Intn(9) == 0 {
				next = next.Add(-time.Minute) // out-of-order tick
			} else {
				next = next.Add(time.Minute)
			}
			cache.Push(q)
		case 2: // a key
			op := keyOp(keys[rng.Intn(len(keys))])
			ops = append(ops, op)
			live = op.on(live)
		case 3: // a resize
			op := sizeOp(widths[rng.Intn(len(widths))], heights[rng.Intn(len(heights))])
			ops = append(ops, op)
			live = op.on(live)
		case 4: // a new symbol set
			n := rng.Intn(len(universe) + 1)
			perm := rng.Perm(len(universe))
			set := make([]string, n)
			for i := range set {
				set[i] = universe[perm[i]]
			}
			op := symbolsOp(set)
			ops = append(ops, op)
			live = op.on(live)
		case 5: // a backfill attempt
			sym := universe[rng.Intn(len(universe))]
			cache.SeedCandles(sym, []provider.OHLCV{
				{Close: 70, Time: next.Add(-200 * time.Minute)},
				{Close: 75, Time: next.Add(-199 * time.Minute)},
			})
		}
		if got, want := plain(live.View()), plain(coldModel(cache, start, ops).View()); got != want {
			t.Fatalf("step %d: rendered matrix is stale\n--- got ---\n%s\n--- want ---\n%s", step, got, want)
		}
	}
}

// The rendered numbers must be the numbers, not merely consistent with
// another run of the same model code. This checks the tab's grid against a
// correlation computed straight off the cache.
func TestMatrixValuesMatchDirectComputation(t *testing.T) {
	cache, next := filledCache()
	syms := []string{"AAA", "BBB", "CCC"}
	m := New(syms, cache)
	m.SetSize(120, 40)

	check := func(label string) {
		t.Helper()
		series := make([][]portfolio.Sample, len(syms))
		for i, s := range syms {
			prices, times := cache.Series(s)
			series[i] = portfolio.SamplesFrom(prices, times)
		}
		want := portfolio.CorrelationMatrixSeries(syms, series, m.Bucket())
		got := m.matrixFor(syms)
		if !sameMatrix(got, want) {
			t.Errorf("%s: matrix %v, want %v", label, got, want)
		}
	}

	check("initial")
	for i := range 12 {
		cache.Push(provider.Quote{Symbol: syms[i%len(syms)], Price: 90 + float64(i)*11, Timestamp: next})
		next = next.Add(time.Minute)
		check(fmt.Sprintf("after tick %d", i))
		if i%4 == 3 {
			m = press(m, "b")
			check(fmt.Sprintf("after bucket change %d", i))
		}
	}
}

func sameMatrix(a, b [][]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			x, y := a[i][j], b[i][j]
			if math.IsNaN(x) != math.IsNaN(y) {
				return false
			}
			if !math.IsNaN(x) && math.Abs(x-y) > 1e-12 {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Does it actually skip the work?
//
// Measured without the memo's own counter: a build allocates a fresh matrix
// every time, so an unchanged frame that hands back the identical backing
// array did not rebuild. Allocation count corroborates it.
// ---------------------------------------------------------------------------

func matrixID(m [][]float64) uintptr { return reflect.ValueOf(m).Pointer() }

func TestUnchangedFrameDoesNoWork(t *testing.T) {
	cache, next := filledCache()
	syms := []string{"AAA", "BBB", "CCC", "DDD"}
	m := New(syms, cache)
	m.SetSize(120, 40)

	first := matrixID(m.matrixFor(syms))
	for i := range 200 {
		if got := matrixID(m.matrixFor(syms)); got != first {
			t.Fatalf("frame %d rebuilt the matrix with nothing changed", i)
		}
	}
	if n := testing.AllocsPerRun(50, func() { _ = m.matrixFor(syms) }); n != 0 {
		t.Errorf("unchanged frame allocated %v times, want 0", n)
	}

	// And every real change does rebuild.
	cache.Push(provider.Quote{Symbol: "AAA", Price: 300, Timestamp: next})
	afterTick := matrixID(m.matrixFor(syms))
	if afterTick == first {
		t.Fatal("a tick for a visible symbol did not rebuild the matrix")
	}

	m2 := press(m, "b")
	if matrixID(m2.matrixFor(syms)) == afterTick {
		t.Fatal("a bucket change did not rebuild the matrix")
	}
}

// ---------------------------------------------------------------------------
// Value receivers: every copy shares one memo.
// ---------------------------------------------------------------------------

// Two copies that differ only in bucket, alternating. Each must render its
// own bucket's matrix, never the other's.
func TestAlternatingCopiesEachSeeTheirOwnBucket(t *testing.T) {
	cache, _ := filledCache()
	syms := []string{"AAA", "BBB", "CCC", "DDD"}
	base := New(syms, cache)
	base.SetSize(120, 40)

	copies := []Model{base}
	for i := range 3 {
		copies = append(copies, press(copies[i], "b"))
	}

	// A cold reference per bucket.
	want := make([]string, len(copies))
	for i := range copies {
		fresh := New(syms, cache)
		fresh.SetSize(120, 40)
		want[i] = plain(press(fresh, repeated("b", i)...).View())
	}

	for round := range 5 {
		for i, c := range copies {
			if got := plain(c.View()); got != want[i] {
				t.Fatalf("round %d, copy %d (bucket %s): rendered another copy's matrix\n--- got ---\n%s\n--- want ---\n%s",
					round, i, c.Bucket(), got, want[i])
			}
		}
	}
}

// Two copies over different symbol sets of the SAME LENGTH, alternating. A
// key that compared only the window's length would hand each one the other's
// grid.
func TestAlternatingCopiesWithSameLengthWindows(t *testing.T) {
	cache, _ := filledCache()
	a := New([]string{"AAA", "BBB", "CCC"}, cache)
	a.SetSize(120, 40)
	b := a
	b.SetSymbols([]string{"FFF", "GGG", "HHH"})

	wantA := plain(coldModel(cache, []string{"AAA", "BBB", "CCC"}, []modelOp{sizeOp(120, 40)}).View())
	wantB := plain(coldModel(cache, []string{"FFF", "GGG", "HHH"}, []modelOp{sizeOp(120, 40)}).View())
	if wantA == wantB {
		t.Fatal("fixture is blind: the two symbol sets render identically")
	}

	for round := range 5 {
		if got := plain(a.View()); got != wantA {
			t.Fatalf("round %d: copy A rendered the wrong grid\n--- got ---\n%s\n--- want ---\n%s", round, got, wantA)
		}
		if got := plain(b.View()); got != wantB {
			t.Fatalf("round %d: copy B rendered the wrong grid\n--- got ---\n%s\n--- want ---\n%s", round, got, wantB)
		}
	}
}

// A copy held from before a tick renders the post-tick matrix, because the
// generation is read at render time and not carried in the copy.
func TestOlderCopyStillSeesNewData(t *testing.T) {
	cache, next := filledCache()
	syms := []string{"AAA", "BBB", "CCC"}
	old := New(syms, cache)
	old.SetSize(120, 40)
	_ = old.View()

	for i := range 6 {
		cache.Push(provider.Quote{Symbol: "AAA", Price: 900 + float64(i)*40, Timestamp: next})
		next = next.Add(time.Minute)
	}

	want := plain(coldModel(cache, syms, []modelOp{sizeOp(120, 40)}).View())
	if got := plain(old.View()); got != want {
		t.Errorf("a Model copy taken before the ticks rendered a stale matrix\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func repeated(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// ---------------------------------------------------------------------------
// Backfill.
//
// A seed is inserted behind the live data, so it only matters when it extends
// a series backwards into the span the other series already cover. A fixture
// whose seeded candles land before every other series' first observation is
// blind: the aligner's shared window does not move and the matrix is
// unchanged either way.
// ---------------------------------------------------------------------------

func writeSeriesFrom(c *market.Cache, sym string, start time.Time, n, salt int) {
	for j := range n {
		p := 100 + float64(salt)*7 + 9*math.Sin(float64(j*(salt+1))/3.0) + float64(j)*0.3
		c.Push(provider.Quote{Symbol: sym, Price: p, Timestamp: start.Add(time.Duration(j) * time.Minute)})
	}
}

func TestSeedThatMovesTheSharedWindowRebuilds(t *testing.T) {
	cache := market.NewCache(240)
	base := time.Unix(1_700_000_000, 0).UTC()
	writeSeries(cache, "AAA", base, 60, 0)
	writeSeries(cache, "BBB", base, 60, 1)
	// CCC only joined halfway through, so it is what bounds the shared window.
	writeSeriesFrom(cache, "CCC", base.Add(30*time.Minute), 30, 2)

	syms := []string{"AAA", "BBB", "CCC"}
	m := New(syms, cache)
	m.SetSize(120, 40)
	before := plain(m.View())

	candles := make([]provider.OHLCV, 30)
	for i := range candles {
		candles[i] = provider.OHLCV{
			Close: 80 + 6*math.Cos(float64(i)/2.5),
			Time:  base.Add(time.Duration(i) * time.Minute),
		}
	}
	if !cache.SeedCandles("CCC", candles) {
		t.Fatal("SeedCandles refused a first backfill")
	}

	want := plain(coldModel(cache, syms, []modelOp{sizeOp(120, 40)}).View())
	if want == before {
		t.Fatal("fixture is blind: the backfill did not change the matrix")
	}
	if got := plain(m.View()); got != want {
		t.Errorf("a backfill left a stale matrix on screen\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ---------------------------------------------------------------------------
// A write landing between the generation read and the series read.
//
// Reading the generation first makes that a redundant rebuild on the next
// frame. Reading it second would store a pre-write matrix under the
// post-write counter, and once the writes stop nothing would ever dislodge
// it. This drives the race for real and then checks what is on screen after
// the writes quiesce.
// ---------------------------------------------------------------------------

func TestMidReadWriteIsNeverPinned(t *testing.T) {
	cache := market.NewCache(64)
	base := time.Unix(1_700_000_000, 0).UTC()
	syms := []string{"AAA", "BBB", "CCC"}
	for i, s := range syms {
		writeSeries(cache, s, base, 20, i)
	}
	m := New(syms, cache)
	m.SetSize(120, 40)
	next := base.Add(20 * time.Minute)

	// Each round is one short burst of writes racing a few frames. Only the
	// round's last write can pin anything — once the writes stop, a matrix
	// stored under the final generation is never reconsidered — so the round
	// is kept small and repeated instead of made long.
	for round := range 2500 {
		done := make(chan struct{})
		start := next
		go func() {
			defer close(done)
			for i := range 24 {
				cache.Push(provider.Quote{
					Symbol:    syms[i%len(syms)],
					Price:     70 + float64((round*7+i*29)%200),
					Timestamp: start.Add(time.Duration(i) * time.Minute),
				})
				runtime.Gosched()
			}
		}()
		for range 40 {
			_ = m.matrixFor(syms)
			runtime.Gosched()
		}
		<-done
		next = start.Add(24 * time.Minute)

		// The writes have stopped. What the tab would paint now has to be the
		// truth, and has to stay the truth however many frames follow.
		want := coldModel(cache, syms, []modelOp{sizeOp(120, 40)}).matrixFor(syms)
		for frame := range 2 {
			if got := m.matrixFor(syms); !sameMatrix(got, want) {
				t.Fatalf("round %d frame %d: a write that landed mid-read pinned a stale matrix\n got %v\nwant %v",
					round, frame, got, want)
			}
		}
	}
}

// SetSymbols stores the caller's slice, so the window handed to the memo
// aliases an array the caller still holds. A memo that kept that slice rather
// than a copy would compare the caller's array against itself and never
// notice an element changing under it.
func TestWindowMutatedInPlaceRebuilds(t *testing.T) {
	cache, _ := filledCache()
	syms := []string{"AAA", "BBB", "CCC"}
	m := New(nil, cache)
	m.SetSize(120, 40)
	m.SetSymbols(syms)
	before := plain(m.View())

	syms[0] = "HHH"

	want := plain(coldModel(cache, []string{"HHH", "BBB", "CCC"}, []modelOp{sizeOp(120, 40)}).View())
	if want == before {
		t.Fatal("fixture is blind: swapping the symbol did not change the grid")
	}
	if got := plain(m.View()); got != want {
		t.Errorf("a symbol changed under the window left a stale matrix\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// matrixFor dereferences the cache for the generation before it reads any
// series, so the guard that keeps an empty window away from the cache has to
// sit in front of both. A tab with nothing to correlate must render without
// reaching for data.
func TestEmptyWindowNeverReachesTheCache(t *testing.T) {
	for _, tc := range []struct {
		name string
		syms []string
		w, h int
	}{
		{"no symbols", nil, 120, 40},
		{"no room", []string{"AAA", "BBB"}, 4, 2},
		{"zero size", []string{"AAA", "BBB"}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.syms, nil)
			m.SetSize(tc.w, tc.h)
			_ = m.View() // a nil cache dereference here is the regression
		})
	}
}
