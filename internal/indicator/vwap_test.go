package indicator

import (
	"math"
	"testing"
)

func TestVWAP(t *testing.T) {
	t.Run("constant price equals VWAP", func(t *testing.T) {
		n := 10
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		vols := make([]float64, n)
		for i := range highs {
			highs[i] = 100
			lows[i] = 100
			closes[i] = 100
			vols[i] = float64(i + 1)
		}
		got := VWAP(highs, lows, closes, vols)
		for i, v := range got {
			if math.Abs(v-100) > 1e-9 {
				t.Fatalf("i=%d got %v want 100", i, v)
			}
		}
	})

	t.Run("equal-volume average", func(t *testing.T) {
		// All same volume → VWAP equals running mean of typical prices.
		highs := []float64{10, 20, 30}
		lows := []float64{10, 20, 30}
		closes := []float64{10, 20, 30}
		vols := []float64{1, 1, 1}
		got := VWAP(highs, lows, closes, vols)
		want := []float64{10, 15, 20}
		assertFloatSliceEqual(t, got, want, 1e-9)
	})

	t.Run("weights higher-volume bars more", func(t *testing.T) {
		// Two bars: tp=10 vol=1, tp=20 vol=9. VWAP should pull toward 20.
		highs := []float64{10, 20}
		lows := []float64{10, 20}
		closes := []float64{10, 20}
		vols := []float64{1, 9}
		got := VWAP(highs, lows, closes, vols)
		want := []float64{10, (10*1 + 20*9) / 10.0} // = 19
		assertFloatSliceEqual(t, got, want, 1e-9)
	})

	t.Run("zero volume yields NaN at start then recovers", func(t *testing.T) {
		highs := []float64{10, 20, 30}
		lows := []float64{10, 20, 30}
		closes := []float64{10, 20, 30}
		vols := []float64{0, 1, 1}
		got := VWAP(highs, lows, closes, vols)
		if !math.IsNaN(got[0]) {
			t.Fatalf("i=0 expected NaN, got %v", got[0])
		}
		if math.IsNaN(got[1]) {
			t.Fatalf("i=1 expected value, got NaN")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := VWAP(nil, nil, nil, nil)
		if len(got) != 0 {
			t.Fatalf("want empty, got %v", got)
		}
	})

	t.Run("mismatched lengths return all NaN", func(t *testing.T) {
		got := VWAP([]float64{1, 2}, []float64{1}, []float64{1, 2}, []float64{1, 2})
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN for mismatched input, got %v", i, v)
			}
		}
	})
}
