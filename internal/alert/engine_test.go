package alert

import (
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestCheckAbove(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Second, func(a TriggeredAlert) {
		fired = append(fired, a)
	})

	e.AddRule(Rule{Symbol: "BTCUSDT", Condition: CondAbove, Value: 50000, Enabled: true})

	// Below threshold — no alert
	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 49000})
	if len(fired) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(fired))
	}

	// Above threshold — alert fires
	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 51000})
	if len(fired) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(fired))
	}
	if fired[0].Price != 51000 {
		t.Errorf("expected price 51000, got %.2f", fired[0].Price)
	}
}

func TestCheckBelow(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Second, func(a TriggeredAlert) {
		fired = append(fired, a)
	})

	e.AddRule(Rule{Symbol: "ETHUSDT", Condition: CondBelow, Value: 2000, Enabled: true})

	e.Check(provider.Quote{Symbol: "ETHUSDT", Price: 2500})
	if len(fired) != 0 {
		t.Fatal("should not fire above threshold")
	}

	e.Check(provider.Quote{Symbol: "ETHUSDT", Price: 1900})
	if len(fired) != 1 {
		t.Fatal("should fire below threshold")
	}
}

func TestCooldown(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Hour, func(a TriggeredAlert) {
		fired = append(fired, a)
	})

	e.AddRule(Rule{Symbol: "BTCUSDT", Condition: CondAbove, Value: 50000, Enabled: true})

	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 51000})
	if len(fired) != 1 {
		t.Fatal("first check should fire")
	}

	// Second check within cooldown — should NOT fire
	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 52000})
	if len(fired) != 1 {
		t.Fatal("should not fire during cooldown")
	}
}

func TestDisabledRule(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Second, func(a TriggeredAlert) {
		fired = append(fired, a)
	})

	e.AddRule(Rule{Symbol: "BTCUSDT", Condition: CondAbove, Value: 50000, Enabled: false})
	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 51000})
	if len(fired) != 0 {
		t.Fatal("disabled rule should not fire")
	}
}
