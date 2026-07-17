package market

import (
	"sync"

	"github.com/stxkxs/mkt/internal/provider"
)

const defaultRingSize = 60

// Cache stores recent quotes per symbol for sparkline rendering. It also
// retains the most recent full Quote per symbol so consumers that need
// more than the price (change %, direction, bid/ask) can read it back —
// the ring buffer intentionally keeps only prices for the sparkline.
type Cache struct {
	mu       sync.RWMutex
	data     map[string]*ring
	last     map[string]provider.Quote
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
	r.push(q.Price)
	c.last[q.Symbol] = q
}

// Prices returns the recent prices for a symbol.
func (c *Cache) Prices(symbol string) []float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	r, ok := c.data[symbol]
	if !ok {
		return nil
	}
	return r.values()
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

// Latest returns the most recent quote data for a symbol.
func (c *Cache) Latest(symbol string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	r, ok := c.data[symbol]
	if !ok {
		return 0, false
	}
	vals := r.values()
	if len(vals) == 0 {
		return 0, false
	}
	return vals[len(vals)-1], true
}

// LatestQuote returns the most recent full Quote for a symbol, including
// change %, direction, and bid/ask. Returns false when the symbol has
// never been seen.
func (c *Cache) LatestQuote(symbol string) (provider.Quote, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	q, ok := c.last[symbol]
	return q, ok
}

// ring is a simple circular buffer for float64 prices.
type ring struct {
	buf  []float64
	head int
	size int
	cap  int
}

func newRing(capacity int) *ring {
	return &ring{
		buf: make([]float64, capacity),
		cap: capacity,
	}
}

func (r *ring) push(v float64) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

func (r *ring) values() []float64 {
	if r.size == 0 {
		return nil
	}
	out := make([]float64, r.size)
	start := (r.head - r.size + r.cap) % r.cap
	for i := range r.size {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}
