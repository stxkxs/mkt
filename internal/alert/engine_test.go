package alert

import (
	"context"
	"errors"
	"sync"
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

// pricesStub satisfies PriceSource for indicator-condition tests.
type pricesStub struct{ vals []float64 }

func (p pricesStub) Prices(string) []float64 { return p.vals }

func TestVolumeAboveFires(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Second, func(a TriggeredAlert) {
		fired = append(fired, a)
	})
	e.AddRule(Rule{Symbol: "BTC", Condition: CondVolumeAbove, Value: 1_000_000, Enabled: true})

	e.Check(provider.Quote{Symbol: "BTC", Price: 50000, Volume: 500_000})
	if len(fired) != 0 {
		t.Fatalf("low volume should not fire, got %d", len(fired))
	}

	e.Check(provider.Quote{Symbol: "BTC", Price: 50000, Volume: 2_000_000})
	if len(fired) != 1 {
		t.Fatalf("high volume should fire, got %d", len(fired))
	}
}

func TestStddevAboveFires(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Second, func(a TriggeredAlert) {
		fired = append(fired, a)
	})
	// Highly variable price series (stddev/mean ratio > 5%)
	prices := []float64{100, 110, 90, 120, 80, 130, 70, 140, 60, 150, 50, 160, 40, 170, 30, 180, 20, 190, 10, 200}
	e.SetPriceSource(pricesStub{vals: prices})
	e.AddRule(Rule{
		Symbol:    "BTC",
		Condition: CondStddevAbove,
		Value:     5, // 5% of mean
		Period:    20,
		Enabled:   true,
	})

	e.Check(provider.Quote{Symbol: "BTC", Price: 200})
	if len(fired) != 1 {
		t.Fatalf("high stddev should fire, got %d", len(fired))
	}
}

func TestStddevAboveDoesNotFireOnFlat(t *testing.T) {
	var fired []TriggeredAlert
	e := NewEngine(1*time.Second, func(a TriggeredAlert) {
		fired = append(fired, a)
	})
	// Flat price series — zero stddev
	prices := make([]float64, 25)
	for i := range prices {
		prices[i] = 100
	}
	e.SetPriceSource(pricesStub{vals: prices})
	e.AddRule(Rule{
		Symbol:    "BTC",
		Condition: CondStddevAbove,
		Value:     1,
		Period:    20,
		Enabled:   true,
	})

	e.Check(provider.Quote{Symbol: "BTC", Price: 100})
	if len(fired) != 0 {
		t.Fatalf("flat series should not fire stddev, got %d", len(fired))
	}
}

// recordingNotifier collects every alert it sees and optionally returns
// a fixed error from Notify.
type recordingNotifier struct {
	name string
	mu   sync.Mutex
	seen []TriggeredAlert
	err  error
}

func (r *recordingNotifier) Name() string { return r.name }

func (r *recordingNotifier) Notify(_ context.Context, a TriggeredAlert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, a)
	return r.err
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

func TestNotifierFanOut(t *testing.T) {
	e := NewEngine(1*time.Second, nil)
	n1 := &recordingNotifier{name: "n1"}
	n2 := &recordingNotifier{name: "n2"}
	e.AddNotifier(n1)
	e.AddNotifier(n2)

	e.AddRule(Rule{Symbol: "BTCUSDT", Condition: CondAbove, Value: 50000, Enabled: true})
	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 51000})

	if got := n1.count(); got != 1 {
		t.Fatalf("n1: expected 1 alert, got %d", got)
	}
	if got := n2.count(); got != 1 {
		t.Fatalf("n2: expected 1 alert, got %d", got)
	}
}

func TestNotifierErrorIsolation(t *testing.T) {
	e := NewEngine(1*time.Second, nil)
	failing := &recordingNotifier{name: "failing", err: errors.New("boom")}
	ok := &recordingNotifier{name: "ok"}
	e.AddNotifier(failing)
	e.AddNotifier(ok)

	e.AddRule(Rule{Symbol: "BTCUSDT", Condition: CondAbove, Value: 50000, Enabled: true})
	e.Check(provider.Quote{Symbol: "BTCUSDT", Price: 51000})

	if got := failing.count(); got != 1 {
		t.Fatalf("failing: expected 1 alert (call still attempted), got %d", got)
	}
	if got := ok.count(); got != 1 {
		t.Fatalf("ok: expected 1 alert (sibling failure must not block), got %d", got)
	}
}
