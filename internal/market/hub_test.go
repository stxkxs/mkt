package market

import (
	"context"
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
}

func (f *fakeProvider) Name() string                { return f.name }
func (f *fakeProvider) Supports(symbol string) bool { return symbol == f.sym }
func (f *fakeProvider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
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
	close(block)
}
