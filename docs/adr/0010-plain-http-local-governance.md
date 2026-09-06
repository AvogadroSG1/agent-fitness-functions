# Plain-HTTP loopback listen mode for local governance

- Status: accepted
- Date: 2026-09-05
- Deciders: Peter O'Connor
- Technical Story: beads calm-poc-rzss (S3), calm-poc-pxmm (S4), calm-poc-c58t

## Context and Problem Statement

Every recurring local-development failure of the governance daemon has been
certificate plumbing, not governance: repositories carrying pre-ADR-0007
`certs/` directories signed by orphaned CAs flooding the daemon log with
`tls: bad certificate`, certificate rotation requiring an unmanaged daemon
restart, and onboarding failures (agent-cockpit, 2026-09-05) whose root cause
was a client presenting the wrong CA's material. Local developers validating
their own edits on their own machine gain nothing from mutual TLS, and the
machinery costs them onboarding friction and a standing class of outages.

How should the machine-local daemon (ADR-0007: one daemon on 127.0.0.1:7890
serving every governed repo) authenticate its callers?

## Decision Drivers

- Local onboarding must be zero-ceremony: no cert generation, rotation, or
  caller authorization edits for a repo on the developer's own machine.
- The remote/container path (mTLS with client-cert CNs authorized in
  `caller-repos.json`) must remain intact and unchanged.
- Staleness detection (`GET /health` identity body) must work precisely when
  transport security would be broken, so it cannot sit behind client auth.

## Considered Options

1. Plain HTTP on loopback, implicit caller, mTLS retained for remote only.
2. Keep mTLS everywhere; automate cert rotation and daemon restart.
3. Plain HTTP on loopback plus a shared bearer token from the governance root.

## Decision Outcome

Chosen option: **1 — plain HTTP on loopback**. The daemon gains a
`local-http` listen mode (`server start --listen-mode local-http`, env
`AGENT_FITNESS_FUNCTIONS_LISTEN_MODE`):

- It refuses to bind any non-loopback address, refuses explicit TLS material,
  refuses managed certificate roots, and refuses trusted-proxy mode.
- A request from a loopback peer carries the implicit caller identity
  `local`; `caller-repos.json` is never consulted. Non-loopback peers (which
  should be impossible given the bind restriction) fail closed with 401.
- The implicit local caller is an admin (`/configs`, `/shutdown`,
  `/register`): the daemon is the developer's own machine-local process, so
  withholding administration would only lock them out of something they
  already control — and self-service registration against it is the managed
  onboarding flow this mode exists to serve.
- The default listen mode remains `mtls`; the container/production path is
  byte-identical to before this ADR.

Option 2 was rejected because it automates the symptom while retaining the
failure class; option 3 adds a secret-distribution problem (and a new "token
missing/stale" failure class) for negligible local threat reduction.

### Residual Risks (accepted)

- **Local re-origination.** Any process on the machine that terminates an
  external connection and re-originates it locally (an SSH `-L`/`-R` tunnel,
  `socat`, a misconfigured reverse proxy) presents a legitimate loopback
  `RemoteAddr` and is indistinguishable from the developer. This is the same
  trade-off as token-less localhost trust in comparable developer tooling.
  The daemon only lints file content and edits its own governance configs, so
  the blast radius is repo-governance state, not code execution.
- **Any local process is the developer.** Other user processes on the machine
  can validate content, register repos, or shut the daemon down. Accepted for
  a single-developer workstation; multi-user machines should use mTLS mode.
- **`/health` identity pre-auth.** The identity body (build revision, listen
  mode, configs dir, pid) is served unauthenticated by design so staleness
  detection works when transport security is broken. On loopback this is
  acceptable; gating those fields for non-loopback mTLS binds is tracked as
  calm-poc-c58t.

### Consequences

- Local onboarding reduces to: write the repo config, install hooks, ensure a
  current daemon. No certs, no caller bindings.
- `devcerts`, `--certificates-only`, and `caller-repos.json` remain solely
  for the remote/container path.
- The client (S4) defaults to `http://127.0.0.1:7890` for managed-local use
  and falls back between schemes so hooks written against either generation
  keep working during migration.
- A daemon serving `mtls` on 7890 is reported as stale by the S5 staleness
  probe (`listen_mode` mismatch) and restarted into `local-http` by onboard.
