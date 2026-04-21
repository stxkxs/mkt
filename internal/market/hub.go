package market

import (
	"context"
	"log"

	"github.com/stxkxs/mkt/internal/provider"
)

// dispatchBuffer sizes the fan-out queue between the provider read loop and the
// onQuote dispatcher. Quotes are dropped when this fills — the cache always has
// the latest price, and the next quote will refresh the UI and re-check alerts.
const dispatchBuffer = 256

// Hub aggregates quote providers and fans out updates.
type Hub struct {
	quoteProviders []provider.QuoteProvider
	cache          *Cache
	quoteCh        chan provider.Quote
}

// NewHub creates a new market hub.
func NewHub(cache *Cache, providers ...provider.QuoteProvider) *Hub {
	return &Hub{
		quoteProviders: providers,
		cache:          cache,
		quoteCh:        make(chan provider.Quote, 128),
	}
}

// Start launches all providers and the fan-out loop.
// onQuote is called for each quote received (used to send to TUI).
// The dispatcher runs on its own goroutine so a slow onQuote cannot stall providers.
func (h *Hub) Start(ctx context.Context, symbols []string, onQuote func(provider.Quote)) {
	// Route symbols to providers
	providerSymbols := make(map[int][]string)
	for _, sym := range symbols {
		for i, p := range h.quoteProviders {
			if p.Supports(sym) {
				providerSymbols[i] = append(providerSymbols[i], sym)
				break
			}
		}
	}

	// Start each provider
	for i, p := range h.quoteProviders {
		syms := providerSymbols[i]
		if len(syms) == 0 {
			continue
		}
		go func(prov provider.QuoteProvider, s []string) {
			if err := prov.Subscribe(ctx, s, h.quoteCh); err != nil {
				log.Printf("provider %s error: %v", prov.Name(), err)
			}
		}(p, syms)
	}

	// Reader: cache.Push is cheap; dispatch is best-effort so the read loop
	// never blocks on a slow consumer.
	dispatchCh := make(chan provider.Quote, dispatchBuffer)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case q := <-h.quoteCh:
				h.cache.Push(q)
				if onQuote == nil {
					continue
				}
				select {
				case dispatchCh <- q:
				default:
					// Consumer backed up; skip rather than block providers.
					// The cache already holds the latest price.
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
}

// Cache returns the hub's quote cache.
func (h *Hub) Cache() *Cache {
	return h.cache
}
