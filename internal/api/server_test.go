package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/market"
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
