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
