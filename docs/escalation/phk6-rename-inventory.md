# PHK.6 Rename Inventory

This is a decision-neutral inventory of the contaminated checkpoint. It MUST NOT be treated as an implementation baseline, and it selects no compatibility, metadata, certificate, or target behavior.

## Evidence

- Immutable base commit: `aaf1f440dccb7610a2a66c88c61622d1ea4e3d97`
- Human-readable relationship: `origin/main` resolved to `aaf1f440dccb7610a2a66c88c61622d1ea4e3d97` when this inventory was produced.
- Checkpoint: `55df0f87fb9af5334d36bcf9148c66817b98c426`
- Change records from `git diff --name-status -M aaf1f440dccb7610a2a66c88c61622d1ea4e3d97..55df0f87fb9af5334d36bcf9148c66817b98c426`: **780**
- Path endpoints classified exactly once: **831** (729 single-path records plus 51 two-endpoint rename records)
- Rename records requiring origin/main to candidate mapping: **51**
- Approved review evidence used: the `calm-poc-phk.6` acceptance contract and `docs/escalation/phk6-rename-madr-sequencing.md` resolved-decision record.
- The existing untracked `.opencode/` worktree directory is not an intended `phk.6` deliverable and was not added to tracked scope.

## Disposition Counts

| Disposition | Path endpoints |
|---|---:|
| active candidate | 56 |
| protected | 29 |
| historical | 13 |
| unrelated | 731 |
| unresolved | 2 |
| **Total** | **831** |

## Origin Mappings

- `.agents/skills/analyzing-dotnet-performance/SKILL.md` -> `.codex/skills/analyzing-dotnet-performance/SKILL.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/async-patterns.md` -> `.codex/skills/analyzing-dotnet-performance/references/async-patterns.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/collections-and-linq.md` -> `.codex/skills/analyzing-dotnet-performance/references/collections-and-linq.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/critical-patterns.md` -> `.codex/skills/analyzing-dotnet-performance/references/critical-patterns.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/io-and-serialization.md` -> `.codex/skills/analyzing-dotnet-performance/references/io-and-serialization.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/memory-and-strings.md` -> `.codex/skills/analyzing-dotnet-performance/references/memory-and-strings.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/regex-patterns.md` -> `.codex/skills/analyzing-dotnet-performance/references/regex-patterns.md` (R100)
- `.agents/skills/analyzing-dotnet-performance/references/structural-patterns.md` -> `.codex/skills/analyzing-dotnet-performance/references/structural-patterns.md` (R100)
- `.agents/skills/coverage-analysis/SKILL.md` -> `.codex/skills/coverage-analysis/SKILL.md` (R100)
- `.agents/skills/coverage-analysis/references/guidelines.md` -> `.codex/skills/coverage-analysis/references/guidelines.md` (R100)
- `.agents/skills/coverage-analysis/references/output-format.md` -> `.codex/skills/coverage-analysis/references/output-format.md` (R100)
- `.agents/skills/coverage-analysis/scripts/Compute-CrapScores.ps1` -> `.codex/skills/coverage-analysis/scripts/Compute-CrapScores.ps1` (R100)
- `.agents/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1` -> `.codex/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1` (R100)
- `.agents/skills/grill-with-docs/ADR-FORMAT.md` -> `.codex/skills/domain-modeling/ADR-FORMAT.md` (R100)
- `.agents/skills/grill-with-docs/CONTEXT-FORMAT.md` -> `.codex/skills/domain-modeling/CONTEXT-FORMAT.md` (R066)
- `.agents/skills/grill-with-docs/SKILL.md` -> `.codex/skills/domain-modeling/SKILL.md` (R071)
- `.agents/skills/golang-context/SKILL.md` -> `.codex/skills/golang-context/SKILL.md` (R084)
- `.agents/skills/golang-context/evals/evals.json` -> `.codex/skills/golang-context/evals/evals.json` (R085)
- `.agents/skills/golang-context/references/cancellation.md` -> `.codex/skills/golang-context/references/cancellation.md` (R092)
- `.agents/skills/golang-context/references/http-services.md` -> `.codex/skills/golang-context/references/http-services.md` (R100)
- `.agents/skills/golang-context/references/values-tracing.md` -> `.codex/skills/golang-context/references/values-tracing.md` (R100)
- `.agents/skills/golang-data-structures/SKILL.md` -> `.codex/skills/golang-data-structures/SKILL.md` (R092)
- `.agents/skills/golang-data-structures/evals/evals.json` -> `.codex/skills/golang-data-structures/evals/evals.json` (R081)
- `.agents/skills/golang-data-structures/references/containers.md` -> `.codex/skills/golang-data-structures/references/containers.md` (R093)
- `.agents/skills/golang-data-structures/references/generics.md` -> `.codex/skills/golang-data-structures/references/generics.md` (R100)
- `.agents/skills/golang-data-structures/references/map-internals.md` -> `.codex/skills/golang-data-structures/references/map-internals.md` (R100)
- `.agents/skills/golang-data-structures/references/pointers.md` -> `.codex/skills/golang-data-structures/references/pointers.md` (R100)
- `.agents/skills/golang-data-structures/references/slice-internals.md` -> `.codex/skills/golang-data-structures/references/slice-internals.md` (R100)
- `.agents/skills/golang-documentation/SKILL.md` -> `.codex/skills/golang-documentation/SKILL.md` (R083)
- `.agents/skills/golang-documentation/assets/templates/CHANGELOG.md` -> `.codex/skills/golang-documentation/assets/templates/CHANGELOG.md` (R100)
- `.agents/skills/golang-documentation/assets/templates/CONTRIBUTING.md` -> `.codex/skills/golang-documentation/assets/templates/CONTRIBUTING.md` (R100)
- `.agents/skills/golang-documentation/assets/templates/README.md` -> `.codex/skills/golang-documentation/assets/templates/README.md` (R100)
- `.agents/skills/golang-documentation/assets/templates/llms.txt` -> `.codex/skills/golang-documentation/assets/templates/llms.txt` (R100)
- `.agents/skills/golang-documentation/references/application.md` -> `.codex/skills/golang-documentation/references/application.md` (R098)
- `.agents/skills/golang-documentation/references/code-comments.md` -> `.codex/skills/golang-documentation/references/code-comments.md` (R091)
- `.agents/skills/golang-documentation/references/library.md` -> `.codex/skills/golang-documentation/references/library.md` (R096)
- `.agents/skills/golang-documentation/references/project-docs.md` -> `.codex/skills/golang-documentation/references/project-docs.md` (R100)
- `.agents/skills/python-anti-patterns/SKILL.md` -> `.codex/skills/python-anti-patterns/SKILL.md` (R100)
- `.agents/skills/python-code-style/SKILL.md` -> `.codex/skills/python-code-style/SKILL.md` (R100)
- `.agents/skills/python-configuration/SKILL.md` -> `.codex/skills/python-configuration/SKILL.md` (R100)
- `.agents/skills/python-performance-optimization/SKILL.md` -> `.codex/skills/python-performance-optimization/SKILL.md` (R100)
- `.agents/skills/python-performance-optimization/references/advanced-patterns.md` -> `.codex/skills/python-performance-optimization/references/advanced-patterns.md` (R100)
- `.agents/skills/python-pro/SKILL.md` -> `.codex/skills/python-pro/SKILL.md` (R100)
- `.agents/skills/python-type-safety/SKILL.md` -> `.codex/skills/python-type-safety/SKILL.md` (R100)
- `.agents/skills/tdd/mocking.md` -> `.codex/skills/tdd/mocking.md` (R100)
- `.agents/skills/tdd/tests.md` -> `.codex/skills/tdd/tests.md` (R074)
- `bin/stack-fitness-functions-serve` -> `bin/agent-fitness-functions-serve` (R056)
- `bin/stack-fitness-functions-test` -> `bin/agent-fitness-functions-test` (R077)
- `cmd/stack-fitness-functions/main.go` -> `cmd/agent-fitness-functions/main.go` (R088)
- `cmd/stack-fitness-functions/main_test.go` -> `cmd/agent-fitness-functions/main_test.go` (R095)
- `docs/adr/0001-rename-calm-bridge-to-stack-fitness-functions.md` -> `docs/adr/0001-rename-calm-bridge-to-agent-fitness-functions.md` (R081)

## Complete Endpoint Inventory

### active candidate

| Path | Rationale |
|---|---|
| `.claude/settings.json` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `.dockerignore` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `.gitignore` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `.vscode/tasks.json` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `AGENTS.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `CLAUDE.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `CONTEXT.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `Dockerfile` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `README.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `bin/agent-fitness-functions-serve` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `bin/agent-fitness-functions-test` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `bin/stack-fitness-functions-serve` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `bin/stack-fitness-functions-test` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `bin_helper_mtls_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `bin_helpers_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `cmd/agent-fitness-functions/main.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `cmd/agent-fitness-functions/main_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `cmd/stack-fitness-functions/main.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `cmd/stack-fitness-functions/main_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docker-compose.yml` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docker_contract_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/quickstart-0-to-governed.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/runbooks/onboard-new-repository.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/runbooks/red-green-demo.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/spec/engineering-spec.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/spec/why-and-what.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/threshold-calibration.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/threshold-exceptions.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `docs/ux-evaluation-0-to-governed.md` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/format-violations.py` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/git-guard.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/infra_error_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/pre-commit.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/pre-push.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/pre-tool-use.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/pre_commit_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/pre_push_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `hooks/pre_tool_use_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/calm/pattern.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/client.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/client_test.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/daemon.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/doctor.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/failure.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/hookassets/format-violations.py` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/hookassets/git-guard.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/hookassets/pre-commit.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/hookassets/pre-push.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/hookassets/pre-tool-use.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/client/onboard.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/sarif/sarif.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/server/auth.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `internal/server/server.go` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `scripts/baseline.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `scripts/demo-agent-governed-edit.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |
| `scripts/smoke-red-green-go.sh` | Only product-identity hunks are active candidates; protected hunks MUST NOT replay. |

### protected

| Path | Rationale |
|---|---|
| `.calm/results.sarif` | CALM configuration and fixture paths MUST retain their existing names. |
| `configs/config_test.go` | CALM configuration and fixture paths MUST retain their existing names. |
| `configs/instill/config.json` | CALM configuration and fixture paths MUST retain their existing names. |
| `fixtures/fixtures_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `go.mod` | The calm-poc module/import boundary MUST remain unchanged. |
| `hooks/test_format_violations.py` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/analyzer.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/baseline_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/csharp_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/golang.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/golang_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/onboarding.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/onboarding_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/analyzer/python.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/fitness/contract.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/fitness/contract_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/report/architecture.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/report/architecture_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/sarif/sarif_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/abuse_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/checker.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/checker_csharp_integration_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/checker_integration_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/checker_python_integration_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/checker_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/server_test.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `internal/server/state.go` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |
| `patterns/governance.json` | The governance $id is protected; the title remains an unresolved MADR decision. |
| `tools/roslyn-analyzer/Program.cs` | Only protected module, CALM, .calm, or configs vocabulary changes; these hunks MUST NOT replay. |

### historical

| Path | Rationale |
|---|---|
| `.beads/issues.jsonl` | Closed Beads history MUST remain immutable. |
| `Explaination.md` | Legacy or historical record MUST remain immutable. |
| `LEGACY_REFERENCES.md` | Legacy or historical record MUST remain immutable. |
| `docs/adr/0001-rename-calm-bridge-to-agent-fitness-functions.md` | Dated plan/specification record MUST remain immutable. |
| `docs/adr/0001-rename-calm-bridge-to-stack-fitness-functions.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/plans/2026-05-24-hook-feedback.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/plans/2026-05-28-fix-bridge-calm-violations.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/plans/2026-06-01-container-governance-alignment.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/plans/2026-06-04-csharp-project-aware-ddc.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/plans/2026-06-16-install-hooks-naming-and-scheme.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/specs/2026-05-24-hook-feedback-design.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/specs/2026-05-29-calm-bridge-container-design.md` | Dated plan/specification record MUST remain immutable. |
| `docs/superpowers/specs/2026-06-16-install-hooks-naming-and-scheme-design.md` | Dated plan/specification record MUST remain immutable. |

### unrelated

| Path | Rationale |
|---|---|
| `.agents/.DS_Store` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/.DS_Store` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/references/authentication.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/references/commands.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/references/proxy-support.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/references/session-management.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/references/snapshot-refs.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/references/video-recording.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/templates/authenticated-session.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/templates/capture-workflow.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/agent-browser/templates/form-automation.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:agent-browser` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:grill-me` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:grill-with-docs` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:handoff` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:multi-reviewer-patterns` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:parallel-debugging` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:parallel-feature-development` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:tdd` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/ai-workflow:to-issues` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/async-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/collections-and-linq.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/critical-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/io-and-serialization.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/memory-and-strings.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/regex-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/analyzing-dotnet-performance/references/structural-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/build-parallelism/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/build-perf-baseline/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/build-perf-diagnostics/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-architecture` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-cli` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-control` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-decorator` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-documentation` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-flow` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-interface` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-metadata` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-moment` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-node` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-pattern` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-relationship` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-standards` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/calm:calm-timeline` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/check-bin-obj-clash/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/cloud:docker` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/clr-activation-debugging/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/clr-activation-debugging/references/activation-flow.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/clr-activation-debugging/references/com-activation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/clr-activation-debugging/references/log-format.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/coverage-analysis/.DS_Store` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/coverage-analysis/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/coverage-analysis/references/guidelines.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/coverage-analysis/references/output-format.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/coverage-analysis/scripts/Compute-CrapScores.ps1` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/crap-score/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/csharp-lsp/LICENSE` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/csharp-lsp/README.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/directory-build-organization/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/directory-build-organization/references/common-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/directory-build-organization/references/multi-level-examples.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/directory-build-organization/references/targetframework-props-pitfall.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dispatching-parallel-agents/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-backend-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-backend-patterns/assets/repository-template.cs` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-backend-patterns/assets/service-template.cs` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-backend-patterns/references/dapper-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-backend-patterns/references/ef-core-best-practices.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-test-frameworks/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-trace-collect/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-trace-collect/references/dotnet-monitor.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-trace-collect/references/dotnet-trace-collect-linux.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-trace-collect/references/dotnet-trace-collect.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-trace-collect/references/perfcollect.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dotnet-trace-collect/references/perfview.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dump-collect/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dump-collect/references/container-dumps.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dump-collect/references/coreclr-dumps.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/dump-collect/references/nativeaot-dumps.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/eval-performance/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/executing-plans/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/filter-syntax/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/finishing-a-development-branch/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/args.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/cli_test.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/completion.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/config.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/exit_codes.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/flags.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/main.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/output.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/root.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/serve.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/signal.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/assets/examples/version.go` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-cli/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-code-style/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-code-style/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-code-style/references/details.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-concurrency/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-concurrency/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-concurrency/references/channels-and-select.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-concurrency/references/pipelines.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-concurrency/references/sync-primitives.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-context/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-context/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-context/references/cancellation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-context/references/http-services.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-context/references/values-tracing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/references/containers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/references/generics.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/references/map-internals.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/references/pointers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-data-structures/references/slice-internals.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-injection/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-injection/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-injection/references/google-wire.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-injection/references/manual-di.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-injection/references/samber-do.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-injection/references/uber-dig-fx.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/references/auditing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/references/automated-updates.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/references/conflicts.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/references/versioning.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/references/visualization.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-dependency-management/references/workspaces.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/references/architecture.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/references/clean-architecture.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/references/data-handling.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/references/ddd.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/references/hexagonal-architecture.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-design-patterns/references/resource-management.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/.DS_Store` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/assets/templates/CHANGELOG.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/assets/templates/CONTRIBUTING.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/assets/templates/README.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/assets/templates/llms.txt` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/references/application.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/references/code-comments.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/references/library.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-documentation/references/project-docs.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-error-handling/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-error-handling/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-error-handling/references/error-creation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-error-handling/references/error-handling.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-error-handling/references/error-wrapping.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-linter/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-linter/assets/.golangci.yml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-linter/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-linter/references/linter-reference.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-linter/references/nolint-directives.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/references/functions-methods.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/references/identifiers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/references/packages-files.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/references/testing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-naming/references/types-errors.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/alerting.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/dashboards.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/logging.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/metrics.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/profiling.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/rum.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-observability/references/tracing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-popular-libraries/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-popular-libraries/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-popular-libraries/references/libraries.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-popular-libraries/references/stdlib.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-popular-libraries/references/tools.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/assets/.gitignore` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/assets/Makefile` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/references/config.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/references/directory-layouts.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/references/testing-layout.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-project-layout/references/workspaces.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-safety/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-safety/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-safety/references/nil-safety.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-safety/references/slice-map-safety.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/architecture.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/checklist.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/cookies.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/cryptography.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/filesystem.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/injection.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/logging.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/memory-safety.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/network.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/secrets.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/third-party.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-security/references/threat-modeling.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-structs-interfaces/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-structs-interfaces/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-testing/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-testing/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-testing/references/helpers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-testing/references/http-testing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-testing/references/integration-testing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-testing/references/mocking.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/code-review-flags.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/common-go-bugs.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/compilation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/concurrency-debug.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/diagnostic-tools.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/methodology.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/performance-debug.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/pprof.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/production-debug.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang-troubleshooting/references/testing-debug.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-cli` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-code-style` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-concurrency` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-data-structures` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-dependency-injection` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-dependency-management` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-design-patterns` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-documentation` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-error-handling` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-linter` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-modernize` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-naming` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-observability` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-popular-libraries` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-project-layout` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-safety` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-security` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-structs-interfaces` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-testing` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/golang:golang-troubleshooting` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/gopls-lsp/LICENSE` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/gopls-lsp/README.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/grill-me/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/grill-with-docs/ADR-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/grill-with-docs/CONTEXT-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/grill-with-docs/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/including-generated-files/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/incremental-build/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/msbuild-antipatterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/msbuild-antipatterns/references/additional-antipatterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/msbuild-antipatterns/references/incremental-build-inputs-outputs.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/msbuild-antipatterns/references/private-assets.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/msbuild-server/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/multi-reviewer-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/multi-reviewer-patterns/references/review-dimensions.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/parallel-debugging/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/parallel-debugging/references/hypothesis-testing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/parallel-feature-development/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/parallel-feature-development/references/file-ownership.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/parallel-feature-development/references/merge-strategies.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-anti-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-background-jobs/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-code-style/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-configuration/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-design-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-error-handling/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-observability/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-packaging/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-packaging/references/advanced-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-performance-optimization/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-performance-optimization/references/advanced-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-pro/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-project-structure/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-resilience/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-resource-management/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-testing-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-testing-patterns/references/advanced-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python-type-safety/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python:python-anti-patterns` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python:python-code-style` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python:python-configuration` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python:python-design-patterns` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/python:python-error-handling` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/receiving-code-review/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/requesting-code-review/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/requesting-code-review/code-reviewer.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/resolve-project-references/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/run-tests/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/subagent-driven-development/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/subagent-driven-development/code-quality-reviewer-prompt.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/subagent-driven-development/implementer-prompt.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/subagent-driven-development/spec-reviewer-prompt.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:dispatching-parallel-agents` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:executing-plans` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:finishing-a-development-branch` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:receiving-code-review` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:requesting-code-review` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:subagent-driven-development` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:systematic-debugging` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:test-driven-development` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:using-git-worktrees` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:verification-before-completion` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/superpowers:writing-plans` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/CREATION-LOG.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/condition-based-waiting-example.ts` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/condition-based-waiting.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/defense-in-depth.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/find-polluter.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/root-cause-tracing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/test-academic.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/test-pressure-1.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/test-pressure-2.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/systematic-debugging/test-pressure-3.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/tdd/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/tdd/deep-modules.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/tdd/interface-design.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/tdd/mocking.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/tdd/refactoring.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/tdd/tests.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/test-anti-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/test-driven-development/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/test-driven-development/testing-anti-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/test-gap-analysis/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/test-smell-detection/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/test-smell-detection/references/test-smell-catalog.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/to-issues/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/using-git-worktrees/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/using-superpowers/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/using-superpowers/references/codex-tools.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/using-superpowers/references/copilot-tools.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/using-superpowers/references/gemini-tools.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/uv-package-manager/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/uv-package-manager/references/advanced-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/verification-before-completion/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-mstest-tests/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-plans/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-plans/plan-document-reviewer-prompt.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/anthropic-best-practices.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/examples/CLAUDE_MD_TESTING.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/graphviz-conventions.dot` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/persuasion-principles.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/render-graphs.js` | Generated or harness skill material is outside the rename inventory candidate. |
| `.agents/skills/writing-skills/testing-skills-with-subagents.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.beads/.auto-import-issues.jsonl` | Tooling, editor, or generated Beads state is not a product rename candidate. |
| `.claude/backups/.DS_Store.20260524-115710` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-121902` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-122225` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-123401` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-125051` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-125426` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-125936` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-130104` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-131503` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-132550` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-134936` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-135044` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-143239` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-143515` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-170841` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-170847` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-171014` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-172340` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-172346` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-172653` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-172723` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-174708` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-174716` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-174722` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-174728` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-174735` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-174740` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260524-175021` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.DS_Store.20260525-091017` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.claude_settings.local.json.20260518-130258` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/backups/.claude_settings.local.json.20260518-130506` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.claude/skill-manifest.json` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `.codex/skills/analyzing-dotnet-performance/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/async-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/collections-and-linq.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/critical-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/io-and-serialization.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/memory-and-strings.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/regex-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/analyzing-dotnet-performance/references/structural-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/ask-matt/PHASE-BOUNDARIES.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/ask-matt/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/ask-matt/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/code-review/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/code-review/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/codebase-design/DEEPENING.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/codebase-design/DESIGN-IT-TWICE.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/codebase-design/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/codebase-design/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/coverage-analysis/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/coverage-analysis/references/guidelines.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/coverage-analysis/references/output-format.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/coverage-analysis/scripts/Compute-CrapScores.ps1` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/coverage-analysis/skillrun.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/csharp-scripts/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/csharp/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/diagnosing-bugs/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/diagnosing-bugs/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/diagnosing-bugs/scripts/hitl-loop.template.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/docker/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/domain-modeling/ADR-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/domain-modeling/CONTEXT-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/domain-modeling/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/domain-modeling/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/git-pushing/.opencode/commands/telemetry-inspect.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/git-pushing/.opencode/commands/telemetry-report.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/git-pushing/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/git-pushing/scripts/smart_commit.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/git-pushing/skillrun.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-context/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-context/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-context/references/cancellation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-context/references/http-services.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-context/references/values-tracing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/references/containers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/references/generics.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/references/map-internals.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/references/pointers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-data-structures/references/slice-internals.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/assets/templates/CHANGELOG.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/assets/templates/CONTRIBUTING.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/assets/templates/README.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/assets/templates/llms.txt` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/references/application.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/references/code-comments.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/references/library.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-documentation/references/project-docs.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-gopls/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-gopls/references/cli.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-gopls/references/features.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-gopls/references/matrix.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-gopls/references/mcp.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-gopls/references/settings.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-how-to/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-how-to/assets/cursor-go-skills.mdc` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-how-to/references/by-category.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-how-to/references/disambiguation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-how-to/references/project-config.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/assets/prometheus-alerts.yml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/references/caching.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/references/cpu.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/references/io-networking.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/references/memory.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/references/observability.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/golang-performance/references/runtime.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/grill-me/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/grill-me/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/grill-with-docs/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/grill-with-docs/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/grilling/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/grilling/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/handoff/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/handoff/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/implement/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/implement/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/improve-codebase-architecture/HTML-REPORT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/improve-codebase-architecture/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/improve-codebase-architecture/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/assets/configmap-template.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/assets/deployment-template.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/assets/service-template.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/references/deployment-spec.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/references/service-spec.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/k8s-manifest-generator/skillrun.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-compound/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-compound/assets/report-template.html` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-compound/references/claude-code-fallback.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-compound/scripts/extract_exit_plan_mode_outcomes.py` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-setup-goal/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-setup-goal/SKILL.test.ts` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-setup-goal/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-visual-explainer/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-visual-explainer/SKILL.test.ts` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-visual-explainer/references/design-system.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-visual-explainer/references/pr-components.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-visual-explainer/references/svg-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/plannotator-visual-explainer/references/theme-override.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/prototype/LOGIC.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/prototype/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/prototype/UI.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/prototype/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-anti-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-code-style/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-configuration/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-performance-optimization/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-performance-optimization/references/advanced-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-pro/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/python-type-safety/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/research/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/research/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/resolving-merge-conflicts/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/resolving-merge-conflicts/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/domain.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/issue-tracker-github.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/issue-tracker-gitlab.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/issue-tracker-local.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/setup-matt-pocock-skills/triage-labels.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/tdd/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/tdd/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/tdd/mocking.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/tdd/tests.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/teach/GLOSSARY-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/teach/LEARNING-RECORD-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/teach/MISSION-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/teach/RESOURCES-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/teach/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/teach/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/to-questionnaire/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/to-questionnaire/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/to-spec/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/to-spec/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/to-tickets/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/to-tickets/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/triage/AGENT-BRIEF.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/triage/OUT-OF-SCOPE.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/triage/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/triage/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wait-what/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wait-what/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wayfinder/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wayfinder/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wizard/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wizard/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/wizard/template.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/writing-for-agents/SKILL-MECHANICS.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/writing-for-agents/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.codex/skills/writing-for-agents/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/commands/telemetry-inspect.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/commands/telemetry-report.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/async-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/collections-and-linq.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/critical-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/io-and-serialization.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/memory-and-strings.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/regex-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/analyzing-dotnet-performance/references/structural-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/ask-matt/PHASE-BOUNDARIES.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/ask-matt/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/ask-matt/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/code-review/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/code-review/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/codebase-design/DEEPENING.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/codebase-design/DESIGN-IT-TWICE.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/codebase-design/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/codebase-design/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/coverage-analysis/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/coverage-analysis/references/guidelines.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/coverage-analysis/references/output-format.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/coverage-analysis/scripts/Compute-CrapScores.ps1` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/coverage-analysis/skillrun.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/csharp-scripts/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/csharp/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/diagnosing-bugs/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/diagnosing-bugs/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/diagnosing-bugs/scripts/hitl-loop.template.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/docker/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/domain-modeling/ADR-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/domain-modeling/CONTEXT-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/domain-modeling/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/domain-modeling/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/git-pushing/.opencode/commands/telemetry-inspect.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/git-pushing/.opencode/commands/telemetry-report.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/git-pushing/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/git-pushing/scripts/smart_commit.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/git-pushing/skillrun.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-context/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-context/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-context/references/cancellation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-context/references/http-services.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-context/references/values-tracing.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/references/containers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/references/generics.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/references/map-internals.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/references/pointers.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-data-structures/references/slice-internals.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/assets/templates/CHANGELOG.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/assets/templates/CONTRIBUTING.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/assets/templates/README.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/assets/templates/llms.txt` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/references/application.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/references/code-comments.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/references/library.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-documentation/references/project-docs.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-gopls/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-gopls/references/cli.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-gopls/references/features.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-gopls/references/matrix.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-gopls/references/mcp.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-gopls/references/settings.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-how-to/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-how-to/assets/cursor-go-skills.mdc` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-how-to/references/by-category.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-how-to/references/disambiguation.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-how-to/references/project-config.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/assets/prometheus-alerts.yml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/evals/evals.json` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/references/caching.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/references/cpu.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/references/io-networking.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/references/memory.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/references/observability.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/golang-performance/references/runtime.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/grill-me/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/grill-me/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/grill-with-docs/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/grill-with-docs/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/grilling/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/grilling/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/handoff/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/handoff/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/implement/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/implement/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/improve-codebase-architecture/HTML-REPORT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/improve-codebase-architecture/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/improve-codebase-architecture/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/assets/configmap-template.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/assets/deployment-template.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/assets/service-template.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/references/deployment-spec.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/references/service-spec.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/k8s-manifest-generator/skillrun.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-compound/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-compound/assets/report-template.html` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-compound/references/claude-code-fallback.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-compound/scripts/extract_exit_plan_mode_outcomes.py` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-setup-goal/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-setup-goal/SKILL.test.ts` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-setup-goal/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-visual-explainer/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-visual-explainer/SKILL.test.ts` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-visual-explainer/references/design-system.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-visual-explainer/references/pr-components.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-visual-explainer/references/svg-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/plannotator-visual-explainer/references/theme-override.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/prototype/LOGIC.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/prototype/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/prototype/UI.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/prototype/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-anti-patterns/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-code-style/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-configuration/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-performance-optimization/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-performance-optimization/references/advanced-patterns.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-pro/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/python-type-safety/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/research/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/research/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/resolving-merge-conflicts/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/resolving-merge-conflicts/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/domain.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/issue-tracker-github.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/issue-tracker-gitlab.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/issue-tracker-local.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/setup-matt-pocock-skills/triage-labels.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/tdd/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/tdd/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/tdd/mocking.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/tdd/tests.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/teach/GLOSSARY-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/teach/LEARNING-RECORD-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/teach/MISSION-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/teach/RESOURCES-FORMAT.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/teach/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/teach/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/to-questionnaire/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/to-questionnaire/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/to-spec/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/to-spec/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/to-tickets/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/to-tickets/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/triage/AGENT-BRIEF.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/triage/OUT-OF-SCOPE.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/triage/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/triage/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wait-what/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wait-what/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wayfinder/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wayfinder/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wizard/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wizard/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/wizard/template.sh` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/writing-for-agents/SKILL-MECHANICS.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/writing-for-agents/SKILL.md` | Generated or harness skill material is outside the rename inventory candidate. |
| `.opencode/skills/writing-for-agents/agents/openai.yaml` | Generated or harness skill material is outside the rename inventory candidate. |
| `.vscode/settings.json` | Tooling or editor state has no changed product-identity or protected vocabulary evidence. |
| `apm.lock.yaml` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `apm.yml` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `baseline-report-graft.json` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `baseline-report-ringstation.json` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `baseline-report-slackstatus.json` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `baseline-report-stackoverflow-api.json` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `caller-repos.json` | No changed product-identity hunk or protected vocabulary change was found. |
| `cover.html` | Generated report, lock, or analysis output is unrelated checkpoint material. |
| `opencode.jsonc` | No changed product-identity hunk or protected vocabulary change was found. |
| `requirements.lock` | Generated report, lock, or analysis output is unrelated checkpoint material. |

### unresolved

| Path | Rationale |
|---|---|
| `internal/client/devcerts.go` | Certificate identity and rotation contract MUST be decided by the MADR. |
| `scripts/generate-dev-certs.sh` | Certificate identity and rotation contract MUST be decided by the MADR. |

## Explicit Protected Boundaries

- Module imports and the `github.com/poconnor/calm-poc` module identity MUST remain protected.
- `CALMNode`, `calm_node`, `.calm/`, `configs/`, and the FINOS `calm` executable MUST remain protected.
- Historical records, including closed Beads history, dated plans/specifications, and legacy references, MUST remain immutable.
- Generated skills and reports MUST remain outside the active rename candidate.
- Active-candidate files MAY contain protected hunks; only product-identity hunks are candidates and protected hunks MUST NOT replay.
- `patterns/governance.json` is protected as a path; its `$id` is protected, while its title is unresolved pending the MADR.
- Certificate identity and rotation in `internal/client/devcerts.go` and `scripts/generate-dev-certs.sh` are unresolved pending the MADR.

## Unresolved MADR Decisions

- The MADR MUST decide the certificate identity, rotation, and trust-boundary contract for `internal/client/devcerts.go` and `scripts/generate-dev-certs.sh`.
- The MADR MUST decide whether the `patterns/governance.json` title changes; its `$id` MUST remain protected.
- The MADR MUST define compatibility behavior, metadata identity, and the exact active path set before any implementation issue proceeds.

## Reproducibility and Verification

```bash
repo_root="$(git rev-parse --show-toplevel)"
inventory_script="${INVENTORY_SCRIPT:-$HOME/peter_code/scratch_work/agent_fitness_functions_phk6_inventory_code/build_inventory.py}"
inventory_dir="$(dirname "$inventory_script")"
uv venv --allow-existing "$inventory_dir/.venv"
uv pip install --python "$inventory_dir/.venv/bin/python" "ruff==0.16.4" "pyright==1.1.411"
git -C "$repo_root" diff --name-status -M aaf1f440dccb7610a2a66c88c61622d1ea4e3d97..55df0f87fb9af5334d36bcf9148c66817b98c426
"$inventory_dir/.venv/bin/python" "$inventory_script" --repo "$repo_root"
"$inventory_dir/.venv/bin/ruff" check "$inventory_script"
"$inventory_dir/.venv/bin/ruff" format --check "$inventory_script"
"$inventory_dir/.venv/bin/pyright" "$inventory_script"
git -C "$repo_root" diff --check aaf1f440dccb7610a2a66c88c61622d1ea4e3d97..55df0f87fb9af5334d36bcf9148c66817b98c426
```

The generator verifies that every diff record has the expected field shape, every path endpoint is unique, every endpoint has exactly one allowed disposition, disposition counts sum to the complete endpoint count, helpers are active candidates, closed Beads state is historical, mixed files are active candidates, and protected-only files remain protected.

`git diff --check aaf1f440dccb7610a2a66c88c61622d1ea4e3d97..55df0f87fb9af5334d36bcf9148c66817b98c426` evidence: FAIL (exit 2)

```text
.codex/skills/plannotator-setup-goal/SKILL.md:108: trailing whitespace.
+A fact is a simple description of each outcome of a goal. It should be easily testable and verifiable. A fact may describe the function of a specific feature or aspect of a system. A fact may determine specific UI and UX. Again, a fact is literally anything that can be tested and verified in automated or manual testing. Keep fact language simple. In a way, a fact sheet is a design spec, but less verbose & using language the human user can easily visualize & rationalize.
.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1:4: trailing whitespace.
+
.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1:10: trailing whitespace.
+
.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1:24: trailing whitespace.
+- Branch coverage percentage
.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1:86: trailing whitespace.
+
.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1:89: trailing whitespace.
+
.opencode/skills/coverage-analysis/scripts/Extract-MethodCoverage.ps1:162: trailing whitespace.
+
.opencode/skills/plannotator-setup-goal/SKILL.md:108: trailing whitespace.
+A fact is a simple description of each outcome of a goal. It should be easily testable and verifiable. A fact may describe the function of a specific feature or aspect of a system. A fact may determine specific UI and UX. Again, a fact is literally anything that can be tested and verified in automated or manual testing. Keep fact language simple. In a way, a fact sheet is a design spec, but less verbose & using language the human user can easily visualize & rationalize.
```

The diagnostic payload lines are right-trimmed here so this inventory does not reproduce the checkpoint's trailing whitespace.

*Authored By Peter O'Connor with Assistance from OpenCode (openai/gpt-5.6-sol) · 2026-08-20 · calm-poc-phk.6 decision-neutral inventory*
