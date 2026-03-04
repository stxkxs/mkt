package market

import (
	"sync"

	"github.com/stxkxs/mkt/internal/provider"
)

const defaultRingSize = 60

// Cache stores recent quotes per symbol for sparkline rendering.
type Cache struct {
	mu       sync.RWMutex
	data     map[string]*ring
	ringSize int
}

// NewCache creates a new quote cache.
func NewCache(ringSize int) *Cache {
	if ringSize <= 0 {
		ringSize = defaultRingSize
	}
	return &Cache{
		data:     make(map[string]*ring),
		ringSize: ringSize,
	}
}

// Push adds a quote to the symbol's ring buffer.
func (c *Cache) Push(q provider.Quote) {
	c.mu.Lock()
	defer c.mu.Unlock()

	r, ok := c.data[q.Symbol]
	if !ok {
		r = newRing(c.ringSize)
		c.data[q.Symbol] = r
	}
	r.push(q.Price)
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
