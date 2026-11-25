# Threat model

`mkt` runs as the local user, reads public market data, and holds two
kinds of data worth protecting: the user's holdings and alert rules, and
the notifier credentials in `config.yaml` (webhook URLs, an ntfy topic, a
Pushover token). This document lists each surface that accepts input from
outside the process, what an attacker on that surface could want, and the
control that stands in the way. Code paths are named so each claim can be
checked against the tree.

## Assets

| Asset | Where it lives | Why it matters |
|---|---|---|
| Holdings, cost basis, P&L | `config.yaml`, the equity-curve file | Private financial position |
| Notifier credentials | `config.yaml` | A Slack/Discord webhook path or Pushover token is a bearer credential |
| Alert rules and history | `config.yaml`, the alert history file | Rewriting them silences or forges alerts |
| The user's terminal | Every rendered frame | Escape sequences can rewrite what is shown or drive the terminal |
| The host | The `mkt` process | A remote-triggered process spawn is code execution |

## Surfaces

### Read API (`--listen`)

Attacker: anyone who can reach the bound address.

- A non-loopback bind without a token refuses to start; `--enable-webhook`
  requires a token on any address; `--require-token` extends the
  requirement to loopback, which is not a trust boundary on a shared host
  (`cmd/listen.go`, `listenGate`).
- The token is accepted only as `Authorization: Bearer`, never in a query
  string, and compared as SHA-256 digests in constant time
  (`internal/api/auth.go`).
- Every route except the webhook is read-only; other methods get 405.
  `/healthz` and `/readyz` answer without a token and disclose only
  liveness and whether any quote has arrived.
- `/alerts` omits notification destinations.
- The webhook caps the body (`maxWebhookBytes`) and each field's length,
  rejects control characters, and is rate-limited (`webhookRatePerSec`,
  `webhookBurst`), so it cannot drive unbounded notifications
  (`internal/api/server.go`).

### SSH dashboard (`mkt serve`)

Attacker: any SSH client that can reach the port; a guest holding an
authorized key.

- The server binds `127.0.0.1:2222` unless told otherwise, and refuses to
  start with an empty allowlist.
- A key is admitted only if it equals one on the allowlist. OpenSSH key
  options (`from=`, `command=`, `restrict`, …) are not enforced, so a key
  carrying any is never admitted (`cmd/serve_auth.go`).
- Sessions without a PTY are rejected; idle sessions are closed
  (`serveIdleTimeout`).
- A guest cannot open a browser on the host (`news.SetOpenDisabled`),
  cannot change the process-wide theme, and does not trigger desktop
  notifications unless the operator enables them.
- Alert edits from a guest are not written to the host's config unless
  the operator passes `--persist-alerts`. Guests share one alert engine,
  so a guest's edit is visible to every session for the life of the
  process; that is the shared-dashboard design, not an isolation boundary.

### MCP server (`mkt mcp`)

Attacker: a prompt-injected agent driving the MCP client.

- Every tool is read-only and annotated as such; nothing places orders or
  changes configuration.
- `get_portfolio`, `mkt://portfolios` and `mkt://config` are absent unless
  the operator passes `--expose-portfolio` / `--expose-config`, and the
  config resource always has every secret and webhook destination removed
  (`cmd/mcp.go`, `filterMCPSurfaces`).
- Each `tools/call` runs under a deadline (`mcp.DefaultToolTimeout`), so a
  stalled upstream cannot hold the session.

### Upstream data (providers, RSS, SEC EDGAR)

Attacker: a compromised or malicious upstream, or anyone on the path for a
plain-HTTP feed the user configures.

- Text that originates upstream (headlines, error bodies) passes through
  `textsafe.Clean` before it reaches a frame, the history file or an MCP
  result: control characters, ANSI/OSC sequences and bidi overrides are
  removed (`internal/textsafe`, `internal/httpx.ErrorSnippet`).
- A headline link opens only if it is `http(s)`, and is passed to the
  platform handler as a single argv element with no shell
  (`internal/news/open.go`).
- Provider and feed responses fetched through `internal/httpx` are capped
  at `httpx.MaxResponseBytes`.

### Notifier destinations

Attacker: anyone who can read `mkt`'s logs or error output.

- Errors and log lines name a webhook only as `scheme://host/…`; the path,
  which is the credential for Slack and Discord, is never printed
  (`internal/alert/redact.go`).
- The desktop notifier passes alert text to the platform helper as argv,
  never through a shell (`internal/alert/notify_args.go`).
- Each notifier is rate-limited, so an alert storm cannot exhaust a paid
  quota.

### Config file

Attacker: another local user; an interrupted write.

- `config.yaml` is written `0600` inside a `0700` directory, and looser
  modes are tightened on load.
- Every write is atomic, preceded by a timestamped backup, and refused
  while the file on disk does not parse (`internal/config/save.go`).

## Supply chain

- GitHub Actions are pinned by commit SHA; Renovate proposes updates after
  a settle period.
- CI runs `govulncheck`, which fails on any known vulnerability reachable
  from called code.
- Releases are built by goreleaser from a tag that has passed the full CI
  suite; `checksums.txt` is signed keyless with Sigstore and each archive
  ships an SBOM.
- CI requires every file named in `scripts/coverage-gate.sh` (the controls
  above) to be fully covered by tests.

## Outside the model

- **Secrets at rest are plaintext.** Notifier credentials are stored in
  `config.yaml`, protected only by file mode. Anyone who can read the
  user's files can read them. The listen token alone can be kept out of
  the file (`--listen-token-file`, `MKT_LISTEN_TOKEN`).
- **The local user is trusted.** Paths the user names (config, token file,
  recordings, imports) are read without a privilege check, because there
  is no privilege boundary between the user and the process.
- **Market data is not authenticated.** Prices come from public endpoints
  over TLS; `mkt` does not verify them against a second source.
