package alert

import (
	"sync"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// fakeClock is a manually advanced time source.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestSetClockDrivesCooldown(t *testing.T) {
	e, fired := newCaptureEngine(5 * time.Minute)
	clk := &fakeClock{t: time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)}
	e.SetClock(clk.now)
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})

	// Quotes carry no timestamp, so the injected clock is the only source
	// of time. Cross, then re-cross inside the cooldown window.
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("first crossing should fire, got %d", len(*fired))
	}
	if got := (*fired)[0].Timestamp; !got.Equal(clk.now()) {
		t.Errorf("timestamp = %v, want the injected clock %v", got, clk.now())
	}

	clk.advance(time.Minute)
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 1 {
		t.Fatalf("cooldown must hold at +1m, got %d", len(*fired))
	}

	clk.advance(10 * time.Minute)
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if len(*fired) != 2 {
		t.Fatalf("cooldown must expire at +11m, got %d", len(*fired))
	}
}

func TestSetClockNilRestoresWallTime(t *testing.T) {
	e := NewEngine(time.Minute, nil)
	e.SetClock(func() time.Time { return time.Unix(0, 0) })
	e.SetClock(nil)

	e.mu.RLock()
	defer e.mu.RUnlock()
	if got := e.now(); time.Since(got) > time.Minute {
		t.Fatalf("clock = %v, expected wall time", got)
	}
}

// TestBurstReplayHonorsRecordedTime is the reported backtest defect: a
// recording replayed in milliseconds must be evaluated in recorded time,
// so four distinct crossings spread over seven hours report four fires
// against a five-minute cooldown — not one.
func TestBurstReplayHonorsRecordedTime(t *testing.T) {
	e, fired := newCaptureEngine(5 * time.Minute)
	e.AddRule(Rule{Symbol: "BTC-USD", Condition: CondAbove, Value: 50000, Enabled: true})

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	prices := []float64{49000, 51000, 49000, 51000, 49000, 51000, 49000, 51000}
	for i, p := range prices {
		e.Check(provider.Quote{
			Symbol:    "BTC-USD",
			Price:     p,
			Timestamp: start.Add(time.Duration(i) * time.Hour),
		})
	}

	if len(*fired) != 4 {
		t.Fatalf("expected 4 fires across 7 recorded hours, got %d", len(*fired))
	}
	for i, a := range *fired {
		want := start.Add(time.Duration(2*i+1) * time.Hour)
		if !a.Timestamp.Equal(want) {
			t.Errorf("fire %d timestamp = %v, want recorded %v", i, a.Timestamp, want)
		}
	}
}

// TestQuoteTimestampBeatsClock pins the precedence: a quote that knows
// when it happened wins over the engine clock.
func TestQuoteTimestampBeatsClock(t *testing.T) {
	e, fired := newCaptureEngine(time.Hour)
	e.SetClock(func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) })
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 100, Enabled: true})

	recorded := time.Date(2026, 3, 1, 15, 4, 5, 0, time.UTC)
	e.Check(provider.Quote{Symbol: "BTC", Price: 50, Timestamp: recorded})
	e.Check(provider.Quote{Symbol: "BTC", Price: 150, Timestamp: recorded.Add(time.Minute)})

	if len(*fired) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(*fired))
	}
	if got, want := (*fired)[0].Timestamp, recorded.Add(time.Minute); !got.Equal(want) {
		t.Errorf("timestamp = %v, want %v", got, want)
	}
}
