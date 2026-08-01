package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/observe"
	"github.com/stxkxs/mkt/internal/provider"
)

func newTestServer(t *testing.T) (*market.Cache, *Server) {
	t.Helper()
	cache := market.NewCache(60)
	cache.Push(provider.Quote{Symbol: "AAPL", Price: 200, Change: -1.5, ChangePct: -0.75})
	cache.Push(provider.Quote{Symbol: "BTC-USD", Price: 60000, Change: 1200, ChangePct: 2.0})
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

func TestQuotesIncludeChangeAndDir(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleQuotes(rec, httptest.NewRequest("GET", "/quotes", nil))
	var got []quoteEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]quoteEntry{}
	for _, e := range got {
		byID[e.Symbol] = e
	}
	if e := byID["AAPL"]; e.ChangePct != -0.75 || e.Dir != "down" {
		t.Errorf("AAPL: got change_pct=%v dir=%q, want -0.75/down", e.ChangePct, e.Dir)
	}
	if e := byID["BTC-USD"]; e.ChangePct != 2.0 || e.Dir != "up" {
		t.Errorf("BTC-USD: got change_pct=%v dir=%q, want 2.0/up", e.ChangePct, e.Dir)
	}
}

func TestDirection(t *testing.T) {
	cases := map[float64]string{2.5: "up", -0.1: "down", 0: "flat"}
	for pct, want := range cases {
		if got := direction(pct); got != want {
			t.Errorf("direction(%v) = %q, want %q", pct, got, want)
		}
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

func TestMetricsIncludesPriceGauges(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE mkt_price gauge",
		`mkt_price{symbol="BTC-USD"} 60000`,
		`mkt_price{symbol="AAPL"} 200`,
		"# TYPE mkt_change_pct gauge",
		`mkt_change_pct{symbol="AAPL"} -0.75`,
		`mkt_change_pct{symbol="BTC-USD"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q in:\n%s", want, body)
		}
	}
}

func TestEscapeLabel(t *testing.T) {
	cases := map[string]string{
		"BTC-USD":  "BTC-USD",
		"FRED:GDP": "FRED:GDP",
		`a"b\c`:    `a\"b\\c`,
	}
	for in, want := range cases {
		if got := escapeLabel(in); got != want {
			t.Errorf("escapeLabel(%q) = %q, want %q", in, got, want)
		}
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

	// The token is header-only; a query-string token must be rejected so
	// secrets don't leak into proxy/access logs.
	queryRejected := httptest.NewRecorder()
	h(queryRejected, httptest.NewRequest("GET", "/quotes?token=hunter2", nil))
	if queryRejected.Code != http.StatusUnauthorized {
		t.Errorf("query token must be rejected: want 401, got %d", queryRejected.Code)
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

func TestWebhookRouteMountedOnlyWhenEnabled(t *testing.T) {
	cache := market.NewCache(60)
	engine := alert.NewEngine(0, nil)

	off := New(":0", cache, engine) // webhook disabled by default
	rec := httptest.NewRecorder()
	off.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(`{"symbol":"X"}`)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("webhook off: want 404 (unmounted), got %d", rec.Code)
	}

	on := New(":0", cache, engine).WithWebhook(true)
	rec2 := httptest.NewRecorder()
	on.handler().ServeHTTP(rec2, httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(`{"symbol":"X","price":1}`)))
	if rec2.Code == http.StatusNotFound {
		t.Errorf("webhook on: route should be mounted, got 404")
	}
}

func TestAlertsRedactsWebhookURLs(t *testing.T) {
	cache := market.NewCache(60)
	engine := alert.NewEngine(0, nil)
	engine.SetRules([]alert.Rule{{
		Symbol: "AAPL", Condition: "above", Value: 200, Enabled: true,
		Webhooks: []string{"https://hooks.slack.com/services/T00/B00/XXXXSECRET"},
	}})
	s := New(":0", cache, engine)

	rec := httptest.NewRecorder()
	s.handleAlerts(rec, httptest.NewRequest("GET", "/alerts", nil))
	body := rec.Body.String()
	if strings.Contains(body, "hooks.slack.com") || strings.Contains(body, "XXXXSECRET") {
		t.Errorf("/alerts leaked a webhook URL:\n%s", body)
	}
	if !strings.Contains(body, `"has_webhooks":true`) {
		t.Errorf("/alerts should report has_webhooks:true:\n%s", body)
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

func TestTradingViewRateLimited(t *testing.T) {
	cache := market.NewCache(60)
	engine := alert.NewEngine(0, nil)
	s := New(":0", cache, engine)
	body := `{"symbol":"AAPL","price":201.5}`

	// The initial burst is allowed; the request past the burst is throttled.
	for i := 0; i < webhookBurst; i++ {
		rec := httptest.NewRecorder()
		s.handleTradingView(rec, httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("burst request %d: want 200, got %d", i, rec.Code)
		}
	}
	over := httptest.NewRecorder()
	s.handleTradingView(over, httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(body)))
	if over.Code != http.StatusTooManyRequests {
		t.Errorf("past burst: want 429, got %d", over.Code)
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

// capture builds a webhook-enabled server whose injected alerts land in the
// returned slice. Inject calls the engine's onAlert hook synchronously, but
// the guard keeps -race honest if that ever changes.
func capture(t *testing.T) (*Server, func() []alert.TriggeredAlert) {
	t.Helper()
	var mu sync.Mutex
	var got []alert.TriggeredAlert
	engine := alert.NewEngine(0, func(a alert.TriggeredAlert) {
		mu.Lock()
		got = append(got, a)
		mu.Unlock()
	})
	s := New(":0", market.NewCache(60), engine).WithWebhook(true)
	return s, func() []alert.TriggeredAlert {
		mu.Lock()
		defer mu.Unlock()
		return append([]alert.TriggeredAlert(nil), got...)
	}
}

func postWebhook(s *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleTradingView(rec, httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(body)))
	return rec
}

func TestCleanText(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "AAPL crossed 200", "AAPL crossed 200", false},
		{"trims and collapses", "  AAPL   crossed \t 200 \n", "AAPL crossed 200", false},
		{"newlines fold to spaces", "line one\nline two\r\nline three", "line one line two line three", false},
		{"unicode is fine", "BTC ↗ 60k €", "BTC ↗ 60k €", false},
		{"ansi CSI", "clear\x1b[2Jgone", "", true},
		{"ansi OSC title", "\x1b]0;pwned\x07", "", true},
		{"bare escape", "\x1b", "", true},
		{"nul byte", "a\x00b", "", true},
		{"bell", "ding\a", "", true},
		{"backspace overwrite", "safe\b\b\b\bevil", "", true},
		{"del", "a\x7fb", "", true},
		{"c1 control", "a\u0085b", "", true},
		{"bidi override", "gnp.eqt\u202Etxt.exe", "", true},
		{"zero width joiner", "a\u200db", "", true},
		{"invalid utf-8", "bad\xffbytes", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cleanText("message", tc.in, maxWebhookMessageRunes)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("cleanText(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("cleanText(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanTextEnforcesLength(t *testing.T) {
	if _, err := cleanText("message", strings.Repeat("x", maxWebhookMessageRunes), maxWebhookMessageRunes); err != nil {
		t.Fatalf("exactly at the cap should pass: %v", err)
	}
	if _, err := cleanText("message", strings.Repeat("x", maxWebhookMessageRunes+1), maxWebhookMessageRunes); err == nil {
		t.Error("one past the cap should fail")
	}
	// The cap counts runes, not bytes: multi-byte text must not be
	// rejected for being wide.
	if _, err := cleanText("message", strings.Repeat("é", maxWebhookMessageRunes), maxWebhookMessageRunes); err != nil {
		t.Errorf("cap must count runes, not bytes: %v", err)
	}
}

func TestSymbolPattern(t *testing.T) {
	for _, ok := range []string{"AAPL", "BTC-USD", "BRK.B", "^GSPC", "GC=F", "FRED:DGS10", "BINANCE:BTCUSDT", "EURUSD=X"} {
		if !symbolPattern.MatchString(ok) {
			t.Errorf("symbolPattern rejected a real symbol: %q", ok)
		}
	}
	for _, bad := range []string{"", "AA PL", "<SCRIPT>", "A;B", "A\\B", `A"B`, "-LEAD", "A{B}", "AAPL\u00A0"} {
		if symbolPattern.MatchString(bad) {
			t.Errorf("symbolPattern accepted %q", bad)
		}
	}
}

func TestWebhookRejectsANSIPayload(t *testing.T) {
	// A terminal escape in a string the TUI renders can repaint the screen
	// or hide text; the alert history replays it on every later run.
	s, injected := capture(t)
	rec := postWebhook(s, `{"symbol":"AAPL","price":1,"message":"safe\u001b[2J\u001b[1;31mPWNED"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ANSI payload: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(injected()) != 0 {
		t.Errorf("a rejected payload must not reach the alert pipeline: %+v", injected())
	}
}

func TestWebhookRejectsNULByte(t *testing.T) {
	s, injected := capture(t)
	rec := postWebhook(s, `{"symbol":"AAPL","price":1,"message":"a\u0000b"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("NUL payload: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(injected()) != 0 {
		t.Errorf("a rejected payload must not reach the alert pipeline: %+v", injected())
	}
}

func TestWebhookRejectsControlCharactersInSymbol(t *testing.T) {
	s, injected := capture(t)
	for _, body := range []string{
		`{"symbol":"AA\u001b[31mPL","price":1}`,
		`{"ticker":"AA\u0000PL","price":1}`,
	} {
		if rec := postWebhook(s, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", body, rec.Code)
		}
	}
	if len(injected()) != 0 {
		t.Errorf("nothing should have been injected: %+v", injected())
	}
}

func TestWebhookRejectsUnrecognizableSymbol(t *testing.T) {
	s, injected := capture(t)
	for _, sym := range []string{"AA PL", "<script>alert(1)</script>", "../../etc/passwd"} {
		body := `{"symbol":` + mustJSON(sym) + `,"price":1}`
		if rec := postWebhook(s, body); rec.Code != http.StatusBadRequest {
			t.Errorf("symbol %q: want 400, got %d", sym, rec.Code)
		}
	}
	if len(injected()) != 0 {
		t.Errorf("nothing should have been injected: %+v", injected())
	}
}

func TestWebhookRejectsOverlongMessage(t *testing.T) {
	s, injected := capture(t)
	long := strings.Repeat("x", maxWebhookMessageRunes+1)
	rec := postWebhook(s, `{"symbol":"AAPL","price":1,"message":"`+long+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overlong message: want 400, got %d", rec.Code)
	}
	if len(injected()) != 0 {
		t.Errorf("nothing should have been injected: %+v", injected())
	}
}

func TestWebhookRejectsOverlongSymbol(t *testing.T) {
	s, _ := capture(t)
	long := strings.Repeat("A", maxWebhookSymbolRunes+1)
	if rec := postWebhook(s, `{"symbol":"`+long+`","price":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("overlong symbol: want 400, got %d", rec.Code)
	}
}

func TestWebhookAcceptsAndNormalizesLegitimateAlert(t *testing.T) {
	s, injected := capture(t)
	// Multi-line templates are routine in TradingView; they must survive,
	// folded onto one line.
	rec := postWebhook(s, `{"ticker":"btc-usd","close":61234.5,"alert":"  crossed\n  60000  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	got := injected()
	if len(got) != 1 {
		t.Fatalf("expected one injected alert, got %d", len(got))
	}
	if got[0].Rule.Symbol != "BTC-USD" {
		t.Errorf("symbol = %q, want BTC-USD (uppercased from the ticker field)", got[0].Rule.Symbol)
	}
	if got[0].Price != 61234.5 {
		t.Errorf("price = %v, want the close fallback 61234.5", got[0].Price)
	}
	if got[0].Message != "crossed 60000" {
		t.Errorf("message = %q, want %q", got[0].Message, "crossed 60000")
	}
}

func TestWebhookNeverInjectsUnsafeText(t *testing.T) {
	// encoding/json silently replaces invalid UTF-8 in a string literal
	// with U+FFFD, so a raw bad byte never reaches cleanText. Whatever the
	// path, what lands in the pipeline must be valid UTF-8 with no
	// control characters.
	s, injected := capture(t)
	body := "{\"symbol\":\"AAPL\",\"price\":1,\"message\":\"bad\xff\xfebytes\"}"
	if rec := postWebhook(s, body); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	got := injected()
	if len(got) != 1 {
		t.Fatalf("expected one injected alert, got %d", len(got))
	}
	if !utf8.ValidString(got[0].Message) {
		t.Errorf("injected message is not valid utf-8: %q", got[0].Message)
	}
	for _, r := range got[0].Message {
		if unicode.IsControl(r) {
			t.Errorf("injected message carries a control character U+%04X: %q", r, got[0].Message)
		}
	}
}

func TestWebhookDefaultMessageWhenTemplateOmitsOne(t *testing.T) {
	s, injected := capture(t)
	if rec := postWebhook(s, `{"symbol":"AAPL","price":201.5}`); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	got := injected()
	if len(got) != 1 || !strings.HasPrefix(got[0].Message, "TradingView alert: AAPL @ 201.5") {
		t.Errorf("expected a synthesized message, got %+v", got)
	}
}

func TestWebhookRejectsCrossOriginRequests(t *testing.T) {
	// A browser page on any origin can reach a loopback bind; the webhook
	// is the one route that changes state.
	s, injected := capture(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/webhook/tradingview", strings.NewReader(`{"symbol":"AAPL","price":1}`))
	req.Header.Set("Origin", "https://evil.example")
	s.handleTradingView(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: want 403, got %d", rec.Code)
	}
	if len(injected()) != 0 {
		t.Errorf("nothing should have been injected: %+v", injected())
	}
}

func TestWebhookRejectsNonPost(t *testing.T) {
	s, _ := capture(t)
	rec := httptest.NewRecorder()
	s.handleTradingView(rec, httptest.NewRequest("GET", "/webhook/tradingview", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: want 405, got %d", rec.Code)
	}
}

func TestWebhookWithoutEngineIsUnavailable(t *testing.T) {
	s := New(":0", market.NewCache(60), nil).WithWebhook(true)
	if rec := postWebhook(s, `{"symbol":"AAPL","price":1}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no engine: want 503, got %d", rec.Code)
	}
}

func TestWebhookCounters(t *testing.T) {
	s, _ := capture(t)
	accepted, rejected := webhookAccepted.Value(), webhookRejected.Value()
	if rec := postWebhook(s, `{"symbol":"AAPL","price":1}`); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if rec := postWebhook(s, `{"symbol":"AAPL","price":1,"message":"\u0000"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := webhookAccepted.Value() - accepted; got != 1 {
		t.Errorf("accepted delta = %d, want 1", got)
	}
	if got := webhookRejected.Value() - rejected; got != 1 {
		t.Errorf("rejected delta = %d, want 1", got)
	}
}

func TestAuthRequiresBearerScheme(t *testing.T) {
	_, s := newTestServer(t)
	s.WithToken("hunter2")
	h := s.auth(s.handleQuotes)

	cases := []struct {
		name, header string
		want         int
	}{
		{"bare token without a scheme", "hunter2", http.StatusUnauthorized},
		{"wrong scheme", "Basic hunter2", http.StatusUnauthorized},
		{"scheme only", "Bearer ", http.StatusUnauthorized},
		{"correct", "Bearer hunter2", http.StatusOK},
		{"scheme is case-insensitive per RFC 7235", "bearer hunter2", http.StatusOK},
		{"surrounding space tolerated", "Bearer  hunter2 ", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/quotes", nil)
			req.Header.Set("Authorization", tc.header)
			h(rec, req)
			if rec.Code != tc.want {
				t.Errorf("header %q: want %d, got %d", tc.header, tc.want, rec.Code)
			}
		})
	}
}

func TestAuthRejectsWrongTokensOfEveryLength(t *testing.T) {
	// The comparison hashes both sides to a fixed 32 bytes before
	// subtle.ConstantTimeCompare, which returns early on a length mismatch
	// — so neither the token's content nor its length is observable.
	_, s := newTestServer(t)
	s.WithToken("hunter2")
	h := s.auth(s.handleQuotes)
	for _, tok := range []string{"", "h", "hunter", "hunter1", "hunter22", strings.Repeat("x", 4096)} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/quotes", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		h(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: want 401, got %d", tok, rec.Code)
		}
	}
}

func TestUnauthorizedAdvertisesTheScheme(t *testing.T) {
	_, s := newTestServer(t)
	s.WithToken("hunter2")
	rec := httptest.NewRecorder()
	s.auth(s.handleQuotes)(rec, httptest.NewRequest("GET", "/quotes", nil))
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
	}
}

func TestEveryRouteIsBehindTheToken(t *testing.T) {
	cache := market.NewCache(60)
	cache.Push(provider.Quote{Symbol: "AAPL", Price: 200})
	engine := alert.NewEngine(0, nil)
	s := New(":0", cache, engine).WithToken("hunter2").WithWebhook(true)
	mux := s.handler()

	routes := []struct{ method, path, body string }{
		{"GET", "/quotes", ""},
		{"GET", "/quotes/AAPL", ""},
		{"GET", "/alerts", ""},
		{"GET", "/metrics", ""},
		{"POST", "/webhook/tradingview", `{"symbol":"AAPL","price":1}`},
	}
	for _, rt := range routes {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: want 401, got %d", rt.method, rt.path, rec.Code)
		}

		ok := httptest.NewRecorder()
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
		req.Header.Set("Authorization", "Bearer hunter2")
		mux.ServeHTTP(ok, req)
		if ok.Code == http.StatusUnauthorized {
			t.Errorf("%s %s with the token: still 401", rt.method, rt.path)
		}
	}
}

func TestAlertsEmptyWithoutEngine(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, httptest.NewRequest("GET", "/alerts", nil))
	var got map[string][]alertRuleView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got["rules"]) != 0 {
		t.Errorf("want an empty rules array, got %+v", got)
	}
}

func TestAlertsReportsNoWebhooksWhenThereAreNone(t *testing.T) {
	engine := alert.NewEngine(0, nil)
	engine.SetRules([]alert.Rule{{Symbol: "AAPL", Condition: "above", Value: 200, Enabled: true}})
	s := New(":0", market.NewCache(60), engine)
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, httptest.NewRequest("GET", "/alerts", nil))
	if !strings.Contains(rec.Body.String(), `"has_webhooks":false`) {
		t.Errorf("want has_webhooks:false, got %s", rec.Body.String())
	}
}

func TestQuotesIncludeTimestamp(t *testing.T) {
	cache := market.NewCache(60)
	stamp := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)
	cache.Push(provider.Quote{Symbol: "AAPL", Price: 200, Timestamp: stamp})
	cache.Push(provider.Quote{Symbol: "NOTIME", Price: 5})
	s := New(":0", cache, nil)

	rec := httptest.NewRecorder()
	s.handleQuotes(rec, httptest.NewRequest("GET", "/quotes", nil))
	var got []quoteEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]quoteEntry{}
	for _, e := range got {
		byID[e.Symbol] = e
	}
	if want := stamp.Format(time.RFC3339); byID["AAPL"].Time != want {
		t.Errorf("AAPL time = %q, want %q", byID["AAPL"].Time, want)
	}
	if byID["NOTIME"].Time != "" {
		t.Errorf("an unstamped quote should omit time, got %q", byID["NOTIME"].Time)
	}
	// The documented fields must keep their names — Prometheus and README
	// consumers scrape them.
	if !strings.Contains(rec.Body.String(), `"change_pct"`) || !strings.Contains(rec.Body.String(), `"dir"`) {
		t.Errorf("documented field names changed: %s", rec.Body.String())
	}
}

func TestMetricsIncludesQuoteAge(t *testing.T) {
	cache := market.NewCache(60)
	cache.Push(provider.Quote{Symbol: "AAPL", Price: 200, Timestamp: time.Now().Add(-90 * time.Second)})
	cache.Push(provider.Quote{Symbol: "NOTIME", Price: 5})
	s := New(":0", cache, nil)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "# TYPE mkt_quote_age_seconds gauge") {
		t.Fatalf("missing age gauge:\n%s", body)
	}
	if !strings.Contains(body, `mkt_quote_age_seconds{symbol="AAPL"} 9`) {
		t.Errorf("expected an ~90s age for AAPL:\n%s", body)
	}
	if strings.Contains(body, `mkt_quote_age_seconds{symbol="NOTIME"}`) {
		t.Errorf("an unstamped quote must not report an age:\n%s", body)
	}
}

func TestMetricsIncludesDropsWhenWired(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(rec.Body.String(), "mkt_quote_drops_total") {
		t.Errorf("drops metric must be absent until wired:\n%s", rec.Body.String())
	}

	s.WithDrops(func() uint64 { return 7 })
	rec2 := httptest.NewRecorder()
	s.handleMetrics(rec2, httptest.NewRequest("GET", "/metrics", nil))
	for _, want := range []string{"# TYPE mkt_quote_drops_total counter", "mkt_quote_drops_total 7"} {
		if !strings.Contains(rec2.Body.String(), want) {
			t.Errorf("metrics missing %q:\n%s", want, rec2.Body.String())
		}
	}
}

func TestMetricsNamesAreStable(t *testing.T) {
	// These names are documented in the README and scraped by Prometheus;
	// renaming one silently breaks every existing dashboard and alert.
	_, s := newTestServer(t)
	s.WithDrops(func() uint64 { return 0 })
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, name := range []string{
		"mkt_uptime_seconds",
		"mkt_symbols_cached",
		"mkt_price",
		"mkt_change_pct",
		"mkt_quote_age_seconds",
		"mkt_quote_drops_total",
		"mkt_webhook_accepted_total",
		"mkt_webhook_rejected_total",
	} {
		if !strings.Contains(body, name) {
			t.Errorf("metric %q missing from /metrics:\n%s", name, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4" {
		t.Errorf("Content-Type = %q, want the Prometheus text format", ct)
	}
}

func TestQuotesEmptyCacheIsAnArray(t *testing.T) {
	s := New(":0", market.NewCache(60), nil)
	rec := httptest.NewRecorder()
	s.handleQuotes(rec, httptest.NewRequest("GET", "/quotes", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("empty cache should render [], got %q", got)
	}
}

func TestQuoteSingleWithoutSymbol(t *testing.T) {
	_, s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleQuote(rec, httptest.NewRequest("GET", "/quotes/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestShutdownBeforeStartIsANoop(t *testing.T) {
	_, s := newTestServer(t)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown before Start: %v", err)
	}
}

func TestServesOverRealHTTP(t *testing.T) {
	cache := market.NewCache(60)
	cache.Push(provider.Quote{Symbol: "AAPL", Price: 200})
	s := New("127.0.0.1:0", cache, nil)

	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/quotes")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestStartAndShutdown(t *testing.T) {
	_, s := newTestServer(t)
	s.addr = "127.0.0.1:0"
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestWebhookRejectsInvalidJSON(t *testing.T) {
	s, injected := capture(t)
	for _, body := range []string{`not json`, `{"symbol":`, `[1,2,3]`} {
		if rec := postWebhook(s, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d", body, rec.Code)
		}
	}
	if len(injected()) != 0 {
		t.Errorf("nothing should have been injected: %+v", injected())
	}
}
