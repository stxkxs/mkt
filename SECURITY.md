# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/stxkxs/mkt/security/advisories/new)
for this repository. Do not open a public issue for a security problem.

A useful report names the version (`mkt version`), the surface involved
(`--listen`, `mkt serve`, `mkt mcp`, a provider or notifier, the config
file), and the steps or input that reproduce it.

## Supported versions

Fixes are made on `main` and ship in the next tagged release. There are no
maintenance branches, so upgrading to the newest release is how a fix is
taken.

## Scope

What `mkt` defends, against whom, and what it leaves to the operator is
written down in [docs/threat-model.md](docs/threat-model.md). A report
that shows one of the controls listed there failing is in scope, as is
any path by which untrusted input reaches a terminal, a notifier, the
config file or an MCP client without passing through those controls.

## Verifying a release

Every release publishes a `checksums.txt` signed keyless with Sigstore and
a per-archive SBOM. The README's install section has the `cosign
verify-blob` command.
