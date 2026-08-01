package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// blockingNotifier parks inside Notify until release is closed, and
// announces each entry on entered.
type blockingNotifier struct {
	name    string
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	seen    int
}

func newBlockingNotifier(name string) *blockingNotifier {
	return &blockingNotifier{
		name:    name,
		entered: make(chan struct{}, 1024),
		release: make(chan struct{}),
	}
}

func (b *blockingNotifier) Name() string { return b.name }

func (b *blockingNotifier) Notify(ctx context.Context, _ TriggeredAlert) error {
	b.mu.Lock()
	b.seen++
	b.mu.Unlock()
	select {
	case b.entered <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil
}

// TestCheckDoesNotBlockOnSlowNotifier is the head-of-line defect: the
// fan-out used to run serially on the hub dispatcher goroutine, so a
// webhook burning its five-second timeout stalled quote delivery for the
// whole app.
func TestCheckDoesNotBlockOnSlowNotifier(t *testing.T) {
	e := NewEngine(time.Millisecond, nil)
	slow := newBlockingNotifier("slow")
	defer close(slow.release)
	e.AddNotifier(slow)
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})

	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	start := time.Now()
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Check blocked for %v on a wedged notifier", elapsed)
	}

	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("notifier was never called")
	}
	slow.mu.Lock()
	defer slow.mu.Unlock()
	if slow.seen != 1 {
		t.Fatalf("notifier saw %d alerts, want 1", slow.seen)
	}
}

// TestWedgedNotifierDoesNotDelaySibling — each destination has its own
// queue and goroutine, so one stuck webhook cannot starve the others.
func TestWedgedNotifierDoesNotDelaySibling(t *testing.T) {
	e := NewEngine(time.Millisecond, nil)
	wedged := newBlockingNotifier("wedged")
	defer close(wedged.release)
	healthy := &recordingNotifier{name: "healthy"}
	e.AddNotifier(wedged)
	e.AddNotifier(healthy)

	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	e.Check(provider.Quote{Symbol: "BTC", Price: 51000})

	deadline := time.After(2 * time.Second)
	for healthy.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("healthy notifier never received the alert")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// TestCheckFanOutIsRateLimited: the per-notifier limiter used to apply
// only on the Inject path, despite the comment promising it capped
// rule-triggered alerts too.
func TestCheckFanOutIsRateLimited(t *testing.T) {
	e := NewEngine(time.Minute, nil)
	n := &recordingNotifier{name: "n"}
	e.AddNotifier(n)

	// An advancing clock retires the cooldown between crossings so every
	// transition is a genuine fire.
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	e.SetClock(clk.now)
	e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: 50000, Enabled: true})

	const crossings = notifierBurst + 5
	e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	for i := 0; i < crossings; i++ {
		clk.advance(time.Hour)
		e.Check(provider.Quote{Symbol: "BTC", Price: 51000})
		e.Check(provider.Quote{Symbol: "BTC", Price: 49000})
	}
	if !e.Flush(2 * time.Second) {
		t.Fatal("notifier queue did not drain")
	}

	if got := n.count(); got != notifierBurst {
		t.Fatalf("delivered %d alerts, want the burst cap of %d", got, notifierBurst)
	}
	if got, want := e.NotifyDrops(), uint64(crossings-notifierBurst); got != want {
		t.Fatalf("NotifyDrops = %d, want %d", got, want)
	}
}

// TestFullQueueDropsRatherThanBlocking pins the back-pressure policy.
func TestFullQueueDropsRatherThanBlocking(t *testing.T) {
	e := NewEngine(time.Minute, nil)
	wedged := newBlockingNotifier("wedged")
	defer close(wedged.release)
	e.AddNotifier(wedged)

	// First alert parks the pump inside Notify.
	e.Inject(sampleAlert())
	select {
	case <-wedged.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("notifier was never called")
	}

	// Fill the queue exactly, then overflow it.
	for i := 0; i < notifierQueueDepth; i++ {
		e.Inject(sampleAlert())
	}
	if got := e.NotifyDrops(); got != 0 {
		t.Fatalf("a queue filled to capacity must not drop, got %d", got)
	}
	const overflow = 5
	for i := 0; i < overflow; i++ {
		e.Inject(sampleAlert())
	}
	if got := e.NotifyDrops(); got != overflow {
		t.Fatalf("NotifyDrops = %d, want %d", got, overflow)
	}
}

func TestInjectDeliversToEveryNotifier(t *testing.T) {
	var got []TriggeredAlert
	e := NewEngine(time.Minute, func(a TriggeredAlert) { got = append(got, a) })
	n1 := &recordingNotifier{name: "n1"}
	n2 := &recordingNotifier{name: "n2"}
	e.AddNotifier(n1)
	e.AddNotifier(n2)

	e.Inject(sampleAlert())
	if !e.Flush(2 * time.Second) {
		t.Fatal("notifier queues did not drain")
	}
	if len(got) != 1 {
		t.Fatalf("onAlert called %d times, want 1", len(got))
	}
	if n1.count() != 1 || n2.count() != 1 {
		t.Fatalf("notifier counts = %d / %d, want 1 / 1", n1.count(), n2.count())
	}
}

func TestFlushReportsFailureWhenWedged(t *testing.T) {
	e := NewEngine(time.Minute, nil)
	wedged := newBlockingNotifier("wedged")
	defer close(wedged.release)
	e.AddNotifier(wedged)

	e.Inject(sampleAlert())
	if e.Flush(50 * time.Millisecond) {
		t.Fatal("Flush should report failure while a notifier is parked")
	}
}

// TestConcurrentCheckAndMutation exercises the lock discipline under
// -race: quotes arriving while the TUI adds, toggles and deletes rules.
func TestConcurrentCheckAndMutation(t *testing.T) {
	e := NewEngine(time.Millisecond, func(TriggeredAlert) {})
	e.SetPriceSource(&pricesStub{vals: flat(60, 100)})
	e.AddNotifier(&recordingNotifier{name: "n"})
	e.SetOnRulesChanged(func([]Rule) {})
	e.SetOnShortHistory(func(RuleStatus) {})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					e.Check(provider.Quote{Symbol: "BTC", Price: 51000, Timestamp: time.Now()})
					e.Check(provider.Quote{Symbol: "BTC", Price: 49000, Timestamp: time.Now()})
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		e.AddRule(Rule{Symbol: "BTC", Condition: CondAbove, Value: float64(50000 + i), Enabled: true})
		_ = e.Statuses()
		e.ToggleRule(0)
		e.RemoveRule(0)
	}
	close(stop)
	wg.Wait()
}
