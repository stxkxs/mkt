package indicator

import (
	"math"
	"testing"
)

func TestStddev(t *testing.T) {
	t.Run("constant series returns 0 after warm-up", func(t *testing.T) {
		got := Stddev([]float64{5, 5, 5, 5, 5}, 3)
		for i := 2; i < len(got); i++ {
			if math.Abs(got[i]) > 1e-12 {
				t.Fatalf("i=%d expected 0, got %v", i, got[i])
			}
		}
	})

	t.Run("known sample stddev", func(t *testing.T) {
		// Values [2,4,4,4,5,5,7,9], full-window sample stddev = 2.138...
		got := Stddev([]float64{2, 4, 4, 4, 5, 5, 7, 9}, 8)
		want := 2.138089935299395
		if math.Abs(got[7]-want) > 1e-9 {
			t.Fatalf("got %v, want %v", got[7], want)
		}
	})

	t.Run("warm-up NaN", func(t *testing.T) {
		got := Stddev([]float64{1, 2, 3, 4, 5}, 4)
		for i := 0; i < 3; i++ {
			if !math.IsNaN(got[i]) {
				t.Fatalf("i=%d expected NaN, got %v", i, got[i])
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := Stddev(nil, 5)
		if len(got) != 0 {
			t.Fatalf("expected empty, got %v", got)
		}
	})

	t.Run("period less than 2 returns all NaN", func(t *testing.T) {
		got := Stddev([]float64{1, 2, 3}, 1)
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})

	t.Run("period > length leaves all NaN", func(t *testing.T) {
		got := Stddev([]float64{1, 2}, 5)
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})

	t.Run("negative period returns all NaN", func(t *testing.T) {
		got := Stddev([]float64{1, 2, 3}, -4)
		for i, v := range got {
			if !math.IsNaN(v) {
				t.Fatalf("i=%d want NaN, got %v", i, v)
			}
		}
	})

	t.Run("non-finite values blank only the windows they touch", func(t *testing.T) {
		got := Stddev([]float64{1, 2, math.Inf(1), 4, 5, 6, 7}, 3)
		for i := 0; i < 5; i++ {
			if !math.IsNaN(got[i]) {
				t.Fatalf("i=%d want NaN, got %v", i, got[i])
			}
		}
		for i := 5; i < len(got); i++ {
			if math.IsNaN(got[i]) || math.IsInf(got[i], 0) {
				t.Fatalf("i=%d should have recovered, got %v", i, got[i])
			}
		}
	})
}
