# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

A remediation pass across the whole tree: every leaf and mid-layer package was
audited and fixed, then those fixes were wired through the CLI and the TUI root.
110 files changed. Several behaviours changed in ways an existing user will
notice — read **Behavioral changes** first.

### Behavioral changes

These are not bugs being fixed quietly; they change what `mkt` does with an
unchanged config.

- **Alert conditions are now edge-triggered.** A level condition (`above`,
  `below`, `pct_up`, `pct_down`, `rsi_above`, `rsi_below`, `volume_above`,
  `stddev_above`) fires on the *transition into* the condition rather than on
  every quote while the condition merely holds. **The first evaluation after
  startup establishes a baseline and does not fire**, so a rule that is already
  breached at launch stays quiet until it un-breaches and re-breaches. This is
  what stopped a fresh install from spraying a notification for every seeded
  alert that happened to be true at that moment. Cross conditions
  (`sma_cross_above`, `sma_cross_below`, `macd_cross`) were already
  edge-triggered by construction and are unchanged.
- **Config writes are refused while `config.yaml` does not parse.** `mkt` no
  longer refuses to *start* — it runs on defaults and shows a persistent banner —
  but every write (CLI or TUI) is refused until the file is repaired, because
  writing then would replace the user's real settings with the defaults mkt fell
  back to. `--force` overrides.
- **Portfolio totals exclude unpriced positions.** A holding with no quote used
  to be folded into the totals at break-even, which quietly understated P&L%
  and overstated cost coverage. `Summary` now carries `Unpriced`,
  `FullyPriced()`, `UnpricedCost()` and `Coverage()`, and the portfolio tab and
  MCP `get_portfolio` report them.
- **`broadcast.Send` is asynchronous.** It used to block on the slowest attached
  program, so one wedged SSH session could stall the shared data plane for
  everyone. It now queues (256 slots by default, `NewSized` to change) and drops
  for that sender alone, counted by `Drops()` / `DropsFor(sender)`.
- **`alert.Engine` notifier fan-out is asynchronous.** Callers must call
  `Engine.Flush(timeout)` before process exit or queued notifications are lost.
  All three commands do this via the shared cleanup path.
- **`market.Hub.Start` now returns `[]string`** — the symbols no provider can
  serve. Callers that ignored the return value compile unchanged; the dashboard
  uses it to name unroutable symbols instead of letting them silently never
  price.
- **`fred.Prefix` was removed.** Use `symbol.FREDPrefix`, the single source of
  truth.
- **Default symbol updates:** `EATON` → `ETN`, `SQ` → `XYZ` (Block's ticker
  change), `GOLD` → `B` (Barrick's ticker change), and `MATIC` → `POL`. `MATIC`
  in an existing config is remapped automatically by `symbol.Canonical`, so no
  edit is required.
- **`mkt daemon` runs the full data plane.** It previously ran only the hub and
  alert engine; it now also keeps the equity curve, news, macro, futures, DeFi
  TVL and calendar advancing, seeds the cache, and honours `MKT_RECORD`.

### Added

- **`mkt config repair`** — recover a config that was broken by hand or
  overwritten. `--list` prints the available timestamped backups (`# / TAKEN /
  AGE / SIZE / PATH`, newest first); `--from-backup <path|N>` restores one, and
  backs up the file it replaces first. Exits non-zero if the restored file still
  does not parse.
- **`mkt portfolio stats`** — Sharpe, Sortino, max drawdown, volatility, CAGR
  and beta computed from the recorded equity curve. `--portfolio` narrows to
  one, `--rf` sets the risk-free rate per mark interval, `--benchmark` picks the
  beta reference (default `SPY`; empty string skips the fetch). Prints `n/a` for
  undefined statistics and warns that annualized figures are extrapolations when
  there is less than a week of history.
- **Automatic timestamped config backups.** Every write that changes the file
  leaves a `config.yaml.bak.YYYYMMDD-HHMMSS` beside it; the newest 10 are kept.
  New `config.Backup`, `ListBackups`, `RestoreBackup`, `MaxBackups`.
- **Destructive-write confirmation.** `config.SaveSafely` diffs the file on disk
  against what is about to be written, names exactly what would be lost, and
  asks before proceeding. `--yes` / `-y` skips the prompt (available on
  `mkt config …` and `mkt portfolio …`); `--force` implies `--yes` and
  additionally authorizes a write over a file that does not parse. With no
  terminal to prompt on, the write is refused rather than silently accepted.
  New `SaveOptions`, `SaveReport`, `DestroyError`, `ErrWouldDestroy`.
- **Degraded-config banner.** A config that does not parse no longer blocks
  startup. `mkt` runs on defaults, prints the path, line and parse error on
  stderr, and shows a persistent, non-dismissible banner under the tab bar on
  every tab plus a `⚠ config` marker in the tab bar. New
  `config.LoadWithResult` / `LoadResult`, `tui.ConfigStatus`,
  `App.LoadConfigBanner`, `App.SetConfigStatus`.
- **`--force`** on `mkt`, `mkt serve` and `mkt daemon`: start and keep writing
  config even when the file does not parse (a timestamped backup is still taken
  before any write).
- **`--persist-alerts`** on `mkt serve`: let SSH sessions write alerts they
  create/toggle/delete back to the host's config. Off by default — a guest must
  not rewrite the host's config.
- **Alert persistence.** Alerts created, toggled or deleted in the TUI are now
  written back to `~/.config/mkt/config.yaml`, coalesced with a 2s debounce and
  backed up per write. Single-writer by construction, so N SSH sessions cannot
  race. New `alert.Engine.SetOnRulesChanged`.
- **Startup cache seeding.** Every subscribed symbol is backfilled from daily
  history at startup (concurrency 4, 20s per symbol, non-fatal, off the first-paint
  path), so sparklines are populated immediately and `RSI(14)` / SMA-cross rules
  evaluate over days rather than over accumulated poll ticks. New
  `market.Cache.Seed`, `SeedCandles`, `Seeded`, `Series`.
- **Unroutable-symbol reporting.** Symbols no provider can serve are named on
  stderr at startup (`mkt: 2 symbol(s) no provider can serve, they will never
  price: APPL, NOTREAL`) with a typo/`FRED:` hint, and surfaced in the TUI as a
  notice row. New `Hub.Unroutable`, `App.LoadUnroutableSymbols`.
- **`--listen-token-file`** (and `MKT_LISTEN_TOKEN_FILE`, `MKT_LISTEN_TOKEN`,
  `MKT_LISTEN`): read the listen bearer token from a file instead of argv.
  Precedence is `--listen-token` > `--listen-token-file` >
  `MKT_LISTEN_TOKEN_FILE` > `MKT_LISTEN_TOKEN`. An explicit `--listen-token`
  now warns that argv is world-readable via `ps`.
- **`mkt config validate --check-symbols`** — fetch one bar per configured
  symbol to catch a typo like `APPL` that routes cleanly but will never produce
  data. Offline validation cannot detect this.
- **`mkt mcp --expose-config` / `--expose-portfolio`** — both off by default.
  `mkt://config`, `mkt://portfolios` and `get_portfolio` are hidden unless
  explicitly enabled.
- **`internal/symbol`** — single source of truth for symbol classification and
  canonicalization. `Canonical` normalizes `btc` / `BTCUSDT` / `btc-usd` to
  `BTC-USD` and `aapl` to `AAPL`, is idempotent, and applies the `MATIC` → `POL`
  migration. `IsStock` now also accepts the index / futures / FX pseudo-tickers
  (`^GSPC`, `^TNX`, `^IRX`, `^VIX`, `DX-Y.NYB`, `GC=F`, `CL=F`, `EURUSD=X`, …).
  `coinbase.Supports`, `yahoo.Supports`, `fred.Supports` and the earnings ticker
  filter all delegate here, so the lists can no longer drift apart.
- **`internal/httpx`** — the shared GET → check-status → read-capped-body →
  decode path every HTTP provider was hand-rolling, with a 16 MiB
  `io.LimitReader` cap so a hostile upstream cannot stream an unbounded body
  into memory. New `StatusError` recoverable with `errors.As`.
- **Macro tab is scrollable** — `j`/`k`, arrows, `g`/`G`, `pgup`/`pgdn` and the
  mouse wheel. It previously had no `Update` at all.
- **Chart `f`** restores auto-fit zoom.
- **Watchlist `/`** opens a fuzzy symbol search (`enter` jumps, `esc` cancels,
  `ctrl+n` / `ctrl+p` move).
- **Correlation `b`** cycles the resampling bucket (30s → 1m → 5m → 15m), and
  the matrix is computed over log returns resampled onto that common bucket so a
  15s-polled stock and a tick-streamed crypto are comparable.
- `mkt_quote_drops_total` on `/metrics` whenever `--listen` is used — quotes shed
  by TUI back-pressure. New `api.Server.WithDrops`.
- `mkt serve` logs back-pressure every 5 minutes (hub dispatch drops, observer
  drops and backlog, notifier drops, and per-session broadcaster drops named by
  SSH peer address), and is silent while everything is healthy.
- `Hub.AddObserver` — a reliable fan-out path that is never dropped. Alert
  evaluation moved onto it.
- `portfolio.Returns`, `LogReturns`, `MarkValues`, `MarkReturns`,
  `StatsFromMarks`, `Stats`, `Sample`, `SamplesFrom`, `CorrelationMatrixSeries`.
- `alert.Engine.SetClock`, `Statuses`, `RuleStatus`, `SetOnShortHistory`,
  `Flush`, `NotifyDrops` — deterministic tests, a "this rule has 8 of the 14
  bars it needs" surface, and a drain-before-exit hook.
- `yahoo.HistoryWithMeta` / `HistoryResult` / `ServedInterval`, plus
  `Provider.Healthy`, `LastError` and `StatusChan` mirroring the Coinbase
  provider, so a Yahoo outage reaches the status bar.
- `coinbase.StreamOrderBookLoop` and `OrderBookStatus` — a reconnecting level-2
  stream that reports its own connection state instead of dying silently.
- `format.Repeat`, `format.Spaces`, `format.VisibleRows`,
  `theme.SectionHeaderHint`, `heatmap.NormalizeSectors`,
  `tui.CanonicalSymbols`.
- Cross-platform CI: tests on ubuntu, macOS and Windows, and a build matrix
  covering all six release targets (linux/darwin/windows × amd64/arm64).
- `bodyclose`, `errorlint` and `gosec` added to `.golangci.yml`, each with
  narrowly-scoped, commented exclusions where they fire on deliberate code.

### Changed

- **`cmd/backend.go` is the single wiring point.** `mkt`, `mkt serve` and
  `mkt daemon` are thin shells over `setupBackend` → `buildApp` →
  `startDataPlane`, so all three share one hub, cache, alert engine and poller
  set. `cmd/daemon.go` shrank from 124 lines to 76 and its hand-copied rule
  conversion is gone.
- **Portfolio holdings and transaction symbols are subscribed** even when not on
  any watchlist. `mkt portfolio import` alone is now enough to get live prices.
- **Every symbol entering the data plane is canonicalized**, so `btc`,
  `BTC-USD` and `BTCUSDT` in a config produce one subscription and one
  correlation row rather than three.
- **`MKT_RECORD` no longer truncates an existing capture.** The previous file is
  preserved as `<path>.bak.<timestamp>` (newest 10 kept) and the path is printed.
  Appending was rejected: an NDJSON of two disjoint sessions replays as one bogus
  timeline.
- **`mkt backtest` runs in recorded time.** The engine's clock is driven from
  each quote's own timestamp, so `--cooldown` (default 5m) expires after the
  recorded interval it represents rather than after the milliseconds a burst
  replay actually takes.
- **The heatmap draws the user's own watchlist groups**, one sector per group,
  instead of a built-in fallback list — so every tile is a symbol the hub is
  actually subscribed to. It re-seeds on config reload.
- **The correlation matrix shares the watchlist universe** and de-duplicates
  aliases.
- `mkt config validate` exits non-zero and never prints "Config OK" for a file
  that did not parse; accepts `dividend`; flags unroutable symbols; and reports
  non-canonical spellings (`"matic"` → `POL-USD`).
- `mkt portfolio import` is idempotent — a transaction already present in the
  destination portfolio (same date, type, symbol, quantity and price) is skipped.
  Matching is by count, so two identical CSV rows still import as two. `--force`
  appends everything.
- `mkt watch` canonicalizes its arguments and errors naming unrecognized symbols
  instead of blocking forever waiting for a quote that can never arrive.
- `mkt position` rejects inputs that would produce a meaningless size:
  `--risk <= 0`, `--risk > 100`, `--stop == --entry`, a stop on the wrong side
  for the chosen side, and a non-positive ATR-implied stop.
- MCP `get_quote` reports `source: "live"` or `"daily-close"` (with a `note` on
  the fallback); `get_portfolio` returns `fullyPriced`, `coverage`, `unpriced`,
  `unpricedCost` and `fetchErrors`.
- Runtime failures print the error only; usage is still attached to flag-parse
  errors.
- Coinbase history aggregates to exactly the requested interval, so `4h` and
  `1w` charts are real rather than the nearest supported granularity.
- Yahoo history honours `params.Limit`, and all Yahoo calls are rate-limited and
  retried internally.
- Yahoo provider outages now surface in the status bar; previously only Coinbase
  reported status. The status bar tracks per-provider state rather than assuming
  Coinbase.
- The macro tab's yield spread is relabelled **10Y-3M Spread**. It computes
  `^TNX - ^IRX`, and `^IRX` is the 13-week bill, so it was never the 2s10s.
- The Crypto Futures panel reports `— unavailable in this region` instead of
  rendering a fabricated flat market. `fapi.binance.com` answers HTTP 451 to US
  IP addresses, which used to display as legitimate-looking `0.00` values.
- `internal/tui/chart/model.go` split into `model.go`, `grid.go`, `series.go`
  and `history.go`; `internal/config` split into `config.go`, `save.go`,
  `backup.go` and `diff.go`.
- `.github/workflows/release.yml` gates the goreleaser job on the full
  test/lint/govulncheck suite. A tag push previously published signed artifacts
  without any of them having run, because `ci.yml` triggered only on
  push-to-main and pull requests.

### Fixed

- **The Options tab never rendered.** `O` on the Watch tab jumped to it and it
  stayed on "Loading…" forever; async results were routed conditionally and lost
  if the user tabbed away or pressed `esc` mid-fetch. Same routing gap fixed for
  the detail panel's level-2 order book.
- **`q` quit the process during the alerts delete confirmation.** A tab holding a
  confirm prompt now consumes the next key before the global quit binding: `y`
  deletes, anything else cancels, and `q` quits normally once the prompt is gone.
- **Mouse clicks selected the wrong row.** The click offset now derives from the
  same `contentOrigin()` the renderer uses (tab bar + notice rows + panel top
  border), fixing an off-by-one on Watch / Portfolio / Alerts at ≥30×15.
- **A click on row 0 of the full-screen chart or comparison chart** silently
  switched a tab the user could not see — those views draw no tab bar. They now
  claim the click first.
- **The detail panel and the centered modals are modal for the mouse**, matching
  how they already absorbed keys. Clicks and wheel events behind them no longer
  move a hidden selection.
- **Alert evaluation was on the lossy path.** Alerts were evaluated from the same
  bounded dispatch that sheds under TUI back-pressure, so a stalled terminal
  could cost a notification. Evaluation moved to the reliable observer path.
- **Alerts created in the TUI were lost on exit.** They now persist (see Added).
- **`mkt backtest` under-reported fires and mis-attributed compound rules.**
  Cooldowns were anchored in wall time (so a burst replay collapsed four
  crossings into one fire) and rules were bucketed by `(Symbol, Condition,
  Value)`, so two compound rules on one symbol shared a bucket and one of them
  reported `(no fires)`. Attribution is now by rule index; on a synthetic
  8-quote tape with four upward crossings of 100, `BTC-USD above 100` went from
  `fires=1` to `fires=4`.
- **A tape written before quotes carried timestamps replayed from the Unix
  epoch.** The wire format stores `ts` as unix nanoseconds with no encoding for
  "absent", so the decoder produced 1970 rather than the zero time and the
  reported span became `1754-08-30 → 2026-03-02`. Anything at or before the
  epoch is now treated as absent.
- **Forced writes destroyed data silently.** `--force` / `--yes` skip the
  confirmation prompt, which was exactly the path where the list of removed data
  was discarded rather than reported. Every write that drops data without a
  prompt now prints what it removed plus the `mkt config repair --from-backup 1`
  recovery command.
- Yahoo `4h` charts fall back to `1h` bars (`ServedInterval`) rather than
  silently returning daily data.
- Sparklines were empty until enough poll ticks had accumulated; the cache is
  now seeded from history at startup.
- Portfolio holdings that were not on a watchlist never priced.
- Background goroutines leaked: both provider status pumps now select on
  `ctx.Done()`, and the earnings fetch uses a 30s timeout derived from the
  parent context instead of `context.Background()`.
- Queued notifications were lost at exit; the engine is now flushed (up to 3s).
- Indicators propagated NaN indefinitely. A single bad tick used to poison every
  later value of an accumulating indicator; rolling-window indicators (SMA,
  Stddev, Bollinger) now report NaN only while bad data is inside the window and
  recover as soon as it leaves, and accumulating indicators (EMA, RSI, ATR, ADX,
  VWAP, OBV) skip the bad sample instead of folding it in.
- `strings.Repeat` with a negative count panicked at small terminal sizes;
  `format.Repeat` / `format.Spaces` are negative-safe and the TUI is swept
  across widths 0–120 and heights 0–40 on every tab in tests.

### Security

- `internal/httpx` caps every provider response body at 16 MiB. A hostile or
  compromised upstream could previously stream an unbounded body into memory.
- Notification destination URLs are redacted to `scheme://host/…` before they
  appear in a log line or an error string. For every destination `mkt` supports
  the secret *is* the URL — a Slack or Discord webhook path is the credential,
  an ntfy topic is the credential, and a userinfo section is a password in plain
  sight.
- `--listen-token-file` / `MKT_LISTEN_TOKEN_FILE` keep the bearer token off
  argv, which is world-readable via `ps`. Passing `--listen-token` explicitly now
  warns.
- Config writes are atomic (temp file + rename), so an interrupted write can no
  longer leave a truncated `config.yaml`.
- `mkt serve` does not write an SSH guest's alert edits back to the host's config
  unless started with `--persist-alerts`.
- `gosec`, `bodyclose` and `errorlint` added to the lint gate; the release
  workflow is gated on lint, tests and `govulncheck`, so a broken tag cannot
  publish signed artifacts.

---

## v0.1.0 — 2026-07-17

First tagged release. Everything below shipped in it.

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
- Dividend tracking: new `dividend` transaction type. `portfolio.Dividends(txs)` and `DividendsYTD(txs, now)`. Dividends are excluded from realized P&L and from `DeriveHoldings`. Portfolio tab shows a "Dividends: $X (YTD: $Y)" line below Realized when any dividend transactions exist.
- `mkt portfolio import --portfolio <name> [--format auto|generic|schwab] [--dry-run] <file>`: reads a broker CSV export and appends transactions to a named portfolio (creating it if absent). New `internal/importer` package with a `Format` interface, `Generic` and `Schwab` implementations, and header-based auto-detect.
- `portfolio.Sharpe`, `Sortino`, `Beta`, `MaxDrawdown`: pure-math risk metrics. UI surface deferred to P6 (equity curve) where the inputs naturally live.
- Persisted portfolio equity curve: `portfolio.EquityFile` records value marks to `~/.config/mkt/equity-history.ndjson` every 5 minutes; portfolio tab header shows a block-character sparkline of the active portfolio's recent marks plus a `MaxDD: X.XX%` readout.
- `mkt position` subcommand: computes share count and dollar risk from equity, risk %, and entry/stop (or ATR-implied stop). `portfolio.PositionSize` and `portfolio.ATRStop` helpers.
- Options chain tab (`8`): `yahoo.FetchOptionsChain` pulls the nearest expiration's calls and puts; new `internal/tui/options` package renders them strike-aligned with a max-pain header. Press `O` on the Watchlist tab to load options for the selected symbol.
- Coinbase order-book depth: new `coinbase.FetchOrderBook` and `OrderBookDepth` helpers. The detail panel renders the top-5 bids/asks for crypto symbols when opened (REST snapshot per open; WebSocket live updates are a follow-up).
- Correlation matrix tab (`9`): pairwise Pearson correlation between watchlist symbols using each symbol's recent cached prices. New `portfolio.Correlation` and `CorrelationMatrix` helpers; colored grid render (green positive, red negative).
- Macro tab "Upcoming Economic Events (30d)" section: lists the next month of FOMC / CPI / NFP / GDP releases from `calendar.EconomicEvents()` with relative `in Xd / Xh` countdowns.
- Multiple named watchlists: `watchlists: [{name: ..., symbols: [...]}]` in config. Watchlist tab shows the active group name + `[/]: switch (i/n)` and cycles with `[` / `]`. The legacy top-level `watchlist:` field is still honored and appears as "Default". The hub subscribes to the deduplicated union so all groups stay live.
- Per-symbol notes: `notes:` map in `config.yaml` (`SYMBOL: "free text"`) renders below the sparkline in the detail panel for that symbol. Read-only for now — edit in YAML.
- Command palette: press `:` to open a tiny prompt at the bottom. Type a tab-name prefix (`watch`, `portfolio`, etc.) to jump, `theme <name>` to switch theme, or `q`/`quit` to exit. `esc` cancels.
- `mkt daemon` subcommand: runs the hub + alert engine + every configured notifier (desktop, webhook, ntfy, Pushover, history) headlessly. Stops on SIGTERM / SIGINT. Useful for an always-on machine that keeps firing alerts even when the TUI isn't open.
- `--listen <addr>` flag (global, e.g. `--listen :9999`): starts a read-only HTTP server with `/quotes`, `/quotes/{symbol}`, `/alerts`, and `/metrics` (Prometheus text format). Works in both dashboard and daemon modes.
- `mkt mcp` subcommand: minimal Model Context Protocol server over stdio so Claude Code / Claude Desktop / other MCP clients can query mkt. Tools: `get_quote`, `query_history`, `get_alerts`, `get_portfolio`. Hand-rolled JSON-RPC (no external dep).
- TradingView webhook receiver at `POST /webhook/tradingview` (when `--listen` is set): parses TradingView's freeform JSON body (`symbol`/`ticker`, `price`/`close`, `message`/`alert`) and fans it out through every configured notifier via the new `alert.Engine.Inject`. Bypasses rule evaluation and cooldown so TV's own rules drive timing.
- `mkt backtest <rules.yaml> <replay.ndjson>` subcommand: replays a recorded quote stream (from `MKT_RECORD`) against a YAML alert-rules file and prints per-rule fire counts plus first/last trigger timestamps. Read-only — no notifiers fire.
- Indicator test coverage: `RSI`, `SMA`, `EMA`, `MACD`, `Bollinger`.
- Hub fan-out test verifying provider reader is isolated from a slow quote consumer.
- GitHub Actions workflow running `go vet`, `go test -race`, and `golangci-lint`.
- `.golangci.yml` with `errcheck`, `govet`, `staticcheck`, `unused`, `gosimple`, `ineffassign`, `misspell`, `unconvert`, `gofmt`.
- Theme-aware heatmap gradient derived from each theme's red / dim / green palette.
- `theme.ChangedMsg` broadcast so each view rebuilds its cached styles in its own `Update`.
- Per-feed context timeout in the RSS fetcher.
- `CONTRIBUTING.md`.
- Yahoo earnings adapter for the calendar: new `yahoo.FetchEarnings` calls the v10 `quoteSummary?modules=calendarEvents` endpoint concurrently per ticker (crumb-authenticated when available). Dashboard wires an `EarningsAdapter` into the calendar source on startup and emits a `CalendarUpdateMsg` so earnings appear alongside the curated economic events.
- Chart hover crosshair: in the full-screen chart and comparison chart views, the mouse cursor now draws dashed vertical + horizontal lines through the candle area and the OHLCV summary line updates to the hovered candle. Powered by a new `MouseModeAllMotion` path for the chart views; other tabs continue to receive only click + wheel events.
- Optional bearer-token authentication on the `--listen` HTTP server: new `--listen-token <token>` flag gates `/quotes`, `/quotes/{symbol}`, `/alerts`, `/metrics`, and `/webhook/tradingview` behind `Authorization: Bearer <token>` (or `?token=<token>`). When unset and the bind address is non-loopback, the server logs a warning at startup.
- `/webhook/tradingview` now wraps the request body in `http.MaxBytesReader` (64KiB cap) and buffers it before attempting strict-then-loose JSON decode. Previously the loose-decode fallback was dead code because `json.Decoder` had already exhausted the body, and an oversize POST could OOM the process.
- Coinbase WS reconnect backoff gains full jitter (±30% of the current delay). Prevents synchronized reconnect storms when many clients see the same disconnect event (e.g. a regional WS outage).
- URL-escape user-controlled symbol strings before interpolating them into HTTP endpoints in `provider/yahoo`, `provider/coinbase`, and `news/edgar`. Previously a `/` or `&` in a symbol could inject path segments or extra query parameters; harmless when symbols come from the user's own config but unsound when symbols arrive from MCP/webhook callers.
- `cmd/dashboard.go`: extracted the five copy-pasted background polling goroutines (macro, futures, DeFi, equity, news) into a single shared `poll(ctx, interval, fetch)` helper. Each call site shrinks from ~20 lines of `NewTicker + initial fetch + select loop` boilerplate to one `go poll(...)` call. New `cmd/poll_test.go` covers the at-least-once-on-startup and cancel semantics.
- `internal/config/config_test.go`: first tests for the config package — `ParseTime` (4 layouts + empty + garbage), `PollDuration` (valid + invalid + default fallback), `AddSymbol` / `RemoveSymbol` (presence + duplicate + absent), `Load` (creates dir with 0o700 + writes defaults on missing file), `Save` round-trip (Watchlist / PollInterval / SparklineLen / Theme / WebhookURL / NtfyTopic / EDGARTickers / Portfolios / Alerts), and a guard that empty optional secrets are omitted from the persisted YAML. 90.6% statement coverage on a previously-untested package.
- `internal/tui/alertdialog/model_test.go`: first tests for the new-alert modal state machine — `Open` initialization, esc-closes-dialog, arrow-keys-wraparound on the condition picker, enter advancing through the (condition → value → confirm) steps, MACD-cross skipping the value step, digit + backspace handling in the value buffer, confirm-saves-rule + bad-value-stays-open, and default period (RSI=14, SMA cross=20) wiring for indicator conditions. 58.4% statement coverage (remainder is the View renderer).
- `internal/observe` — tiny dependency-free counter registry. Counters self-register via `observe.NewCounter(name)`; `/metrics` walks the registry and emits each as a Prometheus `counter` in lex-sorted name order. Atomic-safe for concurrent provider goroutines. First counters wired: `mkt_provider_yahoo_batch_failures_total`, `mkt_provider_yahoo_session_init_failures_total`, `mkt_provider_coinbase_ws_reconnects_total`. Operators running `mkt --listen :9999` can now see provider health from outside the process instead of just stderr.
- Split `internal/tui/chart/model.go` (1534 lines, 26 functions) along its natural responsibility seams: `model.go` (835 lines) keeps Model + Update + View + main candlestick / line render orchestration + hover/crosshair + the low-level grid renderer; `chart/overlays.go` (179) holds the on-chart indicator helpers (`drawOverlays`, `drawPatternMarkers`, `drawVolumeProfileGutter`, `extractHL`); `chart/subpanels.go` (550) holds the six sub-panel renderers (`renderRSI`, `renderMACD`, `renderADX`, `renderATR`, `renderStoch`, `renderOBV`). Identical behavior — same-package file split with no API or rendering change.

### Changed
- MCP server (`mkt mcp`) expanded to full read-only spec compliance: proper initialize handshake (capabilities for tools/resources/prompts/logging), ping, notifications/initialized + cancelled + progress (silently consumed), logging/setLevel ack, resources/list + read (`mkt://config`, `mkt://watchlist`, `mkt://portfolios`), prompts/list + get (`analyze_symbol`, `portfolio_review`).
- Coinbase order book in the detail panel updates live via the level2 WebSocket channel instead of just the REST snapshot. New `coinbase.StreamOrderBook` opens a per-product WS, applies snapshot + l2update events to an in-memory book, and emits throttled snapshots (every 250ms). Detail panel starts/stops the streamer when the symbol changes or the panel closes.
- `market.Hub` now dispatches `onQuote` on a dedicated goroutine behind a 256-slot buffer; quotes drop when the TUI stalls rather than blocking providers.
- Yahoo session init failures are now logged instead of silently discarded.
- Config directory is created with `0o700` permissions; holdings and alert rules were previously world-readable.
- `alert.Notify` replaced by a `Notifier` interface; `Engine.AddNotifier` registers destinations and `Engine.Check` dispatches each trigger after releasing the lock, with per-call timeouts and error isolation so one failing destination cannot block siblings.
- Heatmap mouse: clicking a sector tile in the overview selects it (and a second click on a selected sector drills in); clicking a ticker tile inside the drill-down view opens its full-screen chart. Coordinates resolve against the actual `tileW × tileH` grid so the hit region matches what's drawn on screen.

### Fixed
- `alert.Engine.Check` now takes the write lock; it mutates `refPrices` and `cooldowns`, which `RLock` did not protect.
- Yahoo history requests for the `4h` interval now fall back to `1h` candles instead of silently returning daily data.
- Coinbase history requests for `4h` / `1w` now send supported granularities (`3600` / `86400`); previously they sent `14400` / `604800`, which the Coinbase candles API rejects.
- Modal overlays (symbol info, alert dialog) now composite over the live tab content via `lipgloss.Compositor` instead of replacing the screen with `lipgloss.Place` on a blank canvas.

