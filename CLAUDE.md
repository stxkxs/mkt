# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`mkt` — real-time terminal market dashboard for stocks and crypto. Single Go binary, no API keys required. Crypto streams via Coinbase WebSocket, stocks poll from Yahoo Finance. Built with Bubbletea v2 + Lipgloss v2.

## Build Commands

Uses [Task](https://taskfile.dev) runner (Taskfile.yml):

```sh
task build     # go build with version ldflags → ./mkt
task run       # build + run
task test      # go test ./...
task lint      # golangci-lint run ./...
task tidy      # go mod tidy
task clean     # rm binary
```

Build injects version/commit/date via ldflags into `cmd.version`, `cmd.commit`, `cmd.date`.

## Architecture

Message-driven TUI with provider abstraction:

```
Providers (Coinbase WS, Yahoo HTTP) → chan Quote → Hub → cache + alertEngine + program.Send()
                                                                    ↓
                                                          Bubbletea Update() → route to tab views
```

**Key layers:**

- **`cmd/`** — Cobra CLI. `dashboard.go` is the main wiring point: creates providers, hub, alert engine, portfolios, and starts the Bubbletea program.
- **`internal/provider/`** — `QuoteProvider` and `HistoryProvider` interfaces (`provider.go`). Implementations: `coinbase/` (WebSocket streaming + REST candles), `yahoo/` (HTTP polling + chart history), `binance/` (alternative crypto WS).
- **`internal/market/`** — `hub.go` multiplexes providers and fans out quotes to cache/alerts/TUI. `cache.go` is a ring buffer per symbol for sparklines.
- **`internal/tui/`** — `app.go` is the root Bubbletea model that routes messages and manages 7 tabs. Each tab is its own package (watchlist, chart, portfolio, alerts, macro, news, heatmap) with Model/Update/View.
- **`internal/alert/`** — Rule engine with cooldown. Supports price, percent change, RSI, SMA cross, MACD cross conditions.
- **`internal/indicator/`** — Pure math functions: RSI(14), SMA, EMA, MACD(12,26,9), Bollinger Bands.
- **`internal/config/`** — Viper-based YAML config at `~/.config/mkt/config.yaml`. `defaults.go` has 13 thematic default portfolios.

**Concurrency model:** Providers run as goroutines writing to a shared quote channel. Hub reads the channel and dispatches via `program.Send()`. Bubbletea serializes all UI updates — no mutexes in the TUI layer. Mutexes only exist in the market cache and alert engine.

**Symbol routing:** Symbols with `-USD`/`-USDT` suffixes route to Coinbase; bare tickers route to Yahoo.

## Key Patterns

- All TUI state changes happen via Bubbletea messages (`messages.go`), not direct mutation
- Indicator and portfolio packages are stateless — pure functions over data
- Theme system (`tui/theme/`) has 7 presets; pressing `T` rebuilds all styles in-place, no restart
- Config persists to YAML; CLI commands (`mkt config set/add/remove`) and TUI both modify it
