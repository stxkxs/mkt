package alert

import (
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestEvaluateIndicatorNeedsTwoSamples(t *testing.T) {
	r := Rule{Symbol: "BTC", Condition: CondRSIAbove, Value: 70, Period: 14}
	for _, prices := range [][]float64{nil, {100}} {
		if fires, _ := evaluateIndicator(r, prices); fires {
			t.Fatalf("must not fire on %d samples", len(prices))
		}
	}
}

func TestSMACrossBelowFires(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	// Flat at 100 so the previous sample sits on the SMA, then a break down.
	e.SetPriceSource(&pricesStub{vals: append(flat(20, 100), 50)})
	e.AddRule(Rule{Symbol: "BTC", Condition: CondSMACrossBelow, Period: 20, Enabled: true})

	e.Check(provider.Quote{Symbol: "BTC", Price: 50})
	if len(*fired) != 1 {
		t.Fatalf("expected a downside cross, got %d", len(*fired))
	}
	if msg := (*fired)[0].Message; !strings.Contains(msg, "below SMA(20)") {
		t.Errorf("message = %q", msg)
	}
}

func TestRSIBelowFiresOnTransition(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	ps := &pricesStub{vals: flat(40, 100)} // RSI 50
	e.SetPriceSource(ps)
	e.AddRule(Rule{Symbol: "COIN", Condition: CondRSIBelow, Value: 30, Period: 14, Enabled: true})

	e.Check(provider.Quote{Symbol: "COIN", Price: 100})
	if len(*fired) != 0 {
		t.Fatalf("neutral RSI should not fire, got %d", len(*fired))
	}

	ps.set(rsiSeries(40, -1)) // all losses -> RSI 0
	e.Check(provider.Quote{Symbol: "COIN", Price: 60})
	if len(*fired) != 1 {
		t.Fatalf("expected fire when RSI drops below 30, got %d", len(*fired))
	}
	if msg := (*fired)[0].Message; !strings.Contains(msg, "RSI(14)") {
		t.Errorf("message = %q", msg)
	}
}

// TestMACDCrossFiresOnceOnReversal walks a decline-then-rally series one
// sample at a time and counts the bullish crossovers the rule reports.
func TestMACDCrossFiresOnceOnReversal(t *testing.T) {
	series := make([]float64, 0, 70)
	p := 200.0
	for i := 0; i < 45; i++ { // sustained decline
		series = append(series, p)
		p -= 2
	}
	for i := 0; i < 25; i++ { // sharp rally
		series = append(series, p)
		p += 5
	}

	r := Rule{Symbol: "NVDA", Condition: CondMACDCross, Enabled: true}
	var bullish, bearish int
	for n := macdMinSamples; n <= len(series); n++ {
		fires, msg := evaluateIndicator(r, series[:n])
		if !fires {
			continue
		}
		switch {
		case strings.Contains(msg, "bullish"):
			bullish++
		case strings.Contains(msg, "bearish"):
			bearish++
		default:
			t.Fatalf("unexpected macd message %q", msg)
		}
	}
	if bullish != 1 {
		t.Errorf("expected exactly 1 bullish crossover on the reversal, got %d", bullish)
	}
	if bearish != 0 {
		t.Errorf("expected no bearish crossover, got %d", bearish)
	}
}

func TestMACDCrossNeedsWarmup(t *testing.T) {
	r := Rule{Symbol: "NVDA", Condition: CondMACDCross}
	if fires, _ := evaluateIndicator(r, rsiSeries(macdMinSamples-1, 1)); fires {
		t.Fatal("macd must not fire before its warm-up completes")
	}
}

func TestCompoundWithIndicatorSubCondition(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	ps := &pricesStub{vals: flat(40, 100)}
	e.SetPriceSource(ps)
	e.AddRule(Rule{
		Symbol:  "NVDA",
		Enabled: true,
		Match:   MatchAll,
		Conditions: []SubCondition{
			{Type: CondRSIBelow, Value: 40, Period: 14},
			{Type: CondAbove, Value: 150},
		},
	})

	// Price leg latches, RSI leg does not.
	e.Check(provider.Quote{Symbol: "NVDA", Price: 160})
	if len(*fired) != 0 {
		t.Fatalf("only one leg met, got %d", len(*fired))
	}

	ps.set(rsiSeries(40, -1)) // RSI collapses
	e.Check(provider.Quote{Symbol: "NVDA", Price: 160})
	if len(*fired) != 1 {
		t.Fatalf("both legs met should fire, got %d", len(*fired))
	}
}

func TestCompoundShortHistoryIsSurfaced(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.SetPriceSource(&pricesStub{vals: flat(5, 100)})
	var warned []RuleStatus
	e.SetOnShortHistory(func(st RuleStatus) { warned = append(warned, st) })
	e.AddRule(Rule{
		Symbol:  "NVDA",
		Enabled: true,
		Match:   MatchAll,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 150},
			{Type: CondRSIBelow, Value: 40, Period: 14},
		},
	})

	e.Check(provider.Quote{Symbol: "NVDA", Price: 160})
	if len(*fired) != 0 {
		t.Fatalf("a compound rule that cannot evaluate must not fire, got %d", len(*fired))
	}
	if len(warned) != 1 || warned[0].Need != defaultRSIPeriod+1 {
		t.Fatalf("expected one 15-sample warning, got %+v", warned)
	}
}

func TestEmptyCompoundNeverFires(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	// Conditions is empty, so the rule is not compound and Condition is
	// unset — nothing can match it.
	e.AddRule(Rule{Symbol: "BTC", Enabled: true})
	e.Check(provider.Quote{Symbol: "BTC", Price: 100})
	e.Check(provider.Quote{Symbol: "BTC", Price: 200})
	if len(*fired) != 0 {
		t.Fatalf("a rule with no condition must never fire, got %d", len(*fired))
	}
}
