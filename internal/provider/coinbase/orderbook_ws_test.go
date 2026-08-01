package coinbase

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fastBackoff keeps the reconnect policy shape but shrinks the delays so
// the loop can be exercised in milliseconds.
func fastBackoff(stable time.Duration) *backoff {
	return &backoff{cur: time.Millisecond, min: time.Millisecond, max: 8 * time.Millisecond, stable: stable}
}

// drainStatus collects everything buffered on ch without blocking.
func drainStatus(ch chan OrderBookStatus) []OrderBookStatus {
	var out []OrderBookStatus
	for {
		select {
		case s := <-ch:
			out = append(out, s)
		default:
			return out
		}
	}
}

func TestOrderBookLoopRetriesAndReportsStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	status := make(chan OrderBookStatus, 32)
	boom := errors.New("socket closed")
	attempts := 0

	err := orderBookLoop(ctx, "BTC-USD", fastBackoff(time.Hour), status,
		func(ctx context.Context, onConnected func()) error {
			attempts++
			onConnected()
			if attempts >= 4 {
				cancel()
				return ctx.Err()
			}
			return boom
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loop returned %v, want context.Canceled", err)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4 — a dropped socket must be retried, not swallowed", attempts)
	}

	got := drainStatus(status)
	var connected, disconnected int
	for _, s := range got {
		if s.ProductID != "BTC-USD" {
			t.Errorf("status carries %q, want BTC-USD", s.ProductID)
		}
		if s.Connected {
			connected++
			if s.Err != nil || s.Retry != 0 {
				t.Errorf("connected status should be clean: %+v", s)
			}
			continue
		}
		disconnected++
		if !errors.Is(s.Err, boom) {
			t.Errorf("disconnected status should carry the stream error, got %v", s.Err)
		}
		if s.Retry <= 0 {
			t.Errorf("disconnected status should advertise the retry delay, got %v", s.Retry)
		}
		if s.At.IsZero() {
			t.Error("status should be timestamped")
		}
	}
	if connected != 4 {
		t.Errorf("connected transitions = %d, want 4", connected)
	}
	if disconnected != 3 {
		t.Errorf("disconnected transitions = %d, want 3", disconnected)
	}
}

func TestOrderBookLoopBacksOffThenResets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// stable=0 makes every established session count as a recovery, which
	// is what a long-lived healthy stream looks like in production.
	b := fastBackoff(0)
	attempts := 0
	err := orderBookLoop(ctx, "ETH-USD", b, nil,
		func(ctx context.Context, onConnected func()) error {
			attempts++
			if attempts < 3 {
				// Never got a subscription up: keep backing off.
				return errors.New("dial refused")
			}
			if attempts == 3 {
				onConnected()
				return errors.New("dropped after a healthy run")
			}
			cancel()
			return ctx.Err()
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loop returned %v, want context.Canceled", err)
	}
	// Two failed dials doubled the delay to 4ms; the healthy session reset
	// it to 1ms and then doubled once more.
	if b.cur != 2*time.Millisecond {
		t.Errorf("backoff after recovery = %v, want 2ms", b.cur)
	}
}

func TestOrderBookLoopStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := orderBookLoop(ctx, "BTC-USD", fastBackoff(time.Hour), nil,
		func(ctx context.Context, onConnected func()) error {
			calls++
			return ctx.Err()
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loop returned %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("attempts = %d, want 1 on an already-cancelled context", calls)
	}
}

func TestSendStatusNeverBlocks(t *testing.T) {
	full := make(chan OrderBookStatus, 1)
	full <- OrderBookStatus{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sendStatus(full, OrderBookStatus{ProductID: "BTC-USD"})
		sendStatus(nil, OrderBookStatus{ProductID: "BTC-USD"})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendStatus blocked; a slow UI must not stall the reconnect loop")
	}
}

func TestApplyL2Snapshot(t *testing.T) {
	bids := map[float64]float64{}
	asks := map[float64]float64{}
	events := []l2Event{{
		Type: "snapshot",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "1.0"},
			{Side: "bid", PriceLevel: "99.50", NewQuantity: "2.0"},
			{Side: "offer", PriceLevel: "100.50", NewQuantity: "1.5"},
		},
	}}
	if !applyL2(bids, asks, events) {
		t.Fatal("expected snapshot to register as changed")
	}
	if len(bids) != 2 || len(asks) != 1 {
		t.Fatalf("counts: bids=%d asks=%d", len(bids), len(asks))
	}
	if bids[100] != 1.0 || bids[99.5] != 2.0 || asks[100.5] != 1.5 {
		t.Errorf("contents: %+v %+v", bids, asks)
	}
}

func TestApplyL2UpdateUpsertAndDelete(t *testing.T) {
	bids := map[float64]float64{100.0: 1.0, 99.5: 2.0}
	asks := map[float64]float64{100.5: 1.5}
	events := []l2Event{{
		Type: "update",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "5.0"}, // upsert
			{Side: "bid", PriceLevel: "99.50", NewQuantity: "0"},    // delete
			{Side: "bid", PriceLevel: "99.00", NewQuantity: "3.0"},  // new
			{Side: "offer", PriceLevel: "100.50", NewQuantity: "0"}, // delete
		},
	}}
	if !applyL2(bids, asks, events) {
		t.Fatal("expected changes")
	}
	if bids[100] != 5.0 {
		t.Errorf("upsert: bids[100]=%v want 5", bids[100])
	}
	if _, exists := bids[99.5]; exists {
		t.Errorf("delete failed: bids[99.5] still present")
	}
	if bids[99] != 3.0 {
		t.Errorf("new level missing: bids[99]=%v want 3", bids[99])
	}
	if len(asks) != 0 {
		t.Errorf("ask delete failed: %+v", asks)
	}
}

func TestApplyL2SnapshotResetsState(t *testing.T) {
	bids := map[float64]float64{50.0: 99.0}
	asks := map[float64]float64{51.0: 99.0}
	events := []l2Event{{
		Type: "snapshot",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "1.0"},
		},
	}}
	_ = applyL2(bids, asks, events)
	if _, exists := bids[50]; exists {
		t.Errorf("snapshot didn't reset old bid")
	}
	if _, exists := asks[51]; exists {
		t.Errorf("snapshot didn't reset old ask")
	}
	if bids[100] != 1.0 {
		t.Errorf("new snapshot data missing")
	}
}

func TestApplyL2InvalidValuesSkipped(t *testing.T) {
	bids := map[float64]float64{}
	asks := map[float64]float64{}
	events := []l2Event{{
		Type: "update",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "abc", NewQuantity: "1.0"},    // bad price
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "xyz"}, // bad qty
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "1.0"}, // good
		},
	}}
	_ = applyL2(bids, asks, events)
	if len(bids) != 1 || bids[100] != 1.0 {
		t.Errorf("good update should remain: %+v", bids)
	}
}

func TestBuildBookSorted(t *testing.T) {
	bids := map[float64]float64{100: 1, 99: 2, 101: 3}
	asks := map[float64]float64{102: 1, 103: 2, 101.5: 0.5}
	book := buildBook("BTC-USD", 42, bids, asks)
	if book.Sequence != 42 || book.ProductID != "BTC-USD" {
		t.Errorf("header wrong: %+v", book)
	}
	// Bids descending
	if book.Bids[0].Price != 101 || book.Bids[2].Price != 99 {
		t.Errorf("bids not sorted desc: %+v", book.Bids)
	}
	// Asks ascending
	if book.Asks[0].Price != 101.5 || book.Asks[2].Price != 103 {
		t.Errorf("asks not sorted asc: %+v", book.Asks)
	}
}
