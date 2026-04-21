package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

const (
	baseURL  = "https://query1.finance.yahoo.com"
	chartURL = "https://query1.finance.yahoo.com/v8/finance/chart"
)

// Provider implements QuoteProvider and HistoryProvider for Yahoo Finance.
type Provider struct {
	client       *http.Client
	pollInterval time.Duration

	mu    sync.Mutex
	crumb string
}

// New creates a new Yahoo Finance provider.
func New(pollInterval time.Duration) *Provider {
	if pollInterval <= 0 {
		pollInterval = 15 * time.Second
	}
	jar, _ := cookiejar.New(nil)
	return &Provider{
		client: &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
		},
		pollInterval: pollInterval,
	}
}

func (p *Provider) Name() string { return "yahoo" }

// Supports returns true for stock symbols (not crypto pairs).
func (p *Provider) Supports(symbol string) bool {
	s := strings.ToUpper(symbol)
	// Not a crypto pair (Coinbase format or Binance format)
	if strings.Contains(s, "-USD") || strings.HasSuffix(s, "USDT") || strings.HasSuffix(s, "BUSD") {
		return false
	}
	// Known crypto bare symbols
	knownCrypto := map[string]bool{
		"BTC": true, "ETH": true, "SOL": true, "XRP": true,
		"ADA": true, "DOGE": true, "AVAX": true, "BNB": true,
	}
	if knownCrypto[s] {
		return false
	}
	// Stock-like: 1-5 uppercase letters, possibly with dots (BRK.B)
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || c == '.' || c == '-') {
			return false
		}
	}
	return len(s) >= 1 && len(s) <= 10
}

// initSession fetches Yahoo homepage to get cookies and crumb.
func (p *Provider) initSession(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.crumb != "" {
		return nil
	}

	// Step 1: Hit finance page to get cookies
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://finance.yahoo.com/quote/AAPL/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch yahoo page: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Step 2: Extract crumb from page content
	crumbRe := regexp.MustCompile(`"crumb"\s*:\s*"([^"]+)"`)
	matches := crumbRe.FindSubmatch(body)
	if len(matches) >= 2 {
		p.crumb = string(matches[1])
		// Unescape unicode
		p.crumb = strings.ReplaceAll(p.crumb, `\u002F`, "/")
		return nil
	}

	// Alternative: try the crumb endpoint directly
	crumbReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return err
	}
	crumbReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	crumbResp, err := p.client.Do(crumbReq)
	if err != nil {
		return fmt.Errorf("fetch crumb: %w", err)
	}
	crumbBody, _ := io.ReadAll(crumbResp.Body)
	crumbResp.Body.Close()

	if crumbResp.StatusCode == 200 && len(crumbBody) > 0 {
		p.crumb = string(crumbBody)
		return nil
	}

	// If we can't get a crumb, try without one (some endpoints work without it)
	return nil
}

// Subscribe polls Yahoo Finance at regular intervals.
func (p *Provider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	// Non-fatal: some endpoints work without a crumb, so we log and proceed.
	if err := p.initSession(ctx); err != nil {
		log.Printf("yahoo: session init failed, continuing without crumb: %v", err)
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	// Initial fetch
	p.fetchAndSend(ctx, symbols, out)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.fetchAndSend(ctx, symbols, out)
		}
	}
}

const batchSize = 50

func (p *Provider) fetchAndSend(ctx context.Context, symbols []string, out chan<- provider.Quote) {
	// Try batch endpoint first
	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]

		quotes, err := p.fetchBatchQuotes(ctx, batch)
		if err != nil {
			// Fallback: parallel per-symbol chart API
			p.fetchParallel(ctx, batch, out)
			continue
		}

		for _, q := range quotes {
			select {
			case out <- q:
			case <-ctx.Done():
				return
			}
		}
	}
}

// fetchParallel fetches quotes for symbols concurrently using the chart API.
// Limited to 10 concurrent requests to avoid overwhelming the API.
func (p *Provider) fetchParallel(ctx context.Context, symbols []string, out chan<- provider.Quote) {
	const workers = 10
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, sym := range symbols {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			defer func() { <-sem }()
			q, err := p.fetchQuoteViaChart(ctx, s)
			if err != nil {
				return
			}
			select {
			case out <- q:
			case <-ctx.Done():
			}
		}(sym)
	}
	wg.Wait()
}

// fetchBatchQuotes fetches quotes for multiple symbols in a single HTTP request
// using the v7/finance/quote endpoint.
func (p *Provider) fetchBatchQuotes(ctx context.Context, symbols []string) ([]provider.Quote, error) {
	joined := strings.Join(symbols, ",")
	// Try v7 first (newer), with explicit field list to ensure high/low are returned
	url := fmt.Sprintf("%s/v7/finance/quote?symbols=%s&fields=regularMarketPrice,regularMarketChange,regularMarketChangePercent,regularMarketVolume,regularMarketDayHigh,regularMarketDayLow,regularMarketPreviousClose", baseURL, joined)
	if p.crumb != "" {
		url += "&crumb=" + p.crumb
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		p.mu.Lock()
		p.crumb = ""
		p.mu.Unlock()
		return nil, fmt.Errorf("yahoo auth error %d, resetting crumb", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo batch quote error %d", resp.StatusCode)
	}

	var result batchQuoteResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse batch quotes: %w", err)
	}

	if result.QuoteResponse.Error != nil {
		return nil, fmt.Errorf("yahoo error: %s", result.QuoteResponse.Error.Description)
	}

	var quotes []provider.Quote
	for _, r := range result.QuoteResponse.Result {
		if r.RegularMarketPrice == 0 {
			continue
		}
		quotes = append(quotes, provider.Quote{
			Symbol:    r.Symbol,
			Price:     r.RegularMarketPrice,
			Change:    r.RegularMarketChange,
			ChangePct: r.RegularMarketChangePercent,
			Volume:    r.RegularMarketVolume,
			High24h:   r.RegularMarketDayHigh,
			Low24h:    r.RegularMarketDayLow,
			Asset:     provider.AssetStock,
			Provider:  "yahoo",
			Timestamp: time.Now(),
		})
	}

	return quotes, nil
}

// fetchQuoteViaChart uses the v8 chart API which is more reliable than the quote API.
func (p *Provider) fetchQuoteViaChart(ctx context.Context, symbol string) (provider.Quote, error) {
	url := fmt.Sprintf("%s/%s?interval=1d&range=2d", chartURL, symbol)
	if p.crumb != "" {
		url += "&crumb=" + p.crumb
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return provider.Quote{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return provider.Quote{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return provider.Quote{}, err
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		// Reset crumb and retry on next poll
		p.mu.Lock()
		p.crumb = ""
		p.mu.Unlock()
		return provider.Quote{}, fmt.Errorf("yahoo auth error %d, resetting crumb", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return provider.Quote{}, fmt.Errorf("yahoo API error %d", resp.StatusCode)
	}

	var result chartResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return provider.Quote{}, fmt.Errorf("parse chart: %w", err)
	}

	if result.Chart.Error != nil {
		return provider.Quote{}, fmt.Errorf("yahoo error: %s", result.Chart.Error.Description)
	}

	if len(result.Chart.Result) == 0 {
		return provider.Quote{}, fmt.Errorf("no data for %s", symbol)
	}

	r := result.Chart.Result[0]
	meta := r.Meta

	price := meta.RegularMarketPrice
	prevClose := meta.ChartPreviousClose
	change := price - prevClose
	var changePct float64
	if prevClose > 0 {
		changePct = (change / prevClose) * 100
	}

	// Get volume from indicators if available
	var volume float64
	if len(r.Indicators.Quote) > 0 {
		q := r.Indicators.Quote[0]
		if len(q.Volume) > 0 {
			// Use last volume
			for i := len(q.Volume) - 1; i >= 0; i-- {
				if q.Volume[i] != nil {
					volume = *q.Volume[i]
					break
				}
			}
		}
	}

	// Get high/low: prefer meta fields, fall back to indicators
	high := meta.RegularMarketDayHigh
	low := meta.RegularMarketDayLow
	if high == 0 && len(r.Indicators.Quote) > 0 {
		q := r.Indicators.Quote[0]
		if len(q.High) > 0 {
			for i := len(q.High) - 1; i >= 0; i-- {
				if q.High[i] != nil {
					high = *q.High[i]
					break
				}
			}
		}
	}
	if low == 0 && len(r.Indicators.Quote) > 0 {
		q := r.Indicators.Quote[0]
		if len(q.Low) > 0 {
			for i := len(q.Low) - 1; i >= 0; i-- {
				if q.Low[i] != nil {
					low = *q.Low[i]
					break
				}
			}
		}
	}

	return provider.Quote{
		Symbol:    symbol,
		Price:     price,
		Change:    change,
		ChangePct: changePct,
		Volume:    volume,
		High24h:   high,
		Low24h:    low,
		Asset:     provider.AssetStock,
		Provider:  "yahoo",
		Timestamp: time.Now(),
	}, nil
}

// History fetches historical OHLCV data.
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	if err := p.initSession(ctx); err != nil {
		log.Printf("yahoo: session init failed for history %s, continuing: %v", params.Symbol, err)
	}

	interval := yahooInterval(params.Interval)
	rng := yahooRange(params.Interval, params.Limit)

	url := fmt.Sprintf("%s/%s?interval=%s&range=%s", chartURL, params.Symbol, interval, rng)
	if p.crumb != "" {
		url += "&crumb=" + p.crumb
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

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
		return nil, fmt.Errorf("yahoo chart error %d: %s", resp.StatusCode, string(body))
	}

	var result chartResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse yahoo chart: %w", err)
	}

	if result.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo chart error: %s", result.Chart.Error.Description)
	}

	if len(result.Chart.Result) == 0 {
		return nil, fmt.Errorf("no chart data for %s", params.Symbol)
	}

	r := result.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no indicators for %s", params.Symbol)
	}

	q := r.Indicators.Quote[0]
	var candles []provider.OHLCV
	for i, ts := range r.Timestamp {
		if i >= len(q.Open) || i >= len(q.Close) {
			break
		}
		if q.Open[i] == nil || q.Close[i] == nil {
			continue
		}
		c := provider.OHLCV{
			Time:  time.Unix(ts, 0),
			Open:  deref(q.Open[i]),
			High:  deref(q.High[i]),
			Low:   deref(q.Low[i]),
			Close: deref(q.Close[i]),
		}
		if i < len(q.Volume) && q.Volume[i] != nil {
			c.Volume = *q.Volume[i]
		}
		candles = append(candles, c)
	}
	return candles, nil
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func yahooInterval(i provider.Interval) string {
	switch i {
	case provider.Interval1m:
		return "1m"
	case provider.Interval5m:
		return "5m"
	case provider.Interval15m:
		return "15m"
	case provider.Interval1h:
		return "1h"
	case provider.Interval1d:
		return "1d"
	case provider.Interval1w:
		return "1wk"
	default:
		return "1d"
	}
}

func yahooRange(i provider.Interval, limit int) string {
	switch i {
	case provider.Interval1m:
		return "1d"
	case provider.Interval5m:
		return "5d"
	case provider.Interval15m:
		return "5d"
	case provider.Interval1h:
		return "1mo"
	case provider.Interval1d:
		if limit > 200 {
			return "2y"
		}
		return "6mo"
	case provider.Interval1w:
		return "2y"
	default:
		return "6mo"
	}
}
