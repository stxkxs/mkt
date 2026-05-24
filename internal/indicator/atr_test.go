package indicator

import (
	"math"
	"testing"
)

func TestATR(t *testing.T) {
	t.Run("constant range yields constant ATR", func(t *testing.T) {
		n := 30
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = 101
			lows[i] = 99
			closes[i] = 100
		}
		atr := ATR(highs, lows, closes, 14)
		for i := 14; i < n; i++ {
			if math.IsNaN(atr[i]) || math.Abs(atr[i]-2) > 1e-9 {
				t.Fatalf("i=%d got %v want 2", i, atr[i])
			}
		}
	})

	t.Run("warm-up entries are NaN", func(t *testing.T) {
		highs := []float64{2, 3, 4, 5, 6}
		lows := []float64{1, 2, 3, 4, 5}
		closes := []float64{1.5, 2.5, 3.5, 4.5, 5.5}
		atr := ATR(highs, lows, closes, 3)
		for i := 0; i < 3; i++ {
			if !math.IsNaN(atr[i]) {
				t.Fatalf("i=%d expected NaN, got %v", i, atr[i])
			}
		}
		if math.IsNaN(atr[3]) {
			t.Fatalf("i=3 expected value")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		atr := ATR(nil, nil, nil, 14)
		if len(atr) != 0 {
			t.Fatalf("want empty, got %v", atr)
		}
	})

	t.Run("zero period returns all NaN", func(t *testing.T) {
		atr := ATR([]float64{1, 2, 3}, []float64{0, 1, 2}, []float64{0.5, 1.5, 2.5}, 0)
		for i, v := range atr {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})

	t.Run("insufficient length returns all NaN", func(t *testing.T) {
		atr := ATR([]float64{1, 2}, []float64{0, 1}, []float64{0.5, 1.5}, 14)
		for i, v := range atr {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN for short input, got %v", i, v)
			}
		}
	})
}
