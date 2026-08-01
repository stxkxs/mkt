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
		{
			name:   "period longer than input",
			input:  []float64{1, 2, 3},
			period: 5,
			want:   []float64{math.NaN(), math.NaN(), math.NaN()},
		},
		{
			// A NaN blanks only the windows that contain it. Folding it
			// into the running sum instead would make every later value
			// NaN — that is what left Stochastic %D permanently NaN.
			name:   "NaN blanks only its own windows",
			input:  []float64{1, 2, math.NaN(), 4, 5, 6, 7},
			period: 3,
			want: []float64{
				math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(),
				5, 6,
			},
		},
		{
			name:   "leading NaN warm-up then real values",
			input:  []float64{math.NaN(), math.NaN(), 3, 4, 5, 6},
			period: 3,
			want:   []float64{math.NaN(), math.NaN(), math.NaN(), math.NaN(), 4, 5},
		},
		{
			name:   "Inf blanks only its own windows",
			input:  []float64{1, 2, math.Inf(1), 4, 5, 6, 7},
			period: 3,
			want: []float64{
				math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(),
				5, 6,
			},
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

	t.Run("period longer than input returns all NaN", func(t *testing.T) {
		got := EMA([]float64{1, 2, 3}, 5)
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})

	t.Run("non-finite sample re-seeds instead of poisoning", func(t *testing.T) {
		in := []float64{1, 2, 3, 4, math.NaN(), 10, 20, 30, 40, 50}
		got := EMA(in, 4)
		if math.IsNaN(got[3]) {
			t.Fatalf("i=3 expected the seed value, got NaN")
		}
		for i := 4; i < 8; i++ {
			if !math.IsNaN(got[i]) {
				t.Fatalf("i=%d expected NaN during the restarted warm-up, got %v", i, got[i])
			}
		}
		// Re-seeds on the simple mean of 10, 20, 30, 40.
		if math.Abs(got[8]-25) > 1e-9 {
			t.Fatalf("i=8 got %v, want the re-seeded mean 25", got[8])
		}
		if math.IsNaN(got[9]) {
			t.Fatalf("i=9 expected a value after the re-seed, got NaN")
		}
	})

	t.Run("Inf sample does not escape into the output", func(t *testing.T) {
		got := EMA([]float64{1, 2, 3, math.Inf(1), 5, 6, 7, 8}, 3)
		for i, v := range got {
			if math.IsInf(v, 0) {
				t.Fatalf("i=%d leaked an infinity", i)
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
