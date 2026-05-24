## Unreleased

### Added
- `internal/provider/recording`: NDJSON record/replay for quote streams. `Recording` decorator wraps any `QuoteProvider` and tees observed quotes to a shared `Sink`; `Replay` provider reads the file back, with `ModeBurst` and `ModeRealtime` pacing. Opt-in via `MKT_RECORD=<path>` env var on the dashboard.
- `portfolio.Transaction` and `portfolio.DeriveHoldings` / `Materialize`: optional transaction log on each portfolio in `config.yaml` (`transactions:` field) folds into derived holdings using weighted-average cost basis. Holdings-only configs continue to load unchanged.
- Mouse support across all tabs: wheel scrolls the cursor in portfolio / alerts / news / heatmap; click sets the cursor in portfolio / alerts / news; wheel zooms in/out on the full-screen chart and comparison chart.
- `indicator.VWAP` (anchored, typical-price weighted) and `indicator.OBV` (signed running volume), wired into the chart `i` menu as keys `6` and `7`. VWAP overlays the price axis; OBV renders in the sub-panel.
- `indicator.ATR` (Wilder-smoothed True Range) and `indicator.Stochastic` (%K and %D), wired into the chart `i` menu as keys `8` and `9`. Both render in the sub-panel; Stoch shows 20/80 reference lines.
- `indicator.ADX` (trend strength with +DI/-DI) and `indicator.PivotsClassic` (floor-trader pivot levels). Toggled via the chart `i` menu with letter keys `a` (ADX sub-panel, ref at 25) and `p` (pivot lines overlaid on the main chart).
- `indicator.VolumeProfile` and `indicator.POC`. Toggled via the chart `i` menu with key `v` — draws a horizontal volume histogram in a right-side gutter with the point-of-control row highlighted; the candle area narrows to make room.
- `indicator.Patterns` detects Doji, Hammer, Shooting Star, Bullish Engulfing, and Bearish Engulfing. Toggled via the chart `i` menu with key `k` — marker glyphs appear on the candlestick chart (▲ green for bullish, ▼ red for bearish, ◇ accent for doji); summary line shows the latest detected pattern.
- `alert.WebhookNotifier` posts triggered alerts as JSON to configured URLs. Config gains a top-level `webhook_url` (default destination) and an optional per-rule `webhooks: [...]` override. Payload is `{symbol, condition, value, price, message, timestamp}`.
- `alert.NtfyNotifier` and `alert.PushoverNotifier` send alerts to mobile. Config: `ntfy_topic` (optional `ntfy_server`, defaults to `https://ntfy.sh`), `pushover_user` + `pushover_token`.
- Compound alert rules: each alert may declare `conditions: [...]` and `match: all|any|sequence`. The engine tracks per-rule progress across quotes; `all` requires every sub-condition to fire, `any` fires on the first match, and `sequence` requires sub-conditions to fire in declared order. Legacy single-condition rules continue to work unchanged.
- Alert conditions `volume_above` (fires when a quote's volume exceeds the value) and `stddev_above` (fires when rolling stddev over `period` quotes exceeds `value` percent of the rolling mean). `indicator.Stddev` helper added.
- Persisted alert history: triggered alerts are appended to `~/.config/mkt/alert-history.ndjson` and reloaded into the Alerts tab on startup. Up to 500 most-recent entries are loaded. New `alert.HistoryFile` and `alert.HistoryNotifier`. `config.ConfigDir()` is now exported.
- `internal/provider/fred`: `HistoryProvider` for FRED economic series via the public fredgraph CSV endpoint (no API key). Symbol prefix `FRED:<series_id>` routes here (e.g. `FRED:DFF`, `FRED:T10Y2Y`). Registered in the dashboard's `MultiHistoryProvider` ahead of Coinbase/Yahoo.
- SEC EDGAR per-ticker filings in the news feed: `news.FetchEDGAR` fetches Atom feeds for configured `edgar_tickers` and merges them into the headline list. `Headline` gains a `Category` field. News tab adds an `f` key cycling between All / News / Filings filters.
- DeFiLlama TVL: new `internal/provider/defillama` package polls per-chain TVL from the public v2 chains endpoint (no API key). Macro tab gains a "DeFi TVL (top 8 chains)" section with 1d / 7d change percentages.
- Binance futures funding + open interest: new `internal/provider/binance` package polls premium-index and open-interest endpoints for BTC/ETH/SOL perps every 2 minutes. Macro tab gains a "Crypto Futures" section showing mark price, funding rate, and open interest.
- `internal/provider/calendar` package: `Event`, `EventType`, `EconomicEvents()` (curated 2026 schedule for FOMC × 8, CPI × 12, NFP × 12, GDP × 4), `Upcoming(events, now, window)` filter, and an `EarningsSource` interface for a future earnings adapter. Consumed by V4 when it lands.
- `portfolio.Realized(txs)` computes cumulative realized P&L from sell transactions using weighted-average cost (buy fees fold into cost basis; sell fees subtract from proceeds). `portfolio.Portfolio` gains a `Transactions` field. Portfolio tab shows a colored "Realized: $X.XX" line below unrealized totals when the active portfolio has any transactions.
- Tax-lot accounting: `portfolio.RealizedByMethod(txs, method)` with `TaxMethod` of `TaxFIFO`, `TaxLIFO`, `TaxHIFO`, or `TaxAverage` (empty default, matches existing weighted-average behavior). Per-portfolio `tax_method` YAML key; portfolio tab labels the realized line with the method name when non-default.
- Indicator test coverage: `RSI`, `SMA`, `EMA`, `MACD`, `Bollinger`.
- Hub fan-out test verifying provider reader is isolated from a slow quote consumer.
- GitHub Actions workflow running `go vet`, `go test -race`, and `golangci-lint`.
- `.golangci.yml` with `errcheck`, `govet`, `staticcheck`, `unused`, `gosimple`, `ineffassign`, `misspell`, `unconvert`, `gofmt`.
- Theme-aware heatmap gradient derived from each theme's red / dim / green palette.
- `theme.ChangedMsg` broadcast so each view rebuilds its cached styles in its own `Update`.
- Per-feed context timeout in the RSS fetcher.
- `CONTRIBUTING.md`.

### Changed
- `market.Hub` now dispatches `onQuote` on a dedicated goroutine behind a 256-slot buffer; quotes drop when the TUI stalls rather than blocking providers.
- Yahoo session init failures are now logged instead of silently discarded.
- Config directory is created with `0o700` permissions; holdings and alert rules were previously world-readable.
- `alert.Notify` replaced by a `Notifier` interface; `Engine.AddNotifier` registers destinations and `Engine.Check` dispatches each trigger after releasing the lock, with per-call timeouts and error isolation so one failing destination cannot block siblings.

### Fixed
- `alert.Engine.Check` now takes the write lock; it mutates `refPrices` and `cooldowns`, which `RLock` did not protect.
- Yahoo history requests for the `4h` interval now fall back to `1h` candles instead of silently returning daily data.
- Coinbase history requests for `4h` / `1w` now send supported granularities (`3600` / `86400`); previously they sent `14400` / `604800`, which the Coinbase candles API rejects.
- Modal overlays (symbol info, alert dialog) now composite over the live tab content via `lipgloss.Compositor` instead of replacing the screen with `lipgloss.Place` on a blank canvas.

### Removed
- Dormant `internal/provider/binance/` package (never imported).
