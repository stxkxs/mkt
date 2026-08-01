package broadcast

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type fakeSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (f *fakeSender) Send(msg tea.Msg) {
	f.mu.Lock()
	f.msgs = append(f.msgs, msg)
	f.mu.Unlock()
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func (f *fakeSender) received() []tea.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tea.Msg(nil), f.msgs...)
}

// blockingSender records every message then parks inside Send until the
// test releases it — a stand-in for an SSH session whose event loop has
// stalled.
type blockingSender struct {
	fakeSender
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

// newBlockingSender returns a sender that wedges on its first Send. The
// wedge is always released when the test ends so no goroutine outlives it.
func newBlockingSender(t *testing.T) *blockingSender {
	t.Helper()
	s := &blockingSender{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(s.unwedge)
	return s
}

func (s *blockingSender) Send(msg tea.Msg) {
	s.fakeSender.Send(msg)
	s.enterOnce.Do(func() { close(s.entered) })
	<-s.release
}

// unwedge lets every parked Send return. Safe to call more than once.
func (s *blockingSender) unwedge() {
	s.releaseOnce.Do(func() { close(s.release) })
}

// awaitEntry blocks until the sender is parked inside Send, which means
// the pump is busy and every later push lands in the queue.
func (s *blockingSender) awaitEntry(t *testing.T) {
	t.Helper()
	select {
	case <-s.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sender never entered Send")
	}
}

type msg struct{ n int }

const settle = 2 * time.Second

// waitFor polls cond until it holds or the test fails on timeout.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(settle)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// subscriberOf reaches into the registry so tests can observe a pump's
// lifetime; production code never needs this.
func subscriberOf(t *testing.T, b *Broadcaster, s Sender) *subscriber {
	t.Helper()
	b.mu.RLock()
	defer b.mu.RUnlock()
	sub, ok := b.subs[s]
	if !ok {
		t.Fatal("sender is not registered")
	}
	return sub
}

func awaitPumpExit(t *testing.T, sub *subscriber) {
	t.Helper()
	select {
	case <-sub.done:
	case <-time.After(settle):
		t.Fatal("pump goroutine did not exit")
	}
}

func TestBroadcastFansOutToAll(t *testing.T) {
	b := New()
	a, c := &fakeSender{}, &fakeSender{}
	b.Add(a)
	b.Add(c)
	if b.Len() != 2 {
		t.Fatalf("Len = %d, want 2", b.Len())
	}
	b.Send(msg{1})
	b.Send(msg{2})
	waitFor(t, "both senders to receive 2 messages", func() bool {
		return a.count() == 2 && c.count() == 2
	})
	if got := a.received(); got[0] != (msg{1}) || got[1] != (msg{2}) {
		t.Errorf("out of order: %v", got)
	}
}

func TestRemoveStopsDelivery(t *testing.T) {
	b := New()
	a := &fakeSender{}
	b.Add(a)
	b.Send(msg{1})
	// Delivery is asynchronous, so let the first message land before
	// removing — otherwise Remove would legitimately discard it.
	waitFor(t, "first message to arrive", func() bool { return a.count() == 1 })

	sub := subscriberOf(t, b, a)
	b.Remove(a)
	awaitPumpExit(t, sub)

	b.Send(msg{2})
	if a.count() != 1 {
		t.Errorf("got %d, want 1 (removed before second send)", a.count())
	}
	if b.Len() != 0 {
		t.Errorf("Len = %d, want 0", b.Len())
	}
}

func TestSendWithNoSendersIsNoop(t *testing.T) {
	b := New()
	b.Send(msg{1}) // must not panic
}

// A session wedged inside Send must not hold up anyone else: neither the
// caller of Send (the hub dispatcher) nor a healthy sibling session.
func TestWedgedSenderDoesNotDelayHealthySender(t *testing.T) {
	b := New()
	wedged := newBlockingSender(t)
	healthy := &fakeSender{}
	b.Add(wedged)
	b.Add(healthy)

	const n = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range n {
			b.Send(msg{i})
		}
	}()
	select {
	case <-done:
	case <-time.After(settle):
		t.Fatal("Send blocked on the wedged sender")
	}

	waitFor(t, "healthy sender to receive every message", func() bool {
		return healthy.count() == n
	})
	for i, m := range healthy.received() {
		if m != (msg{i}) {
			t.Fatalf("healthy sender got %v at index %d, want %v", m, i, msg{i})
		}
	}
	if got := wedged.count(); got != 1 {
		t.Errorf("wedged sender got %d messages, want 1 (the one in flight)", got)
	}
	if got := b.Drops(); got != 0 {
		t.Errorf("Drops = %d, want 0 (the default queue absorbs %d messages)", got, n)
	}
}

// Overflow is charged to the wedged sender alone; a sender that keeps
// draining never loses a message no matter how far behind its neighbor is.
func TestWedgedSenderDropsOnlyItsOwn(t *testing.T) {
	const queue = 4
	b := NewSized(queue)
	wedged := newBlockingSender(t)
	healthy := &fakeSender{}
	b.Add(wedged)
	b.Add(healthy)

	// Park the wedged sender's pump, then pace the remaining sends so the
	// healthy sender is always caught up and can never overflow.
	b.Send(msg{0})
	wedged.awaitEntry(t)
	waitFor(t, "healthy sender to catch up", func() bool { return healthy.count() == 1 })
	for i := 1; i <= queue+3; i++ {
		b.Send(msg{i})
		waitFor(t, "healthy sender to catch up", func() bool { return healthy.count() == i+1 })
	}

	if got := b.Drops(); got != 3 {
		t.Errorf("Drops = %d, want 3", got)
	}
	if got := b.DropsFor(healthy); got != 0 {
		t.Errorf("DropsFor(healthy) = %d, want 0", got)
	}
	if got := b.DropsFor(wedged); got != 3 {
		t.Errorf("DropsFor(wedged) = %d, want 3", got)
	}
}

// Overflow evicts the oldest queued message, so a session that catches up
// sees the freshest data rather than a stale backlog.
func TestFullQueueDropsOldest(t *testing.T) {
	const queue = 4
	b := NewSized(queue)
	s := newBlockingSender(t)
	b.Add(s)

	// Park the pump inside Send so the queue is empty and quiescent.
	b.Send(msg{0})
	s.awaitEntry(t)

	for i := 1; i <= queue+3; i++ {
		b.Send(msg{i})
	}
	if got := b.Drops(); got != 3 {
		t.Fatalf("Drops = %d, want 3", got)
	}

	s.unwedge()
	waitFor(t, "queued messages to drain", func() bool { return s.count() == queue+1 })
	want := []tea.Msg{msg{0}, msg{4}, msg{5}, msg{6}, msg{7}}
	got := s.received()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivered %v, want %v (oldest should be dropped)", got, want)
		}
	}
}

// Remove must reclaim the pump goroutine even for a sender that is still
// parked inside Send: no message is delivered after Remove, and the
// goroutine exits as soon as the stuck call returns.
func TestRemoveTerminatesWedgedPump(t *testing.T) {
	b := NewSized(2)
	s := newBlockingSender(t)
	b.Add(s)
	sub := subscriberOf(t, b, s)

	b.Send(msg{0})
	s.awaitEntry(t)
	b.Send(msg{1}) // queued behind the wedge

	b.Remove(s) // must not block on the wedged Send
	if b.Len() != 0 {
		t.Fatalf("Len = %d, want 0", b.Len())
	}

	s.unwedge()
	awaitPumpExit(t, sub)
	if got := s.count(); got != 1 {
		t.Errorf("sender got %d messages, want 1 (the queued one is discarded)", got)
	}
}

func TestDropsSurviveRemoval(t *testing.T) {
	b := NewSized(1)
	s := newBlockingSender(t)
	b.Add(s)

	b.Send(msg{0})
	s.awaitEntry(t)
	b.Send(msg{1})
	b.Send(msg{2}) // evicts msg{1}

	before := b.Drops()
	if before != 1 {
		t.Fatalf("Drops = %d, want 1", before)
	}
	b.Remove(s)
	if got := b.Drops(); got != before {
		t.Errorf("Drops = %d after Remove, want it to stay at %d", got, before)
	}
	if got := b.DropsFor(s); got != 0 {
		t.Errorf("DropsFor(removed) = %d, want 0", got)
	}
}

func TestAddIsIdempotent(t *testing.T) {
	b := New()
	s := &fakeSender{}
	b.Add(s)
	sub := subscriberOf(t, b, s)
	b.Add(s)
	if b.Len() != 1 {
		t.Fatalf("Len = %d, want 1", b.Len())
	}
	if subscriberOf(t, b, s) != sub {
		t.Error("re-adding replaced the existing subscriber")
	}
	b.Send(msg{1})
	waitFor(t, "one delivery", func() bool { return s.count() == 1 })
}

func TestRemoveUnknownSenderIsNoop(t *testing.T) {
	b := New()
	s := &fakeSender{}
	b.Remove(s) // never added
	b.Add(s)
	b.Remove(s)
	b.Remove(s) // second removal must be harmless
	if b.Len() != 0 {
		t.Errorf("Len = %d, want 0", b.Len())
	}
	if b.Drops() != 0 {
		t.Errorf("Drops = %d, want 0", b.Drops())
	}
}

func TestNewSizedFallsBackToDefault(t *testing.T) {
	b := NewSized(0)
	if b.queueSize != DefaultQueueSize {
		t.Errorf("queueSize = %d, want %d", b.queueSize, DefaultQueueSize)
	}
	s := &fakeSender{}
	b.Add(s)
	b.Send(msg{1})
	waitFor(t, "delivery", func() bool { return s.count() == 1 })
}

// White-box guard: Send and Remove are ordered by the registry lock, so a
// stopped subscriber never sees a push through the public API. The guard
// still has to hold — without it a late push would refill a discarded
// queue that no pump will ever drain.
func TestPushAfterStopIsDiscarded(t *testing.T) {
	sub := newSubscriber(&fakeSender{}, 2)
	sub.stop()
	sub.push(msg{1})

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.count != 0 {
		t.Errorf("queued %d messages after stop, want 0", sub.count)
	}
	if got := sub.drops.Load(); got != 0 {
		t.Errorf("drops = %d, want 0 (a stopped sender is not falling behind)", got)
	}
}

// Session churn races the data plane in `mkt serve`; the race detector
// should find nothing.
func TestConcurrentAddRemoveSend(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			b.Send(msg{i})
		}
	}()

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				s := &fakeSender{}
				b.Add(s)
				b.Send(msg{-1})
				_ = b.Len()
				_ = b.Drops()
				_ = b.DropsFor(s)
				b.Remove(s)
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
	if b.Len() != 0 {
		t.Errorf("Len = %d, want 0", b.Len())
	}
}
