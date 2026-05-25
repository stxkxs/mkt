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

// StreamOrderBook opens a WebSocket connection subscribed to the
// `level2` channel for productID, maintains the book in memory by
// applying snapshot + updates, and emits the current OrderBook on out
// at most every orderBookThrottle. Returns when ctx is cancelled or
// the connection errors out — caller restarts as needed.
//
// out should be buffered; full sends are dropped (the next update will
// catch up).
func (p *Provider) StreamOrderBook(ctx context.Context, productID string, out chan<- OrderBook) error {
	productID = toCoinbaseSymbol(productID)
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

	bids := map[float64]float64{}
	asks := map[float64]float64{}
	var lastSent time.Time
	var lastSeq int64

	for {
		_, data, err := ws.Read(ctx)
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
		case <-ctx.Done():
			return ctx.Err()
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
