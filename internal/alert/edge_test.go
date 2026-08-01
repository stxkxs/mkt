package alert

import (
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// rising / falling RSI series long enough to clear the 14-period warm-up.
func rsiSeries(n int, step float64) []float64 {
	out := make([]float64, n)
	p := 100.0
	for i := range out {
		out[i] = p
		p += step
	}
	return out
}

// TestEdgeTriggerNoFireOnEstablishingEvaluation covers the fresh-install
// bug: seeded alerts that are already breached must stay quiet until they
// transition, not fire desktop notifications seconds after launch.
func TestEdgeTriggerNoFireOnEstablishingEvaluation(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		// breached is a quote that satisfies the rule; clear does not.
		breached, clear provider.Quote
		prices          func(breached bool) []float64
	}{
		{
			name:     "above",
			rule:     Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 51000},
			clear:    provider.Quote{Symbol: "BTC", Price: 49000},
		},
		{
			name:     "below",
			rule:     Rule{Symbol: "BTC", Condition: CondBelow, Value: 50000, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 49000},
			clear:    provider.Quote{Symbol: "BTC", Price: 51000},
		},
		{
			name:     "pct_up",
			rule:     Rule{Symbol: "BTC", Condition: CondPctUp, Value: 5, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 110, Change: 10},
			clear:    provider.Quote{Symbol: "BTC", Price: 101, Change: 1},
		},
		{
			name:     "pct_down",
			rule:     Rule{Symbol: "BTC", Condition: CondPctDown, Value: 5, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 90, Change: -10},
			clear:    provider.Quote{Symbol: "BTC", Price: 99, Change: -1},
		},
		{
			name:     "volume_above",
			rule:     Rule{Symbol: "BTC", Condition: CondVolumeAbove, Value: 1_000_000, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 100, Volume: 2_000_000},
			clear:    provider.Quote{Symbol: "BTC", Price: 100, Volume: 500_000},
		},
		{
			name:     "rsi_above",
			rule:     Rule{Symbol: "BTC", Condition: CondRSIAbove, Value: 70, Period: 14, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 100},
			clear:    provider.Quote{Symbol: "BTC", Price: 100},
			prices: func(b bool) []float64 {
				if b {
					return rsiSeries(40, 1) // all gains -> RSI 100
				}
				return flat(40, 100) // flat -> RSI 50
			},
		},
		{
			name:     "rsi_below",
			rule:     Rule{Symbol: "BTC", Condition: CondRSIBelow, Value: 30, Period: 14, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 100},
			clear:    provider.Quote{Symbol: "BTC", Price: 100},
			prices: func(b bool) []float64 {
				if b {
					return rsiSeries(40, -1) // all losses -> RSI 0
				}
				return flat(40, 100)
			},
		},
		{
			name:     "stddev_above",
			rule:     Rule{Symbol: "BTC", Condition: CondStddevAbove, Value: 5, Period: 20, Enabled: true},
			breached: provider.Quote{Symbol: "BTC", Price: 100},
			clear:    provider.Quote{Symbol: "BTC", Price: 100},
			prices: func(b bool) []float64 {
				if b {
					return []float64{100, 110, 90, 120, 80, 130, 70, 140, 60, 150, 50, 160, 40, 170, 30, 180, 20, 190, 10, 200}
				}
				return flat(20, 100)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, fired := newCaptureEngine(time.Millisecond)
			ps := &pricesStub{}
			if tc.prices != nil {
				ps.set(tc.prices(true))
				e.SetPriceSource(ps)
			}
			e.AddRule(tc.rule)

			// The very first evaluation is already breached — it must only
			// establish the baseline.
			e.Check(tc.breached)
			if len(*fired) != 0 {
				t.Fatalf("establishing evaluation must not fire, got %d", len(*fired))
			}

			// Still breached: no transition, still silent.
			e.Check(tc.breached)
			if len(*fired) != 0 {
				t.Fatalf("holding the condition must not re-fire, got %d", len(*fired))
			}

			// Leave the condition, then re-enter it: that is the transition.
			if tc.prices != nil {
				ps.set(tc.prices(false))
			}
			e.Check(tc.clear)
			if len(*fired) != 0 {
				t.Fatalf("clearing the condition must not fire, got %d", len(*fired))
			}

			time.Sleep(2 * time.Millisecond) // clear the cooldown window
			if tc.prices != nil {
				ps.set(tc.prices(true))
			}
			e.Check(tc.breached)
			if len(*fired) != 1 {
				t.Fatalf("transition into the condition must fire, got %d", len(*fired))
			}
		})
	}
}

// TestSeededAlertsStaySilentOnFreshStart is the reported symptom: every
// shipped default alert evaluated against an already-breached market must
// produce zero notifications on the first quotes.
func TestSeededAlertsStaySilentOnFreshStart(t *testing.T) {
	e, fired := newCaptureEngine(5 * time.Minute)
	e.SetPriceSource(&pricesStub{vals: rsiSeries(60, 2)})
	e.SetRules([]Rule{
		{Symbol: "BTC-USD", Condition: CondAbove, Value: 100000, Enabled: true},
		{Symbol: "BTC-USD", Condition: CondBelow, Value: 200000, Enabled: true},
		{Symbol: "BTC-USD", Condition: CondRSIAbove, Value: 70, Period: 14, Enabled: true},
		{Symbol: "BTC-USD", Condition: CondPctUp, Value: 5, Enabled: true},
	})

	for i := 0; i < 5; i++ {
		e.Check(provider.Quote{Symbol: "BTC-USD", Price: 150000, Change: 20000})
	}
	if len(*fired) != 0 {
		t.Fatalf("already-breached seeded alerts must not fire on startup, got %d", len(*fired))
	}
}

// TestCrossConditionsAreNotDoubleGated verifies sma_cross is left alone:
// it is edge-based by construction and must fire on the first crossing it
// sees, with no baseline quote required.
func TestCrossConditionsAreNotDoubleGated(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	// Twenty flat samples then a jump — the last sample crosses above the
	// SMA(20) while the previous one sat on it.
	prices := append(flat(20, 100), 200)
	e.SetPriceSource(&pricesStub{vals: prices})
	e.AddRule(Rule{Symbol: "BTC", Condition: CondSMACrossAbove, Period: 20, Enabled: true})

	e.Check(provider.Quote{Symbol: "BTC", Price: 200})
	if len(*fired) != 1 {
		t.Fatalf("sma_cross should fire on the crossing itself, got %d", len(*fired))
	}
}

// TestEdgeBaselineTracksDuringCooldown documents the interaction: the
// baseline is kept current while a rule is muted, so the mute window
// swallows transitions instead of deferring them.
func TestEdgeBaselineTracksDuringCooldown(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})

	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("expected first crossing to fire, got %d", len(*fired))
	}
	// Oscillate hard inside the cooldown window.
	for i := 0; i < 10; i++ {
		e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
		e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	}
	if len(*fired) != 1 {
		t.Fatalf("cooldown must swallow the oscillation, got %d", len(*fired))
	}
}

func TestPctUsesSessionPreviousClose(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{Symbol: "AAPL", Condition: CondPctUp, Value: 5, Enabled: true})

	// mkt starts mid-session with the stock already up 1% on the day.
	e.Check(provider.Quote{Symbol: "AAPL", Price: 101, Change: 1})
	if len(*fired) != 0 {
		t.Fatalf("baseline quote should not fire, got %d", len(*fired))
	}

	// A 4% move off that first-seen price is only 5% off the previous
	// close — measuring from the session close is what makes this correct.
	e.Check(provider.Quote{Symbol: "AAPL", Price: 105, Change: 5})
	if len(*fired) != 1 {
		t.Fatalf("expected fire measured from previous close, got %d", len(*fired))
	}
	if got := (*fired)[0].Message; got == "" || !strings.Contains(got, "previous close") {
		t.Errorf("message should name the baseline, got %q", got)
	}
}

func TestPctFallsBackToFirstSeenWithoutSessionData(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{Symbol: "BTC-USD", Condition: CondPctUp, Value: 5, Enabled: true})

	// No Change / ChangePct — nothing to derive a session close from.
	e.Check(provider.Quote{Symbol: "BTC-USD", Price: 100})
	e.Check(provider.Quote{Symbol: "BTC-USD", Price: 106})
	if len(*fired) != 1 {
		t.Fatalf("expected fire measured from first seen, got %d", len(*fired))
	}
	if got := (*fired)[0].Message; !strings.Contains(got, "first seen") {
		t.Errorf("message should name the fallback baseline, got %q", got)
	}
}

func TestSessionBaselineDerivation(t *testing.T) {
	cases := []struct {
		name string
		q    provider.Quote
		want float64
		ok   bool
	}{
		{"from change", provider.Quote{Price: 105, Change: 5}, 100, true},
		{"from change pct", provider.Quote{Price: 105, ChangePct: 5}, 100, true},
		{"change wins", provider.Quote{Price: 105, Change: 5, ChangePct: 50}, 100, true},
		{"no session data", provider.Quote{Price: 105}, 0, false},
		{"zero price", provider.Quote{Price: 0, Change: 5}, 0, false},
		{"change exceeds price", provider.Quote{Price: 5, Change: 10}, 0, false},
		{"pct wipeout", provider.Quote{Price: 5, ChangePct: -100}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sessionBaseline(tc.q)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && !almost(got, tc.want) {
				t.Errorf("baseline = %v, want %v", got, tc.want)
			}
		})
	}
}

func almost(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}
