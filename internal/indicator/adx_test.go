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

	t.Run("values stay in [0, 100] and never leak an infinity", func(t *testing.T) {
		n := 80
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			base := 100 + 20*math.Sin(float64(i)/5)
			highs[i] = base + float64(i%3)
			lows[i] = base - float64(i%4)
			closes[i] = base
		}
		adx, plusDI, minusDI := ADX(highs, lows, closes, 14)
		for _, series := range [][]float64{adx, plusDI, minusDI} {
			for i, v := range series {
				if math.IsNaN(v) {
					continue
				}
				if math.IsInf(v, 0) || v < 0 || v > 100 {
					t.Fatalf("i=%d out of [0,100]: %v", i, v)
				}
			}
		}
	})

	t.Run("a non-finite bar does not poison the rest of the series", func(t *testing.T) {
		n := 80
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = float64(i + 2)
			lows[i] = float64(i)
			closes[i] = float64(i + 1)
		}
		highs[40] = math.NaN()
		adx, plusDI, minusDI := ADX(highs, lows, closes, 14)
		if math.IsNaN(adx[n-1]) {
			t.Fatalf("ADX should still resolve after a bad bar, got NaN")
		}
		if math.IsNaN(plusDI[n-1]) || math.IsNaN(minusDI[n-1]) {
			t.Fatalf("DI should still resolve after a bad bar: +DI=%v -DI=%v",
				plusDI[n-1], minusDI[n-1])
		}
	})

	t.Run("flat series yields zero DI and ADX rather than NaN", func(t *testing.T) {
		n := 60
		highs := make([]float64, n)
		lows := make([]float64, n)
		closes := make([]float64, n)
		for i := range highs {
			highs[i] = 100
			lows[i] = 100
			closes[i] = 100
		}
		adx, plusDI, minusDI := ADX(highs, lows, closes, 14)
		for i := 2 * 14; i < n; i++ {
			if adx[i] != 0 || plusDI[i] != 0 || minusDI[i] != 0 {
				t.Fatalf("i=%d: zero-range series should read 0, got adx=%v +DI=%v -DI=%v",
					i, adx[i], plusDI[i], minusDI[i])
			}
		}
	})
}
