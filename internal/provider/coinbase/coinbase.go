package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/stxkxs/mkt/internal/httpx"
	"github.com/stxkxs/mkt/internal/observe"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/symbol"
)

// Provider-level health counters surfaced on /metrics.
var wsReconnects = observe.NewCounter("mkt_provider_coinbase_ws_reconnects_total")

const (
	wsURL        = "wss://advanced-trade-ws.coinbase.com"
	reconnectMin = 1 * time.Second
	reconnectMax = 30 * time.Second
	// reconnectJitter randomizes up to ±this fraction of the current
	// backoff to avoid synchronized reconnect storms when many clients
	// see the same disconnect (e.g. a regional WS outage).
	reconnectJitter = 0.3
	// reconnectStable is how long a connection has to stay up before it
	// counts as a genuine recovery rather than a flap. Resetting the
	// backoff on any successful dial would let a server that accepts then
	// immediately drops us spin at reconnectMin forever; requiring a
	// stable session means real recoveries reset the delay and flapping
	// ones keep backing off.
	reconnectStable = 60 * time.Second
	// WS liveness: actively ping the server on this cadence and give the
	// pong this long to arrive. A silently half-open connection (no data,
	// no error — common through NAT/proxies/load balancers) blocks Read
	// forever; a failed ping round-trip cancels the read loop so the
	// existing reconnect logic kicks in.
	pingInterval = 15 * time.Second
	pingTimeout  = 10 * time.Second
	// maxCandlesPerRequest is the Coinbase Exchange per-request candle cap.
	maxCandlesPerRequest = 300
)

// restURL is the Coinbase Exchange REST base; a var so tests can point it
// at an httptest server.
var restURL = "https://api.exchange.coinbase.com"

// Provider implements QuoteProvider and HistoryProvider for Coinbase.
type Provider struct {
	statusCh chan bool
	client   *http.Client
}

// New creates a new Coinbase provider.
func New() *Provider {
	return &Provider{
		statusCh: make(chan bool, 1),
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) Name() string { return "coinbase" }

// Supports returns true for crypto symbols in Coinbase format (XXX-USD,
// XXX-USDT), Binance bare-pair format (XXXUSDT), or a known bare base
// (BTC, ETH, …). Classification is delegated to the shared symbol package
// so it can't drift from the other providers.
func (p *Provider) Supports(sym string) bool {
	return symbol.IsCrypto(sym)
}

// StatusChan returns a channel that receives connection status updates.
func (p *Provider) StatusChan() <-chan bool {
	return p.statusCh
}

// Subscribe connects to Coinbase WebSocket and streams quotes.
func (p *Provider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	ids := productIDs(symbols)

	b := newBackoff()
	for {
		established, err := p.connect(ctx, ids, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wsReconnects.Inc()
		p.notifyStatus(false)

		delay := b.observe(established, time.Now())
		log.Printf("coinbase ws disconnected: %v, reconnecting in %v", err, delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// productIDs normalizes a watchlist to Coinbase product IDs, preserving
// order and dropping empties and duplicates: several spellings collapse to
// the same product, and subscribing to one twice only doubles the inbound
// ticker rate.
func productIDs(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	seen := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		id := toCoinbaseSymbol(s)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// backoff is the reconnect delay state for a single stream. It exists
// because the delay used to be a bare local that only ever doubled: after
// a couple of early blips it pinned at reconnectMax for the lifetime of
// the process, so a connection that had been healthy for hours still
// waited 30s to come back. observe folds each finished session into the
// state and resets the delay when the session was genuinely healthy.
type backoff struct {
	cur    time.Duration // delay for the next attempt, before jitter
	min    time.Duration
	max    time.Duration
	stable time.Duration // session lifetime that counts as a real recovery
}

// newBackoff returns the reconnect policy shared by the ticker and level2
// streams.
func newBackoff() *backoff {
	return &backoff{cur: reconnectMin, min: reconnectMin, max: reconnectMax, stable: reconnectStable}
}

// observe folds one finished session into the backoff and returns how long
// to wait before retrying. established is when the session came up (zero
// if it never did) and now is the current time. A session that stayed up
// at least b.stable resets the delay to b.min; anything shorter keeps
// backing off toward b.max.
func (b *backoff) observe(established, now time.Time) time.Duration {
	if !established.IsZero() && now.Sub(established) >= b.stable {
		b.cur = b.min
	}
	d := jittered(b.cur)
	b.cur = min(b.cur*2, b.max)
	return d
}

// jittered returns d ± up to reconnectJitter of d, full-jitter style.
// Spreads reconnect attempts across the client fleet during a shared
// outage so we don't all hammer the WS endpoint in lockstep.
func jittered(d time.Duration) time.Duration {
	delta := float64(d) * reconnectJitter
	return d + time.Duration((rand.Float64()*2-1)*delta)
}

// connect dials, subscribes, and pumps quotes until the stream fails. It
// returns the instant the subscription went live — zero if the connection
// never got that far — so Subscribe can tell a real session from a
// dial-then-drop flap when deciding whether to reset the backoff.
func (p *Provider) connect(ctx context.Context, productIDs []string, out chan<- provider.Quote) (time.Time, error) {
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("dial: %w", err)
	}
	defer ws.CloseNow()

	ws.SetReadLimit(1 << 20)

	// Subscribe to ticker_batch channel (updates every 5s, less noisy than ticker)
	sub := subscribeMsg{
		Type:       "subscribe",
		ProductIDs: productIDs,
		Channel:    "ticker_batch",
	}
	subData, err := json.Marshal(sub)
	if err != nil {
		return time.Time{}, fmt.Errorf("marshal subscribe: %w", err)
	}
	if err := ws.Write(ctx, websocket.MessageText, subData); err != nil {
		return time.Time{}, fmt.Errorf("write subscribe: %w", err)
	}

	established := time.Now()
	p.notifyStatus(true)

	// Probe liveness in the background so a stalled-but-open stream is
	// detected and reconnected instead of blocking Read indefinitely.
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go keepAlive(connCtx, ws, cancel)

	for {
		_, data, err := ws.Read(connCtx)
		if err != nil {
			return established, fmt.Errorf("read: %w", err)
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		if msg.Channel != "ticker" && msg.Channel != "ticker_batch" {
			continue
		}

		for _, event := range msg.Events {
			for _, t := range event.Tickers {
				q, err := tickerToQuote(t)
				if err != nil {
					continue
				}
				select {
				case out <- q:
				case <-connCtx.Done():
					ws.Close(websocket.StatusNormalClosure, "closing")
					return established, connCtx.Err()
				}
			}
		}
	}
}

// keepAlive pings the server every pingInterval and gives each pong
// pingTimeout to arrive. A failed ping means the connection is dead (or
// half-open), so it cancels connCtx — unblocking the read loop, which then
// returns and lets Subscribe reconnect. This is the liveness check that a
// bare blocking Read cannot provide.
func keepAlive(ctx context.Context, ws *websocket.Conn, cancel context.CancelFunc) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, pcancel := context.WithTimeout(ctx, pingTimeout)
			err := ws.Ping(pingCtx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func (p *Provider) notifyStatus(connected bool) {
	select {
	case p.statusCh <- connected:
	default:
	}
}

func tickerToQuote(t wsTicker) (provider.Quote, error) {
	price, err := strconv.ParseFloat(t.Price, 64)
	if err != nil {
		return provider.Quote{}, err
	}
	vol, _ := strconv.ParseFloat(t.Volume24H, 64)
	high, _ := strconv.ParseFloat(t.High24H, 64)
	low, _ := strconv.ParseFloat(t.Low24H, 64)
	bid, _ := strconv.ParseFloat(t.BestBid, 64)
	ask, _ := strconv.ParseFloat(t.BestAsk, 64)
	pctChange, _ := strconv.ParseFloat(t.PricePercentChg24H, 64)

	// Calculate absolute change from percent
	var change float64
	if pctChange != 0 && price != 0 {
		// price = open * (1 + pctChange/100), so open = price / (1 + pctChange/100)
		open := price / (1 + pctChange/100)
		change = price - open
	}

	return provider.Quote{
		// Re-canonicalize rather than trusting the server echo: every
		// consumer (cache, alerts, portfolio) keys off Symbol, so it has
		// to be the same "<BASE>-USD" string symbol.Canonical produces
		// from user input no matter how Coinbase spells the product.
		Symbol:    toCoinbaseSymbol(t.ProductID),
		Price:     price,
		Change:    change,
		ChangePct: pctChange,
		Volume:    vol,
		High24h:   high,
		Low24h:    low,
		Bid:       bid,
		Ask:       ask,
		Asset:     provider.AssetCrypto,
		Provider:  "coinbase",
		Timestamp: time.Now(),
	}, nil
}

// quoteSuffixes are the quote-currency tails the fallback path collapses
// onto -USD, longest first so USDT/USDC are matched before the USD they
// end with.
var quoteSuffixes = []string{"USDT", "USDC", "BUSD", "USD"}

// toCoinbaseSymbol converts any spelling of a crypto symbol into the
// canonical Coinbase product ID "<BASE>-USD".
//
// For anything symbol.IsCrypto recognizes it defers to symbol.Canonical,
// which is what the watchlist, cache and alert engine key off. Deferring
// rather than re-deriving is the point: the shared package also applies
// ticker migrations (MATIC -> POL), and a private copy of the rules here
// would silently stream POL-USD prices under a MATIC-USD key.
//
// The fallback only covers symbols reaching the exported History /
// StreamOrderBook entry points directly, bypassing Supports; it folds a
// stablecoin quote currency onto USD so a hand-typed BTC-USDT still
// resolves to the one market mkt tracks per base asset.
func toCoinbaseSymbol(s string) string {
	if symbol.IsCrypto(s) {
		return symbol.Canonical(s)
	}
	base := strings.ToUpper(strings.TrimSpace(s))
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	} else {
		for _, q := range quoteSuffixes {
			if len(base) > len(q) && strings.HasSuffix(base, q) {
				base = strings.TrimSuffix(base, q)
				break
			}
		}
	}
	if base == "" {
		return ""
	}
	return base + "-USD"
}

// History fetches historical OHLCV from the Coinbase Exchange REST API and
// returns candles at exactly params.Interval, oldest first.
//
// Coinbase only serves 60/300/900/3600/21600/86400-second candles, so 4h
// and 1w have no native granularity. Rather than silently returning 1h
// candles under a "4h" label, History over-fetches at the nearest native
// granularity and aggregates the result into buckets of the requested
// interval (see aggregate). Callers can therefore label the series with
// the interval they asked for.
//
// params.Limit counts candles at the requested interval, not native ones.
// The Coinbase per-request cap of 300 native candles applies to the
// over-fetch, so a large Limit at 1w yields fewer buckets than asked for
// rather than an upstream error. The oldest bucket may cover a partial
// period when the fetch window does not begin on a bucket boundary.
//
// Coinbase candle format: [time, low, high, open, close, volume]
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	productID := toCoinbaseSymbol(params.Symbol)
	granularity := nativeGranularity(params.Interval)

	// How many native candles make up one candle at the requested interval.
	perBucket := int(intervalDuration(params.Interval) / (time.Duration(granularity) * time.Second))
	if perBucket < 1 {
		perBucket = 1
	}

	limit := params.Limit
	if limit == 0 {
		limit = 100
	}
	native := limit * perBucket
	if native > maxCandlesPerRequest {
		native = maxCandlesPerRequest
		limit = native / perBucket
	}

	end := time.Now()
	if !params.End.IsZero() {
		end = params.End
	}
	start := end.Add(-time.Duration(native*granularity) * time.Second)
	if !params.Start.IsZero() {
		start = params.Start
	}

	endpoint := fmt.Sprintf("%s/products/%s/candles?granularity=%d&start=%s&end=%s",
		restURL, url.PathEscape(productID), granularity,
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339))

	var raw [][]float64
	if err := httpx.GetJSON(ctx, p.client, endpoint, map[string]string{"User-Agent": "mkt/1.0"}, &raw); err != nil {
		return nil, fmt.Errorf("coinbase candles: %w", err)
	}

	// Coinbase returns newest first, reverse to chronological
	candles := make([]provider.OHLCV, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		r := raw[i]
		if len(r) < 6 {
			continue
		}
		candles = append(candles, provider.OHLCV{
			Time:   time.Unix(int64(r[0]), 0).UTC(),
			Low:    r[1],
			High:   r[2],
			Open:   r[3],
			Close:  r[4],
			Volume: r[5],
		})
	}
	if perBucket == 1 {
		return candles, nil
	}

	buckets := aggregate(candles, params.Interval)
	if len(buckets) > limit {
		buckets = buckets[len(buckets)-limit:]
	}
	return buckets, nil
}

// nativeGranularity returns the candle granularity, in seconds, that the
// Coinbase Exchange API will actually serve for i. Coinbase supports only
// 60/300/900/3600/21600/86400, so 4h is served from 1h candles and 1w from
// 1d candles; History aggregates those up before returning.
func nativeGranularity(i provider.Interval) int {
	switch i {
	case provider.Interval1m:
		return 60
	case provider.Interval5m:
		return 300
	case provider.Interval15m:
		return 900
	case provider.Interval1h, provider.Interval4h:
		return 3600
	case provider.Interval1d, provider.Interval1w:
		return 86400
	default:
		return 86400
	}
}

// intervalDuration is the wall-clock span of one candle History returns at
// interval i — as opposed to nativeGranularity, which is what Coinbase
// serves.
func intervalDuration(i provider.Interval) time.Duration {
	switch i {
	case provider.Interval1m:
		return time.Minute
	case provider.Interval5m:
		return 5 * time.Minute
	case provider.Interval15m:
		return 15 * time.Minute
	case provider.Interval1h:
		return time.Hour
	case provider.Interval4h:
		return 4 * time.Hour
	case provider.Interval1d:
		return 24 * time.Hour
	case provider.Interval1w:
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// aggregate folds chronologically ordered native candles into buckets of
// interval i: the bucket opens at the first candle's open, closes at the
// last candle's close, and carries the extreme high/low and summed volume
// across the bucket. Bucket timestamps are the bucket start.
//
// Gaps are tolerated — a bucket is emitted whenever the bucket start
// changes, so a missing native candle shortens a bucket instead of merging
// two of them.
func aggregate(candles []provider.OHLCV, i provider.Interval) []provider.OHLCV {
	if len(candles) == 0 {
		return candles
	}
	out := make([]provider.OHLCV, 0, len(candles))
	var cur provider.OHLCV
	var open bool
	for _, c := range candles {
		bs := bucketStart(c.Time, i)
		if !open || !bs.Equal(cur.Time) {
			if open {
				out = append(out, cur)
			}
			cur = provider.OHLCV{
				Time:   bs,
				Open:   c.Open,
				High:   c.High,
				Low:    c.Low,
				Close:  c.Close,
				Volume: c.Volume,
			}
			open = true
			continue
		}
		if c.High > cur.High {
			cur.High = c.High
		}
		if c.Low < cur.Low {
			cur.Low = c.Low
		}
		cur.Close = c.Close
		cur.Volume += c.Volume
	}
	return append(out, cur)
}

// bucketStart returns the UTC start of the aggregation bucket t falls in.
// Intraday and daily buckets align to the UTC epoch; weekly buckets align
// to Monday 00:00 UTC so a "1w" candle spans the conventional trading week
// rather than an arbitrary 7-day offset from the zero time.
func bucketStart(t time.Time, i provider.Interval) time.Time {
	u := t.UTC()
	if i == provider.Interval1w {
		day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
		offset := (int(day.Weekday()) + 6) % 7 // Monday == 0
		return day.AddDate(0, 0, -offset)
	}
	return u.Truncate(intervalDuration(i))
}
