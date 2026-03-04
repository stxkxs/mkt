package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

const (
	restBaseURL = "https://api.binance.com"
)

// Provider implements QuoteProvider and HistoryProvider for Binance.
type Provider struct {
	client   *http.Client
	statusCh chan bool
}

// New creates a new Binance provider.
func New() *Provider {
	return &Provider{
		client:   &http.Client{Timeout: 15 * time.Second},
		statusCh: make(chan bool, 1),
	}
}

func (p *Provider) Name() string { return "binance" }

// Supports returns true for crypto trading pairs (uppercase, ends with USDT/BTC/ETH/BUSD/BNB).
func (p *Provider) Supports(symbol string) bool {
	s := strings.ToUpper(symbol)
	return strings.HasSuffix(s, "USDT") ||
		strings.HasSuffix(s, "BTC") ||
		strings.HasSuffix(s, "ETH") ||
		strings.HasSuffix(s, "BUSD") ||
		strings.HasSuffix(s, "BNB") ||
		strings.HasSuffix(s, "USD")
}

// StatusChan returns a channel that receives connection status updates.
func (p *Provider) StatusChan() <-chan bool {
	return p.statusCh
}

// Subscribe opens a WebSocket connection and streams quotes.
func (p *Provider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	msgCh := make(chan combinedStreamMsg, 64)

	c := &conn{
		symbols: symbols,
		out:     msgCh,
		status:  p.statusCh,
	}

	go c.run(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-msgCh:
			q, err := tickerToQuote(msg.Data)
			if err != nil {
				continue
			}
			select {
			case out <- q:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// History fetches historical OHLCV data from Binance REST API.
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	limit := params.Limit
	if limit == 0 {
		limit = 100
	}
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		restBaseURL, strings.ToUpper(params.Symbol), params.Interval, limit)

	if !params.Start.IsZero() {
		url += fmt.Sprintf("&startTime=%d", params.Start.UnixMilli())
	}
	if !params.End.IsZero() {
		url += fmt.Sprintf("&endTime=%d", params.End.UnixMilli())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("binance API error %d: %s", resp.StatusCode, string(body))
	}

	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse klines: %w", err)
	}

	var candles []provider.OHLCV
	for _, k := range raw {
		if len(k) < 6 {
			continue
		}
		candle, err := parseKline(k)
		if err != nil {
			continue
		}
		candles = append(candles, candle)
	}
	return candles, nil
}

func tickerToQuote(t miniTickerEvent) (provider.Quote, error) {
	price, err := strconv.ParseFloat(t.Close, 64)
	if err != nil {
		return provider.Quote{}, err
	}
	open, _ := strconv.ParseFloat(t.Open, 64)
	high, _ := strconv.ParseFloat(t.High, 64)
	low, _ := strconv.ParseFloat(t.Low, 64)
	vol, _ := strconv.ParseFloat(t.Volume, 64)

	change := price - open
	var changePct float64
	if open != 0 {
		changePct = (change / open) * 100
	}

	return provider.Quote{
		Symbol:    t.Symbol,
		Price:     price,
		Change:    change,
		ChangePct: changePct,
		Volume:    vol,
		High24h:   high,
		Low24h:    low,
		Asset:     provider.AssetCrypto,
		Provider:  "binance",
		Timestamp: time.UnixMilli(t.EventTime),
	}, nil
}

func parseKline(k []any) (provider.OHLCV, error) {
	ts, ok := k[0].(float64)
	if !ok {
		return provider.OHLCV{}, fmt.Errorf("invalid timestamp")
	}

	parseStr := func(v any) float64 {
		s, ok := v.(string)
		if !ok {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}

	return provider.OHLCV{
		Time:   time.UnixMilli(int64(ts)),
		Open:   parseStr(k[1]),
		High:   parseStr(k[2]),
		Low:    parseStr(k[3]),
		Close:  parseStr(k[4]),
		Volume: parseStr(k[5]),
	}, nil
}
