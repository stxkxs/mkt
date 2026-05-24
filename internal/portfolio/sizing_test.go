package portfolio

import (
	"math"
	"testing"
)

func TestPositionSizeKnownValue(t *testing.T) {
	// equity 100k, risk 1%, entry 50, stop 48 → 500 shares, $1000 risk
	shares, dollar := PositionSize(100_000, 1, 50, 48)
	if math.Abs(shares-500) > 1e-9 {
		t.Errorf("shares: got %v want 500", shares)
	}
	if math.Abs(dollar-1000) > 1e-9 {
		t.Errorf("dollar: got %v want 1000", dollar)
	}
}

func TestPositionSizeShortDirection(t *testing.T) {
	// Stop above entry → shorts; absolute distance is what matters.
	shares, dollar := PositionSize(100_000, 1, 50, 52)
	if math.Abs(shares-500) > 1e-9 {
		t.Errorf("shares: got %v want 500", shares)
	}
	if math.Abs(dollar-1000) > 1e-9 {
		t.Errorf("dollar: got %v want 1000", dollar)
	}
}

func TestPositionSizeDegenerateZero(t *testing.T) {
	cases := []struct {
		name                                 string
		equity, risk, entry, stop, wantShare float64
	}{
		{"zero equity", 0, 1, 50, 48, 0},
		{"negative equity", -100, 1, 50, 48, 0},
		{"zero risk", 100_000, 0, 50, 48, 0},
		{"negative risk", 100_000, -1, 50, 48, 0},
		{"entry equals stop", 100_000, 1, 50, 50, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shares, _ := PositionSize(tc.equity, tc.risk, tc.entry, tc.stop)
			if shares != tc.wantShare {
				t.Errorf("shares: got %v want %v", shares, tc.wantShare)
			}
		})
	}
}

func TestATRStop(t *testing.T) {
	// Long: entry 50, ATR 1.5, mult 2 → 47
	if got := ATRStop(50, 1.5, 2, true); math.Abs(got-47) > 1e-9 {
		t.Errorf("long stop: got %v want 47", got)
	}
	// Short: 53
	if got := ATRStop(50, 1.5, 2, false); math.Abs(got-53) > 1e-9 {
		t.Errorf("short stop: got %v want 53", got)
	}
}
