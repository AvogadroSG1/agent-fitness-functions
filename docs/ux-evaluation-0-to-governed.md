# Platform UX Evaluation: 0 to Governed

> **Historical snapshot.** This evaluation predates ADR-0007 and ADR-0010, and its
> findings have since been resolved. Managed dev certificates and the daemon's configs
> directory now default to the machine governance root
> (`~/.local/state/agent-fitness-functions/governance/`), not `<repo>/certs` /
> `<repo>/configs` as described below. More importantly, **the local path no longer
> uses TLS at all**: the machine-local daemon serves plain HTTP on loopback with an
> implicit caller, so F1's certificate deadlock and the `https://127.0.0.1:7890`
> default it describes no longer exist, and F2's agent Edit/Write hook is installed by
> `client install-hooks`. See
> [ADR-0007](adr/0007-machine-scoped-shared-governance-state.md),
> [ADR-0010](adr/0010-plain-http-local-governance.md), and the
> [onboarding runbook](runbooks/onboard-new-repository.md) for current behavior.

**Date:** 2026-07-08
**Lens:** Platform UX — every debug session a developer needs between "I added agent-fitness-functions to my repo" and "my coding agent is governed" is a product failure.
**North star:** One command, zero debugging, from fresh clone to a governed coding agent.

## Why this evaluation exists

The core value proposition of agent-fitness-functions is putting architecture fitness
functions as close to the coding agent as possible — validating an Edit/Write *before*
it lands, not in CI an hour later. That value is only demonstrable if onboarding is
frictionless. Today it is not: real onboarding attempts fail on certificate errors,
hook installation gaps, and dead-end error messages. This document maps the current
0-to-governed journey, identifies the structural causes (with file evidence), and
defines the prioritized task list to fix them.

## The journey today

Three personas participate in onboarding:

| Persona | Steps today | Touch points |
|---|---|---|
| Platform owner | Hand-write `configs/<repo>/config.json`; hand-edit `caller-repos.json`; update `configs/config_test.go` caller pin; redeploy container | 4 |
| Repo developer | Build/install binary; install pyyaml; generate dev certs; start TLS server via docker compose; `client install-hooks`; export up to 6 env vars; hand-author the agent hook block in `.claude/settings.json`; verify with a test commit | 8 |
| Coding agent | Gets Bash git-guard only; content validation on Edit/Write never arrives unless step above was done by hand | — |

**~9–12 manual touch points, two hand-edited JSON files, one redeploy, and the
flagship agent hook is never installed by tooling.** Every one of these is a place
where a new user stalls and debugs.

## Findings

### F1 — The local golden path is broken by design (the "certificate issues")

`StartDaemon` (`internal/client/client.go:608`) auto-starts the server with **no TLS
flags**, so it listens on plain HTTP. But the client default address is
`https://127.0.0.1:7890` (`internal/client/client.go:52`) and the server hardcodes
`RequireAuthentication = true` (`internal/server/server.go:366`). The TLS handshake can
never succeed, `waitHealthy` times out after 500ms, and the user sees
`daemon at <addr> did not become healthy within 500ms`. The advertised zero-config
auto-start path is non-functional; the only working local setup is docker compose +
`scripts/generate-dev-certs.sh`, and nothing in the failure output says so.

### F2 — The agent content-validation hook is never installed

`client install-hooks` installs only `pre-commit`, `pre-push`, and the Bash-matcher
`git-guard` (`internal/client/client.go:114-120`, `hookassets/`). The actual Edit/Write
content-validation hook — `hooks/pre-tool-use.sh`, the heart of "fitness functions next
to the agent" — is **not embedded in the binary and not installed**. Its only wiring in
existence is this repo's own hand-authored `.claude/settings.json`, which hardcodes
paths and seven env vars. A freshly onboarded repo gets bypass protection but no
pre-write architecture validation for the agent.

### F3 — No preflight, no doctor, no diagnostics

`install-hooks` performs zero checks: not server reachability, not cert presence, not
python3/pyyaml, not whether `configs/<repo>/config.json` exists server-side. There is
no `doctor`/`status` subcommand and no caller-facing "am I ready?" endpoint (the useful
`GET /configs` diagnostic is admin-CN-gated and undocumented in the runbook). The first
feedback a developer gets that something is wrong is a blocked commit.

### F4 — Every infrastructure failure masquerades as a violation

The hooks fail closed with the generic `agent-fitness-functions check failed for
<file>` whether the server is down, certs are missing, the TLS handshake failed, the
caller is unauthorized (403), or the repo isn't configured (404)
(`hookassets/pre-commit.sh:114-118`, `hooks/pre-tool-use.sh:205-214`). The developer
cannot distinguish "fix my architecture" from "fix my setup," which turns every
misconfiguration into a debugging session at commit time.

### F5 — Server-side onboarding is manual copy-paste across two files (plus a hidden test)

Per-repo config is hand-written from a heredoc in the runbook; the templates at
`configs/block-template.json` / `configs/advisory-template.json` exist but nothing uses
them. Caller authorization is a hand-edit of `caller-repos.json` — and
`configs/config_test.go:60-71` pins the exact caller map with `reflect.DeepEqual`, so
authorizing a new caller silently breaks the test suite (undocumented). Config reaches
production only via redeploy.

### F6 — Footguns that convert small mistakes into long debugging sessions

- `AGENT_FITNESS_FUNCTIONS_TLS_CERT/KEY/CA` are set in `docker-compose.yml` and the
  `Dockerfile` but the binary **never reads them** — TLS is flags-only
  (`cmd/agent-fitness-functions/main.go:108-110`). Setting only the env vars yields a
  plain-HTTP server and inexplicable 401s.
- Silent `test-repo` fallback in the server config resolver
  (`internal/server/config.go:96-106`) and silent `calm-poc` repo-name fallback in
  `bin/agent-fitness-functions-test` can mask a misnamed repo as a wrong-config
  mystery instead of a clear "not configured."
- The Go client has no cert auto-discovery (flags only); only the shell hooks default
  to `<repo>/certs`. Running `client validate` by hand — the natural debugging move —
  fails with a bare handshake error even when certs sit in the default location.

### F7 — Baseline → config is manual, and thresholds are global + compiled in

`baseline` emits percentile reports, but a human reads them, applies the calibration
rule, and hand-edits `patterns/governance.json` — which is **embedded in the binary**
(`patterns/embed.go`), so recalibration means rebuild + redeploy. Per-repo
`config.json` can only toggle functions and enforcement mode. The documented promise of
"calibrate before enforcement to avoid day-one false positives" is a manual,
doc-driven process.

### F8 — None of this is tracked

The beads tracker (153 issues) contains no issue for onboarding UX, config
generation, doctor/preflight, or baseline automation. These gaps were latent until
this evaluation.

## Task list

Priorities: **P0** = the golden path must work; **P1** = failures must explain
themselves; **P2** = server-side ergonomics; **P3** = value demonstration.

### P0 — Make the golden path actually work

**T1. Fix local daemon auto-start (kills the certificate failure).** *(F1, F6)*
Auto-start must produce a server the default client can reach: pass
`--tls-cert/--tls-key/--tls-ca` resolved with the same defaults the shell hooks use
(`AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR` → `<repo>/certs`), generating dev certs when
absent. Give the Go client the same env-var/default cert discovery as the hooks.
Raise the 500ms health-wait window to something a TLS server can meet.
*Acceptance:* with only the binary and a config for the repo, `client validate`
succeeds with zero TLS flags and zero pre-generated certs.

**T2. Embed and install the agent content-validation hook.** *(F2)*
Embed `pre-tool-use.sh` (+ its formatter dependency) in `internal/client/hookassets/`;
`install-hooks` registers the `Edit|Write` PreToolUse entry in `.claude/settings.json`
with the correct env block. Twin-guard the embedded copy against `hooks/pre-tool-use.sh`
drift, as done for the existing hooks.
*Acceptance:* fresh repo + `install-hooks` → agent Edit/Write calls are validated with
no manual settings authoring.

**T3. `agent-fitness-functions doctor`.** *(F3)*
New subcommand: ordered ✔/✘ checks with one-line remediation each — binary version;
python3 + pyyaml; cert files present/parseable/CN; server reachable; TLS handshake +
auth; repo configured server-side; hooks installed (git + settings.json).
**T3a.** New `GET /preflight?repo=<name>` endpoint (client-cert authenticated,
non-admin) returning `{authenticated_cn, repo_configured, caller_authorized,
enforcement_mode}` — the missing "am I ready?" affordance.

**T4. `agent-fitness-functions client onboard` — single-command 0-to-governed.** *(F1–F5)*
Orchestrates: repo-name detection/validation, dev-cert generation, server-side config
scaffold from the (currently unused) `configs/*-template.json`, caller-authorization
entry, `install-hooks`, then `doctor` as the final gate. Prints exactly what remains
manual (e.g. prod redeploy).
*Acceptance:* scratch repo → governed agent in one command plus one server restart.

### P1 — Failure modes must explain themselves

**T5. Differentiate infra failure from violation.** *(F4)*
Client distinguishes connection-refused / TLS failure / 401 / 403-unauthorized /
404-not-configured / real block (exit codes or structured output). Hooks print the
specific cause plus a fix hint (404 → "repo not onboarded server-side — run
`client onboard` / see runbook"). Honor an infra-error advisory mode consistent with
the server-side `enforcement-on-error` setting.

**T6. Kill the footguns.** *(F6, F5)*
(a) Binary reads `AGENT_FITNESS_FUNCTIONS_TLS_CERT/KEY/CA` (flags override env);
(b) remove the silent `test-repo` fallback; (c) remove the silent `calm-poc` fallback
in `bin/agent-fitness-functions-test`; (d) loosen the `configs/config_test.go`
caller-map pin so authorizing a new caller doesn't break tests.

### P2 — Server-side onboarding ergonomics

**T7. `baseline --emit-config`.** *(F7)*
Baseline emits a ready-to-use `configs/<repo>/config.json` (enforcement-mode
recommendation derived from violation counts) plus a threshold-delta report against
the embedded `patterns/governance.json`, and documents the global-threshold
limitation explicitly. (Per-repo thresholds are a separate future epic.)

**T8. Rewrite the onboarding runbook + 5-minute quickstart.** *(F3, F5, F6)*
Rebuild `docs/runbooks/onboard-new-repository.md` around `onboard` + `doctor`;
document `GET /configs` and `/preflight`; fix the README env-var table (annotate or
remove decoys).

### P3 — Value demonstration (agent-first)

**T9. Agent-path demo script.** Extend `scripts/smoke-red-green-go.sh` into an
agent-journey demo: Edit blocked → violation explained → fix applied → Edit passes —
the "fitness functions next to the agent" story in one runnable script.

## Dependency order

T1 → T2 (both touch `internal/client`); T3a → T3; T1–T3 → T4 (onboard composes them);
T5/T6 independent after P0; T8 last (documents the new surface); T9 after T2.

## Success metrics

- Manual touch points from ~9–12 to **≤2** (run `onboard`; restart/redeploy server).
- Zero failure modes where an infra problem is reported as a fitness-function block.
- Time from fresh clone to first governed agent edit: **under 5 minutes**, no debugging.
