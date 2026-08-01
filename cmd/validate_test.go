package cmd

import (
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/portfolio"
)

func validCfg() *config.Config {
	return &config.Config{
		Watchlist:    []string{"BTC-USD", "AAPL"},
		PollInterval: "15s",
		SparklineLen: 60,
		Theme:        "tokyonight",
		Portfolios: []config.Portfolio{{
			Name:      "Core",
			Holdings:  []config.Holding{{Symbol: "AAPL", Quantity: 10, CostBasis: 150}},
			TaxMethod: "fifo",
			Transactions: []config.Transaction{
				{Type: "buy", Symbol: "AAPL", Quantity: 10, Price: 150, Time: "2025-01-02"},
			},
		}},
		Alerts: []config.AlertRule{
			{Symbol: "BTC-USD", Condition: "above", Value: 100000, Enabled: true},
			{Symbol: "ETH-USD", Match: "any", Conditions: []config.AlertSubCondition{
				{Condition: "rsi_above", Value: 70},
				{Condition: "pct_up", Value: 5},
			}},
		},
	}
}

// TestDefaultSeedDataPassesValidate loads a fresh config (all seeded
// defaults) and asserts it validates clean — so the seeded alerts,
// watchlist groups, portfolios, and EDGAR tickers can never ship a value
// that `mkt config validate` would flag.
func TestDefaultSeedDataPassesValidate(t *testing.T) {
	isolateHome(t, t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if issues := validateConfig(cfg); len(issues) > 0 {
		t.Errorf("default seeded config should validate clean, got:\n%s", strings.Join(issues, "\n"))
	}
	if len(cfg.Alerts) == 0 {
		t.Error("expected seeded alerts")
	}
	if len(cfg.Watchlists) == 0 {
		t.Error("expected seeded watchlist groups")
	}
	if len(cfg.EDGARTickers) == 0 {
		t.Error("expected seeded EDGAR tickers")
	}
	if len(cfg.Notes) == 0 {
		t.Error("expected seeded notes")
	}
}

func TestValidateConfigClean(t *testing.T) {
	if issues := validateConfig(validCfg()); len(issues) != 0 {
		t.Errorf("valid config reported issues: %v", issues)
	}
}

func TestValidateConfigCatchesProblems(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantSub string
	}{
		{"bad poll interval", func(c *config.Config) { c.PollInterval = "xyz" }, "poll_interval"},
		{"negative poll interval", func(c *config.Config) { c.PollInterval = "-5s" }, "must be positive"},
		{"zero sparkline", func(c *config.Config) { c.SparklineLen = 0 }, "sparkline_len"},
		{"unknown theme", func(c *config.Config) { c.Theme = "vaporwave" }, "theme"},
		{"unknown condition", func(c *config.Config) { c.Alerts[0].Condition = "abovee" }, "not a known condition"},
		{"unknown sub-condition", func(c *config.Config) { c.Alerts[1].Conditions[0].Condition = "rsi" }, "not a known condition"},
		{"bad match", func(c *config.Config) { c.Alerts[1].Match = "some" }, "match"},
		{"missing alert symbol", func(c *config.Config) { c.Alerts[0].Symbol = "" }, "symbol is required"},
		{"typo tax method", func(c *config.Config) { c.Portfolios[0].TaxMethod = "fifoo" }, "tax_method"},
		{"negative quantity", func(c *config.Config) { c.Portfolios[0].Holdings[0].Quantity = -1 }, "quantity"},
		{"bad tx type", func(c *config.Config) { c.Portfolios[0].Transactions[0].Type = "transfer" }, "must be one of buy, sell, dividend"},
		{"bad tx time", func(c *config.Config) { c.Portfolios[0].Transactions[0].Time = "01/02/2025" }, "not a recognized format"},
		{"empty watchlist group", func(c *config.Config) { c.Watchlists = []config.Watchlist{{Name: "Empty"}} }, "no symbols"},
		{"unroutable watchlist symbol", func(c *config.Config) { c.Watchlist = append(c.Watchlist, "APPLE INC") }, "routes to no provider"},
		{"unroutable holding", func(c *config.Config) { c.Portfolios[0].Holdings[0].Symbol = "BTC/USD" }, "routes to no provider"},
		{"unroutable alert symbol", func(c *config.Config) { c.Alerts[0].Symbol = "my stock" }, "routes to no provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validCfg()
			tt.mutate(cfg)
			issues := validateConfig(cfg)
			if len(issues) == 0 {
				t.Fatal("no issues reported")
			}
			found := false
			for _, issue := range issues {
				if strings.Contains(issue, tt.wantSub) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no issue mentioning %q in %v", tt.wantSub, issues)
			}
		})
	}
}

func TestValidThemeAcceptsDarkAlias(t *testing.T) {
	if !validTheme("dark") {
		t.Error("dark alias rejected")
	}
	if validTheme("light") {
		t.Error("unknown theme accepted")
	}
}

// TestValidateAcceptsDividendTransactions is the regression test for two
// first-party commands contradicting each other: `mkt portfolio import`
// writes "dividend" for both supported CSV formats, and validate rejected it.
func TestValidateAcceptsDividendTransactions(t *testing.T) {
	cfg := validCfg()
	cfg.Portfolios[0].Transactions = append(cfg.Portfolios[0].Transactions, config.Transaction{
		Type: string(portfolio.TxDividend), Symbol: "AAPL", Quantity: 10, Price: 0.24, Time: "2025-02-13",
	})
	for _, issue := range validateConfig(cfg) {
		if strings.Contains(issue, "dividend") || strings.Contains(issue, "transactions[1]") {
			t.Errorf("dividend transaction rejected: %s", issue)
		}
	}
}

func TestValidTxType(t *testing.T) {
	for _, ok := range []string{"buy", "sell", "dividend"} {
		if !validTxType(ok) {
			t.Errorf("validTxType(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "transfer", "BUY"} {
		if validTxType(bad) {
			t.Errorf("validTxType(%q) = true", bad)
		}
	}
}

func TestRoutable(t *testing.T) {
	for _, ok := range []string{"AAPL", "BTC-USD", "btc", "^GSPC", "GC=F", "EURUSD=X", "FRED:DGS10", "BRK.B"} {
		if !routable(ok) {
			t.Errorf("routable(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "APPLE INC", "BTC/USD", "a much longer string"} {
		if routable(bad) {
			t.Errorf("routable(%q) = true", bad)
		}
	}
}

// TestCanonicalNotesReadsTheFileNotTheConfig checks the report survives
// normalization: Load rewrites every symbol, so the note has to come off
// the bytes on disk.
func TestCanonicalNotesReadsTheFileNotTheConfig(t *testing.T) {
	path := seedConfig(t, `watchlist:
  - btc
  - aapl
  - MATIC
portfolios:
  - name: Core
    holdings:
      - symbol: eth
        quantity: 1
`)
	notes := canonicalNotes(path)
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"BTC-USD", "AAPL", "POL-USD", "ETH-USD"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no note mapping onto %s:\n%s", want, joined)
		}
	}
}

func TestCanonicalNotesSilentOnACanonicalFile(t *testing.T) {
	path := seedConfig(t, goodConfig)
	if notes := canonicalNotes(path); len(notes) != 0 {
		t.Errorf("canonical file produced notes: %v", notes)
	}
}

// TestConfigSymbolsIsTheDeduplicatedUnion guards what --check-symbols walks.
func TestConfigSymbolsIsTheDeduplicatedUnion(t *testing.T) {
	cfg := &config.Config{
		Watchlist:  []string{"AAPL", "AAPL"},
		Watchlists: []config.Watchlist{{Name: "g", Symbols: []string{"MSFT"}}},
		Portfolios: []config.Portfolio{{Name: "p", Holdings: []config.Holding{{Symbol: "VTI"}}}},
		Alerts:     []config.AlertRule{{Symbol: "BTC-USD"}},
	}
	got := configSymbols(cfg)
	want := []string{"AAPL", "BTC-USD", "MSFT", "VTI"}
	if len(got) != len(want) {
		t.Fatalf("configSymbols = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("configSymbols = %v, want %v", got, want)
		}
	}
}

// TestValidateCleanConfigExitsZero is the other side of the headline test:
// a healthy file must still say so.
func TestValidateCleanConfigExitsZero(t *testing.T) {
	seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, nil, "config", "validate")
	if err != nil {
		t.Fatalf("validate on a healthy config: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "Config OK") {
		t.Errorf("healthy config not reported OK:\n%s", out)
	}
}
