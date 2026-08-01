package indicator

import (
	"math"
	"testing"
)

func TestRSI(t *testing.T) {
	t.Run("insufficient data returns all NaN", func(t *testing.T) {
		got := RSI([]float64{1, 2, 3}, 14)
		if len(got) != 3 {
			t.Fatalf("length %d, want 3", len(got))
		}
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})

	t.Run("period<=0 returns all NaN", func(t *testing.T) {
		got := RSI([]float64{1, 2, 3, 4, 5}, 0)
		for _, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("want NaN, got %v", v)
			}
		}
	})

	t.Run("monotonic up saturates at 100", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = float64(i + 1)
		}
		got := RSI(in, 14)
		last := got[len(got)-1]
		if last != 100 {
			t.Fatalf("expected 100 on pure uptrend, got %v", last)
		}
	})

	t.Run("monotonic down bottoms near 0", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = float64(100 - i)
		}
		got := RSI(in, 14)
		last := got[len(got)-1]
		if last > 1 {
			t.Fatalf("expected RSI near 0 on pure downtrend, got %v", last)
		}
	})

	// Wilder's published RSI(14) worked example, the series every reference
	// implementation is checked against.
	t.Run("matches the canonical Wilder worked example", func(t *testing.T) {
		in := []float64{
			44.3389, 44.0902, 44.1497, 43.6124, 44.3278, 44.8264, 45.0955,
			45.4245, 45.8433, 46.0826, 45.8931, 46.0328, 45.6140, 46.2820,
			46.2820, 46.0028, 46.0328, 46.4116, 46.2222, 45.6439, 46.2122,
			46.2521, 45.7137, 46.4515, 45.7835, 45.3548, 44.0288, 44.1783,
			44.2181, 44.5672, 43.4205, 42.6628, 43.1314,
		}
		want := map[int]float64{
			14: 70.5328,
			15: 66.3186,
			16: 66.5498,
			32: 37.7730,
		}
		got := RSI(in, 14)
		for i, w := range want {
			if math.Abs(got[i]-w) > 1e-4 {
				t.Fatalf("i=%d: got %v, want %v", i, got[i], w)
			}
		}
	})

	t.Run("warmup NaN then numeric", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = 50 + float64(i%5) // oscillating
		}
		got := RSI(in, 14)
		for i := 0; i < 14; i++ {
			if !math.IsNaN(got[i]) {
				t.Fatalf("i=%d want NaN, got %v", i, got[i])
			}
		}
		for i := 14; i < 30; i++ {
			if math.IsNaN(got[i]) {
				t.Fatalf("i=%d want numeric, got NaN", i)
			}
			if got[i] < 0 || got[i] > 100 {
				t.Fatalf("i=%d out of range: %v", i, got[i])
			}
		}
	})

	// A completely flat window has no gains AND no losses, so it carries no
	// directional information. Returning 100 there (the naive division-by-zero
	// branch) makes `rsi_above 70` fire on every symbol whose polled price
	// stops changing overnight or over a weekend.
	t.Run("flat series is neutral, RSI=50", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = 42
		}
		got := RSI(in, 14)
		for i := 14; i < len(got); i++ {
			if got[i] != 50 {
				t.Fatalf("i=%d: flat series should read neutral 50, got %v", i, got[i])
			}
		}
	})

	t.Run("all gains still reads 100", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = 10 + float64(i)
		}
		got := RSI(in, 14)
		for i := 14; i < len(got); i++ {
			if got[i] != 100 {
				t.Fatalf("i=%d: unbroken gains should read 100, got %v", i, got[i])
			}
		}
	})

	t.Run("all losses still reads 0", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = 100 - float64(i)
		}
		got := RSI(in, 14)
		for i := 14; i < len(got); i++ {
			if got[i] != 0 {
				t.Fatalf("i=%d: unbroken losses should read 0, got %v", i, got[i])
			}
		}
	})

	t.Run("a non-finite close does not poison the rest of the series", func(t *testing.T) {
		in := make([]float64, 40)
		for i := range in {
			in[i] = 50 + float64(i%5)
		}
		in[20] = math.NaN()
		got := RSI(in, 14)
		if !math.IsNaN(got[20]) {
			t.Fatalf("i=20 should be NaN for a non-finite close, got %v", got[20])
		}
		for i := 21; i < len(got); i++ {
			if math.IsNaN(got[i]) {
				t.Fatalf("i=%d should recover after the gap, got NaN", i)
			}
		}
	})

	t.Run("output never escapes [0, 100]", func(t *testing.T) {
		in := []float64{
			1e300, -1e300, 1e300, -1e300, 0, 1e-300, -1e-300, 5, 5, 5,
			1e300, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		}
		got := RSI(in, 14)
		for i, v := range got {
			if math.IsNaN(v) {
				continue
			}
			if v < 0 || v > 100 || math.IsInf(v, 0) {
				t.Fatalf("i=%d out of range: %v", i, v)
			}
		}
	})
}
