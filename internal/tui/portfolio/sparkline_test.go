package portfolio

import (
	"strings"
	"testing"
)

func TestSparklineEmptyForShortInput(t *testing.T) {
	if sparkline(nil, 20) != "" {
		t.Error("nil should be empty")
	}
	if sparkline([]float64{1}, 20) != "" {
		t.Error("single point should be empty")
	}
	if sparkline([]float64{1, 2}, 0) != "" {
		t.Error("zero width should be empty")
	}
}

func TestSparklineLengthBounded(t *testing.T) {
	// 100 values into a 10-wide sparkline should produce exactly 10 cells.
	vals := make([]float64, 100)
	for i := range vals {
		vals[i] = float64(i)
	}
	got := sparkline(vals, 10)
	if n := len([]rune(got)); n != 10 {
		t.Errorf("expected 10 cells, got %d", n)
	}
}

func TestSparklineFlatUsesLowestBlock(t *testing.T) {
	got := sparkline([]float64{5, 5, 5, 5, 5}, 5)
	// All cells should be the lowest block (no range → idx=0).
	for _, r := range got {
		if r != '▁' {
			t.Errorf("flat input should use ▁, got %q", string(r))
			break
		}
	}
}

func TestSparklineMonotonicEndsHigh(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	got := sparkline(vals, len(vals))
	runes := []rune(got)
	last := runes[len(runes)-1]
	if !strings.ContainsRune("▇█", last) {
		t.Errorf("monotonic-up should end at top block, got %q", string(last))
	}
}
