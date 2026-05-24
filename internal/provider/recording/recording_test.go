package recording

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// fakeProvider is a deterministic QuoteProvider that emits a fixed slice
// of quotes and returns.
type fakeProvider struct {
	name   string
	quotes []provider.Quote
}

func (f *fakeProvider) Name() string           { return f.name }
func (f *fakeProvider) Supports(_ string) bool { return true }

func (f *fakeProvider) Subscribe(ctx context.Context, _ []string, out chan<- provider.Quote) error {
	for _, q := range f.quotes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- q:
		}
	}
	return nil
}

func TestRecordReplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.ndjson")

	now := time.Now().Round(time.Nanosecond)
	inputs := []provider.Quote{
		{Symbol: "BTC-USD", Price: 50000, Volume: 1.5, Asset: provider.AssetCrypto, Provider: "coinbase", Timestamp: now},
		{Symbol: "ETH-USD", Price: 3000, Volume: 2.0, Asset: provider.AssetCrypto, Provider: "coinbase", Timestamp: now.Add(100 * time.Millisecond)},
		{Symbol: "AAPL", Price: 200, Bid: 199.5, Ask: 200.5, Asset: provider.AssetStock, Provider: "yahoo", Timestamp: now.Add(200 * time.Millisecond)},
	}

	// --- Record ---
	sink, err := NewSink(path)
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	rec := New(&fakeProvider{name: "fake", quotes: inputs}, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan provider.Quote, len(inputs))
	subDone := make(chan struct{})
	go func() {
		_ = rec.Subscribe(ctx, nil, out)
		close(subDone)
	}()

	var forwarded []provider.Quote
	for range inputs {
		select {
		case q := <-out:
			forwarded = append(forwarded, q)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for forwarded quote")
		}
	}
	<-subDone
	if err := sink.Close(); err != nil {
		t.Fatalf("sink close: %v", err)
	}

	if len(forwarded) != len(inputs) {
		t.Fatalf("forwarded %d, want %d", len(forwarded), len(inputs))
	}
	for i, q := range forwarded {
		if q.Symbol != inputs[i].Symbol || q.Price != inputs[i].Price {
			t.Errorf("forwarded[%d]: got %+v, want %+v", i, q, inputs[i])
		}
	}

	// --- File content ---
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("file has %d lines, want %d", len(lines), len(inputs))
	}
	if !strings.Contains(lines[0], `"v":1`) {
		t.Errorf("first line missing schema version: %s", lines[0])
	}

	// --- Replay ---
	rep := NewReplay(path, ModeBurst)
	out2 := make(chan provider.Quote, len(inputs))
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	repDone := make(chan struct{})
	go func() {
		_ = rep.Subscribe(ctx2, nil, out2)
		close(repDone)
	}()

	var replayed []provider.Quote
	for range inputs {
		select {
		case q := <-out2:
			replayed = append(replayed, q)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for replayed quote")
		}
	}
	<-repDone

	for i, got := range replayed {
		want := inputs[i]
		if got.Symbol != want.Symbol {
			t.Errorf("replay[%d] symbol: got %q want %q", i, got.Symbol, want.Symbol)
		}
		if got.Price != want.Price {
			t.Errorf("replay[%d] price: got %v want %v", i, got.Price, want.Price)
		}
		if got.Volume != want.Volume {
			t.Errorf("replay[%d] volume: got %v want %v", i, got.Volume, want.Volume)
		}
		if got.Bid != want.Bid || got.Ask != want.Ask {
			t.Errorf("replay[%d] bid/ask: got %v/%v want %v/%v", i, got.Bid, got.Ask, want.Bid, want.Ask)
		}
		if got.Asset != want.Asset {
			t.Errorf("replay[%d] asset: got %v want %v", i, got.Asset, want.Asset)
		}
		if got.Provider != want.Provider {
			t.Errorf("replay[%d] provider: got %q want %q", i, got.Provider, want.Provider)
		}
		if !got.Timestamp.Equal(want.Timestamp) {
			t.Errorf("replay[%d] timestamp: got %v want %v", i, got.Timestamp, want.Timestamp)
		}
	}
}

func TestRecordingNameAndSupports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.ndjson")
	sink, err := NewSink(path)
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close()

	rec := New(&fakeProvider{name: "inner"}, sink)
	if got, want := rec.Name(), "recording(inner)"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if !rec.Supports("ANYTHING") {
		t.Error("Supports should delegate to inner")
	}
}

func TestSinkSharedByTwoRecordings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.ndjson")
	sink, err := NewSink(path)
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}

	now := time.Now()
	cbQuotes := []provider.Quote{
		{Symbol: "BTC-USD", Price: 50000, Asset: provider.AssetCrypto, Provider: "coinbase", Timestamp: now},
		{Symbol: "ETH-USD", Price: 3000, Asset: provider.AssetCrypto, Provider: "coinbase", Timestamp: now.Add(50 * time.Millisecond)},
	}
	yhQuotes := []provider.Quote{
		{Symbol: "AAPL", Price: 200, Asset: provider.AssetStock, Provider: "yahoo", Timestamp: now.Add(25 * time.Millisecond)},
	}

	cbRec := New(&fakeProvider{name: "cb", quotes: cbQuotes}, sink)
	yhRec := New(&fakeProvider{name: "yh", quotes: yhQuotes}, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan provider.Quote, 10)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = cbRec.Subscribe(ctx, nil, out)
	}()
	go func() {
		defer wg.Done()
		_ = yhRec.Subscribe(ctx, nil, out)
	}()

	want := len(cbQuotes) + len(yhQuotes)
	var seen []provider.Quote
	for len(seen) < want {
		select {
		case q := <-out:
			seen = append(seen, q)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout: got %d/%d quotes", len(seen), want)
		}
	}
	wg.Wait()
	if err := sink.Close(); err != nil {
		t.Fatalf("sink close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != want {
		t.Fatalf("file has %d lines, want %d (concurrent writes lost)", len(lines), want)
	}
}
