package market

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

// TestServedIntervalRoutesToProvider pins the honesty contract the chart
// header depends on: Yahoo cannot serve 4h and answers with 1h bars, while a
// provider that aggregates to the requested bucket reports it unchanged.
func TestServedIntervalRoutesToProvider(t *testing.T) {
	m := NewMultiHistoryProvider(yahoo.New(time.Minute), coinbase.New())

	tests := []struct {
		name      string
		symbol    string
		requested provider.Interval
		want      provider.Interval
	}{
		{"yahoo has no 4h bucket", "AAPL", provider.Interval4h, provider.Interval1h},
		{"yahoo 1d is native", "AAPL", provider.Interval1d, provider.Interval1d},
		{"yahoo 1h is native", "AAPL", provider.Interval1h, provider.Interval1h},
		{"coinbase aggregates 4h", "BTC-USD", provider.Interval4h, provider.Interval4h},
		{"coinbase aggregates 1w", "BTC-USD", provider.Interval1w, provider.Interval1w},
		// Nothing claims this symbol; echoing the request beats inventing an
		// answer the caller would render as fact.
		{"unroutable echoes the request", "!!!", provider.Interval4h, provider.Interval4h},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.ServedInterval(tt.symbol, tt.requested); got != tt.want {
				t.Errorf("ServedInterval(%q, %q) = %q, want %q", tt.symbol, tt.requested, got, tt.want)
			}
		})
	}
}

// stubHistory is a HistoryProvider that claims exactly one symbol.
type stubHistory struct {
	symbol  string
	candles []provider.OHLCV
	err     error
}

func (s stubHistory) Name() string             { return "stub" }
func (s stubHistory) Supports(sym string) bool { return sym == s.symbol }
func (s stubHistory) History(context.Context, provider.HistoryParams) ([]provider.OHLCV, error) {
	return s.candles, s.err
}

func TestHistoryRoutesToFirstSupportingProvider(t *testing.T) {
	want := []provider.OHLCV{{Close: 42}}
	m := NewMultiHistoryProvider(
		stubHistory{symbol: "AAA", candles: []provider.OHLCV{{Close: 1}}},
		stubHistory{symbol: "BBB", candles: want},
	)

	got, err := m.History(context.Background(), provider.HistoryParams{Symbol: "BBB"})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 1 || got[0].Close != 42 {
		t.Errorf("routed to the wrong provider: %+v", got)
	}
}

func TestHistoryUnroutableSymbolErrors(t *testing.T) {
	m := NewMultiHistoryProvider(stubHistory{symbol: "AAA"})

	if _, err := m.History(context.Background(), provider.HistoryParams{Symbol: "ZZZ"}); err == nil {
		t.Fatal("want an error for a symbol no provider serves")
	}
}

func TestHistoryPropagatesProviderError(t *testing.T) {
	sentinel := errors.New("upstream down")
	m := NewMultiHistoryProvider(stubHistory{symbol: "AAA", err: sentinel})

	_, err := m.History(context.Background(), provider.HistoryParams{Symbol: "AAA"})
	if !errors.Is(err, sentinel) {
		t.Errorf("want the provider's error to survive, got %v", err)
	}
}
