package indicator

import (
	"math"

	"github.com/stxkxs/mkt/internal/provider"
)

// Pattern identifies a detected candle pattern. PatternNone means no
// pattern was detected at that bar.
type Pattern int

const (
	PatternNone Pattern = iota
	PatternDoji
	PatternHammer
	PatternShootingStar
	PatternBullishEngulfing
	PatternBearishEngulfing
)

// Name returns the human-readable label for the pattern.
func (p Pattern) Name() string {
	switch p {
	case PatternDoji:
		return "Doji"
	case PatternHammer:
		return "Hammer"
	case PatternShootingStar:
		return "Shooting Star"
	case PatternBullishEngulfing:
		return "Bullish Engulfing"
	case PatternBearishEngulfing:
		return "Bearish Engulfing"
	}
	return ""
}

// IsBullish reports whether the pattern is a bullish reversal cue.
func (p Pattern) IsBullish() bool {
	return p == PatternHammer || p == PatternBullishEngulfing
}

// IsBearish reports whether the pattern is a bearish reversal cue.
func (p Pattern) IsBearish() bool {
	return p == PatternShootingStar || p == PatternBearishEngulfing
}

// Patterns detects classic candle patterns in the input series. Returns
// one Pattern per candle; PatternNone for bars without a match or
// without sufficient context (e.g., two-candle patterns at index 0).
// Detection order: Doji → Hammer → Shooting Star → Engulfing. First
// matching pattern wins.
func Patterns(candles []provider.OHLCV) []Pattern {
	out := make([]Pattern, len(candles))
	for i, c := range candles {
		body := math.Abs(c.Close - c.Open)
		rng := c.High - c.Low
		if rng <= 0 {
			continue
		}

		// Doji: very small body relative to range
		if body <= 0.1*rng {
			out[i] = PatternDoji
			continue
		}

		bodyTop := max(c.Open, c.Close)
		bodyBot := min(c.Open, c.Close)
		upperWick := c.High - bodyTop
		lowerWick := bodyBot - c.Low

		// Hammer: small body in upper third, long lower wick
		if body < rng/3 && lowerWick >= 2*body && upperWick <= body {
			out[i] = PatternHammer
			continue
		}

		// Shooting Star: small body in lower third, long upper wick
		if body < rng/3 && upperWick >= 2*body && lowerWick <= body {
			out[i] = PatternShootingStar
			continue
		}

		// Engulfing patterns need a previous candle
		if i == 0 {
			continue
		}
		prev := candles[i-1]
		prevBullish := prev.Close > prev.Open
		prevBearish := prev.Close < prev.Open
		currBullish := c.Close > c.Open
		currBearish := c.Close < c.Open

		// Bullish Engulfing
		if prevBearish && currBullish &&
			c.Open <= prev.Close && c.Close >= prev.Open {
			out[i] = PatternBullishEngulfing
			continue
		}

		// Bearish Engulfing
		if prevBullish && currBearish &&
			c.Open >= prev.Close && c.Close <= prev.Open {
			out[i] = PatternBearishEngulfing
		}
	}
	return out
}
