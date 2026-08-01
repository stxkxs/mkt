package chart

import (
	"strings"
	"testing"
)

func TestChartFillsTheTerminalWidthByDefault(t *testing.T) {
	// The zoom used to be pinned at 50 candles two columns wide, so the
	// plot was 100 columns no matter how wide the terminal was.
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(215, 50)

	want := m.capacity() // (215-12)/2
	if got := m.visibleCount(); got != want {
		t.Fatalf("visible candles = %d, want %d (the full plot width)", got, want)
	}
	if want <= 50 {
		t.Fatalf("fixture terminal is too narrow to show the bug: capacity %d", want)
	}

	// The rendered grid really is that wide: the last candle column is
	// occupied on at least one row.
	lastCol := gridLabelWidth + 1 + (want-1)*m.candleStep()
	if !anyRowOccupiesColumn(m.View(), lastCol) {
		t.Fatalf("column %d is blank; the plot does not reach the right edge", lastCol)
	}
}

func TestZoomRefitsOnResize(t *testing.T) {
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)

	m.SetSize(80, 30)
	narrow := m.visibleCount()
	m.SetSize(215, 50)
	wide := m.visibleCount()

	if wide <= narrow {
		t.Fatalf("widening the terminal did not show more candles: %d -> %d", narrow, wide)
	}
	if wide != m.capacity() {
		t.Fatalf("visible = %d, capacity = %d", wide, m.capacity())
	}
}

func TestManualZoomTakesOverAndClamps(t *testing.T) {
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(60)
	m.SetSize(215, 50)

	fitted := m.visibleCount()
	if fitted != 60 {
		t.Fatalf("expected all 60 fetched candles to fit, got %d", fitted)
	}

	// Zoom in a few times: the window narrows by zoomStep each press and
	// stops at minZoom.
	m, _ = m.Update(key("+"))
	if got := m.visibleCount(); got != fitted-zoomStep {
		t.Fatalf("after one zoom in: %d, want %d", got, fitted-zoomStep)
	}
	if m.autoZoom {
		t.Fatal("manual zoom did not take over from auto-fit")
	}
	for range 20 {
		m, _ = m.Update(key("+"))
	}
	if got := m.visibleCount(); got != minZoom {
		t.Fatalf("zoomed in past the floor: %d, want %d", got, minZoom)
	}

	// Zooming back out stops at the data actually fetched.
	for range 40 {
		m, _ = m.Update(key("-"))
	}
	if got := m.visibleCount(); got != 60 {
		t.Fatalf("zoomed out past the fetched history: %d, want 60", got)
	}
	if m.zoom > 60 {
		t.Fatalf("zoom state ran away to %d with only 60 candles", m.zoom)
	}
}

func TestZoomIsClampedToWhatFits(t *testing.T) {
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(60, 30) // capacity (60-12)/2 = 24

	for range 30 {
		m, _ = m.Update(key("-"))
	}
	if got, want := m.visibleCount(), m.capacity(); got != want {
		t.Fatalf("visible = %d, capacity = %d", got, want)
	}

	// Shrinking the terminal clamps a manual zoom that no longer fits.
	m.SetSize(30, 30) // capacity (30-12)/2 = 9 -> floor of minZoom
	if m.zoom > m.zoomLimit() {
		t.Fatalf("zoom %d exceeds limit %d after resize", m.zoom, m.zoomLimit())
	}
	if got := m.visibleCount(); got > m.capacity() {
		t.Fatalf("visible %d exceeds capacity %d", got, m.capacity())
	}
}

func TestFitKeyRestoresAutoZoom(t *testing.T) {
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(215, 50)

	m, _ = m.Update(key("+"))
	if m.autoZoom {
		t.Fatal("zoom did not go manual")
	}
	m, _ = m.Update(key("f"))
	if !m.autoZoom {
		t.Fatal("f did not restore auto-fit")
	}
	if got := m.visibleCount(); got != m.capacity() {
		t.Fatalf("after refit visible = %d, capacity = %d", got, m.capacity())
	}
}

func TestLineModeFitsOneCandlePerColumn(t *testing.T) {
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(400)
	m.SetSize(215, 50)
	m.mode = ModeLine

	if got, want := m.visibleCount(), m.plotWidth(); got != want {
		t.Fatalf("line mode shows %d points, want %d (one per column)", got, want)
	}
}

func TestVolumeProfileGutterReservesColumns(t *testing.T) {
	m := New(nil)
	m.symbol = "TEST"
	m.data = genCandles(200)
	m.SetSize(215, 50)

	without := m.capacity()
	m.indicators[IndVolProfile] = true
	with := m.capacity()
	if with != without-volumeProfileGutterW/candleWidth {
		t.Fatalf("gutter did not reserve its columns: %d -> %d", without, with)
	}
}

// anyRowOccupiesColumn reports whether any rendered line has a
// non-space rune at the given column.
func anyRowOccupiesColumn(view string, col int) bool {
	for _, line := range strings.Split(plain(view), "\n") {
		r := []rune(line)
		if col < len(r) && r[col] != ' ' {
			return true
		}
	}
	return false
}
