package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>8-K - Current report</title>
    <link rel="alternate" href="https://www.sec.gov/Archives/8K-1.html"/>
    <updated>2024-08-02T16:30:42-04:00</updated>
    <category term="8-K"/>
  </entry>
  <entry>
    <title>10-Q - Quarterly report</title>
    <link rel="alternate" href="https://www.sec.gov/Archives/10Q-1.html"/>
    <updated>2024-08-01T12:00:00-04:00</updated>
    <category term="10-Q"/>
  </entry>
  <entry>
    <title></title>
    <link rel="alternate" href="https://www.sec.gov/Archives/empty.html"/>
    <updated>2024-08-01T12:00:00-04:00</updated>
    <category term="bogus"/>
  </entry>
</feed>`

func newAtomServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestFetchEDGARHappyPath(t *testing.T) {
	srv := newAtomServer(t, sampleAtom, 200)
	defer srv.Close()

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	defer func() { EDGARBaseURL = prev }()

	got := FetchEDGAR(context.Background(), []string{"AAPL"}, 0)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (empty-title skipped), got %d", len(got))
	}
	if !strings.HasPrefix(got[0].Source, "SEC:AAPL") {
		t.Errorf("source missing SEC prefix: %q", got[0].Source)
	}
	if got[0].Category != "8-K" {
		t.Errorf("category: got %q want 8-K", got[0].Category)
	}
	if got[0].PubTime.IsZero() {
		t.Errorf("PubTime should parse RFC3339")
	}
	// Sorted descending by time → 8-K (newer) first
	if got[0].PubTime.Before(got[1].PubTime) {
		t.Errorf("entries not sorted descending")
	}
}

func TestFetchEDGARLimit(t *testing.T) {
	srv := newAtomServer(t, sampleAtom, 200)
	defer srv.Close()

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	defer func() { EDGARBaseURL = prev }()

	got := FetchEDGAR(context.Background(), []string{"AAPL"}, 1)
	if len(got) != 1 {
		t.Fatalf("limit=1 should yield 1 entry, got %d", len(got))
	}
}

func TestFetchEDGAREmptyTickers(t *testing.T) {
	got := FetchEDGAR(context.Background(), nil, 0)
	if got != nil {
		t.Errorf("nil tickers should yield nil, got %v", got)
	}
	got = FetchEDGAR(context.Background(), []string{""}, 0)
	if got != nil {
		t.Errorf("blank ticker should yield nil, got %v", got)
	}
}

func TestFetchEDGARBadStatus(t *testing.T) {
	srv := newAtomServer(t, "", 500)
	defer srv.Close()

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	defer func() { EDGARBaseURL = prev }()

	got := FetchEDGAR(context.Background(), []string{"AAPL"}, 0)
	if len(got) != 0 {
		t.Errorf("expected empty on 500, got %d", len(got))
	}
}

func TestFetchEDGARMalformedXML(t *testing.T) {
	srv := newAtomServer(t, "this is not xml", 200)
	defer srv.Close()

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	defer func() { EDGARBaseURL = prev }()

	got := FetchEDGAR(context.Background(), []string{"AAPL"}, 0)
	if len(got) != 0 {
		t.Errorf("expected empty on malformed XML, got %d", len(got))
	}
}
