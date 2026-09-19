package news

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"golang.org/x/time/rate"
)

type rssItemFixture struct {
	title   string
	link    string
	pubDate string
}

// feedXML renders an RSS 2.0 channel around the given items. Fixture text is
// inserted verbatim, so a fixture that needs `&` or `<` must escape it.
func feedXML(items ...rssItemFixture) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<rss version=\"2.0\">\n<channel>\n")
	for _, it := range items {
		fmt.Fprintf(&b, "  <item><title>%s</title><link>%s</link><pubDate>%s</pubDate></item>\n",
			it.title, it.link, it.pubDate)
	}
	b.WriteString("</channel>\n</rss>\n")
	return b.String()
}

func newRSSServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withGate swaps the package limiter for the duration of one test. Every
// fetch path in this package waits on it, so a test that fans out over more
// feeds than the burst either neutralizes it or pays its pacing.
func withGate(t *testing.T, l *rate.Limiter) {
	t.Helper()
	prev := feedGate
	feedGate = l
	t.Cleanup(func() { feedGate = prev })
}

// openGate is a limiter that never delays, for tests measuring something
// other than pacing.
func openGate() *rate.Limiter { return rate.NewLimiter(rate.Inf, 0) }

func TestFetchAllParsesFeed(t *testing.T) {
	withGate(t, openGate())
	srv := newRSSServer(t, feedXML(
		rssItemFixture{"Fed holds rates steady", "https://example.test/fed", "Mon, 05 Aug 2024 14:30:00 -0400"},
		rssItemFixture{"Chips rally on demand", "https://example.test/chips", "Mon, 05 Aug 2024 09:00:00 -0400"},
	), http.StatusOK)

	got := FetchAll(context.Background(), []Feed{{Name: "Wire", URL: srv.URL}})
	if len(got) != 2 {
		t.Fatalf("got %d headlines, want 2", len(got))
	}
	if got[0].Title != "Fed holds rates steady" {
		t.Errorf("title: got %q", got[0].Title)
	}
	if got[0].Link != "https://example.test/fed" {
		t.Errorf("link: got %q", got[0].Link)
	}
	if got[0].Source != "Wire" {
		t.Errorf("source: got %q, want the feed name", got[0].Source)
	}
	if got[0].Category != "" {
		t.Errorf("RSS items carry no category, got %q", got[0].Category)
	}
	want := time.Date(2024, 8, 5, 14, 30, 0, 0, time.FixedZone("", -4*60*60))
	if !got[0].PubTime.Equal(want) {
		t.Errorf("pubTime: got %v, want %v", got[0].PubTime, want)
	}
}

// A feed that dates an item in a format parseTime does not know still yields
// the headline: the text is the payload, the timestamp is decoration, and a
// zero PubTime is what TimeAgo renders as blank.
func TestFetchAllKeepsItemWithUnparsableDate(t *testing.T) {
	withGate(t, openGate())
	srv := newRSSServer(t, feedXML(
		rssItemFixture{"Undated wire copy", "https://example.test/undated", "sometime last week"},
	), http.StatusOK)

	got := FetchAll(context.Background(), []Feed{{Name: "Wire", URL: srv.URL}})
	if len(got) != 1 {
		t.Fatalf("got %d headlines, want 1", len(got))
	}
	if !got[0].PubTime.IsZero() {
		t.Errorf("unparsable date should yield the zero time, got %v", got[0].PubTime)
	}
}

// Syndicated wires republish each other, so the same story arrives from
// several feeds under the same URL. The link is the identity, and the two
// axes are separated here because a fixture that varies both at once is
// satisfied by deduplicating on either: a recurring column headline
// ("Market Wrap") is a different story each day under a different link, and
// two feeds that retitle one wire item still point at the same link.
func TestFetchAllDeduplicatesByURL(t *testing.T) {
	withGate(t, openGate())
	sameLink := "https://example.test/wire"
	a := newRSSServer(t, feedXML(
		rssItemFixture{"Market Wrap", "https://example.test/wrap-monday", "Mon, 05 Aug 2024 14:30:00 -0400"},
		rssItemFixture{"Fed holds rates steady", sameLink, "Mon, 05 Aug 2024 13:00:00 -0400"},
	), http.StatusOK)
	b := newRSSServer(t, feedXML(
		rssItemFixture{"Market Wrap", "https://example.test/wrap-tuesday", "Tue, 06 Aug 2024 14:30:00 -0400"},
		rssItemFixture{"Fed leaves policy unchanged", sameLink, "Mon, 05 Aug 2024 13:00:00 -0400"},
	), http.StatusOK)

	got := FetchAll(context.Background(), []Feed{{Name: "A", URL: a.URL}, {Name: "B", URL: b.URL}})

	byLink := map[string]int{}
	byTitle := map[string]int{}
	for _, h := range got {
		byLink[h.Link]++
		byTitle[h.Title]++
	}
	if n := byLink[sameLink]; n != 1 {
		t.Errorf("one link under two titles kept %d times, want 1: the link is the identity", n)
	}
	if n := byTitle["Market Wrap"]; n != 2 {
		t.Errorf("one title under two links kept %d times, want 2: the title is not the identity", n)
	}
	if len(got) != 3 {
		t.Fatalf("got %d headlines, want 3", len(got))
	}
}

func TestFetchAllSortsNewestFirst(t *testing.T) {
	withGate(t, openGate())
	base := time.Date(2024, 8, 5, 12, 0, 0, 0, time.UTC)
	a := newRSSServer(t, feedXML(
		rssItemFixture{"oldest", "https://example.test/1", base.Format(time.RFC1123Z)},
		rssItemFixture{"newest", "https://example.test/4", base.Add(3 * time.Hour).Format(time.RFC1123Z)},
	), http.StatusOK)
	b := newRSSServer(t, feedXML(
		rssItemFixture{"third", "https://example.test/3", base.Add(2 * time.Hour).Format(time.RFC1123Z)},
		rssItemFixture{"second", "https://example.test/2", base.Add(time.Hour).Format(time.RFC1123Z)},
	), http.StatusOK)

	got := FetchAll(context.Background(), []Feed{{Name: "A", URL: a.URL}, {Name: "B", URL: b.URL}})
	if len(got) != 4 {
		t.Fatalf("got %d headlines, want 4", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].PubTime.Before(got[i].PubTime) {
			t.Fatalf("position %d (%v) is newer than %d (%v)", i, got[i].PubTime, i-1, got[i-1].PubTime)
		}
	}
	if got[0].Title != "newest" || got[3].Title != "oldest" {
		t.Errorf("ends: got %q … %q", got[0].Title, got[3].Title)
	}
}

// The tab renders a fixed-height list; the cap is what keeps an oversized
// feed from turning one poll into an unbounded slice held for the session.
func TestFetchAllCapsAtFifty(t *testing.T) {
	withGate(t, openGate())
	const total = 60
	base := time.Date(2024, 8, 5, 12, 0, 0, 0, time.UTC)
	items := make([]rssItemFixture, total)
	for i := range items {
		// Emitted oldest-first so the cap depends on the sort, not feed order.
		items[i] = rssItemFixture{
			title:   fmt.Sprintf("story %d", total-1-i),
			link:    fmt.Sprintf("https://example.test/%d", total-1-i),
			pubDate: base.Add(-time.Duration(total-1-i) * time.Minute).Format(time.RFC1123Z),
		}
	}
	srv := newRSSServer(t, feedXML(items...), http.StatusOK)

	got := FetchAll(context.Background(), []Feed{{Name: "Wire", URL: srv.URL}})
	if len(got) != 50 {
		t.Fatalf("got %d headlines, want the top 50 of %d", len(got), total)
	}
	if got[0].Link != "https://example.test/0" {
		t.Errorf("first kept: got %q, want the newest item", got[0].Link)
	}
	if got[49].Link != "https://example.test/49" {
		t.Errorf("last kept: got %q, want the 50th newest", got[49].Link)
	}
}

func TestFetchAllNoFeeds(t *testing.T) {
	withGate(t, openGate())
	if got := FetchAll(context.Background(), nil); len(got) != 0 {
		t.Fatalf("no feeds should yield nothing, got %d", len(got))
	}
}

// One broken source is the normal case, not the exceptional one: feed URLs
// rot, and a poll that returns nothing because one of three hosts is down
// empties the tab.
func TestFetchAllIsolatesBrokenFeeds(t *testing.T) {
	healthy := feedXML(rssItemFixture{
		"Healthy story", "https://example.test/ok", "Mon, 05 Aug 2024 14:30:00 -0400",
	})
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"non-2xx", "", http.StatusInternalServerError},
		{"rate limited", "slow down", http.StatusTooManyRequests},
		{"malformed xml", "<rss><channel><item><title>unclosed", http.StatusOK},
		{"not xml at all", "this is not xml", http.StatusOK},
		{"empty body", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withGate(t, openGate())
			broken := newRSSServer(t, tc.body, tc.status)
			good := newRSSServer(t, healthy, http.StatusOK)

			got := FetchAll(context.Background(), []Feed{
				{Name: "Broken", URL: broken.URL},
				{Name: "Good", URL: good.URL},
			})
			if len(got) != 1 {
				t.Fatalf("got %d headlines, want only the healthy feed's one", len(got))
			}
			if got[0].Source != "Good" {
				t.Errorf("source: got %q, want Good", got[0].Source)
			}
		})
	}
}

// A headline is rendered into a terminal frame from text a remote publisher
// controls. encoding/xml rejects ESC outright, raw or as a character entity,
// so no ANSI sequence can arrive by this route; U+202E is well-formed XML
// and does arrive, and it renders what follows it right-to-left so a stored
// title and a displayed title stop agreeing. Sanitizing at parse time covers
// that without relying on the decoder staying strict.
func TestFetchAllNeutralizesHostileTitles(t *testing.T) {
	withGate(t, openGate())
	srv := newRSSServer(t, feedXML(rssItemFixture{
		title:   "Earnings beat \u202Ednuf tsevni\u200B",
		link:    "https://example.test/hostile",
		pubDate: "Mon, 05 Aug 2024 14:30:00 -0400",
	}), http.StatusOK)

	got := FetchAll(context.Background(), []Feed{{Name: "Wire", URL: srv.URL}})
	if len(got) != 1 {
		t.Fatalf("got %d headlines, want 1", len(got))
	}
	for _, r := range got[0].Title {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("U+%04X reached a rendered title: %q", r, got[0].Title)
		}
	}
	if !strings.Contains(got[0].Title, "Earnings beat") {
		t.Fatalf("sanitizing dropped the readable text: %q", got[0].Title)
	}
}

// barrierSettle is how long a barrier keeps holding after the cap is
// saturated. A capped fan-out cannot exceed its cap however long the handler
// waits, so a wider window adds no failure a correct cap can hit; what it
// buys is the only thing that separates capped from uncapped, namely the
// requests an uncapped fan-out has already dispatched but that have not yet
// reached a handler. Releasing at the instant of saturation records a peak
// of exactly the cap in both cases.
const barrierSettle = 250 * time.Millisecond

// barrierTimeout bounds the hold when the cap is never saturated, so a
// fan-out narrower than the cap is reported by the peak assertion rather
// than hanging until the suite deadline.
const barrierTimeout = 2 * time.Second

// barrier is an HTTP handler prelude that holds concurrent requests long
// enough to measure how many the caller runs at once.
type barrier struct {
	limit     int64
	inFlight  atomic.Int64
	peak      atomic.Int64
	releaseAt atomic.Int64
	saturated chan struct{}
	once      sync.Once
}

func newBarrier(limit int) *barrier {
	return &barrier{limit: int64(limit), saturated: make(chan struct{})}
}

// hold blocks until the caller has limit requests in flight at once and the
// settle window has elapsed, recording the high-water mark either way.
func (b *barrier) hold() {
	n := b.inFlight.Add(1)
	defer b.inFlight.Add(-1)
	for {
		p := b.peak.Load()
		if n <= p || b.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if n >= b.limit {
		b.once.Do(func() {
			b.releaseAt.Store(time.Now().Add(barrierSettle).UnixNano())
			close(b.saturated)
		})
	}
	select {
	case <-b.saturated:
	case <-time.After(barrierTimeout):
		return
	}
	if d := time.Until(time.Unix(0, b.releaseAt.Load())); d > 0 {
		time.Sleep(d)
	}
}

// assertPeak fails unless the caller ran exactly limit requests at once:
// above means the cap does not bind, below means the fan-out is serialized.
func (b *barrier) assertPeak(t *testing.T) {
	t.Helper()
	switch p := b.peak.Load(); {
	case p > b.limit:
		t.Fatalf("%d requests in flight at once, cap is %d", p, b.limit)
	case p < b.limit:
		t.Fatalf("peak concurrency %d never reached the cap of %d: the fan-out is serialized", p, b.limit)
	}
}

// The feed list comes from config and has no length bound, so an uncapped
// fan-out turns one poll into as many simultaneous requests as the user has
// feeds. Three times the cap of feeds means an uncapped fan-out overshoots
// by enough that no scheduling order hides it.
func TestFetchAllCapsConcurrency(t *testing.T) {
	withGate(t, openGate())

	b := newBarrier(fetchConcurrency)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.hold()
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(feedXML(rssItemFixture{
			title:   "story " + r.URL.Path,
			link:    "https://example.test" + r.URL.Path,
			pubDate: "Mon, 05 Aug 2024 14:30:00 -0400",
		})))
	}))
	defer srv.Close()

	feeds := make([]Feed, 3*fetchConcurrency)
	for i := range feeds {
		feeds[i] = Feed{Name: fmt.Sprintf("feed-%d", i), URL: fmt.Sprintf("%s/%d", srv.URL, i)}
	}

	got := FetchAll(context.Background(), feeds)
	if len(got) != len(feeds) {
		t.Fatalf("got %d headlines, want one per feed (%d)", len(got), len(feeds))
	}
	b.assertPeak(t)
}

// The ticker list is the watchlist, which is as long as the user makes it,
// and SEC EDGAR blocks a client that fans out past its published rate.
func TestFetchEDGARCapsConcurrency(t *testing.T) {
	withGate(t, openGate())

	b := newBarrier(fetchConcurrency)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.hold()
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(sampleAtom))
	}))
	defer srv.Close()

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	t.Cleanup(func() { EDGARBaseURL = prev })

	tickers := make([]string, 3*fetchConcurrency)
	for i := range tickers {
		tickers[i] = fmt.Sprintf("TKR%d", i)
	}

	if got := FetchEDGAR(context.Background(), tickers, 0); len(got) == 0 {
		t.Fatal("no filings returned, so the peak below measures nothing")
	}
	b.assertPeak(t)
}

// Both fan-outs wait on one limiter so their concurrency caps cannot
// compound into a request rate the upstreams throttle. A closed gate proves
// the wait precedes the request on both paths.
func TestFeedGateIsSharedByBothFanOuts(t *testing.T) {
	withGate(t, rate.NewLimiter(0, 0))

	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(feedXML(rssItemFixture{
			"ungated", "https://example.test/ungated", "Mon, 05 Aug 2024 14:30:00 -0400",
		})))
	}))
	defer srv.Close()

	if got := FetchAll(context.Background(), []Feed{{Name: "Wire", URL: srv.URL}}); len(got) != 0 {
		t.Errorf("FetchAll returned %d headlines through a closed gate", len(got))
	}

	prev := EDGARBaseURL
	EDGARBaseURL = srv.URL
	defer func() { EDGARBaseURL = prev }()
	if got := FetchEDGAR(context.Background(), []string{"AAPL"}, 0); len(got) != 0 {
		t.Errorf("FetchEDGAR returned %d headlines through a closed gate", len(got))
	}

	if n := requests.Load(); n != 0 {
		t.Fatalf("%d requests were issued without a token", n)
	}
}

func TestDefaultFeeds(t *testing.T) {
	feeds := DefaultFeeds()
	if len(feeds) == 0 {
		t.Fatal("no default feeds")
	}
	seen := map[string]string{}
	for _, f := range feeds {
		if strings.TrimSpace(f.Name) == "" {
			t.Errorf("feed %q has no name to label its headlines with", f.URL)
		}
		u, err := url.Parse(f.URL)
		if err != nil {
			t.Errorf("feed %q: unparsable URL %q: %v", f.Name, f.URL, err)
			continue
		}
		// A plaintext feed lets anything on the path rewrite a headline and
		// the link it opens in the user's browser.
		if u.Scheme != "https" {
			t.Errorf("feed %q is %s, want https", f.Name, u.Scheme)
		}
		if prev, dup := seen[f.URL]; dup {
			t.Errorf("feeds %q and %q share a URL, so every story arrives twice before dedup", prev, f.Name)
		}
		seen[f.URL] = f.Name
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Date(2024, 8, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"zero time renders blank", time.Time{}, ""},
		{"same instant", now, "just now"},
		{"under a minute", now.Add(-30 * time.Second), "just now"},
		{"one second under a minute", now.Add(-59 * time.Second), "just now"},
		{"exactly a minute", now.Add(-time.Minute), "1m ago"},
		{"truncates to whole minutes", now.Add(-90 * time.Second), "1m ago"},
		{"one second under an hour", now.Add(-time.Hour + time.Second), "59m ago"},
		{"exactly an hour", now.Add(-time.Hour), "1h ago"},
		{"one minute under a day", now.Add(-24*time.Hour + time.Minute), "23h ago"},
		{"exactly a day", now.Add(-24 * time.Hour), "1d ago"},
		{"several days", now.Add(-72 * time.Hour), "3d ago"},
		{"a publisher clock ahead of ours", now.Add(time.Hour), "just now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TimeAgo(tc.when, now); got != tc.want {
				t.Errorf("TimeAgo(%v) = %q, want %q", tc.when, got, tc.want)
			}
		})
	}
}
