---
status: factual-inventory
date: 2026-08-21
scope: module and repository identity hunks
authorized_by: ADR-0003
baseline: d761e723e5aec2bfe6aa207cc3864b32665d4096
---

# Q8D.1 module identity inventory addendum

## Authority and Scope

The certified PHK.6 inventory remains immutable. ADR-0003 authorizes this factual addendum to select exact repository/module literal hunks that PHK.6 protected before the canonical source-identity decision existed.

This addendum selects module and repository identity hunks only; it does not implement product changes, authorize product hunks, or alter any PHK.6 disposition. The baseline is exactly `d761e723e5aec2bfe6aa207cc3864b32665d4096`, and the old literal is exactly `github.com/poconnor/calm-poc`.

## Baseline Result

At the baseline, the exact old literal occurs 62 times in 30 tracked paths. The complete partition is:

| Disposition | Paths | Occurrences |
|---|---:|---:|
| Rewrite `go.mod` module declaration | 1 | 1 |
| Rewrite active Go source and test literals | 21 | 42 |
| Rewrite current product documentation/instructions | 1 | 1 |
| Retain immutable, historical, or generated evidence | 7 | 18 |
| **Total** | **30** | **62** |

The rewrite set therefore contains exactly 23 paths and 44 occurrences. The retained exact-path allowlist contains exactly 7 paths and 18 occurrences.

## Exact Rewrite Hunks

Every occurrence selected below MUST replace exact prefix/literal `github.com/poconnor/calm-poc` with `github.com/AvogadroSG1/agent-fitness-functions`. No surrounding byte is selected merely because it shares a file with an authorized occurrence.

### Module declaration

| Path | Baseline line | Exact selected hunk | Occurrences |
|---|---:|---|---:|
| `go.mod` | 1 | `module github.com/poconnor/calm-poc` | 1 |

### Active source and test imports

| Path | Baseline lines | Exact selected suffixes after the old module prefix | Occurrences |
|---|---|---|---:|
| `cmd/stack-fitness-functions/main.go` | 21-23 | `/internal/analyzer`, `/internal/client`, `/internal/server` | 3 |
| `cmd/stack-fitness-functions/main_test.go` | 27-28 | `/internal/client`, `/internal/fitness` | 2 |
| `configs/config_test.go` | 10 | `/internal/server` | 1 |
| `fixtures/fixtures_test.go` | 11 | `/internal/analyzer` | 1 |
| `hooks/pre_commit_test.go` | 13 | `/internal/fitness` | 1 |
| `internal/analyzer/onboarding.go` | 10-11 | `/internal/calm`, `/patterns` | 2 |
| `internal/analyzer/onboarding_test.go` | 10 | `/internal/calm` | 1 |
| `internal/client/client.go` | 23-24 | `/internal/fitness`, `/internal/sarif` | 2 |
| `internal/client/client_test.go` | 18-19, 30, 33 | `/internal/analyzer`, `/internal/fitness` twice, `/internal/server` | 4 |
| `internal/report/architecture.go` | 8 | `/internal/analyzer` | 1 |
| `internal/report/architecture_test.go` | 8 | `/internal/analyzer` | 1 |
| `internal/sarif/sarif.go` | 7 | `/internal/fitness` | 1 |
| `internal/sarif/sarif_test.go` | 8-9 | `/internal/fitness`, `/internal/sarif` | 2 |
| `internal/server/abuse_test.go` | 16-18 | `/internal/analyzer`, `/internal/calm`, `/internal/fitness` | 3 |
| `internal/server/checker.go` | 16-20 | `/internal/analyzer`, `/internal/calm`, `/internal/fitness`, `/internal/report`, `/patterns` | 5 |
| `internal/server/checker_csharp_integration_test.go` | 16 | `/internal/calm` | 1 |
| `internal/server/checker_python_integration_test.go` | 17-20 | `/internal/analyzer`, `/internal/calm`, `/internal/fitness`, `/internal/report` | 4 |
| `internal/server/checker_test.go` | 20-22 | `/internal/analyzer`, `/internal/calm`, `/internal/fitness` | 3 |
| `internal/server/server.go` | 17 | `/internal/fitness` | 1 |
| `internal/server/server_test.go` | 24-25 | `/internal/calm`, `/internal/fitness` | 2 |
| `internal/server/state.go` | 8 | `/internal/fitness` | 1 |
| **Total** | | | **42** |

`configs/config_test.go` is path-protected by PHK.6, but its line 10 import literal is authorized through ADR-0003's normalized hash. Its `calm-poc` logical config-key path and assertions at lines 100, 102, 107, and 116, plus every other non-import byte, remain protected.

### Current product documentation and instructions

| Path | Baseline lines | Exact selected hunk | Occurrences |
|---|---|---|---:|
| `CLAUDE.md` | 93 | Exact old module literal in the architecture overview | 1 |
| **Total** | | | **1** |

Only the exact module literal is selected by this literal partition. Product-name edits remain governed independently by ADR-0002, and historical statements outside the literal and semantic selections remain evidence unless another accepted decision expressly authorizes them.

## Exact Semantic Current-Document Hunks

Four current-document lines express the obsolete source/module boundary without containing the full old module literal. ADR-0003 authorizes these exact baseline-to-target line replacements; each target makes the source repository and Go module `github.com/AvogadroSG1/agent-fitness-functions` while preserving FINOS CALM, `.calm/`, `configs/`, the FINOS `calm` CLI, and logical governance key `calm-poc`. Product tokens in these lines remain baseline tokens in this addendum so the semantic hunks do not authorize the product rename beyond ADR-0002.

| Path | Baseline line | Exact baseline text | Exact target text |
|---|---:|---|---|
| `AGENTS.md` | 11 | `- FINOS CALM, .calm/config.json, configs/, the FINOS calm CLI, and the calm-poc module/repo path MUST keep their existing names.` (inline code markers preserved in the manifest) | `- FINOS CALM, .calm/config.json, configs/, and the FINOS calm CLI MUST keep their existing names. The source repository and Go module MUST be github.com/AvogadroSG1/agent-fitness-functions; the logical governance key calm-poc MUST remain unchanged.` (inline code markers preserved in the manifest) |
| `CLAUDE.md` | 126 | Baseline JSON string below | Target JSON string below |
| `CONTEXT.md` | 25 | Baseline JSON string below | Target JSON string below |
| `README.md` | 3 | `Stack Fitness Functions is a local proof-of-concept architecture-as-code system that uses FINOS CALM fitness functions to evaluate proposed source changes before they are written or committed. The repository path remains calm-poc while the product and binary surface are stack-fitness-functions.` (inline code markers preserved in the manifest) | `Stack Fitness Functions is a local proof-of-concept architecture-as-code system that uses FINOS CALM fitness functions to evaluate proposed source changes before they are written or committed. The source repository and Go module are github.com/AvogadroSG1/agent-fitness-functions; the logical governance key remains calm-poc, while the product and binary surface are stack-fitness-functions.` (inline code markers preserved in the manifest) |

The byte-exact line replacements, including every backtick, are JSON strings:

`AGENTS.md` baseline line 11:

```json
"- FINOS CALM, `.calm/config.json`, `configs/`, the FINOS `calm` CLI, and the `calm-poc` module/repo path MUST keep their existing names."
```

`AGENTS.md` target:

```json
"- FINOS CALM, `.calm/config.json`, `configs/`, and the FINOS `calm` CLI MUST keep their existing names. The source repository and Go module MUST be `github.com/AvogadroSG1/agent-fitness-functions`; the logical governance key `calm-poc` MUST remain unchanged."
```

`CLAUDE.md` baseline line 126:

```json
"- FINOS CALM, `.calm/config.json`, `configs/`, the FINOS `calm` CLI, and the `calm-poc` module/repo path retain their names \u2014 CALM is the external standard being enforced, never the product name."
```

`CLAUDE.md` target:

```json
"- FINOS CALM, `.calm/config.json`, `configs/`, and the FINOS `calm` CLI retain their names. The source repository and Go module are `github.com/AvogadroSG1/agent-fitness-functions`; the logical governance key `calm-poc` remains unchanged. CALM is the external standard being enforced, never the product name."
```

`CONTEXT.md` baseline line 25:

```json
"The FINOS Common Architecture Language Model \u2014 the external **standard** this tool enforces. Survives in `.calm/config.json`, `configs/`, the FINOS `calm` CLI dependency, and the `calm-poc` module path. Distinct from the product."
```

`CONTEXT.md` target:

```json
"The FINOS Common Architecture Language Model \u2014 the external **standard** this tool enforces. Survives in `.calm/config.json`, `configs/`, and the FINOS `calm` CLI dependency. The source repository and Go module are `github.com/AvogadroSG1/agent-fitness-functions`; the logical governance key `calm-poc` remains unchanged. Distinct from the product."
```

`README.md` baseline line 3:

```json
"Stack Fitness Functions is a local proof-of-concept architecture-as-code system that uses FINOS CALM fitness functions to evaluate proposed source changes before they are written or committed. The repository path remains `calm-poc` while the product and binary surface are `stack-fitness-functions`."
```

`README.md` target:

```json
"Stack Fitness Functions is a local proof-of-concept architecture-as-code system that uses FINOS CALM fitness functions to evaluate proposed source changes before they are written or committed. The source repository and Go module are `github.com/AvogadroSG1/agent-fitness-functions`; the logical governance key remains `calm-poc`, while the product and binary surface are `stack-fitness-functions`."
```

JSON decoding restores each exact line; specifically, `\u2014` decodes to U+2014 without placing a physical non-ASCII dash in this addendum or manifest. The executable manifest MUST decode to these byte-exact lines. The summary table removes nested inline-code backticks only to remain readable.

| Combined selection | Paths | Authorized hunks/occurrences |
|---|---:|---:|
| Exact literal rewrite partition | 23 | 44 |
| Exact semantic line partition | 4 | 4 |
| Overlap: `CLAUDE.md` | -1 | 0 |
| **Distinct implementation selection** | **26** | **48** |

`CLAUDE.md` is present in both partitions because baseline line 93 contains the old literal and baseline line 126 is a separate semantic hunk. The fixed baseline literal evidence partition remains independently exactly 30 paths/62 occurrences and MUST NOT be recomputed by folding in semantic lines.

## Exact Retained Paths

The old literal MUST remain allowed only at these exact baseline evidence paths. The allowlist MUST use exact paths, MUST NOT use globs, and MUST fail if a row becomes stale or an occurrence appears outside this table.

| Path | Baseline lines | Classification | Retained occurrences |
|---|---|---|---:|
| `.beads/issues.jsonl` | 26 (two), 39 (two), 61 (one) | Immutable closed issue history | 5 |
| `cover.html` | 58, 60, 62, 64, 66, 68 | Generated baseline coverage evidence | 6 |
| `docs/adr/0001-rename-calm-bridge-to-stack-fitness-functions.md` | 23 | Immutable ADR and deferred-migration evidence | 1 |
| `docs/adr/0002-rename-stack-fitness-functions-to-agent-fitness-functions.md` | 118, 140 | Immutable ADR requirement and diagram evidence, partially superseded by ADR-0003 | 2 |
| `docs/escalation/phk6-rename-inventory.md` | 941 | Immutable certified inventory evidence | 1 |
| `docs/escalation/phk6-rename-madr-sequencing.md` | 14 | Immutable sequencing evidence | 1 |
| `LEGACY_REFERENCES.md` | 12, 71 | Immutable legacy/historical audit evidence under PHK.6 | 2 |
| **Total** | | | **18** |

Retention records historical truth; it does not create an old repository/module maintenance obligation, compatibility module, redirect contract, mirror, or valid consumer import path.

### PHK.6 Historical Raw-Hash Closure

The PHK.6 historical table intersects baseline `d761e723e5aec2bfe6aa207cc3864b32665d4096` at exactly these 12 tracked paths, all of which MUST be present in `protected_raw_paths`:

- `.beads/issues.jsonl`
- `Explaination.md`
- `LEGACY_REFERENCES.md`
- `docs/adr/0001-rename-calm-bridge-to-stack-fitness-functions.md`
- `docs/superpowers/plans/2026-05-24-hook-feedback.md`
- `docs/superpowers/plans/2026-05-28-fix-bridge-calm-violations.md`
- `docs/superpowers/plans/2026-06-01-container-governance-alignment.md`
- `docs/superpowers/plans/2026-06-04-csharp-project-aware-ddc.md`
- `docs/superpowers/plans/2026-06-16-install-hooks-naming-and-scheme.md`
- `docs/superpowers/specs/2026-05-24-hook-feedback-design.md`
- `docs/superpowers/specs/2026-05-29-calm-bridge-container-design.md`
- `docs/superpowers/specs/2026-06-16-install-hooks-naming-and-scheme-design.md`

PHK.6 also lists `docs/adr/0001-rename-calm-bridge-to-agent-fitness-functions.md`, but that checkpoint endpoint does not exist at the baseline and therefore is not a baseline raw-hash path. The verifier MUST derive the authoritative set independently by reading the pinned PHK.6 blob, strictly parsing exact path rows after `### historical` and before `### unrelated`, and intersecting those rows with the pinned baseline tree. Manifest `phk6_historical_paths` MUST equal the derived set exactly, every derived path MUST occur in `protected_raw_paths`, and an independent code constant MUST require exactly 31 total raw-protected paths.

## Logical Governance-Key Protection

No `calm-poc` logical governance-key occurrence is authorized to change merely because the Go module changes. In particular:

- `configs/calm-poc/` MUST remain the exact configuration directory key.
- `caller-repos.json` MUST retain exact `calm-poc` authorization entries.
- Settings, environment-backed repository selection, command fixtures, and tests MUST retain `calm-poc` wherever it identifies the logical governed repository.
- `configs/config_test.go` MUST retain its `calm-poc/config.json` path and expected-key assertions.
- `.calm/`, `configs/`, `CALMNode`, `calm_node`, the FINOS `calm` executable/package and CALM vocabulary, and governance `$id` `https://stackoverflow.com/calm-poc/patterns/governance.json` MUST remain exact.

The addendum authorizes no configuration-key migration and no authorization-key migration. Under ADR-0002, `patterns/governance.json` permits only the already-decided product `title` and `description` changes; its `$id` and every other field remain protected.

## Hash Application

For the 23 literal rewrite paths and four semantic paths, candidate verification MUST use `$HOME/peter_code/scratch_work/agent_fitness_functions_q8d1_module_revision_code/verify_module_revision.py` with its adjacent `module_revision_manifest.json`. This module-ticket gate authorizes only module and semantic hunks. Product-token tolerance is comparison-only delegation to the separately required **ADR-0002 Confirmation verifier**, which is not delivered by this ticket; passing this verifier MUST NOT authorize a product hunk or substitute for ADR-0002 confirmation.

The verifier MUST execute this algorithm:

1. Read exact baseline bytes from `d761e723e5aec2bfe6aa207cc3864b32665d4096` and candidate bytes from the checkout, mapping baseline paths only through explicit ADR-0002 product path token pairs.
2. Verify the baseline's exact 23-path/44-occurrence rewrite map, 7-path/18-occurrence retained map, total 30-path/62-occurrence literal partition, 4-path/4-line semantic partition, and combined 26-path/48-hunk selection.
3. Require each rewrite path to contain zero old module literals and its manifest count of new literals; require each semantic target once and its baseline line zero times.
4. Normalize old/new module literals to `<CANONICAL_MODULE>`.
5. Normalize only `STACK_FITNESS_FUNCTIONS`/`AGENT_FITNESS_FUNCTIONS`, `Stack Fitness Functions`/`Agent Fitness Functions`, and `stack-fitness-functions`/`agent-fitness-functions` to distinct stable placeholders.
6. Normalize each manifest semantic before/after line to a stable hunk-specific placeholder, then compare per-path baseline/candidate SHA-256.
7. Derive the definitive 31-path protected set from the baseline and compare raw SHA-256 for every tracked `.calm/` file; every `configs/` file except `configs/config_test.go`; `caller-repos.json`; `internal/fitness/contract.go`; all 12 baseline PHK.6 historical paths; other immutable evidence paths; and stable `internal/calm/` files.
8. Exact-project `patterns/governance.json` by masking only ADR-0002's baseline-to-target title and description, requiring target values, exact `$id`, and equality for every other byte. Product-project `internal/calm/pattern.go` only as explicit ADR-0002 comparison delegation.
9. Derive and compare exact baseline/candidate path-count inventories for `CALMNode`, `calm_node`, `.calm/`, `configs/`, `@finos/calm-cli`, governance `$id`, and boundary-matched logical key `calm-poc`, after only manifest-enumerated transformations.
10. Compare exact baseline/candidate path inventories below `.calm/`, `configs/`, and `internal/calm/`; scan every tracked candidate file and reject unlisted old occurrences, stale or missing allowlist rows, unexpected module identities, protected drift, and any byte drift left after normalization.

The manifest's protected prefixes, raw paths, exact projection, literal matchers, and candidate exclusions are the definitive executable protected contract for this ticket. They are derived and self-checked against baseline `d761e723e5aec2bfe6aa207cc3864b32665d4096`; they do not expand ADR-0002 product scope.

## Reproducibility

Run from any checkout of this repository:

```bash
baseline=d761e723e5aec2bfe6aa207cc3864b32665d4096
old=github.com/poconnor/calm-poc

git rev-parse "$baseline"
git grep -n -F "$old" "$baseline" --
git grep -l -F "$old" "$baseline" -- | wc -l
git grep -o -F "$old" "$baseline" -- | wc -l
git grep -l -F "$old" "$baseline" -- '*.go' | wc -l
git grep -o -F "$old" "$baseline" -- '*.go' | wc -l
```

Expected output counts:

```text
all tracked paths: 30
all exact occurrences: 62
Go source/test paths: 21
Go source/test occurrences: 42
go.mod paths/occurrences: 1/1
current documentation paths/occurrences: 1/1
rewrite paths/occurrences: 23/44
retained paths/occurrences: 7/18
```

The first `git grep` output is the authoritative line-level reproduction of every literal table row. Counts alone are insufficient: verification MUST compare the exact path partition, exact occurrence count per path, and exact retained allowlist, and MUST reject globs and stale rows.

The executable verification commands are:

```bash
repo="$HOME/peter_code/agent-fitness-functions-q8d1-revision"
scratch_root="$HOME/peter_code/scratch_work"
python="$HOME/peter_code/scratch_work/agent_fitness_functions_phk6_inventory_code/.venv/bin/python"
verifier="$HOME/peter_code/scratch_work/agent_fitness_functions_q8d1_module_revision_code/verify_module_revision.py"

"$python" "$verifier" self-check --repo "$repo"
"$python" "$verifier" candidate --repo "$repo"
"$python" "$verifier" pretag --repo "$repo" --scratch-root "$scratch_root"
"$python" "$verifier" metadata --repo "$repo"

candidate_commit="$(git -C "$repo" rev-parse HEAD)"
git -C "$repo" push origin "$candidate_commit:refs/heads/main"
git -C "$repo" tag -a v0.1.0 "$candidate_commit" -m "agent-fitness-functions v0.1.0"
git -C "$repo" push origin refs/tags/v0.1.0

"$python" "$verifier" posttag \
  --repo "$repo" \
  --scratch-root "$scratch_root" \
  --tag v0.1.0 \
  --candidate-commit "$candidate_commit"
```

`candidate` verifies union and protected hashes, protected path/literal inventories, and allowlists. `pretag` first runs candidate tests under the normal authenticated environment so required dependencies are cached, then runs local-replacement consumer `go mod tidy`, `go test`, and command build with `GOPROXY=off`, `GONOPROXY=none`, `GOSUMDB=off`, `GOVCS=*:off`, and `GIT_TERMINAL_PROMPT=0`. These settings prevent proxy, direct private-module, checksum-service, VCS, and interactive credential fallback; a missing cache entry MUST fail rather than access the network. `metadata` parses authenticated GitHub GraphQL JSON and verifies exact repository fields, while origin mismatch diagnostics redact URL user information. `posttag` validates a plain semantic-version tag, compares local and canonical-remote annotated tag-object SHAs, compares both peeled commits to the candidate, then performs authenticated no-replace download, import compilation, and installation in a retained caller-supplied scratch-root child. Credentials MUST come from the environment or an existing credential helper and MUST NOT appear in commands or files; the verifier MUST NOT create, move, delete, or push tags.

## Non-Implementation Statement

This addendum selects source-identity literals and exact semantic current-document lines for later implementation. It changes no module declaration, import, product surface, CALM content, governance key, authorization key, historical record, or runtime behavior.

*Authored By Peter O'Connor with Assistance from OpenCode (openai/gpt-5.6-sol) · 2026-08-21 · calm-poc-q8d.1 module identity inventory addendum*
