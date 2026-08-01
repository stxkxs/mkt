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

	// %D used to be NaN for every bar of every input: %K's warm-up NaNs were
	// folded into SMA's running sum and poisoned every later value.
	t.Run("D is a real SMA of K, not all NaN", func(t *testing.T) {
		n := 40
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			base := 100 + 5*math.Sin(float64(i)/4)
			highs[i] = base + 1
			lows[i] = base - 1
			closes[i] = base
		}
		k, d := Stochastic(highs, lows, closes, 14, 3)

		// %K is valid from index 13, so %D is valid from index 15.
		for i := 0; i < 15; i++ {
			if !math.IsNaN(d[i]) {
				t.Fatalf("d[%d] expected NaN during warm-up, got %v", i, d[i])
			}
		}
		var valid int
		for i := 15; i < n; i++ {
			if math.IsNaN(d[i]) {
				t.Fatalf("d[%d] expected a value, got NaN", i)
			}
			want := (k[i] + k[i-1] + k[i-2]) / 3
			if math.Abs(d[i]-want) > 1e-9 {
				t.Fatalf("d[%d] = %v, want the 3-period mean of K %v", i, d[i], want)
			}
			valid++
		}
		if valid != n-15 {
			t.Fatalf("got %d valid D values, want %d", valid, n-15)
		}
	})

	t.Run("a flat window blanks D only while it is in range", func(t *testing.T) {
		n := 24
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = 100 + float64(i)
			lows[i] = 98 + float64(i)
			closes[i] = 99 + float64(i)
		}
		// Freeze bars 14..18 flat so the %K window at 18 has zero range.
		for i := 14; i < 19; i++ {
			highs[i], lows[i], closes[i] = 113, 113, 113
		}
		k, d := Stochastic(highs, lows, closes, 5, 3)
		var blanked int
		for i := 7; i < n; i++ {
			if math.IsNaN(d[i]) {
				blanked++
			}
		}
		if blanked == 0 {
			t.Fatalf("expected the flat window to blank some D values")
		}
		if math.IsNaN(d[n-1]) {
			t.Fatalf("d[%d] should have recovered once the flat window passed", n-1)
		}
		if math.IsNaN(k[n-1]) {
			t.Fatalf("k[%d] should have recovered once the flat window passed", n-1)
		}
	})

	t.Run("high below low is rejected, not clamped", func(t *testing.T) {
		// Bad provider data: an inverted bar gives a negative range, which
		// would otherwise produce a clamped but meaningless reading.
		k, _ := Stochastic(
			[]float64{1, 1, 1, 1},
			[]float64{5, 5, 5, 5},
			[]float64{3, 3, 3, 3},
			3, 2,
		)
		for i := 2; i < len(k); i++ {
			if !math.IsNaN(k[i]) {
				t.Fatalf("k[%d] expected NaN for an inverted range, got %v", i, k[i])
			}
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
