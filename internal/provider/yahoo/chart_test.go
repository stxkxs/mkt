package yahoo

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// chartFixture builds a v8 chart payload with the given meta price, meta
// chartPreviousClose and close series.
func chartFixture(sym string, price, chartPrevClose float64, closes []float64) string {
	ts := make([]string, len(closes))
	oc := make([]string, len(closes))
	for i, c := range closes {
		ts[i] = fmt.Sprintf("%d", 1700000000+int64(i)*86400)
		oc[i] = fmt.Sprintf("%g", c)
	}
	return fmt.Sprintf(`{"chart":{"result":[{
		"meta":{"symbol":%q,"regularMarketPrice":%g,"chartPreviousClose":%g},
		"timestamp":[%s],
		"indicators":{"quote":[{"open":[%s],"close":[%s],"high":[%s],"low":[%s],"volume":[%s]}]}
	}]}}`, sym, price, chartPrevClose,
		join(ts), join(oc), join(oc), join(oc), join(oc), join(oc))
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func chartServer(t *testing.T, body func(sym string) string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"crumb":"testcrumb"}`))
	})
	mux.HandleFunc("/v8/finance/chart/", func(w http.ResponseWriter, r *http.Request) {
		sym := r.URL.Path[len("/v8/finance/chart/"):]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body(sym)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(swapURLs(srv.URL))
	return srv
}

func TestChartQuoteUsesPreviousBarClose(t *testing.T) {
	// chartPreviousClose (200) is the close *before the requested range*.
	// The daily change must be measured against the previous bar (100).
	chartServer(t, func(sym string) string {
		return chartFixture(sym, 110, 200, []float64{100, 110})
	})

	p := New(time.Second)
	q, err := p.fetchQuoteViaChart(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("fetchQuoteViaChart: %v", err)
	}
	if q.Change != 10 {
		t.Errorf("change = %v, want 10 (110 - previous bar 100)", q.Change)
	}
	if math.Abs(q.ChangePct-10) > 1e-9 {
		t.Errorf("changePct = %v, want 10", q.ChangePct)
	}
}

func TestChartQuoteFallsBackToChartPreviousCloseForSingleBar(t *testing.T) {
	chartServer(t, func(sym string) string {
		return chartFixture(sym, 110, 100, []float64{110})
	})

	p := New(time.Second)
	q, err := p.fetchQuoteViaChart(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("fetchQuoteViaChart: %v", err)
	}
	if q.Change != 10 {
		t.Errorf("change = %v, want 10 (single bar → chartPreviousClose)", q.Change)
	}
}

func TestChartQuoteRejectsMissingPrice(t *testing.T) {
	chartServer(t, func(sym string) string {
		return `{"chart":{"result":[{"meta":{"symbol":"HALT","chartPreviousClose":70.5},
			"timestamp":[1700000000],"indicators":{"quote":[{"open":[70.5],"close":[70.5],"high":[70.5],"low":[70.5],"volume":[1]}]}}]}}`
	})

	p := New(time.Second)
	q, err := p.fetchQuoteViaChart(context.Background(), "HALT")
	if err == nil {
		t.Fatalf("expected an error, got phantom quote %+v", q)
	}
	if q.Price != 0 || q.ChangePct != 0 {
		t.Errorf("quote should be zero-valued on error, got %+v", q)
	}
}

func TestFetchMacroQuotesSkipsPricelessSymbols(t *testing.T) {
	chartServer(t, func(sym string) string {
		if sym == url.PathEscape("^VIX") || sym == "^VIX" {
			// No regularMarketPrice — the phantom-quote shape.
			return `{"chart":{"result":[{"meta":{"symbol":"^VIX","chartPreviousClose":18},"timestamp":[],"indicators":{"quote":[{}]}}]}}`
		}
		return chartFixture(sym, 110, 200, []float64{100, 110})
	})

	p := New(time.Second)
	quotes := p.FetchMacroQuotes(context.Background())
	if len(quotes) != len(MacroSymbols)-1 {
		t.Fatalf("got %d quotes, want %d (VIX dropped)", len(quotes), len(MacroSymbols)-1)
	}
	for _, q := range quotes {
		if q.Price <= 0 {
			t.Errorf("macro quote with no price leaked: %+v", q)
		}
	}
}

func TestHistoryAppliesLimit(t *testing.T) {
	closes := make([]float64, 300)
	for i := range closes {
		closes[i] = float64(i + 1)
	}
	chartServer(t, func(sym string) string {
		return chartFixture(sym, closes[len(closes)-1], 0, closes)
	})

	p := New(time.Second)
	candles, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "AAPL", Interval: provider.Interval1d, Limit: 50,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(candles) != 50 {
		t.Fatalf("got %d candles, want 50", len(candles))
	}
	// The limit keeps the most recent bars.
	if candles[len(candles)-1].Close != closes[len(closes)-1] {
		t.Errorf("last close = %v, want %v", candles[len(candles)-1].Close, closes[len(closes)-1])
	}
	if candles[0].Close != closes[len(closes)-50] {
		t.Errorf("first close = %v, want %v", candles[0].Close, closes[len(closes)-50])
	}
}

func TestHistoryNoLimitReturnsEverything(t *testing.T) {
	chartServer(t, func(sym string) string {
		return chartFixture(sym, 3, 0, []float64{1, 2, 3})
	})

	p := New(time.Second)
	candles, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "AAPL", Interval: provider.Interval1d,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(candles) != 3 {
		t.Fatalf("got %d candles, want 3", len(candles))
	}
}

func TestHistoryToleratesShortHighLowVolume(t *testing.T) {
	// open/close carry three bars; high/low/volume are truncated. Yahoo
	// declares them as independent arrays, so this must not panic.
	chartServer(t, func(sym string) string {
		return `{"chart":{"result":[{"meta":{"symbol":"AAPL","regularMarketPrice":3},
			"timestamp":[1700000000,1700086400,1700172800],
			"indicators":{"quote":[{"open":[1,2,3],"close":[1,2,3],"high":[1],"low":[1],"volume":[10]}]}}]}}`
	})

	p := New(time.Second)
	candles, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "AAPL", Interval: provider.Interval1d,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(candles) != 3 {
		t.Fatalf("got %d candles, want 3", len(candles))
	}
	if candles[0].High != 1 || candles[0].Volume != 10 {
		t.Errorf("first candle = %+v", candles[0])
	}
	if candles[2].High != 0 || candles[2].Low != 0 || candles[2].Volume != 0 {
		t.Errorf("missing high/low/volume should read 0, got %+v", candles[2])
	}
}

func TestHistoryWithMetaReportsServedInterval(t *testing.T) {
	var gotInterval atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"crumb":"testcrumb"}`))
	})
	mux.HandleFunc("/v8/finance/chart/", func(w http.ResponseWriter, r *http.Request) {
		gotInterval.Store(r.URL.Query().Get("interval"))
		_, _ = w.Write([]byte(chartFixture("AAPL", 3, 0, []float64{1, 2, 3})))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapURLs(srv.URL)()

	p := New(time.Second)
	res, err := p.HistoryWithMeta(context.Background(), provider.HistoryParams{
		Symbol: "AAPL", Interval: provider.Interval4h,
	})
	if err != nil {
		t.Fatalf("HistoryWithMeta: %v", err)
	}
	if got := gotInterval.Load(); got != "1h" {
		t.Errorf("requested interval %v upstream, want 1h", got)
	}
	if res.Requested != provider.Interval4h {
		t.Errorf("Requested = %s, want 4h", res.Requested)
	}
	if res.Interval != provider.Interval1h {
		t.Errorf("Interval = %s, want 1h — Yahoo has no 4h bucket", res.Interval)
	}
	if len(res.Candles) != 3 {
		t.Errorf("got %d candles, want 3", len(res.Candles))
	}
}

func TestServedInterval(t *testing.T) {
	cases := map[provider.Interval]provider.Interval{
		provider.Interval1m:           provider.Interval1m,
		provider.Interval5m:           provider.Interval5m,
		provider.Interval15m:          provider.Interval15m,
		provider.Interval1h:           provider.Interval1h,
		provider.Interval4h:           provider.Interval1h,
		provider.Interval1d:           provider.Interval1d,
		provider.Interval1w:           provider.Interval1w,
		provider.Interval("nonsense"): provider.Interval1d,
	}
	for in, want := range cases {
		if got := ServedInterval(in); got != want {
			t.Errorf("ServedInterval(%s) = %s, want %s", in, got, want)
		}
	}
}
