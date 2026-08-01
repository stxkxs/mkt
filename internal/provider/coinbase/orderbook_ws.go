package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// orderBookThrottle is the minimum interval between consecutive
// OrderBook snapshots emitted to the consumer. Coinbase's level2 channel
// can fire many updates per second on busy products; this keeps the TUI
// responsive without dropping signal.
const orderBookThrottle = 250 * time.Millisecond

// l2Message is the Advanced Trade WS envelope for the level2 channel.
type l2Message struct {
	Channel  string    `json:"channel"`
	Sequence int64     `json:"sequence_num"`
	Events   []l2Event `json:"events"`
}

type l2Event struct {
	Type      string     `json:"type"` // "snapshot" or "update"
	ProductID string     `json:"product_id"`
	Updates   []l2Update `json:"updates"`
}

type l2Update struct {
	Side        string `json:"side"` // "bid" or "offer"
	PriceLevel  string `json:"price_level"`
	NewQuantity string `json:"new_quantity"`
}

// OrderBookStatus reports a connection transition on a
// StreamOrderBookLoop stream so the UI can mark a frozen book as stale
// instead of rendering it as live.
type OrderBookStatus struct {
	ProductID string        // canonical Coinbase product ID
	Connected bool          // true once the level2 subscription is live
	Err       error         // why the last stream ended; nil when Connected
	At        time.Time     // when the transition happened
	Retry     time.Duration // wait before the next attempt; 0 when Connected
}

// StreamOrderBookLoop maintains a level2 subscription for productID for as
// long as ctx lives, reconnecting with the same jittered exponential
// backoff the ticker stream uses. This is the entry point UIs should use:
// the single-shot StreamOrderBook returns on the first dropped socket, and
// a caller that discards that error is left showing a book frozen at
// whatever the last update happened to be, forever, with no indication.
//
// Book snapshots go to out (throttled to orderBookThrottle) and connection
// transitions go to status. Both are non-blocking sends so a slow consumer
// can never stall the reconnect loop — buffer status with room for at
// least a couple of transitions. status may be nil to opt out.
//
// Returns only when ctx is cancelled, with ctx.Err(); stream failures are
// reported on status and retried.
func (p *Provider) StreamOrderBookLoop(ctx context.Context, productID string, out chan<- OrderBook, status chan<- OrderBookStatus) error {
	productID = toCoinbaseSymbol(productID)
	return orderBookLoop(ctx, productID, newBackoff(), status,
		func(ctx context.Context, onConnected func()) error {
			return p.streamOrderBook(ctx, productID, out, onConnected)
		})
}

// orderBookLoop drives attempt in a reconnect loop, publishing transitions
// on status. attempt must synchronously invoke the onConnected callback it
// is handed as soon as its subscription is live. Split out from
// StreamOrderBookLoop, and taking the backoff as a parameter, so the retry
// policy is testable without a WebSocket.
func orderBookLoop(ctx context.Context, productID string, b *backoff, status chan<- OrderBookStatus, attempt func(context.Context, func()) error) error {
	for {
		var established time.Time
		err := attempt(ctx, func() {
			established = time.Now()
			sendStatus(status, OrderBookStatus{ProductID: productID, Connected: true, At: established})
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wsReconnects.Inc()

		delay := b.observe(established, time.Now())
		sendStatus(status, OrderBookStatus{ProductID: productID, Err: err, At: time.Now(), Retry: delay})

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// sendStatus publishes a transition without ever blocking the reconnect
// loop. A consumer that isn't draining loses the transition, not the
// stream; the next failure re-publishes the disconnected state.
func sendStatus(ch chan<- OrderBookStatus, s OrderBookStatus) {
	if ch == nil {
		return
	}
	select {
	case ch <- s:
	default:
	}
}

// StreamOrderBook opens a WebSocket connection subscribed to the
// `level2` channel for productID, maintains the book in memory by
// applying snapshot + updates, and emits the current OrderBook on out
// at most every orderBookThrottle. Returns when ctx is cancelled or
// the connection errors out — caller restarts as needed.
//
// Prefer StreamOrderBookLoop, which does that restarting and reports
// staleness; this is the single-shot primitive underneath it.
//
// out should be buffered; full sends are dropped (the next update will
// catch up).
func (p *Provider) StreamOrderBook(ctx context.Context, productID string, out chan<- OrderBook) error {
	return p.streamOrderBook(ctx, toCoinbaseSymbol(productID), out, nil)
}

// streamOrderBook is StreamOrderBook with an establishment hook. productID
// must already be canonical. onConnected, if non-nil, is called on this
// goroutine once the level2 subscribe has been written.
func (p *Provider) streamOrderBook(ctx context.Context, productID string, out chan<- OrderBook, onConnected func()) error {
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("orderbook ws dial: %w", err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(1 << 24)

	sub := subscribeMsg{Type: "subscribe", ProductIDs: []string{productID}, Channel: "level2"}
	subData, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("orderbook marshal subscribe: %w", err)
	}
	if err := ws.Write(ctx, websocket.MessageText, subData); err != nil {
		return fmt.Errorf("orderbook write subscribe: %w", err)
	}

	if onConnected != nil {
		onConnected()
	}

	// Probe liveness so a stalled level2 stream reconnects instead of
	// blocking Read forever. Book updates are event-driven and can be
	// quiet on an illiquid product, so a data-gap timeout would false-
	// positive; an active ping does not.
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go keepAlive(connCtx, ws, cancel)

	bids := map[float64]float64{}
	asks := map[float64]float64{}
	var lastSent time.Time
	var lastSeq int64

	for {
		_, data, err := ws.Read(connCtx)
		if err != nil {
			return fmt.Errorf("orderbook read: %w", err)
		}
		var msg l2Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Channel != "l2_data" {
			continue
		}
		lastSeq = msg.Sequence
		changed := applyL2(bids, asks, msg.Events)
		if !changed {
			continue
		}
		if time.Since(lastSent) < orderBookThrottle {
			continue
		}
		book := buildBook(productID, lastSeq, bids, asks)
		select {
		case out <- book:
			lastSent = time.Now()
		case <-connCtx.Done():
			return connCtx.Err()
		default:
			// consumer slow; drop this snapshot
		}
	}
}

// applyL2 mutates bids/asks in place per the event list and returns
// whether any change occurred. snapshot events reset the books before
// applying their updates; update events upsert price levels (qty == 0
// removes the level).
func applyL2(bids, asks map[float64]float64, events []l2Event) bool {
	var changed bool
	for _, ev := range events {
		if ev.Type == "snapshot" {
			for k := range bids {
				delete(bids, k)
			}
			for k := range asks {
				delete(asks, k)
			}
		}
		for _, u := range ev.Updates {
			price, err := strconv.ParseFloat(u.PriceLevel, 64)
			if err != nil {
				continue
			}
			qty, err := strconv.ParseFloat(u.NewQuantity, 64)
			if err != nil {
				continue
			}
			target := bids
			if u.Side == "offer" || u.Side == "ask" {
				target = asks
			}
			if qty == 0 {
				delete(target, price)
			} else {
				target[price] = qty
			}
			changed = true
		}
	}
	return changed
}

// buildBook materializes the bid/ask maps into a sorted OrderBook.
func buildBook(productID string, seq int64, bids, asks map[float64]float64) OrderBook {
	bookBids := make([]Level, 0, len(bids))
	for p, s := range bids {
		bookBids = append(bookBids, Level{Price: p, Size: s})
	}
	sort.Slice(bookBids, func(i, j int) bool { return bookBids[i].Price > bookBids[j].Price })

	bookAsks := make([]Level, 0, len(asks))
	for p, s := range asks {
		bookAsks = append(bookAsks, Level{Price: p, Size: s})
	}
	sort.Slice(bookAsks, func(i, j int) bool { return bookAsks[i].Price < bookAsks[j].Price })

	return OrderBook{
		ProductID: productID,
		Sequence:  seq,
		Bids:      bookBids,
		Asks:      bookAsks,
	}
}
