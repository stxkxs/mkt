package market

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"

	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/symbol"
)

// dispatchBuffer sizes the fan-out queue between the provider read loop and the
// onQuote dispatcher. Quotes are dropped when this fills — the cache always has
// the latest price, and the next quote will refresh the UI and re-check alerts.
// Anything that must not miss a quote registers an observer instead.
const dispatchBuffer = 256

// observerBacklogMax bounds an individual observer's pending queue. It is a
// safety valve, not a working limit: at a realistic 100 quotes/second a
// permanently wedged observer needs roughly three hours to reach it. Past
// that the oldest quotes are discarded (counted by ObserverDrops) so one
// stuck callback cannot exhaust memory.
const observerBacklogMax = 1 << 20

// Hub aggregates quote providers and fans out updates.
//
// There are two delivery paths and they have different guarantees:
//
//   - the observer path (AddObserver) is reliable — every quote reaches every
//     observer, in arrival order, and is never dropped;
//   - the dispatch path (the onQuote passed to Start) is best-effort — quotes
//     are dropped when the consumer falls behind, so the UI never applies
//     back-pressure to the providers. Drops counts them.
//
// Anything stateful — alert evaluation, sequence matching, recording — belongs
// on the observer path. Rendering belongs on the dispatch path.
type Hub struct {
	quoteProviders []provider.QuoteProvider
	cache          *Cache
	quoteCh        chan provider.Quote

	drops    atomic.Uint64
	obsDrops atomic.Uint64

	mu         sync.RWMutex
	observers  []*observer
	unroutable []string
	stopped    bool
}

// NewHub creates a new market hub.
func NewHub(cache *Cache, providers ...provider.QuoteProvider) *Hub {
	return &Hub{
		quoteProviders: providers,
		cache:          cache,
		quoteCh:        make(chan provider.Quote, 128),
	}
}

// Start launches all providers and the fan-out loop and returns the symbols no
// provider claimed.
//
// Symbols are canonicalized (symbol.Canonical) before routing and deduplicated,
// so "btc", "BTCUSDT" and "BTC-USD" subscribe once, to Coinbase. A symbol no
// provider Supports is returned in the caller's original spelling — a typo like
// "APPL" used to be discarded in silence, which made it indistinguishable from a
// symbol that simply had not ticked yet. Callers should surface the result;
// Unroutable returns the same list later.
//
// onQuote is called for each quote received (used to send to the TUI) on its own
// goroutine, so a slow onQuote cannot stall providers. It is best-effort: see
// Hub and Drops.
func (h *Hub) Start(ctx context.Context, symbols []string, onQuote func(provider.Quote)) []string {
	providerSymbols, unroutable := h.route(symbols)

	h.mu.Lock()
	h.unroutable = unroutable
	h.mu.Unlock()

	// Start each provider
	for i, p := range h.quoteProviders {
		syms := providerSymbols[i]
		if len(syms) == 0 {
			continue
		}
		go func(prov provider.QuoteProvider, s []string) {
			// A cancelled context is how every clean shutdown ends. Logging it
			// makes a normal quit print "provider yahoo error: context
			// canceled", which reads like a crash.
			if err := prov.Subscribe(ctx, s, h.quoteCh); err != nil && !isShutdown(err) {
				log.Printf("provider %s error: %v", prov.Name(), err)
			}
		}(p, syms)
	}

	// Reader: cache.Push and observer hand-off are cheap and reliable; dispatch
	// is best-effort so the read loop never blocks on a slow consumer.
	dispatchCh := make(chan provider.Quote, dispatchBuffer)
	go func() {
		defer h.stopObservers()
		for {
			select {
			case <-ctx.Done():
				return
			case q := <-h.quoteCh:
				// Providers echo the venue's own spelling. Canonicalize once,
				// here, so the cache, the alert rules and the config all key
				// the same quote the same way.
				q.Symbol = symbol.Canonical(q.Symbol)
				h.cache.Push(q)
				h.notify(q)
				if onQuote == nil {
					continue
				}
				select {
				case dispatchCh <- q:
				default:
					// Consumer backed up; skip rather than block providers.
					// The cache already holds the latest price.
					h.drops.Add(1)
				}
			}
		}
	}()

	// Dispatcher: calls onQuote. May block freely without starving providers.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case q := <-dispatchCh:
				onQuote(q)
			}
		}
	}()

	return unroutable
}

// route canonicalizes and deduplicates symbols, then assigns each to the first
// provider that claims it. It returns the per-provider symbol lists keyed by
// index into h.quoteProviders, plus the symbols nothing claimed (in the
// caller's original spelling, in input order).
func (h *Hub) route(symbols []string) (map[int][]string, []string) {
	providerSymbols := make(map[int][]string)
	seen := make(map[string]bool, len(symbols))
	var unroutable []string
	for _, raw := range symbols {
		sym := symbol.Canonical(raw)
		if sym == "" || seen[sym] {
			continue
		}
		seen[sym] = true

		routed := false
		for i, p := range h.quoteProviders {
			if p.Supports(sym) {
				providerSymbols[i] = append(providerSymbols[i], sym)
				routed = true
				break
			}
		}
		if !routed {
			unroutable = append(unroutable, raw)
		}
	}
	return providerSymbols, unroutable
}

// Unroutable returns the symbols from the most recent Start that no provider
// claimed, in the caller's original spelling. Nil before Start runs.
func (h *Hub) Unroutable() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.unroutable) == 0 {
		return nil
	}
	return append([]string(nil), h.unroutable...)
}

// Drops returns the number of quotes dropped because the onQuote consumer could
// not keep up. It only moves when the UI is behind; a non-zero and climbing
// value means the dispatch path is lossy right now. Observers are unaffected.
func (h *Hub) Drops() uint64 { return h.drops.Load() }

// ObserverDrops returns the number of quotes discarded because an observer
// accumulated more than observerBacklogMax pending quotes. Any non-zero value
// means an observer is wedged — under normal operation this stays at zero.
func (h *Hub) ObserverDrops() uint64 { return h.obsDrops.Load() }

// AddObserver registers fn on the reliable path. Every quote the hub reads is
// delivered to every observer, in arrival order, with the symbol already
// canonicalized — nothing is dropped by back-pressure, unlike the onQuote
// dispatch path.
//
// The guarantee to the reader loop is that hand-off is non-blocking: each
// observer owns a goroutine and its own unbounded-until-observerBacklogMax
// queue, so an observer that blocks forever costs one parked goroutine and its
// own backlog, and can never stall quote ingestion or another observer. The
// cost of the never-drop promise is memory, so an observer should still return
// promptly; ObserverBacklog and ObserverDrops expose one that does not.
//
// Observers may be registered before or after Start. Registering after the hub
// has shut down is a no-op.
func (h *Hub) AddObserver(fn func(provider.Quote)) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	o := newObserver(fn, &h.obsDrops)
	h.observers = append(h.observers, o)
	go o.run()
}

// ObserverBacklog returns the largest number of quotes queued for any single
// observer. It is zero when observers are keeping up.
func (h *Hub) ObserverBacklog() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var max int
	for _, o := range h.observers {
		if n := o.pending(); n > max {
			max = n
		}
	}
	return max
}

// notify hands a quote to every observer. Never blocks.
func (h *Hub) notify(q provider.Quote) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, o := range h.observers {
		o.push(q)
	}
}

// stopObservers wakes every observer pump so it exits, and refuses later
// registrations. Pending quotes are discarded — the hub is shutting down.
func (h *Hub) stopObservers() {
	h.mu.Lock()
	observers := h.observers
	h.observers = nil
	h.stopped = true
	h.mu.Unlock()
	for _, o := range observers {
		o.stop()
	}
}

// Cache returns the hub's quote cache.
func (h *Hub) Cache() *Cache {
	return h.cache
}

// isShutdown reports whether err is a provider reporting that its context
// ended, which is the normal way every subscription terminates.
func isShutdown(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// observer is one registered callback plus the queue that decouples it from the
// reader loop. The queue is a mutex/cond FIFO rather than a buffered channel
// because it must never block the producer and must be closable from either
// side without a send-on-closed race.
type observer struct {
	fn    func(provider.Quote)
	drops *atomic.Uint64
	max   int // queue cap; observerBacklogMax outside tests

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []provider.Quote
	stopped bool
}

func newObserver(fn func(provider.Quote), drops *atomic.Uint64) *observer {
	o := &observer{fn: fn, drops: drops, max: observerBacklogMax}
	o.cond = sync.NewCond(&o.mu)
	return o
}

// push enqueues q and returns immediately. When the queue is at its cap the
// oldest quote is discarded and counted, so a wedged observer trades staleness
// for a bounded footprint.
func (o *observer) push(q provider.Quote) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return
	}
	if len(o.queue) >= o.max {
		o.queue = o.queue[1:]
		o.drops.Add(1)
	}
	o.queue = append(o.queue, q)
	o.cond.Signal()
}

func (o *observer) pending() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.queue)
}

// stop discards the backlog and wakes the pump. It does not wait for an
// in-flight callback: the observer signature offers no way to cancel one, and
// waiting would reintroduce the stall this design exists to prevent.
func (o *observer) stop() {
	o.mu.Lock()
	o.stopped = true
	o.queue = nil
	o.mu.Unlock()
	o.cond.Broadcast()
}

// run drains the queue in FIFO order until stopped.
func (o *observer) run() {
	for {
		o.mu.Lock()
		for len(o.queue) == 0 && !o.stopped {
			o.cond.Wait()
		}
		if o.stopped {
			o.mu.Unlock()
			return
		}
		batch := o.queue
		o.queue = nil
		o.mu.Unlock()

		for _, q := range batch {
			o.fn(q)
		}
	}
}
