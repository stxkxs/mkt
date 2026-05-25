package yahoo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider/calendar"
)

const sampleEarnings = `{
  "quoteSummary": {
    "result": [{
      "calendarEvents": {
        "earnings": {
          "earningsDate": [
            {"raw": 1764000000, "fmt": "2025-11-24"},
            {"raw": 1764086400, "fmt": "2025-11-25"}
          ]
        }
      }
    }]
  }
}`

const emptyEarnings = `{"quoteSummary":{"result":[{"calendarEvents":{"earnings":{}}}]}}`

func TestFetchEarningsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, sampleEarnings)
	}))
	defer srv.Close()
	prev := QuoteSummaryURL
	QuoteSummaryURL = srv.URL
	defer func() { QuoteSummaryURL = prev }()

	p := New(15 * time.Second)
	got, err := p.FetchEarnings(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatalf("FetchEarnings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if got[0].Symbol != "AAPL" || got[0].Type != calendar.Earnings {
		t.Errorf("got %+v", got[0])
	}
	if got[0].Time.Unix() != 1764000000 {
		t.Errorf("time = %v, want 1764000000", got[0].Time.Unix())
	}
	// Sorted ascending
	if got[1].Time.Before(got[0].Time) {
		t.Errorf("results not sorted ascending")
	}
}

func TestFetchEarningsEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, emptyEarnings)
	}))
	defer srv.Close()
	prev := QuoteSummaryURL
	QuoteSummaryURL = srv.URL
	defer func() { QuoteSummaryURL = prev }()

	p := New(15 * time.Second)
	got, err := p.FetchEarnings(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatalf("FetchEarnings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestFetchEarningsBadStatusReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	prev := QuoteSummaryURL
	QuoteSummaryURL = srv.URL
	defer func() { QuoteSummaryURL = prev }()

	p := New(15 * time.Second)
	got, err := p.FetchEarnings(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatalf("FetchEarnings should swallow per-ticker failures, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestFetchEarningsEmptyTickers(t *testing.T) {
	p := New(15 * time.Second)
	got, err := p.FetchEarnings(context.Background(), nil)
	if err != nil || got != nil {
		t.Errorf("nil tickers should be a no-op, got %v %v", got, err)
	}
}

func TestEarningsAdapterImplementsInterface(t *testing.T) {
	var _ calendar.EarningsSource = EarningsAdapter{P: New(15 * time.Second)}
}

func TestFetchEarningsMultipleTickersAggregated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "AAPL"):
			_, _ = fmt.Fprint(w, sampleEarnings)
		case strings.Contains(r.URL.Path, "MSFT"):
			_, _ = fmt.Fprint(w, `{"quoteSummary":{"result":[{"calendarEvents":{"earnings":{"earningsDate":[{"raw":1765000000,"fmt":"2025-12-06"}]}}}]}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	prev := QuoteSummaryURL
	QuoteSummaryURL = srv.URL
	defer func() { QuoteSummaryURL = prev }()

	p := New(15 * time.Second)
	got, err := p.FetchEarnings(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatalf("FetchEarnings: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events (2 AAPL + 1 MSFT), got %d", len(got))
	}
}
