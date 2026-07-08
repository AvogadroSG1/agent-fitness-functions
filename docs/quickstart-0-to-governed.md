# Quickstart: 0 to Governed in 5 Minutes

This is the fast path from a fresh repository to a governed coding agent. One
command — `stack-fitness-functions client onboard` — provisions dev certificates,
scaffolds the per-repo config, authorizes the local caller, installs the Git and
agent hooks, auto-starts a local governance daemon, and runs `doctor` as the final
gate.

For the authoritative operator reference (production deployment, the full config
schema, verification endpoints, and the error-kind table), see
[Onboarding a New Repository](runbooks/onboard-new-repository.md).

## Prerequisites

- The `stack-fitness-functions` binary on `PATH` (`go build ./cmd/stack-fitness-functions`,
  or add `bin/` to `PATH` — see the [README](../README.md)).
- `python3` with `pyyaml` (the hooks and violation formatter need it):
  `python3 -m pip install -r hooks/requirements.txt`.
- The FINOS `calm` CLI 1.40.0 on `PATH` (`npm install -g @finos/calm-cli@1.40.0`) —
  the governance server shells out to it during validation.
- A running governance server **or none at all**. For local development you need
  neither a container nor pre-generated certificates: `onboard` (and `client validate`)
  auto-start a local TLS daemon, generating dev certs in-process. For production you
  point the hooks at a shared container instead (see the runbook).

You do **not** need `openssl`, a hand-written `configs/<repo>/config.json`, a
hand-edited `caller-repos.json`, or a hand-authored `.claude/settings.json` block.
`onboard` produces all of them.

## One command

```bash
cd /path/to/your-repo
stack-fitness-functions client onboard
```

By default this onboards in `advisory` mode (violations are reported but do not block)
and derives the governance repo name from the working-tree basename. Override either:

```bash
stack-fitness-functions client onboard --enforcement block --repo my-service
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--repo <name>` | working-tree basename | Governance repo name; must match `^[a-z][a-z0-9_-]{0,63}$` |
| `--enforcement <advisory\|block>` | `advisory` | Initial enforcement mode written into the scaffolded config |
| `--addr <url>` | `https://127.0.0.1:7890` | Governance daemon base URL |
| `[path]` | `.` | Repository path (defaults to the current directory) |

`onboard` is idempotent — re-running it leaves an existing config untouched and only
adds the caller authorization or hook entries that are missing.

## What the output looks like

Each step prints a `>` header and an indented result line:

```
Onboarding "my-service" (enforcement=advisory, addr=https://127.0.0.1:7890)

> Dev certificates: /path/to/your-repo/certs
  client CN dev-hook-pool ready

> Server-side config: /path/to/your-repo/configs/my-service/config.json
  scaffolded advisory config with all five fitness functions enabled

> Caller authorization: /path/to/your-repo/caller-repos.json
  authorized CN dev-hook-pool for my-service

> Installing hooks (git + agent Edit/Write validation)
  ...

> Starting local governance daemon at https://127.0.0.1:7890
  daemon healthy

> Running doctor (final gate)
✔ binary: /path/to/stack-fitness-functions
✔ python3: /usr/bin/python3
✔ pyyaml: importable
✔ client certificate: CN=dev-hook-pool valid until ... (/path/to/your-repo/certs/client.crt)
✔ server CA bundle: /path/to/your-repo/certs/ca.crt
✔ server reachable: https://127.0.0.1:7890/health OK
✔ server authentication: authenticated as CN=dev-hook-pool
✔ repo configured server-side: my-service
✔ caller authorized for repo: CN=dev-hook-pool
✔ enforcement mode: advisory
✔ git pre-commit hook: ...
✔ git pre-push hook: ...
✔ agent git-guard hook: configured in .claude/settings.json
✔ agent Edit/Write hook (optional): configured in .claude/settings.json

All checks passed: this repository is ready for governance.

my-service is governed locally.
Remaining manual step for PRODUCTION governance:
  - copy .../configs/my-service/config.json and the .../caller-repos.json entry to the production deployment, then redeploy the container
  - see docs/runbooks/onboard-new-repository.md
```

## Re-check anytime with `doctor`

`doctor` runs the same ordered checks without changing anything. Run it whenever
something looks off:

```bash
stack-fitness-functions doctor
```

Each failing check prints a `→` remediation line naming the exact command or file
that fixes it. `doctor` exits non-zero if any non-advisory check fails, so it is safe
to use as a gate in a script. The "agent Edit/Write hook (optional)" line is advisory
(`⚠`) — it never fails the run — so `doctor` stays correct on repos that only ran an
older `install-hooks`.

Common flags: `--repo <name>` (defaults to the working-tree basename), `--addr <url>`,
and `--client-cert/--client-key/--client-ca` (which otherwise auto-discover from
`STACK_FITNESS_FUNCTIONS_CLIENT_*` or `<repo>/certs`).

## How a coding agent's Edit gets validated

After onboarding, `install-hooks` has registered two `PreToolUse` entries in
`.claude/settings.json`: a Bash git-guard (blocks bypass commands) and an `Edit|Write`
content-validation hook. When the agent proposes an Edit or Write to a `.go`, `.py`,
or `.cs` file, the hook reconstructs the proposed file content, sends it to the
governance server via `client validate`, and acts on the verdict *before the write
lands*:

| Verdict | What the agent sees |
|---------|---------------------|
| **pass** | Nothing — the edit proceeds silently. |
| **advisory** | A formatted violation report on stderr; the edit is **allowed**. |
| **block** | A formatted violation report on stderr; the hook exits non-zero and the edit is **rejected**. The agent should fix the architecture and retry. |
| **setup error** | A labeled block: `stack-fitness-functions SETUP problem ... (infrastructure/configuration, NOT an architecture violation)` with a `kind:`, `detail:`, and `fix:` line. This is a setup problem to resolve (usually `stack-fitness-functions doctor`), not an architecture change. |

The setup-error path is the T5 distinction: infrastructure failures (server down,
cert/TLS problem, repo not configured, caller unauthorized) are labeled as setup
problems and are never dressed up as architecture violations. By default a setup error
blocks the edit (fail-closed); set `STACK_FITNESS_FUNCTIONS_ON_ERROR=advisory` to let
edits through despite a setup failure while you fix it.

The same distinction applies to `git commit`: the pre-commit hook prints the labeled
SETUP block for infrastructure failures and honors `STACK_FITNESS_FUNCTIONS_ON_ERROR`.

## Production handoff (what is still manual)

`onboard` governs your repo **locally**. Production governance is served by a shared,
centrally operated container — the authoritative path — and reaching it requires two
artifacts to be present in that deployment:

1. `configs/<repo>/config.json` — the governance config `onboard` scaffolded (or one
   produced by `baseline --emit-config`, recommended for existing codebases).
2. The `caller-repos.json` entry authorizing the caller CN for `<repo>`.

Copy both to the production deployment and redeploy the container. That is the one
remaining manual step (the container mounts these read-only, so a redeploy is required;
a locally running sandbox server hot-reloads via `fsnotify`). Point the hooks at the
container with `STACK_FITNESS_FUNCTIONS_ADDR` and the remote-mode environment variables.

The full production sequence — including `baseline --emit-config`, the `/preflight` and
`/configs` verification endpoints, and the error-kind troubleshooting table — is in
[Onboarding a New Repository](runbooks/onboard-new-repository.md).
