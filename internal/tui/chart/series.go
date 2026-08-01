package chart

import (
	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/provider"
)

// Indicator parameters. Declared once so the overlay, the sub-panel and
// the header readout can never disagree about which period they are
// showing, and so the labels in indicatorNames stay honest.
const (
	maPeriod         = 20
	bollingerPeriod  = 20
	bollingerMult    = 2.0
	rsiPeriod        = 14
	atrPeriod        = 14
	adxPeriod        = 14
	stochKPeriod     = 14
	stochDPeriod     = 3
	macdFastPeriod   = 12
	macdSlowPeriod   = 26
	macdSignalPeriod = 9
)

// indicatorSet holds every indicator series the chart can draw.
//
// Each series is computed over the FULL fetched history and only then
// narrowed to the visible window. That ordering is the whole point:
// computing over the visible slice — which is what the chart used to do
// — reseeds every moving average, RSI and MACD from whatever happens to
// be on screen, so an "SMA(20)" at a given date changed value every time
// the user pressed + or -. The displayed number was simply wrong.
//
// Cumulative indicators (VWAP, OBV) are anchored at the start of the
// fetched history for the same reason, rather than at the left edge of
// the viewport.
type indicatorSet struct {
	sma      []float64
	ema      []float64
	bb       indicator.BollingerResult
	rsi      []float64
	macd     indicator.MACDResult
	vwap     []float64
	obv      []float64
	atr      []float64
	stochK   []float64
	stochD   []float64
	adx      []float64
	plusDI   []float64
	minusDI  []float64
	patterns []indicator.Pattern

	// pivots are derived from the last completed bar of the full
	// history, so they too are independent of the zoom level.
	pivots    indicator.PivotLevels
	hasPivots bool
}

// columns splits a candle series into the parallel float slices the
// indicator package takes.
func columns(candles []provider.OHLCV) (opens, highs, lows, closes, volumes []float64) {
	n := len(candles)
	opens = make([]float64, n)
	highs = make([]float64, n)
	lows = make([]float64, n)
	closes = make([]float64, n)
	volumes = make([]float64, n)
	for i, c := range candles {
		opens[i] = c.Open
		highs[i] = c.High
		lows[i] = c.Low
		closes[i] = c.Close
		volumes[i] = c.Volume
	}
	return opens, highs, lows, closes, volumes
}

// computeIndicators evaluates the active indicators over the full
// fetched history. Only the enabled ones are computed — a chart with no
// indicators on does no indicator work at all.
func computeIndicators(full []provider.OHLCV, active [indCount]bool) indicatorSet {
	var set indicatorSet
	if len(full) == 0 {
		return set
	}
	_, highs, lows, closes, volumes := columns(full)

	if active[IndSMA] {
		set.sma = indicator.SMA(closes, maPeriod)
	}
	if active[IndEMA] {
		set.ema = indicator.EMA(closes, maPeriod)
	}
	if active[IndBollinger] {
		set.bb = indicator.Bollinger(closes, bollingerPeriod, bollingerMult)
	}
	if active[IndRSI] {
		set.rsi = indicator.RSI(closes, rsiPeriod)
	}
	if active[IndMACD] {
		set.macd = indicator.MACD(closes, macdFastPeriod, macdSlowPeriod, macdSignalPeriod)
	}
	if active[IndVWAP] {
		set.vwap = indicator.VWAP(highs, lows, closes, volumes)
	}
	if active[IndOBV] {
		set.obv = indicator.OBV(closes, volumes)
	}
	if active[IndATR] {
		set.atr = indicator.ATR(highs, lows, closes, atrPeriod)
	}
	if active[IndStoch] {
		set.stochK, set.stochD = indicator.Stochastic(highs, lows, closes, stochKPeriod, stochDPeriod)
	}
	if active[IndADX] {
		set.adx, set.plusDI, set.minusDI = indicator.ADX(highs, lows, closes, adxPeriod)
	}
	if active[IndPatterns] {
		set.patterns = indicator.Patterns(full)
	}
	if active[IndPivots] && len(full) >= 2 {
		prev := full[len(full)-2]
		set.pivots = indicator.PivotsClassic(prev.High, prev.Low, prev.Close)
		set.hasPivots = true
	}
	return set
}

// window narrows every computed series to the half-open range
// [start, end) of the full history.
func (s indicatorSet) window(start, end int) indicatorSet {
	out := indicatorSet{
		sma:     sliceFloats(s.sma, start, end),
		ema:     sliceFloats(s.ema, start, end),
		rsi:     sliceFloats(s.rsi, start, end),
		vwap:    sliceFloats(s.vwap, start, end),
		obv:     sliceFloats(s.obv, start, end),
		atr:     sliceFloats(s.atr, start, end),
		stochK:  sliceFloats(s.stochK, start, end),
		stochD:  sliceFloats(s.stochD, start, end),
		adx:     sliceFloats(s.adx, start, end),
		plusDI:  sliceFloats(s.plusDI, start, end),
		minusDI: sliceFloats(s.minusDI, start, end),
		bb: indicator.BollingerResult{
			Upper:  sliceFloats(s.bb.Upper, start, end),
			Middle: sliceFloats(s.bb.Middle, start, end),
			Lower:  sliceFloats(s.bb.Lower, start, end),
		},
		macd: indicator.MACDResult{
			MACD:      sliceFloats(s.macd.MACD, start, end),
			Signal:    sliceFloats(s.macd.Signal, start, end),
			Histogram: sliceFloats(s.macd.Histogram, start, end),
		},
		pivots:    s.pivots,
		hasPivots: s.hasPivots,
	}
	if len(s.patterns) > 0 {
		lo, hi := clampRange(len(s.patterns), start, end)
		out.patterns = s.patterns[lo:hi]
	}
	return out
}

// sliceFloats narrows a series, tolerating a range that does not fit —
// the indicator package is free to return a shorter slice than the input
// for a degenerate period, and a viewport must never index past it.
func sliceFloats(v []float64, start, end int) []float64 {
	if len(v) == 0 {
		return nil
	}
	lo, hi := clampRange(len(v), start, end)
	return v[lo:hi]
}

// clampRange pins [start, end) inside a slice of length n.
func clampRange(n, start, end int) (int, int) {
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	if end < start {
		end = start
	}
	if end > n {
		end = n
	}
	return start, end
}

// viewport is everything the renderers need about the slice of history
// currently on screen: the candles, their OHLCV columns, and the
// indicator series already narrowed to the same range.
//
// Exactly one viewport is built per frame and every renderer derives its
// scale, its axis, its sub-panels and its hover readout from it, so the
// price axis, the readout and the sub-panels cannot end up describing
// different slices of the data — which is what happened when the main
// chart re-truncated the candles after the price range had already been
// measured.
type viewport struct {
	candles []provider.OHLCV
	highs   []float64
	lows    []float64
	closes  []float64
	ind     indicatorSet

	// step is the number of grid columns each candle occupies. The
	// sub-panels use it too so their x-axis lines up with the candles.
	step int
}

// newViewport slices the last count candles out of full and narrows the
// precomputed indicator set to match.
func newViewport(full []provider.OHLCV, set indicatorSet, count, step int) viewport {
	if count < 0 {
		count = 0
	}
	if count > len(full) {
		count = len(full)
	}
	start := len(full) - count
	candles := full[start:]
	_, highs, lows, closes, _ := columns(candles)
	if step < 1 {
		step = 1
	}
	return viewport{
		candles: candles,
		highs:   highs,
		lows:    lows,
		closes:  closes,
		ind:     set.window(start, len(full)),
		step:    step,
	}
}

// len is the number of visible candles.
func (v viewport) len() int { return len(v.candles) }

// valueAt returns the series value at index i, or NaN when the index is
// outside the series. Indicator series are as long as the window, but a
// degenerate period can shorten them, and the readout indexes them with
// a hover position.
func valueAt(series []float64, i int) (float64, bool) {
	if i < 0 || i >= len(series) {
		return 0, false
	}
	v := series[i]
	if v != v { // NaN
		return 0, false
	}
	return v, true
}
