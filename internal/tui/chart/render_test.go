package chart

import (
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stxkxs/mkt/internal/provider"
)

// allIndicators is every indicator switched on at once — the worst case
// for the layout arithmetic, since it enables the volume-profile gutter,
// a sub-panel and the separator line all at the same time.
func allIndicators() [indCount]bool {
	var on [indCount]bool
	for i := range on {
		on[i] = true
	}
	return on
}

// sweepData memoizes the fixtures the sweeps reuse, so a sweep measures
// rendering rather than candle generation.
var sweepData = func() func(int) []provider.OHLCV {
	var mu sync.Mutex
	cache := map[int][]provider.OHLCV{}
	return func(n int) []provider.OHLCV {
		mu.Lock()
		defer mu.Unlock()
		if d, ok := cache[n]; ok {
			return d
		}
		d := genCandles(n)
		cache[n] = d
		return d
	}
}()

// sweepModel builds a model at the given size with the given indicator
// set and a hover position that is deliberately out of range as often as
// it is in range.
func sweepModel(bars, w, h int, on [indCount]bool, mode ChartMode, menu bool) Model {
	m := New(nil)
	m.symbol = "TEST"
	m.data = sweepData(bars)
	m.indicators = on
	m.mode = mode
	m.indicatorMenu = menu
	m.SetSize(w, h)
	return m
}

func TestViewNeverPanicsAcrossTerminalSizes(t *testing.T) {
	// A width of 1 with a sub-panel enabled used to panic in
	// strings.Repeat("─", m.width-2). Sweep the full width 0..200 by
	// height 0..60 matrix with every indicator on — the worst case for
	// the layout arithmetic, since it enables the volume-profile gutter,
	// a sub-panel and the separator all at once.
	on := allIndicators()
	for w := range 202 {
		for h := range 62 {
			m := sweepModel(60, w, h, on, ModeCandlestick, false)
			m.hoverCol = w / 2
			m.hoverRow = h / 2
			mustRender(t, m, 60, w, h)
		}
	}
}

func TestViewNeverPanicsAtLayoutBoundaries(t *testing.T) {
	// The same sweep across both chart modes, the indicator menu and
	// the data lengths that shorten the indicator series, restricted to
	// the widths and heights where the layout arithmetic changes sign.
	on := allIndicators()
	widths := []int{0, 1, 2, 10, 11, 12, 13, 14, 25, 26, 27, 28, 40, 80, 215, 400}
	heights := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 24, 60}
	for _, bars := range []int{0, 1, 2, 19, 200} {
		for _, mode := range []ChartMode{ModeCandlestick, ModeLine} {
			for _, menu := range []bool{false, true} {
				for _, w := range widths {
					for _, h := range heights {
						m := sweepModel(bars, w, h, on, mode, menu)
						m.hoverCol = w - 1
						m.hoverRow = h - 1
						mustRender(t, m, bars, w, h)
					}
				}
			}
		}
	}
}

func TestViewNeverPanicsForAnyIndicatorCombination(t *testing.T) {
	// All 2^13 combinations at a size where the layout is tight, plus a
	// sampled pass over the other mode and a roomier terminal.
	for mask := range 1 << indCount {
		var on [indCount]bool
		for i := range on {
			on[i] = mask&(1<<i) != 0
		}
		m := sweepModel(60, 26, 9, on, ModeCandlestick, false)
		m.hoverCol = 13
		m.hoverRow = 5
		mustRender(t, m, 60, 26, 9)

		if mask%31 != 0 {
			continue
		}
		for _, size := range [][2]int{{12, 6}, {60, 24}, {215, 50}} {
			for _, mode := range []ChartMode{ModeCandlestick, ModeLine} {
				m := sweepModel(60, size[0], size[1], on, mode, false)
				m.hoverCol = size[0] / 2
				m.hoverRow = size[1] / 2
				mustRender(t, m, 60, size[0], size[1])
			}
		}
	}
}

func TestViewNeverPanicsWithExtremeHover(t *testing.T) {
	on := allIndicators()
	for _, col := range []int{-100, -1, 0, 1, 10, 11, 12, 200, 1 << 20} {
		for _, row := range []int{-100, -1, 0, 1, 2, 3, 40, 1 << 20} {
			m := sweepModel(200, 120, 40, on, ModeCandlestick, false)
			m.hoverCol = col
			m.hoverRow = row
			mustRender(t, m, 200, 120, 40)
		}
	}
}

func TestSubPanelSeparatorSurvivesWidthOne(t *testing.T) {
	// The exact regression: width 1 with a sub-panel enabled.
	m := sweepModel(200, 1, 20, [indCount]bool{IndRSI: true}, ModeCandlestick, false)
	if got := m.View(); strings.Contains(got, "─") {
		t.Fatalf("separator drawn at width 1: %q", got)
	}
}

func TestZeroSizeRendersNothing(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {0, 40}, {120, 0}, {-5, -5}, {120, -1}} {
		m := sweepModel(200, size[0], size[1], allIndicators(), ModeCandlestick, false)
		if got := m.View(); got != "" {
			t.Errorf("View at %v = %q, want empty", size, got)
		}
	}
}

func mustRender(t *testing.T, m Model, bars, w, h int) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked at bars=%d width=%d height=%d: %v", bars, w, h, r)
		}
	}()
	_ = m.View()
}

func TestPanelIsBoundsChecked(t *testing.T) {
	p := newPanel(3, 2)
	// None of these may panic or alter the panel.
	p.paint(-1, 0, 'x', nil, paintOver)
	p.paint(0, -1, 'x', nil, paintOver)
	p.paint(99, 99, 'x', nil, paintOver)
	p.hline(-5, 'x', nil, paintOver)
	p.vline(42, 'x', nil, paintOver)
	if got := p.render(nil); got != strings.Repeat(strings.Repeat(" ", gridLabelWidth+1)+"   \n", 2) {
		t.Fatalf("panel mutated by out-of-bounds writes: %q", got)
	}

	// A degenerate panel renders as the empty string.
	for _, size := range [][2]int{{0, 0}, {-1, 5}, {5, -1}} {
		if got := newPanel(size[0], size[1]).render(nil); got != "" {
			t.Errorf("newPanel%v render = %q, want empty", size, got)
		}
	}
}

func TestRepeatGuardsNegativeCounts(t *testing.T) {
	for _, n := range []int{-100, -1, 0} {
		if got := repeat("─", n); got != "" {
			t.Errorf("repeat(%d) = %q, want empty", n, got)
		}
	}
	if got := repeat("ab", 3); got != "ababab" {
		t.Fatalf("repeat = %q", got)
	}
}

func TestVScaleHandlesDegenerateInput(t *testing.T) {
	cases := []struct {
		name  string
		scale vscale
		v     float64
		ok    bool
	}{
		{"flat range", newPriceScale(5, 5, 10), 5, true},
		{"nan value", newPriceScale(0, 10, 10), math.NaN(), false},
		{"inf value", newPriceScale(0, 10, 10), math.Inf(1), false},
		{"zero height", newPriceScale(0, 10, 0), 5, false},
		{"negative height", newPriceScale(0, 10, -3), 5, false},
		{"huge value", newPriceScale(0, 10, 10), 1e308, true},
		{"huge negative", newPriceScale(0, 10, 10), -1e308, true},
		{"panel height one", newPanelScale(0, 100, 1), 100, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, ok := tc.scale.row(tc.v)
			if ok != tc.ok {
				t.Fatalf("row(%v) ok = %v, want %v", tc.v, ok, tc.ok)
			}
			if ok && (row < 0 || row >= tc.scale.height) {
				t.Fatalf("row(%v) = %d out of [0,%d)", tc.v, row, tc.scale.height)
			}
		})
	}
}

func TestVScaleRowValueRoundTrip(t *testing.T) {
	// value(r) is the top *boundary* of row r, so the value that reads
	// back as row r is the middle of the band it labels.
	s := newPriceScale(100, 200, 20)
	for r := range 20 {
		mid := s.value(r) - 0.5/s.span
		got, ok := s.row(mid)
		if !ok {
			t.Fatalf("row(mid of %d) not placeable", r)
		}
		if got != r {
			t.Errorf("row(mid of row %d) = %d", r, got)
		}
	}
	if got := s.value(0); got != 200 {
		t.Errorf("value(0) = %v, want the top of the range", got)
	}
}

func TestPlotSeriesStaysInsideThePanel(t *testing.T) {
	p := newPanel(4, 3)
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i)
	}
	plotSeries(p, values, 2, newPanelScale(0, 99, 3), '●', nil, paintOver)
	out := p.render(nil)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := len([]rune(line)); got != gridLabelWidth+1+4 {
			t.Fatalf("row width = %d, want %d: %q", got, gridLabelWidth+1+4, line)
		}
	}
	if !strings.Contains(out, "●") {
		t.Fatal("nothing plotted")
	}
}
