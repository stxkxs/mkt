package config

import (
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// The exact strings the prompt shows. They are the whole reason a user can
// tell an intentional cleanup apart from the data loss this package exists
// to prevent, so they are pinned.
func TestRemovals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		old  *Config
		next *Config
		want []string
	}{
		{
			name: "nothing lost",
			old:  &Config{Watchlist: []string{"AAPL"}},
			next: &Config{Watchlist: []string{"AAPL", "TSLA"}},
			want: nil,
		},
		{
			name: "portfolio dropped",
			old: &Config{Portfolios: []Portfolio{{
				Name:     "Retirement",
				Holdings: []Holding{{Symbol: "VTI", Quantity: 500, CostBasis: 210}},
			}}},
			next: &Config{},
			want: []string{`portfolio "Retirement" (1 holding, 500 VTI)`},
		},
		{
			name: "portfolio dropped, several holdings",
			old: &Config{Portfolios: []Portfolio{{
				Name: "Core",
				Holdings: []Holding{
					{Symbol: "VTI", Quantity: 500},
					{Symbol: "AAPL", Quantity: 10},
					{Symbol: "BTC-USD", Quantity: 0.25},
				},
			}}},
			next: &Config{},
			want: []string{`portfolio "Core" (3 holdings, 500 VTI + 2 more)`},
		},
		{
			name: "empty portfolio dropped",
			old:  &Config{Portfolios: []Portfolio{{Name: "Empty"}}},
			next: &Config{},
			want: []string{`portfolio "Empty" (no holdings)`},
		},
		{
			name: "holding dropped from a surviving portfolio",
			old: &Config{Portfolios: []Portfolio{{
				Name:     "Core",
				Holdings: []Holding{{Symbol: "VTI", Quantity: 500}, {Symbol: "AAPL", Quantity: 10}},
			}}},
			next: &Config{Portfolios: []Portfolio{{
				Name:     "Core",
				Holdings: []Holding{{Symbol: "AAPL", Quantity: 10}},
			}}},
			want: []string{`holding 500 VTI from portfolio "Core"`},
		},
		{
			name: "transactions dropped from a surviving portfolio",
			old: &Config{Portfolios: []Portfolio{{
				Name:         "Core",
				Transactions: []Transaction{{Type: "buy", Symbol: "VTI"}, {Type: "sell", Symbol: "VTI"}},
			}}},
			next: &Config{Portfolios: []Portfolio{{Name: "Core"}}},
			want: []string{`2 transactions from portfolio "Core"`},
		},
		{
			name: "alert rule dropped",
			old:  &Config{Alerts: []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400, Enabled: true}}},
			next: &Config{},
			want: []string{"alert rule (AAPL above 400)"},
		},
		{
			name: "period-only and bare alert rules read sensibly",
			old: &Config{Alerts: []AlertRule{
				{Symbol: "AMD", Condition: "sma_cross_above", Period: 50, Enabled: true},
				{Symbol: "NVDA", Condition: "macd_cross", Enabled: true},
			}},
			next: &Config{},
			want: []string{"alert rule (AMD sma_cross_above 50)", "alert rule (NVDA macd_cross)"},
		},
		{
			name: "compound alert rule dropped",
			old: &Config{Alerts: []AlertRule{{Symbol: "NVDA", Match: "all", Enabled: true, Conditions: []AlertSubCondition{
				{Condition: "rsi_below", Value: 40, Period: 14},
				{Condition: "above", Value: 150},
			}}}},
			next: &Config{},
			want: []string{"alert rule (NVDA all of 2 conditions)"},
		},
		{
			name: "compound rule with no explicit match reads as all",
			old: &Config{Alerts: []AlertRule{{Symbol: "NVDA", Conditions: []AlertSubCondition{
				{Condition: "rsi_below", Value: 40, Period: 14},
			}}}},
			next: &Config{},
			want: []string{"alert rule (NVDA all of 1 condition)"},
		},
		{
			name: "duplicate rules are counted, not deduped",
			old: &Config{Alerts: []AlertRule{
				{Symbol: "AAPL", Condition: "above", Value: 400},
				{Symbol: "AAPL", Condition: "above", Value: 400},
			}},
			next: &Config{Alerts: []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400}}},
			want: []string{"alert rule (AAPL above 400)"},
		},
		{
			name: "toggling a rule off is not a removal",
			old:  &Config{Alerts: []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400, Enabled: true}}},
			next: &Config{Alerts: []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400, Enabled: false}}},
			want: []string{"alert rule (AAPL above 400)"},
		},
		{
			name: "watchlist symbols dropped",
			old:  &Config{Watchlist: []string{"AAPL", "VTI", "TSLA"}},
			next: &Config{Watchlist: []string{"AAPL"}},
			want: []string{"2 watchlist symbols (VTI, TSLA)"},
		},
		{
			name: "one watchlist symbol dropped reads singular",
			old:  &Config{Watchlist: []string{"AAPL", "VTI"}},
			next: &Config{Watchlist: []string{"AAPL"}},
			want: []string{"1 watchlist symbol (VTI)"},
		},
		{
			name: "a long list is sampled",
			old:  &Config{Watchlist: []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}},
			next: &Config{},
			want: []string{"10 watchlist symbols (A, B, C, D, E, F, G, H, +2 more)"},
		},
		{
			name: "watchlist group dropped",
			old:  &Config{Watchlists: []Watchlist{{Name: "Tech", Symbols: []string{"AAPL", "MSFT"}}}},
			next: &Config{},
			want: []string{`watchlist group "Tech" (2 symbols)`},
		},
		{
			name: "symbols dropped from a surviving group",
			old:  &Config{Watchlists: []Watchlist{{Name: "Tech", Symbols: []string{"AAPL", "MSFT"}}}},
			next: &Config{Watchlists: []Watchlist{{Name: "Tech", Symbols: []string{"AAPL"}}}},
			want: []string{`1 symbol from watchlist group "Tech" (MSFT)`},
		},
		{
			name: "note dropped",
			old:  &Config{Notes: map[string]string{"AAPL": "flagship"}},
			next: &Config{},
			want: []string{"note for AAPL (8 chars)"},
		},
		{
			name: "note replaced",
			old:  &Config{Notes: map[string]string{"AAPL": "flagship"}},
			next: &Config{Notes: map[string]string{"AAPL": "something else"}},
			want: []string{"the previous note for AAPL (8 chars, replaced)"},
		},
		{
			name: "unchanged note is not a removal",
			old:  &Config{Notes: map[string]string{"AAPL": "flagship"}},
			next: &Config{Notes: map[string]string{"AAPL": "flagship"}},
			want: nil,
		},
		{
			name: "edgar tickers dropped",
			old:  &Config{EDGARTickers: []string{"AAPL", "NVDA"}},
			next: &Config{EDGARTickers: []string{"AAPL"}},
			want: []string{"1 edgar ticker (NVDA)"},
		},
		{
			name: "serve settings cleared",
			old: &Config{Serve: ServeConfig{
				Addr:               "0.0.0.0:2222",
				HostKey:            "/keys/host",
				AuthorizedKeysFile: "/keys/authorized",
				AuthorizedKeys:     []string{"ssh-ed25519 A a@b", "ssh-ed25519 B b@c"},
			}},
			next: &Config{},
			want: []string{
				"serve address 0.0.0.0:2222",
				"serve host key path /keys/host",
				"serve authorized_keys file /keys/authorized",
				"2 serve authorized keys",
			},
		},
		{
			name: "news feed dropped",
			old:  &Config{NewsFeeds: []NewsFeed{{Name: "Reuters", URL: "https://example.invalid/rss"}}},
			next: &Config{},
			want: []string{`news feed "Reuters" (https://example.invalid/rss)`},
		},
		{
			name: "secrets cleared",
			old: &Config{
				WebhookURL:    "https://example.invalid/hook",
				NtfyTopic:     "mkt",
				NtfyServer:    "https://ntfy.invalid",
				PushoverUser:  "u",
				PushoverToken: "t",
			},
			next: &Config{},
			want: []string{
				"cleared webhook_url",
				"cleared ntfy_topic",
				"cleared ntfy_server",
				"cleared pushover_user",
				"cleared pushover_token",
			},
		},
		{
			name: "toggles cleared",
			old: &Config{
				DesktopNotify: ptr(false),
				Notifications: ptr(true),
				Providers:     Providers{Binance: ptr(false), News: ptr(false)},
			},
			next: &Config{},
			want: []string{
				"cleared desktop_notify (was false)",
				"cleared notifications (was true)",
				"cleared providers.binance (was false)",
				"cleared providers.news (was false)",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := removals(tc.old, tc.next, nil, nil)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("removals()\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// A hand-edited config can carry sections mkt does not model. Rebuilding the
// file from the Config struct drops them, so say so.
func TestSectionRemovals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		old, next      map[string]any
		want           []string
		wantNoMentions []string
	}{
		{
			name: "unknown section with content",
			old:  map[string]any{"watchlist": []any{"AAPL"}, "my_experiment": map[string]any{"a": 1}},
			next: map[string]any{"watchlist": []any{"AAPL"}},
			want: []string{`unrecognized section "my_experiment" (mkt does not read it and cannot preserve it)`},
		},
		{
			name: "empty unknown section is not worth a warning",
			old:  map[string]any{"my_experiment": nil, "other": "", "third": []any{}},
			next: map[string]any{},
			want: nil,
		},
		{
			name: "sections mkt models are reported by the typed diff, not here",
			old:  map[string]any{"portfolios": []any{map[string]any{"name": "Retirement"}}, "notes": map[string]any{"AAPL": "x"}},
			next: map[string]any{},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sectionRemovals(tc.old, tc.next)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sectionRemovals()\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// knownSections is derived from the struct tags; if that derivation ever
// breaks, every section starts looking unrecognized.
func TestKnownSectionsCoversConfig(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"watchlist", "watchlists", "portfolios", "alerts", "poll_interval", "sparkline_len", "theme", "webhook_url", "notes", "serve", "providers", "news_feeds", "edgar_tickers"} {
		if !knownSections[want] {
			t.Errorf("knownSections is missing %q", want)
		}
	}
	if knownSections["definitely_not_a_field"] {
		t.Error("knownSections claims a field that does not exist")
	}
}

// The removal list is what the user reads before answering a prompt; a
// non-deterministic order would make it impossible to review.
func TestRemovalsAreDeterministic(t *testing.T) {
	t.Parallel()
	old := &Config{
		Notes:        map[string]string{"AAPL": "a", "MSFT": "b", "TSLA": "c", "NVDA": "d"},
		EDGARTickers: []string{"AAPL", "NVDA"},
	}
	first := removals(old, &Config{}, nil, nil)
	for range 20 {
		if got := removals(old, &Config{}, nil, nil); !reflect.DeepEqual(got, first) {
			t.Fatalf("removals order is unstable:\n%v\nvs\n%v", got, first)
		}
	}
	if !strings.HasPrefix(first[0], "note for AAPL") {
		t.Errorf("notes should be sorted, got %v", first)
	}
}

func TestNum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   float64
		want string
	}{
		{500, "500"},
		{0.25, "0.25"},
		{1e9, "1000000000"},
		{0, "0"},
		{-3.5, "-3.5"},
	}
	for _, tc := range tests {
		if got := num(tc.in); got != tc.want {
			t.Errorf("num(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlural(t *testing.T) {
	t.Parallel()
	if got := plural(1, "holding"); got != "holding" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(0, "holding"); got != "holdings" {
		t.Errorf("plural(0) = %q", got)
	}
	if got := plural(2, "holding"); got != "holdings" {
		t.Errorf("plural(2) = %q", got)
	}
}

func TestIsEmptyValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"empty list", []any{}, true},
		{"empty map", map[string]any{}, true},
		{"string", "x", false},
		{"list", []any{1}, false},
		{"map", map[string]any{"a": 1}, false},
		{"int", 0, false},
		{"bool", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmptyValue(tc.in); got != tc.want {
				t.Errorf("isEmptyValue(%#v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// A section we do not model but do still write out is not lost.
func TestSectionRemovalsSkipsKeysWeStillWrite(t *testing.T) {
	t.Parallel()
	old := map[string]any{"experiment": map[string]any{"a": 1}}
	next := map[string]any{"experiment": map[string]any{"a": 2}}
	if got := sectionRemovals(old, next); got != nil {
		t.Errorf("sectionRemovals() = %v, want nil", got)
	}
}

// Two rules that differ only in where they notify are two different rules.
func TestRuleKeyDistinguishesWebhooks(t *testing.T) {
	t.Parallel()
	old := &Config{Alerts: []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400, Webhooks: []string{"https://a.invalid"}}}}
	next := &Config{Alerts: []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400}}}
	got := removals(old, next, nil, nil)
	if len(got) != 1 || got[0] != "alert rule (AAPL above 400)" {
		t.Errorf("removals() = %v, want the webhook-carrying rule reported as lost", got)
	}
}

// A portfolio that is nothing but a transaction log is still worth naming.
func TestDescribeHoldingsTransactionsOnly(t *testing.T) {
	t.Parallel()
	old := &Config{Portfolios: []Portfolio{{
		Name:         "Log",
		Transactions: []Transaction{{Type: "buy", Symbol: "VTI"}},
	}}}
	got := removals(old, &Config{}, nil, nil)
	want := []string{`portfolio "Log" (no holdings, 1 transaction)`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("removals() = %v, want %v", got, want)
	}
}
