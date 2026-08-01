package chart

import (
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/tui/format"
)

// gridLines returns the rendered grid rows, i.e. the view with its
// header lines stripped.
func gridLines(m Model) []string {
	lines := strings.Split(plain(m.View()), "\n")
	skip := m.hoverHeaderRows()
	if skip > len(lines) {
		return nil
	}
	return lines[skip:]
}

// axisLabel is the price-axis text on a rendered grid row.
func axisLabel(line string) string {
	r := []rune(line)
	if len(r) < gridLabelWidth {
		return ""
	}
	return strings.TrimSpace(string(r[:gridLabelWidth]))
}

func TestPriceAxisDescribesTheCandlesActuallyDrawn(t *testing.T) {
	// The scale used to be measured before the candle slice was
	// truncated to fit, so the axis described bars that were never
	// drawn. Ask for more candles than fit and check the top label.
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(60, 30) // capacity (60-12)/2 = 24
	m.autoZoom = false
	m.zoom = 200

	visible := m.visibleCount()
	if visible != m.capacity() {
		t.Fatalf("visible = %d, capacity = %d", visible, m.capacity())
	}

	maxHigh := m.data[len(m.data)-visible].High
	for _, c := range m.data[len(m.data)-visible:] {
		if c.High > maxHigh {
			maxHigh = c.High
		}
	}
	// The full 200-bar window has a different maximum, or the test
	// cannot tell the two slices apart.
	fullMax := m.data[0].High
	for _, c := range m.data {
		if c.High > fullMax {
			fullMax = c.High
		}
	}
	if format.FormatAxisPrice(fullMax) == format.FormatAxisPrice(maxHigh) {
		t.Skip("fixture's visible and full maxima coincide")
	}

	lines := gridLines(m)
	if len(lines) == 0 {
		t.Fatal("no grid rendered")
	}
	if got, want := axisLabel(lines[0]), format.FormatAxisPrice(maxHigh); got != want {
		t.Fatalf("top axis label = %q, want %q (the visible maximum, not %q)",
			got, want, format.FormatAxisPrice(fullMax))
	}
}

func TestReadoutAgreesWithTheAxisSlice(t *testing.T) {
	// The newest candle is the rightmost drawn one, and the readout
	// describes it. Both must come from the same slice.
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(60, 30)
	m.autoZoom = false
	m.zoom = 200

	visible := m.visibleCount()
	newest := m.data[len(m.data)-1]
	if !strings.Contains(plain(m.View()), newest.Time.Format("2006-01-02 15:04")) {
		t.Fatal("readout does not describe the newest candle")
	}

	// And hovering the leftmost column selects the oldest visible bar,
	// not the oldest fetched one.
	m.hoverCol = gridLabelWidth + 1
	m.hoverRow = m.hoverHeaderRows()
	oldestVisible := m.data[len(m.data)-visible]
	view := plain(m.View())
	if !strings.Contains(view, oldestVisible.Time.Format("2006-01-02 15:04")) {
		t.Fatalf("hover readout does not describe the oldest visible bar:\n%s", view)
	}
	if strings.Contains(view, m.data[0].Time.Format("2006-01-02 15:04")) {
		t.Fatal("hover readout reached outside the visible window")
	}
}

func TestSubPanelAlignsWithTheCandles(t *testing.T) {
	// The sub-panel used to plot one column per bar while the candles
	// used two, so the RSI under a candle belonged to a different bar.
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.indicators[IndRSI] = true
	m.SetSize(120, 40)

	view := plain(m.View())
	idx := strings.Index(view, "RSI(14)")
	if idx < 0 {
		t.Fatal("RSI sub-panel not rendered")
	}
	panelLines := strings.Split(view[idx:], "\n")[1:]

	found := false
	for _, line := range panelLines {
		r := []rune(line)
		for c := gridLabelWidth + 1; c < len(r); c++ {
			if r[c] != '●' {
				continue
			}
			found = true
			if (c-(gridLabelWidth+1))%candleWidth != 0 {
				t.Fatalf("RSI sample at column %d does not sit under a candle column", c)
			}
		}
	}
	if !found {
		t.Fatal("no RSI samples plotted")
	}
}

func TestHoverCandleIdxTracksTheCandleStep(t *testing.T) {
	m := New(nil)
	cases := []struct {
		col, step, visible, want int
	}{
		{gridLabelWidth + 1, 2, 50, 0},
		{gridLabelWidth + 2, 2, 50, 0},
		{gridLabelWidth + 3, 2, 50, 1},
		{gridLabelWidth + 1, 1, 50, 0},
		{gridLabelWidth + 2, 1, 50, 1},
		{gridLabelWidth, 2, 50, -1},
		{0, 2, 50, -1},
		{-1, 2, 50, -1},
		{gridLabelWidth + 1 + 200, 2, 50, -1},
		{gridLabelWidth + 1, 2, 0, -1},
	}
	for _, tc := range cases {
		m.hoverCol = tc.col
		if got := m.hoverCandleIdx(tc.visible, tc.step); got != tc.want {
			t.Errorf("hoverCandleIdx(col=%d step=%d visible=%d) = %d, want %d",
				tc.col, tc.step, tc.visible, got, tc.want)
		}
	}
}

func TestCrosshairSitsOnTheHoveredRow(t *testing.T) {
	// The crosshair's row offset used to be calibrated to four header
	// lines while View prints two, so the horizontal line was drawn two
	// rows away from the cursor.
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(120, 40)
	m.hoverRow = m.hoverHeaderRows() + 7
	m.hoverCol = gridLabelWidth + 1 + 40

	lines := gridLines(m)
	if len(lines) <= 7 {
		t.Fatalf("grid has %d rows", len(lines))
	}
	if !strings.Contains(lines[7], "───") {
		t.Fatalf("crosshair not on the hovered row:\n%q", lines[7])
	}
	for i, line := range lines {
		if i != 7 && strings.Contains(line, "───") {
			t.Fatalf("crosshair also drawn on row %d", i)
		}
	}
}
