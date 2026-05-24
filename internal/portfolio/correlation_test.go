package portfolio

import (
	"math"
	"testing"
)

func TestCorrelationPerfect(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{2, 4, 6, 8, 10}
	got := Correlation(a, b)
	if math.Abs(got-1) > 1e-9 {
		t.Errorf("perfectly correlated: got %v want 1", got)
	}
}

func TestCorrelationInverse(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{5, 4, 3, 2, 1}
	got := Correlation(a, b)
	if math.Abs(got+1) > 1e-9 {
		t.Errorf("perfectly inverse: got %v want -1", got)
	}
}

func TestCorrelationFlatYieldsNaN(t *testing.T) {
	got := Correlation([]float64{1, 2, 3}, []float64{5, 5, 5})
	if !math.IsNaN(got) {
		t.Errorf("flat series should yield NaN, got %v", got)
	}
}

func TestCorrelationLengthMismatch(t *testing.T) {
	if !math.IsNaN(Correlation([]float64{1, 2, 3}, []float64{1, 2})) {
		t.Error("length mismatch should yield NaN")
	}
}

func TestCorrelationMatrixDiagonalIsOne(t *testing.T) {
	syms := []string{"A", "B", "C"}
	prices := [][]float64{
		{1, 2, 3, 4, 5},
		{5, 4, 3, 2, 1},
		{2, 4, 6, 8, 10},
	}
	m := CorrelationMatrix(syms, prices)
	for i := range syms {
		if math.Abs(m[i][i]-1) > 1e-9 {
			t.Errorf("diagonal [%d][%d] = %v, want 1", i, i, m[i][i])
		}
	}
	// A vs C is perfectly correlated (both linear); A vs B inverse
	if math.Abs(m[0][2]-1) > 1e-9 {
		t.Errorf("A vs C: got %v want 1", m[0][2])
	}
	if math.Abs(m[0][1]+1) > 1e-9 {
		t.Errorf("A vs B: got %v want -1", m[0][1])
	}
	// Matrix is symmetric
	if m[1][0] != m[0][1] {
		t.Errorf("not symmetric")
	}
}

func TestCorrelationMatrixTrimsToShortest(t *testing.T) {
	syms := []string{"A", "B"}
	prices := [][]float64{
		{1, 2, 3, 4, 5},
		{2, 4}, // only 2 long → truncated window
	}
	m := CorrelationMatrix(syms, prices)
	// minLen=2 → still valid; A's last 2 are [4,5], B is [2,4]; perfectly correlated
	if math.Abs(m[0][1]-1) > 1e-9 {
		t.Errorf("expected 1 on trimmed window, got %v", m[0][1])
	}
}

func TestCorrelationMatrixInsufficientData(t *testing.T) {
	syms := []string{"A", "B"}
	prices := [][]float64{{1}, {2}}
	m := CorrelationMatrix(syms, prices)
	if m[0][0] != 1 || !math.IsNaN(m[0][1]) {
		t.Errorf("insufficient data: diagonal=1 off-diag=NaN, got [%v %v / %v %v]", m[0][0], m[0][1], m[1][0], m[1][1])
	}
}
