package indicator

import (
	"testing"
)

func TestOBV(t *testing.T) {
	t.Run("monotonic up adds all volume", func(t *testing.T) {
		closes := []float64{1, 2, 3, 4}
		vols := []float64{10, 20, 30, 40}
		got := OBV(closes, vols)
		want := []float64{0, 20, 50, 90}
		assertFloatSliceEqual(t, got, want, 1e-9)
	})

	t.Run("monotonic down subtracts all volume", func(t *testing.T) {
		closes := []float64{4, 3, 2, 1}
		vols := []float64{10, 20, 30, 40}
		got := OBV(closes, vols)
		want := []float64{0, -20, -50, -90}
		assertFloatSliceEqual(t, got, want, 1e-9)
	})

	t.Run("flat close holds value", func(t *testing.T) {
		closes := []float64{5, 5, 5, 5}
		vols := []float64{1, 2, 3, 4}
		got := OBV(closes, vols)
		want := []float64{0, 0, 0, 0}
		assertFloatSliceEqual(t, got, want, 1e-9)
	})

	t.Run("alternating direction nets correctly", func(t *testing.T) {
		closes := []float64{10, 11, 10, 12}
		vols := []float64{5, 5, 5, 5}
		got := OBV(closes, vols)
		want := []float64{0, 5, 0, 5}
		assertFloatSliceEqual(t, got, want, 1e-9)
	})

	t.Run("empty input", func(t *testing.T) {
		got := OBV(nil, nil)
		if len(got) != 0 {
			t.Fatalf("want empty, got %v", got)
		}
	})

	t.Run("mismatched lengths returns zero-filled", func(t *testing.T) {
		got := OBV([]float64{1, 2}, []float64{1})
		if len(got) != 2 {
			t.Fatalf("want length 2, got %d", len(got))
		}
		for i, v := range got {
			if v != 0 {
				t.Fatalf("i=%d want 0 for mismatched input, got %v", i, v)
			}
		}
	})
}
