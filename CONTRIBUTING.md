# Contributing

Thanks for looking at `mkt`. This is a single-binary Go TUI, so the contribution loop is short.

## Getting started

Prereqs:

- Go 1.25+ (see `go.mod`)
- [Task](https://taskfile.dev) — `brew install go-task` or `go install github.com/go-task/task/v3/cmd/task@latest`
- [golangci-lint](https://golangci-lint.run) for local lint runs

```sh
git clone https://github.com/stxkxs/mkt.git
cd mkt
task build      # → ./mkt
task test       # go test ./...
task lint       # golangci-lint run ./...
```

## Code layout

See the architecture section in `README.md` and `CLAUDE.md`. Briefly:

- Providers (`internal/provider/`) implement `QuoteProvider` / `HistoryProvider` — no TUI coupling.
- `market.Hub` aggregates providers and fans quotes out to the cache, alert engine, and TUI.
- Each tab under `internal/tui/` is its own package with `Model`, `Update`, `View`.
- Theme changes broadcast `theme.ChangedMsg`; views rebuild cached styles in their own `Update`.

## Adding a new tab

1. Create `internal/tui/<name>/model.go` with `New` and a `Model` carrying the shape the root binds: `SetSize(w, h int)`, `Update(tea.Msg) (Model, tea.Cmd)`, `View() string`. `Update` returns your own concrete type — `internal/tui/tabs.go` adapts it, so the package keeps value semantics.
2. In `Update`, handle `theme.ChangedMsg` by calling your local `RebuildStyles()`. Every tab is in the theme fan-out.
3. Name the tab in `internal/tui/keys.go`: a `Tab` constant and a `tabNames` entry. The digit key that selects it follows from its position.
4. Add the typed field to `App` in `internal/tui/app.go` and construct it in `NewApp`. The field is what code needing concrete access reaches for (`a.watchlist.CurrentPrice`, `a.alerts.TriggeredCount`).
5. Add one `tabEntry` to `bindSurfaces` in `internal/tui/tabs.go` — `bind(&a.<name>)`, plus `fetches: true` if the tab loads anything. Sizing, key and mouse forwarding, the theme fan-out and rendering all read that entry. `TestEveryTabIsBound` fails if a named tab has no entry.
6. Add its keys to `tabBindings` in `internal/tui/help/model.go`, or the tab ships with an empty help card.

`fetches: true` is the one step with no compile error behind it: results arrive as message types the tab's own package keeps private, so the root offers unclaimed messages to the fetching surfaces and nothing else. A fetching tab left unmarked renders "Loading…" for the rest of the session.

A tab that departs from the common path says so in its entry rather than by being left out of a switch: `signpost` renders in place of the model's view for a tab whose model owns the whole frame (Chart, reached with `c` from the watchlist), and `noMouse` withholds clicks and wheel events from a tab with no content under the pointer.

## Adding a provider

1. Implement `provider.QuoteProvider` (and optionally `HistoryProvider`) in `internal/provider/<name>/`.
2. `Supports(symbol)` is the routing hook — the hub picks the first supporting provider for each symbol.
3. Wire it in `cmd/backend.go`, the single point where the hub is constructed for `mkt`, `mkt serve` and `mkt daemon` alike.

## Tests

- Indicator math, provider parsing, hub concurrency, and alert logic all deserve unit tests.
- `go test -race ./...` must pass; CI enforces it.
- Keep tests hermetic — no network, no disk beyond `t.TempDir()`.

## Pull requests

- Run `task lint` and `task test` locally first.
- Keep commits focused; commit messages should explain the *why*, not just the *what*.
- Update `CHANGELOG.md` under `## Unreleased`.
