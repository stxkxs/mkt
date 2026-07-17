package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Holding represents a portfolio position from config.
type Holding struct {
	Symbol    string  `mapstructure:"symbol" yaml:"symbol"`
	Name      string  `mapstructure:"name" yaml:"name"`
	Quantity  float64 `mapstructure:"quantity" yaml:"quantity"`
	CostBasis float64 `mapstructure:"cost_basis" yaml:"cost_basis"`
}

// Transaction is a buy or sell event for an optional transaction log.
// Time is parsed lazily by callers (see ParseTime); zero means "unset".
type Transaction struct {
	Type     string  `mapstructure:"type" yaml:"type"` // "buy" or "sell"
	Symbol   string  `mapstructure:"symbol" yaml:"symbol"`
	Quantity float64 `mapstructure:"quantity" yaml:"quantity"`
	Price    float64 `mapstructure:"price" yaml:"price"`
	Time     string  `mapstructure:"time,omitempty" yaml:"time,omitempty"`
	Fee      float64 `mapstructure:"fee,omitempty" yaml:"fee,omitempty"`
	Note     string  `mapstructure:"note,omitempty" yaml:"note,omitempty"`
}

// Portfolio is a named collection of holdings and optional transactions.
type Portfolio struct {
	Name         string        `mapstructure:"name" yaml:"name"`
	Holdings     []Holding     `mapstructure:"holdings" yaml:"holdings,omitempty"`
	Transactions []Transaction `mapstructure:"transactions,omitempty" yaml:"transactions,omitempty"`
	TaxMethod    string        `mapstructure:"tax_method,omitempty" yaml:"tax_method,omitempty"` // fifo | lifo | hifo (empty = weighted average)
}

// ParseTime accepts a few common YAML date layouts. Returns the zero
// time on empty input or any parse failure; callers should treat the
// zero time as "unset" rather than as an error.
func ParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// AlertSubCondition is one leaf inside a compound alert rule.
type AlertSubCondition struct {
	Condition string  `mapstructure:"condition" yaml:"condition"`
	Value     float64 `mapstructure:"value" yaml:"value"`
	Period    int     `mapstructure:"period,omitempty" yaml:"period,omitempty"`
}

// AlertRule represents a saved alert from config. When Conditions is
// non-empty the legacy Condition / Value / Period fields are ignored
// and the rule is evaluated as a compound according to Match.
type AlertRule struct {
	Symbol     string              `mapstructure:"symbol" yaml:"symbol"`
	Condition  string              `mapstructure:"condition,omitempty" yaml:"condition,omitempty"`
	Value      float64             `mapstructure:"value,omitempty" yaml:"value,omitempty"`
	Period     int                 `mapstructure:"period,omitempty" yaml:"period,omitempty"`
	Enabled    bool                `mapstructure:"enabled" yaml:"enabled"`
	Webhooks   []string            `mapstructure:"webhooks,omitempty" yaml:"webhooks,omitempty"`
	Conditions []AlertSubCondition `mapstructure:"conditions,omitempty" yaml:"conditions,omitempty"`
	Match      string              `mapstructure:"match,omitempty" yaml:"match,omitempty"` // all | any | sequence
}

// Watchlist is a named group of symbols. Multiple groups can be defined
// under `watchlists:` and switched at runtime with `[` / `]` on the
// watchlist tab.
type Watchlist struct {
	Name    string   `mapstructure:"name" yaml:"name"`
	Symbols []string `mapstructure:"symbols" yaml:"symbols"`
}

// ServeConfig configures `mkt serve`, the Wish SSH dashboard. Access is
// gated by a public-key allowlist: connections are refused unless the
// client key appears in AuthorizedKeys or AuthorizedKeysFile. The command
// refuses to start with an empty allowlist rather than serve openly.
type ServeConfig struct {
	Addr               string   `mapstructure:"addr,omitempty" yaml:"addr,omitempty"`                                 // SSH bind, e.g. 127.0.0.1:2222 or 0.0.0.0:2222
	HostKey            string   `mapstructure:"host_key,omitempty" yaml:"host_key,omitempty"`                         // path to the SSH host key (auto-generated if missing)
	AuthorizedKeys     []string `mapstructure:"authorized_keys,omitempty" yaml:"authorized_keys,omitempty"`           // inline allowlist of public keys
	AuthorizedKeysFile string   `mapstructure:"authorized_keys_file,omitempty" yaml:"authorized_keys_file,omitempty"` // optional path to an authorized_keys file
}

// Config is the application configuration.
type Config struct {
	Watchlist     []string          `mapstructure:"watchlist" yaml:"watchlist"`
	Watchlists    []Watchlist       `mapstructure:"watchlists,omitempty" yaml:"watchlists,omitempty"`
	Portfolios    []Portfolio       `mapstructure:"portfolios" yaml:"portfolios"`
	Alerts        []AlertRule       `mapstructure:"alerts" yaml:"alerts"`
	PollInterval  string            `mapstructure:"poll_interval" yaml:"poll_interval"`
	SparklineLen  int               `mapstructure:"sparkline_len" yaml:"sparkline_len"`
	Theme         string            `mapstructure:"theme" yaml:"theme"`
	WebhookURL    string            `mapstructure:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`
	NtfyTopic     string            `mapstructure:"ntfy_topic,omitempty" yaml:"ntfy_topic,omitempty"`
	NtfyServer    string            `mapstructure:"ntfy_server,omitempty" yaml:"ntfy_server,omitempty"`
	PushoverUser  string            `mapstructure:"pushover_user,omitempty" yaml:"pushover_user,omitempty"`
	PushoverToken string            `mapstructure:"pushover_token,omitempty" yaml:"pushover_token,omitempty"`
	EDGARTickers  []string          `mapstructure:"edgar_tickers,omitempty" yaml:"edgar_tickers,omitempty"`
	Notes         map[string]string `mapstructure:"notes,omitempty" yaml:"notes,omitempty"` // per-symbol freeform notes (markdown plaintext)
	Serve         ServeConfig       `mapstructure:"serve,omitempty" yaml:"serve,omitempty"`
}

// ConfigDir returns the application's config / data directory path.
func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mkt")
}

// configPath returns the config file path.
func configPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// Load reads the config file, creating defaults if it doesn't exist.
func Load() (*Config, error) {
	dir := ConfigDir()
	// 0o700: holdings and alert rules are user-private; don't expose to other local users.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath())
	v.SetConfigType("yaml")

	// Operational fallbacks — always active so any run has sane values even
	// when a hand-edited config omits them. These mirror the long-standing
	// pre-seed defaults and are safe to inject into an existing config.
	v.SetDefault("watchlist", DefaultWatchlist)
	v.SetDefault("poll_interval", DefaultPollInterval)
	v.SetDefault("sparkline_len", DefaultSparklineLen)
	v.SetDefault("theme", DefaultTheme)
	v.SetDefault("portfolios", DefaultPortfolios)

	if err := v.ReadInConfig(); err != nil {
		// With SetConfigFile, a missing file surfaces as os.ErrNotExist (a
		// *fs.PathError), NOT viper's ConfigFileNotFoundError — handle both.
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist) {
			// Fresh install ONLY: seed the rich content (grouped watchlists,
			// example alerts, EDGAR tickers, notes) and persist a full
			// config.yaml the user can see and edit. We deliberately do NOT
			// inject these sections into a config that already exists — an
			// upgrader keeps their file exactly as-is.
			v.SetDefault("watchlists", DefaultWatchlists)
			v.SetDefault("alerts", DefaultAlerts)
			v.SetDefault("edgar_tickers", DefaultEDGARTickers)
			v.SetDefault("notes", DefaultNotes)
			if werr := v.WriteConfig(); werr != nil {
				fmt.Fprintf(os.Stderr, "config: could not write default config to %s: %v\n", configPath(), werr)
			}
		}
		// Any other read error is non-fatal — fall through with defaults.
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// viper lowercases all map keys; notes are keyed by symbol, which is
	// uppercase everywhere else, so normalize back so lookups match.
	if len(cfg.Notes) > 0 {
		notes := make(map[string]string, len(cfg.Notes))
		for k, v := range cfg.Notes {
			notes[strings.ToUpper(k)] = v
		}
		cfg.Notes = notes
	}

	return &cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	v := viper.New()
	v.SetConfigFile(configPath())
	v.SetConfigType("yaml")

	v.Set("watchlist", cfg.Watchlist)
	if len(cfg.Watchlists) > 0 {
		v.Set("watchlists", cfg.Watchlists)
	}
	v.Set("portfolios", cfg.Portfolios)
	v.Set("alerts", cfg.Alerts)
	v.Set("poll_interval", cfg.PollInterval)
	v.Set("sparkline_len", cfg.SparklineLen)
	v.Set("theme", cfg.Theme)
	if cfg.WebhookURL != "" {
		v.Set("webhook_url", cfg.WebhookURL)
	}
	if cfg.NtfyTopic != "" {
		v.Set("ntfy_topic", cfg.NtfyTopic)
	}
	if cfg.NtfyServer != "" {
		v.Set("ntfy_server", cfg.NtfyServer)
	}
	if cfg.PushoverUser != "" {
		v.Set("pushover_user", cfg.PushoverUser)
	}
	if cfg.PushoverToken != "" {
		v.Set("pushover_token", cfg.PushoverToken)
	}
	if len(cfg.EDGARTickers) > 0 {
		v.Set("edgar_tickers", cfg.EDGARTickers)
	}
	if len(cfg.Notes) > 0 {
		v.Set("notes", cfg.Notes)
	}
	// Persist the SSH serve block so a `mkt config set` doesn't drop a
	// hand-edited serve config (Save rebuilds the file from scratch).
	if cfg.Serve.Addr != "" || cfg.Serve.HostKey != "" || len(cfg.Serve.AuthorizedKeys) > 0 || cfg.Serve.AuthorizedKeysFile != "" {
		v.Set("serve", cfg.Serve)
	}

	return v.WriteConfig()
}

// PollDuration parses the poll interval as a duration.
func (c *Config) PollDuration() time.Duration {
	d, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		d, _ = time.ParseDuration(DefaultPollInterval)
	}
	return d
}

// AddSymbol adds a symbol to the watchlist if not already present.
func (c *Config) AddSymbol(symbol string) bool {
	for _, s := range c.Watchlist {
		if s == symbol {
			return false
		}
	}
	c.Watchlist = append(c.Watchlist, symbol)
	return true
}

// RemoveSymbol removes a symbol from the watchlist.
func (c *Config) RemoveSymbol(symbol string) bool {
	for i, s := range c.Watchlist {
		if s == symbol {
			c.Watchlist = append(c.Watchlist[:i], c.Watchlist[i+1:]...)
			return true
		}
	}
	return false
}
