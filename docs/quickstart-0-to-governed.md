# Quickstart: 0 to Governed in 5 Minutes

This is the fast path from a fresh repository to a governed coding agent. Two
commands: install the binary, then run `agent-fitness-functions client onboard`
inside the repo. On a terminal that command is an interactive wizard — it shows
you what state the repo is in, lets you pick from the nine-function catalog,
writes the config, installs the Git and agent hooks, makes sure a current local
daemon is running, and gates on `doctor`.

**There are no certificates on the local path.** Since
[ADR-0010](adr/0010-plain-http-local-governance.md) the machine-local daemon
speaks plain HTTP on loopback and trusts any loopback peer as the implicit
caller `local`. Nothing to generate, nothing to rotate, no `caller-repos.json`
entry to add. Certificates and mTLS belong to the remote/container path only —
see the [onboarding runbook](runbooks/onboard-new-repository.md).

For the authoritative operator reference (production deployment, the full config
schema, verification endpoints, and the error-kind table), see
[Onboarding a New Repository](runbooks/onboard-new-repository.md).

## Prerequisites

- `python3` with `pyyaml` (the hooks and violation formatter need it):
  `python3 -m pip install -r hooks/requirements.txt`.
- The FINOS `calm` CLI 1.40.0 on `PATH` (`npm install -g @finos/calm-cli@1.40.0`) —
  the governance server shells out to it during validation.
- The Roslyn analyzer on `PATH` for the C# path. `doctor` checks for it
  unconditionally and fails if it is absent, so a machine that governs only Go
  and Python still needs it present. `agent-fitness-functions doctor --repair`
  compiles it when the .NET 8 SDK is available, or set
  `AGENT_FITNESS_FUNCTIONS_ROSLYN_PATH` to an existing build.

You do **not** need Docker, `openssl`, dev certificates, a hand-written
`configs/<repo>/config.json`, a hand-edited `caller-repos.json`, or a
hand-authored `.claude/settings.json` block for local governance.

## Step 1 — install the binary

```bash
make install          # builds and installs into ~/.local/bin (PREFIX overrides it)
```

Make sure `~/.local/bin` is on `PATH`. On macOS the recipe installs with
`install -S` — write a temp file, then rename into place — rather than an in-place
overwrite, because overwriting a running signed binary on Apple Silicon leaves a stale
code-signature cache that SIGKILLs the next exec. So it is safe to run while a daemon
started from the previous build is still running; the next `onboard` notices the
revision change and restarts the daemon.

(From a release archive instead of a source checkout, use `scripts/install.sh`;
see the [runbook](runbooks/onboard-new-repository.md#getting-the-binary-on-path--installer-recommended).)

## Step 2 — onboard the repo

```bash
cd /path/to/your-repo
agent-fitness-functions client onboard
```

That is the whole local path. The governance repo name defaults to the
working-tree basename and enforcement defaults to `advisory` (violations are
reported but do not block).

All local governance state is machine-scoped (ADR-0007): one configs directory
and one daemon on `127.0.0.1:7890` under
`~/.local/state/agent-fitness-functions/governance/`, serving every governed repo
on the machine. Onboarding a second repository registers it with the same daemon
— no port conflicts, no per-repo certificates.

| Flag | Default | Meaning |
|------|---------|---------|
| `--repo <name>` | working-tree basename | Governance repo name; must match `^[a-z][a-z0-9_-]{0,63}$` |
| `--enforcement <advisory\|block>` | `advisory` | Enforcement mode written into the config |
| `--functions <a,b,...>` | the five metric functions | Enable an explicit subset instead. Supplying it **skips the wizard** |
| `--update` | off | Permit rewriting an existing config without a terminal to confirm at (CI, agents, pipes) |
| `--addr <url>` | `http://127.0.0.1:7890` | Governance daemon base URL |
| `[path]` | `.` | Repository path |

## The wizard

`onboard` runs the wizard when stdin is a real terminal and you passed neither
`--functions` nor `--certificates-only`. It has five parts, in order.

**1. The state panel** — what this repo looks like right now:

```
agent-fitness-functions onboard wizard
  repository: my-service
  daemon: current (revision=48fd7b4 listen-mode=local-http configs=/Users/you/.local/state/agent-fitness-functions/governance/configs)
  governance: not onboarded (no governance config for this repository yet)
```

On a repo that is already onboarded the status line reads `up to date
(enforcement=advisory, 5 of 9 fitness functions enabled)`, or `needs attention`
followed by one indented line per item — an unsynced repo-local config, a stale
daemon that onboard will restart, or legacy pre-ADR-0007 certs/bindings it will
quarantine.

**2. Enforcement mode** — the default in brackets is this repo's current mode on
a re-run, `advisory` on a first onboard:

```
Enforcement mode [advisory/block] (default advisory):
```

**3. The nine-function picker** — pre-checked from the product defaults on a
first run (the five metric functions on, the four opt-in ones off), or from this
repo's current config on a re-run:

```
Select fitness functions to enable (digits 1-9 toggle, 'a' all, 'n' none, empty line confirms):
  [x] 1. cyclomatic-complexity   Maximum cyclomatic complexity per function (lte 9 function)
  [x] 2. interface-width         Maximum public method count per CALM node (lte 20 module)
  [x] 3. implementation-depth    Minimum average implementation LOC per public method (gte 0.722 module)
  [x] 4. logic-density           Minimum logic density ratio per file (gte 0.255 file)
  [x] 5. dependency-discipline   Minimum used-import ratio per file (gte 0.8 file)
  [ ] 6. layer-sovereignty       Maximum layer-sovereignty violations per file (lte 0 file) [prompts for layer definitions]
  [ ] 7. temporal-purity         Maximum temporal-purity violations per file (lte 0 file)
  [ ] 8. sql-composition-safety  Maximum sql-composition-safety violations per file (lte 0 file)
  [ ] 9. deterministic-ordering  Maximum deterministic-ordering violations per file (lte 0 file)
```

**4. Layer definitions** — only if you ticked `layer-sovereignty` and the repo
has no layers defined yet. The wizard loops over `{name, paths, forbidden
patterns}`, taking comma-separated Go regexes, until you enter an empty name:

```
layer-sovereignty needs at least one layer: a name, the paths that belong to it, and the patterns those paths must not contain.

Layer name (empty line when done): domain
  Paths in domain (comma-separated regexes): ^internal/domain/
  Patterns forbidden in domain (comma-separated regexes): net/http,database/sql
Layer name (empty line when done):
```

An uncompilable regex re-prompts that field, and the assembled config is parsed
by the daemon's real config parser before anything is written — you cannot
confirm your way into a config the server would reject.

**5. Diff and confirm** — nothing is written until you answer `y`:

```
Review:
  enforcement: advisory -> block
  temporal-purity: disabled -> enabled

Apply these choices? [y/N]:
```

Declining is an error exit that writes nothing.

## What runs after you confirm

```
Onboarding "my-service" (enforcement=advisory, addr=http://127.0.0.1:7890)

> Dev certificates: not required (local-http mode)

> Server-side config: /path/to/your-repo/configs/my-service/config.json
  scaffolded advisory config with fitness functions enabled: cyclomatic-complexity, interface-width, implementation-depth, logic-density, dependency-discipline, temporal-purity

> Caller authorization: not required (implicit local caller)

> Installing hooks (git + agent Edit/Write validation)
  ...

> Starting local governance daemon at http://127.0.0.1:7890
  daemon healthy and current

> Running doctor (final gate)
✔ binary: /path/to/agent-fitness-functions
✔ python3: /usr/bin/python3
✔ pyyaml: importable
✔ client certificate: not applicable in local-http mode
✔ server CA bundle: not applicable in local-http mode
✔ governance root: ~/.local/state/agent-fitness-functions/governance (configs: .../governance/configs, certificates not required)
✔ roslyn analyzer: /path/to/calm-roslyn-analyzer
✔ server reachable: http://127.0.0.1:7890/health OK
✔ daemon up to date: revision=48fd7b4 listen-mode=local-http configs=.../governance/configs
✔ repo configured server-side: my-service
✔ caller authorized for repo: implicit local caller
✔ enforcement mode: advisory
✔ validation pipeline: ...
✔ git pre-commit hook: ...
✔ git pre-push hook: ...
✔ agent git-guard hook: configured in .claude/settings.json
✔ agent Edit/Write hook (optional): configured in .claude/settings.json

All checks passed: this repository is ready for governance.

my-service is governed locally.
Remaining manual step for PRODUCTION governance:
  - copy .../configs/my-service/config.json to the production deployment, then redeploy the container
  - authorize the deploying client's CN for my-service in the production caller-repos.json
  - see docs/runbooks/onboard-new-repository.md
```

Two steps deserve a note:

- **`Dev certificates: not required (local-http mode)`** and **`Caller
  authorization: not required (implicit local caller)`** are the whole of
  ADR-0010 in the output. In the legacy mTLS-on-loopback world these two steps
  generated a CA and edited `caller-repos.json`; on the plain-HTTP local path
  they no-op.
- **`daemon healthy and current`** is an identity check, not just a liveness
  ping. `onboard` reads `GET /health`, which returns the daemon's build revision,
  listen mode, configs directory, and pid. If any of those does not match what
  this binary expects, it prints `daemon stale: <reason>; restarting`, does a
  graceful `POST /shutdown`, drains the port, starts a fresh daemon, and only
  reports success once it re-reads a current identity. That is how a machine
  still running a pre-ADR-0010 mTLS daemon migrates to plain HTTP — the reason
  printed is a listen-mode mismatch.

## Re-running onboard to change your mind

Re-run `agent-fitness-functions client onboard` in an onboarded repo and the
wizard seeds every prompt from the current config, so you are editing rather
than starting over. Confirming the diff rewrites `configs/<repo>/config.json` and
re-syncs it into the governance root; the daemon picks the change up via
`fsnotify` with no restart. `exclude-patterns` and `fitness-function-settings`
you hand-edited are carried forward through the rewrite.

Without a terminal — CI, an agent, a pipe — there are no prompts and an existing
config is never touched. Pass `--update` (with `--functions` / `--enforcement`)
to authorize a non-interactive rewrite.

## Re-check anytime with `doctor`

`doctor` runs the same ordered checks without changing anything:

```bash
agent-fitness-functions doctor
```

Each failing check prints a `→` remediation line naming the exact command or file
that fixes it. `doctor` exits non-zero if any non-advisory check fails, so it is
safe to use as a gate in a script. The transport-security checks report `not
applicable in local-http mode` rather than failing, and `daemon up to date` is
advisory (`⚠`) — a previous-generation daemon still answers checks, so a stale
one is "this machine is due a restart", never a failure. Its remediation is to
re-run `client onboard`.

`doctor`'s own validation probe is sent as a **dry run**: it computes a real
verdict but never persists into the repository's outstanding-violation state, so
diagnosing a repo can never poison it.

Common flags: `--repo <name>` (defaults to the working-tree basename) and
`--addr <url>`. The `--client-cert/--client-key/--client-ca` flags exist for the
remote path; the local path needs none of them.

## How a coding agent's Edit gets validated

After onboarding, `install-hooks` has registered two `PreToolUse` entries in
`.claude/settings.json` (and the equivalents in `.codex/hooks.json` and the
OpenCode plugin): a Bash git-guard (blocks bypass commands) and an `Edit|Write`
content-validation hook. When the agent proposes an Edit or Write to a `.go`,
`.py`, or `.cs` file, the hook reconstructs the proposed file content, sends it to
the governance server via `client validate`, and acts on the verdict *before the
write lands*:

| Verdict | What the agent sees |
|---------|---------------------|
| **pass** | Nothing — the edit proceeds silently. |
| **advisory** | A formatted violation report on stderr; the edit is **allowed**. |
| **block** | A formatted violation report on stderr; the hook exits non-zero and the edit is **rejected**. The agent should fix the architecture and retry. |
| **setup error** | A labeled block: `agent-fitness-functions SETUP problem ... (infrastructure/configuration, NOT an architecture violation)` with a `kind:`, `detail:`, and `fix:` line. This is a setup problem to resolve (usually `agent-fitness-functions doctor`), not an architecture change. |

The pre-write hook validates as a **dry run**: the proposal may never land, so its
verdict is returned but never written into the repository's violation state. The
commit-path hooks (`pre-commit`, `pre-push`) are deliberately *not* dry runs —
staged content lands on disk if the commit succeeds, and outstanding violations
are meant to gate the repository until they are fixed.

The setup-error path is the T5 distinction: infrastructure failures (server down,
repo not configured, caller unauthorized) are labeled as setup problems and are
never dressed up as architecture violations. By default a setup error blocks the
edit (fail-closed); set `AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory` to let edits
through despite a setup failure while you fix it. The same distinction applies to
`git commit`.

> The installed hook scripts still default `AGENT_FITNESS_FUNCTIONS_ADDR` to
> `https://127.0.0.1:7890`. They work anyway — `client validate` falls back
> between schemes on loopback — at the cost of one failed HTTPS probe per
> invocation. Flipping the embedded default is tracked as `calm-poc-mzkx`.

## Production handoff (what is still manual)

`onboard` governs your repo **locally**. Production governance is served by a
shared, centrally operated container over HTTPS with mTLS — the authoritative
path — and reaching it requires the `configs/<repo>/config.json` that `onboard`
scaffolded (or one produced by `baseline --emit-config`, recommended for existing
codebases) plus a `caller-repos.json` entry authorizing the caller CN for
`<repo>`, to be present in that deployment.

Copy both to the production deployment and redeploy the container (the container
mounts these read-only, so a redeploy is required; a locally running server
hot-reloads via `fsnotify`). Point the hooks at the container with
`AGENT_FITNESS_FUNCTIONS_ADDR` and the remote-mode environment variables.

When the remote server is reachable, `client onboard` run with external TLS
material skips the file copying entirely and registers via `POST /register`
instead. The full production sequence — including `baseline --emit-config`, the
`/preflight` and `/configs` verification endpoints, and the error-kind
troubleshooting table — is in
[Onboarding a New Repository](runbooks/onboard-new-repository.md).

*Authored By Peter O'Connor with Assistance from Claude Code (claude-opus-5[1m]) · 2026-09-06 · Quickstart rewrite for ADR-0010 plain-HTTP local governance and the onboard wizard*
