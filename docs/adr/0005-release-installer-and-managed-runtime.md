---
status: accepted
date: 2026-08-22
scope: repository
authors: "Peter O'Connor with Claude Code assistance"
type: decision
---

# Release installer and managed runtime

## Metadata

| Field | Value |
|---|---|
| Date | 2026-08-22 |
| Status | Accepted |
| Scope | Repository |
| Accountable owner | Peter O'Connor, repository maintainer |
| Authors | Peter O'Connor with Claude Code assistance |
| Type | Decision |

This MADR is Accepted following independent specification review.
It MUST NOT be treated as authorizing implementation of calm-poc-phk.2 until that
review completes.

**Naming note:** the repository currently spells the product, binary, environment
variables, and helper scripts `stack-fitness-functions` / `STACK_FITNESS_FUNCTIONS_*`
pending the rename in q8d.8. This MADR describes an installer that ships *after* q8d.8
lands, so it uses the target names throughout: `agent-fitness-functions`,
`AGENT_FITNESS_FUNCTIONS_*`, `agent-fitness-functions-serve`,
`agent-fitness-functions-test`.

## Context and Problem Statement

`agent-fitness-functions` today is consumed by cloning the repository: `go build
./cmd/agent-fitness-functions`, then hand-installing `radon`, the FINOS `calm` CLI, and
a .NET 8 SDK to run the Python and C# analyzers, plus a source-tree checkout to locate
`tools/roslyn-analyzer` and the `bin/` helper scripts. calm-poc-phk.2 reproduces the
resulting failure mode directly: in a fresh shell, `command -v agent-fitness-functions
agent-fitness-functions-serve agent-fitness-functions-test radon pytest` all fail, and
the host's default `dotnet` resolves to 10.0.400 (confirmed live on this machine via
Homebrew) while the shipped Roslyn analyzer targets `net8.0` — `dotnet@8` 8.0.130 is
installed but not linked onto `PATH`. Nothing in the repository provisions, pins, or
verifies any of these dependencies; every consumer independently improvises a
workstation setup, so the exact pinned versions this project already depends on
elsewhere (CALM CLI 1.40.0 in the Dockerfile, radon 6.0.1 and its hash-locked
dependencies in `requirements.lock`, .NET SDK 8.0.301 / runtime-deps 8.0.6 in the
Dockerfile) are never guaranteed on a developer's or CI runner's host. This MADR
decides how the product ships and installs itself, without a source checkout, for
fresh-shell Go, Python, and C# governance.

Two gaps surfaced during specification review must be closed as part of this
decision rather than assumed away: `pyyaml` is not currently hash-pinned anywhere in
the repository (only the unpinned `pyyaml>=6` in `hooks/requirements.txt`), and no
integrity mechanism exists today for CALM CLI's npm-sourced provisioning. Both are
addressed as explicit MUSTs in Advice below.

## Decision Drivers

- Fresh-shell install to a working `client validate` / `doctor` / hook-installable
  state, without cloning the repository.
- Exact version pinning that matches what the container already pins (CALM 1.40.0,
  radon 6.0.1, .NET 8.0.301 SDK / 8.0.6 runtime) so local and containerized governance
  agree.
- A deterministic answer to "which `dotnet` runs the Roslyn analyzer" that does not
  depend on, or silently rewrite, the host's default `dotnet` — the exact failure mode
  in calm-poc-phk.2's reproduction steps.
- Checksummed provenance for the release archive and every provisioned runtime
  component — including npm- and pip-sourced ones — proportionate to a governance tool
  that gates commits.
- Atomic install/upgrade with rollback and partial-failure recovery, in the spirit of
  ADR-0004's versioned `current` pointer, without importing that ADR's full
  crash-journal or cross-process locking machinery — installer transactions target a
  single operator's own machine, not concurrent writers.
- Idempotent re-install and upgrade, and a `doctor` extension that tells a maintainer
  exactly which managed component is missing or corrupt.
- Minimize new engineering scope: reuse the Dockerfile's already-working provisioning
  logic and the repository's existing hash-pinned `requirements.lock` pattern rather
  than inventing parallel mechanisms.

## Considered Options

### Option 1: Status quo — system prerequisites documented but user-managed

**Benefits**

- Zero new engineering effort; the runbook already documents `npm install -g
  @finos/calm-cli@1.40.0`, `pip install -r hooks/requirements.txt`, and a .NET 8 SDK as
  prerequisites.
- No new installer surface to secure, test, or maintain.

**Costs**

- This is the exact state calm-poc-phk.2 reports as broken: nothing pins CALM, radon,
  or .NET 8 on a fresh host, and nothing prevents a host default `dotnet` from silently
  shadowing the required 8.x runtime; no checksum guarantee beyond `npm`/`pip`/brew.
- Not idempotent or diagnosable as one command; every workstation drifts independently.
- Rejected because it does not resolve the problem the MADR exists to solve and leaves
  version drift between local and containerized governance as a standing, silent risk.
- Rejected despite having the lowest implementation cost and requiring no new
  release-archive or checksum infrastructure.

### Option 2 (Selected): Product-owned managed runtime under XDG state with pinned versions

**Benefits**

- One installer provisions a checksummed binary plus pinned CALM/radon/.NET components
  into product-owned state, independent of whatever the host already has on `PATH`.
- Reuses provisioning logic already proven in the Dockerfile (same pinned versions) and
  extends the existing `requirements.lock` hash-pinning pattern — once `pyyaml` is
  added to it (see Advice) — rather than inventing a parallel mechanism.
- Atomic versioned publication mirrors ADR-0004's `current` pointer pattern, giving
  install/upgrade/rollback a precedent already accepted for this repository.
- Keeps the fast synchronous local daemon (`https://127.0.0.1:7890`) that `client
  onboard` already auto-starts, preserving today's low-latency commit-hook UX.

**Costs**

- New installer, version-manifest, npm-lockfile, and `doctor`-extension code must be
  designed, built, and kept in sync with the Dockerfile's pins.
- Provisioning the CALM CLI and the Python venv still requires a minimal host Node/npm
  and python3 as bootstrap prerequisites (documented, not product-managed, in v1) —
  this option does not achieve full hermeticity.
- Adopted because it directly answers calm-poc-phk.2, without changing the production
  container path or the fast local-daemon developer loop, and lets `doctor` diagnose
  exactly which pinned component is missing or corrupt — something neither Option 1
  nor Option 3 can do without equivalent new code.
- Adopted despite the residual Node/npm/python3 bootstrap-prerequisite gap (addressed
  as an explicit exclusion below) and despite adding a second provisioning
  implementation the confirmation evidence below MUST keep from drifting apart from
  the Dockerfile's.

### Option 3: Container-backed runtime — everything runs via Docker

**Benefits**

- Reuses the Dockerfile's provisioning verbatim with zero new pinning or checksum
  code; CALM, radon, and .NET are already correctly pinned inside the image.
- True hermeticity: no host Node, python3, or .NET dependency of any kind.
- Matches CLAUDE.md's statement that "the containerized service is the primary
  production path."

**Costs**

- Every `pre-commit` / agent Edit-Write hook invocation would incur container-start
  latency, directly undoing the reason the local daemon auto-start exists in `client
  onboard` (a fast, synchronous, loopback-only check); it also still requires *some*
  native, installed client binary to invoke `docker run` or talk to a container
  daemon, and requires Docker Desktop running as a hard prerequisite.
- Rejected because it trades the existing fast synchronous hook UX for per-commit
  container-start latency, still leaves the native-client installer question
  unanswered, and collapses "CALM missing" / "radon missing" / "wrong .NET" into one
  undifferentiated "container unreachable" `doctor` result.
- Rejected despite requiring the least new provisioning code and already being the
  accepted production path for the server role.

## Decision Outcome

`agent-fitness-functions` MUST ship as checksummed, XDG-state-managed release
archives (Option 2). A `darwin-arm64` archive MUST ship at minimum, verified against a
committed `SHA256SUMS` manifest before extraction; other platforms and architectures
are out of scope for this MADR and MUST be tracked as a follow-on bd issue filed at
acceptance rather than silently advertised as supported. All product-owned state MUST
live under `$XDG_STATE_HOME/agent-fitness-functions/`, published through an
ADR-0004-style atomic `versions/<version>` + `current` pointer for the binary and, in
the same shape, for each managed runtime component: the FINOS CALM CLI, the Python
venv holding radon, and a local-rebuild-only .NET 8 SDK.

The release archive MUST ship a prebuilt, self-contained Roslyn analyzer so C#
governance checks never invoke the host `dotnet` or depend on `DOTNET_ROOT`; a
separately provisioned pinned .NET 8 SDK exists only for the optional local
contributor path of rebuilding `tools/roslyn-analyzer` from source. CALM CLI
provisioning MUST be verified against a committed npm lockfile, and the existing
`requirements.lock` hash-pinning MUST be extended to cover `pyyaml` as a precondition
of calm-poc-phk.2 implementation — this MADR decides the mechanism but does not itself
add that entry. Install, upgrade, rollback, uninstall, and `doctor`-driven repair MUST
be idempotent, atomic, and recoverable from partial failure. The full normative detail
for each of these is specified in Advice below.

## Advice

RFC 2119 terms in this MADR are normative.

### Platform Archives, Checksums, and State Layout

- Releases MUST be published as `agent-fitness-functions-<version>-<os>-<arch>.tar.gz`,
  `darwin-arm64` at minimum for v1. Linux, Windows, and `darwin-amd64` archives are
  out of scope and MUST be tracked as a follow-on bd issue filed at acceptance.
- Each release MUST publish a `SHA256SUMS` manifest covering every archive; the
  installer MUST verify the downloaded archive's SHA-256 before extraction and MUST
  abort without touching any existing installed version on mismatch.
- All product-owned runtime state MUST live under `$XDG_STATE_HOME` (default
  `~/.local/state` when unset), rooted at `$XDG_STATE_HOME/agent-fitness-functions/`.
  Download and extraction scratch space MUST use `$XDG_CACHE_HOME` (default
  `~/.cache`) and MUST NOT leave partial state under the state root on failure.
- The state root MUST use `versions/<version>/` for the installed binary and `bin/`
  helpers, with `current -> versions/<version>` as a relative symlink, mirroring
  ADR-0004's atomic-pointer shape. Before starting any install or upgrade attempt, the
  installer MUST scan `versions/` and every `runtimes/<tool>/versions/` for
  directories lacking a completion marker — a `.verified` sentinel written only after
  full checksum and provenance verification succeeds — and remove any such unverified
  partial directory before beginning a new attempt; it MUST NOT be reused or left to
  accumulate.
- Publishing a new version MUST create and fully verify the version directory (writing
  its `.verified` sentinel last), then atomically rename a freshly created symlink
  over `current`; `current` MUST NOT be repointed before that verification completes.

### Managed Tool Components and Integrity

Managed tool components MUST be versioned identically to the binary, each under
`runtimes/<tool>/versions/<pinned-version>/` with its own `runtimes/<tool>/current`
pointer: `runtimes/calm/` (FINOS CALM CLI, pinned `1.40.0`), `runtimes/python/` (a
venv containing radon, pinned `6.0.1`), and `runtimes/dotnet-sdk/` (a pinned .NET SDK
version-matched to the Dockerfile's `8.0.301`, local-rebuild use only).

**CALM CLI.** The repository MUST commit an npm `package-lock.json` pinning
`@finos/calm-cli@1.40.0` and its full resolved dependency tree, carrying npm's own
per-package `integrity` (SHA-512) fields as the checksum mechanism. Provisioning MUST
run `npm ci --prefix runtimes/calm/versions/<version>` against that committed
lockfile — `npm ci` itself refuses to proceed if the lockfile is missing, out of date,
or an installed package's integrity hash fails to match, which is the integrity
guarantee the decision drivers require. `doctor` MUST verify an installed CALM tree by
running `npm ls --json --prefix <path>` and comparing resolved versions against the
committed lockfile, treating any mismatch as corrupt. Host Node.js and npm MUST be a
documented (not product-managed) bootstrap prerequisite, floored at the oldest LTS line
still receiving security updates at implementation time (Node.js 22 as of 2026-08); the installer MUST NOT
bundle a managed Node.js runtime in v1.

**Python, radon, and pyyaml.** Provisioning MUST create a venv with host `python3`
(also a documented, not product-managed, bootstrap prerequisite) and install with
`pip install --require-hashes -r requirements.lock`. `requirements.lock` today pins
only `radon==6.0.1`, `colorama`, `mando`, and `six` — it does not pin `pyyaml`.
Before calm-poc-phk.2 implementation begins, `pyyaml` MUST be added to
`requirements.lock` with pinned hashes in the same form as its existing entries; the
same hash-pinning mechanism MUST be extended to cover it rather than a second
mechanism being invented for it. Until that precondition is met, `runtimes/python`
MUST NOT be treated as fully hash-pinned, and provisioning MUST fail closed rather
than silently falling back to the unpinned `pyyaml>=6` in `hooks/requirements.txt`.

**.NET 8 SDK and the Roslyn analyzer.** The release archive MUST ship a prebuilt,
self-contained Roslyn analyzer executable per platform, built with the pinned .NET 8
SDK during the release pipeline exactly as the Dockerfile already does (`dotnet
publish --self-contained true -r <rid>`). Because the shipped analyzer bundles its own
.NET 8 runtime, invoking it at governance-check time MUST NOT execute the host
`dotnet` command or depend on `DOTNET_ROOT` at all — this is how the product
guarantees .NET 8 execution regardless of the host default `dotnet` version.
`runtimes/dotnet-sdk/` exists solely for the separate, optional local contributor path
of rebuilding `tools/roslyn-analyzer` from source (e.g.
`agent-fitness-functions-test`'s development use); any product-internal invocation of
`dotnet build` against that path MUST set `DOTNET_ROOT` explicitly to the pinned SDK
directory, scoped only to that child process's environment, and MUST document this in
`--help` output and this ADR — it MUST NOT export or persist `DOTNET_ROOT` into the
user's shell profile or global environment.

### Install, Upgrade, Rollback, Uninstall, and Recovery

- First install MUST use a small, independently checksummed POSIX-shell bootstrap
  script (`install.sh`) — the same category of artifact as rustup's or uv's installer
  — that resolves platform, downloads and verifies the archive, publishes the initial
  `current`, and reports the `PATH` export needed to reach
  `$XDG_STATE_HOME/agent-fitness-functions/current/bin`. Every subsequent lifecycle
  operation (`upgrade`, `uninstall`, `rollback`, and `doctor`'s runtime checks) MUST be
  a subcommand of the installed `agent-fitness-functions` binary itself.
- `agent-fitness-functions upgrade` MUST be idempotent: re-running it when every
  managed component's checksum/version already matches the pinned manifest MUST be a
  complete no-op. Upgrading to a new pinned version MUST retain exactly one previous
  `versions/` entry as the rollback target, mirroring ADR-0004's
  exactly-one-predecessor retention rule. A partially failed install or upgrade
  (interrupted download, failed checksum, failed component provisioning) MUST leave
  `current` and every previously verified `runtimes/<tool>/current` pointer untouched;
  only a fully verified new version and component set MUST become eligible for atomic
  publication.
- `agent-fitness-functions rollback` MUST re-point `current` to the retained previous
  version via the same atomic rename used for publication, and MUST fail without
  mutating state if no predecessor is retained. Rollback MUST also reconcile every
  `runtimes/<tool>/current` pointer to the restored version's pinned manifest, not
  merely the binary: a rolled-back binary paired with a newer or mismatched
  CALM/radon/Roslyn version could silently produce a different verdict than the
  version being restored, so this MADR treats the binary and its pinned runtime set as
  one atomic rollback unit rather than deferring runtime reconciliation to a later
  `doctor --repair`.
- `agent-fitness-functions uninstall` MUST remove the `versions/`, `runtimes/`, and
  `current` pointers under the state root. It MUST NOT modify any governed
  repository's `.git/hooks` or `.claude/settings.json` — hook removal remains a
  separate, explicit `client` operation and is out of scope here.

### doctor and Repair

- `doctor` MUST be extended to verify each managed component's presence, pinned
  version, and integrity (CALM CLI's `npm ls --json` check against the committed
  lockfile, radon's version string inside the venv, the shipped Roslyn analyzer's
  self-reported version) and report exactly which component is missing or corrupt,
  consistent with calm-poc-phk.2's acceptance criteria.
- `agent-fitness-functions upgrade --repair` MUST re-provision only the components
  `doctor` reports as missing or corrupt, without touching healthy components or the
  published binary version.

### Recommendations

- `install.sh`'s printed output and `doctor`'s per-check remediation strings SHOULD
  follow the existing `doctor` `→ remediation` convention documented in the onboarding
  runbook; this MADR does not fix their exact wording.
- Partial-failure and corruption test methodology SHOULD use deterministic filesystem
  seams and fault injection consistent with the repository's existing analyzer tests,
  rather than relying solely on wall-clock or live-network integration tests.
- Operators SHOULD avoid running concurrent `install`/`upgrade`/`rollback` invocations
  on the same machine; see Consequences for the accepted single-operator scope this
  MADR assumes instead of ADR-0004-style locking.

## Consequences

- Local and containerized governance run identical pinned tool versions, closing the
  drift calm-poc-phk.2 reports.
- The C# path gains a genuine hermeticity win: shipping a self-contained analyzer
  removes any host `dotnet` dependency for governance checks, at the cost of a larger
  per-platform archive (a self-contained .NET 8 binary is tens of megabytes).
- Node/npm and python3 remain explicitly documented (not eliminated) bootstrap
  prerequisites; this MADR does not achieve full hermeticity, and the Advice section requires
  installer diagnostics to keep that gap visible rather than silently assumed. The Node.js LTS floor
  is a documented minimum, not a pinned exact version or checksum — this mirrors the
  Dockerfile's own unpinned `apt-get install nodejs npm`, so this MADR does not widen
  the gap the container already has, but it does not close it either.
- Unlike ADR-0004's locked, crash-recoverable certificate publication, this MADR
  provides no cross-process locking for concurrent installer invocations; concurrent
  `install`/`upgrade`/`rollback` runs on one machine are a named, accepted
  out-of-scope assumption — this design targets single-operator development machines,
  not multi-agent or multi-process concurrent installs.
- A second provisioning implementation (installer) now exists beside the Dockerfile's;
  confirmation evidence MUST include a check that pinned versions cannot drift between
  the two without a corresponding review.
- Release archives, a checksum manifest, a committed npm lockfile, and a bootstrap
  shell script become new supply-chain surface reviewed with the same scrutiny as the
  Go binary itself.
- Only `darwin-arm64` is supported at ship time; any other platform or architecture
  has no installer path until the follow-on bd issue's work extends the archive
  matrix.
- **Descoped in v1:** `runtimes/dotnet-sdk` (the third managed component, the optional
  local contributor path for rebuilding `tools/roslyn-analyzer` from source) is not
  implemented in this pass. The release archive ships a prebuilt, self-contained Roslyn
  analyzer (see "`.NET 8 SDK and the Roslyn analyzer`" above), which removes any
  end-user need for a managed .NET SDK to run governance checks; only a contributor
  rebuilding the analyzer from source would want it, and that path remains a manual,
  documented local setup for now. Provisioning `runtimes/dotnet-sdk` through the same
  atomic `versions/<version>` + `current` shape as `runtimes/calm` and `runtimes/python`
  ships in a follow-on issue rather than this one.

## Impact

**Repository Maintainers** MUST own installer implementation, review evidence, and
acceptance, with Peter O'Connor as accountable owner. Implementation is
calm-poc-phk.2 and MUST NOT begin until this MADR passes independent specification
review and the `pyyaml` hash-pinning precondition is met. Estimated implementation and
review effort is 5-9 engineer-days.

## Implementation and Migration Phases

Estimated implementation and review effort is 5-9 engineer-days:

1. **1-2 days:** version manifest (mirroring the Dockerfile's pins), release-archive
   build and `SHA256SUMS` generation, bootstrap `install.sh`, unverified-partial-
   directory cleanup.
2. **2-3 days:** `runtimes/calm` (committed npm lockfile, `npm ci`, `doctor`'s
   `npm ls --json` verification), `runtimes/python` (venv, `requirements.lock` reuse,
   and adding the pinned `pyyaml` entry), and self-contained Roslyn analyzer packaging.
3. **1-2 days:** atomic `current`-pointer publish/rollback (with runtime-pointer
   reconciliation)/uninstall subcommands and `runtimes/dotnet-sdk` provisioning for the
   local contributor rebuild path.
4. **1-2 days:** `doctor` extension for managed-component diagnosis, `upgrade
   --repair`, idempotency and partial-failure tests, and re-review fixes.

## Confirmation

Acceptance requires automated evidence for:

- Fresh-shell install via `install.sh` on `darwin-arm64` reaching a working `client
  validate` / `doctor` state with no source checkout present, and checksum
  verification rejecting a corrupted or mismatched archive without mutating any
  existing `current` pointer.
- CALM CLI, radon, and the shipped Roslyn analyzer each resolving to their pinned
  versions (1.40.0 / 6.0.1 / the release's pinned .NET 8 SDK build), and the analyzer
  executing a governance check without invoking the host `dotnet` binary or reading
  `DOTNET_ROOT` from the ambient shell environment.
- `npm ci` failing a CALM provisioning attempt when the committed lockfile's integrity
  hashes do not match, and `doctor`'s `npm ls --json` check flagging a version-mismatched or
  incomplete CALM install as corrupt.
- `requirements.lock` containing a hash-pinned `pyyaml` entry before calm-poc-phk.2
  merges, and Python provisioning failing closed (not falling back to an unpinned
  install) if that entry is absent.
- Idempotent re-install/re-upgrade producing no filesystem changes when already
  current; upgrade retaining exactly one rollback predecessor; `rollback` restoring it
  via atomic pointer rename and reconciling every `runtimes/<tool>/current` pointer to
  the restored version's manifest.
- Partial-failure injection (interrupted download, deliberately corrupted component)
  leaving `current` and healthy `runtimes/*/current` pointers unchanged, and a
  subsequent install/upgrade attempt detecting and removing the unverified partial
  directory before proceeding.
- `doctor` correctly identifying each of a missing CALM install, a missing/corrupt
  Python venv, and a missing/corrupt Roslyn analyzer as distinct failures with its own
  remediation, and `upgrade --repair` fixing only the reported component.
- Pinned-version parity checks between the installer's manifest, the committed npm
  lockfile, and the Dockerfile's `@finos/calm-cli@1.40.0`, `radon==6.0.1`, and
  `dotnet/sdk:8.0.301` pins.
- `go test ./...` and ShellCheck conformance for `install.sh`.

## 30-Second Summary

The installer ships checksummed `darwin-arm64` release archives, publishes the binary
and helper scripts through an ADR-0004-style atomic `current` pointer under XDG state,
and provisions pinned CALM 1.40.0 (verified via a committed npm lockfile), a
hash-locked radon 6.0.1 venv (extended to cover `pyyaml` as an implementation
precondition), and a self-contained Roslyn analyzer that never touches the host's
`dotnet`. Node/npm and python3 remain documented (not product-managed) bootstrap
prerequisites in v1, and concurrent installer invocations are an accepted
single-operator assumption rather than a locked contract. Upgrade, rollback (with
runtime-pointer reconciliation), uninstall, and `doctor`-driven repair are idempotent
and independently verifiable.

## Counterarguments

### Why not just require the prerequisites, like today?

Because that is the exact configuration calm-poc-phk.2 reports as broken: no pinning,
no checksum, and a silent host-`dotnet`-version hazard the ticket reproduces live.

### Why not go fully hermetic and bundle Node.js too?

Bundling a managed Node runtime is real scope this MADR defers rather than hides; the
residual Node/npm and python3 prerequisites are documented and diagnosable by
`doctor` — a meaningfully better state than today's fully undocumented one, without
expanding this release's engineering surface further.

### Why not just run everything through Docker?

The local daemon auto-start in `client onboard` exists specifically to give
commit-time hooks synchronous, low-latency checks; routing every hook invocation
through a container reintroduces the latency that design already avoids, and still
requires a native client to exist on the host by some installer — this option
relocates the problem instead of resolving it.

### Why does rollback reconcile runtime pointers instead of leaving that to doctor --repair?

Because a binary and a mismatched CALM/radon/Roslyn version can silently produce a
different verdict than the version being restored; treating the whole pinned set as
one rollback unit avoids a rolled-back binary running against unrelated runtime
versions until someone happens to run `doctor --repair`.

## Supporting Evidence

- [ADR-0002: Rename stack-fitness-functions to agent-fitness-functions](0002-rename-stack-fitness-functions-to-agent-fitness-functions.md)
- [ADR-0004: Atomic development certificate publication](0004-atomic-development-certificate-publication.md)
- `Dockerfile` — existing pinned provisioning of CALM CLI 1.40.0, radon 6.0.1 via
  `requirements.lock`, and .NET SDK 8.0.301 / runtime-deps 8.0.6.
- `requirements.lock` / `hooks/requirements.txt` — confirms `pyyaml` is currently
  unpinned, motivating the precondition above.
- `docs/runbooks/onboard-new-repository.md` — current manual prerequisite list this
  MADR replaces with a managed runtime.
- calm-poc-phk.2 — the bug report this MADR's decision must make implementable.
- [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119)

*Authored By Peter O'Connor with Assistance from Claude Code (claude-sonnet-5) · 2026-08-22 · Release installer and managed runtime architecture decision for agent-fitness-functions*
