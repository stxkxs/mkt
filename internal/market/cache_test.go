package market

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestLatestQuoteRetainsFullQuote(t *testing.T) {
	c := NewCache(60)
	c.Push(provider.Quote{Symbol: "BTC-USD", Price: 60000, Change: 1200, ChangePct: 2.0})
	c.Push(provider.Quote{Symbol: "BTC-USD", Price: 61000, Change: 2200, ChangePct: 3.7})

	q, ok := c.LatestQuote("BTC-USD")
	if !ok {
		t.Fatal("LatestQuote(BTC-USD): not found")
	}
	if q.Price != 61000 || q.ChangePct != 3.7 || q.Change != 2200 {
		t.Errorf("got %+v, want latest push (price=61000 change=2200 pct=3.7)", q)
	}

	// Price ring still tracks history for sparklines.
	if prices := c.Prices("BTC-USD"); len(prices) != 2 {
		t.Errorf("Prices len = %d, want 2", len(prices))
	}
}

func TestLatestQuoteUnknownSymbol(t *testing.T) {
	c := NewCache(60)
	if _, ok := c.LatestQuote("NOPE"); ok {
		t.Error("LatestQuote(NOPE): want not found")
	}
}

func TestNewCacheClampsRingSize(t *testing.T) {
	for _, size := range []int{0, -1} {
		c := NewCache(size)
		if c.ringSize != defaultRingSize {
			t.Errorf("NewCache(%d).ringSize = %d, want %d", size, c.ringSize, defaultRingSize)
		}
	}
}

func TestRingWrapsAroundKeepingMostRecent(t *testing.T) {
	c := NewCache(4)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 1; i <= 10; i++ {
		c.Push(provider.Quote{
			Symbol:    "AAA",
			Price:     float64(i),
			Timestamp: base.Add(time.Duration(i) * time.Second),
		})
	}

	prices, times := c.Series("AAA")
	want := []float64{7, 8, 9, 10}
	if len(prices) != len(want) {
		t.Fatalf("len = %d, want %d", len(prices), len(want))
	}
	for i := range want {
		if prices[i] != want[i] {
			t.Fatalf("prices = %v, want %v (oldest first, most recent retained)", prices, want)
		}
		if got := times[i]; !got.Equal(base.Add(time.Duration(want[i]) * time.Second)) {
			t.Fatalf("times[%d] = %v, want the timestamp pushed with price %v", i, got, want[i])
		}
	}
}

func TestRingPartialFillOrdersOldestFirst(t *testing.T) {
	c := NewCache(8)
	for i := 1; i <= 3; i++ {
		c.Push(provider.Quote{Symbol: "AAA", Price: float64(i)})
	}
	prices := c.Prices("AAA")
	if len(prices) != 3 || prices[0] != 1 || prices[2] != 3 {
		t.Fatalf("prices = %v, want [1 2 3]", prices)
	}
}

func TestRingSizeOneAlwaysHoldsLatest(t *testing.T) {
	c := NewCache(1)
	for i := 1; i <= 5; i++ {
		c.Push(provider.Quote{Symbol: "AAA", Price: float64(i)})
	}
	prices := c.Prices("AAA")
	if len(prices) != 1 || prices[0] != 5 {
		t.Fatalf("prices = %v, want [5]", prices)
	}
}

func TestPricesAndSeriesUnknownSymbol(t *testing.T) {
	c := NewCache(8)
	if got := c.Prices("NOPE"); got != nil {
		t.Errorf("Prices(NOPE) = %v, want nil", got)
	}
	if p, ts := c.Series("NOPE"); p != nil || ts != nil {
		t.Errorf("Series(NOPE) = %v %v, want nil nil", p, ts)
	}
}

func TestSeedBackfillsEmptyRing(t *testing.T) {
	c := NewCache(8)
	if !c.Seed("AAPL", []float64{1, 2, 3}) {
		t.Fatal("Seed reported no work on an empty ring")
	}
	if !c.Seeded("AAPL") {
		t.Error("Seeded(AAPL) = false after seeding")
	}
	if got := c.Prices("AAPL"); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("Prices = %v, want [1 2 3]", got)
	}
	// History is not a live price.
	if _, ok := c.Latest("AAPL"); ok {
		t.Error("Latest(AAPL) = ok after seeding only history")
	}
	if _, ok := c.LatestQuote("AAPL"); ok {
		t.Error("LatestQuote(AAPL) = ok after seeding only history")
	}
}

func TestSeedIsOneShot(t *testing.T) {
	c := NewCache(8)
	c.Seed("AAPL", []float64{1, 2, 3})
	if c.Seed("AAPL", []float64{9, 9}) {
		t.Fatal("second Seed reported work")
	}
	if got := c.Prices("AAPL"); len(got) != 3 {
		t.Fatalf("Prices = %v, want the first seed untouched", got)
	}
	if c.Seed("AAPL", nil) {
		t.Error("Seed with no prices reported work")
	}
}

func TestSeedInsertsBehindLiveTicks(t *testing.T) {
	c := NewCache(8)
	c.Push(provider.Quote{Symbol: "AAPL", Price: 100})
	c.Push(provider.Quote{Symbol: "AAPL", Price: 101})

	if !c.Seed("AAPL", []float64{97, 98, 99}) {
		t.Fatal("Seed reported no work")
	}
	got := c.Prices("AAPL")
	want := []float64{97, 98, 99, 100, 101}
	if len(got) != len(want) {
		t.Fatalf("Prices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Prices = %v, want %v (history behind live ticks)", got, want)
		}
	}
	// Live data still answers Latest.
	if p, ok := c.Latest("AAPL"); !ok || p != 101 {
		t.Errorf("Latest = %v %v, want 101 true", p, ok)
	}
}

func TestSeedTrimsToAvailableRoomKeepingRecent(t *testing.T) {
	c := NewCache(4)
	c.Push(provider.Quote{Symbol: "AAPL", Price: 100})

	c.Seed("AAPL", []float64{1, 2, 3, 4, 5})
	got := c.Prices("AAPL")
	want := []float64{3, 4, 5, 100}
	if len(got) != len(want) {
		t.Fatalf("Prices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Prices = %v, want %v (most recent history kept)", got, want)
		}
	}
}

func TestSeedSkippedWhenRingIsFullOfLiveTicks(t *testing.T) {
	c := NewCache(2)
	c.Push(provider.Quote{Symbol: "AAPL", Price: 100})
	c.Push(provider.Quote{Symbol: "AAPL", Price: 101})

	if c.Seed("AAPL", []float64{1, 2}) {
		t.Fatal("Seed reported work on a full ring")
	}
	if got := c.Prices("AAPL"); len(got) != 2 || got[0] != 100 || got[1] != 101 {
		t.Fatalf("Prices = %v, want live data untouched", got)
	}
}

func TestSeedCandlesCarriesTimestamps(t *testing.T) {
	c := NewCache(8)
	base := time.Unix(1_700_000_000, 0).UTC()
	candles := []provider.OHLCV{
		{Time: base, Close: 10},
		{Time: base.Add(time.Hour), Close: 11},
	}
	if !c.SeedCandles("AAPL", candles) {
		t.Fatal("SeedCandles reported no work")
	}
	prices, times := c.Series("AAPL")
	if len(prices) != 2 || prices[0] != 10 || prices[1] != 11 {
		t.Fatalf("prices = %v, want [10 11]", prices)
	}
	if !times[0].Equal(base) || !times[1].Equal(base.Add(time.Hour)) {
		t.Fatalf("times = %v, want the candle times", times)
	}
}

func TestSeedWithoutTimesLeavesZeroTimestamps(t *testing.T) {
	c := NewCache(8)
	c.Seed("AAPL", []float64{1, 2})
	_, times := c.Series("AAPL")
	for i, ts := range times {
		if !ts.IsZero() {
			t.Errorf("times[%d] = %v, want zero — Seed has no times to report", i, ts)
		}
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := NewCache(32)
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sym := fmt.Sprintf("S%d", w%3)
			for i := range 200 {
				c.Push(provider.Quote{Symbol: sym, Price: float64(i), Timestamp: time.Now()})
				c.Prices(sym)
				c.Series(sym)
				c.Latest(sym)
				c.LatestQuote(sym)
				c.Symbols()
				c.Seed(sym, []float64{1, 2})
				c.Seeded(sym)
			}
		}(w)
	}
	wg.Wait()
}
