# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`mkt` — real-time terminal market dashboard for stocks and crypto. Single Go binary, no API keys required. Crypto streams via Coinbase WebSocket, stocks poll from Yahoo Finance. Built with Bubbletea v2 + Lipgloss v2.

## Build Commands

Uses [Task](https://taskfile.dev) runner (Taskfile.yml):

```sh
task build             # go build with version ldflags → ./mkt
task run               # build + run
task test              # go test ./...
task lint              # golangci-lint run ./...
task tidy              # go mod tidy
task demo              # build, then render demo/mkt.gif via vhs
task release-snapshot  # local goreleaser build of all 6 targets, no publish
task clean             # rm binary + dist/
```

Build injects version/commit/date via ldflags into `cmd.version`, `cmd.commit`, `cmd.date`.

## Architecture

Message-driven TUI over a program-agnostic data plane:

```
Providers (Coinbase WS, Yahoo HTTP) → chan Quote → Hub ─┬─► cache.Push()
                                                        ├─► observers (reliable) → alertEngine.Check()
                                                        └─► dispatch (bounded, drops) → broadcast.Send()
                                                                                            ↓
                                                                  Bubbletea Update() → route to tab views
```

**Key layers:**

- **`cmd/`** — Cobra CLI. `backend.go` is the single wiring point (`setupBackend` → `buildApp` → `startDataPlane`); `dashboard.go`, `serve.go` and `daemon.go` are thin shells over it, so all three surfaces share one hub, cache, alert engine and poller set. `dataplane_seed.go` backfills history into the cache at startup; `dataplane_wiring.go` hands degraded-config / unroutable-symbol state to the TUI through optional interfaces.
- **`internal/symbol/`** — the one place symbols are canonicalized and routed. `Canonical` normalizes (`btc`/`BTCUSDT`/`btc-usd` → `BTC-USD`, `aapl` → `AAPL`, `MATIC` → `POL`) and is idempotent. Never re-implement this loop; call it.
- **`internal/provider/`** — `QuoteProvider` and `HistoryProvider` interfaces (`provider.go`). Implementations: `coinbase/` (WebSocket streaming + REST candles), `yahoo/` (HTTP polling + chart history), plus `fred/`, `defillama/`, `binance/`, `calendar/`, `recording/`.
- **`internal/market/`** — `hub.go` multiplexes providers. It has **two** fan-out paths: `AddObserver` is reliable and never drops (alert evaluation lives here), while the TUI dispatch is bounded and sheds under back-pressure (`Drops()`, exported as `mkt_quote_drops_total`). `Start` returns the symbols no provider can serve. `cache.go` is a ring buffer per symbol, seedable from history via `SeedCandles`.
- **`internal/tui/`** — `app.go` is the root Bubbletea model that routes messages and manages 9 tabs. Each tab is its own package (watchlist, portfolio, alerts, chart, macro, news, heatmap, options, correlation) with Model/Update/View, plus the overlay packages (detail, alertdialog, symbolinfo, palette, help, statusbar).
- **`internal/alert/`** — Rule engine with cooldown. Conditions are **edge-triggered**: the engine remembers the previous evaluation of each rule and fires on the transition into the condition, and the first evaluation after startup establishes that baseline without firing. Notifier fan-out is asynchronous — call `Flush` before process exit or queued notifications are lost.
- **`internal/indicator/`** — Pure math functions: RSI(14), SMA, EMA, MACD(12,26,9), Bollinger, VWAP, OBV, ATR, Stochastic, ADX, Pivots, VolumeProfile, Patterns.
- **`internal/config/`** — Viper-based YAML config at `~/.config/mkt/config.yaml`. `defaults.go` has 11 default watchlists, 12 thematic default portfolios and 11 example alerts (counts are pinned by `defaults_test.go`).

**Config write safety (`internal/config/save.go`, `backup.go`, `diff.go`):** every write is atomic (temp + rename), takes a timestamped backup (`config.yaml.bak.<ts>`, newest 10 kept), and diffs old against new to name what would be dropped. `LoadWithResult` reports a *degraded* load — the file exists but does not parse — in which case `Config` holds defaults and `SaveSafely` refuses every write, returning a `*DestroyError` recoverable with `errors.As`. `--force` overrides; `--yes` skips only the confirmation prompt. Never add a write path that bypasses `SaveSafely`.

**Concurrency model:** Providers run as goroutines writing to a shared quote channel. Hub reads the channel, calls observers synchronously, and dispatches to the broadcaster. Bubbletea serializes all UI updates — no mutexes in the TUI layer. Mutexes only exist in the market cache, the alert engine and the broadcaster.

**Symbol routing:** Symbols with the `symbol.FREDPrefix` (`FRED:`) prefix route to the FRED economic-data provider (history only); symbols with `-USD`/`-USDT` suffixes route to Coinbase; bare tickers and the index/futures/FX pseudo-tickers (`^GSPC`, `^TNX`, `DX-Y.NYB`, `GC=F`, `EURUSD=X`, …) route to Yahoo.

## Key Patterns

- All TUI state changes happen via Bubbletea messages (`messages.go`), not direct mutation
- Indicator and portfolio packages are stateless — pure functions over data
- Theme system (`tui/theme/`) has 7 presets; pressing `T` rebuilds all styles in-place, no restart
- Config persists to YAML; CLI commands (`mkt config set/add/remove`), `mkt config repair` and the TUI (alert edits) all go through the same safe-write layer
- Portfolio totals cover priced positions only — `Summary.Unpriced` / `Coverage()` exist so an unquoted holding is reported, never folded in at break-even
- Exported symbols carry doc comments; errors wrap with `%w` (the config layer's `errors.As` recovery depends on it)
