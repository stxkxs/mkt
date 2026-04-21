package indicator

import (
	"math"
	"testing"
)

func TestMACD(t *testing.T) {
	t.Run("output lengths match input", func(t *testing.T) {
		in := make([]float64, 100)
		for i := range in {
			in[i] = float64(i)
		}
		r := MACD(in, 12, 26, 9)
		if len(r.MACD) != len(in) || len(r.Signal) != len(in) || len(r.Histogram) != len(in) {
			t.Fatalf("length mismatch: macd=%d signal=%d hist=%d", len(r.MACD), len(r.Signal), len(r.Histogram))
		}
	})

	t.Run("constant input converges to zero", func(t *testing.T) {
		in := make([]float64, 100)
		for i := range in {
			in[i] = 50
		}
		r := MACD(in, 12, 26, 9)
		// Once both EMAs and signal EMA have filled, MACD = Signal = Histogram = 0
		last := len(in) - 1
		if math.IsNaN(r.MACD[last]) || math.Abs(r.MACD[last]) > 1e-9 {
			t.Fatalf("macd last = %v, want 0", r.MACD[last])
		}
		if math.IsNaN(r.Signal[last]) || math.Abs(r.Signal[last]) > 1e-9 {
			t.Fatalf("signal last = %v, want 0", r.Signal[last])
		}
		if math.IsNaN(r.Histogram[last]) || math.Abs(r.Histogram[last]) > 1e-9 {
			t.Fatalf("hist last = %v, want 0", r.Histogram[last])
		}
	})

	t.Run("uptrend produces positive MACD", func(t *testing.T) {
		in := make([]float64, 100)
		for i := range in {
			in[i] = float64(i)
		}
		r := MACD(in, 12, 26, 9)
		last := len(in) - 1
		if math.IsNaN(r.MACD[last]) {
			t.Fatalf("macd NaN at end")
		}
		if r.MACD[last] <= 0 {
			t.Fatalf("uptrend should produce positive MACD, got %v", r.MACD[last])
		}
	})

	t.Run("histogram equals macd minus signal", func(t *testing.T) {
		in := make([]float64, 100)
		for i := range in {
			in[i] = float64(i) + float64(i%7)
		}
		r := MACD(in, 12, 26, 9)
		for i := range r.Histogram {
			if math.IsNaN(r.Histogram[i]) {
				continue
			}
			want := r.MACD[i] - r.Signal[i]
			if math.Abs(r.Histogram[i]-want) > 1e-9 {
				t.Fatalf("i=%d hist=%v want %v", i, r.Histogram[i], want)
			}
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		r := MACD([]float64{}, 12, 26, 9)
		if len(r.MACD) != 0 || len(r.Signal) != 0 || len(r.Histogram) != 0 {
			t.Fatalf("expected empty result")
		}
	})

	t.Run("short input returns all NaN", func(t *testing.T) {
		r := MACD([]float64{1, 2, 3}, 12, 26, 9)
		if len(r.MACD) != 3 {
			t.Fatalf("length mismatch")
		}
		for _, v := range r.MACD {
			if !math.IsNaN(v) {
				t.Fatalf("expected NaN, got %v", v)
			}
		}
	})
}
