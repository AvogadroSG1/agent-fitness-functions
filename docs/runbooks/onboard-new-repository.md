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
(config scaffolding, caller-authorization edits, hook installation, daemon start and
restart). What remains genuinely manual is the production redeploy on the
manual/air-gapped path below.

> **Transport, by path.** Since
> [ADR-0010](../adr/0010-plain-http-local-governance.md) the two paths no longer share
> a trust model. The **local** daemon serves **plain HTTP bound to loopback**, and any
> loopback peer is the implicit caller `local` — there are no certificates to
> generate or rotate and no `caller-repos.json` entry for a local repo. The
> **remote/container** path is unchanged: HTTPS with mutual TLS, client-certificate
> CNs authorized in `caller-repos.json`, admins gating `/configs` and `/shutdown`.
> Everything in this runbook that mentions certificates, `caller-repos.json`, or
> `scripts/generate-dev-certs.sh` belongs to the remote/container path unless it says
> otherwise.

## Self-service path

The server now boots with **zero configs** — an empty `configs/` directory is a valid
steady state ("awaiting registration"), not a deployment error. `client onboard` works
from that empty state end to end:

- `client functions` (offline, reads the embedded governance pattern) or `GET
  /functions` (remote, unprivileged — any authenticated caller, no repo needs to exist
  yet) lists the nine-function catalog: description, threshold, operator, unit,
  default-enabled. The five original metric functions default to enabled; the four
  generalized functions (`layer-sovereignty`, `temporal-purity`,
  `sql-composition-safety`, `deterministic-ordering` — see
  [Enabling the generalized fitness functions](#enabling-the-generalized-fitness-functions)
  below) default to disabled.
- `--functions cyclomatic-complexity,logic-density` selects a subset instead of the
  all-five-metric default; an interactive TTY without `--functions` gets a picker
  checklist over the same catalog instead of the silent default.
  `--functions temporal-purity,sql-composition-safety,deterministic-ordering` may
  include three of the four generalized functions; `layer-sovereignty` is rejected by
  `--functions` because it needs layer definitions onboard cannot infer — on a
  terminal the wizard prompts for them instead (see below).
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
- **In managed/local mode** `onboard` writes the repo-local config and syncs it into
  the machine governance root — the local daemon reads that file directly, so there is
  nothing to register remotely. Under ADR-0010 it writes **no caller binding**: the
  loopback caller is implicit, so `caller-repos.json` is not consulted at all and
  `onboard` prints `Caller authorization: not required (implicit local caller)`.

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

For **local** development both acts happen on your machine over plain HTTP on
loopback, and `client onboard` performs all of them — the authorization half collapses
to nothing, because the loopback caller is implicitly authorized for every repo. For
**production** the server-side act happens in the container deployment over HTTPS +
mTLS; the client-side act is `client install-hooks` in the governed repo with the
remote-mode environment variables set (`client onboard` is the local convenience that
also auto-starts and restarts a daemon — not wanted against a shared container).

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

- Go 1.25 or newer for source builds; minimum-toolchain verification uses Go 1.25.14.
- `python3` with `pyyaml` for the hooks and violation formatter:
  `python3 -m pip install -r hooks/requirements.txt`.
- The FINOS `calm` CLI 1.40.0 on `PATH` for the server (`npm install -g
  @finos/calm-cli@1.40.0`).

## Local / developer path — one command

```bash
cd /path/to/<repo>
agent-fitness-functions client onboard
```

On a terminal this is an interactive wizard (see
[The onboard wizard](#the-onboard-wizard) below); with `--functions`, or with no
terminal on stdin, it runs straight through on the flag-derived selection. Either way
it runs the same 0-to-governed sequence, in this order, and gates on `doctor` at the
end:

1. **Dev certificates** — **skipped on the local path.** In local-http mode
   (`--addr` is a loopback `http://` URL, the default) the step prints
   `Dev certificates: not required (local-http mode)` and generates and loads
   nothing. Certificates are still published here on the legacy managed-TLS-local
   path (a loopback `https://` `--addr`), under `--certificates-only`, or validated
   from `AGENT_FITNESS_FUNCTIONS_CLIENT_*` in external/remote mode.
2. **Server-side config scaffold** — the tracked `<repo>/configs/<repo>/config.json`
   production handoff artifact from the embedded template, with the selected fitness
   functions enabled and the rest explicitly present but disabled (the default
   selection is the five metric functions on, the four opt-in ones off), then copied
   verbatim into the shared governance configs dir the local daemon serves
   (`AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR` if set, else `<govRoot>/configs`).
   An existing config is **left unchanged** unless this run has rewrite authority —
   a confirmed wizard diff, or `--update`. Re-running onboard always re-syncs the
   repo-local config into the governance root; the repo-local file is the source of
   truth.
3. **Caller authorization** — **skipped on the local path.** Prints
   `Caller authorization: not required (implicit local caller)`. On the legacy
   managed-TLS-local path it merges CN `dev-hook-pool → <repo>` into the governance
   root's `caller-repos.json` (a sibling of its `configs/` directory), preserving all
   other callers and admins. In external/remote mode this step is replaced by
   `POST /register`.
4. **Legacy pre-ADR-0007 migration** — quarantines repo-local state left over from
   the per-repo-certificate generation. See
   [Legacy migration](#legacy-migration-pre-adr-0007-repo-local-state) below.
5. **Hook installation** — `install-hooks` (see below).
6. **Ensure a current local daemon** — not merely "start one if none is running".
   Onboard probes `GET /health`, which returns the daemon's identity (build
   revision, build-modified flag, listen mode, configs dir, pid, start time), and
   compares it against what this binary expects. A match prints `daemon healthy and
   current`. A mismatch prints `daemon stale: <reasons>; restarting` and performs the
   graceful restart described in [Daemon restarts](#daemon-restarts-staleness-and-recovery)
   below. A daemon still serving `mtls` on the local port is stale by listen mode —
   that is how a machine migrates onto ADR-0010.
7. **Registration acknowledgement** — onboard polls `GET /preflight` (up to 5
   seconds) until the daemon reports the repo configured, its config valid, and the
   caller authorized, absorbing the fsnotify reload debounce so the doctor gate never
   races a live daemon that has not yet seen the files onboard just wrote.
8. **`doctor` (final gate)** — the ordered checks below. Onboard fails (non-zero exit)
   if any non-advisory check fails.

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--repo <name>` | working-tree basename | Governance repo name (validated against the grammar) |
| `--enforcement <advisory\|block>` | `advisory` | Enforcement mode written into the scaffolded config |
| `--addr <url>` | `http://127.0.0.1:7890` | Governance daemon base URL (ADR-0010 local-http default; a legacy `https` loopback daemon is still reached by scheme fallback) |
| `--functions <a,b,...>` | all five metric functions | Comma-separated subset of fitness functions to enable; accepts three of the four generalized functions (`temporal-purity`, `sql-composition-safety`, `deterministic-ordering`) but rejects `layer-sovereignty`. **Supplying it suppresses the wizard** even on a terminal |
| `--update` | off | Rewrite this repository's existing `configs/<repo>/config.json` from the resolved selection. Without it an existing config is never touched. On a terminal the wizard's `y` at the diff gate is the same authority, so `--update` is for runs with no terminal to confirm at (CI, agents, pipes) |
| `--certificates-only` | off | Publish managed development certificates and **do nothing else** — no config, no hooks, no daemon. Remote/container path only |
| `--force-dev-cert-rotation` | off | Rotate the managed development certificates. Only meaningful alongside `--certificates-only` or a legacy managed-TLS-local `--addr` |
| `[path]` | `.` | Repository path |

There is no `--listen-mode` flag on `client onboard`; the mode is derived from the
scheme of `--addr` and passed to the daemon onboard starts. `--listen-mode` is a
`server start` flag.

`onboard` is idempotent. In managed/local mode, when it finishes it prints the one
remaining manual step for production governance (copy the config + caller entry to the
deployment, redeploy) — see the manual/air-gapped path below. In external/remote TLS
mode there is no remaining step: registration against `--addr` already happened via
`POST /register` (see [Self-service path](#self-service-path) above).

### The onboard wizard

`client onboard` prompts when stdin is a real terminal **and** neither `--functions`
nor `--certificates-only` was passed. Anything else — a pipe, CI, an agent — takes the
silent flag-derived path and is byte-identical to the pre-wizard behavior. The wizard
runs five parts in order:

1. **State panel.** Prints the repository name, a daemon line
   (`daemon: unreachable at <addr> (onboard will start one)` /
   `daemon: current (<identity>)` / `daemon: stale (<first reason>)`), and a governance
   status line — `not onboarded (no governance config for this repository yet)`,
   `up to date (enforcement=<mode>, <n> of <total> fitness functions enabled)`, or
   `needs attention` followed by one indented line per item. The attention items are:
   the repo-local config is not registered in the machine governance root; the daemon
   is not current and onboard will restart it; legacy repo-local dev certs; legacy
   repo-local caller bindings.
2. **Enforcement prompt.** `Enforcement mode [advisory/block] (default <mode>):` —
   the default is this repo's current mode on a re-run, `advisory` on a first onboard.
   An empty line accepts it; an invalid answer re-prompts (up to five attempts).
3. **Nine-function picker.** All nine functions with their operator, threshold, and
   unit; digits toggle, `a` selects all, `n` clears, an empty line confirms. Checks are
   seeded from the product defaults on a first run and from this repo's current config
   on a re-run. `layer-sovereignty` is annotated `[prompts for layer definitions]`.
   Confirming an empty selection re-prompts.
4. **Layer prompts.** Only when `layer-sovereignty` was ticked and the repo has no
   layers defined yet. Loops over `{layer name, comma-separated path regexes,
   comma-separated forbidden-pattern regexes}` until an empty name ends it. An
   uncompilable regex re-prompts that field, and the assembled config is validated
   through the daemon's own config parser before anything is written.
5. **Diff and confirm.** Prints the enforcement change (`<mode> (new)`,
   `<mode> (unchanged)`, or `<old> -> <new>`) and either the list of functions to
   enable (first run), `fitness functions: unchanged (...)`, or one
   `<function>: enabled -> disabled` line per change. `Apply these choices? [y/N]:` —
   only `y`/`yes` proceeds. Declining exits with an error and writes nothing.

A confirmed diff is what authorizes rewriting an existing `configs/<repo>/config.json`;
`--update` is the same authority for runs with no terminal. A rewrite carries the
existing file's `exclude-patterns` and `fitness-function-settings` forward.

`--functions` still rejects `layer-sovereignty` — it needs layer definitions the flag
cannot express — and its error points at running onboard interactively.

### Daemon restarts: staleness and recovery

Nothing used to restart the local daemon, so a stale binary or stale certificate
material could serve forever. `onboard` now actuates a restart; `doctor` reports
staleness as an advisory `⚠` but never restarts; `client validate` never restarts.

A daemon is **stale** when its `GET /health` identity disagrees with the running
binary's expectations on any of these dimensions (an empty value on either side means
"not enforced" and never triggers staleness):

| Dimension | Reported reason |
|---|---|
| No identity body at all | `the daemon is a legacy binary that reports no identity on GET /health` |
| Build revision | `daemon build revision "..." does not match the expected "..."` |
| Build from a modified tree | `the daemon binary was built from a modified working tree, so its revision does not describe what is running` |
| Configs directory | `daemon configs directory "..." does not match the expected "..."` |
| Listen mode | `daemon listen mode "..." does not match the expected "..."` |

The restart sequence is:

1. Acquire the machine-wide `restart.lock` in the governance root (an atomic
   `mkdir`-based lock with a heartbeat).
2. `POST /shutdown`. On a scheme/TLS mismatch the client retries the alternate
   loopback scheme, then with the machine's managed dev-cert client — this is the
   migration path for a still-running pre-ADR-0010 mTLS daemon. The scheme flip is
   applied **only** to loopback addresses, never to a remote endpoint.
3. Drain: dial the port until the listener is gone (up to 5s).
4. Start a fresh daemon from the current binary.
5. Re-probe `GET /health` until a **current** identity is observed (up to 5s). Success
   is judged by the observed identity, not by whether our own child won the port bind,
   so a concurrent `client validate` that auto-started first is not a failure.

If the daemon refuses to shut down, the restart aborts before draining or starting
anything and prints the escape hatch:

```
the daemon at <addr> would not shut down (<cause>); identify the process that
owns the port with `lsof -nP -iTCP:<port>` and stop it, then re-run
```

#### One-time manual step on legacy mTLS machines (403 on `/shutdown`)

`POST /shutdown` is **admin-gated**. On a machine whose pre-ADR-0010 daemon is still
serving mTLS and whose `caller-repos.json` has no `admins` list containing the dev CN,
the shutdown is answered **403** — an explicit refusal — so the migration restart stops
and falls through to the `lsof` escape hatch above. This is tracked as `calm-poc-nem3`.

The one-time fix, once per affected machine, before re-running onboard:

1. Edit `<govRoot>/caller-repos.json` and add the dev CN to `admins`:

   ```json
   {
     "callers": { "dev-hook-pool": ["repo-a", "repo-b"] },
     "admins": ["dev-hook-pool"]
   }
   ```

   `<govRoot>` is `${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/governance`.
2. Wait ~1s — the running daemon hot-reloads the caller policy via `fsnotify`, so no
   restart is needed for the edit itself to take effect.
3. Re-run `agent-fitness-functions client onboard` in any governed repo. The restart
   now succeeds and brings the machine up on local-http.

After the migration the file is no longer consulted for local governance at all. It
remains the authorization source of truth for the remote/container path.

### Legacy migration (pre-ADR-0007 repo-local state)

Every `onboard` run migrates state left over from the generation that kept
certificates and caller bindings inside each repository. It is idempotent (a clean
tree prints nothing) and it never deletes anything — it renames, appending
`.pre-adr-0007.bak`, with a timestamp suffix if that name is taken.

| Artifact | Action |
|---|---|
| `<repo>/certs/` matching the managed layout (a `current` symlink into `versions/`, or a `versions/v-<digest>` child) | Quarantined: `migrated: <path> -> <path>.pre-adr-0007.bak (pre-ADR-0007 repo-local dev certs; delete once you no longer need them)` |
| `<repo>/certs/` in any other shape | Left alone: `left alone: <path> is not a managed cert layout, so it is not ours to move` |
| `<repo>/caller-repos.json`, untracked by git | Quarantined: `migrated: <path> -> <path>.pre-adr-0007.bak (untracked pre-ADR-0007 caller bindings)` |
| `<repo>/caller-repos.json`, tracked by git | Left alone: `left alone: <path> is tracked by git; remove it in a commit - the daemon reads the machine governance root's copy` |

Old hook sidecar scripts that still resolve `cert_dir=$repo/certs` are rewritten by the
hook-installation step in the same run, so a legacy repo needs no hook editing by hand.

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
  in `.claude/settings.json` and `.codex/hooks.json`, and intercepted by `.opencode/plugins/agent-fitness-functions.js`.
- `agent-fitness-functions-pre-tool-use` (+ `format-violations.py`) at the git-resolved hooks path — the
  agent Edit/Write content-validation hook, registered as an `Edit|Write` `PreToolUse`
  entry in `.claude/settings.json` and `.codex/hooks.json`, and intercepted by `.opencode/plugins/agent-fitness-functions.js`.
- `.opencode/plugins/agent-fitness-functions.js` — the native OpenCode plugin intercepting tool execution via `tool.execute.before`.

Both `.claude/settings.json` and `.codex/hooks.json` entries are upserted idempotently, preserving non-product hooks (such as `PreCompact`). If the repo already has
unrelated Git hooks the installer refuses to overwrite them; set
`AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1` (sidecar) or
`AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1` (replace).

The registered `.claude/settings.json` and `.codex/hooks.json` commands are machine-portable: each entry is
`"$(git rev-parse --git-path hooks/<name>)"`, which every machine resolves to its own
installed script, so committing these files never embeds one developer's
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
| `.codex/hooks.json` PreToolUse entries | **Portable (tracked)** | Self-locating `git rev-parse --git-path` commands, preserves existing sections |
| `.opencode/plugins/agent-fitness-functions.js` | **Portable (tracked)** | ESM plugin intercepting `bash`, `edit`, `write` via `tool.execute.before` |
| `.git/hooks/*` scripts (or `core.hooksPath` equivalents) | **Per-machine** | Inside `.git/`, never committed; installed by `install-hooks` |
| `<govRoot>/configs/` | **Per-machine** | The local daemon's view; rebuilt by onboarding each repo once per machine |
| `<govRoot>/certs` dev CA + client/server certs | **Per-machine, remote/legacy only** | Not produced by a local-http onboard (ADR-0010); still generated by `--certificates-only` and for the container path |
| `<govRoot>/caller-repos.json` | **Per-machine, remote/legacy only** | Not written or read by a local-http onboard; authorization source of truth for the remote/container path |
| `.claude/settings.local.json` entries (forge repos) | **Per-machine** | forge gitignores this file; see below |

On a new computer: clone, then run `agent-fitness-functions client onboard` once per
governed repo. Nothing committed by another machine needs editing.

### forge-generated repositories

Repositories initialized by the `forge` scaffolder come pre-wired with Beads and Lefthook in a chained composition. Onboarding with `agent-fitness-functions client install-hooks` auto-composes into that chain without needing any environment variables:

- **Automatic composition:** `forge` creates a dispatcher hook (e.g. `.beads/hooks/pre-commit`) that calls Beads' hook (`.beads/hooks/pre-commit.old`) and then Lefthook's hook (`.beads/hooks/pre-commit.lefthook`). `install-hooks` recognizes this dispatcher pattern via the sibling-signature rule (the dispatcher references `.old` and `.lefthook` by name, and those siblings carry known-owner signatures) and inserts the agent-fitness-functions sidecar call before Lefthook's stage.
- **Execution order:** Beads hook → agent-fitness-functions sidecar → Lefthook. All three stages run exactly once; a nonzero exit from Beads or agent-fitness-functions blocks the subsequent stages.
- **PreToolUse entries in `.claude/settings.local.json`:** Because `forge upgrade` rewrites `.claude/settings.json` wholesale on every run (and runs at every Claude session start), PreToolUse entries cannot be stored there. `install-hooks` instead upserts them to `.claude/settings.local.json`, which `forge` gitignores and never overwrites. Entries are per-machine: run `agent-fitness-functions client install-hooks` once per fresh clone. Running `agent-fitness-functions client doctor` flags missing PreToolUse entries (advisory `⚠` if absent) and explains the per-machine setup.

## Repository validation history

`client onboard` starts or reconciles one **local history writer** per user after
governance setup succeeds. This also applies when the governance server is remote:
the history writer and history databases remain on the client machine. Writer
startup failure is a warning and does not fail otherwise successful onboarding.
`doctor` reports an absent or stale writer as a warning; it does not start or repair
the writer implicitly. Run `agent-fitness-functions client onboard` to reconcile it.

Each local Git clone stores completed validation attempts at
`<common-git-dir>/agent-fitness-functions/history.sqlite3`. Git worktrees share that
database, with worktree, branch, and HEAD recorded per attempt. Separate clones and
machines have separate histories. Removing a linked worktree preserves its records.
`uninstall --yes` stops the verified local writer before removing its executable and
retains every clone's database. If writer shutdown cannot be confirmed, uninstall
preserves the executable and reports the incomplete uninstall. Records do not expire
or undergo automatic pruning.

History retains each submitted source string and its received pass, advisory, or
block verdict, including unknown request/result JSON values. Agent dry-run attempts
remain **proposals**: a passing verdict does not prove an edit was applied. The
server's outstanding-violation state still excludes dry runs. Invocation source,
tool, action, and session ID are recorded when supplied; unavailable identity is
`null` in JSON and `unknown` in text. The server's implicit `local` caller and Git
authorship do not identify the agent.

History delivery is at most once and downstream of validation. The validation
client never opens SQLite, waits for persistence, starts the writer, retries an
event, or replays dropped events. A busy database, absent writer, or overload MAY
lose a record while validation keeps its original output and exit behavior. A
missing record is not proof that validation did not run. Timeouts, connection
failures, malformed responses, and warming responses create no history row; their
bounded operational diagnostics go through native OS `logger` without source or
raw response bodies. OS logging failure is itself dropped. OpenTelemetry remains
a possible future consumer of that stream.

### Inspect and compare proposals

Read commands open existing storage read-only and work with the writer and governance
server stopped. They do not initialize, migrate, or repair a database. Unlike
`client validate --repo LOGICAL_NAME`, history's `--repo PATH` selects a Git checkout
and defaults to the current directory. For validation, `--history-worktree PATH`
selects the checkout independently of the governance key.

```bash
agent-fitness-functions client history list --repo /path/to/checkout --format json
agent-fitness-functions client history list --file src/example.go --session SESSION_ID --limit 20
agent-fitness-functions client history list --worktree /path/to/linked-worktree --before 42
agent-fitness-functions client history show EVENT_ID --format json
agent-fitness-functions client history diff FROM_ID TO_ID --format json
```

| Command | JSON output and selection |
|---------|---------------------------|
| `list` | `{"records": [...], "next_before": null}`; newest sequence first, payloads omitted. `--file`, `--session`, and `--worktree` filter records. `--limit` defaults to 50 (1–1000); `--before` is an exclusive sequence cursor. Use a returned `next_before` for the next page. |
| `show EVENT_ID` | One record with metadata and `request_json` / `result_json` objects, including `request_json.proposed_content`. |
| `diff FROM_ID TO_ID` | `from` and `to` record contexts plus `unified_diff`, with three context lines. IDs MUST refer to the same normalized file; explicit comparison across worktrees is allowed. |

All three commands accept `--repo PATH` and `--format text|json` (default `text`).
An absent database produces an empty list without creating files. Exit `0` means
successful inspection, including a nonempty diff; exit `2` means invalid arguments
or an unlike-file comparison; exit `1` means missing IDs or unavailable/corrupt
storage. Comparison is explicit and does not automatically pair attempts or infer
that one proposal caused another to pass.

### Capture context

Manual validation defaults to source `manual` with unknown tool, action, and session.
The optional flags `--history-source`, `--history-tool`, `--history-action`,
`--history-session-id`, and `--history-worktree` override corresponding
`AGENT_FITNESS_FUNCTIONS_HISTORY_SOURCE`, `_TOOL`, `_ACTION`, `_SESSION_ID`, and
`_WORKTREE` environment variables (each uses the full prefix).
Installed agent and Git hooks supply known context using environment variables so
older binaries ignore the metadata safely. Git hooks keep their staged/committed
source selection; stored contents are the contents actually submitted.

The writer's default socket is beneath the installer state root at
`history/writer.sock`, in a private 0700 directory. The history directory is 0700
and its database is 0600. `AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET` MAY override the
socket for isolated tests or long state-root paths; an overlong Unix socket path is
rejected. Attribution fields describe local invocation context, not authenticated
identity. See [ADR-0011](../adr/0011-client-validation-history-is-downstream-observability.md)
for the delivery and lifecycle decisions.

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

`--emit-config` writes a ready-to-use `configs/<repo>/config.json` (all five metric
functions enabled; the four generalized functions are omitted and therefore fall back
to their disabled-by-default baseline when the server parses the config — enable them
by hand per [Enabling the generalized fitness functions](#enabling-the-generalized-fitness-functions)
above) and prints an enforcement-mode recommendation — `block` when zero files
violate the current global thresholds, otherwise `advisory` — plus a threshold-delta
report. The report includes findings-count delta rows for `temporal-purity` and
`sql-composition-safety` (pure AST-finding counts, computable offline); it has no row
for `layer-sovereignty` or `deterministic-ordering`, which need content-scoring
against a live checkout the offline baseline can't do. Thresholds remain **global**
and compiled into `patterns/governance.json`; a per-repo config toggles functions and
enforcement mode but cannot change a threshold. See
[threshold-calibration.md](../threshold-calibration.md) for the full behavior and the
recalibration procedure.

#### Config schema

Defined by `Config` in `internal/server/config.go`:

| Field | Required | Values | Meaning |
|-------|----------|--------|---------|
| `enforcement-mode` | Yes | `block`, `advisory`, `off` | How active violations are routed. `block` rejects; `advisory` reports but allows; `off` disables checks. |
| `enforcement-on-error` | No (defaults to `block`) | `block`, `advisory`, `pass` | How analyzer failures are routed. `block` fails closed; `advisory` reports without blocking; `pass` suppresses failures. |
| `fitness-functions` | Yes | object of `string → bool` | Enables/disables individual checks. A missing key falls back to that function's own baseline default — **enabled** for the five metric functions, **disabled** for the four generalized functions below. |
| `exclude-patterns` | No | array of glob strings | File paths to skip, e.g. `"*_test.go"`, `"fixtures/**"`. |
| `fitness-function-settings` | No | object, see below | Structured configuration for the functions that need more than an enabled/disabled flag. |

Fitness function keys — five metric functions (enabled by default): `cyclomatic-complexity`,
`interface-width`, `implementation-depth`, `logic-density`, `dependency-discipline`.
Four generalized functions (disabled by default): `layer-sovereignty`,
`temporal-purity`, `sql-composition-safety`, `deterministic-ordering`. An unrecognized
key in `fitness-functions` fails config parsing (fail closed) rather than being
silently ignored.

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

#### Enabling the generalized fitness functions

**Recommendation: enable in `advisory` mode first.** `temporal-purity` and
`sql-composition-safety` have documented detection-scope gaps (no import resolution,
receiver-agnostic matching, no variable-indirection or concatenation detection); their
matchers are wide enough to flag things a reviewer would not, but never narrow enough
to miss a real issue that isn't disguised through indirection. Run a repository in
`advisory` with these enabled, review the findings against the codebase for a cycle,
and only move to `block` once the false-positive rate is understood. See
[docs/threshold-exceptions.md](../threshold-exceptions.md) for the full scope
write-up.

**Rollout sequencing.** A per-repo config that enables a key the running server
version doesn't recognize fails config parsing outright — `parseConfigContent`
rejects unknown `fitness-functions` and `fitness-function-settings` keys, and the
`ConfigStore` marks that repo's entry invalid (fail closed) rather than partially
applying it. **The server MUST be redeployed to a version that ships the four
generalized functions before any per-repo config enabling them is mounted** — deploy
the server first, confirm `GET /functions` lists all nine, then roll out configs that
turn the new ones on.

**`layer-sovereignty` requires layer definitions before it can be enabled at all** —
`validateFitnessFunctionSettings` rejects `"layer-sovereignty": true` with an empty or
absent `fitness-function-settings.layer-sovereignty.layers`. There is no offline way
to infer layers from a repository, so `client onboard --functions` refuses
`layer-sovereignty` outright. There are two ways to supply the layers: enable it in
the interactive `client onboard` wizard, which prompts for each layer's name, path
patterns, and forbidden patterns (rejecting a pattern Go's `regexp` package will not
compile and re-asking for that field), or author them by hand. A repository that
already defines layers keeps them verbatim — the wizard never re-asks for settings it
can read. A complete example (mirroring
`internal/server/config_settings_test.go`'s `settingsConfig` fixture):

```json
{
  "enforcement-mode": "advisory",
  "fitness-functions": {
    "layer-sovereignty": true,
    "deterministic-ordering": true,
    "temporal-purity": true
  },
  "fitness-function-settings": {
    "layer-sovereignty": {
      "layers": [
        {
          "name": "bronze",
          "paths": ["src/bronze/**"],
          "forbidden-patterns": ["\\bsilver\\.", "\\bgold\\."]
        },
        {
          "name": "consumer",
          "paths": ["consumers/*.cs"],
          "forbidden-patterns": ["\\braw_[a-z_]+\\b"]
        }
      ]
    },
    "deterministic-ordering": {
      "tie-breaker-tokens": ["_id", "_pk"]
    },
    "temporal-purity": {
      "csharp-policy": "require-offset"
    }
  }
}
```

`layers[].paths` are globs (`**` crosses `/`, `*` does not, `?` matches one
non-`/` character); `layers[].forbidden-patterns` are Go `regexp` source strings.
Both are compiled fail-closed at config-parse time — an invalid glob or regex rejects
the whole config. `deterministic-ordering.tie-breaker-tokens` defaults to
`["_id", "_key", "_pk", "_sk", "id"]` when omitted. `temporal-purity.csharp-policy`
(`naive-only` default, or `require-offset`) is reserved for a future C# milestone and
has no runtime effect today — Python is the only language with a temporal-purity
detector.

### Step 2 — Authorize the caller

> Remote/container path only. A local-http daemon never consults this file — the
> loopback caller `local` is implicitly authorized for every repo and is implicitly an
> admin (ADR-0010). Do not add a `local` entry here; the server rejects an mTLS peer
> certificate asserting `CN=local` and never persists that name during registration.

The caller's certificate CN MUST be authorized for the repo name in
`caller-repos.json`. Add the repo to the relevant caller's list:

```json
{
  "callers": {
    "ci-runner-graft": ["graft"],
    "ci-runner-all": ["graft", "ringstation", "slackstatus"],
    "dev-hook-pool": ["agent-fitness-functions", "graft", "ringstation", "slackstatus", "<repo-name>"]
  },
  "admins": ["dev-hook-pool"]
}
```

- `dev-hook-pool` is the CN that `client onboard --certificates-only` and
  `scripts/generate-dev-certs.sh` issue developer hook certs for. It matters when a
  developer machine points its hooks at an mTLS server; a purely local-http machine
  needs no entry here at all.
- Add a CI caller CN (e.g. `ci-runner-<repo-name>`) for the repo's pipeline.
- `admins` lists CNs allowed to call the admin-gated `GET /configs` and
  `POST /shutdown` endpoints. A CN missing from `admins` gets **403** on `/shutdown`,
  which is what blocks a graceful restart on legacy mTLS machines — see
  [the one-time manual step](#one-time-manual-step-on-legacy-mtls-machines-403-on-shutdown)
  above.

A legacy shape is also accepted: a bare `{"<cn>": ["<repo>", ...]}` object with no
`callers`/`admins` wrapper is parsed as callers with no admins. An entry that fails
validation makes the whole file fail to load, and the daemon falls back to a deny-all
policy rather than crashing.

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

The server itself must be started with TLS material **in its default `mtls` listen
mode**. `server start` reads `AGENT_FITNESS_FUNCTIONS_TLS_CERT`,
`AGENT_FITNESS_FUNCTIONS_TLS_KEY`, and `AGENT_FITNESS_FUNCTIONS_TLS_CA` (the
`--tls-cert/--tls-key/--tls-ca` flags override them). All three must be provided
together or the server refuses to start — there is no silent plain-HTTP fallback.

#### `server start` listen modes

`--listen-mode` (env `AGENT_FITNESS_FUNCTIONS_LISTEN_MODE`; the flag wins) selects one
of exactly two modes. Any other value is rejected at startup.

| Mode | Transport | Caller identity | Use |
|---|---|---|---|
| `mtls` (default) | HTTPS with required client certificates | client-certificate CN, authorized via `caller-repos.json` | Container / remote / production. Byte-identical to the pre-ADR-0010 behavior |
| `local-http` | Plain HTTP, loopback bind only | implicit `local` for any loopback peer; an admin, and authorized for every repo | The machine-local developer daemon that `client onboard` starts |

`local-http` fails closed rather than silently degrading. It refuses to start if asked
to bind a non-loopback address (`local listen mode requires a loopback bind address,
got "..."`), if given explicit server TLS inputs, if given managed development
certificates, or if combined with trusted-proxy headers. A non-loopback peer that
somehow reaches it gets **401**, not an implicit identity.

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
3. `client certificate` — resolved, parseable, CN, and not expired. **Reports `not
   applicable in local-http mode` and passes** on the local path.
4. `server CA bundle` — resolved and contains PEM certificates. Likewise `not
   applicable in local-http mode` on the local path.
5. `governance root` — ADR-0007's machine-scoped root has what it needs. On the local
   path this means the configs directory only: the detail reads
   `<root> (configs: <dir>, certificates not required)`.
6. `roslyn analyzer` — the C# analyzer is present and executable. This is a hard
   failure, not an advisory, even on a machine that governs no C#.
7. `server reachable` — `GET /health` returns 200. Its remediation names
   `agent-fitness-functions client onboard`, not a container command.
8. `daemon up to date` — the daemon's `/health` identity matches this binary's
   expectations. Advisory `⚠` with the staleness reasons and a "re-run client onboard"
   remediation when it does not; `not verifiable` when the daemon is unreachable
   (check 7 already owns that failure).
9. `server authentication` — the authenticated `GET /preflight?repo=<name>` succeeds
   (401 here means the client certificate was not accepted).
10. `repo configured server-side` — from `/preflight` facts.
11. `caller authorized for repo` — from `/preflight` facts; reports `implicit local
    caller` on the local path.
12. `enforcement mode` — from `/preflight` facts (advisory `⚠` when unknown).
13. `validation pipeline` — a synthetic `POST /check` round trip. It is sent with
    `dry_run: true`, so the probe computes a real verdict but never persists into the
    repository's outstanding-violation state (`calm-poc-cpvk`).
14. `git pre-commit hook`, `git pre-push hook`, `agent git-guard hook` (required),
    `agent Edit/Write hook (optional)`, `codex PreToolUse hooks (optional)`, and
    `opencode plugin (optional)` (advisories `⚠` if absent).
15. `legacy repo-local certs` and `config sync` — remaining pre-ADR-0007 artifacts,
    and whether the repo-local config matches the copy in the governance root.

### Endpoints and who may call them

| Endpoint | Method | Auth |
|---|---|---|
| `/health` | GET | **Unauthenticated by design** — the identity body must be readable precisely when transport security is broken, so staleness detection still works |
| `/check` | POST | Authenticated; caller must be authorized for the repo |
| `/state` | GET | Authenticated; caller must be authorized for the repo |
| `/preflight` | GET | Authenticated; reports authorization as facts rather than 403-ing |
| `/functions` | GET | Authenticated, unprivileged, repo-agnostic |
| `/register` | POST | Authenticated; an **admin** CN is required only to overwrite an existing, differing config |
| `/configs` | GET | Authenticated **and admin** |
| `/shutdown` | POST | Authenticated **and admin**; 403 otherwise |

In `local-http` mode the loopback caller satisfies every one of those requirements,
including the admin gates.

### `GET /health` — the identity endpoint

`/health` is more than a liveness ping: it returns the daemon's identity so a client
can tell a current daemon from a stale one before attempting any authenticated call.

```json
{
  "status": "ok",
  "build_revision": "48fd7b4",
  "build_modified": false,
  "listen_mode": "local-http",
  "configs_dir": "/Users/you/.local/state/agent-fitness-functions/governance/configs",
  "pid": 41234,
  "started_at": "2026-09-06T10:11:12Z"
}
```

The body shape never varies. A daemon started without an injected identity answers 200
with the identity fields empty rather than inventing values, and a 200 with no
parseable identity at all is how the client recognizes a pre-ADR-0010 binary. Gating
these fields for non-loopback mTLS binds is tracked as `calm-poc-c58t`.

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
| `server_unreachable` | dial refused / timeout / daemon auto-start failure | The governance server could not be reached at all | Run `agent-fitness-functions doctor`; the local daemon auto-starts on `client validate` when a repo config exists (in local-http mode it needs no certificates). Its stdout/stderr are captured at `<govRoot>/daemon.log` in local-http mode, or `<certDir>/daemon.log` when managed certificates are in play — check it when auto-start fails |
| `tls_failure` | TLS handshake / certificate-material error | The client and server did not agree on TLS. **Cannot occur on the local-http path** — there is no TLS there; seeing it locally means something is still pointing at an `https` daemon | Run `doctor`. Remote/container: fix or regenerate the client material (`scripts/generate-dev-certs.sh --force` for dev material). Local: re-run `client onboard`, which restarts the daemon into local-http |
| `port_conflict` | TLS/scheme probe failure against a live listener on the shared default local port, in managed local mode | A stale daemon from a previous generation — before this machine migrated to the shared governance root (ADR-0007), before a certificate rotation, or before ADR-0010 flipped the local listen mode — still owns the configured local port | Re-run `client onboard` first: it detects the mismatch as staleness and restarts the daemon gracefully. If that fails, identify the owner with `lsof -nP -iTCP:7890` and stop it; or rerun with a distinct `--addr` |
| `unauthenticated` | HTTP 401 | The server rejected the client certificate — or, in local-http mode, the request did not arrive from a loopback peer | Remote/container: run `doctor`; regenerate dev certs, or set `AGENT_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA` to a trusted pair. Local: check that `--addr` is a loopback address |
| `unauthorized` | HTTP 403 | The certificate's CN is not authorized for this repo, or not an admin for an admin-gated endpoint | Remote/container: add the CN to `caller-repos.json` for the repo (and to `admins` for `/configs` or `/shutdown`), then redeploy. Cannot occur for a loopback caller in local-http mode |
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
- **Installed hooks still say `https://127.0.0.1:7890`** — expected, and harmless. The
  embedded hook scripts have not yet had their `AGENT_FITNESS_FUNCTIONS_ADDR` default
  flipped; `client validate` falls back between schemes on loopback, so they reach the
  local-http daemon anyway, at the cost of one failed HTTPS probe per invocation.
  Tracked as `calm-poc-mzkx`.
- **A local daemon that will not restart** — see
  [the one-time manual step](#one-time-manual-step-on-legacy-mtls-machines-403-on-shutdown)
  for the 403-on-`/shutdown` case, and the `lsof` escape hatch for everything else.

## Quick reference

```bash
# Local: one command (advisory mode by default; interactive wizard on a terminal).
# No certificates, no caller-repos.json entry — plain HTTP on loopback (ADR-0010).
cd /path/to/<repo> && agent-fitness-functions client onboard

# Local, non-interactive (CI, agents, pipes) — --update authorizes a rewrite
agent-fitness-functions client onboard --enforcement block \
  --functions cyclomatic-complexity,logic-density,temporal-purity --update

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
walkthrough, and [ADR-0010](../adr/0010-plain-http-local-governance.md) for why the
local path has no certificates.

*Authored By Peter O'Connor with Assistance from Claude Code (databricks-claude-opus-4-8[1m]) · 2026-07-08 · Onboarding a New Repository runbook*
*Revised with Assistance from Claude Code (claude-opus-5[1m]) · 2026-09-06 · ADR-0010 plain-HTTP local governance, onboard wizard, daemon restarts*

*Authored By Peter O'Connor with Assistance from Codex (gpt-6) · 2026-09-06 · Repository validation history operator reference*
