package fred

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
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
	cases := []struct {
		sym  string
		want bool
	}{
		{"FRED:DFF", true},
		{"fred:dgs10", true}, // case-insensitive: used to route nowhere
		{"Fred:CPIAUCSL", true},
		{"AAPL", false},
		{"", false},
		{"BTC-USD", false},
	}
	for _, c := range cases {
		if got := p.Supports(c.sym); got != c.want {
			t.Errorf("Supports(%q) = %v, want %v", c.sym, got, c.want)
		}
	}
}

func TestHistoryLowercaseSymbolRoutesAndUppercasesSeries(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.URL.Query().Get("id")
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	got, err := p.History(context.Background(), provider.HistoryParams{Symbol: "fred:dff"})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if gotID != "DFF" {
		t.Errorf("series id sent upstream = %q, want %q", gotID, "DFF")
	}
	if len(got) != 3 {
		t.Errorf("want 3 rows, got %d", len(got))
	}
}

func TestHistoryEscapesSeriesID(t *testing.T) {
	// A series id carrying query syntax must not be able to append
	// parameters to the fredgraph request.
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	// Rejected outright by the charset check.
	if _, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "FRED:DFF&cosd=1900-01-01",
	}); err == nil {
		t.Fatal("expected error for series id with query syntax")
	}
	if raw != "" {
		t.Errorf("request should never have been made, got query %q", raw)
	}
}

func TestSeriesIDRejectsMalformed(t *testing.T) {
	bad := []string{
		"FRED:",
		"FRED:  ",
		"FRED:DFF/../etc",
		"FRED:DFF DGS10",
		"AAPL",
		"",
	}
	for _, s := range bad {
		if _, err := seriesID(s); err == nil {
			t.Errorf("seriesID(%q) should have failed", s)
		}
	}
	got, err := seriesID(" fred:dgs10 ")
	if err != nil {
		t.Fatalf("seriesID: %v", err)
	}
	if got != "DGS10" {
		t.Errorf("seriesID = %q, want DGS10", got)
	}
}

func TestParseCSVCapsObservations(t *testing.T) {
	var b strings.Builder
	b.WriteString("observation_date,DFF\n")
	day := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= maxObservations; i++ {
		b.WriteString(day.AddDate(0, 0, i).Format("2006-01-02"))
		b.WriteString(",1.0\n")
	}
	if _, err := parseCSV(strings.NewReader(b.String())); err == nil {
		t.Fatal("expected error once past maxObservations")
	}
}

func TestHistoryBodyIsCappedByHTTPX(t *testing.T) {
	// httpx.Get truncates at MaxResponseBytes; verify fred goes through it
	// rather than reading an unbounded stream. Serving a body that never
	// ends would hang a provider that used its own io.ReadAll.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("observation_date,DFF\n2024-01-02,5.33\n"))
		f, ok := w.(http.Flusher)
		if !ok {
			return
		}
		f.Flush()
		chunk := bytes.Repeat([]byte("2024-01-02,1.0\n"), 4096)
		written := 0
		for written < httpx.MaxResponseBytes+len(chunk) {
			n, err := w.Write(chunk)
			if err != nil {
				return
			}
			written += n
			f.Flush()
		}
	}))
	defer srv.Close()

	p := New()
	p.SetBaseURL(srv.URL)

	// httpx truncates at 16 MiB, which is still ~1M short rows, so the
	// observation cap trips too. What matters is that this returns at all,
	// with a bounded allocation, instead of streaming into memory.
	_, err := p.History(context.Background(), provider.HistoryParams{Symbol: "FRED:DFF"})
	if err == nil {
		t.Fatal("expected the capped body to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "observations") {
		t.Errorf("unexpected error: %v", err)
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
