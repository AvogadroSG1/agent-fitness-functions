# install-hooks: post-rename naming + default scheme/mTLS fix

- **Date:** 2026-06-16
- **Status:** Proposed
- **Scope:** `internal/client/client.go`, `internal/client/hookassets/{pre-commit,pre-push}.sh`, associated tests
- **Related:** ADR 0001 (calm-bridge → stack-fitness-functions rename); calm-poc-qo7 (sibling bug, `bin/stack-fitness-functions-test` mTLS — already fixed)

## Context

Two distinct defects in `stack-fitness-functions client install-hooks` were found during
cross-language server testing. Both are fallout from the `calm-bridge → stack-fitness-functions`
rename (ADR 0001) leaving the install path behind.

### Defect 1 — installed files still carry the `calm-*` identity

`RunInstallHooks` (`internal/client/client.go`) writes sidecars named
`calm-pre-commit`, `calm-pre-push`, and `calm-git-guard`, stamps `# CALM ...` comment
markers into hook bodies, and **detects an existing managed hook by the `CALM` marker**
(`client.go:127,137`). The Claude `settings.json` PreToolUse entry is matched on the
literal substring `"calm-git-guard"` (`client.go:242`). After the product rename, a
freshly installed hook still announces itself as CALM, which is confusing and
inconsistent with the `stack-fitness-functions` binary, commands, env vars, and helper
scripts.

> Note: per `CLAUDE.md` Naming Surface, **FINOS CALM, `.calm/config.json`, `configs/`,
> the FINOS `calm` CLI, and the `calm-poc` module/repo path retain their names.** This
> change touches **only the generated git-hook artifacts**, which are product surface,
> not FINOS CALM surface.

### Defect 2 — generated hooks default to `http://`, but the server serves `https://`

The generated `pre-commit.sh` / `pre-push.sh` leave `addr` empty
(`addr=${STACK_FITNESS_FUNCTIONS_ADDR:-}`) and pass `--addr` only when set. With no
override, `client validate` falls back to its hardcoded default
`http://localhost:7890` (`client.go:35`). But `server start` serves **HTTPS with
mandatory mTLS** whenever TLS certs are present — which is the Docker/production path
(`docker-compose.yml` mounts `/app/certs` and passes `--tls-*`; `server.go:389`,
`server.go:366` `RequireAuthentication=true`). A fresh install therefore cannot reach
its own server: the `http://` request is refused, and even an `https://` request is
rejected without client certificates. The only escape today is manually exporting
`STACK_FITNESS_FUNCTIONS_ADDR` plus the `CLIENT_*` cert vars.

The sibling helper `bin/stack-fitness-functions-test` already solved this exact problem
(calm-poc-qo7): default `https://127.0.0.1:7890` + auto-discover `<repo>/certs`
(`client.crt`/`client.key`/`ca.crt`) with 12-factor env override precedence. The
generated hooks should mirror that one convention rather than invent a second.

## Decisions

1. **Rename generated artifacts, recognize legacy on upgrade.** New installs write
   `stack-fitness-functions-pre-commit`, `stack-fitness-functions-pre-push`,
   `stack-fitness-functions-git-guard` and stamp
   `# stack-fitness-functions <hook> hook (sidecar)` markers. Existing-hook **detection**
   recognizes *both* the new marker and the legacy `CALM`/`calm-git-guard` markers, so
   re-running `install-hooks` on a calm-era install upgrades idempotently — no duplicate
   hooks, no spurious overwrite-guard trip.

2. **Mirror the test-helper scheme/mTLS convention in generated hooks.** Change
   `client validate`'s `--addr` default to `https://127.0.0.1:7890`, and teach the
   generated hook scripts to resolve mTLS client credentials exactly as
   `bin/stack-fitness-functions-test` does:
   - `addr` default `https://127.0.0.1:7890` (still overridable via
     `STACK_FITNESS_FUNCTIONS_ADDR`).
   - cert-dir discovery: `STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR`, else `<repo>/certs`.
   - per-credential env override precedence:
     `STACK_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA` win; else
     `<cert_dir>/client.crt|client.key|ca.crt`.
   - pass discovered/overridden creds through as `--client-cert/--client-key/--client-ca`
     **only when the files exist**, so a non-TLS local server still works.

## Design

### `internal/client/client.go`

- Introduce a single naming constant/prefix `stack-fitness-functions` for hook artifact
  names and markers; replace the three `calm-*` literals and the `# CALM ...` /
  `CALM <hook> hook` / `calm-git-guard` literals.
- `installGitHook`: write sidecar `stack-fitness-functions-<hook>`; new marker
  `# stack-fitness-functions <hook> hook (sidecar)`. Detection branch matches the new
  marker **OR** the legacy `CALM <hook> hook` / `# CALM <hook> hook (sidecar)` markers.
- `installGitGuard` / `upsertGitGuard`: write `stack-fitness-functions-git-guard`; when
  scanning existing `settings.json` PreToolUse entries, treat a command containing either
  `stack-fitness-functions-git-guard` **or** legacy `calm-git-guard` as the managed entry
  and rewrite it to the new path (idempotent upgrade).
- Default `--addr` for `client validate` → `https://127.0.0.1:7890`.

### `internal/client/hookassets/{pre-commit,pre-push}.sh`

- `addr=${STACK_FITNESS_FUNCTIONS_ADDR:-https://127.0.0.1:7890}` (was empty).
- Add cert-dir + per-credential resolution identical to the test helper, guarded so that
  missing cert files simply omit the `--client-*` flags (degrade to plain HTTP for a
  local non-TLS server) rather than hard-fail. The remote-mode loopback/https guards
  already present stay intact.
- Comment markers in the script headers updated to `# stack-fitness-functions ...`.

### Data flow (unchanged shape, corrected defaults)

```
install-hooks → writes stack-fitness-functions-{pre-commit,pre-push,git-guard}
                + .claude/settings.json PreToolUse → stack-fitness-functions-git-guard

commit → pre-commit hook → resolve addr (https default) + discover <repo>/certs
       → client validate --addr https://127.0.0.1:7890 --client-cert/key/ca …
       → server (mTLS) → PASS/BLOCK/ADVISORY
```

## Error handling

- Missing cert files in a hook: omit `--client-*` flags (no hard failure) — preserves the
  plain-HTTP local-server path. (The test helper hard-fails because mTLS is its sole mode;
  hooks must support both, so they degrade instead.)
- Legacy hook present: recognized and upgraded in place; never duplicated.
- Genuinely foreign (non-managed) hook: existing overwrite/append guard
  (`STACK_FITNESS_FUNCTIONS_HOOK_OVERWRITE` / `_APPEND`) is preserved unchanged.

## Testing (BDD, red-green-refactor)

1. **Naming (new install):** `install-hooks` on a clean repo writes
   `stack-fitness-functions-pre-commit/-pre-push/-git-guard` and a
   `# stack-fitness-functions pre-commit hook (sidecar)` marker; asserts no `calm-` hook
   files are created. (Update existing assertions at `client_test.go:89,116,136,180,193`.)
2. **Legacy upgrade idempotency:** seed a repo with a legacy `# CALM pre-commit hook
   (sidecar)` hook + `calm-git-guard` settings entry; run `install-hooks`; assert the
   managed entry is upgraded (single git-guard entry, new path) with no duplication.
3. **Scheme default:** `client validate` with no `--addr` resolves
   `https://127.0.0.1:7890` (assert via the daemon-base-URL used in a stubbed request).
4. **Hook cert discovery:** with `<repo>/certs/{client.crt,client.key,ca.crt}` present,
   generated hook forwards `--client-cert/--client-key/--client-ca`; with them absent and
   no env override, the flags are omitted. (Mirror the helper's
   `TestStackFitnessFunctionsTestPassesMTLS` / `…EnvOverridesCerts` shape.)

## Out of scope (YAGNI)

- No change to `server start` defaults or to FINOS CALM names (`.calm/`, `configs/`,
  `calm-poc`, the `calm` CLI).
- No migration tool to rewrite already-installed hooks beyond the idempotent re-run path.
