package cmd

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/mcp"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
)

func testTools(t *testing.T, hist historyFetcher, portfolios []portfolio.Portfolio) map[string]mcp.Tool {
	t.Helper()
	cfg := &config.Config{Alerts: []config.AlertRule{{Symbol: "AAPL", Condition: "above", Value: 200, Enabled: true}}}
	byName := map[string]mcp.Tool{}
	for _, tool := range mcpTools(cfg, nil, hist, portfolios) {
		byName[tool.Name] = tool
	}
	return byName
}

// jsonFields lists the JSON member names a struct type encodes.
func jsonFields(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	var names []string
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("%s.%s has no json name", typ.Name(), typ.Field(i).Name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func schemaFields(t *testing.T, schema map[string]any) []string {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %+v", schema)
	}
	var names []string
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func itemsSchema(t *testing.T, schema map[string]any, field string) map[string]any {
	t.Helper()
	prop := schema["properties"].(map[string]any)[field].(map[string]any)
	return prop["items"].(map[string]any)
}

// structuredContent is validated by clients against outputSchema, so the
// schema and the encoded result type must name the same members, at every
// nesting level.
func TestMCPToolOutputSchemasMatchResults(t *testing.T) {
	cases := []struct {
		name   string
		schema map[string]any
		typ    reflect.Type
	}{
		{"quote", quoteOutputSchema, reflect.TypeFor[quoteResult]()},
		{"history", historyOutputSchema, reflect.TypeFor[historyResult]()},
		{"history.bars", itemsSchema(t, historyOutputSchema, "bars"), reflect.TypeFor[ohlcvBar]()},
		{"alerts", alertsOutputSchema, reflect.TypeFor[alertsResult]()},
		{"alerts.rules", itemsSchema(t, alertsOutputSchema, "rules"), reflect.TypeFor[alertRuleResult]()},
		{"portfolio", portfolioOutputSchema, reflect.TypeFor[portfolioResult]()},
	}
	for _, c := range cases {
		got, want := schemaFields(t, c.schema), jsonFields(t, c.typ)
		if !slices.Equal(got, want) {
			t.Errorf("%s: schema properties %v, result fields %v", c.name, got, want)
		}
		// A required member must be one the type always encodes.
		for _, req := range c.schema["required"].([]string) {
			for i := range c.typ.NumField() {
				f := c.typ.Field(i)
				name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
				if name == req && strings.Contains(opts, "omitempty") {
					t.Errorf("%s: %q is required but omitted when empty", c.name, req)
				}
			}
		}
	}
}

// Every tool carries what a client reads before choosing it: a title, the
// read-only hints, and the shape of what it returns.
func TestMCPToolsDeclareTheirContract(t *testing.T) {
	tools := mcpTools(&config.Config{}, nil, &stubHistory{}, nil)
	if len(tools) != 4 {
		t.Fatalf("got %d tools, want 4", len(tools))
	}
	for _, tool := range tools {
		if tool.Title == "" {
			t.Errorf("%s has no title", tool.Name)
		}
		if len(tool.OutputSchema) == 0 {
			t.Errorf("%s has no output schema", tool.Name)
		}
		if tool.Annotations["readOnlyHint"] != true || tool.Annotations["destructiveHint"] != false {
			t.Errorf("%s annotations = %+v, want read-only", tool.Name, tool.Annotations)
		}
	}
}

func TestQueryHistoryLimitIsValidatedNotClamped(t *testing.T) {
	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	hist := &stubHistory{bars: map[string][]provider.OHLCV{"AAPL": {dailyBar(day, 1)}}}
	tool := testTools(t, hist, nil)["query_history"]

	for _, bad := range []any{0.0, -1.0, float64(maxHistoryLimit + 1), 2.5, "10"} {
		_, err := tool.Handler(context.Background(), map[string]any{"symbol": "AAPL", "limit": bad})
		if err == nil || !strings.Contains(err.Error(), "limit must") {
			t.Errorf("limit %v: err = %v, want a range error", bad, err)
		}
	}
	for _, good := range []any{nil, 1.0, float64(maxHistoryLimit)} {
		args := map[string]any{"symbol": "AAPL"}
		if good != nil {
			args["limit"] = good
		}
		if _, err := tool.Handler(context.Background(), args); err != nil {
			t.Errorf("limit %v: %v", good, err)
		}
	}
	if hist.callCount() != 3 {
		t.Errorf("provider calls = %d, want 3 (a refused limit must not fetch)", hist.callCount())
	}
}

func TestSymbolErrorsNameTheAcceptedForms(t *testing.T) {
	tool := testTools(t, &stubHistory{}, nil)["get_quote"]
	for _, args := range []map[string]any{{}, {"symbol": ""}, {"symbol": "  "}, {"symbol": 7.0}} {
		_, err := tool.Handler(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "BTC-USD") {
			t.Errorf("args %v: err = %v, want the accepted symbol forms", args, err)
		}
	}
}

func TestUnknownPortfolioListsTheConfiguredNames(t *testing.T) {
	pfs := []portfolio.Portfolio{{Name: "Tech"}, {Name: "Income"}}
	tool := testTools(t, &stubHistory{}, pfs)["get_portfolio"]
	_, err := tool.Handler(context.Background(), map[string]any{"name": "tech"})
	if err == nil || !strings.Contains(err.Error(), `"Tech"`) || !strings.Contains(err.Error(), `"Income"`) {
		t.Fatalf("err = %v, want both configured names", err)
	}

	tool = testTools(t, &stubHistory{}, nil)["get_portfolio"]
	_, err = tool.Handler(context.Background(), map[string]any{"name": "Tech"})
	if err == nil || !strings.Contains(err.Error(), "no portfolios are configured") {
		t.Fatalf("err = %v, want the empty-config explanation", err)
	}
}
