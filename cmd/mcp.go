package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/mcp"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/fred"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

func init() {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP (Model Context Protocol) server over stdio",
		Long: `Exposes mkt's read-only data via the Model Context Protocol over
stdio so Claude Code / Claude Desktop / other MCP clients can query
quotes, portfolio summaries, alerts, and historical OHLCV.

Tools:
  get_quote(symbol)               — current price via Yahoo/Coinbase chart
  query_history(symbol, limit)    — daily OHLCV via the active history provider
  get_alerts()                    — configured alert rules
  get_portfolio(name)             — portfolio summary computed from config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			yahooProv := yahoo.New(cfg.PollDuration())
			coinbaseProv := coinbase.New()
			fredProv := fred.New()
			histProvider := market.NewMultiHistoryProvider(fredProv, coinbaseProv, yahooProv)

			tools := []mcp.Tool{
				{
					Name:        "get_quote",
					Description: "Fetch the current price for a symbol (Yahoo or Coinbase).",
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
						p, err := histProvider.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d, Limit: 1})
						if err != nil {
							return nil, err
						}
						if len(p) == 0 {
							return nil, fmt.Errorf("no data for %s", sym)
						}
						last := p[len(p)-1]
						return fmt.Sprintf("%s: $%.4f (as of %s)", sym, last.Close, last.Time.Format(time.RFC3339)), nil
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
						limit := 30
						if v, ok := args["limit"].(float64); ok && v > 0 {
							limit = int(v)
						}
						bars, err := histProvider.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d, Limit: limit})
						if err != nil {
							return nil, err
						}
						var sb strings.Builder
						sb.WriteString(fmt.Sprintf("%s (%d bars):\n", sym, len(bars)))
						for _, b := range bars {
							sb.WriteString(fmt.Sprintf("  %s O=%.2f H=%.2f L=%.2f C=%.2f V=%.0f\n",
								b.Time.Format("2006-01-02"), b.Open, b.High, b.Low, b.Close, b.Volume))
						}
						return sb.String(), nil
					},
				},
				{
					Name:        "get_alerts",
					Description: "List configured alert rules.",
					InputSchema: map[string]any{"type": "object"},
					Handler: func(ctx context.Context, args map[string]any) (any, error) {
						var sb strings.Builder
						sb.WriteString(fmt.Sprintf("%d alert rule(s):\n", len(cfg.Alerts)))
						for _, r := range cfg.Alerts {
							sb.WriteString(fmt.Sprintf("  %s %s %v (enabled=%v)\n", r.Symbol, r.Condition, r.Value, r.Enabled))
						}
						return sb.String(), nil
					},
				},
				{
					Name:        "get_portfolio",
					Description: "Get portfolio summary by name.",
					InputSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"name": map[string]any{"type": "string"}},
						"required":   []string{"name"},
					},
					Handler: func(ctx context.Context, args map[string]any) (any, error) {
						name, _ := args["name"].(string)
						for _, cp := range cfg.Portfolios {
							if cp.Name != name {
								continue
							}
							var holdings []portfolio.Holding
							for _, h := range cp.Holdings {
								holdings = append(holdings, portfolio.Holding{
									Symbol: h.Symbol, Name: h.Name,
									Quantity: h.Quantity, CostBasis: h.CostBasis,
								})
							}
							quotes := map[string]provider.Quote{}
							for _, h := range holdings {
								bars, _ := histProvider.History(ctx, provider.HistoryParams{Symbol: h.Symbol, Interval: provider.Interval1d, Limit: 1})
								if len(bars) > 0 {
									quotes[h.Symbol] = provider.Quote{Symbol: h.Symbol, Price: bars[len(bars)-1].Close}
								}
							}
							sum := portfolio.Evaluate(holdings, quotes)
							return fmt.Sprintf("%s: cost=$%.2f value=$%.2f pnl=$%.2f (%+.2f%%)",
								name, sum.TotalCost, sum.TotalValue, sum.TotalPnL, sum.TotalPnLPct), nil
						}
						return nil, fmt.Errorf("portfolio %q not found", name)
					},
				},
			}

			resources := []mcp.Resource{
				{
					URI:         "mkt://config",
					Name:        "Configuration",
					Description: "Current mkt YAML config (watchlist, portfolios, alerts, etc.)",
					MimeType:    "text/yaml",
					Handler: func(ctx context.Context) (string, error) {
						b, err := os.ReadFile(filepath.Join(config.ConfigDir(), "config.yaml"))
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

1. Call get_quote with symbol="%s" to see the current price.
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

1. Call get_portfolio with name="%s" to get the totals.
2. For each symbol in the portfolio, call get_quote and query_history to assess recent moves.
3. Identify the largest absolute and percentage P&L contributors.
4. Flag any single position that exceeds 25%% of the portfolio's market value.
5. Conclude with two suggestions: one to consider trimming, one to consider adding to (with reasoning).`, name, name), nil
					},
				},
			}

			srv := mcp.New("mkt", version).WithTools(tools...).WithResources(resources...).WithPrompts(prompts...)
			return srv.Serve(context.Background(), os.Stdin, os.Stdout)
		},
	}
	rootCmd.AddCommand(mcpCmd)
}
