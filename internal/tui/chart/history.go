package chart

import (
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// historyLimit is the number of bars requested from the provider. It is
// comfortably more than any terminal can display, which is what lets the
// indicators warm up off-screen and stay stable as the user zooms.
const historyLimit = 200

// historyCacheTTL is how long a fetched series stays reusable. Long
// enough that flipping intervals with [ and ] costs nothing, short
// enough that the chart still tracks the market.
const historyCacheTTL = 30 * time.Second

// historyCacheSize is the number of symbol+interval series kept before
// the oldest is evicted: the seven intervals for two symbols, plus
// headroom. Bounded because a long session browsing a large watchlist
// would otherwise pin every series it ever loaded.
const historyCacheSize = 16

// ServedIntervalProvider is optionally implemented by history providers
// that cannot serve every interval natively and answer with a different
// one. Yahoo has no 4h bucket, for example, and serves a 4h request as
// 1h bars. When the provider implements this, the chart labels its
// header from the served interval so it never claims a resolution it is
// not actually drawing; when it does not, the header shows the requested
// interval as before.
type ServedIntervalProvider interface {
	ServedInterval(symbol string, requested provider.Interval) provider.Interval
}

// historyKey identifies one cached series.
type historyKey struct {
	symbol   string
	interval provider.Interval
}

type historyEntry struct {
	data    []provider.OHLCV
	fetched time.Time
}

// historyCache is a small, bounded, TTL'd cache of fetched candle series
// keyed by symbol and interval.
//
// Every [ or ] press used to fire a fresh network request; the upstream
// endpoints are unauthenticated and rate limited, so a burst of key
// presses could earn the whole process a cooldown. With the cache a
// burst costs at most one request per interval.
//
// Cached slices are shared with callers and must be treated as
// read-only. All methods are safe on a nil receiver so a zero-value
// Model degrades to "no caching" instead of panicking.
type historyCache struct {
	mu      sync.Mutex
	entries map[historyKey]historyEntry
	order   []historyKey // insertion order, oldest first
	ttl     time.Duration
	max     int
	now     func() time.Time
}

// newHistoryCache creates a cache with the package defaults.
func newHistoryCache() *historyCache {
	return &historyCache{
		entries: make(map[historyKey]historyEntry),
		ttl:     historyCacheTTL,
		max:     historyCacheSize,
		now:     time.Now,
	}
}

// clock returns the cache's time source.
func (c *historyCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// get returns a cached series when one is present and still fresh.
func (c *historyCache) get(sym string, iv provider.Interval) ([]provider.OHLCV, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return nil, false
	}
	key := historyKey{symbol: sym, interval: iv}
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.ttl > 0 && c.clock().Sub(e.fetched) >= c.ttl {
		c.dropLocked(key)
		return nil, false
	}
	return e.data, true
}

// put stores a series, evicting the oldest entries once the cache is
// full. A nil series is not cached: an empty answer is usually a
// transient upstream failure and should be retried, not remembered.
func (c *historyCache) put(sym string, iv provider.Interval, data []provider.OHLCV) {
	if c == nil || len(data) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[historyKey]historyEntry)
	}
	key := historyKey{symbol: sym, interval: iv}
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = historyEntry{data: data, fetched: c.clock()}

	max := c.max
	if max < 1 {
		max = historyCacheSize
	}
	for len(c.order) > max {
		c.dropLocked(c.order[0])
	}
}

// dropLocked removes one key from both the map and the order list. The
// caller must hold c.mu.
func (c *historyCache) dropLocked(key historyKey) {
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// size is the number of live entries; used by tests.
func (c *historyCache) size() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
