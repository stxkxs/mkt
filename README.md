# mkt

Real-time stock and crypto market dashboard for the terminal — single binary, no API keys.

Crypto prices stream live via Coinbase WebSocket. Stock quotes poll from Yahoo Finance. Watchlist with sparklines, candlestick/line charts with technical indicators, portfolio P&L tracking, macro dashboard, news feed, sector heatmap, and price alerts with desktop notifications. 7 color themes.

Built with Go, [Bubbletea v2](https://charm.land/bubbletea), and [Lipgloss v2](https://charm.land/lipgloss).

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
mkt config show                 # view configuration
mkt config add TSLA LINK-USD    # add symbols to watchlist
mkt config remove DOGE-USD      # remove a symbol
mkt config set poll_interval 30s
mkt version
```

### TUI Keybindings

| Key | Action |
|-----|--------|
| `1`–`7` | Jump to tab (Watch, Portfolio, Alerts, Chart, Macro, News, Heatmap) |
| `tab` / `shift+tab` | Cycle tabs |
| `j` / `k` | Navigate rows |
| `enter` | Detail panel (watchlist) / open link (news) / drill down (heatmap) |
| `c` | Full-screen chart for selected symbol |
| `i` | Toggle indicator menu on chart (1-5 to toggle SMA/EMA/Bollinger/RSI/MACD) |
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
    condition: above    # above, below, pct_up, pct_down
    value: 100000
    enabled: true
```

Alerts have a 5-minute cooldown to prevent spam. Toggle and delete alerts from the TUI.

### Charts

Press `c` on any symbol for a full-screen candlestick or line chart. Press `i` to overlay technical indicators:

- **SMA(20)** / **EMA(20)** — moving average lines on the price axis
- **Bollinger Bands** — upper/middle/lower bands on the price axis
- **RSI(14)** — relative strength index in a sub-panel (0–100, ref lines at 30/70)
- **MACD(12,26,9)** — MACD line, signal line, and histogram in a sub-panel

Multiple indicators can be active simultaneously.

### Comparison Chart

From the watchlist, press `a` on up to 3 symbols to add them to a comparison set, then `C` to open. Prices are normalized to % change from the first visible candle. Each symbol gets a distinct color.

### Macro Dashboard

Fixed set of macro indicators updated on the same poll interval: 10Y Treasury, 13W T-Bill, VIX, Dollar (DXY), Gold, WTI Crude, S&P 500, and Bitcoin. Includes a computed 2s10s yield spread.

### News Feed

Aggregated RSS headlines from Yahoo Finance, MarketWatch, and CNBC. Polls every 3 minutes. Press `enter` to open a headline in your browser.

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
| [Coinbase Advanced Trade](https://docs.cdp.coinbase.com/advanced-trade-api/docs/ws-overview) | WebSocket | Real-time crypto prices | None |
| [Coinbase Exchange](https://docs.cloud.coinbase.com/exchange/reference/exchangerestapi_getproductcandles) | REST | Historical crypto candles | None |
| [Yahoo Finance](https://finance.yahoo.com) | REST (polling) | Stock quotes, history, macro indicators | None (session cookies) |
| Yahoo Finance / MarketWatch / CNBC | RSS | News headlines | None |

No API keys required. Crypto streams from Coinbase (US-native, no geo-restrictions). Stock data polls from Yahoo Finance's chart API.

---

## Architecture

```
Coinbase WS ──→ chan Quote ──→ Hub ──→ cache.Push()
Yahoo HTTP  ──→ chan Quote ──↗       ──→ alertEngine.Check()
                                     ──→ program.Send(QuoteUpdateMsg)
                                               ↓
Yahoo macro ──→ program.Send(MacroUpdateMsg)
RSS feeds   ──→ program.Send(NewsUpdateMsg)
                                               ↓
                                     bubbletea Update() → route to views
```

- **Providers** stream/poll quotes into a shared channel
- **Hub** reads the channel, updates the ring buffer cache, and calls back to send TUI messages
- **Alert engine** evaluates rules inline on each quote
- **Indicator package** provides pure-math SMA, EMA, RSI, MACD, Bollinger calculations
- **Bubbletea** serializes all UI updates — no mutexes in the TUI layer

### Project Layout

```
mkt/
├── main.go                        # cmd.Execute()
├── cmd/
│   ├── root.go                    # cobra root, version, --quiet
│   ├── dashboard.go               # default cmd — wires providers + hub + TUI
│   ├── watch.go                   # mkt watch — non-TUI price streaming
│   └── config.go                  # mkt config show/set/add/remove
└── internal/
    ├── config/                    # viper load/save ~/.config/mkt/config.yaml
    ├── provider/
    │   ├── provider.go            # QuoteProvider, HistoryProvider interfaces
    │   ├── types.go               # Quote, OHLCV, Interval
    │   ├── coinbase/              # WebSocket streaming + REST history
    │   └── yahoo/                 # HTTP polling + chart history + macro quotes
    ├── market/
    │   ├── hub.go                 # aggregates providers, fan-out via callback
    │   ├── cache.go               # ring buffer per symbol for sparklines
    │   └── history.go             # multi-provider history routing
    ├── alert/                     # rule engine, conditions, cooldown, desktop notifications
    ├── portfolio/                 # stateless P&L calculator
    ├── indicator/                 # SMA, EMA, RSI, MACD, Bollinger Bands
    ├── news/                      # RSS feed parser, browser URL opener
    └── tui/
        ├── app.go                 # root model: tab switching, message routing
        ├── keys.go                # keybindings, tab types
        ├── messages.go            # TUI message types
        ├── theme/                 # color palette, 7 theme presets, Apply/NextTheme
        ├── watchlist/             # price table with sparklines
        ├── detail/                # expanded symbol info panel
        ├── chart/                 # candlestick/line charts, indicators, comparison
        ├── portfolio/             # holdings table with live P&L
        ├── alerts/                # alert rule management
        ├── macro/                 # macro dashboard (rates, VIX, commodities)
        ├── news/                  # RSS news feed with browser open
        ├── heatmap/               # sector treemap with drill-down
        ├── statusbar/             # connection status, theme name, help
        └── format/                # price/volume formatting utilities
```

---

## License

[MIT](LICENSE)
