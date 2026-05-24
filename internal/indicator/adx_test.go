package indicator

import (
	"math"
	"testing"
)

func TestADX(t *testing.T) {
	t.Run("monotonic up trend rises", func(t *testing.T) {
		n := 60
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = float64(i + 2)
			lows[i] = float64(i)
			closes[i] = float64(i + 1)
		}
		adx, plusDI, minusDI := ADX(highs, lows, closes, 14)
		last := adx[n-1]
		if math.IsNaN(last) {
			t.Fatalf("expected ADX value at end of trend, got NaN")
		}
		if last < 30 {
			t.Errorf("ADX should be elevated for a clean trend, got %v", last)
		}
		if plusDI[n-1] <= minusDI[n-1] {
			t.Errorf("+DI should exceed -DI on up trend: +DI=%v -DI=%v", plusDI[n-1], minusDI[n-1])
		}
	})

	t.Run("warm-up NaN handled", func(t *testing.T) {
		n := 60
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = 101
			lows[i] = 99
			closes[i] = 100
		}
		adx, _, _ := ADX(highs, lows, closes, 14)
		for i := 0; i < 2*14-1; i++ {
			if !math.IsNaN(adx[i]) {
				t.Fatalf("i=%d expected NaN, got %v", i, adx[i])
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		adx, plusDI, minusDI := ADX(nil, nil, nil, 14)
		if len(adx) != 0 || len(plusDI) != 0 || len(minusDI) != 0 {
			t.Fatalf("want empty, got adx=%v +DI=%v -DI=%v", adx, plusDI, minusDI)
		}
	})

	t.Run("insufficient length returns NaN", func(t *testing.T) {
		adx, _, _ := ADX(
			[]float64{1, 2, 3},
			[]float64{0, 1, 2},
			[]float64{0.5, 1.5, 2.5},
			14,
		)
		for i, v := range adx {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN for short input, got %v", i, v)
			}
		}
	})
}
