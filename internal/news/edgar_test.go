package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"
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

// A filing title is attacker-influenceable free text that mkt renders into
// a terminal frame. encoding/xml rejects a document carrying ESC, raw or as
// a character entity, so an ANSI sequence cannot arrive by this route at
// all. U+202E is well-formed XML and does arrive: it renders the text that
// follows it right-to-left, so a stored title and a displayed title stop
// agreeing. Sanitizing at parse time covers that and does not rely on the
// decoder staying strict.
var hostileAtom = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
	"<feed xmlns=\"http://www.w3.org/2005/Atom\">\n" +
	"  <entry>\n" +
	"    <title>8-K \u202Ednuf tsevni</title>\n" +
	"    <link rel=\"alternate\" href=\"https://www.sec.gov/Archives/8K-1.html\"/>\n" +
	"    <updated>2024-08-02T16:30:42-04:00</updated>\n" +
	"    <category term=\"8-K\u200b\"/>\n" +
	"  </entry>\n" +
	"</feed>"

func TestFetchEDGARStripsFormattingCharacters(t *testing.T) {
	srv := newAtomServer(t, hostileAtom, http.StatusOK)
	defer srv.Close()

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	defer func() { EDGARBaseURL = prev }()

	items := FetchEDGAR(context.Background(), []string{"AAPL"}, 0)
	if len(items) != 1 {
		t.Fatalf("got %d headlines, want 1", len(items))
	}
	for _, field := range []string{items[0].Title, items[0].Category, items[0].Source} {
		for _, r := range field {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				t.Fatalf("U+%04X reached a rendered field: %q", r, field)
			}
		}
	}
	if !strings.Contains(items[0].Title, "8-K") {
		t.Fatalf("sanitizing dropped the readable text: %q", items[0].Title)
	}
	if items[0].Category != "8-K" {
		t.Fatalf("category: got %q want 8-K", items[0].Category)
	}
}
