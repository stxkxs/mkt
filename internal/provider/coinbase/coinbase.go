package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/stxkxs/mkt/internal/provider"
)

const (
	wsURL        = "wss://advanced-trade-ws.coinbase.com"
	restURL      = "https://api.exchange.coinbase.com"
	reconnectMin = 1 * time.Second
	reconnectMax = 30 * time.Second
)

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

// Supports returns true for crypto symbols in Coinbase format (XXX-USD, XXX-USDT)
// or bare symbols that we can convert (BTC, ETH, etc).
func (p *Provider) Supports(symbol string) bool {
	s := strings.ToUpper(symbol)
	// Direct Coinbase format
	if strings.Contains(s, "-") {
		return strings.HasSuffix(s, "-USD") || strings.HasSuffix(s, "-USDT")
	}
	// Bare crypto symbols we know about
	knownCrypto := map[string]bool{
		"BTC": true, "ETH": true, "SOL": true, "XRP": true,
		"ADA": true, "DOGE": true, "AVAX": true, "DOT": true,
		"MATIC": true, "LINK": true, "UNI": true, "ATOM": true,
		"LTC": true, "NEAR": true, "FIL": true, "APT": true,
		"ARB": true, "OP": true, "SUI": true, "SEI": true,
		"BNB": true, "PEPE": true, "SHIB": true, "WIF": true,
	}
	return knownCrypto[s]
}

// StatusChan returns a channel that receives connection status updates.
func (p *Provider) StatusChan() <-chan bool {
	return p.statusCh
}

// Subscribe connects to Coinbase WebSocket and streams quotes.
func (p *Provider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	// Normalize symbols to Coinbase format (BTC -> BTC-USD)
	productIDs := make([]string, 0, len(symbols))
	for _, s := range symbols {
		productIDs = append(productIDs, toCoinbaseSymbol(s))
	}

	backoff := reconnectMin
	for {
		err := p.connect(ctx, productIDs, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("coinbase ws disconnected: %v, reconnecting in %v", err, backoff)
		p.notifyStatus(false)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, reconnectMax)
	}
}

func (p *Provider) connect(ctx context.Context, productIDs []string, out chan<- provider.Quote) error {
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
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
		return fmt.Errorf("marshal subscribe: %w", err)
	}
	if err := ws.Write(ctx, websocket.MessageText, subData); err != nil {
		return fmt.Errorf("write subscribe: %w", err)
	}

	p.notifyStatus(true)

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
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
				case <-ctx.Done():
					ws.Close(websocket.StatusNormalClosure, "closing")
					return ctx.Err()
				}
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
		Symbol:    t.ProductID,
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

// toCoinbaseSymbol converts various formats to Coinbase product ID.
func toCoinbaseSymbol(s string) string {
	s = strings.ToUpper(s)
	// Already in Coinbase format
	if strings.Contains(s, "-") {
		return s
	}
	// Strip trailing USDT/USD
	base := s
	if strings.HasSuffix(base, "USDT") {
		base = strings.TrimSuffix(base, "USDT")
	} else if strings.HasSuffix(base, "USD") {
		base = strings.TrimSuffix(base, "USD")
	}
	return base + "-USD"
}

// History fetches historical OHLCV from Coinbase Exchange REST API.
// Coinbase candle format: [time, low, high, open, close, volume]
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	productID := toCoinbaseSymbol(params.Symbol)
	granularity := coinbaseGranularity(params.Interval)

	limit := params.Limit
	if limit == 0 {
		limit = 100
	}
	// Coinbase max 300 candles per request
	if limit > 300 {
		limit = 300
	}

	end := time.Now()
	if !params.End.IsZero() {
		end = params.End
	}
	start := end.Add(-time.Duration(limit) * granularityDuration(granularity))
	if !params.Start.IsZero() {
		start = params.Start
	}

	url := fmt.Sprintf("%s/products/%s/candles?granularity=%d&start=%s&end=%s",
		restURL, productID, granularity,
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mkt/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coinbase API error %d: %s", resp.StatusCode, string(body))
	}

	var raw [][]float64
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse candles: %w", err)
	}

	// Coinbase returns newest first, reverse to chronological
	candles := make([]provider.OHLCV, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		r := raw[i]
		if len(r) < 6 {
			continue
		}
		candles = append(candles, provider.OHLCV{
			Time:   time.Unix(int64(r[0]), 0),
			Low:    r[1],
			High:   r[2],
			Open:   r[3],
			Close:  r[4],
			Volume: r[5],
		})
	}
	return candles, nil
}

func coinbaseGranularity(i provider.Interval) int {
	switch i {
	case provider.Interval1m:
		return 60
	case provider.Interval5m:
		return 300
	case provider.Interval15m:
		return 900
	case provider.Interval1h:
		return 3600
	case provider.Interval4h:
		return 3600 * 4 // Not natively supported, use 1h
	case provider.Interval1d:
		return 86400
	case provider.Interval1w:
		return 86400 * 7 // Not natively supported, use 1d
	default:
		return 86400
	}
}

func granularityDuration(g int) time.Duration {
	return time.Duration(g) * time.Second
}
