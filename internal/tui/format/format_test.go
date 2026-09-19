package format

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"fits", "AAPL", 10, "AAPL"},
		{"exact", "AAPL", 4, "AAPL"},
		{"cut", "Berkshire Hathaway", 9, "Berkshir…"},
		{"zero", "AAPL", 0, ""},
		{"negative", "AAPL", -1, ""},
		{"one", "AAPL", 1, "…"},
		{"multibyte", "Société Générale", 8, "Société…"},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.in, tt.max); got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestViewportStart(t *testing.T) {
	tests := []struct {
		name                   string
		cursor, total, visible int
		want                   int
	}{
		{"all fit", 3, 5, 10, 0},
		{"exactly fit", 9, 10, 10, 0},
		{"cursor at top", 0, 20, 5, 0},
		{"cursor inside first window", 4, 20, 5, 0},
		{"cursor past window", 7, 20, 5, 3},
		{"cursor at end", 19, 20, 5, 15},
		{"zero visible", 5, 20, 0, 0},
		{"empty list", 0, 0, 5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ViewportStart(tt.cursor, tt.total, tt.visible); got != tt.want {
				t.Errorf("ViewportStart(%d, %d, %d) = %d, want %d",
					tt.cursor, tt.total, tt.visible, got, tt.want)
			}
		})
	}
}

func TestRepeatClampsNegative(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{-5, ""},
		{-1, ""},
		{0, ""},
		{1, "─"},
		{3, "───"},
	}
	for _, tt := range tests {
		if got := Repeat("─", tt.n); got != tt.want {
			t.Errorf("Repeat(─, %d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	if got := Spaces(-9); got != "" {
		t.Errorf("Spaces(-9) = %q, want empty", got)
	}
	if got := Spaces(2); got != "  " {
		t.Errorf("Spaces(2) = %q, want two spaces", got)
	}
}

func TestVisibleRows(t *testing.T) {
	tests := []struct {
		name                 string
		height, fixed, total int
		want                 int
	}{
		{"fits exactly", 12, 2, 10, 10},
		{"room to spare", 40, 3, 5, 5},
		{"clipped", 12, 3, 40, 9},
		{"header taller than frame", 2, 3, 40, 1},
		{"zero height", 0, 3, 40, 1},
		{"negative height", -5, 3, 40, 1},
		{"nothing to list", 40, 3, 0, 0},
		{"negative total", 40, 3, -1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisibleRows(tt.height, tt.fixed, tt.total); got != tt.want {
				t.Errorf("VisibleRows(%d, %d, %d) = %d, want %d",
					tt.height, tt.fixed, tt.total, got, tt.want)
			}
		})
	}
}

// A short window used to slice the tail off the price slice, leaving it
// empty, and then index element zero of it.
func TestSparklineNonPositiveWidth(t *testing.T) {
	for _, w := range []int{-3, 0} {
		if got := Sparkline([]float64{1, 2, 3}, w); got != "" {
			t.Errorf("Sparkline(width=%d) = %q, want empty", w, got)
		}
	}
}

func TestSpinnerFrameNegativeTick(t *testing.T) {
	for _, tick := range []int{-1, -7, -10, -11} {
		if got := SpinnerFrame(tick); got == "" {
			t.Errorf("SpinnerFrame(%d) returned empty", tick)
		}
	}
	if SpinnerFrame(-10) != SpinnerFrame(0) {
		t.Error("SpinnerFrame does not wrap negative ticks onto the same cycle")
	}
}

func TestBrailleSparklineDegenerate(t *testing.T) {
	cases := []struct {
		prices []float64
		width  int
	}{
		{nil, 10},
		{[]float64{}, 10},
		{[]float64{1}, 0},
		{[]float64{1}, -4},
		{[]float64{1, 1, 1}, 1},
	}
	for _, c := range cases {
		_ = BrailleSparkline(c.prices, c.width)
	}
}

func TestDayRangeDegenerate(t *testing.T) {
	for _, w := range []int{-1, 0, 1, 8} {
		track, idx := DayRange(10, 5, 20, w)
		if w <= 0 && track != "" {
			t.Errorf("DayRange(width=%d) track = %q, want empty", w, track)
		}
		if idx >= len([]rune(track)) && idx != -1 {
			t.Errorf("DayRange(width=%d) marker %d out of track", w, idx)
		}
	}
}

// Truncate budgets terminal columns. A CJK glyph occupies two cells, so
// counting runes spends twice the width the caller reserved and the row
// overruns its frame.
func TestTruncateCountsDisplayCells(t *testing.T) {
	// Five double-width runes = 10 cells. A 6-cell budget fits two of them
	// plus the ellipsis.
	got := Truncate("日本語です", 6)
	if w := lipgloss.Width(got); w > 6 {
		t.Fatalf("Truncate(%q, 6) = %q, %d cells wide", "日本語です", got, w)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated value lost its ellipsis: %q", got)
	}

	// An ASCII string that already fits is returned unchanged.
	if got := Truncate("abc", 6); got != "abc" {
		t.Fatalf("fitting string was modified: %q", got)
	}

	// A string exactly at the budget is not truncated.
	if got := Truncate("日本語", 6); got != "日本語" {
		t.Fatalf("exact-fit string was truncated: %q", got)
	}
}

// The budget is a ceiling for every input, not only for the ones whose
// cells a rune tally happens to get right. A keycap sequence (digit,
// U+FE0F, U+20E3) and an emoji-presentation pair spend two cells across
// several runes, a ZWJ family spends two across seven, and a skin-tone
// modifier spends two across two — so the vectors that disagree with a
// per-rune count are the ones worth holding the ceiling against.
func TestTruncateNeverExceedsBudget(t *testing.T) {
	for _, s := range []string{
		"abcdef", "日本語です", "aあbいc", "🙂🙂🙂", "",
		"1️⃣2️⃣3️⃣4️⃣5️⃣", "#️⃣*️⃣#️⃣", "❤️✔️⚠️ℹ️", "👨‍👩‍👧‍👦", "🇺🇸🇯🇵", "👍🏽👍🏿", "e\u0301e\u0301e\u0301",
	} {
		for max := range 12 {
			got := Truncate(s, max)
			if w := lipgloss.Width(got); w > max {
				t.Errorf("Truncate(%q, %d) = %q is %d cells", s, max, got, w)
			}
		}
	}
}

// The three precision ladders are rendered into every row of every tab. A
// boundary that shifts by one magnitude is invisible in review and wrong in
// every cell, so both sides of each threshold are pinned.
func TestFormatPriceLadder(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"99.999", "99.9990"},
		{"100", "100.00"},
		{"0.999", "0.999000"},
		{"1", "1.0000"},
		{"0.00999", "0.00999000"},
		{"0.01", "0.010000"},
	} {
		var f float64
		fmt.Sscanf(tc.in, "%g", &f)
		if got := FormatPrice(f); got != tc.want {
			t.Errorf("FormatPrice(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatAxisPriceLadder(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{9999.9, "9999.9"},
		{10000, "10000"},
		{99.9, "99.90"},
		{100, "100.0"},
		{0.99, "0.9900"},
		{1, "1.00"},
	} {
		if got := FormatAxisPrice(tc.in); got != tc.want {
			t.Errorf("FormatAxisPrice(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatVolumeLadder(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{999, "999"},
		{1000, "1.0K"},
		{999999, "1000.0K"},
		{1e6, "1.0M"},
		{999999999, "1000.0M"},
		{1e9, "1.0B"},
		{0, "0"},
	} {
		if got := FormatVolume(tc.in); got != tc.want {
			t.Errorf("FormatVolume(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
