package yahoo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// swapURLs points every Yahoo endpoint at a test server and returns a
// restore func. The package-level URL vars are why the core quote path is
// now testable without network access.
func swapURLs(base string) func() {
	ob, oc, os_, ocr := baseURL, chartURL, sessionURL, crumbURL
	baseURL = base
	chartURL = base + "/v8/finance/chart"
	sessionURL = base + "/session"
	crumbURL = base + "/getcrumb"
	return func() { baseURL, chartURL, sessionURL, crumbURL = ob, oc, os_, ocr }
}

func testYahooServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><script>root.App.main = {"crumb":"testcrumb"};</script></html>`))
	})
	mux.HandleFunc("/getcrumb", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("testcrumb"))
	})
	mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quoteResponse":{"result":[
			{"symbol":"AAPL","regularMarketPrice":201.5,"regularMarketChange":1.5,"regularMarketChangePercent":0.75,"regularMarketVolume":1000,"regularMarketDayHigh":202,"regularMarketDayLow":199}
		]}}`))
	})
	return httptest.NewServer(mux)
}

func TestFetchBatchQuotes(t *testing.T) {
	srv := testYahooServer()
	defer srv.Close()
	defer swapURLs(srv.URL)()

	p := New(5 * time.Second)
	quotes, err := p.fetchBatchQuotes(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatalf("fetchBatchQuotes: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(quotes))
	}
	q := quotes[0]
	if q.Symbol != "AAPL" || q.Price != 201.5 || q.High24h != 202 || q.Low24h != 199 {
		t.Errorf("quote = %+v", q)
	}
	if q.Asset != provider.AssetStock || q.Provider != "yahoo" {
		t.Errorf("asset/provider = %v/%s", q.Asset, q.Provider)
	}
}

func TestFetchBatchQuotesResetsCrumbOn401(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapURLs(srv.URL)()

	p := New(5 * time.Second)
	p.setCrumb("stale")
	if _, err := p.fetchBatchQuotes(context.Background(), []string{"AAPL"}); err == nil {
		t.Fatal("expected error on 401")
	}
	if got := p.crumbValue(); got != "" {
		t.Errorf("crumb should be cleared after 401, got %q", got)
	}
}

func TestSubscribeEmitsQuote(t *testing.T) {
	srv := testYahooServer()
	defer srv.Close()
	defer swapURLs(srv.URL)()

	p := New(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan provider.Quote, 4)
	done := make(chan struct{})
	go func() { _ = p.Subscribe(ctx, []string{"AAPL"}, out); close(done) }()

	select {
	case q := <-out:
		if q.Symbol != "AAPL" {
			t.Errorf("subscribe quote = %+v", q)
		}
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("Subscribe emitted no quote")
	}
	cancel()
	<-done // ensure the goroutine stops before swapURLs restores the vars
}
