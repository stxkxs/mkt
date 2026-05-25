package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/observe"
	"github.com/stxkxs/mkt/internal/provider"
)

func newTestServer(t *testing.T) (*market.Cache, *Server) {
	t.Helper()
	cache := market.NewCache(60)
	cache.Push(provider.Quote{Symbol: "AAPL", Price: 200})
	cache.Push(provider.Quote{Symbol: "BTC-USD", Price: 60000})
	s := New(":0", cache, nil)
	return cache, s
}

func TestQuotes(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleQuotes(rec, httptest.NewRequest("GET", "/quotes", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
}

func TestQuoteSingle(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleQuote(rec, httptest.NewRequest("GET", "/quotes/AAPL", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["symbol"] != "AAPL" {
		t.Errorf("got %+v", got)
	}
}

func TestQuoteUnknown(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleQuote(rec, httptest.NewRequest("GET", "/quotes/NOPE", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestMetrics(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "mkt_uptime_seconds") {
		t.Errorf("missing uptime metric: %s", body)
	}
	if !strings.Contains(string(body), "mkt_symbols_cached 2") {
		t.Errorf("missing/wrong symbols metric: %s", body)
	}
}

func TestAuthRequiredWhenTokenSet(t *testing.T) {
	_, s := newTestServer(t)
	s.WithToken("hunter2")
	h := s.auth(s.handleQuotes)

	noAuth := httptest.NewRecorder()
	h(noAuth, httptest.NewRequest("GET", "/quotes", nil))
	if noAuth.Code != http.StatusUnauthorized {
		t.Errorf("missing token: want 401, got %d", noAuth.Code)
	}

	wrong := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/quotes", nil)
	req.Header.Set("Authorization", "Bearer nope")
	h(wrong, req)
	if wrong.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: want 401, got %d", wrong.Code)
	}

	ok := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/quotes", nil)
	req2.Header.Set("Authorization", "Bearer hunter2")
	h(ok, req2)
	if ok.Code != http.StatusOK {
		t.Errorf("correct token: want 200, got %d", ok.Code)
	}

	queryOK := httptest.NewRecorder()
	h(queryOK, httptest.NewRequest("GET", "/quotes?token=hunter2", nil))
	if queryOK.Code != http.StatusOK {
		t.Errorf("query token: want 200, got %d", queryOK.Code)
	}
}

func TestAuthDisabledWhenTokenEmpty(t *testing.T) {
	_, s := newTestServer(t)
	h := s.auth(s.handleQuotes)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/quotes", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("no token configured: want 200, got %d", rec.Code)
	}
}

func TestTradingViewBodyTooLarge(t *testing.T) {
	cache := market.NewCache(60)
	engine := alert.NewEngine(0, nil)
	s := New(":0", cache, engine)
	rec := httptest.NewRecorder()
	big := strings.Repeat("a", maxWebhookBytes+10)
	req := httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(`{"symbol":"X","alert":"`+big+`"}`))
	s.handleTradingView(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: want 413, got %d", rec.Code)
	}
}

func TestMetricsIncludesRegisteredCounters(t *testing.T) {
	// Register a counter and bump it; /metrics should emit it in
	// Prometheus text format with TYPE annotation.
	c := observe.NewCounter("mkt_test_api_metrics_counter_total")
	c.Inc()
	c.Inc()

	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "# TYPE mkt_test_api_metrics_counter_total counter") {
		t.Errorf("missing TYPE line: %s", body)
	}
	if !strings.Contains(body, "mkt_test_api_metrics_counter_total 2") {
		t.Errorf("expected counter value 2, body:\n%s", body)
	}
}

func TestTradingViewLooseDecodeAcceptsExtraFields(t *testing.T) {
	cache := market.NewCache(60)
	engine := alert.NewEngine(0, nil)
	s := New(":0", cache, engine)
	// includes an unknown "exchange" field that strict decode would reject;
	// the loose-decode fallback must accept it.
	body := `{"symbol":"AAPL","price":201.5,"exchange":"NASDAQ"}`
	rec := httptest.NewRecorder()
	s.handleTradingView(rec, httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Errorf("loose decode: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}
