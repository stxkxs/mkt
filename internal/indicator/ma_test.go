package indicator

import (
	"math"
	"testing"
)

func TestSMA(t *testing.T) {
	tests := []struct {
		name   string
		input  []float64
		period int
		want   []float64
	}{
		{
			name:   "known values period 3",
			input:  []float64{1, 2, 3, 4, 5},
			period: 3,
			want:   []float64{math.NaN(), math.NaN(), 2, 3, 4},
		},
		{
			name:   "constant series",
			input:  []float64{10, 10, 10, 10, 10},
			period: 3,
			want:   []float64{math.NaN(), math.NaN(), 10, 10, 10},
		},
		{
			name:   "period equals length",
			input:  []float64{2, 4, 6},
			period: 3,
			want:   []float64{math.NaN(), math.NaN(), 4},
		},
		{
			name:   "empty input",
			input:  []float64{},
			period: 3,
			want:   []float64{},
		},
		{
			name:   "zero period",
			input:  []float64{1, 2, 3},
			period: 0,
			want:   []float64{math.NaN(), math.NaN(), math.NaN()},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SMA(tc.input, tc.period)
			assertFloatSliceEqual(t, got, tc.want, 1e-9)
		})
	}
}

func TestEMA(t *testing.T) {
	t.Run("constant series converges to constant", func(t *testing.T) {
		in := make([]float64, 50)
		for i := range in {
			in[i] = 100
		}
		got := EMA(in, 10)
		for i := 9; i < len(got); i++ {
			if math.Abs(got[i]-100) > 1e-9 {
				t.Fatalf("i=%d want 100, got %v", i, got[i])
			}
		}
	})

	t.Run("first valid value equals SMA of first period", func(t *testing.T) {
		in := []float64{1, 2, 3, 4, 5, 6, 7, 8}
		ema := EMA(in, 4)
		// EMA seed is SMA of first 4 values
		want := (1.0 + 2 + 3 + 4) / 4
		if math.Abs(ema[3]-want) > 1e-9 {
			t.Fatalf("ema[3]=%v want %v", ema[3], want)
		}
	})

	t.Run("NaN before period fills", func(t *testing.T) {
		got := EMA([]float64{1, 2, 3, 4, 5}, 4)
		for i := 0; i < 3; i++ {
			if !math.IsNaN(got[i]) {
				t.Fatalf("expected NaN at i=%d, got %v", i, got[i])
			}
		}
		if math.IsNaN(got[3]) {
			t.Fatalf("expected value at i=3")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := EMA([]float64{}, 5)
		if len(got) != 0 {
			t.Fatalf("want empty, got %v", got)
		}
	})

	t.Run("zero period returns all NaN", func(t *testing.T) {
		got := EMA([]float64{1, 2, 3}, 0)
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})
}

func assertFloatSliceEqual(t *testing.T, got, want []float64, eps float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		gn := math.IsNaN(got[i])
		wn := math.IsNaN(want[i])
		if gn != wn {
			t.Fatalf("i=%d NaN mismatch: got %v, want %v", i, got[i], want[i])
		}
		if !gn && math.Abs(got[i]-want[i]) > eps {
			t.Fatalf("i=%d: got %v, want %v", i, got[i], want[i])
		}
	}
}
