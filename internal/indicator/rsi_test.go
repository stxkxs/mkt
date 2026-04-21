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

	t.Run("flat series has no losses, RSI=100", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = 42
		}
		got := RSI(in, 14)
		if got[14] != 100 {
			t.Fatalf("flat series should hit 100 (div-by-zero branch), got %v", got[14])
		}
	})
}
