package alert

import (
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func newCaptureEngine(cooldown time.Duration) (*Engine, *[]TriggeredAlert) {
	var fired []TriggeredAlert
	e := NewEngine(cooldown, func(a TriggeredAlert) {
		fired = append(fired, a)
	})
	return e, &fired
}

func TestLegacyRuleStillFires(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("legacy rule should fire, got %d", len(*fired))
	}
}

func TestCompoundAllFiresWhenAllSubsMet(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{
		Symbol:  "BTC",
		Enabled: true,
		Match:   MatchAll,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 50000},
			{Type: CondAbove, Value: 60000},
		},
	})

	// First quote satisfies only sub[0]
	e.Check(provider.Quote{Symbol: "BTC", Price: 55000})
	if len(*fired) != 0 {
		t.Fatalf("should not fire when only one sub met, got %d", len(*fired))
	}

	// Second quote satisfies both
	e.Check(provider.Quote{Symbol: "BTC", Price: 65000})
	if len(*fired) != 1 {
		t.Fatalf("should fire when all subs met, got %d", len(*fired))
	}
	if got := (*fired)[0].Message; got == "" {
		t.Errorf("expected message, got empty")
	}
}

func TestCompoundAllPersistsAcrossQuotes(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{
		Symbol:  "BTC",
		Enabled: true,
		Match:   MatchAll,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 50000},
			{Type: CondBelow, Value: 40000},
		},
	})

	// Cross above 50k — sub[0] fires, state persists
	e.Check(provider.Quote{Symbol: "BTC", Price: 55000})
	if len(*fired) != 0 {
		t.Fatalf("should not fire yet, got %d", len(*fired))
	}

	// Drop below 40k — sub[1] fires; both flags set; rule fires
	e.Check(provider.Quote{Symbol: "BTC", Price: 35000})
	if len(*fired) != 1 {
		t.Fatalf("expected fire after both subs met across quotes, got %d", len(*fired))
	}
}

func TestCompoundAnyFiresOnFirstMatch(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{
		Symbol:  "BTC",
		Enabled: true,
		Match:   MatchAny,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 50000},
			{Type: CondBelow, Value: 40000},
		},
	})

	e.Check(provider.Quote{Symbol: "BTC", Price: 55000})
	if len(*fired) != 1 {
		t.Fatalf("any should fire on first match, got %d", len(*fired))
	}
}

func TestCompoundSequenceFiresInOrder(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{
		Symbol:  "BTC",
		Enabled: true,
		Match:   MatchSequence,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 50000},
			{Type: CondBelow, Value: 40000},
		},
	})

	// Step 1: cross above 50k
	e.Check(provider.Quote{Symbol: "BTC", Price: 55000})
	if len(*fired) != 0 {
		t.Fatalf("step 1 alone should not fire, got %d", len(*fired))
	}

	// Step 2: drop below 40k
	e.Check(provider.Quote{Symbol: "BTC", Price: 35000})
	if len(*fired) != 1 {
		t.Fatalf("sequence should fire after step 2, got %d", len(*fired))
	}
}

func TestCompoundSequenceIgnoresOutOfOrderMatches(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.AddRule(Rule{
		Symbol:  "BTC",
		Enabled: true,
		Match:   MatchSequence,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 50000}, // step 1
			{Type: CondBelow, Value: 40000}, // step 2
		},
	})

	// Quote satisfies step 2 first — should NOT advance because nextIdx is 0
	e.Check(provider.Quote{Symbol: "BTC", Price: 35000})
	if len(*fired) != 0 {
		t.Fatalf("out-of-order match should not fire, got %d", len(*fired))
	}

	// Now satisfy step 1
	e.Check(provider.Quote{Symbol: "BTC", Price: 55000})
	if len(*fired) != 0 {
		t.Fatalf("should not fire after step 1 alone, got %d", len(*fired))
	}

	// Now satisfy step 2 in order
	e.Check(provider.Quote{Symbol: "BTC", Price: 35000})
	if len(*fired) != 1 {
		t.Fatalf("expected fire after in-order completion, got %d", len(*fired))
	}
}

func TestCompoundFireResetsState(t *testing.T) {
	e, fired := newCaptureEngine(1 * time.Millisecond) // short cooldown for test speed
	e.AddRule(Rule{
		Symbol:  "BTC",
		Enabled: true,
		Match:   MatchAll,
		Conditions: []SubCondition{
			{Type: CondAbove, Value: 50000},
		},
	})

	// First fire
	e.Check(provider.Quote{Symbol: "BTC", Price: 55000})
	if len(*fired) != 1 {
		t.Fatalf("first fire expected, got %d", len(*fired))
	}

	// Wait for cooldown
	time.Sleep(5 * time.Millisecond)

	// Should fire again because state was cleared on fire
	e.Check(provider.Quote{Symbol: "BTC", Price: 56000})
	if len(*fired) != 2 {
		t.Fatalf("second fire expected after cooldown, got %d", len(*fired))
	}
}

func TestIsCompound(t *testing.T) {
	if (Rule{Condition: CondAbove}).IsCompound() {
		t.Error("legacy rule should not be compound")
	}
	if !(Rule{Conditions: []SubCondition{{Type: CondAbove}}}).IsCompound() {
		t.Error("rule with conditions should be compound")
	}
}
