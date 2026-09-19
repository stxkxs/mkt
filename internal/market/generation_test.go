package market

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// writeSeries fills a symbol's ring with a deterministic, symbol-specific
// series on a one-minute cadence.
func writeSeries(c *Cache, sym string, base time.Time, n, salt int) {
	for j := range n {
		p := 100 + float64(salt)*7 + 9*math.Sin(float64(j*(salt+1))/3.0) + float64(j)*0.3
		c.Push(provider.Quote{Symbol: sym, Price: p, Timestamp: base.Add(time.Duration(j) * time.Minute)})
	}
}

// The property a consumer keying off Generation actually depends on: two
// equal readings bracket an unchanged cache. It holds because the counter
// advances inside the critical section the write already holds — the mutex is
// what orders the increment against the data becoming visible. An increment
// placed after the unlock lets a reader see post-write data between two
// pre-write readings, and the reader has no way to tell.
//
// Checked as one snapshot recorded per generation any reader brackets: two
// readers that bracket the same generation and disagree about what they read
// is the violation. A bracket only proves something when it closes with the
// counter unmoved, so the readers run until enough of them have.
func TestEqualGenerationsBracketAnUnchangedCache(t *testing.T) {
	c := NewCache(16)
	syms := []string{"AAA", "BBB"}
	base := time.Unix(1_700_000_000, 0).UTC()
	const want = 200

	var seen sync.Map // generation -> the snapshot bracketed under it
	var checks atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup

	for w, sym := range syms {
		wg.Add(1)
		go func(w int, sym string) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				c.Push(provider.Quote{
					Symbol:    sym,
					Price:     float64(1 + (i*7+w)%500),
					Timestamp: base.Add(time.Duration(i) * time.Second),
				})
				// Leave the writers' own window open often enough that a
				// reader can close a bracket inside it.
				runtime.Gosched()
			}
		}(w, sym)
	}

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				g1 := c.Generation()
				snap := snapshot(c, syms)
				if c.Generation() != g1 {
					continue // a write landed; the reading proves nothing
				}
				if prev, loaded := seen.LoadOrStore(g1, snap); loaded && prev.(string) != snap {
					t.Errorf("generation %d bracketed two different caches: %s and %s", g1, prev, snap)
					return
				}
				checks.Add(1)
			}
		}()
	}

	deadline := time.Now().Add(20 * time.Second)
	for checks.Load() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()

	if got := checks.Load(); got < want {
		t.Fatalf("%d readings closed a bracket, want at least %d", got, want)
	}
}

// snapshot is what a reader compares across a bracket: the latest price of
// every symbol, which Push writes under the same lock that guards the rings.
func snapshot(c *Cache, syms []string) string {
	var b []byte
	for _, s := range syms {
		p, ok := c.Latest(s)
		b = fmt.Appendf(b, "%s=%g/%t;", s, p, ok)
	}
	return string(b)
}

// Every write path must move the counter and every read path must leave it
// alone, checked by comparing against an independently kept tally rather
// than against the counter's own arithmetic.
func TestGenerationTallyMatchesAcceptedWrites(t *testing.T) {
	c := NewCache(16)
	base := time.Unix(1_700_000_000, 0).UTC()
	accepted := uint64(0)
	start := c.Generation()

	push := func(sym string, i int) {
		c.Push(provider.Quote{Symbol: sym, Price: float64(i + 1), Timestamp: base.Add(time.Duration(i) * time.Minute)})
		accepted++
	}
	for i := range 10 {
		push("AAA", i)
	}
	// A push that carries nothing usable is still a write: it displaces a
	// real sample out of the ring.
	c.Push(provider.Quote{Symbol: "AAA"})
	accepted++
	c.Push(provider.Quote{Symbol: "AAA", Price: -1, Timestamp: base})
	accepted++

	if c.Seed("BBB", []float64{1, 2, 3}) {
		accepted++
	} else {
		t.Fatal("first Seed refused")
	}
	if c.Seed("BBB", []float64{4, 5}) {
		t.Fatal("second Seed accepted")
	}
	if c.Seed("CCC", nil) {
		t.Fatal("empty Seed accepted")
	}
	if c.SeedCandles("DDD", []provider.OHLCV{{Close: 1, Time: base}}) {
		accepted++
	} else {
		t.Fatal("SeedCandles refused a first backfill")
	}

	// Reads.
	for range 3 {
		c.Prices("AAA")
		c.Series("AAA")
		c.Latest("AAA")
		c.LatestQuote("AAA")
		c.Symbols()
		c.Seeded("BBB")
		c.Generation()
	}

	if got := c.Generation() - start; got != accepted {
		t.Errorf("generation advanced %d, accepted writes %d", got, accepted)
	}
}

// Concurrent writers, backfills and readers, for the race detector. The
// counter must also account for exactly the writes that were accepted.
func TestGenerationUnderConcurrentPushSeriesAndSeed(t *testing.T) {
	c := NewCache(24)
	base := time.Unix(1_700_000_000, 0).UTC()
	const writers, each = 6, 1500

	var accepted atomic.Uint64
	var wg sync.WaitGroup

	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sym := fmt.Sprintf("S%d", w%3)
			for i := range each {
				c.Push(provider.Quote{Symbol: sym, Price: float64(i%97 + 1), Timestamp: base.Add(time.Duration(i) * time.Second)})
				accepted.Add(1)
			}
		}(w)
	}
	// Backfills racing the pushes: whichever wins, only the accepted ones count.
	for w := range 3 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sym := fmt.Sprintf("S%d", w)
			for range 50 {
				if c.SeedCandles(sym, []provider.OHLCV{{Close: 5, Time: base.Add(-time.Minute)}}) {
					accepted.Add(1)
				}
			}
		}(w)
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 3000 {
				_ = c.Generation()
				c.Series("S0")
				c.Prices("S1")
				c.Latest("S2")
				c.Symbols()
				c.Seeded("S0")
			}
		}()
	}
	wg.Wait()

	if got, want := c.Generation(), accepted.Load(); got != want {
		t.Errorf("generation %d, accepted writes %d", got, want)
	}
}

// The backfills that change nothing must also be the ones that move nothing:
// a refused seed leaves the rings exactly as they were, which is why it is
// allowed not to advance the generation.
func TestRefusedSeedLeavesTheRingsAlone(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	candles := []provider.OHLCV{
		{Close: 10, Time: base.Add(-3 * time.Minute)},
		{Close: 11, Time: base.Add(-2 * time.Minute)},
	}

	t.Run("second seed", func(t *testing.T) {
		c := NewCache(60)
		writeSeries(c, "AAA", base, 10, 0)
		if !c.SeedCandles("AAA", candles) {
			t.Fatal("first backfill refused")
		}
		gen := c.Generation()
		p0, t0 := c.Series("AAA")
		if c.SeedCandles("AAA", []provider.OHLCV{{Close: 99, Time: base.Add(-time.Minute)}}) {
			t.Fatal("second backfill accepted")
		}
		assertRingUnchanged(t, c, "AAA", gen, p0, t0)
	})

	t.Run("full ring", func(t *testing.T) {
		c := NewCache(4)
		writeSeries(c, "AAA", base, 4, 0)
		gen := c.Generation()
		p0, t0 := c.Series("AAA")
		if c.SeedCandles("AAA", candles) {
			t.Fatal("backfill accepted into a full ring")
		}
		assertRingUnchanged(t, c, "AAA", gen, p0, t0)
	})

	t.Run("empty candles", func(t *testing.T) {
		c := NewCache(60)
		writeSeries(c, "AAA", base, 4, 0)
		gen := c.Generation()
		p0, t0 := c.Series("AAA")
		if c.SeedCandles("AAA", nil) {
			t.Fatal("empty backfill accepted")
		}
		assertRingUnchanged(t, c, "AAA", gen, p0, t0)
	})
}

func assertRingUnchanged(t *testing.T, c *Cache, sym string, gen uint64, prices []float64, times []time.Time) {
	t.Helper()
	p1, t1 := c.Series(sym)
	if !reflect.DeepEqual(prices, p1) || !reflect.DeepEqual(times, t1) {
		t.Errorf("refused backfill changed the ring: %v/%v -> %v/%v", prices, times, p1, t1)
	}
	if got := c.Generation(); got != gen {
		t.Errorf("refused backfill advanced the generation %d -> %d", gen, got)
	}
}
