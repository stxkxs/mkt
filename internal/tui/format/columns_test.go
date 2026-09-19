package format

import (
	"strings"
	"testing"
)

// watchCols mirrors the Watch tab's real plan closely enough to exercise
// shedding, shrinking and flex together.
func watchCols() []Col {
	return []Col{
		{Key: "symbol", Width: 12, Min: 6, Prio: 6},
		{Key: "price", Width: 12, Min: 8, Prio: 5},
		{Key: "change", Width: 10, Min: 8, Prio: 4},
		{Key: "vol", Width: 8, Prio: 3},
		{Key: "range", Width: 8, Prio: 2},
		{Key: "trend", Width: 20, Min: 8, Prio: 1, Flex: true},
	}
}

// The guarantee Fit exists for: the panel word-wraps what it cannot fit, so
// a layout that overruns its width silently costs a screen line and
// desynchronizes the click hit-test.
func TestFitNeverExceedsWidth(t *testing.T) {
	for width := 0; width <= 200; width++ {
		for gutter := 0; gutter <= 3; gutter++ {
			got := Fit(watchCols(), width, gutter)
			if total := got.Total(gutter); total > width {
				t.Fatalf("width=%d gutter=%d: layout occupies %d cells (%d columns)", width, gutter, total, len(got))
			}
		}
	}
}

func TestFitKeepsEveryColumnWhenWideEnough(t *testing.T) {
	got := Fit(watchCols(), 200, 2)
	if len(got) != 6 {
		t.Fatalf("got %d columns at width 200, want 6", len(got))
	}
	for _, c := range watchCols() {
		if !got.Has(c.Key) {
			t.Errorf("column %q dropped at width 200", c.Key)
		}
	}
}

// Columns shed lowest Prio first, so the reader loses the least valuable
// cell before the one that identifies the row.
func TestFitShedsLowestPriorityFirst(t *testing.T) {
	var dropped []string
	prev := Fit(watchCols(), 200, 2)
	for width := 199; width >= 1; width-- {
		got := Fit(watchCols(), width, 2)
		for _, c := range prev {
			if !got.Has(c.Key) {
				dropped = append(dropped, c.Key)
			}
		}
		prev = got
	}
	want := []string{"range", "vol", "change", "price", "symbol"}
	// trend flexes and shrinks before it is shed, so it may leave at any
	// point before range; assert the order of the rest.
	var seen []string
	for _, d := range dropped {
		if d != "trend" {
			seen = append(seen, d)
		}
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("drop order = %v, want %v (trend excluded)", seen, want)
	}
}

// A column with slack is squeezed before any column is dropped, so a frame
// loses detail gradually rather than losing a whole cell while another is
// still padded.
func TestFitShrinksBeforeDropping(t *testing.T) {
	cols := []Col{
		{Key: "a", Width: 10, Min: 4, Prio: 2},
		{Key: "b", Width: 10, Min: 4, Prio: 1},
	}
	// 2 gutter + 4 + 1 + 4 = 11 is the floor for keeping both.
	got := Fit(cols, 11, 2)
	if len(got) != 2 {
		t.Fatalf("got %d columns at the two-column floor, want 2: %+v", len(got), got)
	}
	if got.Cells("a") != 4 || got.Cells("b") != 4 {
		t.Errorf("columns not squeezed to their Min: %+v", got)
	}
	// One cell tighter and the lower-Prio column must go.
	got = Fit(cols, 10, 2)
	if got.Has("b") {
		t.Errorf("lower-Prio column survived below the two-column floor: %+v", got)
	}
}

// A fixed column (Min 0) is dropped rather than squeezed: a glyph bar or a
// sparkline at half width is not a smaller version of itself.
func TestFitDoesNotShrinkFixedColumns(t *testing.T) {
	cols := []Col{
		{Key: "id", Width: 10, Min: 4, Prio: 2},
		{Key: "bar", Width: 8, Prio: 1},
	}
	for width := 1; width <= 60; width++ {
		got := Fit(cols, width, 0)
		if got.Has("bar") && got.Cells("bar") != 8 {
			t.Fatalf("width=%d: fixed column squeezed to %d", width, got.Cells("bar"))
		}
	}
}

func TestFitGivesLeftoverToFlex(t *testing.T) {
	got := Fit(watchCols(), 120, 2)
	if got.Cells("trend") <= 20 {
		t.Errorf("flex column did not absorb leftover: %d cells at width 120", got.Cells("trend"))
	}
	if total := got.Total(2); total != 120 {
		t.Errorf("flex left %d cells unused", 120-total)
	}
}

// Two flex columns split the remainder deterministically, so a column does
// not change width between two frames of the same size.
func TestFitFlexSplitIsStable(t *testing.T) {
	cols := []Col{
		{Key: "a", Width: 4, Prio: 3, Flex: true},
		{Key: "b", Width: 4, Prio: 2, Flex: true},
		{Key: "c", Width: 4, Prio: 1},
	}
	first := Fit(cols, 40, 2)
	for range 5 {
		if got := Fit(cols, 40, 2); got.Cells("a") != first.Cells("a") || got.Cells("b") != first.Cells("b") {
			t.Fatalf("flex split is not stable: %+v then %+v", first, got)
		}
	}
	if first.Total(2) != 40 {
		t.Errorf("flex split left cells unused: %d of 40", first.Total(2))
	}
}

// Below the floor the caller gets nil and prints its own message, rather
// than a layout that overflows.
func TestFitRefusesWhenNothingFits(t *testing.T) {
	cols := []Col{{Key: "only", Width: 12, Min: 10, Prio: 1}}
	if got := Fit(cols, 8, 2); got != nil {
		t.Errorf("want nil below the floor, got %+v", got)
	}
	if got := Fit(cols, 12, 2); len(got) != 1 {
		t.Errorf("want the single column at its floor, got %+v", got)
	}
}

func TestFitDegenerateInputs(t *testing.T) {
	if got := Fit(nil, 100, 2); got != nil {
		t.Error("nil columns should yield nil")
	}
	if got := Fit(watchCols(), 0, 0); got != nil {
		t.Error("zero width should yield nil")
	}
	if got := Fit(watchCols(), -5, 2); got != nil {
		t.Error("negative width should yield nil")
	}
	if got := Fit(watchCols(), 100, -1); got != nil {
		t.Error("negative gutter should yield nil")
	}
	// A Min above Width is a caller error, not a reason to overflow.
	odd := []Col{{Key: "a", Width: 4, Min: 99, Prio: 1}}
	if got := Fit(odd, 20, 0); got.Total(0) > 20 {
		t.Errorf("Min > Width overflowed: %+v", got)
	}
}

// Fit is called every frame with a slice the tab owns; mutating it would
// shrink the plan a little more on each render.
func TestFitDoesNotMutateItsInput(t *testing.T) {
	cols := watchCols()
	before := make([]Col, len(cols))
	copy(before, cols)

	for width := 10; width < 90; width++ {
		Fit(cols, width, 2)
	}
	for i := range cols {
		if cols[i] != before[i] {
			t.Fatalf("input column %d mutated: %+v -> %+v", i, before[i], cols[i])
		}
	}
}

// Widths are monotonic in the frame: a wider terminal never shows less.
func TestFitIsMonotonicInWidth(t *testing.T) {
	prev := Fit(watchCols(), 1, 2)
	for width := 2; width <= 200; width++ {
		got := Fit(watchCols(), width, 2)
		if len(got) < len(prev) {
			t.Fatalf("width=%d shows %d columns, width=%d showed %d", width, len(got), width-1, len(prev))
		}
		prev = got
	}
}

func TestLayoutAccessors(t *testing.T) {
	l := Fit(watchCols(), 200, 2)
	if !l.Has("symbol") || l.Cells("symbol") != 12 {
		t.Errorf("Has/Cells wrong for a present column: %+v", l)
	}
	if l.Has("absent") || l.Cells("absent") != 0 {
		t.Error("Has/Cells wrong for an absent column")
	}
	if Layout(nil).Total(2) != 0 {
		t.Error("empty layout should occupy no cells")
	}
}
