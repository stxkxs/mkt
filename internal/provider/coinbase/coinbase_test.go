package coinbase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/symbol"
)

// ─── backoff ───

func TestBackoffDoublesAndCaps(t *testing.T) {
	b := newBackoff()
	var prev time.Duration
	for i := 0; i < 10; i++ {
		d := b.observe(time.Time{}, time.Now())
		if d <= 0 {
			t.Fatalf("attempt %d: non-positive delay %v", i, d)
		}
		if d > reconnectMax+time.Duration(float64(reconnectMax)*reconnectJitter) {
			t.Fatalf("attempt %d: delay %v exceeds jittered max", i, d)
		}
		if i > 0 && d < prev/2 && b.cur < b.max {
			t.Fatalf("attempt %d: delay went backwards %v -> %v", i, prev, d)
		}
		prev = d
	}
	if b.cur != reconnectMax {
		t.Errorf("cur pinned at %v, want %v", b.cur, reconnectMax)
	}
}

func TestBackoffResetsAfterHealthySession(t *testing.T) {
	b := newBackoff()
	// Five failed dials in a row: the delay climbs.
	for i := 0; i < 5; i++ {
		b.observe(time.Time{}, time.Now())
	}
	if b.cur <= reconnectMin {
		t.Fatalf("cur should have climbed, got %v", b.cur)
	}

	// Now a session that stayed up well past reconnectStable drops. This
	// is the regression: the delay used to stay pinned for the lifetime of
	// the process, so an hours-healthy connection still waited 30s.
	now := time.Now()
	d := b.observe(now.Add(-2*reconnectStable), now)
	if b.cur != reconnectMin*2 {
		t.Errorf("cur after healthy session = %v, want %v", b.cur, reconnectMin*2)
	}
	if d > reconnectMin*2 {
		t.Errorf("delay after healthy session = %v, want ~%v", d, reconnectMin)
	}
}

func TestBackoffDoesNotResetOnFlap(t *testing.T) {
	b := newBackoff()
	for i := 0; i < 4; i++ {
		b.observe(time.Time{}, time.Now())
	}
	climbed := b.cur

	// Connected, then dropped a second later: not a recovery. Resetting
	// here would spin the reconnect loop at reconnectMin forever against a
	// server that accepts and immediately hangs up.
	now := time.Now()
	b.observe(now.Add(-time.Second), now)
	if b.cur <= climbed {
		t.Errorf("cur = %v after a flap, want it to keep climbing past %v", b.cur, climbed)
	}
}

func TestJitterStaysInBand(t *testing.T) {
	const d = 10 * time.Second
	lo := time.Duration(float64(d) * (1 - reconnectJitter))
	hi := time.Duration(float64(d) * (1 + reconnectJitter))
	var sawLow, sawHigh bool
	for i := 0; i < 200; i++ {
		got := jittered(d)
		if got < lo || got > hi {
			t.Fatalf("jittered(%v) = %v, outside [%v, %v]", d, got, lo, hi)
		}
		if got < d {
			sawLow = true
		}
		if got > d {
			sawHigh = true
		}
	}
	if !sawLow || !sawHigh {
		t.Error("jitter should spread both above and below the base delay")
	}
}

// ─── symbol canonicalization ───

func TestToCoinbaseSymbol(t *testing.T) {
	cases := map[string]string{
		"btc":       "BTC-USD",
		"BTC":       "BTC-USD",
		"BTC-USD":   "BTC-USD",
		"btc-usd":   "BTC-USD",
		"BTCUSD":    "BTC-USD",
		"BTCUSDT":   "BTC-USD",
		"btc-usdt":  "BTC-USD",
		"BTCUSDC":   "BTC-USD",
		"BTCBUSD":   "BTC-USD",
		" eth-usd ": "ETH-USD",
		"PEPE-USD":  "PEPE-USD",
		"":          "",
	}
	for in, want := range cases {
		if got := toCoinbaseSymbol(in); got != want {
			t.Errorf("toCoinbaseSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

// seededCryptoSymbols is every crypto symbol internal/config/defaults.go
// ships in its stock portfolios. They must survive canonicalization
// unchanged, otherwise a seeded watchlist entry and the quotes streaming
// in for it land under different cache keys.
var seededCryptoSymbols = []string{
	"ADA-USD", "ARB-USD", "AVAX-USD", "BTC-USD", "DOGE-USD", "DOT-USD",
	"ETH-USD", "LINK-USD", "NEAR-USD", "OP-USD", "PEPE-USD", "SOL-USD",
	"SUI-USD", "XRP-USD",
}

func TestSeededSymbolsAreAlreadyCanonical(t *testing.T) {
	for _, s := range seededCryptoSymbols {
		if got := toCoinbaseSymbol(s); got != s {
			t.Errorf("toCoinbaseSymbol(%q) = %q, want it unchanged", s, got)
		}
		if got := symbol.Canonical(s); got != s {
			t.Errorf("symbol.Canonical(%q) = %q, want it unchanged", s, got)
		}
	}
}

// Quote.Symbol has to be byte-identical to what symbol.Canonical produces
// from user input, or the watchlist row and the quote streamed for it
// never meet. This pins the two together for every spelling a user can
// type, including the ticker renames only the shared package knows about.
func TestProductIDMatchesSymbolCanonical(t *testing.T) {
	ins := append([]string{
		"btc", "BTC", "btc-usd", "BTCUSD", "BTCUSDT", "btc-usdt", "BTCBUSD",
		"eth", "ETHUSDT", "ETH-USDT", "sui", " sol ", "matic", "MATIC-USD",
	}, seededCryptoSymbols...)

	for _, in := range ins {
		if !symbol.IsCrypto(in) {
			t.Errorf("symbol.IsCrypto(%q) = false; coinbase would never see it", in)
			continue
		}
		want := symbol.Canonical(in)
		if got := toCoinbaseSymbol(in); got != want {
			t.Errorf("toCoinbaseSymbol(%q) = %q, symbol.Canonical = %q", in, got, want)
		}
		if !strings.HasSuffix(want, "-USD") {
			t.Errorf("canonical crypto form %q is not <BASE>-USD", want)
		}
		// Quote.Symbol is stamped from the product ID Coinbase echoes back,
		// which is whatever we subscribed with.
		q, err := tickerToQuote(wsTicker{ProductID: want, Price: "1"})
		if err != nil {
			t.Fatalf("tickerToQuote(%q): %v", want, err)
		}
		if q.Symbol != want {
			t.Errorf("Quote.Symbol = %q for product %q", q.Symbol, want)
		}
	}
}

func TestTickerToQuoteCanonicalizesSymbol(t *testing.T) {
	q, err := tickerToQuote(wsTicker{
		ProductID:          "btc-usdt",
		Price:              "110",
		PricePercentChg24H: "10",
		Volume24H:          "5",
		High24H:            "120",
		Low24H:             "90",
		BestBid:            "109",
		BestAsk:            "111",
	})
	if err != nil {
		t.Fatalf("tickerToQuote: %v", err)
	}
	if q.Symbol != "BTC-USD" {
		t.Errorf("Symbol = %q, want BTC-USD", q.Symbol)
	}
	// price 110 at +10% implies an open of 100, so change is 10.
	if diff := q.Change - 10; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Change = %v, want 10", q.Change)
	}
	if q.Asset != provider.AssetCrypto || q.Provider != "coinbase" {
		t.Errorf("unexpected provenance: %+v", q)
	}
}

func TestProductIDsDedupesAndPreservesOrder(t *testing.T) {
	got := productIDs([]string{"btc", "BTC-USD", "eth", "", "ETHUSDT", "sol"})
	want := []string{"BTC-USD", "ETH-USD", "SOL-USD"}
	if len(got) != len(want) {
		t.Fatalf("productIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("productIDs = %v, want %v", got, want)
		}
	}
}

func TestTickerToQuoteRejectsBadPrice(t *testing.T) {
	if _, err := tickerToQuote(wsTicker{ProductID: "BTC-USD", Price: "n/a"}); err == nil {
		t.Fatal("expected an error for an unparseable price")
	}
}

// ─── candle granularity ───

func TestNativeGranularityCoversEveryInterval(t *testing.T) {
	valid := map[int]bool{60: true, 300: true, 900: true, 3600: true, 21600: true, 86400: true}
	for _, i := range []provider.Interval{
		provider.Interval1m, provider.Interval5m, provider.Interval15m,
		provider.Interval1h, provider.Interval4h, provider.Interval1d,
		provider.Interval1w,
	} {
		g := nativeGranularity(i)
		if !valid[g] {
			t.Errorf("%s maps to granularity %d, which Coinbase does not serve", i, g)
		}
		step := intervalDuration(i)
		if step%(time.Duration(g)*time.Second) != 0 {
			t.Errorf("%s: interval %v is not a whole multiple of granularity %ds", i, step, g)
		}
	}
}

func TestBucketStartAlignsToUTC(t *testing.T) {
	ts := time.Date(2024, 3, 14, 13, 47, 12, 0, time.UTC)
	if got := bucketStart(ts, provider.Interval4h); !got.Equal(time.Date(2024, 3, 14, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("4h bucket = %v, want 12:00", got)
	}
	if got := bucketStart(ts, provider.Interval1h); !got.Equal(time.Date(2024, 3, 14, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("1h bucket = %v, want 13:00", got)
	}
	if got := bucketStart(ts, provider.Interval1d); !got.Equal(time.Date(2024, 3, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("1d bucket = %v, want midnight", got)
	}
}

func TestBucketStartWeeklyAlignsToMonday(t *testing.T) {
	// 2024-03-14 is a Thursday; its week starts Monday 2024-03-11.
	want := time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC)
	for day := 11; day <= 17; day++ {
		ts := time.Date(2024, 3, day, 9, 30, 0, 0, time.UTC)
		if got := bucketStart(ts, provider.Interval1w); !got.Equal(want) {
			t.Errorf("week of %v = %v, want %v", ts, got, want)
		}
	}
	// Sunday 2024-03-17 is the last day of that week; Monday the 18th rolls over.
	next := bucketStart(time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC), provider.Interval1w)
	if !next.Equal(time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("next week = %v, want 2024-03-18", next)
	}
}

func TestAggregateFoldsHourlyIntoFourHour(t *testing.T) {
	base := time.Date(2024, 3, 14, 12, 0, 0, 0, time.UTC)
	in := []provider.OHLCV{
		{Time: base, Open: 10, High: 12, Low: 9, Close: 11, Volume: 1},
		{Time: base.Add(time.Hour), Open: 11, High: 15, Low: 10, Close: 14, Volume: 2},
		{Time: base.Add(2 * time.Hour), Open: 14, High: 14, Low: 8, Close: 9, Volume: 3},
		{Time: base.Add(3 * time.Hour), Open: 9, High: 13, Low: 9, Close: 13, Volume: 4},
		{Time: base.Add(4 * time.Hour), Open: 13, High: 20, Low: 13, Close: 18, Volume: 5},
	}
	got := aggregate(in, provider.Interval4h)
	if len(got) != 2 {
		t.Fatalf("want 2 buckets, got %d: %+v", len(got), got)
	}
	first := got[0]
	if !first.Time.Equal(base) {
		t.Errorf("bucket time = %v, want %v", first.Time, base)
	}
	if first.Open != 10 || first.Close != 13 {
		t.Errorf("open/close = %v/%v, want 10/13", first.Open, first.Close)
	}
	if first.High != 15 || first.Low != 8 {
		t.Errorf("high/low = %v/%v, want 15/8", first.High, first.Low)
	}
	if first.Volume != 10 {
		t.Errorf("volume = %v, want 10", first.Volume)
	}
	if got[1].Open != 13 || got[1].Close != 18 || got[1].Volume != 5 {
		t.Errorf("trailing bucket wrong: %+v", got[1])
	}
}

func TestAggregateFoldsDailyIntoWeekly(t *testing.T) {
	// Monday 2024-03-11 through Tuesday 2024-03-19: 2 full weeks + 1 day.
	var in []provider.OHLCV
	for d := 0; d < 9; d++ {
		day := time.Date(2024, 3, 11+d, 0, 0, 0, 0, time.UTC)
		in = append(in, provider.OHLCV{
			Time: day, Open: float64(d), High: float64(d) + 1,
			Low: float64(d) - 1, Close: float64(d), Volume: 1,
		})
	}
	got := aggregate(in, provider.Interval1w)
	if len(got) != 2 {
		t.Fatalf("want 2 weekly buckets, got %d", len(got))
	}
	if !got[0].Time.Equal(time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first week starts %v", got[0].Time)
	}
	if got[0].Volume != 7 {
		t.Errorf("first week volume = %v, want 7", got[0].Volume)
	}
	if !got[1].Time.Equal(time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("second week starts %v", got[1].Time)
	}
	if got[1].Volume != 2 {
		t.Errorf("partial trailing week volume = %v, want 2", got[1].Volume)
	}
}

func TestAggregateEmpty(t *testing.T) {
	if got := aggregate(nil, provider.Interval4h); len(got) != 0 {
		t.Errorf("aggregate(nil) = %+v", got)
	}
}

// candleServer serves Coinbase-shaped candles (newest first) at the
// requested granularity and records the query it was asked for.
func candleServer(t *testing.T, count int, query *url.Values) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query != nil {
			*query = r.URL.Query()
		}
		gran, _ := strconv.Atoi(r.URL.Query().Get("granularity"))
		if gran == 0 {
			gran = 3600
		}
		base := time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC)
		rows := make([][]float64, 0, count)
		for i := count - 1; i >= 0; i-- { // newest first
			ts := base.Add(time.Duration(i*gran) * time.Second).Unix()
			v := float64(i)
			rows = append(rows, []float64{float64(ts), v - 1, v + 1, v, v, 1})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	}))
}

func TestHistoryReturnsRequestedGranularity(t *testing.T) {
	var q url.Values
	srv := candleServer(t, 24, &q)
	defer srv.Close()
	prev := restURL
	restURL = srv.URL
	defer func() { restURL = prev }()

	p := New()
	got, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "BTC-USD", Interval: provider.Interval4h, Limit: 6,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// Coinbase has no 4h candle, so we must have asked it for 1h ones.
	if q.Get("granularity") != "3600" {
		t.Errorf("requested granularity %q, want 3600", q.Get("granularity"))
	}
	// 24 hourly candles fold into exactly 6 four-hour buckets.
	if len(got) != 6 {
		t.Fatalf("want 6 4h candles, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if d := got[i].Time.Sub(got[i-1].Time); d != 4*time.Hour {
			t.Errorf("candle %d is %v after the previous, want 4h", i, d)
		}
	}
	if got[0].Volume != 4 {
		t.Errorf("bucket volume = %v, want 4 hourly candles summed", got[0].Volume)
	}
}

func TestHistoryWeeklyAggregates(t *testing.T) {
	var q url.Values
	srv := candleServer(t, 21, &q)
	defer srv.Close()
	prev := restURL
	restURL = srv.URL
	defer func() { restURL = prev }()

	p := New()
	got, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "ETH-USD", Interval: provider.Interval1w, Limit: 3,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if q.Get("granularity") != "86400" {
		t.Errorf("requested granularity %q, want 86400", q.Get("granularity"))
	}
	if len(got) != 3 {
		t.Fatalf("want 3 weekly candles, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if d := got[i].Time.Sub(got[i-1].Time); d != 7*24*time.Hour {
			t.Errorf("candle %d is %v after the previous, want 168h", i, d)
		}
	}
}

func TestHistoryPassesNativeIntervalsThrough(t *testing.T) {
	var q url.Values
	srv := candleServer(t, 10, &q)
	defer srv.Close()
	prev := restURL
	restURL = srv.URL
	defer func() { restURL = prev }()

	p := New()
	got, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "BTC-USD", Interval: provider.Interval1h, Limit: 10,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if q.Get("granularity") != "3600" {
		t.Errorf("granularity %q, want 3600", q.Get("granularity"))
	}
	if len(got) != 10 {
		t.Errorf("want 10 candles passed through unaggregated, got %d", len(got))
	}
	// Oldest first.
	if !got[0].Time.Before(got[len(got)-1].Time) {
		t.Error("candles should be chronological")
	}
}

func TestHistoryClampsOverFetchToCoinbaseLimit(t *testing.T) {
	var q url.Values
	srv := candleServer(t, 300, &q)
	defer srv.Close()
	prev := restURL
	restURL = srv.URL
	defer func() { restURL = prev }()

	p := New()
	// 100 weekly candles would need 700 daily ones; Coinbase caps a
	// request at 300, so we ask for 300 and return the 42 whole weeks
	// they cover rather than erroring out upstream.
	got, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "BTC-USD", Interval: provider.Interval1w, Limit: 100,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) > 300/7 {
		t.Errorf("got %d weekly candles, want at most %d", len(got), 300/7)
	}
	if len(got) == 0 {
		t.Fatal("expected some candles")
	}
	span := q.Get("end")
	if span == "" {
		t.Error("end not sent upstream")
	}
}

func TestHistoryURLEscapesProductID(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	prev := restURL
	restURL = srv.URL
	defer func() { restURL = prev }()

	p := New()
	if _, err := p.History(context.Background(), provider.HistoryParams{
		Symbol: "btc", Interval: provider.Interval1d,
	}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if !strings.Contains(path, "/products/BTC-USD/candles") {
		t.Errorf("path = %q, want the canonical product id", path)
	}
}

func TestHistoryPropagatesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	prev := restURL
	restURL = srv.URL
	defer func() { restURL = prev }()

	p := New()
	_, err := p.History(context.Background(), provider.HistoryParams{Symbol: "BTC-USD"})
	if err == nil {
		t.Fatal("expected an error for a 503")
	}
	if !strings.Contains(err.Error(), "coinbase candles") {
		t.Errorf("error should be wrapped: %v", err)
	}
}

// ─── provider plumbing ───

func TestSupportsDelegatesToSymbolPackage(t *testing.T) {
	p := New()
	for _, s := range []string{"BTC-USD", "btc", "ETHUSDT", "SOL-USD"} {
		if !p.Supports(s) {
			t.Errorf("Supports(%q) = false", s)
		}
	}
	for _, s := range []string{"AAPL", "FRED:DGS10", ""} {
		if p.Supports(s) {
			t.Errorf("Supports(%q) = true", s)
		}
	}
}

func TestNotifyStatusNeverBlocks(t *testing.T) {
	p := New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			p.notifyStatus(i%2 == 0)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notifyStatus blocked on a full status channel")
	}
	if _, ok := <-p.StatusChan(); !ok {
		t.Fatal("status channel closed")
	}
}
