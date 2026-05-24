package config

import (
	"fmt"
	"os"
	"path/filepath"
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

// Config is the application configuration.
type Config struct {
	Watchlist     []string    `mapstructure:"watchlist" yaml:"watchlist"`
	Portfolios    []Portfolio `mapstructure:"portfolios" yaml:"portfolios"`
	Alerts        []AlertRule `mapstructure:"alerts" yaml:"alerts"`
	PollInterval  string      `mapstructure:"poll_interval" yaml:"poll_interval"`
	SparklineLen  int         `mapstructure:"sparkline_len" yaml:"sparkline_len"`
	Theme         string      `mapstructure:"theme" yaml:"theme"`
	WebhookURL    string      `mapstructure:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`
	NtfyTopic     string      `mapstructure:"ntfy_topic,omitempty" yaml:"ntfy_topic,omitempty"`
	NtfyServer    string      `mapstructure:"ntfy_server,omitempty" yaml:"ntfy_server,omitempty"`
	PushoverUser  string      `mapstructure:"pushover_user,omitempty" yaml:"pushover_user,omitempty"`
	PushoverToken string      `mapstructure:"pushover_token,omitempty" yaml:"pushover_token,omitempty"`
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

	// Defaults
	v.SetDefault("watchlist", DefaultWatchlist)
	v.SetDefault("poll_interval", DefaultPollInterval)
	v.SetDefault("sparkline_len", DefaultSparklineLen)
	v.SetDefault("theme", DefaultTheme)
	v.SetDefault("portfolios", DefaultPortfolios)
	v.SetDefault("alerts", []AlertRule{})

	if err := v.ReadInConfig(); err != nil {
		// Write defaults if file doesn't exist. A concurrent create is fine; defaults still apply in-memory.
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			_ = v.SafeWriteConfig()
		}
		// Not fatal — use defaults
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	v := viper.New()
	v.SetConfigFile(configPath())
	v.SetConfigType("yaml")

	v.Set("watchlist", cfg.Watchlist)
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
