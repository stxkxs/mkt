package cmd

import (
	"math"
	"strings"
	"testing"
)

// TestPlanStopRejectsUnsizableTrades covers every input that made
// PositionSize answer (0, 0). As a CLI result that zero is indistinguishable
// from a correct "this trade is too small to take", so each case must be
// named rather than printed as a position of zero shares.
func TestPlanStopRejectsUnsizableTrades(t *testing.T) {
	tests := []struct {
		name                      string
		equity, risk, entry, stop float64
		atr, atrMult              float64
		long                      bool
		wantErrContains           string
	}{
		{
			name: "zero equity", equity: 0, risk: 1, entry: 100, stop: 95, long: true,
			wantErrContains: "--equity must be positive",
		},
		{
			name: "zero risk", equity: 10000, risk: 0, entry: 100, stop: 95, long: true,
			wantErrContains: "--risk must be positive",
		},
		{
			name: "negative risk", equity: 10000, risk: -2, entry: 100, stop: 95, long: true,
			wantErrContains: "--risk must be positive",
		},
		{
			name: "risk over 100 percent", equity: 10000, risk: 150, entry: 100, stop: 95, long: true,
			wantErrContains: "exceeds 100%",
		},
		{
			name: "zero entry", equity: 10000, risk: 1, entry: 0, stop: 95, long: true,
			wantErrContains: "--entry must be positive",
		},
		{
			name: "negative stop", equity: 10000, risk: 1, entry: 100, stop: -5, long: true,
			wantErrContains: "--stop must be positive",
		},
		{
			name: "stop equals entry", equity: 10000, risk: 1, entry: 100, stop: 100, long: true,
			wantErrContains: "risk per share is zero",
		},
		{
			name: "long stop above entry", equity: 10000, risk: 1, entry: 100, stop: 110, long: true,
			wantErrContains: "long stop must be below the entry",
		},
		{
			name: "short stop below entry", equity: 10000, risk: 1, entry: 100, stop: 90, long: false,
			wantErrContains: "short stop must be above the entry",
		},
		{
			name: "neither stop nor atr", equity: 10000, risk: 1, entry: 100, long: true,
			wantErrContains: "provide either --stop or --atr",
		},
		{
			name: "non-positive atr multiplier", equity: 10000, risk: 1, entry: 100, atr: 5, atrMult: 0, long: true,
			wantErrContains: "--atr-mult must be positive",
		},
		{
			// 3 × ATR 40 = 120, below a long entry of 100: the implied stop is
			// negative, which PositionSize would have turned into a silently
			// oversized position rather than an error.
			name: "atr stop falls at or below zero", equity: 10000, risk: 1, entry: 100, atr: 40, atrMult: 3, long: true,
			wantErrContains: "use a smaller --atr-mult",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := planStop(tt.equity, tt.risk, tt.entry, tt.stop, tt.atr, tt.atrMult, tt.long)
			if err == nil {
				t.Fatalf("planStop accepted an unsizable trade")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErrContains)
			}
		})
	}
}

// TestPlanStopAcceptsValidTrades checks the cases that must still work, in
// both directions and through both ways of specifying the stop.
func TestPlanStopAcceptsValidTrades(t *testing.T) {
	tests := []struct {
		name                      string
		equity, risk, entry, stop float64
		atr, atrMult              float64
		long                      bool
		want                      float64
	}{
		{name: "long explicit stop", equity: 10000, risk: 1, entry: 100, stop: 95, long: true, want: 95},
		{name: "short explicit stop", equity: 10000, risk: 1, entry: 100, stop: 105, long: false, want: 105},
		{name: "long atr stop", equity: 10000, risk: 1, entry: 100, atr: 5, atrMult: 2, long: true, want: 90},
		{name: "short atr stop", equity: 10000, risk: 1, entry: 100, atr: 5, atrMult: 2, long: false, want: 110},
		{name: "risk of exactly 100 percent", equity: 10000, risk: 100, entry: 100, stop: 95, long: true, want: 95},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := planStop(tt.equity, tt.risk, tt.entry, tt.stop, tt.atr, tt.atrMult, tt.long)
			if err != nil {
				t.Fatalf("planStop rejected a valid trade: %v", err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("stop = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPositionCommandRejectsBadInputWithoutUsageDump is the end-to-end
// statement of the same thing, and doubles as the SilenceUsage regression: a
// validation failure prints the one line the user needs, not the command
// tree.
func TestPositionCommandRejectsBadInputWithoutUsageDump(t *testing.T) {
	out, errOut, err := runCLI(t, nil, "position", "--equity", "10000", "--risk", "0", "--entry", "100", "--stop", "95")
	if err == nil {
		t.Fatal("expected an error for --risk 0")
	}
	if !strings.Contains(err.Error(), "--risk must be positive") {
		t.Errorf("error = %v", err)
	}
	if strings.Contains(out+errOut, "Usage:") {
		t.Errorf("a runtime failure printed a usage dump:\n%s%s", out, errOut)
	}
}

// TestPositionCommandPrintsThePlan checks the happy path still renders every
// field, including the side it was sized for.
func TestPositionCommandPrintsThePlan(t *testing.T) {
	out, _, err := runCLI(t, nil, "position", "--equity", "10000", "--risk", "1", "--entry", "100", "--stop", "95")
	if err != nil {
		t.Fatalf("position: %v", err)
	}
	for _, want := range []string{"Side:", "long", "Equity:", "Risk:", "Entry:", "Stop:", "Shares:", "Notional:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// $10,000 at 1% is $100 of risk over a $5 stop distance: 20 shares.
	if !strings.Contains(out, "20.0000") {
		t.Errorf("expected 20 shares:\n%s", out)
	}
}

// TestPositionCommandSizesAShort checks --long=false flips both the accepted
// stop side and the reported side.
func TestPositionCommandSizesAShort(t *testing.T) {
	out, _, err := runCLI(t, nil, "position", "--equity", "10000", "--risk", "1", "--entry", "100", "--stop", "105", "--long=false")
	if err != nil {
		t.Fatalf("position: %v", err)
	}
	if !strings.Contains(out, "Side:       short") {
		t.Errorf("expected a short plan:\n%s", out)
	}
}
