---
status: accepted
implementation_status: implemented
implemented: 2026-06-14
---

# Rename calm-bridge to stack-fitness-functions, split bridge into client/server/fitness

The binary `calm-bridge` overloaded one name across two roles (the commit-time client check and the authoritative governance daemon), forcing readers to ask "which side of the bridge am I on?" We are renaming the product and binary to `stack-fitness-functions`, restructuring the CLI into `client validate`, `server start`, and a top-level `baseline`, and splitting the internal `internal/bridge` package (~7,900 lines) into `internal/client`, `internal/server`, and a side-neutral `internal/fitness` that owns the wire contract (`ValidationRequest`, `ValidationResult`, `Violation`). CALM is preserved everywhere it names the FINOS standard (`.calm/config.json`, `configs/`, the FINOS `calm` CLI, the `calm-poc` module path) — only the product name changes.

## Considered Options

- **One atomic rename PR** — never leaves the repo half-renamed, but is effectively unreviewable at several thousand lines.
- **Sequenced stacked PRs behind an epic (chosen)** — (1) extract `internal/fitness`, (2) rename `bridge`→`server`, (3) extract `internal/client` from `cmd/.../main.go`, (4) CLI restructure, (5) env-var rename + bin-script rename + embed hooks via `go:embed`, (6) docs. The repo compiles and tests pass at each step.
- **CLI/env/docs only, defer the package split** — fast, but leaves the `bridge` confusion the rename exists to remove.

## Consequences

- A pure rename cannot fit the project's <300-line PR guideline as a single change; sequencing honors the spirit (small, verifiable increments) even though the total diff is large.
- Env vars take the derived prefix `STACK_FITNESS_FUNCTIONS_*`; no abbreviations are introduced (the binary, env vars, and bin helpers all spell the product out in full).
- `client install-hooks` becomes a binary subcommand with hook scripts embedded via `go:embed`, removing the runtime dependency on a co-located source checkout. Installed hooks invoke `stack-fitness-functions client validate` on `PATH`; an env override is retained solely as a test seam.
- The old command names (`check`, `serve`) are **not** aliased — this is a hard cutover. The caller population is small and enumerable (the `graft`, `ringstation`, `slackstatus` hooks, CI, and `docker-compose.yml`), so the rollout (PR step 5) re-runs `client install-hooks` against the governed repos rather than carrying a deprecation cycle. No zombie command names survive.
- The `cmd/calm-bridge/` directory is renamed to `cmd/stack-fitness-functions/` so `go build ./cmd/...` yields the correctly named binary. The Go module path (`github.com/poconnor/calm-poc`) and repo name are **deliberately left unchanged for now** — that migration (push to a new repo, reclone) is deferred and tracked separately.

## Implementation Status

Accepted and implemented. The active product, binary, command tree, environment variables, helper scripts, container path, hook installation path, and documentation now use `stack-fitness-functions`; remaining `calm-bridge` references in this ADR are historical evidence for the rename decision.
