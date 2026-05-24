package indicator

import (
	"testing"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestPatternsDoji(t *testing.T) {
	// Tiny body, large range
	candles := []provider.OHLCV{
		{Open: 100, High: 110, Low: 90, Close: 100.5},
	}
	got := Patterns(candles)
	if got[0] != PatternDoji {
		t.Errorf("expected Doji, got %v", got[0])
	}
}

func TestPatternsHammer(t *testing.T) {
	// Body 100-102 (size 2), low at 90 (lower wick 10), high at 103 (upper wick 1)
	candles := []provider.OHLCV{
		{Open: 100, High: 103, Low: 90, Close: 102},
	}
	got := Patterns(candles)
	if got[0] != PatternHammer {
		t.Errorf("expected Hammer, got %v", got[0])
	}
}

func TestPatternsShootingStar(t *testing.T) {
	// Body 100-98 (size 2), high at 110 (upper wick 8), low at 97 (lower wick 1)
	candles := []provider.OHLCV{
		{Open: 100, High: 110, Low: 97, Close: 98},
	}
	got := Patterns(candles)
	if got[0] != PatternShootingStar {
		t.Errorf("expected Shooting Star, got %v", got[0])
	}
}

func TestPatternsBullishEngulfing(t *testing.T) {
	candles := []provider.OHLCV{
		// Prev: bearish, body 105 → 100
		{Open: 105, High: 106, Low: 99, Close: 100},
		// Curr: bullish, body 99 → 107 — engulfs prev body
		{Open: 99, High: 108, Low: 98, Close: 107},
	}
	got := Patterns(candles)
	if got[1] != PatternBullishEngulfing {
		t.Errorf("expected Bullish Engulfing at i=1, got %v", got[1])
	}
}

func TestPatternsBearishEngulfing(t *testing.T) {
	candles := []provider.OHLCV{
		// Prev: bullish, body 100 → 105
		{Open: 100, High: 106, Low: 99, Close: 105},
		// Curr: bearish, body 106 → 99 — engulfs prev body
		{Open: 106, High: 107, Low: 98, Close: 99},
	}
	got := Patterns(candles)
	if got[1] != PatternBearishEngulfing {
		t.Errorf("expected Bearish Engulfing at i=1, got %v", got[1])
	}
}

func TestPatternsNoneForOrdinaryCandle(t *testing.T) {
	candles := []provider.OHLCV{
		// Medium body in mid-range, no extreme wicks
		{Open: 100, High: 102, Low: 99, Close: 101.5},
	}
	got := Patterns(candles)
	if got[0] != PatternNone {
		t.Errorf("expected None, got %v", got[0])
	}
}

func TestPatternsZeroRangeReturnsNone(t *testing.T) {
	candles := []provider.OHLCV{
		{Open: 100, High: 100, Low: 100, Close: 100},
	}
	got := Patterns(candles)
	if got[0] != PatternNone {
		t.Errorf("expected None for zero-range, got %v", got[0])
	}
}

func TestPatternsEmptyInput(t *testing.T) {
	got := Patterns(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestPatternsEngulfingNeedsTwoBars(t *testing.T) {
	// Single bullish candle that, if a prev bearish existed, would be engulfing
	candles := []provider.OHLCV{
		{Open: 100, High: 108, Low: 99, Close: 107},
	}
	got := Patterns(candles)
	if got[0] == PatternBullishEngulfing {
		t.Errorf("engulfing should require a previous bar, got %v", got[0])
	}
}

func TestPatternBullishBearishFlags(t *testing.T) {
	if !PatternHammer.IsBullish() {
		t.Error("Hammer should be bullish")
	}
	if !PatternBullishEngulfing.IsBullish() {
		t.Error("Bullish Engulfing should be bullish")
	}
	if !PatternShootingStar.IsBearish() {
		t.Error("Shooting Star should be bearish")
	}
	if !PatternBearishEngulfing.IsBearish() {
		t.Error("Bearish Engulfing should be bearish")
	}
	if PatternDoji.IsBullish() || PatternDoji.IsBearish() {
		t.Error("Doji should be neither bullish nor bearish")
	}
}
