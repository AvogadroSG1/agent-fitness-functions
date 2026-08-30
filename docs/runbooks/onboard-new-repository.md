# Onboarding a New Repository

This runbook is the authoritative operator reference for onboarding a repository to
the `agent-fitness-functions` governance system. It covers three paths:

- **Local / developer path** — one command, `agent-fitness-functions client onboard`,
  which provisions everything for a locally governed repo. For the hurried version see
  the [5-minute quickstart](../quickstart-0-to-governed.md).
- **Self-service remote path** — the same `client onboard` command, run with external
  TLS material against a remote server, registers the repo server-side via `POST
  /register` in one authenticated call. See [Self-service path](#self-service-path)
  below.
- **Manual / air-gapped production path** — the server-side steps for when a live
  `POST /register` call isn't possible: getting the per-repo config and the caller
  authorization into the shared container deployment by hand, then redeploying.

The tooling now automates what this runbook previously walked through by hand
(config scaffolding, caller-authorization edits, hook installation, cert generation,
daemon start). What remains genuinely manual is the production redeploy on the
manual/air-gapped path below.

## Self-service path

The server now boots with **zero configs** — an empty `configs/` directory is a valid
steady state ("awaiting registration"), not a deployment error. `client onboard` works
from that empty state end to end:

- `client functions` (offline, reads the embedded governance pattern) or `GET
  /functions` (remote, unprivileged — any authenticated caller, no repo needs to exist
  yet) lists the five-function catalog: description, threshold, operator, unit.
- `--functions cyclomatic-complexity,logic-density` selects a subset instead of the
  all-five default; an interactive TTY without `--functions` gets a picker checklist
  over the same catalog instead of the silent default.
- **In external/remote TLS mode** (`AGENT_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA` set,
  `--addr` pointing at a server that is not a managed local dev daemon), `onboard`
  self-service registers the repo with `POST /register` instead of writing local
  `configs/<repo>/config.json` and `caller-repos.json` files — a remote server never
  sees those local files. Registration creates the config server-side and binds the
  caller's own certificate CN to the repo in one authenticated call; it is idempotent
  (replaying the same registration returns 200 with `created: false`). Registering a
  repo that already exists with a **different** configuration requires an admin CN
  (409 otherwise). Operators can disable self-service registration entirely with the
  kill switch `AGENT_FITNESS_FUNCTIONS_DISABLE_REGISTRATION=1`, falling back to the
  manual/air-gapped path below.
- **In managed/local mode** `onboard` keeps writing the local config and caller-binding
  files as before — the local daemon reads those files directly, so there is nothing to
  register remotely.

The manual production path described below remains for air-gapped or otherwise
disconnected deployments where a live `POST /register` call isn't possible.

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

`uninstall --yes` removes only installer-owned state (`versions/`, `current`,
`runtimes/`). The machine governance root survives it by design (ADR-0007); to
remove local governance state too, delete the `governance/` directory under
`${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/` yourself.

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

1. **Dev certificates** — generated in-process into the machine governance root
   (`${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/governance/certs`, ADR-0007; no
   `openssl`, no `scripts/generate-dev-certs.sh` needed) with client CN
   `dev-hook-pool`. One dev CA serves every governed repository on the machine.
   `AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR` overrides the location.
2. **Server-side config scaffold** — the tracked `<repo>/configs/<repo>/config.json`
   production handoff artifact from the embedded template, with all five fitness
   functions enabled (an existing config is left unchanged), then copied verbatim
   into the shared governance configs dir the local daemon serves
   (`AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR` if set, else `<govRoot>/configs`).
   Re-running onboard re-syncs a user-edited repo-local config; the repo-local file
   is the source of truth.
3. **Caller authorization** — merges CN `dev-hook-pool → <repo>` into the governance
   root's `caller-repos.json` (a sibling of its `configs/` directory), accumulating
   entries across every onboarded repository and preserving all other callers and
   admins.
4. **Hook installation** — `install-hooks` (see below).
5. **Local daemon auto-start** — a TLS daemon on `https://127.0.0.1:7890` (generating
   dev certs and pointing at the resolved configs directory); a healthy daemon is a
   no-op. The health wait is 5 seconds.
6. **Registration acknowledgement** — onboard polls `GET /preflight` (up to 5
   seconds) until the daemon reports the repo configured and the caller authorized,
   absorbing the fsnotify reload debounce so the doctor gate never races a live
   daemon that has not yet seen the files onboard just wrote.
7. **`doctor` (final gate)** — the ordered checks below. Onboard fails (non-zero exit)
   if any non-advisory check fails.

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--repo <name>` | working-tree basename | Governance repo name (validated against the grammar) |
| `--enforcement <advisory\|block>` | `advisory` | Enforcement mode written into the scaffolded config |
| `--addr <url>` | `https://127.0.0.1:7890` | Governance daemon base URL |
| `--functions <a,b,...>` | all five | Comma-separated subset of fitness functions to enable |
| `[path]` | `.` | Repository path |

`onboard` is idempotent. In managed/local mode, when it finishes it prints the one
remaining manual step for production governance (copy the config + caller entry to the
deployment, redeploy) — see the manual/air-gapped path below. In external/remote TLS
mode there is no remaining step: registration against `--addr` already happened via
`POST /register` (see [Self-service path](#self-service-path) above).

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

- `pre-commit` hook at the git-resolved hooks path (via `git rev-parse --git-path hooks/pre-commit`, which honors `core.hooksPath` when set — e.g., `.beads/hooks/pre-commit` in Beads-initialized repos, `.git/hooks/pre-commit` otherwise) — validates staged files at commit time.
- `pre-push` hook at the git-resolved hooks path (via `git rev-parse --git-path hooks/pre-push`) — push-time hook.
- `agent-fitness-functions-git-guard` at the git-resolved hooks path — blocks bypass commands
  (`--no-verify`, force-push, ff-only merges), registered as a Bash `PreToolUse` entry
  in `.claude/settings.json`.
- `agent-fitness-functions-pre-tool-use` (+ `format-violations.py`) at the git-resolved hooks path — the
  agent Edit/Write content-validation hook, registered as an `Edit|Write` `PreToolUse`
  entry in `.claude/settings.json`.

Both `.claude/settings.json` entries are upserted idempotently. If the repo already has
unrelated Git hooks the installer refuses to overwrite them; set
`AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1` (sidecar) or
`AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1` (replace).

The registered `.claude/settings.json` commands are machine-portable: each entry is
`"$(git rev-parse --git-path hooks/<name>)"`, which every machine resolves to its own
installed script, so committing `.claude/settings.json` never embeds one developer's
absolute paths. `doctor` verifies the resolved script actually exists on disk, so a
fresh clone that has the entries but not the scripts fails honestly instead of
false-passing.

### Portable vs per-machine artifacts

Onboarding produces two kinds of artifacts. Committing the portable ones is safe and
expected; the per-machine ones must be (re)created on every clone — which is exactly
what re-running `client onboard` (or `client install-hooks`) does:

| Artifact | Scope | Notes |
|----------|-------|-------|
| `<repo>/configs/<repo>/config.json` | **Portable (tracked)** | The production handoff source of truth; onboard syncs it into the machine governance root |
| `.claude/settings.json` PreToolUse entries | **Portable (tracked)** | Self-locating `git rev-parse --git-path` commands, no absolute paths |
| `.git/hooks/*` scripts (or `core.hooksPath` equivalents) | **Per-machine** | Inside `.git/`, never committed; installed by `install-hooks` |
| `<govRoot>/certs` dev CA + client/server certs | **Per-machine** | One CA per machine under `~/.local/state/agent-fitness-functions/governance/` |
| `<govRoot>/configs/` + `<govRoot>/caller-repos.json` | **Per-machine** | The local daemon's view; rebuilt by onboarding each repo once per machine |
| `.claude/settings.local.json` entries (forge repos) | **Per-machine** | forge gitignores this file; see below |

On a new computer: clone, then run `agent-fitness-functions client onboard` once per
governed repo. Nothing committed by another machine needs editing.

### forge-generated repositories

Repositories initialized by the `forge` scaffolder come pre-wired with Beads and Lefthook in a chained composition. Onboarding with `agent-fitness-functions client install-hooks` auto-composes into that chain without needing any environment variables:

- **Automatic composition:** `forge` creates a dispatcher hook (e.g. `.beads/hooks/pre-commit`) that calls Beads' hook (`.beads/hooks/pre-commit.old`) and then Lefthook's hook (`.beads/hooks/pre-commit.lefthook`). `install-hooks` recognizes this dispatcher pattern via the sibling-signature rule (the dispatcher references `.old` and `.lefthook` by name, and those siblings carry known-owner signatures) and inserts the agent-fitness-functions sidecar call before Lefthook's stage.
- **Execution order:** Beads hook → agent-fitness-functions sidecar → Lefthook. All three stages run exactly once; a nonzero exit from Beads or agent-fitness-functions blocks the subsequent stages.
- **PreToolUse entries in `.claude/settings.local.json`:** Because `forge upgrade` rewrites `.claude/settings.json` wholesale on every run (and runs at every Claude session start), PreToolUse entries cannot be stored there. `install-hooks` instead upserts them to `.claude/settings.local.json`, which `forge` gitignores and never overwrites. Entries are per-machine: run `agent-fitness-functions client install-hooks` once per fresh clone. Running `agent-fitness-functions client doctor` flags missing PreToolUse entries (advisory `⚠` if absent) and explains the per-machine setup.

## Manual / air-gapped production path — server-side onboarding

Production governance is served by the shared container. When a live `POST /register`
call isn't possible (see [Self-service path](#self-service-path) above for the
preferred, connected alternative) — an air-gapped deployment, or registration
deliberately disabled with `AGENT_FITNESS_FUNCTIONS_DISABLE_REGISTRATION=1` — two
artifacts must reach that deployment by hand instead.

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
> `AGENT_FITNESS_FUNCTIONS_CLIENT_*` variables are unset, `client validate` resolves
> managed credentials from the machine governance root itself — the hooks pass no TLS
> material (ADR-0007).

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
| `server_unreachable` | dial refused / timeout / daemon auto-start failure | The governance server could not be reached at all | Run `agent-fitness-functions doctor`; the local daemon auto-starts on `client validate` when dev certs and a repo config exist. Its stdout/stderr are captured at `<govRoot>/certs/daemon.log` (the machine governance root) — check it when auto-start fails |
| `tls_failure` | TLS handshake / certificate-material error | The client and server did not agree on TLS | Run `doctor`; regenerate dev certs with `scripts/generate-dev-certs.sh --force` |
| `port_conflict` | TLS probe failure against a live listener on the shared default local port, in managed local mode | A stale daemon from before this machine migrated to the shared governance root (ADR-0007) — or from before a certificate rotation — still owns the configured local port | Identify it with `lsof -i :7890` and stop it, then re-run `client onboard`; or rerun with a distinct `--addr` |
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
