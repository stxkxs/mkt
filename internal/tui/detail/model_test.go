package detail

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// recorder stands in for *tea.Program.
type recorder struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (r *recorder) Send(msg tea.Msg) {
	r.mu.Lock()
	r.msgs = append(r.msgs, msg)
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

// fakeBook is a bookSource whose stream stays up until its context is
// cancelled, so a test can observe teardown.
type fakeBook struct {
	started chan string
	stopped chan struct{}
	once    sync.Once
}

func newFakeBook() *fakeBook {
	return &fakeBook{started: make(chan string, 4), stopped: make(chan struct{})}
}

func (f *fakeBook) FetchOrderBook(context.Context, string) (coinbase.OrderBook, error) {
	return coinbase.OrderBook{ProductID: "BTC-USD"}, nil
}

func (f *fakeBook) StreamOrderBookLoop(ctx context.Context, productID string, out chan<- coinbase.OrderBook, status chan<- coinbase.OrderBookStatus) error {
	f.started <- productID
	status <- coinbase.OrderBookStatus{ProductID: productID, Connected: true, At: time.Now()}
	out <- coinbase.OrderBook{
		ProductID: productID,
		Bids:      []coinbase.Level{{Price: 100, Size: 1}},
		Asks:      []coinbase.Level{{Price: 101, Size: 2}},
	}
	<-ctx.Done()
	f.once.Do(func() { close(f.stopped) })
	return ctx.Err()
}

func newTestModel(t *testing.T, src bookSource) Model {
	t.Helper()
	rec := &recorder{}
	SetLiveProgram(rec)
	t.Cleanup(func() { SetLiveProgram(nil) })
	m := Model{cache: market.NewCache(60), cb: src}
	m.SetSize(100, 30)
	return m
}

func waitStopped(t *testing.T, f *fakeBook) {
	t.Helper()
	select {
	case <-f.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("level2 stream was never cancelled")
	}
}

func waitStarted(t *testing.T, f *fakeBook) string {
	t.Helper()
	select {
	case id := <-f.started:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("level2 stream never started")
		return ""
	}
}

// esc only flipped active to false; the stream and its keepalive
// goroutine stayed up, one per open/close cycle.
func TestEscCancelsTheStream(t *testing.T) {
	f := newFakeBook()
	m := newTestModel(t, f)
	m.SetSymbol("BTC-USD")
	m.SetActive(true)
	waitStarted(t, f)

	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "esc"})
	if m.active {
		t.Error("esc did not deactivate the panel")
	}
	waitStopped(t, f)
}

func TestSetActiveFalseCancelsTheStream(t *testing.T) {
	f := newFakeBook()
	m := newTestModel(t, f)
	m.SetSymbol("BTC-USD")
	waitStarted(t, f)

	m.SetActive(false)
	waitStopped(t, f)
}

func TestSwitchingSymbolCancelsThePreviousStream(t *testing.T) {
	f := newFakeBook()
	m := newTestModel(t, f)
	m.SetSymbol("BTC-USD")
	if got := waitStarted(t, f); got != "BTC-USD" {
		t.Fatalf("stream started for %q", got)
	}
	m.SetSymbol("ETH-USD")
	waitStopped(t, f)
}

// An app-level cancel must reach the streamer; it used to hang off
// context.Background and outlive everything.
func TestParentContextCancelsTheStream(t *testing.T) {
	f := newFakeBook()
	m := newTestModel(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	m.SetContext(ctx)
	m.SetSymbol("BTC-USD")
	waitStarted(t, f)

	cancel()
	waitStopped(t, f)
}

func TestNonCryptoSymbolStartsNoStream(t *testing.T) {
	f := newFakeBook()
	m := newTestModel(t, f)
	if cmd := m.SetSymbol("AAPL"); cmd != nil {
		t.Error("expected no order-book command for a stock")
	}
	if m.liveSym != "" {
		t.Errorf("liveSym = %q, want empty", m.liveSym)
	}
	select {
	case id := <-f.started:
		t.Fatalf("stream started for stock %q", id)
	case <-time.After(100 * time.Millisecond):
	}
}

// A dropped socket used to freeze the book with nothing on screen to
// say so.
func TestStatusMessagesDriveTheStaleMarker(t *testing.T) {
	f := newFakeBook()
	m := newTestModel(t, f)
	m.SetSymbol("BTC-USD")
	m.UpdateQuote(provider.Quote{Symbol: "BTC-USD", Price: 100, ChangePct: 1})
	waitStarted(t, f)

	m, _ = m.Update(orderBookLoadedMsg{
		symbol: "BTC-USD",
		book: coinbase.OrderBook{
			ProductID: "BTC-USD",
			Bids:      []coinbase.Level{{Price: 100, Size: 1}},
		},
	})
	m, _ = m.Update(orderBookStatusMsg{
		symbol: "BTC-USD",
		status: coinbase.OrderBookStatus{ProductID: "BTC-USD", Connected: true},
	})
	if out := plain(m.View()); !strings.Contains(out, "live") {
		t.Errorf("connected book not marked live:\n%s", out)
	}

	m, _ = m.Update(orderBookStatusMsg{
		symbol: "BTC-USD",
		status: coinbase.OrderBookStatus{
			ProductID: "BTC-USD",
			Err:       errors.New("websocket: close 1006"),
			Retry:     4 * time.Second,
		},
	})
	out := plain(m.View())
	if !strings.Contains(out, "stale") {
		t.Errorf("dropped socket not marked stale:\n%s", out)
	}
	if !strings.Contains(out, "retrying in 4s") {
		t.Errorf("retry delay not surfaced:\n%s", out)
	}
	if !strings.Contains(out, "close 1006") {
		t.Errorf("stream error not surfaced:\n%s", out)
	}
}

// A message from a stream that has already been replaced must not
// repaint the book for a different symbol.
func TestStaleMessagesForOtherSymbolsAreDropped(t *testing.T) {
	m := newTestModel(t, newFakeBook())
	m.symbol = "ETH-USD"
	m.liveSym = "ETH-USD"
	m, _ = m.Update(orderBookLoadedMsg{
		symbol: "BTC-USD",
		book:   coinbase.OrderBook{ProductID: "BTC-USD", Bids: []coinbase.Level{{Price: 1, Size: 1}}},
	})
	if len(m.book.Bids) != 0 {
		t.Error("book from a previous symbol was applied")
	}
	m, _ = m.Update(orderBookStatusMsg{
		symbol: "BTC-USD",
		status: coinbase.OrderBookStatus{Connected: true},
	})
	if m.bookLive {
		t.Error("status from a previous symbol was applied")
	}
}

// liveProgram is written from the main goroutine at shutdown and read
// from every streamer goroutine. Run this under -race.
func TestSetLiveProgramIsRaceSafe(t *testing.T) {
	t.Cleanup(func() { SetLiveProgram(nil) })
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					sendLive(orderBookLoadedMsg{symbol: "BTC-USD"})
					_ = liveEnabled()
				}
			}
		}()
	}
	for range 200 {
		SetLiveProgram(&recorder{})
		SetLiveProgram(nil)
	}
	close(stop)
	wg.Wait()
}

// Once the program is deregistered the streamer must stop dispatching.
func TestSendLiveStopsAfterDeregistration(t *testing.T) {
	rec := &recorder{}
	SetLiveProgram(rec)
	sendLive(orderBookLoadedMsg{})
	SetLiveProgram(nil)
	sendLive(orderBookLoadedMsg{})
	if got := rec.count(); got != 1 {
		t.Errorf("recorder got %d messages, want 1", got)
	}
}

func TestNewIgnoresTypedNilProvider(t *testing.T) {
	m := New(market.NewCache(60), nil)
	if m.cb != nil {
		t.Error("a nil *coinbase.Provider must not become a non-nil interface")
	}
	if cmd := m.SetSymbol("BTC-USD"); cmd != nil {
		t.Error("expected no command without a provider")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	cache := market.NewCache(60)
	cache.Push(provider.Quote{Symbol: "BTC-USD", Price: 100, Timestamp: time.Now()})
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			m := Model{cache: cache}
			m.SetSize(w, h)
			m.symbol = "BTC-USD"
			m.liveSym = "BTC-USD"
			m.SetNotes(map[string]string{"btc-usd": "long term\nhold"})
			m.UpdateQuote(provider.Quote{Symbol: "BTC-USD", Price: 100, ChangePct: -1})
			m, _ = m.Update(orderBookLoadedMsg{
				symbol: "BTC-USD",
				book: coinbase.OrderBook{
					ProductID: "BTC-USD",
					Bids:      []coinbase.Level{{Price: 99, Size: 1}},
					Asks:      []coinbase.Level{{Price: 101, Size: 1}, {Price: 102, Size: 2}},
				},
			})
			_ = m.View()
			m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "esc"})
			_ = m.View()
		}
	}
}
