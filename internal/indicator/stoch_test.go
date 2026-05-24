package indicator

import (
	"math"
	"testing"
)

func TestStochastic(t *testing.T) {
	t.Run("monotonic up converges to 100", func(t *testing.T) {
		n := 20
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = float64(i + 2)
			lows[i] = float64(i)
			closes[i] = float64(i + 2) // close at the high
		}
		k, _ := Stochastic(highs, lows, closes, 14, 3)
		last := k[n-1]
		if math.IsNaN(last) || last < 95 {
			t.Fatalf("expected K near 100 on monotonic up, got %v", last)
		}
	})

	t.Run("monotonic down converges to 0", func(t *testing.T) {
		n := 20
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			v := float64(n - i)
			highs[i] = v + 1
			lows[i] = v - 1
			closes[i] = v - 1 // close at the low
		}
		k, _ := Stochastic(highs, lows, closes, 14, 3)
		last := k[n-1]
		if math.IsNaN(last) || last > 5 {
			t.Fatalf("expected K near 0 on monotonic down, got %v", last)
		}
	})

	t.Run("warm-up entries are NaN", func(t *testing.T) {
		k, d := Stochastic(
			[]float64{2, 3, 4, 5, 6},
			[]float64{1, 2, 3, 4, 5},
			[]float64{1.5, 2.5, 3.5, 4.5, 5.5},
			3, 2,
		)
		for i := 0; i < 2; i++ {
			if !math.IsNaN(k[i]) {
				t.Fatalf("k[%d] expected NaN, got %v", i, k[i])
			}
		}
		if math.IsNaN(k[2]) {
			t.Fatalf("k[2] expected value")
		}
		// D is SMA over kSlice; needs 2 valid K values, so first valid at index 3
		if !math.IsNaN(d[2]) {
			t.Fatalf("d[2] expected NaN (SMA warm-up)")
		}
	})

	t.Run("flat range yields NaN K", func(t *testing.T) {
		k, _ := Stochastic(
			[]float64{5, 5, 5, 5},
			[]float64{5, 5, 5, 5},
			[]float64{5, 5, 5, 5},
			3, 2,
		)
		for i := 2; i < len(k); i++ {
			if !math.IsNaN(k[i]) {
				t.Fatalf("k[%d] expected NaN for flat range, got %v", i, k[i])
			}
		}
	})

	t.Run("values stay in [0, 100]", func(t *testing.T) {
		highs := []float64{10, 11, 12, 13, 14, 15, 14, 13, 12, 11, 10, 11, 12, 13, 14}
		lows := []float64{9, 10, 11, 12, 13, 14, 13, 12, 11, 10, 9, 10, 11, 12, 13}
		closes := []float64{9.5, 10.5, 11.5, 12.5, 13.5, 14.5, 13.5, 12.5, 11.5, 10.5, 9.5, 10.5, 11.5, 12.5, 13.5}
		k, d := Stochastic(highs, lows, closes, 5, 3)
		for i, v := range k {
			if math.IsNaN(v) {
				continue
			}
			if v < 0 || v > 100 {
				t.Fatalf("k[%d] = %v out of [0,100]", i, v)
			}
		}
		for i, v := range d {
			if math.IsNaN(v) {
				continue
			}
			if v < 0 || v > 100 {
				t.Fatalf("d[%d] = %v out of [0,100]", i, v)
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		k, d := Stochastic(nil, nil, nil, 14, 3)
		if len(k) != 0 || len(d) != 0 {
			t.Fatalf("want empty, got k=%v d=%v", k, d)
		}
	})

	t.Run("mismatched lengths return all NaN", func(t *testing.T) {
		k, d := Stochastic([]float64{1, 2}, []float64{1}, []float64{1, 2}, 14, 3)
		for i, v := range k {
			if !math.IsNaN(v) {
				t.Fatalf("k[%d] want NaN, got %v", i, v)
			}
			if !math.IsNaN(d[i]) {
				t.Fatalf("d[%d] want NaN, got %v", i, d[i])
			}
		}
	})
}
