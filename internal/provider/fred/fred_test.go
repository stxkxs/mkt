package fred

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

const sampleCSV = `observation_date,DFF
2024-01-02,5.33
2024-01-03,5.32
2024-01-04,.
2024-01-05,5.34
`

func newCSVServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestSupports(t *testing.T) {
	p := New()
	if !p.Supports("FRED:DFF") {
		t.Error("FRED:DFF should be supported")
	}
	if p.Supports("AAPL") {
		t.Error("AAPL should not be supported")
	}
	if p.Supports("") {
		t.Error("empty should not be supported")
	}
}

func TestHistoryHappyPath(t *testing.T) {
	srv := newCSVServer(t, sampleCSV, 200)
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	got, err := p.History(context.Background(), provider.HistoryParams{Symbol: "FRED:DFF"})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// 4 data rows in the CSV, one is "." → 3 valid
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	if got[0].Close != 5.33 || got[2].Close != 5.34 {
		t.Errorf("unexpected close values: %v %v", got[0].Close, got[2].Close)
	}
	if got[0].Time != time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) {
		t.Errorf("unexpected first date: %v", got[0].Time)
	}
	// open/high/low all equal close
	if got[0].Open != got[0].Close || got[0].High != got[0].Close || got[0].Low != got[0].Close {
		t.Errorf("ohlc should all equal close: %+v", got[0])
	}
}

func TestHistoryLimit(t *testing.T) {
	srv := newCSVServer(t, sampleCSV, 200)
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	got, err := p.History(context.Background(), provider.HistoryParams{Symbol: "FRED:DFF", Limit: 2})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Limit=2 should yield 2 rows, got %d", len(got))
	}
	if got[1].Close != 5.34 {
		t.Errorf("expected most recent close 5.34, got %v", got[1].Close)
	}
}

func TestHistoryDateFilter(t *testing.T) {
	srv := newCSVServer(t, sampleCSV, 200)
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	start := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	got, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "FRED:DFF",
		Start:  start,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// Start filters to 2024-01-03 onwards: rows for 1/3 and 1/5 are valid
	if len(got) != 2 {
		t.Fatalf("start-filter should yield 2 rows, got %d", len(got))
	}
}

func TestHistory404(t *testing.T) {
	srv := newCSVServer(t, "", 404)
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	_, err := p.History(context.Background(), provider.HistoryParams{Symbol: "FRED:NOPE"})
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestHistoryEmptySeriesID(t *testing.T) {
	p := New()
	_, err := p.History(context.Background(), provider.HistoryParams{Symbol: "FRED:"})
	if err == nil {
		t.Fatal("expected error for empty series id")
	}
}

func TestName(t *testing.T) {
	if New().Name() != "fred" {
		t.Error("Name() should be 'fred'")
	}
}
