package market

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

type fakeProvider struct {
	name    string
	sym     string
	emit    int
	started chan struct{}

	mu   sync.Mutex
	subs []string
}

func (f *fakeProvider) Name() string                { return f.name }
func (f *fakeProvider) Supports(symbol string) bool { return symbol == f.sym }
func (f *fakeProvider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	f.mu.Lock()
	f.subs = append([]string(nil), symbols...)
	f.mu.Unlock()
	close(f.started)
	for i := 0; i < f.emit; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- provider.Quote{Symbol: f.sym, Price: float64(i + 1), Timestamp: time.Now()}:
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeProvider) subscribed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.subs...)
}

func TestHubRoutesSymbolsToSupportingProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewCache(16)
	pA := &fakeProvider{name: "a", sym: "AAA", emit: 3, started: make(chan struct{})}
	pB := &fakeProvider{name: "b", sym: "BBB", emit: 3, started: make(chan struct{})}
	hub := NewHub(cache, pA, pB)

	var seen atomic.Int64
	hub.Start(ctx, []string{"AAA", "BBB", "ZZZ"}, func(q provider.Quote) {
		seen.Add(1)
	})

	// Wait for both providers to have started (only supported symbols routed).
	select {
	case <-pA.started:
	case <-time.After(time.Second):
		t.Fatal("provider A did not start")
	}
	select {
	case <-pB.started:
	case <-time.After(time.Second):
		t.Fatal("provider B did not start")
	}

	// Give dispatcher a moment to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && seen.Load() < 6 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := seen.Load(); got != 6 {
		t.Fatalf("onQuote called %d times, want 6", got)
	}

	// Cache received all quotes.
	if vals := cache.Prices("AAA"); len(vals) != 3 {
		t.Fatalf("cache AAA = %d, want 3", len(vals))
	}
}

func TestHubReportsUnroutableSymbols(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &fakeProvider{name: "a", sym: "AAPL", emit: 0, started: make(chan struct{})}
	hub := NewHub(NewCache(16), p)

	// "APPL" is a typo nothing supports; it used to vanish without a trace.
	bad := hub.Start(ctx, []string{"aapl", "APPL"}, nil)
	if len(bad) != 1 || bad[0] != "APPL" {
		t.Fatalf("Start unroutable = %v, want [APPL]", bad)
	}
	if got := hub.Unroutable(); len(got) != 1 || got[0] != "APPL" {
		t.Fatalf("Unroutable = %v, want [APPL]", got)
	}

	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	// The routable symbol reached the provider in canonical form.
	if subs := p.subscribed(); len(subs) != 1 || subs[0] != "AAPL" {
		t.Fatalf("subscribed = %v, want [AAPL]", subs)
	}
}

func TestHubCanonicalizesAndDedupesRoutedSymbols(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &fakeProvider{name: "cb", sym: "BTC-USD", emit: 0, started: make(chan struct{})}
	hub := NewHub(NewCache(16), p)

	if bad := hub.Start(ctx, []string{"btc", "BTCUSDT", "BTC-USD", ""}, nil); bad != nil {
		t.Fatalf("Start unroutable = %v, want none", bad)
	}
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	if subs := p.subscribed(); len(subs) != 1 || subs[0] != "BTC-USD" {
		t.Fatalf("subscribed = %v, want [BTC-USD] (three spellings of one market)", subs)
	}
}

// echoProvider emits quotes under whatever symbol the venue happens to use.
type echoProvider struct {
	emit    []provider.Quote
	started chan struct{}
}

func (e *echoProvider) Name() string         { return "echo" }
func (e *echoProvider) Supports(string) bool { return true }
func (e *echoProvider) Subscribe(ctx context.Context, _ []string, out chan<- provider.Quote) error {
	close(e.started)
	for _, q := range e.emit {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- q:
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestHubCanonicalizesIncomingQuoteSymbols(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewCache(16)
	p := &echoProvider{
		started: make(chan struct{}),
		emit: []provider.Quote{
			{Symbol: "BTC-USDT", Price: 100, Timestamp: time.Now()},
			{Symbol: "btc-usd", Price: 101, Timestamp: time.Now()},
			{Symbol: "aapl", Price: 200, Timestamp: time.Now()},
		},
	}
	hub := NewHub(cache, p)

	seen := make(chan string, 8)
	hub.AddObserver(func(q provider.Quote) { seen <- q.Symbol })
	hub.Start(ctx, []string{"BTC-USD", "AAPL"}, nil)

	got := make([]string, 0, 3)
	for range 3 {
		select {
		case s := <-seen:
			got = append(got, s)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out; got %v", got)
		}
	}
	want := []string{"BTC-USD", "BTC-USD", "AAPL"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("observed symbols = %v, want %v", got, want)
		}
	}

	// Both BTC spellings landed under one cache key.
	if prices := cache.Prices("BTC-USD"); len(prices) != 2 {
		t.Fatalf("cache BTC-USD = %v, want two prices under one key", prices)
	}
	if prices := cache.Prices("AAPL"); len(prices) != 1 {
		t.Fatalf("cache AAPL = %v, want one price", prices)
	}
}

func TestHubDropsWhenConsumerStalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewCache(1024)
	p := &fakeProvider{name: "a", sym: "AAA", emit: dispatchBuffer * 4, started: make(chan struct{})}
	hub := NewHub(cache, p)

	// onQuote blocks indefinitely; dispatch buffer will fill and new quotes must drop.
	block := make(chan struct{})
	hub.Start(ctx, []string{"AAA"}, func(q provider.Quote) {
		<-block
	})

	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}

	// Cache should still receive all pushes regardless of dispatch backpressure.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cache.Prices("AAA")) >= dispatchBuffer*4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(cache.Prices("AAA")); got < dispatchBuffer*4 {
		t.Fatalf("cache got %d quotes, want at least %d — provider was blocked by slow consumer", got, dispatchBuffer*4)
	}
	if got := hub.Drops(); got == 0 {
		t.Fatal("Drops = 0, want the stalled dispatch path to be counted")
	}
	close(block)
}

// TestObserversNeverMissAQuoteWhileDispatchDrops is the regression for alerts
// running on the lossy path: a spike that the UI drops must still be evaluated.
func TestObserversNeverMissAQuoteWhileDispatchDrops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const emit = dispatchBuffer * 4
	p := &fakeProvider{name: "a", sym: "AAA", emit: emit, started: make(chan struct{})}
	hub := NewHub(NewCache(emit*2), p)

	var observed atomic.Int64
	var sum atomic.Int64
	hub.AddObserver(func(q provider.Quote) {
		observed.Add(1)
		sum.Add(int64(q.Price))
	})

	block := make(chan struct{})
	hub.Start(ctx, []string{"AAA"}, func(q provider.Quote) { <-block })
	defer close(block)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && observed.Load() < emit {
		time.Sleep(10 * time.Millisecond)
	}
	if got := observed.Load(); got != emit {
		t.Fatalf("observer saw %d quotes, want all %d", got, emit)
	}
	// Prices are 1..emit, so the sum proves nothing was reordered away either.
	want := int64(emit) * int64(emit+1) / 2
	if got := sum.Load(); got != want {
		t.Fatalf("observed price sum = %d, want %d", got, want)
	}
	if hub.Drops() == 0 {
		t.Fatal("expected the dispatch path to have dropped quotes in this test")
	}
	if hub.ObserverDrops() != 0 {
		t.Fatalf("ObserverDrops = %d, want 0", hub.ObserverDrops())
	}
}

func TestWedgedObserverDoesNotStallIngestOrOtherObservers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const emit = 512
	cache := NewCache(emit * 2)
	p := &fakeProvider{name: "a", sym: "AAA", emit: emit, started: make(chan struct{})}
	hub := NewHub(cache, p)

	wedge := make(chan struct{})
	var wedged atomic.Int64
	hub.AddObserver(func(q provider.Quote) {
		wedged.Add(1)
		<-wedge
	})
	var healthy atomic.Int64
	hub.AddObserver(func(q provider.Quote) { healthy.Add(1) })

	hub.Start(ctx, []string{"AAA"}, nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && healthy.Load() < emit {
		time.Sleep(10 * time.Millisecond)
	}
	if got := healthy.Load(); got != emit {
		t.Fatalf("healthy observer saw %d, want %d — a wedged sibling blocked it", got, emit)
	}
	if got := len(cache.Prices("AAA")); got != emit {
		t.Fatalf("cache holds %d, want %d — the wedged observer stalled ingestion", got, emit)
	}
	if got := wedged.Load(); got != 1 {
		t.Fatalf("wedged observer entered fn %d times, want exactly 1 in flight", got)
	}
	if got := hub.ObserverBacklog(); got == 0 {
		t.Fatal("ObserverBacklog = 0, want the wedged observer's backlog to be visible")
	}
	close(wedge)
}

func TestAddObserverAfterShutdownIsNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &fakeProvider{name: "a", sym: "AAA", emit: 0, started: make(chan struct{})}
	hub := NewHub(NewCache(8), p)
	hub.Start(ctx, []string{"AAA"}, nil)

	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	cancel()

	// Wait for the reader loop to tear the observers down.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		stopped := hub.stopped
		hub.mu.RUnlock()
		if stopped {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	hub.AddObserver(func(provider.Quote) { t.Error("observer fired after shutdown") })
	hub.mu.RLock()
	n := len(hub.observers)
	hub.mu.RUnlock()
	if n != 0 {
		t.Fatalf("observers registered after shutdown = %d, want 0", n)
	}
}

func TestObserverBacklogOverflowDropsOldestAndCounts(t *testing.T) {
	var drops atomic.Uint64
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	delivered := make(chan float64, 8)
	o := newObserver(func(q provider.Quote) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		delivered <- q.Price
	}, &drops)
	o.max = 3
	go o.run()
	defer o.stop()

	// Park the pump inside fn so nothing drains while the queue fills.
	o.push(provider.Quote{Price: 1})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("pump never entered fn")
	}

	for _, p := range []float64{2, 3, 4, 5, 6} {
		o.push(provider.Quote{Price: p})
	}
	if got := o.pending(); got != 3 {
		t.Fatalf("pending = %d, want the queue capped at 3", got)
	}
	if got := drops.Load(); got != 2 {
		t.Fatalf("drops = %d, want 2 (oldest evicted)", got)
	}

	// The newest quotes survive: 2 and 3 were evicted, 4/5/6 remain.
	close(release)
	var got []float64
	for range 4 {
		select {
		case p := <-delivered:
			got = append(got, p)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out; delivered %v", got)
		}
	}
	want := []float64{1, 4, 5, 6}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivered = %v, want %v", got, want)
		}
	}
}

func TestIsShutdownFiltersContextErrors(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{context.Canceled, true},
		{context.DeadlineExceeded, true},
		{fmt.Errorf("websocket read: %w", context.Canceled), true},
		{errors.New("dial tcp: connection refused"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isShutdown(c.err); got != c.want {
			t.Errorf("isShutdown(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
