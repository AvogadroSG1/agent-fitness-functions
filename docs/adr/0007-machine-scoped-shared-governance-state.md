---
status: accepted
date: 2026-08-30
scope: repository
authors: Peter O'Connor with Claude Code assistance
type: decision
supplements: ADR-0004, ADR-0005
supersedes: the q8d.11.3 hook-side managed TLS handoff contract (partially)
---

# Machine-scoped shared governance state

## Metadata

| Field | Value |
|---|---|
| Date | 2026-08-30 |
| Status | Accepted |
| Scope | Repository |
| Accountable owner | Peter O'Connor, repository maintainer |
| Authors | Peter O'Connor with Claude Code assistance |
| Type | Decision |
| Supplements | ADR-0004 certificate publication, ADR-0005 state root |
| Supersedes | q8d.11.3 hook-side managed TLS resolution, partially |

## Context and Problem Statement

`client onboard` governs one repository locally by generating a per-repository
development CA under `<repo>/certs`, scaffolding `<repo>/configs/<repo>/config.json`
and `<repo>/caller-repos.json`, and auto-starting a daemon on the fixed loopback
port `127.0.0.1:7890` with `--configs-dir <repo>/configs` and the repository's
certificate root. The port is machine-scoped while the trust material and the
configuration directory are repository-scoped. The last onboarded repository
therefore owns the port, and every other governed repository on the machine fails
its TLS probe with `port_conflict` (calm-poc-l9u recorded the symptom and shipped
diagnostics; the scoping contradiction itself remained). A related defect,
calm-poc-wgi, showed that the hooks' explicit-TLS handoff drops the managed root
required for daemon auto-start after a daemon dies.

## Decision Drivers

- One loopback port per machine cannot serve N repository-scoped CAs and configs
  directories; exactly one scoping model must win.
- The server already hot-reloads `configs/<repo>/config.json` and
  `caller-repos.json` via fsnotify, so registering a repository into a shared
  configs directory requires no daemon restart.
- `callerRepoBindingsPath` resolves the bindings file as the sibling of a
  directory named `configs`, so a shared root needs zero server-side change.
- The tracked `<repo>/configs/<repo>/config.json` is the production handoff
  artifact and must remain the source of truth.
- ADR-0004's publisher is parameterized by certificate root and must be reused
  unchanged.
- Hook-context daemon auto-start must survive daemon death without depending on
  an environment selector the hooks have already unset (calm-poc-wgi).
- The local daemon is the developer-latency sandbox (ADR-0005); the shared
  container remains the production path. Simplicity outranks flexibility here.

## Considered Options

### Option 1: Status quo plus documentation

**Benefits**

- No code change; the `port_conflict` diagnostics from calm-poc-l9u already name
  the collision.
- Keeps per-repository trust isolation.

**Costs**

- Only one repository per machine can be governed at a time; every additional
  repository needs a hand-picked `--addr` and its own daemon.
- Rejected because the developer workflow this product exists for — several
  governed repositories on one workstation — fails by default.
- Rejected because documentation cannot make two repo-scoped resources share one
  machine-scoped port.

### Option 2: Per-repository daemons on derived ports

**Benefits**

- Preserves per-repository CA isolation with no shared state.
- No migration of existing certificate roots.

**Costs**

- N governed repositories mean N daemons, N CAs, and N ports to diagnose;
  `doctor`, hooks, and the runbook all grow port-derivation logic.
- Port derivation (hashing repository names) invites silent collisions and makes
  the loopback guard and firewall story harder to reason about.
- Rejected because it multiplies the operational surface of a convenience path.
- Rejected because daemon lifecycle bugs (calm-poc-wgi) would multiply per repo.

### Option 3 (Selected): Machine-scoped shared governance state

One governance root per user per machine, under the ADR-0005 state root:

```
${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/governance/
├── certs/                       # ONE machine development CA (ADR-0004 layout)
├── configs/<repo>/config.json   # registered per onboard; fsnotify hot-reload
└── caller-repos.json            # sibling of configs/ per callerRepoBindingsPath
```

One daemon on `127.0.0.1:7890` serves every governed repository on the machine.

**Benefits**

- Resolves the scoping contradiction: port, CA, and configs directory are all
  machine-scoped.
- Onboarding repository B while repository A's daemon runs is a hot-reloaded
  registration, not a port fight.
- Reuses the ADR-0004 publisher, the ADR-0005 state root, and the server's
  existing bindings-path resolution unchanged.
- Lets hooks stop resolving managed TLS material entirely, which fixes
  calm-poc-wgi structurally.

**Costs**

- One CA and the `dev-hook-pool` CN become machine-wide: any local process
  holding the client certificate can validate against any locally governed
  repository's config. Accepted for the loopback development path; the
  production path (external TLS, per-CN bindings in the container) is unchanged.
- One daemon is a machine-wide blast radius when wedged. Mitigated by the
  now-reliable hook auto-start and fail-closed `AGENT_FITNESS_FUNCTIONS_ON_ERROR`
  semantics.
- Existing per-repository layouts require one `client onboard` re-run per repo.

## Decision Outcome

Chosen option: **Option 3, machine-scoped shared governance state**, because the
local daemon is a machine-scoped resource and its state must be scoped to match.

## Advice

### Root location

The governance root MUST be `<StateRoot>/governance` where `StateRoot` is
ADR-0005's resolution (`$XDG_STATE_HOME/agent-fitness-functions`, defaulting
`XDG_STATE_HOME` to `~/.local/state`). The `governance/` name MUST NOT collide
with installer-owned `versions/`, `runtimes/`, or `current`. `uninstall`
deliberately removes only installer-owned entries; governance state survives
uninstall, and removing it is a documented manual step, because it embodies
per-repository governance decisions rather than installable product bytes.

### Repository config synchronization: copy, not symlink

`client onboard` MUST continue to scaffold the tracked
`<repo>/configs/<repo>/config.json` (production handoff source of truth) and
MUST copy it byte-for-byte into `<govRoot>/configs/<repo>/config.json`.
Re-running onboard re-syncs repo-local to shared. Symlinks are rejected: they
dangle when repositories move, and fsnotify events fire on the symlink's target
directory rather than the watched shared directory, silently disabling hot
reload.

### Precedence and no legacy fallback

- Development certificate root: `AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR`
  overrides, otherwise `<govRoot>/certs`. The former `<repo>/certs` default is
  removed.
- Configs directory: `--configs-dir` flag, then
  `AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR`, otherwise `<govRoot>/configs`. The
  former "use `<repo>/configs` when present" client-side default is removed.
- There is NO silent fallback to legacy per-repository layouts. Legacy installs
  fail closed through the existing SETUP-error surface with a remediation naming
  `client onboard`; `doctor` reports legacy `<repo>/certs` material as removable.
  Recovery is one command per repository.

### Hook contract amendment (supersedes part of q8d.11.3)

Managed-mode hooks MUST NOT resolve, pin, or pass client TLS material and MUST
NOT consume the `AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR` selector. `client
validate` resolves the machine-default governance root itself, so daemon
auto-start always carries its managed root — the calm-poc-wgi failure mode
cannot recur. Explicit `AGENT_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA` passthrough
and the selector-vs-explicit mutual-exclusion guard remain. Consequence:
certificate-version pinning moves from per-hook-run to per-client-invocation; a
multi-file hook run spanning a rotation may use two generations, both trusted
under ADR-0004's exactly-one-previous retention. Accepted for the development
path.

## Consequences

- Positive: N repositories, one daemon, zero port conflicts; onboarding becomes
  additive; calm-poc-wgi is fixed structurally; server code untouched.
- Negative: machine-wide dev CA trust and daemon blast radius, both accepted and
  documented above; a one-time re-onboard per existing repository.
- Neutral: env-var overrides keep their exact semantics for CI and power users.

## Confirmation

The end-to-end acceptance test onboards two repositories against one daemon,
validates both, kills the daemon, and confirms a hook-context commit in either
repository restarts it serving both. `doctor` confirms governance-root health
and flags legacy layouts.

## 30-Second Summary

The local daemon's port was machine-scoped while its CA and configs were
repo-scoped, so the last onboarded repo always broke the others. All local
governance state now lives once per machine under
`~/.local/state/agent-fitness-functions/governance/`, one daemon serves every
governed repo via fsnotify hot-reload, hooks no longer juggle TLS material, and
re-running `client onboard` migrates a legacy repo in one command.
