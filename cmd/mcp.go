package cmd

import (
	"context"
	"fmt"
	"os"
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

			srv := mcp.New(tools)
			return srv.Serve(context.Background(), os.Stdin, os.Stdout)
		},
	}
	rootCmd.AddCommand(mcpCmd)
}
