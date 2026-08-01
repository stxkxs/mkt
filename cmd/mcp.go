package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/mcp"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
)

// maxHistoryLimit caps how many bars an MCP client can request in one
// query_history call, so a caller can't ask for an unbounded series.
const maxHistoryLimit = 1000

// portfolioFetchWorkers bounds the concurrent price lookups behind
// get_portfolio. A portfolio with thirty positions used to take thirty
// sequential round trips, which is well past the point where an MCP client
// gives up on the call.
const portfolioFetchWorkers = 6

// liveProbeTimeout caps the attempt to reach a running mkt's read API. It is
// an optimization — the provider fallback always exists — so it must fail
// fast rather than delay every quote.
const liveProbeTimeout = 2 * time.Second

// Values of quoteResult.Source. The distinction matters: an agent asked for
// "the current price" and a daily candle's close is not one, so the answer
// says which it got.
const (
	sourceLive       = "live"
	sourceDailyClose = "daily-close"
)

// MCP tool result shapes. Returning typed structs (rather than pre-formatted
// prose) means the server emits them as structuredContent JSON an agent can
// parse directly — see mcp.toolResult.
type quoteResult struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	AsOf      string  `json:"asOf"`
	Source    string  `json:"source"`
	Change    float64 `json:"change,omitempty"`
	ChangePct float64 `json:"changePercent,omitempty"`
	Note      string  `json:"note,omitempty"`
}

type ohlcvBar struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type historyResult struct {
	Symbol string     `json:"symbol"`
	Count  int        `json:"count"`
	Bars   []ohlcvBar `json:"bars"`
}

type alertRuleResult struct {
	Symbol    string  `json:"symbol"`
	Condition string  `json:"condition"`
	Value     float64 `json:"value"`
	Enabled   bool    `json:"enabled"`
}

type alertsResult struct {
	Count int               `json:"count"`
	Rules []alertRuleResult `json:"rules"`
}

// portfolioResult carries the coverage of the valuation alongside it. A P&L
// computed from half the positions is not a smaller P&L, it is a wrong one,
// and an agent has no way to know that from the number alone.
type portfolioResult struct {
	Name         string   `json:"name"`
	Cost         float64  `json:"cost"`
	Value        float64  `json:"value"`
	PnL          float64  `json:"pnl"`
	PnLPercent   float64  `json:"pnlPercent"`
	FullyPriced  bool     `json:"fullyPriced"`
	Coverage     float64  `json:"coverage"`
	Unpriced     []string `json:"unpriced,omitempty"`
	UnpricedCost float64  `json:"unpricedCost,omitempty"`
	Errors       []string `json:"fetchErrors,omitempty"`
	Note         string   `json:"note,omitempty"`
}

func init() {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP (Model Context Protocol) server over stdio",
		Long: `Exposes mkt's read-only data via the Model Context Protocol over
stdio so Claude Code / Claude Desktop / other MCP clients can query
quotes, portfolio summaries, alerts, and historical OHLCV.

Tools:
  get_quote(symbol)               — live price when a dashboard is listening,
                                    otherwise the last daily close (labelled)
  query_history(symbol, limit)    — daily OHLCV via the active history provider
  get_alerts()                    — configured alert rules
  get_portfolio(name)             — portfolio summary computed from config

If a dashboard is serving its read-only HTTP API (--listen / ` + EnvListen + `),
get_quote and get_portfolio read live cached quotes from it and fall back to
the history providers when it is unreachable.`,
		RunE: runMCP,
	}
	mcpCmd.Flags().Bool("expose-config", false, "expose the mkt://config resource (secrets redacted); off by default")
	mcpCmd.Flags().Bool("expose-portfolio", false, "expose the get_portfolio tool + mkt://portfolios resource (holdings, cost basis, P&L); off by default")
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	res, err := config.LoadWithResult()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// stdout is the MCP transport — the banner must never go there.
	warnDegradedConfig(cmd.ErrOrStderr(), res)
	cfg := res.Config

	histProvider := historyProvider(cfg)
	live := liveClientFromFlags(cmd)

	// One canonical conversion, shared with the dashboard: it carries the
	// transaction log, the tax method and the Materialize fold. Re-deriving
	// it here is what used to make get_portfolio value stale snapshot
	// holdings while the dashboard showed the materialized ones.
	portfolios := portfoliosFromConfig(cfg.Portfolios)

	tools := []mcp.Tool{
		{
			Name: "get_quote",
			Description: "Fetch the price for a symbol. Returns source=\"live\" when a running mkt " +
				"dashboard's HTTP API is reachable, and source=\"daily-close\" when it falls back to " +
				"the most recent daily candle — which is a close, not a live price.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"symbol": map[string]any{"type": "string"}},
				"required":   []string{"symbol"},
			},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sym, _ := args["symbol"].(string)
				if sym == "" {
					return nil, fmt.Errorf("symbol required")
				}
				return quoteFor(ctx, live, histProvider, sym)
			},
		},
		{
			Name:        "query_history",
			Description: "Fetch up to N most-recent daily OHLCV bars.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "default": 30},
				},
				"required": []string{"symbol"},
			},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sym, _ := args["symbol"].(string)
				if sym == "" {
					return nil, fmt.Errorf("symbol required")
				}
				limit := 30
				if v, ok := args["limit"].(float64); ok && v > 0 {
					limit = int(v)
				}
				if limit > maxHistoryLimit {
					limit = maxHistoryLimit
				}
				bars, err := histProvider.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d, Limit: limit})
				if err != nil {
					return nil, err
				}
				res := historyResult{Symbol: sym, Count: len(bars), Bars: make([]ohlcvBar, 0, len(bars))}
				for _, b := range bars {
					res.Bars = append(res.Bars, ohlcvBar{
						Date:   b.Time.Format("2006-01-02"),
						Open:   b.Open,
						High:   b.High,
						Low:    b.Low,
						Close:  b.Close,
						Volume: b.Volume,
					})
				}
				return res, nil
			},
		},
		{
			Name:        "get_alerts",
			Description: "List configured alert rules.",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				res := alertsResult{Count: len(cfg.Alerts), Rules: make([]alertRuleResult, 0, len(cfg.Alerts))}
				for _, r := range cfg.Alerts {
					res.Rules = append(res.Rules, alertRuleResult{
						Symbol:    r.Symbol,
						Condition: r.Condition,
						Value:     r.Value,
						Enabled:   r.Enabled,
					})
				}
				return res, nil
			},
		},
		{
			Name: "get_portfolio",
			Description: "Get portfolio summary by name. Positions are priced concurrently; " +
				"the result reports coverage and lists any symbol that could not be priced, so " +
				"a partial valuation is never mistaken for a complete one.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []string{"name"},
			},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				name, _ := args["name"].(string)
				for _, pf := range portfolios {
					if pf.Name != name {
						continue
					}
					return summarizePortfolio(ctx, live, histProvider, pf), nil
				}
				return nil, fmt.Errorf("portfolio %q not found", name)
			},
		},
	}

	resources := []mcp.Resource{
		{
			URI:         "mkt://config",
			Name:        "Configuration",
			Description: "mkt config with secrets redacted (watchlist, portfolios, alerts, etc.)",
			MimeType:    "text/yaml",
			Handler: func(ctx context.Context) (string, error) {
				// Never hand raw config.yaml to an agent — it holds the
				// Pushover token, webhook/ntfy destinations, and per-rule
				// webhook URLs (often embedding Slack/Discord tokens). Marshal
				// a redacted copy instead. This resource is also gated behind
				// --expose-config (default off); see below.
				red := *cfg
				red.WebhookURL = ""
				red.NtfyTopic = ""
				red.NtfyServer = ""
				red.PushoverUser = ""
				red.PushoverToken = ""
				red.Alerts = make([]config.AlertRule, len(cfg.Alerts))
				copy(red.Alerts, cfg.Alerts)
				for i := range red.Alerts {
					red.Alerts[i].Webhooks = nil
				}
				b, err := yaml.Marshal(&red)
				if err != nil {
					return "", err
				}
				return string(b), nil
			},
		},
		{
			URI:         "mkt://watchlist",
			Name:        "Watchlist",
			Description: "All configured watchlist symbols (deduplicated union of every group).",
			MimeType:    "application/json",
			Handler: func(ctx context.Context) (string, error) {
				syms := append([]string{}, cfg.Watchlist...)
				for _, w := range cfg.Watchlists {
					syms = append(syms, w.Symbols...)
				}
				seen := map[string]struct{}{}
				out := syms[:0]
				for _, s := range syms {
					if _, ok := seen[s]; ok {
						continue
					}
					seen[s] = struct{}{}
					out = append(out, s)
				}
				b, _ := json.Marshal(out)
				return string(b), nil
			},
		},
		{
			URI:         "mkt://portfolios",
			Name:        "Portfolio names",
			Description: "List of configured portfolio names.",
			MimeType:    "application/json",
			Handler: func(ctx context.Context) (string, error) {
				names := make([]string, 0, len(cfg.Portfolios))
				for _, p := range cfg.Portfolios {
					names = append(names, p.Name)
				}
				b, _ := json.Marshal(names)
				return string(b), nil
			},
		},
	}

	prompts := []mcp.Prompt{
		{
			Name:        "analyze_symbol",
			Description: "Suggest an analysis of a symbol's recent price action using mkt tools.",
			Arguments:   []mcp.PromptArg{{Name: "symbol", Description: "Ticker or product id", Required: true}},
			Handler: func(ctx context.Context, args map[string]string) (string, error) {
				sym := args["symbol"]
				if sym == "" {
					return "", fmt.Errorf("symbol required")
				}
				return fmt.Sprintf(`Analyze the recent price action for %s.

1. Call get_quote with symbol="%s" to see the current price. Check the "source"
   field: "daily-close" means it is the last daily candle's close, not a live price.
2. Call query_history with symbol="%s" and limit=30 to see the last month of daily bars.
3. Identify the recent trend (up/down/range), the day's range vs the 30-day range, and any obvious support/resistance levels.
4. Note volatility (which days had unusually wide ranges).
5. Conclude with a one-paragraph summary suitable for a watchlist note.`, sym, sym, sym), nil
			},
		},
		{
			Name:        "portfolio_review",
			Description: "Walk through a portfolio's positions, P&L drivers, and concentrations.",
			Arguments:   []mcp.PromptArg{{Name: "name", Description: "Portfolio name", Required: true}},
			Handler: func(ctx context.Context, args map[string]string) (string, error) {
				name := args["name"]
				if name == "" {
					return "", fmt.Errorf("name required")
				}
				return fmt.Sprintf(`Review portfolio "%s".

1. Call get_portfolio with name="%s" to get the totals. If "fullyPriced" is false,
   say so up front and name the symbols in "unpriced" — the P&L excludes them.
2. For each symbol in the portfolio, call get_quote and query_history to assess recent moves.
3. Identify the largest absolute and percentage P&L contributors.
4. Flag any single position that exceeds 25%% of the portfolio's market value.
5. Conclude with two suggestions: one to consider trimming, one to consider adding to (with reasoning).`, name, name), nil
			},
		},
	}

	// Default-deny the sensitive surfaces. The config resource (even
	// redacted) and the portfolio tool/resource (holdings, cost basis,
	// P&L) are exposed only when the operator explicitly opts in, so a
	// prompt-injected agent can't enumerate them by default.
	exposeConfig, _ := cmd.Flags().GetBool("expose-config")
	exposePortfolio, _ := cmd.Flags().GetBool("expose-portfolio")

	filteredRes := resources[:0]
	for _, r := range resources {
		if r.URI == "mkt://config" && !exposeConfig {
			continue
		}
		if r.URI == "mkt://portfolios" && !exposePortfolio {
			continue
		}
		filteredRes = append(filteredRes, r)
	}
	filteredTools := tools[:0]
	for _, t := range tools {
		if t.Name == "get_portfolio" && !exposePortfolio {
			continue
		}
		filteredTools = append(filteredTools, t)
	}

	srv := mcp.New("mkt", version).WithTools(filteredTools...).WithResources(filteredRes...).WithPrompts(prompts...)
	return srv.Serve(context.Background(), os.Stdin, os.Stdout)
}

// ───────────────────────────── quote sourcing ─────────────────────────────

// quoteFor answers with the freshest price available, and says which one it
// is. The old implementation asked the history provider for one daily bar
// and labelled its close as the current price: on any weekday afternoon
// that is yesterday's number, handed to an agent as today's.
func quoteFor(ctx context.Context, live *liveQuoteClient, hist historyFetcher, sym string) (quoteResult, error) {
	if live != nil {
		if q, err := live.quote(ctx, sym); err == nil {
			return q, nil
		}
	}
	bars, err := hist.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d, Limit: 1})
	if err != nil {
		return quoteResult{}, err
	}
	if len(bars) == 0 {
		return quoteResult{}, fmt.Errorf("no data for %s", sym)
	}
	last := bars[len(bars)-1]
	return quoteResult{
		Symbol: sym,
		Price:  last.Close,
		AsOf:   last.Time.Format(time.RFC3339),
		Source: sourceDailyClose,
		Note:   "closing price of the most recent daily bar, not a live quote",
	}, nil
}

// liveQuoteClient reads cached quotes from a running mkt dashboard's
// read-only HTTP API.
type liveQuoteClient struct {
	base   string
	token  string
	client *http.Client
}

// liveClientFromFlags builds a client for the read API when one is
// configured, and returns nil when it is not — get_quote then falls back to
// the history providers.
func liveClientFromFlags(cmd *cobra.Command) *liveQuoteClient {
	addr, _ := cmd.Flags().GetString("listen")
	if addr == "" {
		return nil
	}
	token, _ := cmd.Flags().GetString("listen-token")
	base := addr
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	base = strings.TrimSuffix(base, "/")
	if _, err := url.Parse(base); err != nil {
		return nil
	}
	return &liveQuoteClient{
		base:   base,
		token:  token,
		client: &http.Client{Timeout: liveProbeTimeout},
	}
}

// quote fetches one symbol from /quotes/{symbol}.
func (c *liveQuoteClient) quote(ctx context.Context, sym string) (quoteResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, liveProbeTimeout)
	defer cancel()

	endpoint := c.base + "/quotes/" + url.PathEscape(sym)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return quoteResult{}, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return quoteResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return quoteResult{}, fmt.Errorf("read API %s: %s", endpoint, resp.Status)
	}
	var entry struct {
		Symbol    string  `json:"symbol"`
		Price     float64 `json:"price"`
		Change    float64 `json:"change"`
		ChangePct float64 `json:"change_pct"`
		Time      string  `json:"time"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&entry); err != nil {
		return quoteResult{}, err
	}
	if entry.Price == 0 {
		return quoteResult{}, fmt.Errorf("read API returned no price for %s", sym)
	}
	return quoteResult{
		Symbol:    entry.Symbol,
		Price:     entry.Price,
		AsOf:      entry.Time,
		Source:    sourceLive,
		Change:    entry.Change,
		ChangePct: entry.ChangePct,
	}, nil
}

// ─────────────────────────── portfolio valuation ───────────────────────────

// summarizePortfolio prices every holding concurrently and reports how much
// of the portfolio the resulting P&L actually covers.
//
// The previous implementation fetched serially and discarded every fetch
// error, so a portfolio where half the symbols failed returned a confident
// P&L computed from the other half — a number an agent would then reason
// about as authoritative.
func summarizePortfolio(ctx context.Context, live *liveQuoteClient, hist historyFetcher, pf portfolio.Portfolio) portfolioResult {
	symbols := make([]string, 0, len(pf.Holdings))
	seen := make(map[string]bool, len(pf.Holdings))
	for _, h := range pf.Holdings {
		if h.Symbol == "" || seen[h.Symbol] {
			continue
		}
		seen[h.Symbol] = true
		symbols = append(symbols, h.Symbol)
	}

	quotes, errs := fetchQuotes(ctx, live, hist, symbols)
	sum := portfolio.Evaluate(pf.Holdings, quotes)

	res := portfolioResult{
		Name:         pf.Name,
		Cost:         sum.TotalCost,
		Value:        sum.TotalValue,
		PnL:          sum.TotalPnL,
		PnLPercent:   sum.TotalPnLPct,
		FullyPriced:  sum.FullyPriced(),
		Coverage:     sum.Coverage(),
		Unpriced:     sum.Unpriced,
		UnpricedCost: sum.UnpricedCost(),
		Errors:       errs,
	}
	if !res.FullyPriced {
		res.Note = fmt.Sprintf("%d of %d positions could not be priced; value and pnl exclude them (coverage %.0f%% of cost basis)",
			len(sum.Unpriced), len(pf.Holdings), sum.Coverage()*100)
	}
	return res
}

// fetchQuotes prices symbols with bounded parallelism, preferring a running
// dashboard's cached quotes and falling back to the history providers.
// Errors are collected and returned rather than discarded.
func fetchQuotes(ctx context.Context, live *liveQuoteClient, hist historyFetcher, symbols []string) (map[string]provider.Quote, []string) {
	quotes := make(map[string]provider.Quote, len(symbols))
	if len(symbols) == 0 {
		return quotes, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		mu   sync.Mutex
		errs []string
		wg   sync.WaitGroup
	)
	work := make(chan string)
	workers := min(portfolioFetchWorkers, len(symbols))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range work {
				q, err := quoteFor(ctx, live, hist, sym)
				mu.Lock()
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", sym, err))
				} else {
					quotes[sym] = provider.Quote{Symbol: sym, Price: q.Price, Change: q.Change, ChangePct: q.ChangePct}
				}
				mu.Unlock()
			}
		}()
	}
	for _, sym := range symbols {
		select {
		case work <- sym:
		case <-ctx.Done():
		}
	}
	close(work)
	wg.Wait()
	return quotes, errs
}
