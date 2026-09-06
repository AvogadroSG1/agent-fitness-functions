# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->

## Build & Test

Use the repo-local Go caches so builds work in sandboxed environments:

```bash
# Quality gate (run before ending a session with code changes)
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test . ./configs ./cmd/agent-fitness-functions ./internal/server

# Full suite
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./...

# Single test
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test -run TestName ./internal/server

# Build the binary
go build ./cmd/agent-fitness-functions

# Container image (requires Docker locally or in CI)
docker build --build-arg GIT_SHA="$(git rev-parse --short HEAD)" --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" -t agent-fitness-functions:local .
```

### External tool dependencies for tests

Several tests shell out to external tools. Integration tests (`*_integration_test.go`) skip themselves when `calm`, `radon`, or `dotnet` are missing, but some regular tests in `./hooks`, `./fixtures`, and `./internal/server` still require the FINOS `calm` CLI on `PATH`. A 503 response with body `running CALM validation` in a test failure means the `calm` CLI is missing from the environment — not a code bug.

- FINOS CALM CLI 1.40.0: `npm install -g @finos/calm-cli@1.40.0`
- `radon` 6.0.1 (Python analysis), .NET 8 SDK (C# analysis via `tools/roslyn-analyzer`)
- `pyyaml` for hook violation formatting: `python3 -m pip install -r hooks/requirements.txt`

### Other common commands

```bash
make install                         # build + atomically install into ~/.local/bin (PREFIX overrides)
agent-fitness-functions client onboard   # govern a repo: wizard on a TTY, ensures a current local daemon
agent-fitness-functions doctor       # ordered readiness checks, each with a remediation
go run ./cmd/agent-fitness-functions baseline --repo /path/to/repo --language csharp --output baseline-report.json
```

Container/remote path only — **not** part of local development since ADR-0010:

```bash
scripts/generate-dev-certs.sh        # dev TLS + mTLS client certs into the machine governance root (ADR-0007)
docker compose config --quiet && docker compose up --build   # verify/run the service container
```

Helper scripts live in `bin/` (`agent-fitness-functions-serve`, `agent-fitness-functions-test`); add `bin/` to `PATH`.

### Local governance transport (ADR-0010)

The machine-local daemon serves **plain HTTP on loopback** (`http://127.0.0.1:7890`), and any loopback peer is the implicit caller `local` — an admin, authorized for every repo. Local development generates no certificates and writes no `caller-repos.json` entry. mTLS, `caller-repos.json`, `scripts/generate-dev-certs.sh`, and `--certificates-only` are the remote/container path. Do not document `docker compose up --build` or `https://127.0.0.1:7890` as a local remediation.

## Architecture Overview

`agent-fitness-functions` is the API boundary for Architecture Fitness Function checks: a single Go binary (module path `github.com/AvogadroSG1/agent-fitness-functions` — the legacy name is retained deliberately) with three roles selected by subcommand:

- `client validate` — sends one file's content to the server, prints the verdict. Invoked by Git hooks at commit time. `client install-hooks` installs the embedded hooks into a governed repo; `client onboard` is the full 0-to-governed flow (interactive wizard on a TTY) and the only command that restarts a stale daemon.
- `server start` — the authoritative governance HTTP daemon (`POST /check`, `GET /state`, `GET /preflight`, `GET /functions`, `POST /register`, `GET /configs`, `POST /shutdown`, plus unauthenticated `GET /health`). Two listen modes via `--listen-mode`: `mtls` (default; HTTPS + client certs + `caller-repos.json`) and `local-http` (ADR-0010; plain HTTP, loopback bind only, implicit `local` caller). The containerized `mtls` service is the **primary production path**; default command is `/app/agent-fitness-functions server start --addr 0.0.0.0:7890`.
- `baseline` — offline calibration; bulk-analyzes a repository to derive thresholds. Belongs to neither client nor server.

### Request flow

Hook (`hooks/pre-commit.sh` or `hooks/pre-tool-use.sh`) → `client validate` → server `POST /check` → per-repo config resolved from `configs/<repo>/config.json` → language analyzer computes metrics → generated architecture doc validated by the FINOS `calm` CLI against `patterns/governance.json` → `ValidationResult` (pass / advisory / block). The transport depends on the daemon's listen mode: plain HTTP with an implicit loopback caller locally, HTTPS + mTLS with a CN authorized in `caller-repos.json` against the container. `client validate` falls back between loopback schemes, so hooks written for either generation keep working. A request may set `dry_run`: the verdict is computed identically but never persists into the repository's outstanding-violation state — the agent Edit/Write pre-write hook and `doctor`'s probe are dry runs because their proposals may never land; the commit-path hooks are deliberately wet. All nine fitness functions are intra-file — the server never touches the repository on disk. Five are metric-scored (cyclomatic complexity, interface width, implementation depth, logic density, dependency discipline): a language analyzer computes a number against a calibrated threshold. Four are generalized, config-driven, opt-in functions that all enforce a fixed zero-occurrences rule rather than a calibrated threshold; they differ only in scoring mechanism: `layer-sovereignty` and `deterministic-ordering` are checker content-scored against the proposed file's raw text (`internal/server/content_scoring.go`, syntax-blind but still gated on a supported language), while `temporal-purity` and `sql-composition-safety` are scored from analyzer findings (implemented in the Python AST scan today). See `docs/spec/engineering-spec.md` §5.4 for the full breakdown.

### Package layout

- `internal/fitness` — the shared wire contract (`ValidationRequest`, `ValidationResult`, `Violation`). Both client and server depend on it; it depends on neither. Do not introduce names like CheckRequest/CheckResponse.
- `internal/server` — HTTP daemon, `Checker`, `ConfigStore` (fsnotify-watched mounted configs), listen-mode selection (`server.go`), auth (`auth.go`: mTLS CN against `caller-repos.json`, or implicit loopback caller in local-http), rate limiting.
- `internal/client` — validate/install-hooks/onboard/doctor commands, local daemon auto-start, the onboard wizard (`wizard.go`, `picker.go`), daemon staleness verdicts (`daemonstatus.go`), the graceful restart engine (`restart.go`), and pre-ADR-0007 legacy migration (`legacymigration.go`).
- `internal/buildinfo` — the build revision and modified flag the daemon reports on `/health` and the client compares against.
- `internal/analyzer` — per-language analyzers: Go (native + gocyclo), Python (shells to `radon`), C# (shells to the Roslyn CLI in `tools/roslyn-analyzer`); plus repository-wide baseline scanning.
- `internal/calm` — thin wrapper that shells out to `calm validate`.
- `internal/report` / `internal/sarif` — architecture document generation and SARIF output.
- `patterns/governance.json` — CALM pattern holding the calibrated thresholds, embedded via `patterns/embed.go`.
- `internal/govconfig` — the config shape and its defaults (`Default()`: the five metric functions on, the four opt-in ones off). The single source of truth for the nine-function catalog; the picker, `/functions`, and the scaffolder all read it.
- `configs/<repo>/config.json` — per-repo governance (enforcement-mode, enabled functions) mounted into the container; this is the source of truth. A repo-local `.calm/config.json` is a developer sandbox only and never affects container governance. For LOCAL development, `client onboard` copies the tracked repo-local config into the machine governance root (`${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/governance/` — one configs dir per machine, ADR-0007) so a single auto-started daemon on `http://127.0.0.1:7890` serves every governed repo on the machine. Under ADR-0010 that root holds no certificates and no `caller-repos.json` for local governance; both remain there only for the remote/container path.
- `fixtures/green/` and `fixtures/violations/` — calibrated fixture files that must pass/fail specific fitness functions (enforced by `fixtures/fixtures_test.go`).
- Root-level tests (`bin_helpers_test.go`, `bin_helper_mtls_test.go`, `docker_contract_test.go`) lock the helper-script and Docker image contracts.

### Self-governance hook

`.claude/settings.json` registers a `PreToolUse` hook that runs `hooks/pre-tool-use.sh` on Edit/Write — this repo validates its own edits against a local agent-fitness-functions server when one is running. If the hook blocks an edit, read the reported violation rather than working around the hook.

The hook resolves the binary from `PATH` (`hooks/pre-tool-use.sh` defaults `AGENT_FITNESS_FUNCTIONS_BIN` to `agent-fitness-functions`). On a fresh clone, install it once before the hook can run:

```bash
go build -o ~/.local/bin/agent-fitness-functions ./cmd/agent-fitness-functions   # ensure ~/.local/bin is on PATH
```

MUST NOT pin `AGENT_FITNESS_FUNCTIONS_BIN` to a build-output path in the tracked settings file, and MUST NOT write absolute paths there — `.tmp/` is gitignored and absolute paths do not survive a clone. `self_governance_hook_test.go` enforces both. Machine-specific hook entries generated by `client install-hooks` belong in the untracked `.claude/settings.local.json`.

The tracked entry still sets `AGENT_FITNESS_FUNCTIONS_ADDR=https://127.0.0.1:7890`. It works against the ADR-0010 local-http daemon because `client validate` falls back between loopback schemes, at the cost of one failed HTTPS probe per edit. Same root cause as the embedded hook scripts' default; tracked as `calm-poc-mzkx`.

This repository governs itself under the key `agent-fitness-functions` (ADR-0009), resolved from `configs/agent-fitness-functions/config.json`.

## Naming Surface

- Product and binary: `agent-fitness-functions` — always spelled out in full, no abbreviations.
- Commands: `agent-fitness-functions client validate`, `agent-fitness-functions server start`, `agent-fitness-functions baseline` (not "check"/"serve").
- Environment variables: `AGENT_FITNESS_FUNCTIONS_*`.
- Helper scripts: `agent-fitness-functions-serve` and `agent-fitness-functions-test`.
- FINOS CALM, `.calm/config.json`, `configs/`, and the FINOS `calm` CLI retain their names. The source repository and Go module are `github.com/AvogadroSG1/agent-fitness-functions`, and since ADR-0009 the logical governance key is `agent-fitness-functions` too. CALM is the external standard being enforced, never the product name.

## Key Documentation

- `CONTEXT.md` — vocabulary, how validation works end to end, CALM vs. linter rationale
- `docs/spec/why-and-what.md` and `docs/spec/engineering-spec.md` — product and engineering specification
- `docs/quickstart-0-to-governed.md` — the developer happy path: `make install`, `client onboard`, done
- `docs/runbooks/onboard-new-repository.md` — authoritative operator reference: local (cert-free) path, self-service remote registration, manual/air-gapped production path, daemon restarts, error-kind table
- `docs/adr/0007-machine-scoped-shared-governance-state.md` — one governance root and one daemon per machine
- `docs/adr/0010-plain-http-local-governance.md` — why local governance has no certificates, and what stayed mTLS
- `docs/threshold-calibration.md` / `docs/threshold-exceptions.md` — how thresholds were derived
