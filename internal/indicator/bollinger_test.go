package indicator

import (
	"math"
	"testing"
)

func TestBollinger(t *testing.T) {
	t.Run("constant series collapses bands to SMA", func(t *testing.T) {
		in := make([]float64, 30)
		for i := range in {
			in[i] = 7
		}
		r := Bollinger(in, 20, 2.0)
		for i := 19; i < len(in); i++ {
			if math.Abs(r.Middle[i]-7) > 1e-9 {
				t.Fatalf("middle i=%d = %v, want 7", i, r.Middle[i])
			}
			if math.Abs(r.Upper[i]-7) > 1e-9 {
				t.Fatalf("upper i=%d = %v, want 7 (zero variance)", i, r.Upper[i])
			}
			if math.Abs(r.Lower[i]-7) > 1e-9 {
				t.Fatalf("lower i=%d = %v, want 7 (zero variance)", i, r.Lower[i])
			}
		}
	})

	t.Run("bands are symmetric around middle", func(t *testing.T) {
		in := make([]float64, 50)
		for i := range in {
			in[i] = math.Sin(float64(i)/3) * 10
		}
		r := Bollinger(in, 20, 2.0)
		for i := 19; i < len(in); i++ {
			upDist := r.Upper[i] - r.Middle[i]
			loDist := r.Middle[i] - r.Lower[i]
			if math.Abs(upDist-loDist) > 1e-9 {
				t.Fatalf("asymmetric bands at i=%d: up=%v lo=%v", i, upDist, loDist)
			}
		}
	})

	t.Run("warmup NaN before period fills", func(t *testing.T) {
		in := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		r := Bollinger(in, 5, 2.0)
		for i := 0; i < 4; i++ {
			if !math.IsNaN(r.Middle[i]) || !math.IsNaN(r.Upper[i]) || !math.IsNaN(r.Lower[i]) {
				t.Fatalf("expected NaN at i=%d", i)
			}
		}
		for i := 4; i < len(in); i++ {
			if math.IsNaN(r.Middle[i]) {
				t.Fatalf("expected numeric middle at i=%d", i)
			}
		}
	})

	t.Run("upper always >= middle >= lower when numeric", func(t *testing.T) {
		in := make([]float64, 40)
		for i := range in {
			in[i] = float64(i%7) + float64(i)/10
		}
		r := Bollinger(in, 10, 2.0)
		for i := range in {
			if math.IsNaN(r.Middle[i]) {
				continue
			}
			if r.Upper[i] < r.Middle[i] || r.Middle[i] < r.Lower[i] {
				t.Fatalf("ordering broken at i=%d: U=%v M=%v L=%v", i, r.Upper[i], r.Middle[i], r.Lower[i])
			}
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		r := Bollinger([]float64{}, 20, 2.0)
		if len(r.Upper) != 0 || len(r.Middle) != 0 || len(r.Lower) != 0 {
			t.Fatalf("expected empty")
		}
	})

	t.Run("zero period returns all NaN", func(t *testing.T) {
		r := Bollinger([]float64{1, 2, 3}, 0, 2.0)
		for _, v := range r.Middle {
			if !math.IsNaN(v) {
				t.Fatalf("expected NaN, got %v", v)
			}
		}
	})
}
