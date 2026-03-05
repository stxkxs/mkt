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

// Portfolio is a named collection of holdings.
type Portfolio struct {
	Name     string    `mapstructure:"name" yaml:"name"`
	Holdings []Holding `mapstructure:"holdings" yaml:"holdings"`
}

// AlertRule represents a saved alert from config.
type AlertRule struct {
	Symbol    string  `mapstructure:"symbol" yaml:"symbol"`
	Condition string  `mapstructure:"condition" yaml:"condition"` // above, below, pct_up, pct_down
	Value     float64 `mapstructure:"value" yaml:"value"`
	Enabled   bool    `mapstructure:"enabled" yaml:"enabled"`
}

// Config is the application configuration.
type Config struct {
	Watchlist    []string    `mapstructure:"watchlist" yaml:"watchlist"`
	Portfolios   []Portfolio `mapstructure:"portfolios" yaml:"portfolios"`
	Alerts       []AlertRule `mapstructure:"alerts" yaml:"alerts"`
	PollInterval string      `mapstructure:"poll_interval" yaml:"poll_interval"`
	SparklineLen int         `mapstructure:"sparkline_len" yaml:"sparkline_len"`
	Theme        string      `mapstructure:"theme" yaml:"theme"`
}

// configDir returns the config directory path.
func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mkt")
}

// configPath returns the config file path.
func configPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

// Load reads the config file, creating defaults if it doesn't exist.
func Load() (*Config, error) {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
		// Write defaults if file doesn't exist
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			if err := v.SafeWriteConfig(); err != nil {
				// Ignore if file already exists
			}
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
