package indicator

import (
	"math"
	"testing"
)

func TestPivotsClassic(t *testing.T) {
	t.Run("known-value spot check", func(t *testing.T) {
		// H=110, L=90, C=100 → P=100, R1=110, S1=90, R2=120, S2=80, R3=130, S3=70
		p := PivotsClassic(110, 90, 100)
		want := PivotLevels{P: 100, R1: 110, S1: 90, R2: 120, S2: 80, R3: 130, S3: 70}
		if math.Abs(p.P-want.P) > 1e-9 ||
			math.Abs(p.R1-want.R1) > 1e-9 || math.Abs(p.R2-want.R2) > 1e-9 || math.Abs(p.R3-want.R3) > 1e-9 ||
			math.Abs(p.S1-want.S1) > 1e-9 || math.Abs(p.S2-want.S2) > 1e-9 || math.Abs(p.S3-want.S3) > 1e-9 {
			t.Fatalf("got %+v want %+v", p, want)
		}
	})

	t.Run("R/S spacing symmetric for centered close", func(t *testing.T) {
		// Close at midpoint → R2-P should equal P-S2
		p := PivotsClassic(110, 90, 100)
		if math.Abs((p.R2-p.P)-(p.P-p.S2)) > 1e-9 {
			t.Errorf("R2-P (%v) vs P-S2 (%v) should match for symmetric session", p.R2-p.P, p.P-p.S2)
		}
	})

	t.Run("zero range yields all-equal pivots", func(t *testing.T) {
		p := PivotsClassic(100, 100, 100)
		if p.P != 100 || p.R1 != 100 || p.S1 != 100 || p.R2 != 100 || p.S2 != 100 {
			t.Errorf("zero-range session should collapse to single value: %+v", p)
		}
	})
}
