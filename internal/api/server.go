// Package api exposes a small read-only HTTP surface for scripting and
// monitoring: /quotes, /quotes/{symbol}, /alerts, /metrics.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
)

// Server is a small read-only HTTP frontend.
type Server struct {
	addr    string
	cache   *market.Cache
	engine  *alert.Engine
	started time.Time
	srv     *http.Server
}

// New constructs a Server. addr is e.g. ":9999".
func New(addr string, cache *market.Cache, engine *alert.Engine) *Server {
	return &Server{
		addr:    addr,
		cache:   cache,
		engine:  engine,
		started: time.Now(),
	}
}

// Start launches the server in a goroutine. Returns the bound port via
// the server's underlying listener; caller can monitor errors via Wait.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/quotes", s.handleQuotes)
	mux.HandleFunc("/quotes/", s.handleQuote)
	mux.HandleFunc("/alerts", s.handleAlerts)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/webhook/tradingview", s.handleTradingView)
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = s.srv.ListenAndServe()
	}()
	return nil
}

// Shutdown stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// /quotes — list of {symbol, price} for every cached symbol.
func (s *Server) handleQuotes(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"price"`
	}
	syms := s.cache.Symbols()
	sort.Strings(syms)
	out := make([]entry, 0, len(syms))
	for _, sym := range syms {
		if p, ok := s.cache.Latest(sym); ok {
			out = append(out, entry{Symbol: sym, Price: p})
		}
	}
	writeJSON(w, out)
}

// /quotes/{symbol} — single price.
func (s *Server) handleQuote(w http.ResponseWriter, r *http.Request) {
	sym := strings.TrimPrefix(r.URL.Path, "/quotes/")
	if sym == "" {
		http.NotFound(w, r)
		return
	}
	price, ok := s.cache.Latest(sym)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{"symbol": sym, "price": price})
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
	_, _ = w.Write([]byte(sb.String()))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
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
	if s.engine == nil {
		http.Error(w, "alerts disabled", http.StatusServiceUnavailable)
		return
	}
	var body tvPayload
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // first attempt strict; fall back to loose below
	if err := dec.Decode(&body); err != nil {
		// retry loose to tolerate extra TV template fields
		r2 := json.NewDecoder(strings.NewReader(consumeBody(r)))
		if err := r2.Decode(&body); err != nil {
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

// consumeBody drains the request body into a string. Used for the
// fallback decode after DisallowUnknownFields rejected the first try.
func consumeBody(r *http.Request) string {
	defer r.Body.Close()
	b := make([]byte, 0, 1024)
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			b = append(b, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(b)
}
