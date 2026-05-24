## Unreleased

### Added
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
