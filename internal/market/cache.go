package market

import (
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

const defaultRingSize = 60

// Cache stores recent quotes per symbol for sparkline rendering. It also
// retains the most recent full Quote per symbol so consumers that need
// more than the price (change %, direction, bid/ask) can read it back —
// the ring buffer intentionally keeps only prices and their timestamps.
//
// The cache is fed by live ticks. Nothing seeds it implicitly from history, so
// a freshly started process has an empty ring for every stock until the poller
// has run: sparklines are blank and any indicator computed off Prices measures
// poll ticks, not days. Seed and SeedCandles exist so a caller can backfill the
// ring from a history provider at startup and close that gap; a symbol that was
// never seeded is simply short, never wrong.
type Cache struct {
	mu       sync.RWMutex
	data     map[string]*ring
	last     map[string]provider.Quote
	seeded   map[string]bool
	ringSize int
}

// NewCache creates a new quote cache.
func NewCache(ringSize int) *Cache {
	if ringSize <= 0 {
		ringSize = defaultRingSize
	}
	return &Cache{
		data:     make(map[string]*ring),
		last:     make(map[string]provider.Quote),
		seeded:   make(map[string]bool),
		ringSize: ringSize,
	}
}

// Push adds a quote to the symbol's ring buffer and records it as the
// symbol's latest full quote.
func (c *Cache) Push(q provider.Quote) {
	c.mu.Lock()
	defer c.mu.Unlock()

	r, ok := c.data[q.Symbol]
	if !ok {
		r = newRing(c.ringSize)
		c.data[q.Symbol] = r
	}
	r.push(q.Price, q.Timestamp)
	c.last[q.Symbol] = q
}

// Prices returns the recent prices for a symbol, oldest first.
func (c *Cache) Prices(symbol string) []float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	r, ok := c.data[symbol]
	if !ok {
		return nil
	}
	prices, _ := r.series()
	return prices
}

// Series returns the recent prices for a symbol together with the timestamp
// each one carried, oldest first. The slices are the same length and index in
// step. A price seeded by Seed (which is given no times) carries the zero
// time — callers that align series across symbols must skip those.
func (c *Cache) Series(symbol string) ([]float64, []time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	r, ok := c.data[symbol]
	if !ok {
		return nil, nil
	}
	return r.series()
}

// Seed backfills a symbol's ring with historical closes, oldest first, so
// sparklines and indicators have something to work with before the first live
// tick arrives. Seeded prices are inserted *behind* whatever the symbol has
// already received, and only as many as the ring has room for (the most recent
// are kept), so a backfill can never displace live data.
//
// Seed is a one-shot per symbol: it reports whether it seeded, and a second
// call returns false without touching the ring. That makes it safe to call from
// a retrying backfill loop. Seeded prices carry no timestamp — prefer
// SeedCandles when the history has them.
//
// Seeding deliberately does not populate LatestQuote or Latest: a historical
// close is not a live price, and letting it pass for one is how a stale number
// ends up in a portfolio total.
func (c *Cache) Seed(symbol string, prices []float64) bool {
	return c.seed(symbol, prices, nil)
}

// SeedCandles backfills a symbol's ring from historical candles, using each
// candle's close and time. Semantics are otherwise identical to Seed.
func (c *Cache) SeedCandles(symbol string, candles []provider.OHLCV) bool {
	prices := make([]float64, len(candles))
	times := make([]time.Time, len(candles))
	for i, k := range candles {
		prices[i] = k.Close
		times[i] = k.Time
	}
	return c.seed(symbol, prices, times)
}

// Seeded reports whether a symbol has already been backfilled.
func (c *Cache) Seeded(symbol string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.seeded[symbol]
}

func (c *Cache) seed(symbol string, prices []float64, times []time.Time) bool {
	if len(prices) == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seeded[symbol] {
		return false
	}
	c.seeded[symbol] = true

	var livePrices []float64
	var liveTimes []time.Time
	if r, ok := c.data[symbol]; ok {
		livePrices, liveTimes = r.series()
	}

	room := c.ringSize - len(livePrices)
	if room <= 0 {
		// Live data already fills the ring; history would be evicted anyway.
		return false
	}
	if len(prices) > room {
		prices = prices[len(prices)-room:]
		if len(times) > room {
			times = times[len(times)-room:]
		}
	}

	r := newRing(c.ringSize)
	for i, p := range prices {
		var t time.Time
		if i < len(times) {
			t = times[i]
		}
		r.push(p, t)
	}
	for i, p := range livePrices {
		r.push(p, liveTimes[i])
	}
	c.data[symbol] = r
	return true
}

// Symbols returns every symbol currently held by the cache, in
// undefined order.
func (c *Cache) Symbols() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.data))
	for s := range c.data {
		out = append(out, s)
	}
	return out
}

// Latest returns the most recent live price for a symbol, and false when the
// symbol has never been quoted. It reads the last received quote rather than
// the ring, so a symbol whose ring holds only seeded history reports false —
// backfilled closes are history, not a current price.
func (c *Cache) Latest(symbol string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	q, ok := c.last[symbol]
	if !ok {
		return 0, false
	}
	return q.Price, true
}

// LatestQuote returns the most recent full Quote for a symbol, including
// change %, direction, and bid/ask. Returns false when the symbol has
// never been quoted.
func (c *Cache) LatestQuote(symbol string) (provider.Quote, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	q, ok := c.last[symbol]
	return q, ok
}

// ring is a simple circular buffer for float64 prices and the timestamp each
// price arrived with.
type ring struct {
	buf   []float64
	times []time.Time
	head  int
	size  int
	cap   int
}

func newRing(capacity int) *ring {
	return &ring{
		buf:   make([]float64, capacity),
		times: make([]time.Time, capacity),
		cap:   capacity,
	}
}

func (r *ring) push(v float64, t time.Time) {
	r.buf[r.head] = v
	r.times[r.head] = t
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// series returns the buffered prices and their timestamps, oldest first.
func (r *ring) series() ([]float64, []time.Time) {
	if r.size == 0 {
		return nil, nil
	}
	prices := make([]float64, r.size)
	times := make([]time.Time, r.size)
	start := (r.head - r.size + r.cap) % r.cap
	for i := range r.size {
		j := (start + i) % r.cap
		prices[i] = r.buf[j]
		times[i] = r.times[j]
	}
	return prices, times
}
