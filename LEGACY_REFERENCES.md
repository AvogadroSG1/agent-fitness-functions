# Legacy Naming References

This document indexes every remaining occurrence of the **`calm-bridge`** /
`bridge` product name in the repository and records a verdict for each: whether
the reference is **intentional** (keep), **historical** (keep as record), or has
been **fixed**.

> **Scope note.** The standalone term **CALM** is *not* legacy. It names the
> FINOS CALM standard and is retained everywhere by design (per
> [ADR-0001](docs/adr/0001-rename-calm-bridge-to-stack-fitness-functions.md)):
> `.calm/config.json`, the `configs/` tree, the FINOS `calm` CLI, the
> `internal/calm` package, and the `github.com/poconnor/calm-poc` module path.
> This document tracks only the dead **`calm-bridge`** product name.

## Background

The product and binary were renamed `calm-bridge` → `stack-fitness-functions`
(ADR-0001, implemented 2026-06-14). The rename fully landed in all functional
code, configuration, scripts, Docker assets, and primary documentation. This
file is the audit trail for what deliberately retains the old name and why.

## Verdict legend

| Verdict | Meaning |
|---------|---------|
| ✅ **Fixed** | Stale reference removed/renamed in the cleanup that produced this doc |
| 📜 **Historical — keep** | Dated record (ADR, plan, spec, issue history). Rewriting would falsify the record |
| 🛡️ **Intentional — keep** | Active code that *must* mention the old name to do its job (regression guards) |

## Active code & configuration

| File | Reference | Verdict |
|------|-----------|---------|
| `hooks/pre_tool_use_test.go` | helper/identifier names `fakeCalmBridge`, `buildCalmBridge`, `startBridgeDaemon`, `bridgeDaemon`, `calmBridge` | ✅ Fixed → `fakeFitnessBin`, `buildFitnessBin`, `startFitnessDaemon`, `fitnessDaemon`, `fitnessBin` |
| `hooks/pre_commit_test.go` | helper/identifier names `fakeCalmBridge`, `buildCalmBridge`, `calmBridge` | ✅ Fixed (same renames) |
| `internal/calm/pattern.go` | doc comment "used by the bridge" | ✅ Fixed → "used by stack-fitness-functions" |
| `internal/server/checker_test.go` | temp fixture filename `bridge_test.go` | ✅ Fixed → `server_helpers_test.go` |
| `requirements.lock` | header comment "calm-bridge container" | ✅ Fixed → "stack-fitness-functions container" |
| `.gitignore` | `/calm-bridge` build-output ignore | ✅ Fixed → `/stack-fitness-functions` |
| `.dockerignore` | `calm-bridge` ignore entry | ✅ Fixed → `stack-fitness-functions` |
| `bin_helpers_test.go` | `[]string{"calm-bridge", "calm-serve", "calm-test"}` | 🛡️ **Intentional — keep.** Regression guard asserting bin helpers contain *no* legacy names. Removing the literal would defeat the test |

## Documentation & history (keep as record)

| File | Refs | Verdict |
|------|------|---------|
| `.beads/issues.jsonl` | 52 | 📜 Issue-tracker history. Closed/dated issues; rewriting tracker history is wrong |
| `docs/adr/0001-rename-calm-bridge-to-stack-fitness-functions.md` | 4 | 📜 The ADR *is* the rename record; the old name is the evidence |
| `docs/superpowers/plans/2026-06-01-container-governance-alignment.md` | 36 | 📜 Dated implementation plan — snapshot of decisions as made |
| `docs/superpowers/specs/2026-05-29-calm-bridge-container-design.md` | 15 | 📜 Dated design spec (superseded, archival). Old name also in the filename |
| `docs/superpowers/plans/2026-06-04-csharp-project-aware-ddc.md` | 8 | 📜 Dated plan |
| `docs/superpowers/plans/2026-05-24-hook-feedback.md` | 6 | 📜 Dated plan |
| `docs/superpowers/plans/2026-05-28-fix-bridge-calm-violations.md` | 3 | 📜 Dated plan (old name also in filename) |
| `docs/superpowers/specs/2026-05-29-calm-bridge-container-design.update.md` | 2 | 📜 Dated spec addendum |
| `docs/superpowers/specs/2026-05-24-hook-feedback-design.md` | 1 | 📜 Dated design spec |
| `Explaination.md` | 1 | 📜 Superseded-pointer doc; the `/tmp/calm-bridge` mention is explicitly described as historical and pointed at `CONTEXT.md` |

## Untracked / generated / ignored artifacts (not source — out of scope)

These contain the old name but are not authored source and are not committed (or
are regenerated). No action required:

- `.claude/backups/*` — beads database backups
- `cover.html`, `cover.out` — generated coverage reports
- `certs/server.crt` — test/dev server certificate carries `DNS:calm-bridge` in
  its SAN. The `certs/` directory is gitignored (`*.crt`, `*.key`). Regenerate
  the cert without the `calm-bridge` SAN entry the next time certs are reissued.

## Module path & repository name

`github.com/poconnor/calm-poc` (module) and the `calm-poc` repo path are
**deliberately retained** per ADR-0001 §Consequences — that migration (push to a
new repo, reclone) is deferred and tracked separately. This is *not* a
`calm-bridge` reference and is in scope only as a reminder of the deferred work.

---

*Authored By Peter O'Connor with Assistance from Claude Code (claude-opus-4-8) · 2026-06-14 · calm-poc legacy-naming audit (branch `chore/purge-legacy-calm-bridge-refs`)*
