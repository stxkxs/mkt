// Package observe is a tiny, dependency-free metrics registry used to
// expose provider health on the /metrics endpoint.
//
// The design intentionally avoids pulling in prometheus/client_golang
// to preserve the project's "single binary, minimal deps" property.
// Counters self-register at construction; api.Server's /metrics handler
// reads the registry via Snapshot.
package observe

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Counter is a monotonic uint64 with a stable Prometheus-style name.
// Safe for concurrent use.
type Counter struct {
	name string
	val  atomic.Uint64
}

// Inc increments the counter by one.
func (c *Counter) Inc() { c.val.Add(1) }

// Add increments the counter by n.
func (c *Counter) Add(n uint64) { c.val.Add(n) }

// Name returns the counter's stable name (used in /metrics output).
func (c *Counter) Name() string { return c.name }

// Value returns the current count.
func (c *Counter) Value() uint64 { return c.val.Load() }

var (
	registryMu sync.Mutex
	registry   []*Counter
)

// NewCounter constructs a counter and registers it. Names should follow
// Prometheus conventions (snake_case, _total suffix for counters).
func NewCounter(name string) *Counter {
	c := &Counter{name: name}
	registryMu.Lock()
	registry = append(registry, c)
	registryMu.Unlock()
	return c
}

// Snapshot returns a name→value map of every registered counter, in a
// freshly-allocated map. Useful for tests and for /metrics emission.
func Snapshot() map[string]uint64 {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make(map[string]uint64, len(registry))
	for _, c := range registry {
		out[c.name] = c.val.Load()
	}
	return out
}

// SortedNames returns every registered counter's name in lexicographic
// order. Useful for stable /metrics output.
func SortedNames() []string {
	registryMu.Lock()
	defer registryMu.Unlock()
	names := make([]string, len(registry))
	for i, c := range registry {
		names[i] = c.name
	}
	sort.Strings(names)
	return names
}
