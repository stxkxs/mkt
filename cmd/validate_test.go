package cmd

import (
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/config"
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
		{"bad tx type", func(c *config.Config) { c.Portfolios[0].Transactions[0].Type = "transfer" }, "buy or sell"},
		{"bad tx time", func(c *config.Config) { c.Portfolios[0].Transactions[0].Time = "01/02/2025" }, "not a recognized format"},
		{"empty watchlist group", func(c *config.Config) { c.Watchlists = []config.Watchlist{{Name: "Empty"}} }, "no symbols"},
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
