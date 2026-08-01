package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
)

// ─────────────────────────────── test doubles ───────────────────────────────

// stubHistory is a historyFetcher backed by a fixed table, recording how many
// calls it saw and how many were in flight at once.
type stubHistory struct {
	bars map[string][]provider.OHLCV
	fail map[string]error

	delay time.Duration

	mu       sync.Mutex
	calls    []string
	inFlight int32
	peak     int32
}

func (s *stubHistory) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	n := atomic.AddInt32(&s.inFlight, 1)
	for {
		peak := atomic.LoadInt32(&s.peak)
		if n <= peak || atomic.CompareAndSwapInt32(&s.peak, peak, n) {
			break
		}
	}
	defer atomic.AddInt32(&s.inFlight, -1)

	s.mu.Lock()
	s.calls = append(s.calls, params.Symbol)
	s.mu.Unlock()

	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err, ok := s.fail[params.Symbol]; ok {
		return nil, err
	}
	bars, ok := s.bars[params.Symbol]
	if !ok {
		return nil, nil
	}
	return bars, nil
}

func (s *stubHistory) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// dailyBar is one close on a given day.
func dailyBar(day time.Time, close float64) provider.OHLCV {
	return provider.OHLCV{Time: day, Open: close, High: close, Low: close, Close: close, Volume: 1}
}

// ───────────────────────────── quote sourcing ─────────────────────────────

// TestQuoteForLabelsADailyCloseHonestly is the regression test for get_quote
// handing an agent a daily candle's close as "the current price". On any
// weekday afternoon that is yesterday's number; the source field is what
// stops an agent reasoning about it as live.
func TestQuoteForLabelsADailyCloseHonestly(t *testing.T) {
	asOf := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	hist := &stubHistory{bars: map[string][]provider.OHLCV{
		"AAPL": {dailyBar(asOf.AddDate(0, 0, -1), 199), dailyBar(asOf, 201)},
	}}

	q, err := quoteFor(context.Background(), nil, hist, "AAPL")
	if err != nil {
		t.Fatalf("quoteFor: %v", err)
	}
	if q.Source != sourceDailyClose {
		t.Errorf("source = %q, want %q", q.Source, sourceDailyClose)
	}
	if q.Note == "" {
		t.Error("a daily close must carry a note saying it is not a live quote")
	}
	if q.Price != 201 {
		t.Errorf("price = %v, want the most recent bar's close 201", q.Price)
	}
	if q.AsOf != asOf.Format(time.RFC3339) {
		t.Errorf("asOf = %q, want the bar's own time", q.AsOf)
	}
}

// TestQuoteForPrefersTheLiveReadAPI checks the live surface wins when a
// dashboard is listening, and that the result says so.
func TestQuoteForPrefersTheLiveReadAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quotes/AAPL" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol": "AAPL", "price": 205.5, "change": 4.5, "change_pct": 2.24,
			"time": "2026-03-02T15:30:00Z",
		})
	}))
	defer srv.Close()

	hist := &stubHistory{bars: map[string][]provider.OHLCV{
		"AAPL": {dailyBar(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), 201)},
	}}
	live := liveClientForTest(t, srv.URL, "")

	q, err := quoteFor(context.Background(), live, hist, "AAPL")
	if err != nil {
		t.Fatalf("quoteFor: %v", err)
	}
	if q.Source != sourceLive {
		t.Errorf("source = %q, want %q", q.Source, sourceLive)
	}
	if q.Price != 205.5 {
		t.Errorf("price = %v, want the live 205.5 not the daily close", q.Price)
	}
	if hist.callCount() != 0 {
		t.Errorf("history was queried even though the read API answered: %d calls", hist.callCount())
	}
}

// TestQuoteForFallsBackWhenTheReadAPIIsDown keeps the live surface an
// optimization rather than a dependency.
func TestQuoteForFallsBackWhenTheReadAPIIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	hist := &stubHistory{bars: map[string][]provider.OHLCV{
		"AAPL": {dailyBar(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), 201)},
	}}
	q, err := quoteFor(context.Background(), liveClientForTest(t, srv.URL, ""), hist, "AAPL")
	if err != nil {
		t.Fatalf("quoteFor: %v", err)
	}
	if q.Source != sourceDailyClose || q.Price != 201 {
		t.Errorf("did not fall back to the history provider: %+v", q)
	}
}

// TestQuoteForSendsTheBearerToken checks the read API's auth is honored, so
// a token-protected dashboard is still usable from the MCP server.
func TestQuoteForSendsTheBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"symbol": "AAPL", "price": 1})
	}))
	defer srv.Close()

	if _, err := quoteFor(context.Background(), liveClientForTest(t, srv.URL, "s3cret"), &stubHistory{}, "AAPL"); err != nil {
		t.Fatalf("quoteFor: %v", err)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer s3cret")
	}
}

// TestQuoteForRejectsAZeroPricedLiveEntry guards against a cached-but-unset
// entry being reported as a price of zero.
func TestQuoteForRejectsAZeroPricedLiveEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"symbol": "AAPL", "price": 0})
	}))
	defer srv.Close()

	hist := &stubHistory{bars: map[string][]provider.OHLCV{
		"AAPL": {dailyBar(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), 201)},
	}}
	q, err := quoteFor(context.Background(), liveClientForTest(t, srv.URL, ""), hist, "AAPL")
	if err != nil {
		t.Fatalf("quoteFor: %v", err)
	}
	if q.Price != 201 {
		t.Errorf("a zero live price was accepted: %+v", q)
	}
}

// TestQuoteForErrorsWhenThereIsNoData checks an unknown symbol is an error,
// not a silent zero.
func TestQuoteForErrorsWhenThereIsNoData(t *testing.T) {
	if _, err := quoteFor(context.Background(), nil, &stubHistory{}, "NOPE"); err == nil {
		t.Fatal("expected an error for a symbol with no bars")
	}
}

// TestLiveClientFromFlagsIsNilWithoutListen keeps the fallback path the
// default: with no --listen there is nothing to probe, and building a client
// would add a doomed round trip to every quote.
func TestLiveClientFromFlagsIsNilWithoutListen(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("listen", "", "")
	cmd.Flags().String("listen-token", "", "")
	if got := liveClientFromFlags(cmd); got != nil {
		t.Errorf("liveClientFromFlags = %+v, want nil without --listen", got)
	}
}

// TestLiveClientFromFlagsAddsAScheme checks a bare host:port from --listen is
// usable as a URL.
func TestLiveClientFromFlagsAddsAScheme(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("listen", "127.0.0.1:9999", "")
	cmd.Flags().String("listen-token", "tok", "")

	got := liveClientFromFlags(cmd)
	if got == nil {
		t.Fatal("liveClientFromFlags = nil")
	}
	if got.base != "http://127.0.0.1:9999" {
		t.Errorf("base = %q, want http://127.0.0.1:9999", got.base)
	}
	if got.token != "tok" {
		t.Errorf("token = %q, want tok", got.token)
	}
}

// ─────────────────────────── portfolio valuation ───────────────────────────

// TestSummarizePortfolioReportsPartialCoverage is the regression test for
// get_portfolio discarding every fetch error: a portfolio where half the
// symbols failed returned a confident P&L computed from the other half, and
// an agent had no way to tell.
func TestSummarizePortfolioReportsPartialCoverage(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	hist := &stubHistory{
		bars: map[string][]provider.OHLCV{"AAPL": {dailyBar(day, 200)}},
		fail: map[string]error{"VTI": fmt.Errorf("provider unreachable")},
	}
	pf := portfolio.Portfolio{
		Name: "Retirement",
		Holdings: []portfolio.Holding{
			{Symbol: "AAPL", Quantity: 10, CostBasis: 150},
			{Symbol: "VTI", Quantity: 100, CostBasis: 180},
		},
	}

	res := summarizePortfolio(context.Background(), nil, hist, pf)

	if res.FullyPriced {
		t.Error("a portfolio with an unpriced position reported fullyPriced")
	}
	if !slices.Contains(res.Unpriced, "VTI") {
		t.Errorf("unpriced = %v, want it to list VTI", res.Unpriced)
	}
	if res.Note == "" {
		t.Error("a partial valuation must carry a note")
	}
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "VTI") {
		t.Errorf("fetch errors were discarded: %v", res.Errors)
	}
	// Coverage is by cost basis: AAPL is 10×150 = 1500 of a 19500 total.
	wantCoverage := 1500.0 / (1500.0 + 18000.0)
	if diff := res.Coverage - wantCoverage; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("coverage = %v, want %v", res.Coverage, wantCoverage)
	}
	if res.UnpricedCost != 18000 {
		t.Errorf("unpricedCost = %v, want 18000", res.UnpricedCost)
	}
}

// TestSummarizePortfolioFullyPricedHasNoNote checks the healthy case stays
// quiet — a note on every response would train an agent to ignore it.
func TestSummarizePortfolioFullyPricedHasNoNote(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	hist := &stubHistory{bars: map[string][]provider.OHLCV{
		"AAPL": {dailyBar(day, 200)},
		"VTI":  {dailyBar(day, 190)},
	}}
	pf := portfolio.Portfolio{
		Name: "Retirement",
		Holdings: []portfolio.Holding{
			{Symbol: "AAPL", Quantity: 10, CostBasis: 150},
			{Symbol: "VTI", Quantity: 100, CostBasis: 180},
		},
	}

	res := summarizePortfolio(context.Background(), nil, hist, pf)
	if !res.FullyPriced {
		t.Fatalf("expected a fully priced portfolio: %+v", res)
	}
	if res.Note != "" || len(res.Unpriced) != 0 || len(res.Errors) != 0 {
		t.Errorf("a healthy valuation carried noise: %+v", res)
	}
	if res.Coverage != 1 {
		t.Errorf("coverage = %v, want 1", res.Coverage)
	}
	// 10×200 + 100×190 = 21000 against a cost of 19500.
	if res.Value != 21000 || res.Cost != 19500 || res.PnL != 1500 {
		t.Errorf("value/cost/pnl = %v/%v/%v, want 21000/19500/1500", res.Value, res.Cost, res.PnL)
	}
}

// TestSummarizePortfolioDeduplicatesSymbols checks two lots of the same
// symbol cost one fetch, not two.
func TestSummarizePortfolioDeduplicatesSymbols(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	hist := &stubHistory{bars: map[string][]provider.OHLCV{"AAPL": {dailyBar(day, 200)}}}
	pf := portfolio.Portfolio{Holdings: []portfolio.Holding{
		{Symbol: "AAPL", Quantity: 10, CostBasis: 150},
		{Symbol: "AAPL", Quantity: 5, CostBasis: 160},
	}}

	summarizePortfolio(context.Background(), nil, hist, pf)
	if hist.callCount() != 1 {
		t.Errorf("fetched %d times for one distinct symbol", hist.callCount())
	}
}

// TestFetchQuotesRunsConcurrently is the regression test for the serial
// fetch: a portfolio with thirty positions took thirty sequential round
// trips, well past where an MCP client gives up on the call.
func TestFetchQuotesRunsConcurrently(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	bars := map[string][]provider.OHLCV{}
	symbols := make([]string, 0, 12)
	for i := range 12 {
		sym := fmt.Sprintf("SYM%02d", i)
		symbols = append(symbols, sym)
		bars[sym] = []provider.OHLCV{dailyBar(day, float64(100+i))}
	}
	hist := &stubHistory{bars: bars, delay: 20 * time.Millisecond}

	quotes, errs := fetchQuotes(context.Background(), nil, hist, symbols)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(quotes) != len(symbols) {
		t.Fatalf("priced %d of %d symbols", len(quotes), len(symbols))
	}
	peak := atomic.LoadInt32(&hist.peak)
	if peak < 2 {
		t.Errorf("peak concurrency = %d: fetches ran serially", peak)
	}
	if peak > portfolioFetchWorkers {
		t.Errorf("peak concurrency = %d, exceeds the %d-worker bound", peak, portfolioFetchWorkers)
	}
}

// TestFetchQuotesOnEmptyInputDoesNothing guards the degenerate case: an empty
// portfolio must not deadlock on a worker pool with no work.
func TestFetchQuotesOnEmptyInputDoesNothing(t *testing.T) {
	quotes, errs := fetchQuotes(context.Background(), nil, &stubHistory{}, nil)
	if len(quotes) != 0 || len(errs) != 0 {
		t.Errorf("fetchQuotes(nil) = %v, %v", quotes, errs)
	}
}

// ──────────────────────────────── helpers ────────────────────────────────

// liveClientForTest builds a liveQuoteClient pointed at a test server.
func liveClientForTest(t *testing.T, base, token string) *liveQuoteClient {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("listen", base, "")
	cmd.Flags().String("listen-token", token, "")
	c := liveClientFromFlags(cmd)
	if c == nil {
		t.Fatalf("liveClientFromFlags(%q) = nil", base)
	}
	return c
}
