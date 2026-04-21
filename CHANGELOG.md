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

### Removed
- Dormant `internal/provider/binance/` package (never imported).
