// Package api exposes a small read-only HTTP surface for scripting and
// monitoring: /quotes, /quotes/{symbol}, /alerts, /metrics.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/time/rate"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/observe"
	"github.com/stxkxs/mkt/internal/provider"
)

// Webhook payload limits.
//
// maxWebhookBytes caps the request body for /webhook/tradingview so an
// oversize POST can't OOM the process. TradingView alerts are tiny — the
// body usually fits in a few hundred bytes. The per-field rune caps bound
// what actually escapes the endpoint: the symbol becomes a cache key and
// a TUI column, the message is persisted to the on-disk alert history and
// pushed to desktop/ntfy/Pushover notifiers.
const (
	maxWebhookBytes        = 64 * 1024
	maxWebhookSymbolRunes  = 32
	maxWebhookMessageRunes = 512
)

// webhook rate limit: the /webhook/tradingview endpoint injects alerts
// that fan out to desktop / push / webhook notifiers, so an unthrottled
// caller could drive notification spam (and drain a paid Pushover quota).
// Legitimate TradingView alerts are infrequent; 1/s sustained with a
// small burst is generous while capping abuse.
const (
	webhookRatePerSec = 1
	webhookBurst      = 5
)

// Webhook outcome counters, surfaced on /metrics. They are registered
// unconditionally so the series exist (at zero) even when the route is
// unmounted — a Prometheus alert on a rejection rate needs a series that
// does not appear out of nowhere.
var (
	webhookAccepted = observe.NewCounter("mkt_webhook_accepted_total")
	webhookRejected = observe.NewCounter("mkt_webhook_rejected_total")
)

// Server is a small read-only HTTP frontend.
type Server struct {
	addr           string
	cache          *market.Cache
	engine         *alert.Engine
	token          string   // optional bearer token; empty disables auth
	tokenHash      [32]byte // sha256 of token, compared in constant time
	webhookEnabled bool     // mount /webhook/tradingview (the inbound injection sink)
	drops          func() uint64
	started        time.Time
	srv            *http.Server
	webhookLimiter *rate.Limiter
}

// New constructs a Server. addr is e.g. ":9999".
func New(addr string, cache *market.Cache, engine *alert.Engine) *Server {
	return &Server{
		addr:           addr,
		cache:          cache,
		engine:         engine,
		started:        time.Now(),
		webhookLimiter: rate.NewLimiter(rate.Limit(webhookRatePerSec), webhookBurst),
	}
}

// WithToken sets a bearer token required on every request. When set,
// clients must send it in the `Authorization: Bearer <token>` header.
// The token is deliberately not accepted as a query parameter — query
// strings leak into proxy and access logs. Empty token (default) leaves
// the server unauthenticated, which the caller only permits for loopback
// binds (see checkListenSafety).
func (s *Server) WithToken(token string) *Server {
	s.token = token
	s.tokenHash = sha256.Sum256([]byte(token))
	return s
}

// WithWebhook controls whether the inbound /webhook/tradingview route is
// mounted. It's off by default: the route injects alerts into the notifier
// fan-out, so it stays unmounted unless a caller explicitly opts in (and
// the caller is expected to require a token alongside it).
func (s *Server) WithWebhook(enabled bool) *Server {
	s.webhookEnabled = enabled
	return s
}

// WithDrops attaches a counter for quotes the hub dropped under TUI
// back-pressure, exported on /metrics as mkt_quote_drops_total. It takes a
// function rather than the hub itself so this package stays independent of
// the hub's lifecycle; callers pass market.Hub.Drops. Unset (the default)
// simply omits the metric.
func (s *Server) WithDrops(fn func() uint64) *Server {
	s.drops = fn
	return s
}

// Start launches the server in a goroutine. ListenAndServe errors are
// logged through the caller's logger (best-effort: bind failures are
// surfaced via stderr).
func (s *Server) Start() error {
	s.srv = &http.Server{
		Addr:    s.addr,
		Handler: s.handler(),
		// Every handler here is in-memory and answers immediately, so the
		// timeouts exist purely to reap slow or wedged peers rather than to
		// accommodate real work.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "api: listen %s: %v\n", s.addr, err)
		}
	}()
	return nil
}

// handler builds the route mux. The inbound webhook (alert injection) is
// mounted only when explicitly enabled — it's the one route that mutates
// state / drives the notifier fan-out. Exposed for tests.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/quotes", s.auth(s.handleQuotes))
	mux.HandleFunc("/quotes/", s.auth(s.handleQuote))
	mux.HandleFunc("/alerts", s.auth(s.handleAlerts))
	mux.HandleFunc("/metrics", s.auth(s.handleMetrics))
	if s.webhookEnabled {
		mux.HandleFunc("/webhook/tradingview", s.auth(s.handleTradingView))
	}
	return mux
}

// auth wraps a handler with token authentication when a token has been
// configured. With no token configured it's a no-op.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	if s.token == "" {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mkt"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// authorized reports whether r presents the configured bearer token.
//
// The presented credential and the configured one are compared as SHA-256
// digests: subtle.ConstantTimeCompare is constant time only across equal
// lengths and returns early otherwise, so comparing the raw strings would
// let a caller recover the token's length from response timing. Hashing
// first makes both operands a fixed 32 bytes, so the comparison leaks
// neither length nor content.
//
// The `Bearer ` scheme is required (RFC 6750), matched case-insensitively
// per RFC 7235. Accepting a bare header value would also accept a Basic
// credential that happened to collide with the token.
func (s *Server) authorized(r *http.Request) bool {
	const scheme = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(scheme) || !strings.EqualFold(h[:len(scheme)], scheme) {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(h[len(scheme):])))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

// Shutdown stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// quoteEntry is the JSON shape returned by /quotes and /quotes/{symbol}.
// change_pct is the provider's 24h (crypto) or day (stock) percent change;
// dir is a pre-computed "up"/"down"/"flat" so status-bar and prompt
// consumers can pick a color without re-deriving the sign.
// time is the provider timestamp of the quote in RFC 3339, omitted when
// the provider did not stamp one. It is additive: consumers that poll this
// endpoint need it to tell a stale feed from a flat market.
type quoteEntry struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"change_pct"`
	Dir       string  `json:"dir"`
	Time      string  `json:"time,omitempty"`
}

func newQuoteEntry(sym string, q provider.Quote) quoteEntry {
	e := quoteEntry{
		Symbol:    sym,
		Price:     q.Price,
		Change:    q.Change,
		ChangePct: q.ChangePct,
		Dir:       direction(q.ChangePct),
	}
	if !q.Timestamp.IsZero() {
		e.Time = q.Timestamp.UTC().Format(time.RFC3339)
	}
	return e
}

// direction maps a percent change to a stable label for consumers that
// colorize by sign (tmux/starship tickers, waybar classes).
func direction(pct float64) string {
	switch {
	case pct > 0:
		return "up"
	case pct < 0:
		return "down"
	default:
		return "flat"
	}
}

// /quotes — list of {symbol, price, change, change_pct, dir} for every
// cached symbol.
func (s *Server) handleQuotes(w http.ResponseWriter, r *http.Request) {
	syms := s.cache.Symbols()
	sort.Strings(syms)
	out := make([]quoteEntry, 0, len(syms))
	for _, sym := range syms {
		if q, ok := s.cache.LatestQuote(sym); ok {
			out = append(out, newQuoteEntry(sym, q))
		}
	}
	writeJSON(w, out)
}

// /quotes/{symbol} — single quote.
func (s *Server) handleQuote(w http.ResponseWriter, r *http.Request) {
	sym := strings.TrimPrefix(r.URL.Path, "/quotes/")
	if sym == "" {
		http.NotFound(w, r)
		return
	}
	q, ok := s.cache.LatestQuote(sym)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, newQuoteEntry(sym, q))
}

// alertRuleView is the redacted wire shape for /alerts. It deliberately
// omits Rule.Webhooks: per-rule webhook URLs frequently embed Slack/Discord
// secret tokens, and /alerts is readable by any local user on a loopback
// bind. Callers get a has_webhooks boolean instead of the URLs.
type alertRuleView struct {
	Symbol      string  `json:"symbol"`
	Condition   string  `json:"condition,omitempty"`
	Value       float64 `json:"value,omitempty"`
	Period      int     `json:"period,omitempty"`
	Enabled     bool    `json:"enabled"`
	Match       string  `json:"match,omitempty"`
	HasWebhooks bool    `json:"has_webhooks"`
}

// /alerts — configured rules with webhook URLs redacted.
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeJSON(w, map[string]any{"rules": []alertRuleView{}})
		return
	}
	rules := s.engine.Rules()
	views := make([]alertRuleView, 0, len(rules))
	for _, rl := range rules {
		views = append(views, alertRuleView{
			Symbol:      rl.Symbol,
			Condition:   string(rl.Condition),
			Value:       rl.Value,
			Period:      rl.Period,
			Enabled:     rl.Enabled,
			Match:       rl.Match,
			HasWebhooks: len(rl.Webhooks) > 0,
		})
	}
	writeJSON(w, map[string]any{"rules": views})
}

// /metrics — minimal Prometheus text format.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	syms := s.cache.Symbols()
	uptime := time.Since(s.started).Seconds()
	var sb strings.Builder
	fmt.Fprintf(&sb, "# HELP mkt_uptime_seconds Process uptime\n")
	fmt.Fprintf(&sb, "# TYPE mkt_uptime_seconds gauge\n")
	fmt.Fprintf(&sb, "mkt_uptime_seconds %f\n", uptime)
	fmt.Fprintf(&sb, "# HELP mkt_symbols_cached Symbols currently cached\n")
	fmt.Fprintf(&sb, "# TYPE mkt_symbols_cached gauge\n")
	fmt.Fprintf(&sb, "mkt_symbols_cached %d\n", len(syms))
	if s.engine != nil {
		fmt.Fprintf(&sb, "# HELP mkt_alert_rules Configured alert rules\n")
		fmt.Fprintf(&sb, "# TYPE mkt_alert_rules gauge\n")
		fmt.Fprintf(&sb, "mkt_alert_rules %d\n", len(s.engine.Rules()))
	}
	if s.drops != nil {
		fmt.Fprintf(&sb, "# HELP mkt_quote_drops_total Quotes dropped by TUI back-pressure\n")
		fmt.Fprintf(&sb, "# TYPE mkt_quote_drops_total counter\n")
		fmt.Fprintf(&sb, "mkt_quote_drops_total %d\n", s.drops())
	}
	// Per-symbol market gauges so a Prometheus/Grafana/Netdata can chart
	// live prices and momentum — not just process health. Symbols are
	// emitted in sorted order for tidy diffs; the label value is escaped
	// per the Prometheus text format (backslash, quote, newline). One pass
	// over the cache feeds all three gauges so a symbol cannot appear in
	// one block and vanish from the next.
	sort.Strings(syms)
	type gauge struct {
		label string
		q     provider.Quote
	}
	live := make([]gauge, 0, len(syms))
	for _, sym := range syms {
		if q, ok := s.cache.LatestQuote(sym); ok {
			live = append(live, gauge{escapeLabel(sym), q})
		}
	}
	if len(live) > 0 {
		now := time.Now()
		fmt.Fprintf(&sb, "# HELP mkt_price Latest price per symbol\n")
		fmt.Fprintf(&sb, "# TYPE mkt_price gauge\n")
		for _, g := range live {
			fmt.Fprintf(&sb, "mkt_price{symbol=\"%s\"} %g\n", g.label, g.q.Price)
		}
		fmt.Fprintf(&sb, "# HELP mkt_change_pct Percent change per symbol (24h crypto / day stock)\n")
		fmt.Fprintf(&sb, "# TYPE mkt_change_pct gauge\n")
		for _, g := range live {
			fmt.Fprintf(&sb, "mkt_change_pct{symbol=\"%s\"} %g\n", g.label, g.q.ChangePct)
		}
		// Staleness: a feed that dies mid-session keeps serving its last
		// price, so price alone cannot distinguish a dead provider from a
		// quiet market. Symbols the provider left unstamped are skipped
		// rather than reported as infinitely old.
		fmt.Fprintf(&sb, "# HELP mkt_quote_age_seconds Seconds since the last quote per symbol\n")
		fmt.Fprintf(&sb, "# TYPE mkt_quote_age_seconds gauge\n")
		for _, g := range live {
			if g.q.Timestamp.IsZero() {
				continue
			}
			fmt.Fprintf(&sb, "mkt_quote_age_seconds{symbol=\"%s\"} %g\n", g.label, now.Sub(g.q.Timestamp).Seconds())
		}
	}
	// Provider health counters self-registered with the observe package
	// (yahoo batch + session failures, coinbase WS reconnects, etc.).
	// Names are emitted in stable lex order so Prometheus diffs stay tidy.
	snap := observe.Snapshot()
	for _, name := range observe.SortedNames() {
		fmt.Fprintf(&sb, "# TYPE %s counter\n", name)
		fmt.Fprintf(&sb, "%s %d\n", name, snap[name])
	}
	_, _ = w.Write([]byte(sb.String()))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// escapeLabel escapes a Prometheus label value: backslash, double-quote,
// and newline, per the text exposition format. Symbols like "BTC-USD" or
// "FRED:GDP" pass through unchanged; the escaping guards against any
// exotic symbol a config could carry.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// tvPayload is the subset of TradingView's webhook body we use. TV lets
// users template the body freely; we recognize symbol/price/message and
// pass through anything else as the message.
type tvPayload struct {
	Symbol  string  `json:"symbol"`
	Ticker  string  `json:"ticker"` // alternate name TV templates often use
	Price   float64 `json:"price"`
	Close   float64 `json:"close"` // alternate name
	Message string  `json:"message"`
	Alert   string  `json:"alert"` // alternate name
}

// symbolPattern is the shape mkt's providers actually emit: an uppercase
// ticker plus the punctuation real symbols carry — "BTC-USD", "BRK.B",
// "^GSPC", "GC=F", "FRED:DGS10", "BINANCE:BTCUSDT". A webhook symbol
// becomes a cache key, an alert-history record, and a TUI cell, so
// anything outside this alphabet is refused instead of forwarded.
var symbolPattern = regexp.MustCompile(`^[A-Z0-9^][A-Z0-9._:^=/-]*$`)

// cleanText validates one free-text field arriving from an untrusted
// webhook before it reaches the alert pipeline, the on-disk alert history,
// and the TUI. Sanitizing at this boundary beats trusting every downstream
// renderer: an ANSI escape in a string a terminal UI prints can reposition
// the cursor, repaint the screen, or hide text entirely, and the same
// string is replayed from disk on every later run.
//
// Tabs, newlines, and carriage returns fold to single spaces — TradingView
// alert templates are routinely multi-line and that is benign. Everything
// else is rejected: any other control character (which includes ESC, so no
// ANSI/CSI/OSC sequence can survive), any Unicode format character (bidi
// overrides let a payload render as something other than what is stored),
// input that is not valid UTF-8, and anything past maxRunes.
func cleanText(field, s string, maxRunes int) (string, error) {
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("%s is not valid utf-8", field)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r):
			return "", fmt.Errorf("%s contains a control character (U+%04X)", field, r)
		case unicode.Is(unicode.Cf, r):
			return "", fmt.Errorf("%s contains a formatting character (U+%04X)", field, r)
		default:
			b.WriteRune(r)
		}
	}
	// Fields both trims and collapses runs of whitespace, so a payload
	// cannot pad an alert line out to the width of the terminal.
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > maxRunes {
		return "", fmt.Errorf("%s exceeds %d characters", field, maxRunes)
	}
	return out, nil
}

// firstNonEmpty returns the first argument that is not blank. TradingView
// templates name the same value differently depending on the alert type.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// rejectWebhook records and answers a refused webhook.
func rejectWebhook(w http.ResponseWriter, msg string, code int) {
	webhookRejected.Inc()
	http.Error(w, msg, code)
}

// /webhook/tradingview — accept a TradingView alert webhook and inject
// it through the alert engine's notifier fan-out (desktop, webhook,
// ntfy, Pushover, history). 200 on accept; 400 on parse failure or on a
// payload that fails validation.
func (s *Server) handleTradingView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// A browser is never a legitimate caller: TradingView posts server-side
	// and curl sends no Origin. Refusing any request that carries one
	// closes the cross-site / DNS-rebinding path to the one endpoint here
	// that changes state, which matters because the server is normally
	// bound to loopback where a page in the user's browser can reach it.
	if r.Header.Get("Origin") != "" {
		rejectWebhook(w, "cross-origin requests are not accepted", http.StatusForbidden)
		return
	}
	if !s.webhookLimiter.Allow() {
		rejectWebhook(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if s.engine == nil {
		rejectWebhook(w, "alerts disabled", http.StatusServiceUnavailable)
		return
	}
	// Buffer the body so we can attempt strict decode first, then fall
	// back to a loose decode if TV's template included extra fields.
	// MaxBytesReader caps memory regardless of Content-Length.
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		rejectWebhook(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var body tvPayload
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&body); err != nil {
		if err := json.Unmarshal(raw, &body); err != nil {
			rejectWebhook(w, "invalid json", http.StatusBadRequest)
			return
		}
	}
	sym, err := cleanText("symbol", firstNonEmpty(body.Symbol, body.Ticker), maxWebhookSymbolRunes)
	if err != nil {
		rejectWebhook(w, err.Error(), http.StatusBadRequest)
		return
	}
	sym = strings.ToUpper(sym)
	if sym != "" && !symbolPattern.MatchString(sym) {
		rejectWebhook(w, "symbol is not a recognizable ticker", http.StatusBadRequest)
		return
	}
	msg, err := cleanText("message", firstNonEmpty(body.Message, body.Alert), maxWebhookMessageRunes)
	if err != nil {
		rejectWebhook(w, err.Error(), http.StatusBadRequest)
		return
	}
	price := body.Price
	if price == 0 {
		price = body.Close
	}
	if msg == "" {
		msg = fmt.Sprintf("TradingView alert: %s @ %.4f", sym, price)
	}
	s.engine.Inject(alert.TriggeredAlert{
		Rule:      alert.Rule{Symbol: sym},
		Price:     price,
		Message:   msg,
		Timestamp: time.Now(),
	})
	webhookAccepted.Inc()
	w.WriteHeader(http.StatusOK)
}
