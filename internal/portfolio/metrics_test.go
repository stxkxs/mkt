package portfolio

import (
	"math"
	"testing"
)

func TestSharpe(t *testing.T) {
	t.Run("positive returns gives positive Sharpe", func(t *testing.T) {
		got := Sharpe([]float64{0.01, 0.02, 0.015, 0.018}, 0)
		if math.IsNaN(got) || got <= 0 {
			t.Errorf("expected positive Sharpe, got %v", got)
		}
	})

	t.Run("rf above mean returns negative", func(t *testing.T) {
		got := Sharpe([]float64{0.01, 0.02, 0.015}, 0.05)
		if math.IsNaN(got) || got >= 0 {
			t.Errorf("expected negative Sharpe, got %v", got)
		}
	})

	t.Run("fewer than two returns NaN", func(t *testing.T) {
		if !math.IsNaN(Sharpe([]float64{0.01}, 0)) {
			t.Error("expected NaN for single point")
		}
		if !math.IsNaN(Sharpe(nil, 0)) {
			t.Error("expected NaN for empty")
		}
	})

	t.Run("zero stddev returns NaN", func(t *testing.T) {
		got := Sharpe([]float64{0.01, 0.01, 0.01}, 0)
		if !math.IsNaN(got) {
			t.Errorf("flat returns should yield NaN, got %v", got)
		}
	})
}

func TestSortino(t *testing.T) {
	t.Run("monotonic up has no downside", func(t *testing.T) {
		got := Sortino([]float64{0.01, 0.02, 0.03, 0.04}, 0)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN when no downside, got %v", got)
		}
	})

	t.Run("mixed returns yields finite value", func(t *testing.T) {
		got := Sortino([]float64{0.05, -0.02, 0.03, -0.01, 0.04}, 0)
		if math.IsNaN(got) {
			t.Error("expected finite Sortino for mixed returns")
		}
	})
}

func TestBeta(t *testing.T) {
	t.Run("perfectly correlated 1:1 returns 1", func(t *testing.T) {
		asset := []float64{0.01, 0.02, 0.03, -0.01}
		bench := []float64{0.01, 0.02, 0.03, -0.01}
		got := Beta(asset, bench)
		if math.Abs(got-1) > 1e-9 {
			t.Errorf("expected beta=1, got %v", got)
		}
	})

	t.Run("2x amplitude returns 2", func(t *testing.T) {
		asset := []float64{0.02, 0.04, 0.06, -0.02}
		bench := []float64{0.01, 0.02, 0.03, -0.01}
		got := Beta(asset, bench)
		if math.Abs(got-2) > 1e-9 {
			t.Errorf("expected beta=2, got %v", got)
		}
	})

	t.Run("length mismatch returns NaN", func(t *testing.T) {
		if !math.IsNaN(Beta([]float64{1, 2}, []float64{1})) {
			t.Error("expected NaN for length mismatch")
		}
	})

	t.Run("zero benchmark variance returns NaN", func(t *testing.T) {
		got := Beta([]float64{1, 2, 3}, []float64{5, 5, 5})
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for zero bench variance, got %v", got)
		}
	})
}

func TestMaxDrawdown(t *testing.T) {
	t.Run("monotonic up has zero drawdown", func(t *testing.T) {
		got := MaxDrawdown([]float64{100, 110, 120, 130})
		if got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})

	t.Run("flat has zero drawdown", func(t *testing.T) {
		got := MaxDrawdown([]float64{100, 100, 100})
		if got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})

	t.Run("known drawdown", func(t *testing.T) {
		// peak 120, trough 90 → DD = (120-90)/120 = 0.25
		got := MaxDrawdown([]float64{100, 120, 110, 90, 95})
		if math.Abs(got-0.25) > 1e-9 {
			t.Errorf("expected 0.25, got %v", got)
		}
	})

	t.Run("empty or single", func(t *testing.T) {
		if MaxDrawdown(nil) != 0 {
			t.Error("nil should be 0")
		}
		if MaxDrawdown([]float64{100}) != 0 {
			t.Error("single point should be 0")
		}
	})
}
