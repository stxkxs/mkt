package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	wsBaseURL    = "wss://stream.binance.com:9443/stream"
	reconnectMin = 1 * time.Second
	reconnectMax = 30 * time.Second
)

// conn wraps the Binance WebSocket connection with reconnection logic.
type conn struct {
	symbols []string
	out     chan<- combinedStreamMsg
	status  chan<- bool // true = connected, false = disconnected
}

// run connects and reads messages, reconnecting on failure.
func (c *conn) run(ctx context.Context) error {
	backoff := reconnectMin
	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("binance ws disconnected: %v, reconnecting in %v", err, backoff)
		c.notifyStatus(false)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, reconnectMax)
	}
}

func (c *conn) connect(ctx context.Context) error {
	// Build combined stream URL
	var streams []string
	for _, s := range c.symbols {
		streams = append(streams, strings.ToLower(s)+"@miniTicker")
	}
	url := wsBaseURL + "?streams=" + strings.Join(streams, "/")

	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer ws.CloseNow()

	// Set read limit to 1MB
	ws.SetReadLimit(1 << 20)

	c.notifyStatus(true)

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var msg combinedStreamMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		select {
		case c.out <- msg:
		case <-ctx.Done():
			ws.Close(websocket.StatusNormalClosure, "closing")
			return ctx.Err()
		}
	}
}

func (c *conn) notifyStatus(connected bool) {
	select {
	case c.status <- connected:
	default:
	}
}
