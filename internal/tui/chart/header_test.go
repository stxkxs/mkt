package chart

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Every row the chart prints fits the width it is given, with the
// indicator menu open and closed: the title's key hints and the menu used
// to run past an 80-column terminal and be cut off or wrapped.
func TestHeaderRowsFitEveryWidth(t *testing.T) {
	for _, menu := range []bool{false, true} {
		for w := 1; w <= 200; w++ {
			m := New(nil)
			m.symbol = "BTC-USD"
			m.data = genCandles(200)
			m.indicatorMenu = menu
			m.SetSize(w, 40)
			for i, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("menu=%v width %d: row %d is %d cells: %q", menu, w, i, got, line)
				}
			}
		}
	}
}

// However many rows the menu wraps onto, the frame stays the height it was
// given: the rows are budgeted, not added on top.
func TestWrappedMenuKeepsTheFrameHeight(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120, 200} {
		closed := New(nil)
		closed.symbol = "BTC-USD"
		closed.data = genCandles(200)
		closed.SetSize(w, 40)
		open := closed
		open.indicatorMenu = true

		hClosed := len(strings.Split(closed.View(), "\n"))
		hOpen := len(strings.Split(open.View(), "\n"))
		if hOpen != hClosed {
			t.Errorf("width %d: frame is %d rows with the menu open, %d closed", w, hOpen, hClosed)
		}
		if w == 40 && len(open.menuLines()) < 2 {
			t.Errorf("width 40: menu did not wrap (%d rows)", len(open.menuLines()))
		}
	}
}

func TestTitleKeepsBackHintFirst(t *testing.T) {
	got := fitHints(chartHints, len("esc: back")+2)
	if got != "esc: back" {
		t.Errorf("tightest budget kept %q, want the back hint", got)
	}
	all := fitHints(chartHints, 500)
	if all != "[/]: interval  +/-: zoom  f: fit  m: mode  i: indicators  esc: back" {
		t.Errorf("full budget = %q", all)
	}
}

func TestErrorLineFitsTheWidth(t *testing.T) {
	m := New(nil)
	m.symbol = "BTC-USD"
	m.errMsg = strings.Repeat("upstream said no ", 20)
	m.SetSize(80, 30)
	for _, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Fatalf("row is %d cells: %q", got, line)
		}
	}
}
