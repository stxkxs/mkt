package config

// Default values for the configuration.
var (
	DefaultWatchlist = []string{
		// Crypto
		"BTC-USD",
		"ETH-USD",
		"SOL-USD",
		"XRP-USD",
		"ADA-USD",
		"DOGE-USD",
		"AVAX-USD",
		"LINK-USD",
		"DOT-USD",
		"NEAR-USD",
		"SUI-USD",
		"ARB-USD",
		"OP-USD",
		"PEPE-USD",
		// Stocks
		"AAPL",
		"MSFT",
		"GOOGL",
		"AMZN",
		"NVDA",
		"TSLA",
		"META",
		"AMD",
		"NFLX",
		"COIN",
	}

	DefaultPollInterval = "15s"
	DefaultSparklineLen = 60
	DefaultTheme        = "dark"
)
