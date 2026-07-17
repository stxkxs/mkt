# mkt

Real-time stock and crypto market dashboard for the terminal — single binary, no API keys.

Crypto prices stream live via Coinbase WebSocket. Stock quotes poll from Yahoo Finance. Watchlist with sparklines, candlestick/line charts with technical indicators, portfolio P&L tracking, macro dashboard, news feed, sector heatmap, and price alerts with desktop notifications. 7 color themes.

Built with Go, [Bubbletea v2](https://charm.land/bubbletea), and [Lipgloss v2](https://charm.land/lipgloss).

![mkt demo](demo/mkt.gif)

---

## Installation

### go install

```sh
go install github.com/stxkxs/mkt@latest
```

### Build from source

Requires Go 1.25+ and [Task](https://taskfile.dev) (`brew install go-task` or `go install github.com/go-task/task/v3/cmd/task@latest`).

```sh
git clone https://github.com/stxkxs/mkt.git
cd mkt
task build    # or: go build -o mkt .
./mkt
```

---

## Usage

```sh
mkt                             # launch TUI dashboard
mkt watch BTC-USD ETH-USD AAPL  # stream prices to stdout (no TUI)
mkt daemon                      # headless: hub + alerts + notifiers, no TUI
mkt mcp                         # Model Context Protocol server over stdio
mkt backtest rules.yaml replay.ndjson   # replay an alert ruleset against a recorded stream
mkt config show                 # view configuration
mkt config add TSLA LINK-USD    # add symbols to watchlist
mkt config remove DOGE-USD      # remove a symbol
mkt config set poll_interval 30s
mkt config validate             # check config for invalid values
mkt portfolio import --portfolio Tech schwab-export.csv   # import broker CSV
mkt position --equity 100000 --risk 1 --entry 50 --stop 48 # share-sizing calc
mkt version
```

Global flags (available on every subcommand):

- `--listen 127.0.0.1:9999` — start a read-only HTTP server exposing `/quotes`, `/quotes/{symbol}`, `/alerts`, `/metrics`, and `/webhook/tradingview`. Works in dashboard or daemon mode.
- `--listen-token <token>` — require `Authorization: Bearer <token>` (header only; query-string tokens are rejected so secrets don't leak into logs) on every HTTP request. **This is mandatory for any non-loopback bind** — a loopback bind (`127.0.0.1:9999`) may omit it, but `mkt` **refuses to start** on any other address (including the all-interfaces form `:9999` / `0.0.0.0`) without a token, since `/webhook/tradingview` can inject alerts and `/alerts` leaks configured destinations. The webhook is also rate-limited to guard against notification spam.
- Record a live quote stream for later backtesting: `MKT_RECORD=session.ndjson mkt`. Replay it: `mkt backtest rules.yaml session.ndjson`.

Import supports two CSV formats (auto-detected from the header):

- **generic** — `date,type,symbol,quantity,price,fee,note` where `type` is `buy|sell|dividend`
- **schwab** — Charles Schwab transaction export (Buy / Sell / Reinvest Dividend)

`--dry-run` parses and prints a summary without modifying the config; `--format` overrides auto-detect.

### TUI Keybindings

| Key | Action |
|-----|--------|
| `1`–`9` | Jump to tab (Watch, Portfolio, Alerts, Chart, Macro, News, Heatmap, Options, Correl) |
| `tab` / `shift+tab` | Cycle tabs |
| `j` / `k` | Navigate rows |
| `?` | Keybinding help for the active tab |
| `enter` | Detail panel (watchlist) / open link (news) / drill down (heatmap) |
| `s` | Cycle watchlist sort (config order → change% → volume → price) |
| `c` | Full-screen chart for selected symbol |
| `i` | Symbol info (watchlist) / toggle indicator menu on chart (1-9, a, p, v, k to toggle SMA/EMA/Bollinger/RSI/MACD/VWAP/OBV/ATR/Stoch/ADX/Pivots/VolProfile/Patterns) |
| `O` | Load options chain for selected symbol (switches to Options tab) |
| `:` | Open command palette (type a tab name, `theme <name>`, or `q`) |
| `a` | Add selected symbol to comparison set |
| `C` | Open multi-symbol comparison chart |
| `[` / `]` | Change chart interval (1m → 1w) / switch portfolio |
| `+` / `-` | Zoom chart in/out |
| `m` | Toggle candlestick / line chart |
| `T` | Cycle color theme |
| `esc` | Close panel / chart / go back |
| `q` | Quit |

### Watchlist Tab

Live prices with 24h change, volume, and sparkline trend for each symbol. Crypto updates in real-time via WebSocket; stocks poll at a configurable interval (default 15s).

### Portfolio Tab

Track holdings with live unrealized P&L across multiple thematic portfolios. Switch portfolios with `[` / `]`. Configure positions in `~/.config/mkt/config.yaml`.

### Alerts Tab

Set price alerts that fire desktop notifications:

```yaml
alerts:
  - symbol: BTC-USD
    condition: above    # above, below, pct_up, pct_down, volume_above, stddev_above
    value: 100000
    enabled: true
```

`volume_above` triggers when the quote's reported volume exceeds the value. `stddev_above` triggers when the rolling standard deviation over `period` quotes exceeds `value` percent of the rolling mean — a volatility expansion proxy when full OHLC isn't available.

Alerts have a 5-minute cooldown to prevent spam. Toggle and delete alerts from the TUI.

Compound rules combine multiple conditions with `match: all`, `match: any`, or `match: sequence` (in declared order):

```yaml
alerts:
  - symbol: BTC-USD
    enabled: true
    match: all                       # every sub-condition must fire
    conditions:
      - condition: above
        value: 100000
      - condition: rsi_above
        value: 70

  - symbol: ETH-USD
    enabled: true
    match: sequence                  # first crossed up, then dropped
    conditions:
      - condition: above
        value: 3500
      - condition: below
        value: 3300
```

Alerts can also POST a JSON payload to a webhook on every trigger — useful for Slack/Discord/IFTTT or any custom receiver. Set a default URL at the top level and/or override per rule:

```yaml
webhook_url: https://hooks.slack.com/services/...   # default destination (optional)

alerts:
  - symbol: BTC-USD
    condition: above
    value: 100000
    enabled: true
    webhooks:                                       # per-rule override (optional)
      - https://discord.com/api/webhooks/...
```

The payload is `{symbol, condition, value, price, message, timestamp}`.

For mobile push, configure ntfy.sh (no signup) and/or Pushover (free dev account):

```yaml
ntfy_topic: mkt-alerts-<your-unique-string>   # subscribe in the ntfy app
# ntfy_server: https://ntfy.sh                # optional override

pushover_user: u-...                          # your Pushover user key
pushover_token: a-...                         # your Pushover application token
```

### Charts

Press `c` on any symbol for a full-screen candlestick or line chart. Press `i` to overlay technical indicators:

- **SMA(20)** / **EMA(20)** — moving average lines on the price axis
- **Bollinger Bands** — upper/middle/lower bands on the price axis
- **VWAP** — anchored volume-weighted average price overlay on the price axis
- **RSI(14)** — relative strength index in a sub-panel (0–100, ref lines at 30/70)
- **MACD(12,26,9)** — MACD line, signal line, and histogram in a sub-panel
- **OBV** — on-balance volume in a sub-panel (running signed-volume total)
- **ATR(14)** — Wilder-smoothed Average True Range in a sub-panel
- **Stochastic(14,3)** — %K and %D oscillator in a sub-panel (ref lines at 20/80)
- **ADX(14)** — trend strength with +DI/-DI in a sub-panel (ref line at 25)
- **Pivots** — classic floor-trader pivot lines (P, R1-R3, S1-S3) overlaid on the main chart from the prior session's HLC
- **Volume Profile** — horizontal volume histogram in a right-side gutter; point-of-control row highlighted
- **Candle Patterns** — Doji, Hammer, Shooting Star, Bullish/Bearish Engulfing marked with glyphs on the candlestick chart

Multiple indicators can be active simultaneously.

### Comparison Chart

From the watchlist, press `a` on up to 3 symbols to add them to a comparison set, then `C` to open. Prices are normalized to % change from the first visible candle. Each symbol gets a distinct color.

### Macro Dashboard

Fixed set of macro indicators updated on the same poll interval: 10Y Treasury, 13W T-Bill, VIX, Dollar (DXY), Gold, WTI Crude, S&P 500, and Bitcoin. Includes a computed 2s10s yield spread.

### News Feed

Aggregated RSS headlines from Yahoo Finance, MarketWatch, and CNBC. Polls every 3 minutes. Press `enter` to open a headline in your browser.

Optionally, add SEC EDGAR per-ticker filings into the same feed via `edgar_tickers` in config:

```yaml
edgar_tickers: [AAPL, NVDA, TSLA]
```

Filings appear with source `SEC:<TICKER>` and a category (8-K, 10-Q, etc.). Press `f` in the News tab to cycle the filter: All / News / Filings.

### Sector Heatmap

Treemap overview of 18 sectors colored by average daily change (red → green gradient). Press `enter` to drill down into a sector and see individual stock tiles sorted by performance with price, change%, volume, and colored bars. Press `esc` to return to the overview.

### Themes

Press `T` to cycle through 7 color themes: Tokyonight (default), Catppuccin Mocha, Gruvbox Dark, Nord, Dracula, Solarized Dark, and Catppuccin Latte (light). Theme choice persists in config.

---

## Configuration

Config lives at `~/.config/mkt/config.yaml` and is created with defaults on first run.

```yaml
watchlist:
  - BTC-USD
  - ETH-USD
  - SOL-USD
  - AAPL
  - NVDA
portfolios: []
alerts: []
poll_interval: 15s
sparkline_len: 60
theme: tokyonight
```

---

## Data Sources

| Source | Protocol | Data | Auth |
|--------|----------|------|------|
| [Coinbase Advanced Trade](https://docs.cdp.coinbase.com/advanced-trade-api/docs/ws-overview) | WebSocket | Real-time crypto prices, level-2 order book | None |
| [Coinbase Exchange](https://docs.cloud.coinbase.com/exchange/reference/exchangerestapi_getproductcandles) | REST | Historical crypto candles, REST order-book snapshot | None |
| [Yahoo Finance](https://finance.yahoo.com) | REST (polling) | Stock quotes, history, macro indicators, options chains, earnings calendar | None (session cookies) |
| [FRED](https://fred.stlouisfed.org/) (St. Louis Fed) | REST (CSV) | Economic series (DFF, T10Y2Y, UNRATE, CPIAUCSL, …) via `FRED:` prefix | None |
| [DeFiLlama](https://defillama.com/) | REST | Per-chain TVL | None |
| [Binance Futures](https://binance-docs.github.io/apidocs/futures/) | REST | Funding rate + open interest (BTC/ETH/SOL perps) | None |
| Yahoo Finance / MarketWatch / CNBC | RSS | News headlines | None |
| [SEC EDGAR](https://www.sec.gov/cgi-bin/browse-edgar) | Atom (RSS) | Per-ticker filings (8-K, 10-Q, 10-K) | None |

No API keys required. Crypto streams from Coinbase (US-native, no geo-restrictions). Stock data polls from Yahoo Finance.

---

## Integrations

`mkt` is both a TUI *and* a headless data source, so it drops into the terminal ecosystem two ways: **hosts** run its TUI, and **consumers** read its data over MCP, HTTP, or a Prometheus scrape. Almost everything below is config-only — no rebuild.

### Serve over SSH (`mkt serve`)

Run the dashboard as a [Charm Wish](https://github.com/charmbracelet/wish) SSH app: every connection gets its own live, per-session dashboard driven by one shared data plane. Access is gated by a **public-key allowlist** — `mkt serve` refuses to start with an empty allowlist rather than expose your holdings.

```yaml
# ~/.config/mkt/config.yaml
serve:
  addr: "0.0.0.0:2222"                   # 127.0.0.1:2222 for local-only (the default)
  host_key: ~/.config/mkt/ssh_host_key   # ed25519 key, auto-generated on first run
  authorized_keys:
    - ssh-ed25519 AAAA...you@laptop
    - ssh-ed25519 AAAA...you@phone       # or: authorized_keys_file: ~/.ssh/authorized_keys
```

```sh
mkt serve                 # start the SSH server
ssh -p 2222 your.host     # from any allowed key → a live dashboard
```

Every tab streams live; the crypto detail order-book falls back to REST snapshots in serve mode (the live level-2 stream is single-program by design).

### AI agents (`mkt mcp`)

`mkt mcp` is a stdio [Model Context Protocol](https://modelcontextprotocol.io) server exposing `quotes`, `query_history` (OHLCV), and `alerts` as structured JSON — any MCP host can query live market data mid-task.

```sh
# Claude Code
claude mcp add --transport stdio mkt -- mkt mcp
```

```json
// Crush (charm-native), Claude Desktop, Cursor, Zed, Goose, Cline, Continue …
{ "mcp": { "mkt": { "type": "stdio", "command": "mkt", "args": ["mcp"] } } }
```

### HTTP data surface (`--listen`)

`--listen` exposes a read-only HTTP API (any non-loopback bind requires `--listen-token`). `/quotes` carries price, change, and a pre-computed direction; `/metrics` emits per-symbol gauges in Prometheus text format.

```sh
mkt --listen 127.0.0.1:9999
curl -s 127.0.0.1:9999/quotes/BTC-USD
# {"symbol":"BTC-USD","price":64210.5,"change":1342.1,"change_pct":2.14,"dir":"up"}
```

```
# /metrics
mkt_price{symbol="BTC-USD"} 64210.5
mkt_change_pct{symbol="BTC-USD"} 2.14
mkt_symbols_cached 12
mkt_uptime_seconds 3841.0
```

**Prometheus / Grafana / Netdata** — point a scrape at it:

```yaml
scrape_configs:
  - job_name: mkt
    static_configs:
      - targets: ['127.0.0.1:9999']
    # non-loopback bind → set --listen-token and uncomment:
    # authorization: { credentials: "<token>" }
```

### Terminal multiplexer (tmux)

Host the TUI in a popup, and put a live ticker in the status bar off `/quotes`:

```tmux
# ~/.tmux.conf — floating dashboard on prefix + g
bind-key g display-popup -E -w 90% -h 90% 'mkt'

# ticker in status-right (start `mkt --listen 127.0.0.1:9999` first)
set -g status-interval 5
set -g status-right '#(curl -s 127.0.0.1:9999/quotes/BTC-USD | jq -r "\(.dir=="up" ? "▲" : "▼") \(.price)")'
```

For a busy status line, curl in a small cached wrapper script rather than inline — tmux re-runs `#()` on every interval.

### Prompt ticker (Starship)

A single-symbol glance on every prompt (needs `mkt --listen` running):

```toml
# ~/.config/starship.toml
command_timeout = 1000

[custom.mkt]
command = '''curl -s 127.0.0.1:9999/quotes/BTC-USD | jq -r '"\(.symbol) \(.price) (\(.change_pct)%)"' '''
when = true
format = '[$output]($style) '
style = 'bold yellow'
```

### Demo GIF (VHS)

`demo/mkt.tape` drives the binary through its tabs, theme toggle, and help overlay. Regenerate with:

```sh
task demo          # builds ./mkt, then renders demo/mkt.gif via vhs
```

---

## Hardening

`mkt`'s sharp surfaces are **off by default** — the HTTP API (`--listen`), the SSH dashboard (`mkt serve`), the MCP server (`mkt mcp`), and the webhook/ntfy/Pushover notifiers all require explicit opt-in. When you do enable them, these knobs scope them down:

| To disable / restrict | Do this |
|---|---|
| Desktop popups + bell | `--no-desktop-notify` (already off under `mkt serve`) or `desktop_notify: false` |
| **All** notifiers (keep rules + history) | `--no-notify` or `notifications: false` |
| Inbound TradingView webhook | off by default; needs `--enable-webhook` **and** `--listen-token` (even on loopback) |
| `/alerts` webhook-URL leak | redacted automatically — the API returns `has_webhooks`, never the URLs |
| Token on a loopback bind | `--require-token` (loopback is not a trust boundary on shared hosts) |
| MCP config exposure | `mkt://config` is off unless `--expose-config`, and secrets are always redacted |
| MCP holdings exposure | `get_portfolio` + `mkt://portfolios` are off unless `--expose-portfolio` |
| Egress to a provider | `providers: { binance: false, defillama: false, news: false, macro: false }` |
| SEC EDGAR egress | `edgar_tickers: []` |
| Swap/drop news feeds | `news_feeds: [{name: ..., url: ...}]` (built-ins use https) |

Notifiers are additionally capped at ~20/min each, so a webhook flood can't drain a paid Pushover quota. Secrets at rest (`config.yaml`) are written `0600`.

**Egress allowlist** (hosts `mkt` may contact; notifier/build hosts only apply when configured):

```
advanced-trade-ws.coinbase.com  api.exchange.coinbase.com
query1.finance.yahoo.com  query2.finance.yahoo.com  finance.yahoo.com
fapi.binance.com  api.llama.fi  feeds.marketwatch.com  search.cnbc.com
www.sec.gov  fred.stlouisfed.org  api.stlouisfed.org
ntfy.sh  api.pushover.net                       # notifiers, only if configured
```

---

## Architecture

```
Quote providers (Coinbase WS, Yahoo HTTP, recording/replay)
        │
        ▼
   chan Quote (cap 128)
        │
        ▼
       Hub ─────► cache.Push()  (ring buffer per symbol)
        │  ─────► alertEngine.Check() ──► Notifiers (desktop, webhook, ntfy, Pushover, history)
        │                              ──► /metrics, /alerts on --listen
        ▼
  dispatchCh (cap 256, drops on TUI stall)
        │
        ▼
  program.Send(QuoteUpdateMsg)
        │
        ▼
  bubbletea Update() ──► route to tab views

Background pollers (each its own goroutine):
  Yahoo macro / earnings ──► MacroUpdateMsg, CalendarUpdateMsg
  Binance futures        ──► FuturesUpdateMsg
  DeFiLlama TVL          ──► TVLUpdateMsg
  RSS + SEC EDGAR        ──► NewsUpdateMsg
  Portfolio equity mark  ──► EquitySnapshotMsg

External integrations:
  --listen 127.0.0.1:9999 → /quotes (price+change+dir), /alerts, /metrics (per-symbol gauges), /webhook/tradingview
  mkt mcp (stdio)         → MCP tools/resources/prompts for Claude clients
  mkt serve               → Wish SSH server; one program per session, shared data plane (broadcaster)
  MKT_RECORD=path         → tee provider quotes to NDJSON (replay-able via mkt backtest)
```

The data plane is program-agnostic: a **broadcaster** fans every quote/update out to all attached `tea.Program`s, so `mkt` (one local program) and `mkt serve` (one per SSH session) share the exact same hub, cache, alert engine, and pollers — built once in `cmd/backend.go`.

- **Providers** stream/poll quotes into a shared channel; `Hub` fans out behind a bounded dispatcher with drop-on-back-pressure semantics
- **Alert engine** evaluates rules inline on each quote; notifiers run outside the lock with per-call timeouts and error isolation
- **Indicator package** provides pure-math SMA, EMA, RSI, MACD, Bollinger, VWAP, OBV, ATR, Stochastic, ADX, Pivots, VolumeProfile, Patterns calculations
- **Portfolio package** is stateless math: transactions → holdings, realized P&L (FIFO/LIFO/HIFO/Average), dividends, risk metrics (Sharpe, Sortino, Beta, MaxDD), correlation matrix, position sizing
- **Bubbletea** serializes all UI updates — no mutexes in the TUI layer
- **Webhook receiver** (`/webhook/tradingview`) injects TradingView alerts through the same notifier fan-out, bypassing rule evaluation

### Project Layout

```
mkt/
├── main.go                        # cmd.Execute()
├── cmd/
│   ├── root.go                    # cobra root, --listen, --listen-token, version
│   ├── backend.go                 # shared data plane (setup + buildApp + startDataPlane) for dashboard & serve
│   ├── dashboard.go               # default cmd — one local program attached to the backend
│   ├── serve.go                   # mkt serve — Wish SSH; one program per session, key allowlist
│   ├── daemon.go                  # mkt daemon — headless hub + alerts + notifiers
│   ├── watch.go                   # mkt watch — non-TUI price streaming
│   ├── config.go                  # mkt config show/set/add/remove/validate
│   ├── portfolio.go               # mkt portfolio import (CSV)
│   ├── position.go                # mkt position — share-sizing calc
│   ├── backtest.go                # mkt backtest — replay rules against an NDJSON stream
│   └── mcp.go                     # mkt mcp — Model Context Protocol server over stdio
└── internal/
    ├── config/                    # viper load/save ~/.config/mkt/config.yaml
    ├── provider/
    │   ├── provider.go            # QuoteProvider, HistoryProvider interfaces
    │   ├── types.go               # Quote, OHLCV, Interval
    │   ├── coinbase/              # WebSocket streaming + REST history + L2 order book
    │   ├── yahoo/                 # HTTP polling + chart history + macro + options + earnings
    │   ├── fred/                  # FRED economic series via CSV endpoint
    │   ├── defillama/             # per-chain TVL
    │   ├── binance/               # futures funding rate + open interest
    │   ├── calendar/              # curated economic calendar + EarningsSource interface
    │   └── recording/             # NDJSON tee decorator + replay provider
    ├── market/
    │   ├── hub.go                 # aggregates providers, fan-out via callback, drop-on-stall
    │   ├── cache.go               # ring buffer per symbol + last full quote (price, change, dir)
    │   └── history.go             # multi-provider history routing
    ├── broadcast/                 # fan bubbletea messages out to N attached programs (dashboard + serve sessions)
    ├── alert/                     # rule engine, conditions, cooldown, notifier fan-out (desktop/webhook/ntfy/Pushover/history)
    ├── portfolio/                 # stateless math: P&L, tax lots, dividends, equity curve, risk, correlation, sizing
    ├── indicator/                 # SMA, EMA, RSI, MACD, Bollinger, VWAP, OBV, ATR, Stoch, ADX, Pivots, VolProfile, Patterns
    ├── importer/                  # broker CSV formats (generic, Schwab) with header auto-detect
    ├── news/                      # RSS + SEC EDGAR feed parsing, browser URL opener
    ├── api/                       # --listen HTTP server (/quotes, /alerts, /metrics, /webhook/tradingview)
    ├── mcp/                       # JSON-RPC over stdio: tools, resources, prompts
    └── tui/
        ├── app.go                 # root model: tab switching, message routing, full-screen overlays
        ├── keys.go                # keybindings, tab types
        ├── messages.go            # TUI message types
        ├── theme/                 # color palette, 7 theme presets, panel renderer
        ├── watchlist/             # price table with sparklines
        ├── detail/                # expanded symbol info panel with live order book
        ├── chart/                 # candlestick/line charts, indicators, comparison, hover crosshair
        ├── portfolio/             # holdings table with live P&L, realized, dividends, equity sparkline
        ├── alerts/                # alert rule management
        ├── macro/                 # macro indicators, futures, TVL, upcoming events
        ├── news/                  # RSS news feed with EDGAR filings filter
        ├── heatmap/               # sector treemap with click-to-drill
        ├── options/               # options chain grid with max-pain
        ├── correlation/           # rolling-window correlation matrix
        ├── palette/               # command palette (jump-to-tab, theme switch)
        ├── alertdialog/           # modal for creating alerts from a symbol
        ├── symbolinfo/            # modal symbol info overlay
        ├── statusbar/             # connection status, theme name, help
        └── format/                # price/volume formatting utilities
```

---

## License

[MIT](LICENSE)
