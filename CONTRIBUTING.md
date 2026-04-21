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

1. Create `internal/tui/<name>/model.go` with `Model`, `New`, `Update`, `View`.
2. In `Update`, handle `theme.ChangedMsg` by calling your local `RebuildStyles()`.
3. Register the tab in `internal/tui/keys.go` (constant + name) and `internal/tui/app.go` (field, wiring, routing, forwarding in the `theme.ChangedMsg` case).

## Adding a provider

1. Implement `provider.QuoteProvider` (and optionally `HistoryProvider`) in `internal/provider/<name>/`.
2. `Supports(symbol)` is the routing hook — the hub picks the first supporting provider for each symbol.
3. Wire it in `cmd/dashboard.go` when constructing the hub.

## Tests

- Indicator math, provider parsing, hub concurrency, and alert logic all deserve unit tests.
- `go test -race ./...` must pass; CI enforces it.
- Keep tests hermetic — no network, no disk beyond `t.TempDir()`.

## Pull requests

- Run `task lint` and `task test` locally first.
- Keep commits focused; commit messages should explain the *why*, not just the *what*.
- Update `CHANGELOG.md` under `## Unreleased`.
