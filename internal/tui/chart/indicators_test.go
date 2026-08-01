package chart

import (
	"math"
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/indicator"
)

// newLoadedModel builds a chart already holding data, sized for a wide
// terminal, with the named indicators switched on.
func newLoadedModel(t *testing.T, bars int, on ...IndicatorType) Model {
	t.Helper()
	m := New(nil)
	m.SetSize(215, 50)
	m.symbol = "TEST"
	m.data = genCandles(bars)
	for _, ind := range on {
		m.indicators[ind] = true
	}
	return m
}

// setZoom pins a manual zoom level.
func setZoom(m Model, n int) Model {
	m.autoZoom = false
	m.zoom = n
	return m
}

func TestIndicatorsDoNotMoveWhenZooming(t *testing.T) {
	// EMA, RSI, MACD and VWAP all carry state from before the window.
	// Computing them over the visible slice reseeds them from whatever
	// is on screen, so the same bar reads a different value at every
	// zoom level. They must be computed over the full fetched history.
	labels := []struct {
		name string
		ind  IndicatorType
		key  string
	}{
		{"EMA", IndEMA, "EMA"},
		{"RSI", IndRSI, "RSI"},
		{"VWAP", IndVWAP, "VWAP"},
		{"ATR", IndATR, "ATR"},
		{"ADX", IndADX, "ADX"},
	}
	for _, tc := range labels {
		t.Run(tc.name, func(t *testing.T) {
			base := newLoadedModel(t, 200, tc.ind)

			narrow := setZoom(base, 30).View()
			wide := setZoom(base, 150).View()
			fitted := base.View() // auto-fit

			want, ok := readoutValue(t, narrow, tc.key)
			if !ok {
				t.Fatalf("no %s value in readout:\n%s", tc.key, plain(narrow))
			}
			for name, view := range map[string]string{"wide": wide, "fitted": fitted} {
				got, ok := readoutValue(t, view, tc.key)
				if !ok {
					t.Fatalf("no %s value in %s readout", tc.key, name)
				}
				if math.Abs(got-want) > 1e-9 {
					t.Errorf("%s at %s zoom = %v, at narrow zoom = %v; indicator must not depend on zoom", tc.key, name, got, want)
				}
			}
		})
	}
}

func TestIndicatorIsWarmAtTheLeftEdgeOfTheWindow(t *testing.T) {
	// The oldest visible bar has 170 bars of history behind it, so an
	// SMA(20) is defined there. Computing over the visible slice leaves
	// the first 19 visible bars blank instead.
	m := newLoadedModel(t, 200, IndSMA)
	m = setZoom(m, 30)
	m.hoverCol = gridLabelWidth + 1 // first candle column
	m.hoverRow = m.hoverHeaderRows()

	view := m.View()
	if _, ok := readoutValue(t, view, "SMA"); !ok {
		t.Fatalf("SMA missing at the oldest visible bar; it was seeded from the window, not the history:\n%s", plain(view))
	}
}

func TestComputeIndicatorsWindowMatchesFullSeries(t *testing.T) {
	full := genCandles(200)
	var active [indCount]bool
	for i := range active {
		active[i] = true
	}
	set := computeIndicators(full, active)

	for _, count := range []int{1, 20, 137, 200} {
		start := len(full) - count
		w := set.window(start, len(full))

		check := func(name string, got, want []float64) {
			t.Helper()
			if len(got) != count {
				t.Fatalf("%s window len = %d, want %d", name, len(got), count)
			}
			for i := range got {
				a, b := got[i], want[start+i]
				if math.IsNaN(a) && math.IsNaN(b) {
					continue
				}
				if a != b {
					t.Fatalf("%s[%d] = %v, full[%d] = %v", name, i, a, start+i, b)
				}
			}
		}
		check("sma", w.sma, set.sma)
		check("ema", w.ema, set.ema)
		check("rsi", w.rsi, set.rsi)
		check("vwap", w.vwap, set.vwap)
		check("obv", w.obv, set.obv)
		check("atr", w.atr, set.atr)
		check("stochK", w.stochK, set.stochK)
		check("stochD", w.stochD, set.stochD)
		check("adx", w.adx, set.adx)
		check("bbUpper", w.bb.Upper, set.bb.Upper)
		check("macd", w.macd.MACD, set.macd.MACD)
		check("signal", w.macd.Signal, set.macd.Signal)
		check("hist", w.macd.Histogram, set.macd.Histogram)
	}
}

func TestComputeIndicatorsOnlyRunsActiveOnes(t *testing.T) {
	full := genCandles(50)
	var active [indCount]bool
	active[IndSMA] = true
	set := computeIndicators(full, active)

	if len(set.sma) != len(full) {
		t.Fatalf("sma len = %d, want %d", len(set.sma), len(full))
	}
	if set.ema != nil || set.rsi != nil || set.obv != nil || set.patterns != nil {
		t.Fatal("inactive indicators were computed")
	}
	if set.hasPivots {
		t.Fatal("pivots computed while inactive")
	}
}

func TestPivotsUseTheLastCompletedBarOfHistory(t *testing.T) {
	full := genCandles(200)
	var active [indCount]bool
	active[IndPivots] = true
	set := computeIndicators(full, active)

	prev := full[len(full)-2]
	want := indicator.PivotsClassic(prev.High, prev.Low, prev.Close)
	if set.pivots != want {
		t.Fatalf("pivots = %+v, want %+v", set.pivots, want)
	}

	// And the window keeps them, so zooming cannot move the levels.
	if got := set.window(150, 200).pivots; got != want {
		t.Fatalf("windowed pivots = %+v, want %+v", got, want)
	}
}

func TestSliceFloatsClampsOutOfRange(t *testing.T) {
	src := []float64{1, 2, 3}
	cases := []struct {
		start, end int
		want       int
	}{
		{0, 3, 3},
		{-5, 99, 3},
		{2, 1, 0},
		{5, 9, 0},
	}
	for _, tc := range cases {
		if got := len(sliceFloats(src, tc.start, tc.end)); got != tc.want {
			t.Errorf("sliceFloats(%d,%d) len = %d, want %d", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestHeaderPOCUsesTheSameBinsAsTheGutter(t *testing.T) {
	// The header used one bin per candle, which collapses the profile
	// into "the typical price of the biggest single candle" and points
	// somewhere the gutter's POC line does not.
	m := newLoadedModel(t, 200, IndVolProfile)
	view := m.View()

	got, ok := readoutValue(t, view, "POC")
	if !ok {
		t.Fatalf("no POC in readout:\n%s", plain(view))
	}

	mainH := m.height - m.headerBudget()
	if mainH < 5 {
		mainH = 5
	}
	v := newViewport(m.data, indicatorSet{}, m.visibleCount(), m.candleStep())
	bins := volumeBins(v.candles, mainH)
	idx, _ := indicator.POC(bins)
	if idx < 0 {
		t.Fatal("gutter profile has no point of control")
	}
	want := (bins[idx].PriceMin + bins[idx].PriceMax) / 2

	if math.Abs(got-want) > 0.005 {
		t.Errorf("header POC = %.2f, gutter POC = %.2f", got, want)
	}

	// Guard the regression: the old one-bin-per-candle binning gives a
	// different answer for this series.
	oldBins := volumeBins(v.candles, len(v.candles))
	oldIdx, _ := indicator.POC(oldBins)
	old := (oldBins[oldIdx].PriceMin + oldBins[oldIdx].PriceMax) / 2
	if math.Abs(old-want) < 0.005 {
		t.Skip("fixture happens to agree under both binnings; test cannot bite")
	}
}

func TestReadoutDescribesTheHoveredCandle(t *testing.T) {
	m := newLoadedModel(t, 200, IndSMA)
	m = setZoom(m, 40)

	last := m.View()
	visible := m.visibleCount()
	lastCandle := m.data[len(m.data)-1]
	if !strings.Contains(plain(last), lastCandle.Time.Format("2006-01-02 15:04")) {
		t.Fatalf("readout does not describe the newest candle:\n%s", plain(last))
	}

	// Hover the oldest visible candle.
	m.hoverCol = gridLabelWidth + 1
	m.hoverRow = m.hoverHeaderRows()
	hovered := m.data[len(m.data)-visible]
	if !strings.Contains(plain(m.View()), hovered.Time.Format("2006-01-02 15:04")) {
		t.Fatalf("readout does not follow the hover to candle %d", len(m.data)-visible)
	}
}
