// Package api exposes a small read-only HTTP surface for scripting and
// monitoring: /quotes, /quotes/{symbol}, /alerts, /metrics.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/observe"
	"github.com/stxkxs/mkt/internal/provider"
)

// maxWebhookBytes caps the request body for /webhook/tradingview so an
// oversize POST can't OOM the process. TradingView alerts are tiny —
// the body usually fits in a few hundred bytes.
const maxWebhookBytes = 64 * 1024

// webhook rate limit: the /webhook/tradingview endpoint injects alerts
// that fan out to desktop / push / webhook notifiers, so an unthrottled
// caller could drive notification spam (and drain a paid Pushover quota).
// Legitimate TradingView alerts are infrequent; 1/s sustained with a
// small burst is generous while capping abuse.
const (
	webhookRatePerSec = 1
	webhookBurst      = 5
)

// Server is a small read-only HTTP frontend.
type Server struct {
	addr           string
	cache          *market.Cache
	engine         *alert.Engine
	token          string // optional bearer token; empty disables auth
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
	return s
}

// Start launches the server in a goroutine. ListenAndServe errors are
// logged through the caller's logger (best-effort: bind failures are
// surfaced via stderr).
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/quotes", s.auth(s.handleQuotes))
	mux.HandleFunc("/quotes/", s.auth(s.handleQuote))
	mux.HandleFunc("/alerts", s.auth(s.handleAlerts))
	mux.HandleFunc("/metrics", s.auth(s.handleMetrics))
	mux.HandleFunc("/webhook/tradingview", s.auth(s.handleTradingView))
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "api: listen %s: %v\n", s.addr, err)
		}
	}()
	return nil
}

// auth wraps a handler with token authentication when a token has been
// configured. With no token configured it's a no-op.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	if s.token == "" {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
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
type quoteEntry struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"change_pct"`
	Dir       string  `json:"dir"`
}

func newQuoteEntry(sym string, q provider.Quote) quoteEntry {
	return quoteEntry{
		Symbol:    sym,
		Price:     q.Price,
		Change:    q.Change,
		ChangePct: q.ChangePct,
		Dir:       direction(q.ChangePct),
	}
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

// /alerts — rules + recent triggers (delegates to engine).
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeJSON(w, map[string]any{"rules": []any{}})
		return
	}
	writeJSON(w, map[string]any{"rules": s.engine.Rules()})
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
	// Per-symbol market gauges so a Prometheus/Grafana/Netdata can chart
	// live prices and momentum — not just process health. Symbols are
	// emitted in sorted order for tidy diffs; the label value is escaped
	// per the Prometheus text format (backslash, quote, newline).
	sort.Strings(syms)
	if len(syms) > 0 {
		fmt.Fprintf(&sb, "# HELP mkt_price Latest price per symbol\n")
		fmt.Fprintf(&sb, "# TYPE mkt_price gauge\n")
		for _, sym := range syms {
			if q, ok := s.cache.LatestQuote(sym); ok {
				fmt.Fprintf(&sb, "mkt_price{symbol=\"%s\"} %g\n", escapeLabel(sym), q.Price)
			}
		}
		fmt.Fprintf(&sb, "# HELP mkt_change_pct Percent change per symbol (24h crypto / day stock)\n")
		fmt.Fprintf(&sb, "# TYPE mkt_change_pct gauge\n")
		for _, sym := range syms {
			if q, ok := s.cache.LatestQuote(sym); ok {
				fmt.Fprintf(&sb, "mkt_change_pct{symbol=\"%s\"} %g\n", escapeLabel(sym), q.ChangePct)
			}
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

// /webhook/tradingview — accept a TradingView alert webhook and inject
// it through the alert engine's notifier fan-out (desktop, webhook,
// ntfy, Pushover, history). 200 on accept; 400 on parse failure.
func (s *Server) handleTradingView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookLimiter.Allow() {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if s.engine == nil {
		http.Error(w, "alerts disabled", http.StatusServiceUnavailable)
		return
	}
	// Buffer the body so we can attempt strict decode first, then fall
	// back to a loose decode if TV's template included extra fields.
	// MaxBytesReader caps memory regardless of Content-Length.
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var body tvPayload
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&body); err != nil {
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	}
	sym := strings.ToUpper(strings.TrimSpace(body.Symbol))
	if sym == "" {
		sym = strings.ToUpper(strings.TrimSpace(body.Ticker))
	}
	price := body.Price
	if price == 0 {
		price = body.Close
	}
	msg := strings.TrimSpace(body.Message)
	if msg == "" {
		msg = strings.TrimSpace(body.Alert)
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
	w.WriteHeader(http.StatusOK)
}
