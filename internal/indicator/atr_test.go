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

	t.Run("length exactly period+1 emits one value", func(t *testing.T) {
		n := 4
		highs := []float64{2, 3, 4, 5}
		lows := []float64{1, 2, 3, 4}
		closes := []float64{1.5, 2.5, 3.5, 4.5}
		atr := ATR(highs, lows, closes, 3)
		for i := 0; i < n-1; i++ {
			if !math.IsNaN(atr[i]) {
				t.Fatalf("i=%d expected NaN, got %v", i, atr[i])
			}
		}
		if math.IsNaN(atr[n-1]) {
			t.Fatalf("i=%d expected the seed value, got NaN", n-1)
		}
	})

	t.Run("a non-finite bar does not poison the rest of the series", func(t *testing.T) {
		n := 30
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = 101
			lows[i] = 99
			closes[i] = 100
		}
		closes[19] = math.NaN() // breaks TR at bars 19 and 20
		atr := ATR(highs, lows, closes, 14)
		var bad int
		for i := 14; i < n; i++ {
			if math.IsNaN(atr[i]) {
				bad++
			}
		}
		if bad == 0 {
			t.Fatalf("expected the bad bars themselves to report NaN")
		}
		if math.IsNaN(atr[n-1]) {
			t.Fatalf("i=%d should have recovered after the gap, got NaN", n-1)
		}
	})

	t.Run("all output is finite for finite input", func(t *testing.T) {
		n := 40
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = 1e-9
			lows[i] = 0
			closes[i] = 0
		}
		atr := ATR(highs, lows, closes, 14)
		for i := 14; i < n; i++ {
			if math.IsNaN(atr[i]) || math.IsInf(atr[i], 0) {
				t.Fatalf("i=%d leaked %v", i, atr[i])
			}
		}
	})
}
