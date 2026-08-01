package yahoo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// TestBatchFailureDoesNotAmplify pins the fix for the retry storm: a failing
// batch endpoint must not turn one request into one request per symbol.
func TestBatchFailureDoesNotAmplify(t *testing.T) {
	var batchHits, chartHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"crumb":"testcrumb"}`))
	})
	mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
		batchHits.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/v8/finance/chart/", func(w http.ResponseWriter, r *http.Request) {
		chartHits.Add(1)
		sym := r.URL.Path[len("/v8/finance/chart/"):]
		_, _ = w.Write([]byte(chartFixture(sym, 110, 200, []float64{100, 110})))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapURLs(srv.URL)()

	symbols := make([]string, 120) // three batches: 50 + 50 + 20
	for i := range symbols {
		symbols[i] = fmt.Sprintf("SYM%d", i)
	}

	p := New(time.Second)
	out := make(chan provider.Quote, len(symbols))
	p.fetchAndSend(context.Background(), symbols, out)
	close(out)

	batches := (len(symbols) + batchSize - 1) / batchSize
	if want := batches * policy.attempts; int(batchHits.Load()) != want {
		t.Errorf("batch requests = %d, want %d (%d batches × %d attempts)",
			batchHits.Load(), want, batches, policy.attempts)
	}
	if max := batches * maxChartFallbacks; int(chartHits.Load()) > max {
		t.Errorf("chart fallback issued %d requests for %d symbols, want <= %d",
			chartHits.Load(), len(symbols), max)
	}
	if chartHits.Load() == 0 {
		t.Error("chart fallback never ran; bounded fallback should still cover some symbols")
	}
	var got int
	for range out {
		got++
	}
	if got != int(chartHits.Load()) {
		t.Errorf("emitted %d quotes for %d chart requests", got, chartHits.Load())
	}
}

// TestFetchAndSendStopsOnContextCancel makes sure a cancelled context ends
// the poll instead of grinding through the fallback for every batch.
func TestFetchAndSendStopsOnContextCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	mux.HandleFunc("/v8/finance/chart/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapURLs(srv.URL)()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := New(time.Second)
	out := make(chan provider.Quote, 1)
	done := make(chan struct{})
	go func() { p.fetchAndSend(ctx, []string{"AAPL", "MSFT"}, out); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fetchAndSend ignored a cancelled context")
	}
}

// TestSummaryEscapesSymbolAndCrumb covers the one Yahoo call that used to
// interpolate both values raw.
func TestSummaryEscapesSymbolAndCrumb(t *testing.T) {
	var gotPath, gotCrumb, gotModules string
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"crumb":"ab/c&d=e"}`))
	})
	mux.HandleFunc("/v10/finance/quoteSummary/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCrumb = r.URL.Query().Get("crumb")
		gotModules = r.URL.Query().Get("modules")
		_, _ = w.Write([]byte(`{"quoteSummary":{"result":[{"summaryDetail":{"marketCap":{"raw":1}}}]}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapURLs(srv.URL)()

	p := New(time.Second)
	if _, err := p.FetchSummary(context.Background(), "BRK&A B"); err != nil {
		t.Fatalf("FetchSummary: %v", err)
	}
	if want := "/v10/finance/quoteSummary/BRK&A B"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotCrumb != "ab/c&d=e" {
		t.Errorf("crumb = %q, want %q", gotCrumb, "ab/c&d=e")
	}
	if gotModules != "summaryDetail,defaultKeyStatistics,summaryProfile" {
		t.Errorf("modules = %q — an unescaped symbol or crumb corrupted the query", gotModules)
	}
}

// TestConcurrentFetchesRaceOnCrumb drives every crumb reader and writer at
// once against a server that 401s the first request (forcing a crumb reset)
// so `go test -race` can prove the accessor covers them all.
func TestConcurrentFetchesRaceOnCrumb(t *testing.T) {
	var first atomic.Bool
	body := func(w http.ResponseWriter, payload string) {
		if first.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(payload))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"crumb":"testcrumb"}`))
	})
	mux.HandleFunc("/getcrumb", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("testcrumb"))
	})
	mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
		body(w, `{"quoteResponse":{"result":[{"symbol":"AAPL","regularMarketPrice":201.5}]}}`)
	})
	mux.HandleFunc("/v8/finance/chart/", func(w http.ResponseWriter, r *http.Request) {
		sym := strings.TrimPrefix(r.URL.Path, "/v8/finance/chart/")
		body(w, chartFixture(sym, 110, 200, []float64{100, 110}))
	})
	mux.HandleFunc("/v10/finance/quoteSummary/", func(w http.ResponseWriter, r *http.Request) {
		body(w, `{"quoteSummary":{"result":[{"summaryDetail":{"marketCap":{"raw":1}}}]}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapURLs(srv.URL)()

	prevSummary := QuoteSummaryURL
	QuoteSummaryURL = srv.URL + "/v10/finance/quoteSummary"
	defer func() { QuoteSummaryURL = prevSummary }()

	p := New(time.Second)
	ctx := context.Background()
	out := make(chan provider.Quote, 512)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sym := fmt.Sprintf("SYM%d", n)
			_, _ = p.fetchBatchQuotes(ctx, []string{sym})
			_, _ = p.fetchQuoteViaChart(ctx, sym)
			_, _ = p.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d})
			_, _ = p.FetchSummary(ctx, sym)
			_, _ = p.FetchEarnings(ctx, []string{sym})
			p.fetchAndSend(ctx, []string{sym}, out)
			_ = p.FetchMacroQuotes(ctx)
			_ = p.crumbValue()
		}(i)
	}
	wg.Wait()

	if p.crumbValue() == "" {
		t.Error("crumb should have been re-established after the 401 reset")
	}
}
