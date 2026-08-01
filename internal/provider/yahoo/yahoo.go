package yahoo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
	"github.com/stxkxs/mkt/internal/observe"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/symbol"
)

// Provider-level failure counters surfaced on /metrics.
var (
	batchFailures   = observe.NewCounter("mkt_provider_yahoo_batch_failures_total")
	sessionFailures = observe.NewCounter("mkt_provider_yahoo_session_init_failures_total")
)

// Yahoo endpoint bases. Declared as vars (not consts) so tests can point
// them at an httptest server; production uses the real hosts. This is the
// same pattern the side endpoints (OptionsBaseURL, QuoteSummaryURL) already
// use — extending it to the core quote/chart paths makes Subscribe and the
// batch-quote path testable without network access.
var (
	baseURL    = "https://query1.finance.yahoo.com"
	chartURL   = "https://query1.finance.yahoo.com/v8/finance/chart"
	sessionURL = "https://finance.yahoo.com/quote/AAPL/"
	crumbURL   = "https://query2.finance.yahoo.com/v1/test/getcrumb"
)

// yahooHeaders is the browser-like header set the core quote/chart/summary
// endpoints expect. Read-only; shared across requests.
var yahooHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
}

// yahooJSONHeaders additionally advertises a JSON Accept for the v10
// quoteSummary (earnings) and v7 options endpoints, which are JSON-only.
var yahooJSONHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
	"Accept":     "application/json",
}

var (
	_ provider.QuoteProvider   = (*Provider)(nil)
	_ provider.HistoryProvider = (*Provider)(nil)
)

// Provider implements QuoteProvider and HistoryProvider for Yahoo Finance.
type Provider struct {
	client       *http.Client
	pollInterval time.Duration

	// sessionMu serializes crumb acquisition. It is deliberately separate
	// from mu: initSession holds it across network I/O (which can block for
	// the length of a 429 cooldown), and a crumb *read* must never wait
	// that long.
	sessionMu sync.Mutex

	// mu guards crumb only. Every read goes through crumbValue — the crumb
	// is written by initSession and cleared by resetCrumbOnAuthError from
	// other goroutines.
	mu    sync.Mutex
	crumb string

	// healthMu guards the reachability signal exposed by Healthy,
	// LastError and StatusChan.
	healthMu sync.Mutex
	healthy  bool
	failures int
	lastErr  error
	statusCh chan bool
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
		healthy:      true,
		statusCh:     make(chan bool, 1),
	}
}

func (p *Provider) Name() string { return "yahoo" }

// Supports returns true for stock-shaped symbols (not crypto, not FRED).
// Classification is delegated to the shared symbol package so Yahoo can't
// disagree with Coinbase about whether something is crypto.
func (p *Provider) Supports(sym string) bool {
	return symbol.IsStock(sym)
}

// crumbValue returns the cached crumb under the lock. All crumb reads must
// go through it; reading p.crumb directly races with initSession and
// resetCrumbOnAuthError.
func (p *Provider) crumbValue() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.crumb
}

func (p *Provider) setCrumb(c string) {
	p.mu.Lock()
	p.crumb = c
	p.mu.Unlock()
}

// crumbParam returns the crumb query parameter (with the given separator)
// for endpoint building, or "" when no crumb is cached.
func (p *Provider) crumbParam(sep string) string {
	c := p.crumbValue()
	if c == "" {
		return ""
	}
	return sep + "crumb=" + url.QueryEscape(c)
}

// initSession fetches Yahoo homepage to get cookies and crumb.
func (p *Provider) initSession(ctx context.Context) error {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()

	if p.crumbValue() != "" {
		return nil
	}

	// Step 1: Hit finance page to get cookies
	body, _, err := p.get(ctx, sessionURL, map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	})
	if err != nil {
		return fmt.Errorf("fetch yahoo page: %w", err)
	}

	// Step 2: Extract crumb from page content
	matches := crumbRe.FindSubmatch(body)
	if len(matches) >= 2 {
		// Unescape unicode
		p.setCrumb(strings.ReplaceAll(string(matches[1]), `\u002F`, "/"))
		return nil
	}

	// Alternative: try the crumb endpoint directly
	crumbBody, _, err := p.get(ctx, crumbURL, yahooHeaders)
	if err != nil {
		return fmt.Errorf("fetch crumb: %w", err)
	}
	if len(crumbBody) > 0 {
		p.setCrumb(string(crumbBody))
		return nil
	}

	// If we can't get a crumb, try without one (some endpoints work without it)
	return nil
}

var crumbRe = regexp.MustCompile(`"crumb"\s*:\s*"([^"]+)"`)

// resetCrumbOnAuthError clears the cached crumb when err reports a 401/403,
// so the next poll re-establishes the session. Returns whether it matched.
func (p *Provider) resetCrumbOnAuthError(err error) bool {
	var se *httpx.StatusError
	if errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden) {
		p.setCrumb("")
		return true
	}
	return false
}

// Subscribe polls Yahoo Finance at regular intervals.
func (p *Provider) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	// Non-fatal: some endpoints work without a crumb, so we log and proceed.
	if err := p.initSession(ctx); err != nil {
		sessionFailures.Inc()
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

const (
	// batchSize is how many symbols one v7/finance/quote request carries.
	batchSize = 50
	// maxChartFallbacks bounds the per-symbol chart fallback taken when a
	// batch fails outright. The batch itself is already retried with
	// backoff inside getJSON, so the fallback exists only to keep a handful
	// of symbols alive when the batch endpoint is broken for the whole
	// list — not to re-request every symbol individually.
	maxChartFallbacks = 8
)

func (p *Provider) fetchAndSend(ctx context.Context, symbols []string, out chan<- provider.Quote) {
	// One log line per poll cycle at most: during a Yahoo outage every batch
	// fails, and this process shares stderr with the TUI.
	logged := false

	// Try batch endpoint first
	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]

		quotes, err := p.fetchBatchQuotes(ctx, batch)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			batchFailures.Inc()
			if !logged {
				log.Printf("yahoo: batch quote for %d symbols failed: %v", len(batch), err)
				logged = true
			}
			if !p.fetchChartFallback(ctx, batch, out) {
				return
			}
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

// fetchChartFallback covers for a failed batch with per-symbol chart
// requests, capped at maxChartFallbacks symbols and issued sequentially
// through the shared limiter. The previous implementation fanned out one
// request per symbol, ten at a time and unthrottled: on the ~150-symbol
// default watchlist a single 429 turned three requests into ~150, which
// guaranteed more 429s and kept the storm going. Returns false when ctx
// ended and the caller should stop.
func (p *Provider) fetchChartFallback(ctx context.Context, symbols []string, out chan<- provider.Quote) bool {
	if len(symbols) > maxChartFallbacks {
		symbols = symbols[:maxChartFallbacks]
	}
	for _, sym := range symbols {
		if ctx.Err() != nil {
			return false
		}
		q, err := p.fetchQuoteViaChart(ctx, sym)
		if err != nil {
			continue
		}
		select {
		case out <- q:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// fetchBatchQuotes fetches quotes for multiple symbols in a single HTTP request
// using the v7/finance/quote endpoint.
func (p *Provider) fetchBatchQuotes(ctx context.Context, symbols []string) ([]provider.Quote, error) {
	escaped := make([]string, len(symbols))
	for i, s := range symbols {
		escaped[i] = url.QueryEscape(s)
	}
	joined := strings.Join(escaped, ",")
	// Try v7 first (newer), with explicit field list to ensure high/low are returned
	endpoint := fmt.Sprintf("%s/v7/finance/quote?symbols=%s&fields=regularMarketPrice,regularMarketChange,regularMarketChangePercent,regularMarketVolume,regularMarketDayHigh,regularMarketDayLow,regularMarketPreviousClose", baseURL, joined)
	endpoint += p.crumbParam("&")

	var result batchQuoteResponse
	if err := p.getJSON(ctx, endpoint, yahooHeaders, &result); err != nil {
		p.resetCrumbOnAuthError(err)
		return nil, fmt.Errorf("yahoo batch quote: %w", err)
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
	endpoint := fmt.Sprintf("%s/%s?interval=1d&range=2d", chartURL, url.PathEscape(symbol))
	endpoint += p.crumbParam("&")

	var result chartResponse
	if err := p.getJSON(ctx, endpoint, yahooHeaders, &result); err != nil {
		p.resetCrumbOnAuthError(err)
		return provider.Quote{}, fmt.Errorf("yahoo chart quote: %w", err)
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
	if price <= 0 {
		// Yahoo answers 200 with a chart that omits regularMarketPrice for
		// halted or thinly-traded symbols. Emitting it anyway produced
		// phantom quotes downstream (price=0, changePct=-100), so treat a
		// missing price as no data — the same guard the batch path applies.
		return provider.Quote{}, fmt.Errorf("no price for %s", symbol)
	}

	prevClose := previousBarClose(r)
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

// previousBarClose returns the close of the bar before the most recent one
// in the returned series — the reference a daily change is measured against.
// meta.chartPreviousClose is the close before the *whole requested range*
// (with range=2d, two sessions back), so using it made every change and
// change-percent one session stale. It is only the right answer when the
// series holds a single bar, which is the one case we fall back to it.
func previousBarClose(r chartResult) float64 {
	if len(r.Indicators.Quote) > 0 {
		var prev, last float64
		var n int
		for _, c := range r.Indicators.Quote[0].Close {
			if c == nil {
				continue
			}
			prev, last = last, *c
			n++
		}
		if n >= 2 {
			return prev
		}
	}
	return r.Meta.ChartPreviousClose
}

// HistoryResult is a history fetch plus the interval Yahoo actually served.
// Yahoo has no 4h bucket, so a 4h request comes back as 1h bars; a caller
// that labels a chart should label it with Interval, not with Requested.
type HistoryResult struct {
	Candles   []provider.OHLCV
	Requested provider.Interval // interval the caller asked for
	Interval  provider.Interval // interval Yahoo actually served
}

// History fetches historical OHLCV data. It implements
// provider.HistoryProvider; callers that need to know which interval was
// really served should use HistoryWithMeta.
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	res, err := p.HistoryWithMeta(ctx, params)
	return res.Candles, err
}

// HistoryWithMeta fetches historical OHLCV data and reports the interval
// Yahoo served alongside it. At most params.Limit bars are returned, most
// recent last; Limit <= 0 means "everything the range yielded".
func (p *Provider) HistoryWithMeta(ctx context.Context, params provider.HistoryParams) (HistoryResult, error) {
	if err := p.initSession(ctx); err != nil {
		log.Printf("yahoo: session init failed for history %s, continuing: %v", params.Symbol, err)
	}

	out := HistoryResult{Requested: params.Interval, Interval: ServedInterval(params.Interval)}

	interval := yahooInterval(params.Interval)
	rng := yahooRange(params.Interval, params.Limit)

	endpoint := fmt.Sprintf("%s/%s?interval=%s&range=%s", chartURL, url.PathEscape(params.Symbol), interval, rng)
	endpoint += p.crumbParam("&")

	var result chartResponse
	if err := p.getJSON(ctx, endpoint, yahooHeaders, &result); err != nil {
		p.resetCrumbOnAuthError(err)
		return out, fmt.Errorf("yahoo chart history: %w", err)
	}

	if result.Chart.Error != nil {
		return out, fmt.Errorf("yahoo chart error: %s", result.Chart.Error.Description)
	}

	if len(result.Chart.Result) == 0 {
		return out, fmt.Errorf("no chart data for %s", params.Symbol)
	}

	r := result.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return out, fmt.Errorf("no indicators for %s", params.Symbol)
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
		// High/Low/Volume are independent slices in the payload; nothing
		// guarantees they are as long as open/close, so index each one
		// defensively rather than dereferencing blind.
		c := provider.OHLCV{
			Time:   time.Unix(ts, 0),
			Open:   deref(q.Open[i]),
			High:   at(q.High, i),
			Low:    at(q.Low, i),
			Close:  deref(q.Close[i]),
			Volume: at(q.Volume, i),
		}
		candles = append(candles, c)
	}

	// Honor the requested bar count, keeping the most recent bars — the
	// chart ranges above are coarse (6mo, 2y), so without this a caller
	// asking for 50 bars could get hundreds.
	if params.Limit > 0 && len(candles) > params.Limit {
		candles = candles[len(candles)-params.Limit:]
	}
	out.Candles = candles
	return out, nil
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// at returns s[i] dereferenced, or 0 when i is out of range or the entry is
// null (Yahoo emits nulls for gapped bars).
func at(s []*float64, i int) float64 {
	if i < 0 || i >= len(s) {
		return 0
	}
	return deref(s[i])
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
	case provider.Interval4h:
		return "1h" // Yahoo has no 4h bucket; fall back to 1h like Coinbase does
	case provider.Interval1d:
		return "1d"
	case provider.Interval1w:
		return "1wk"
	default:
		return "1d"
	}
}

// ServedInterval reports the bar interval Yahoo actually serves for a
// requested one. Yahoo has no 4h bucket, so a 4h request is served as 1h
// bars; unsupported intervals fall back to 1d. Everything else maps 1:1.
// Exported so a caller can label a chart honestly without inspecting the
// response.
func ServedInterval(i provider.Interval) provider.Interval {
	switch i {
	case provider.Interval1m, provider.Interval5m, provider.Interval15m,
		provider.Interval1h, provider.Interval1d, provider.Interval1w:
		return i
	case provider.Interval4h:
		return provider.Interval1h
	default:
		return provider.Interval1d
	}
}

// ServedInterval is the method form of the package function, so a router
// holding this provider behind an interface can ask what it will really get
// without importing this package or type-asserting to a concrete type.
func (p *Provider) ServedInterval(i provider.Interval) provider.Interval {
	return ServedInterval(i)
}

func yahooRange(i provider.Interval, limit int) string {
	switch i {
	case provider.Interval1m:
		return "1d"
	case provider.Interval5m:
		return "5d"
	case provider.Interval15m:
		return "5d"
	case provider.Interval1h, provider.Interval4h:
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
