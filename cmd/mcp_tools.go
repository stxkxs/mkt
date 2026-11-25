package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/mcp"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
)

// defaultHistoryLimit is the bar count query_history returns when the
// caller names none.
const defaultHistoryLimit = 30

// symbolHint is appended to every error about a missing or unusable symbol
// argument, so the calling model learns the accepted forms from the error
// rather than from a second round of guessing.
const symbolHint = `pass a ticker such as "AAPL", a crypto pair such as "BTC-USD", an index such as "^GSPC", or a FRED series such as "FRED:GDP"`

// mcpTools builds the tool set `mkt mcp` serves. Every tool is read-only;
// get_portfolio is filtered out later unless the operator exposes it.
func mcpTools(cfg *config.Config, live *liveQuoteClient, hist historyFetcher, portfolios []portfolio.Portfolio) []mcp.Tool {
	return []mcp.Tool{
		getQuoteTool(live, hist),
		queryHistoryTool(hist),
		getAlertsTool(cfg),
		getPortfolioTool(live, hist, portfolios),
	}
}

func getQuoteTool(live *liveQuoteClient, hist historyFetcher) mcp.Tool {
	return mcp.Tool{
		Name:        "get_quote",
		Title:       "Get quote",
		Annotations: mcp.ReadOnlyTool(),
		Description: "Fetch the price for one symbol. Returns source=\"live\" when a running mkt " +
			"dashboard's HTTP API is reachable, and source=\"daily-close\" when it falls back to " +
			"the most recent daily candle — which is a close, not a live price. Use query_history " +
			"for more than one bar.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"symbol": map[string]any{"type": "string", "minLength": 1, "description": symbolDescription},
			},
			"required": []string{"symbol"},
		},
		OutputSchema: quoteOutputSchema,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			sym, err := symbolArg(args)
			if err != nil {
				return nil, err
			}
			return quoteFor(ctx, live, hist, sym)
		},
	}
}

func queryHistoryTool(hist historyFetcher) mcp.Tool {
	return mcp.Tool{
		Name:        "query_history",
		Title:       "Query daily price history",
		Annotations: mcp.ReadOnlyTool(),
		Description: "Fetch the most recent daily OHLCV bars for a symbol, oldest first. Bars are " +
			"daily closes: the newest one is the previous session until today's close is published. " +
			"Does not return intraday bars.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"symbol": map[string]any{"type": "string", "minLength": 1, "description": symbolDescription},
				"limit": map[string]any{
					"type": "integer", "minimum": 1, "maximum": maxHistoryLimit, "default": defaultHistoryLimit,
					"description": "number of most recent daily bars to return",
				},
			},
			"required": []string{"symbol"},
		},
		OutputSchema: historyOutputSchema,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			sym, err := symbolArg(args)
			if err != nil {
				return nil, err
			}
			limit, err := limitArg(args)
			if err != nil {
				return nil, err
			}
			bars, err := hist.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d, Limit: limit})
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
	}
}

func getAlertsTool(cfg *config.Config) mcp.Tool {
	return mcp.Tool{
		Name:        "get_alerts",
		Title:       "List alert rules",
		Annotations: mcp.ReadOnlyTool(),
		Description: "List the configured alert rules and whether each is enabled. " +
			"Notification destinations are omitted — the result never says where an alert is sent. " +
			"Does not report which alerts have fired.",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: alertsOutputSchema,
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
	}
}

func getPortfolioTool(live *liveQuoteClient, hist historyFetcher, portfolios []portfolio.Portfolio) mcp.Tool {
	return mcp.Tool{
		Name:        "get_portfolio",
		Title:       "Get portfolio summary",
		Annotations: mcp.ReadOnlyTool(),
		Description: "Get one portfolio's cost, value and P&L by name. Positions are priced concurrently; " +
			"the result reports coverage and lists any symbol that could not be priced, so a partial " +
			"valuation is never mistaken for a complete one. The mkt://portfolios resource lists the names.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "minLength": 1, "description": "portfolio name, exactly as configured"},
			},
			"required": []string{"name"},
		},
		OutputSchema: portfolioOutputSchema,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			name, _ := args["name"].(string)
			names := make([]string, 0, len(portfolios))
			for _, pf := range portfolios {
				if pf.Name == name {
					return summarizePortfolio(ctx, live, hist, pf), nil
				}
				names = append(names, fmt.Sprintf("%q", pf.Name))
			}
			if len(names) == 0 {
				return nil, fmt.Errorf("portfolio %q not found: no portfolios are configured", name)
			}
			return nil, fmt.Errorf("portfolio %q not found; configured portfolios: %s", name, strings.Join(names, ", "))
		},
	}
}

const symbolDescription = "ticker (AAPL), crypto pair (BTC-USD), index (^GSPC) or FRED series (FRED:GDP)"

// symbolArg reads the required symbol argument.
func symbolArg(args map[string]any) (string, error) {
	raw, present := args["symbol"]
	sym, isString := raw.(string)
	switch {
	case !present:
		return "", fmt.Errorf("symbol is required: %s", symbolHint)
	case !isString:
		return "", fmt.Errorf("symbol must be a string, got %T: %s", raw, symbolHint)
	case strings.TrimSpace(sym) == "":
		return "", fmt.Errorf("symbol is empty: %s", symbolHint)
	}
	return strings.TrimSpace(sym), nil
}

// limitArg reads query_history's optional limit. A value outside the
// schema's range is refused rather than clamped, so the caller never
// receives a different number of bars than it asked for without being told.
func limitArg(args map[string]any) (int, error) {
	raw, present := args["limit"]
	if !present || raw == nil {
		return defaultHistoryLimit, nil
	}
	v, ok := raw.(float64)
	if !ok || v != float64(int(v)) {
		return 0, fmt.Errorf("limit must be a whole number from 1 to %d, got %v", maxHistoryLimit, raw)
	}
	if v < 1 || v > maxHistoryLimit {
		return 0, fmt.Errorf("limit must be from 1 to %d, got %d", maxHistoryLimit, int(v))
	}
	return int(v), nil
}

// Output schemas for the tools' structuredContent. Each mirrors the JSON
// encoding of its result type; TestMCPToolOutputSchemasMatchResults fails
// when a field is added to one without the other.
var (
	quoteOutputSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"symbol":        map[string]any{"type": "string"},
			"price":         map[string]any{"type": "number"},
			"asOf":          map[string]any{"type": "string", "description": "RFC 3339 time of the price"},
			"source":        map[string]any{"type": "string", "enum": []string{sourceLive, sourceDailyClose}},
			"change":        map[string]any{"type": "number"},
			"changePercent": map[string]any{"type": "number"},
			"note":          map[string]any{"type": "string"},
		},
		"required": []string{"symbol", "price", "asOf", "source"},
	}

	historyOutputSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"symbol": map[string]any{"type": "string"},
			"count":  map[string]any{"type": "integer"},
			"bars": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"date":   map[string]any{"type": "string", "description": "YYYY-MM-DD"},
						"open":   map[string]any{"type": "number"},
						"high":   map[string]any{"type": "number"},
						"low":    map[string]any{"type": "number"},
						"close":  map[string]any{"type": "number"},
						"volume": map[string]any{"type": "number"},
					},
					"required": []string{"date", "open", "high", "low", "close", "volume"},
				},
			},
		},
		"required": []string{"symbol", "count", "bars"},
	}

	alertsOutputSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "integer"},
			"rules": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"symbol":    map[string]any{"type": "string"},
						"condition": map[string]any{"type": "string"},
						"value":     map[string]any{"type": "number"},
						"enabled":   map[string]any{"type": "boolean"},
					},
					"required": []string{"symbol", "condition", "value", "enabled"},
				},
			},
		},
		"required": []string{"count", "rules"},
	}

	portfolioOutputSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"cost":         map[string]any{"type": "number"},
			"value":        map[string]any{"type": "number"},
			"pnl":          map[string]any{"type": "number"},
			"pnlPercent":   map[string]any{"type": "number"},
			"fullyPriced":  map[string]any{"type": "boolean"},
			"coverage":     map[string]any{"type": "number", "description": "share of cost basis priced, 0 to 1"},
			"unpriced":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"unpricedCost": map[string]any{"type": "number"},
			"fetchErrors":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"note":         map[string]any{"type": "string"},
		},
		"required": []string{"name", "cost", "value", "pnl", "pnlPercent", "fullyPriced", "coverage"},
	}
)
