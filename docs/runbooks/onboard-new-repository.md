# Onboarding a New Repository

This runbook is the authoritative operator reference for onboarding a repository to
the `agent-fitness-functions` governance system. It covers both paths:

- **Local / developer path** — one command, `agent-fitness-functions client onboard`,
  which provisions everything for a locally governed repo. For the hurried version see
  the [5-minute quickstart](../quickstart-0-to-governed.md).
- **Production path** — the server-side steps that remain: getting the per-repo config
  and the caller authorization into the shared container deployment, then redeploying.

The tooling now automates what this runbook previously walked through by hand
(config scaffolding, caller-authorization edits, hook installation, cert generation,
daemon start). What remains genuinely manual is the production redeploy.

## Mental model

Onboarding is still two distinct acts:

1. **Server-side governance** — the authoritative config the container serves for this
   repo, and the caller identity authorized to check it.
2. **Client-side wiring** — the Git and agent hooks installed into the target repo,
   pointed at the server.

```
┌─────────────────────────────────────────────┐
│  Container (authoritative)                    │
│  configs/<repo>/config.json  ← governance     │
│  caller-repos.json           ← authorization  │
└──────────────┬────────────────────────────────┘
               │ HTTPS + mTLS, --repo=<name>
       ┌───────┴────────┐
       │                │
  pre-commit.sh    pre-tool-use.sh
  (developer git)  (AI agent Edit/Write hook)
```

For **local** development both acts happen on your machine and `client onboard`
performs all of them. For **production** the server-side act happens in the container
deployment; the client-side act is `client install-hooks` in the governed repo with the
remote-mode environment variables set (`client onboard` is the local convenience that
also auto-starts a daemon and generates dev certs — neither is wanted against a shared
container).

**Critical:** the server resolves config **by repository name**, not by inspecting the
repo's contents (`internal/server/config.go` `loadConfig`). The hook sends `--repo
<name>`; the server looks up `configs/<name>/config.json`. A repo with no matching
config directory receives a not-found error (surfaced as the `not_configured`
error-kind — see the table below). The config MUST exist server-side before
client-side wiring means anything.

A local `.calm/config.json` is a developer sandbox only and **cannot weaken** container
governance. When the hook connects to the container, governance is resolved exclusively
from the mounted `configs/<repo>/config.json`.

## Prerequisites

- The repository name MUST match `^[a-z][a-z0-9_-]{0,63}$` (lowercase letter first,
  then lowercase alphanumerics, hyphens, or underscores; max 64 chars). `client
  onboard` validates this and, when the working-tree basename is not a valid name, tells
  you to pass `--repo <name>`.
- The `agent-fitness-functions` binary on `PATH`.
- For production: write access to the deployment's `configs/` and `caller-repos.json`
  artifacts, and mTLS client credentials issued for an authorized caller CN.

### Getting the binary on `PATH` — installer (recommended)

Per ADR-0005, the supported way to get a working `agent-fitness-functions` without a
source checkout is the release installer:

```bash
scripts/install.sh --archive agent-fitness-functions-<version>-darwin-arm64.tar.gz \
  --checksums SHA256SUMS --provision-runtimes
```

- `--provision-runtimes` provisions the pinned managed CALM CLI and Python
  (radon/pyyaml) runtimes the analyzers need, into product-owned state under
  `$XDG_STATE_HOME/agent-fitness-functions/` (default `~/.local/state`) — no manual
  `npm install -g` or `pip install` required. It still requires a host `node`/`npm` and
  `python3` as documented (not product-managed) bootstrap prerequisites.
- Add `$XDG_STATE_HOME/agent-fitness-functions/current/bin` to `PATH` once install.sh
  reports it.
- `agent-fitness-functions runtime doctor` verifies the managed CALM/Python runtimes
  are present, pinned, and healthy — run it any time after installing, and after
  `upgrade`/`rollback`.
- `agent-fitness-functions upgrade --archive <archive> --checksums <checksums>` installs
  a new version through the same verified atomic path, retaining exactly one
  predecessor; `agent-fitness-functions rollback` repoints `current` back at that
  predecessor (reconciling the managed runtime pointers too); `agent-fitness-functions
  uninstall --yes` removes all product-owned installer state.

### Source-checkout alternative (manual prerequisites)

If you are working from a source checkout instead of an installed release (e.g.
repository development), the manual prerequisite path still applies:

- `python3` with `pyyaml` for the hooks and violation formatter:
  `python3 -m pip install -r hooks/requirements.txt`.
- The FINOS `calm` CLI 1.40.0 on `PATH` for the server (`npm install -g
  @finos/calm-cli@1.40.0`).

## Local / developer path — one command

```bash
cd /path/to/<repo>
agent-fitness-functions client onboard
```

This runs the whole 0-to-governed sequence and gates on `doctor` at the end:

1. **Dev certificates** — generated in-process into `<repo>/certs` (no `openssl`,
   no `scripts/generate-dev-certs.sh` needed) with client CN `dev-hook-pool`.
   `AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR` overrides the location.
2. **Server-side config scaffold** — `<configs-dir>/<repo>/config.json` from the
   embedded template, with all five fitness functions enabled. The configs directory is
   `AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR` if set, else `<repo>/configs`. An existing
   config is left unchanged.
3. **Caller authorization** — merges CN `dev-hook-pool → <repo>` into
   `caller-repos.json` (a sibling of the `configs/` directory), preserving all other
   callers and admins.
4. **Hook installation** — `install-hooks` (see below).
5. **Local daemon auto-start** — a TLS daemon on `https://127.0.0.1:7890` (generating
   dev certs and pointing at the resolved configs directory); a healthy daemon is a
   no-op. The health wait is 5 seconds.
6. **`doctor` (final gate)** — the ordered checks below. Onboard fails (non-zero exit)
   if any non-advisory check fails.

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--repo <name>` | working-tree basename | Governance repo name (validated against the grammar) |
| `--enforcement <advisory\|block>` | `advisory` | Enforcement mode written into the scaffolded config |
| `--addr <url>` | `https://127.0.0.1:7890` | Governance daemon base URL |
| `[path]` | `.` | Repository path |

`onboard` is idempotent. When it finishes it prints the one remaining manual step for
production governance (copy the config + caller entry to the deployment, redeploy).

> **Enforcement default is `advisory`.** The scaffolded config starts in `advisory`
> (report, don't block) so a first onboarding never blocks day-one commits on latent
> debt. Pass `--enforcement block` for a greenfield repo you want fail-closed from the
> start, or flip the config to `block` once the team has cleared the backlog. For an
> existing codebase, prefer `baseline --emit-config` (below), which recommends the mode
> from the actual violation count.

### What `install-hooks` installs

`client onboard` calls `install-hooks`; you can also run it directly:

```bash
cd /path/to/<repo>
agent-fitness-functions client install-hooks
```

It installs and registers, with zero manual settings authoring:

- `.git/hooks/pre-commit` — validates staged files at commit time.
- `.git/hooks/pre-push` — push-time hook.
- `.git/hooks/agent-fitness-functions-git-guard` — blocks bypass commands
  (`--no-verify`, force-push, ff-only merges), registered as a Bash `PreToolUse` entry
  in `.claude/settings.json`.
- `.git/hooks/agent-fitness-functions-pre-tool-use` (+ `format-violations.py`) — the
  agent Edit/Write content-validation hook, registered as an `Edit|Write` `PreToolUse`
  entry in `.claude/settings.json`.

Both `.claude/settings.json` entries are upserted idempotently. If the repo already has
unrelated Git hooks the installer refuses to overwrite them; set
`AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1` (sidecar) or
`AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1` (replace).

## Production path — server-side onboarding

Production governance is served by the shared container. Two artifacts must reach that
deployment.

### Step 1 — Produce the per-repo config

For a greenfield repo, the config `onboard` scaffolded (or a hand-written one) is
enough. For an **existing** codebase, produce it from a baseline so the enforcement
mode is chosen from real data:

```bash
agent-fitness-functions baseline \
  --repo /path/to/<repo> --language <go|python|csharp> \
  --output baseline-report-<repo>.json \
  --emit-config configs/<repo>/config.json --name <repo>
```

`--emit-config` writes a ready-to-use `configs/<repo>/config.json` (all five functions
enabled) and prints an enforcement-mode recommendation — `block` when zero files
violate the current global thresholds, otherwise `advisory` — plus a threshold-delta
report. Thresholds remain **global** and compiled into `patterns/governance.json`; a
per-repo config toggles functions and enforcement mode but cannot change a threshold.
See [threshold-calibration.md](../threshold-calibration.md) for the full behavior and
the recalibration procedure.

#### Config schema

Defined by `Config` in `internal/server/config.go`:

| Field | Required | Values | Meaning |
|-------|----------|--------|---------|
| `enforcement-mode` | Yes | `block`, `advisory`, `off` | How active violations are routed. `block` rejects; `advisory` reports but allows; `off` disables checks. |
| `enforcement-on-error` | No (defaults to `block`) | `block`, `advisory`, `pass` | How analyzer failures are routed. `block` fails closed; `advisory` reports without blocking; `pass` suppresses failures. |
| `fitness-functions` | Yes | object of `string → bool` | Enables/disables individual checks. A missing key defaults to **enabled**. |
| `exclude-patterns` | No | array of glob strings | File paths to skip, e.g. `"*_test.go"`, `"fixtures/**"`. |

Fitness function keys: `cyclomatic-complexity`, `interface-width`,
`implementation-depth`, `logic-density`, `dependency-discipline`.

Example with exclude patterns:

```json
{
  "enforcement-mode": "advisory",
  "enforcement-on-error": "block",
  "fitness-functions": {
    "cyclomatic-complexity": true,
    "interface-width": true,
    "implementation-depth": true,
    "logic-density": true,
    "dependency-discipline": true
  },
  "exclude-patterns": ["*_test.go", "test_*.py", "fixtures/**"]
}
```

### Step 2 — Authorize the caller

The caller's certificate CN MUST be authorized for the repo name in
`caller-repos.json`. Add the repo to the relevant caller's list:

```json
{
  "callers": {
    "ci-runner-graft": ["graft"],
    "ci-runner-all": ["graft", "ringstation", "slackstatus"],
    "dev-hook-pool": ["calm-poc", "graft", "ringstation", "slackstatus", "<repo-name>"]
  },
  "admins": ["dev-hook-pool"]
}
```

- `dev-hook-pool` is the CN that `client onboard` / `scripts/generate-dev-certs.sh`
  issue developer hook certs for. Add the new repo here for local developer commits.
- Add a CI caller CN (e.g. `ci-runner-<repo-name>`) for the repo's pipeline.
- `admins` lists CNs allowed to call the admin-gated `GET /configs` endpoint.

Without a matching caller entry, an authenticated request is rejected (the
`unauthorized` error-kind) even when the config exists.

> Authorizing a new caller no longer breaks the test suite: `configs/config_test.go`
> no longer pins the exact `caller-repos.json` contents.

### Step 3 — Deploy the config to the server

- **Container (production):** `configs/` and `caller-repos.json` are mounted read-only
  (`docker-compose.yml`). Changes take effect by redeploying the service with the
  updated mounts/image.
- **Local sandbox server:** the server watches the configs directory with `fsnotify`
  (`internal/server/configstore.go`) and hot-reloads both repo configs and the caller
  policy. No restart is required.

The server itself must be started with TLS material. `server start` reads
`AGENT_FITNESS_FUNCTIONS_TLS_CERT`, `AGENT_FITNESS_FUNCTIONS_TLS_KEY`, and
`AGENT_FITNESS_FUNCTIONS_TLS_CA` (the `--tls-cert/--tls-key/--tls-ca` flags override
them). All three must be provided together or the server refuses to start — there is no
silent plain-HTTP fallback.

### Step 4 — Point the repo's hooks at the container

From inside the governed repo, run `client install-hooks` and export the remote-mode
variables so the hooks reach the container instead of a local daemon (use
`install-hooks`, not `onboard`, here — `onboard`'s daemon auto-start and dev-cert
generation are for the local path):

```bash
export AGENT_FITNESS_FUNCTIONS_ADDR=https://calm-governance.example:7890
export AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1
export AGENT_FITNESS_FUNCTIONS_CLIENT_CERT=/path/to/client.crt
export AGENT_FITNESS_FUNCTIONS_CLIENT_KEY=/path/to/client.key
export AGENT_FITNESS_FUNCTIONS_CLIENT_CA=/path/to/ca.crt
export AGENT_FITNESS_FUNCTIONS_REPO_NAME=<repo-name>
```

> `AGENT_FITNESS_FUNCTIONS_REPO_NAME` is the linchpin in remote mode: it becomes the
> `--repo` value the hooks send and MUST equal the `configs/<repo-name>` directory from
> Step 1. Without it the hooks fall back to the working-tree basename, which may not
> match the config directory name and will produce a `not_configured` error. When the
> `AGENT_FITNESS_FUNCTIONS_CLIENT_*` variables are unset, the client and hooks
> auto-discover credentials from `<repo>/certs`.

## Verification

Run `doctor` from inside the repo — it is the single verification entry point:

```bash
agent-fitness-functions doctor            # local
agent-fitness-functions doctor \
  --addr https://calm-governance.example:7890 --repo <repo-name>   # remote
```

`doctor` runs these ordered checks (offline checks first, so a stopped server never
hides a missing dependency), each with a `→` remediation on failure:

1. `binary` — the resolved executable and build revision.
2. `python3` / `pyyaml` — present and importable.
3. `client certificate` — resolved, parseable, CN, and not expired.
4. `server CA bundle` — resolved and contains PEM certificates.
5. `server reachable` — `GET /health` returns 200 over TLS.
6. `server authentication` — the authenticated `GET /preflight?repo=<name>` succeeds
   (401 here means the client certificate was not accepted).
7. `repo configured server-side` — from `/preflight` facts.
8. `caller authorized for repo` — from `/preflight` facts.
9. `enforcement mode` — from `/preflight` facts (advisory `⚠` when unknown).
10. `git pre-commit hook`, `git pre-push hook`, `agent git-guard hook` (required), and
    `agent Edit/Write hook (optional)` (advisory `⚠` if absent).

### `GET /preflight?repo=<name>` — the caller-facing "am I ready?" endpoint

Client-certificate authenticated, **not** admin-gated. It never 403s an unauthorized
caller or 404s an unconfigured repo — it reports those as JSON facts so `doctor` can
render them as distinct checks:

```json
{
  "authenticated_cn": "dev-hook-pool",
  "repo_configured": true,
  "repo_config_valid": true,
  "caller_authorized": true,
  "enforcement_mode": "advisory"
}
```

### `GET /configs` — the operator inventory endpoint

Admin-gated (the caller CN must be in `caller-repos.json` `admins`). Returns the loaded
config snapshot for every repo — status, enforcement mode, enabled functions, and any
parse error — for auditing what the running server actually serves:

```json
{
  "version": "...",
  "loaded_at": "2026-07-08T...",
  "repos": {
    "<repo-name>": {
      "status": "valid",
      "enforcement-mode": "advisory",
      "fitness-functions": { "cyclomatic-complexity": true }
    }
  }
}
```

### End-to-end smoke test

```bash
cd /path/to/<repo>
git add <clean-file>
git commit -m "verify fitness-function onboarding"
```

Expected: the pre-commit hook runs, prints nothing for clean files (or the
`block`/`advisory` verdict for violations), and the commit succeeds. To validate one
file directly against the server:

```bash
agent-fitness-functions client validate \
  --file <path> --repo <repo-name> --language <go|python|csharp>
```

## Troubleshooting — infrastructure error kinds

Infrastructure/setup failures are now distinct from architecture violations. `client
validate` exits **3** and prints a machine-readable object,
`{"status":"error","error_kind":"...","message":"...","remediation":"..."}`; the hooks
render it as a labeled `agent-fitness-functions SETUP problem ... (NOT an architecture
violation)` block. A real block is a *successful* check whose status is `block` (exit
0) — so the exit code alone distinguishes "fix your setup" from "fix your architecture."

| `error_kind` | Trigger | Meaning | Fix |
|--------------|---------|---------|-----|
| `server_unreachable` | dial refused / timeout / daemon auto-start failure | The governance server could not be reached at all | Run `agent-fitness-functions doctor`; the local daemon auto-starts on `client validate` when dev certs and a repo config exist. Its stdout/stderr are captured at `<repo>/certs/daemon.log` — check it when auto-start fails |
| `tls_failure` | TLS handshake / certificate-material error | The client and server did not agree on TLS | Run `doctor`; regenerate dev certs with `scripts/generate-dev-certs.sh --force` |
| `port_conflict` | TLS probe failure against a live listener on the shared default local port, in managed local mode | Another repository's local daemon — or a stale daemon from before certificate rotation — already owns the configured local port | Identify it with `lsof -i :7890` and stop it, or rerun with a distinct `--addr` |
| `unauthenticated` | HTTP 401 | The server rejected the client certificate | Run `doctor`; regenerate dev certs, or set `AGENT_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA` to a trusted pair |
| `unauthorized` | HTTP 403 | The certificate's CN is not authorized for this repo | Add the CN to `caller-repos.json` for the repo on the server, then redeploy |
| `not_configured` | HTTP 404 | No `configs/<repo>/config.json` on the server, or `--repo`/`AGENT_FITNESS_FUNCTIONS_REPO_NAME` does not match the config directory | Run `client onboard`, or create `configs/<repo>/config.json` on the server |
| `invalid_request` | HTTP 400 | The server rejected the request (e.g. malformed `--repo` name or unsupported `--language`) | Check `--repo` (grammar `^[a-z][a-z0-9_-]{0,63}$`) and `--language`; run `doctor` |
| `server_error` | other non-200 | The server could not produce a verdict | Check the server logs; retry, or run `doctor` |

### On-error policy

Infrastructure errors fail **closed** (block) by default. Set
`AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory` to let commits and agent edits proceed
despite a setup failure while you fix it — mirroring the server-side
`enforcement-on-error` config field. `block` is the default; `advisory` is the only
other accepted value at the hook layer.

### Other issues

- **`AGENT_FITNESS_FUNCTIONS_ADDR must be loopback`** — set
  `AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1` for a trusted remote server, and use an
  `https://` URL.
- **Hook installation refuses to overwrite** — inspect the existing hook, then use
  `AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1` (sidecar) or
  `AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1` (replace).
- **Config change not picked up (container)** — container mounts are read-only;
  redeploy the service. A locally running sandbox server hot-reloads via `fsnotify`.

## Quick reference

```bash
# Local: one command (advisory mode by default)
cd /path/to/<repo> && agent-fitness-functions client onboard

# Re-check anytime
agent-fitness-functions doctor

# Production: derive a config from a baseline, then deploy it
agent-fitness-functions baseline --repo /path/to/<repo> --language <go|python|csharp> \
  --output baseline-report-<repo>.json --emit-config configs/<repo>/config.json --name <repo>
#   1. copy configs/<repo>/config.json to the deployment
#   2. authorize the caller CN in caller-repos.json
#   3. redeploy the container (local sandbox hot-reloads)

# Point a repo's hooks at the container
export AGENT_FITNESS_FUNCTIONS_ADDR=https://calm-governance.example:7890
export AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1
export AGENT_FITNESS_FUNCTIONS_CLIENT_CERT=/path/to/client.crt
export AGENT_FITNESS_FUNCTIONS_CLIENT_KEY=/path/to/client.key
export AGENT_FITNESS_FUNCTIONS_CLIENT_CA=/path/to/ca.crt
export AGENT_FITNESS_FUNCTIONS_REPO_NAME=<repo-name>
cd /path/to/<repo> && agent-fitness-functions client install-hooks
```

See the [5-minute quickstart](../quickstart-0-to-governed.md) for the developer-facing
walkthrough.

*Authored By Peter O'Connor with Assistance from Claude Code (databricks-claude-opus-4-8[1m]) · 2026-07-08 · Onboarding a New Repository runbook*
