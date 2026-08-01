package market

import (
	"context"
	"fmt"

	"github.com/stxkxs/mkt/internal/provider"
)

// MultiHistoryProvider routes history requests to the appropriate provider.
type MultiHistoryProvider struct {
	providers []provider.HistoryProvider
}

// NewMultiHistoryProvider creates a history provider that routes by symbol.
func NewMultiHistoryProvider(providers ...provider.HistoryProvider) *MultiHistoryProvider {
	return &MultiHistoryProvider{providers: providers}
}

// History fetches OHLCV data from the first matching provider.
func (m *MultiHistoryProvider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	for _, p := range m.providers {
		if p.Supports(params.Symbol) {
			return p.History(ctx, params)
		}
	}
	return nil, fmt.Errorf("no history provider for %s", params.Symbol)
}

// servedIntervalProvider is implemented by a provider that cannot serve every
// interval natively and answers with a different bar size.
type servedIntervalProvider interface {
	ServedInterval(requested provider.Interval) provider.Interval
}

// ServedInterval reports the bar size the routed provider will actually
// return for symbol at requested.
//
// Yahoo has no 4h bucket and serves a 4h request as 1h bars; US equity
// sessions are 6.5 hours, so folding those into 4h buckets would produce
// uneven, partly synthetic candles. Reporting the real bar size instead lets
// the chart header say "4h (1h bars)" rather than claiming a granularity
// nobody delivered. Providers that serve exactly what was asked (Coinbase
// aggregates) need not implement anything.
//
// This is what satisfies chart.ServedIntervalProvider.
func (m *MultiHistoryProvider) ServedInterval(symbol string, requested provider.Interval) provider.Interval {
	for _, p := range m.providers {
		if !p.Supports(symbol) {
			continue
		}
		if sp, ok := p.(servedIntervalProvider); ok {
			if got := sp.ServedInterval(requested); got != "" {
				return got
			}
		}
		return requested
	}
	return requested
}
