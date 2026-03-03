# mkt

Real-time stock and crypto market dashboard for the terminal — single binary, no API keys.

Crypto prices stream live via Coinbase WebSocket. Stock quotes poll from Yahoo Finance. Watchlist with sparklines, candlestick/line charts, portfolio P&L tracking, and price alerts with desktop notifications.

Built with Go, [Bubbletea v2](https://charm.land/bubbletea), and [Lipgloss v2](https://charm.land/lipgloss).

---

## Installation

### go install

```sh
go install github.com/stxkxs/mkt@latest
```

### Build from source

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
| `1`–`4` | Jump to tab (Watchlist, Portfolio, Alerts, Chart) |
| `tab` / `shift+tab` | Cycle tabs |
| `j` / `k` | Navigate rows |
| `enter` | Detail panel for selected symbol |
| `c` | Full-screen chart |
| `[` / `]` | Change chart interval (1m → 1w) |
| `+` / `-` | Zoom chart in/out |
| `m` | Toggle candlestick / line chart |
| `esc` | Close panel / chart |
| `q` | Quit |

### Watchlist Tab

Live prices with 24h change, volume, and sparkline trend for each symbol. Crypto updates in real-time via WebSocket; stocks poll at a configurable interval (default 15s).

### Portfolio Tab

Track holdings with live unrealized P&L. Configure positions in `~/.config/mkt/config.yaml`:

```yaml
holdings:
  - symbol: BTC-USD
    quantity: 0.5
    cost_basis: 40000
  - symbol: AAPL
    quantity: 100
    cost_basis: 150
```

### Alerts Tab

Set price alerts that fire desktop notifications:

```yaml
alerts:
  - symbol: BTC-USD
    condition: above    # above, below, pct_up, pct_down
    value: 100000
    enabled: true
  - symbol: AAPL
    condition: below
    value: 150
    enabled: true
```

Alerts have a 5-minute cooldown to prevent spam. Toggle and delete alerts from the TUI.

### Charts

Press `c` on any symbol for a full-screen candlestick or line chart. Interval switching from 1-minute to 1-week. Zoom with `+`/`-`. Historical data from Coinbase (crypto) and Yahoo Finance (stocks).

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
holdings: []
alerts: []
poll_interval: 15s
sparkline_len: 60
theme: dark
```

---

## Data Sources

| Source | Protocol | Data | Auth |
|--------|----------|------|------|
| [Coinbase Advanced Trade](https://docs.cdp.coinbase.com/advanced-trade-api/docs/ws-overview) | WebSocket | Real-time crypto prices | None |
| [Coinbase Exchange](https://docs.cloud.coinbase.com/exchange/reference/exchangerestapi_getproductcandles) | REST | Historical crypto candles | None |
| [Yahoo Finance](https://finance.yahoo.com) | REST (polling) | Stock quotes + history | None (session cookies) |

No API keys required. Crypto streams from Coinbase (US-native, no geo-restrictions). Stock data polls from Yahoo Finance's chart API.

---

## Architecture

```
Coinbase WS ──→ chan Quote ──→ Hub ──→ cache.Push()
Yahoo HTTP  ──→ chan Quote ──↗       ──→ alertEngine.Check()
                                     ──→ program.Send(QuoteUpdateMsg)
                                               ↓
                                     bubbletea Update() → route to views
```

- **Providers** stream/poll quotes into a shared channel
- **Hub** reads the channel, updates the ring buffer cache, and calls back to send TUI messages
- **Alert engine** evaluates rules inline on each quote
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
    │   ├── yahoo/                 # HTTP polling + chart history
    │   └── binance/               # WebSocket (non-US, kept as alternative)
    ├── market/
    │   ├── hub.go                 # aggregates providers, fan-out via callback
    │   ├── cache.go               # ring buffer per symbol for sparklines
    │   └── history.go             # multi-provider history routing
    ├── alert/                     # rule engine, conditions, cooldown, desktop notifications
    ├── portfolio/                 # stateless P&L calculator
    └── tui/
        ├── app.go                 # root model: tab switching, message routing
        ├── keys.go                # keybindings, tab types
        ├── styles.go              # lipgloss v2 color palette
        ├── messages.go            # TUI message types
        ├── watchlist/             # price table with sparklines
        ├── detail/                # expanded symbol info panel
        ├── chart/                 # full-screen candlestick/line charts
        ├── portfolio/             # holdings table with live P&L
        ├── alerts/                # alert rule management
        └── statusbar/             # connection status, help
```

---

## License

[MIT](LICENSE)
