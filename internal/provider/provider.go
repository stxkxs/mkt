package provider

import "context"

// QuoteProvider streams real-time quotes for symbols.
type QuoteProvider interface {
	Name() string
	Supports(symbol string) bool
	Subscribe(ctx context.Context, symbols []string, out chan<- Quote) error
}

// HistoryProvider fetches historical OHLCV data.
type HistoryProvider interface {
	Name() string
	Supports(symbol string) bool
	History(ctx context.Context, params HistoryParams) ([]OHLCV, error)
}
