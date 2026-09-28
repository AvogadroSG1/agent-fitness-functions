# C# CALM baseline hardening and C4 fitness implementation plan

Date: 2026-09-16  
Target worktree: `/Users/gcasar/.codex/worktrees/625d/agent-fitness-functions`  
Follow-up issue: `calm-poc-u1uh` (local only)

## Purpose and execution boundary

Complete the client-built C# architecture baseline before adding architectural-level fitness evaluation. Then introduce explicit C4 mappings and advisory component-level evaluation without changing existing single-file governance.

This is an implementation specification and subagent handoff document. Beads remains the task/status tracker; this document is not a completion checklist. Creating this plan does not authorize starting implementation. When implementation starts, the coordinator creates dependent local Beads slices under the follow-up issue and records ownership there.

**Keep all work local.** The user explicitly declined remote synchronization. Do not run `git push`, `bd dolt push`, open PRs, publish artifacts, or send messages externally. Do not commit the existing dirty implementation as part of preparing or reviewing this plan. Implementation agents must not commit, reset, stash, or remove other agents' work; the coordinator owns Git operations within the user's authorization.

Existing uncommitted changes are the starting implementation, not disposable scaffolding. Inspect them before editing. Avoid incidental formatting of unrelated code, especially the existing Roslyn single-file metrics implementation.

## Milestones

| Milestone | Outcome | Release boundary |
| --- | --- | --- |
| A: trustworthy observed baseline | Complete, deterministic C# facts; valid CALM; atomic local storage; safe refresh/onboarding | Required correction to the original delivery plan |
| B: explicit architectural levels | Declared C4 entities, code mappings, hierarchy, evidence-preserving component projection | Separate increment after A is verified |
| C: scoped advisory fitness | Component dependency direction, cycles, and boundary-bypass findings | Separate increment after B; no hook enforcement |
| D: container/system expansion | Runtime/deployment evidence and higher-level controls | Future design only; do not implement in A–C |

Do not make A depend on C4 policy authoring or new graph fitness functions. B and C are follow-on scope, not acceptance requirements for A.

## Findings that motivate milestone A

The reviewed implementation has these concrete gaps:

- Local onboarding invokes C# analysis for every repository.
- `architecture.Validate` does not perform FINOS CALM schema validation.
- The builder emits unmeasured fitness scores of `1` on every node.
- Roslyn omits delegates, loses record identity, misattributes nested-type dependencies, and lacks the promised dependency classifications.
- Constructed generic references can have IDs that do not match declarations; the builder silently drops dangling edges.
- Project-load failures can yield a successful partial scan; successful-process stderr is discarded.
- Invalid explicit `--path` can fall back to the current checkout.
- Relationship IDs depend on sort position; edge evidence is placed under `connects.protocol`; isolated graphs serialize a null relationship collection.
- Handler, client, extraction-contract, and automated end-to-end coverage are missing. The runbook is not updated.

Review evidence: the example contains 384 nodes and 675 relationships and passes CALM CLI 1.40.0 schema checks with 55 isolated-node warnings. A temporary C# fixture with a delegate, record, inheritance, interface implementation, generics, and nested types yielded only `Consumer -> Target` construction/member edges. Passing CALM schema checks therefore does not establish semantic completeness.

## Invariants for every stream

1. The client reads the checkout and runs Roslyn. The daemon receives documents only, never checkout paths or source bodies for repository analysis.
2. Local governance stays plain HTTP on loopback with the implicit `local` caller. Remote/mTLS architecture requests receive an explicit unsupported-mode response after existing authentication.
3. `/check`, the single-file Roslyn JSON contract, hook behavior, violation ledger, and validation-history behavior remain unchanged.
4. The daemon stores one latest accepted architecture per governance repository key. No graph history, CI evidence, automatic hook scans, or visual UI.
5. An extraction, validation, or storage failure preserves the previous accepted document. Incomplete evidence is not a passing architecture result.
6. Stdout from explicit refresh contains only the accepted document. Diagnostics go to stderr. Failed analysis/submission produces no JSON on stdout.
7. Stable facts are deterministic. Timestamps and transient diagnostics do not enter the canonical structural digest.
8. No invented metrics, network protocols, runtime topology, or architectural boundaries.

## Contract gate: coordinator-owned, before parallel implementation

The coordinator owns the initial contract slice (A0). Define the following in a short contract document and executable fixture before starting dependent agents. Agents may propose changes, but may not independently change shared wire shapes.

### Repository analysis contract

Use a versioned language-neutral graph envelope with `schema_version`, `language`, `nodes`, `edges`, and `analysis`. Existing single-file output is unchanged.

- `nodes`: ID, display name, symbol kind, project identity, canonical symbol name, declaration locations. Kinds distinguish class, interface, struct, record class, record struct, enum, and delegate.
- `edges`: source ID, destination ID, dependency kind, and evidence locations. Required kinds: `inherits`, `implements`, `field`, `property`, `parameter`, `return`, `constructs`, and `calls`. Do not collapse these into `member`.
- `analysis`: completeness, analyzed/skipped project identities, target framework/configuration context, and structured diagnostics. External/unresolved dependencies are classified separately from internal facts so deliberate exclusions are distinguishable from extraction failures.
- Locations use repository-relative slash-separated paths and one-based line/column positions. Preserve all partial-type declarations; deduplicate identical locations.
- Collections are arrays, including when empty. Ordering is ordinal and independent of discovery order.

**Canonical identity:** hash an unambiguous tuple of repository-relative project identity, assembly identity, and fully qualified metadata name. Use original generic definitions, preserve case, namespace, nested-type boundaries, and generic arity. Exclude absolute checkout paths. Relocating a checkout preserves IDs; renaming a project or type may change them. Publish cross-language identity vectors and one source of truth for the algorithm. Do not retain competing Go/Roslyn algorithms with different results.

Constructed generic dependencies resolve to definition nodes; recursively emit references to in-repository type arguments and array element types under the relevant dependency kind. Exclude external/framework/package definitions without losing internal arguments. Model target-typed construction and generic/extension method calls through semantic operations. Source ownership is the nearest declared type, not its enclosing type. Keep intra-type recursion outside the type-to-type graph contract.

**Project scope:** support an explicit solution/project selection and deterministic discovery when absent. Never silently select the first of multiple solutions. Either require disambiguation or document an explicit all-project scope. Report target-framework selection; do not claim all-target coverage after analyzing one target. Distinguish intentional directory exclusions from failed project loading. A selected-project load failure or unreliable compilation must fail acceptance by default.

### Graph-to-CALM contract

Recommended Go boundary: `Build(Graph) (Document, error)` and `ValidateDocument(context.Context, []byte) error`, with exact names finalized by A0.

- Emit CALM 1.2 and a project-defined `code-element` node type, with namespaced `agent-fitness-functions` metadata containing language, symbol kind, project, canonical name, locations, and `c4-level: code`.
- Aggregate one directed `connects` relationship per endpoint pair. Metadata contains sorted dependency kinds and evidence records pairing each kind with its locations.
- Relationship IDs derive from canonical endpoints and relationship semantics, not array position. Adding unrelated nodes/edges does not rename existing relationships.
- Source dependency facts belong in relationship `metadata`. Do not use `protocol` for compiler facts or invent a runtime transport.
- No `fitness` field until actual measurements exist. No C#-specific prose in the language-neutral builder.
- Validate duplicate IDs, conflicting node identities, missing endpoints, empty required fields, and unsupported kinds. Merge identical facts deterministically; reject contradictory facts.
- Emit `relationships: []` for isolated graphs. Define empty-node graphs as unsupported refresh results with an actionable diagnostic; isolated nonempty graphs are valid.

Use pinned CALM 1.2 schema resources with deterministic local resolution. Validation must not fetch document-supplied schema URLs. A0 selects one validation implementation using existing repository dependencies where feasible. Verify representative output with the installed FINOS CLI as an independent integration check. Schema validation and graph-integrity validation are separate requirements.

Support all CALM relationship variants accepted by the chosen document API, including `composed-of`; if any profile restriction is necessary, document and test it explicitly. Do not deserialize away valid metadata or future hierarchy fields before storage. Isolated-node lint warnings must not reject valid observed graphs.

### Daemon API and receipt contract

- `PUT /architecture?repo=<governance-key>` validates the whole document before atomic replacement.
- `GET /architecture?repo=<governance-key>` returns current accepted bytes and a content digest/ETag.
- Use the existing repository-key grammar and registration/config lookup. Unknown repositories must not silently create unrelated baselines.
- Architecture storage lives under the machine governance root. Production startup must not silently fall back to a shared temporary directory; tests inject a temporary root.
- PUT returns the accepted representation and its digest. Last completed atomic replacement wins for concurrent writers; never expose partial JSON.
- Preserve the original upload-then-retrieve workflow with a conditional GET bound to the PUT receipt. If a concurrent refresh replaced it, fail explicitly without writing another writer's document to stdout. The coordinator may instead simplify the command to print PUT's accepted representation, but must update the contract and docs together before implementation.
- Define error categories for malformed input, unknown repository, unsupported mode/schema, oversized body, missing baseline, invalid graph, concurrent replacement, and storage failure. Use existing server error conventions; 413 is appropriate for oversized bodies.

### Client lifecycle contract

- Resolve an explicit `--path` strictly. A nonexistent path is an error; never substitute cwd. Support Git worktrees and report moved/missing checkouts clearly.
- Resolve the governance key using existing conventions/configuration; preserve `--repo` override. Require a matching registered configuration rather than treating a folder basename as sufficient proof.
- Reject remote architecture targets before scanning. Preserve established daemon startup/staleness rules: refresh does not acquire a new implicit restart behavior.
- Use a repository-specific timeout/cancellation path, not the single-file runner's 30-second cap. Surface diagnostics from successful and failed analyzer invocations on stderr.
- Non-C# onboarding skips architecture refresh without requiring Roslyn. For C# applicability, use deterministic eligible-project discovery, not a nonexistent language config field.
- Initial local C# onboarding creates a missing baseline. Existing baselines are not implicitly rescanned on repeated onboarding. Explicit refresh remains the update path. Retrieval errors other than a missing baseline must not be treated as permission to replace it.
- Initial C# analysis failure is reported distinctly and returns a failure while preserving completed governance setup; provide the explicit refresh recovery command.

## Milestone A: independently assignable workstreams

| Slice | Owner and files | Depends on | Required evidence |
| --- | --- | --- | --- |
| A0 | Coordinator: graph contracts, schema policy, golden wire fixtures, initial failing contract tests | None | Approved shared shapes and exact commands; no production implementation needed |
| A1 | Roslyn agent: repository extraction in `tools/roslyn-analyzer`, new C# fixture repositories and extraction tests | A0 | Exact nodes/edges for all promised kinds; single-file compatibility |
| A2 | Builder agent: `internal/architecture` and schema fixtures | A0 | Deterministic, truthful, schema-valid CALM; validation negatives |
| A3 | Persistence agent: `internal/server/architecture.go`, related architecture tests | A0 | Local API, genuine validation, preservation, atomicity, auth/mode checks |
| A4 | Client agent: `internal/client/architecture.go`, repository wrapper in `internal/analyzer/csharp.go`, client tests | A0 | Pipe-safe refresh, diagnostics, strict identity/path resolution, receipt matching |
| A5 | Integration owner: onboarding, CLI dispatch, startup wiring, docs and end-to-end tests | A1–A4 | Full local C# flow and unchanged existing governance |

At most three implementation agents run concurrently beside the coordinator. Suggested waves: A1/A2/A3, then A4 while the coordinator integrates completed streams. A3/A4 may develop against the A0 validator/analyzer interfaces with injected fakes; A5 must use real implementations.

Only the integration owner edits shared wiring files: `cmd/agent-fitness-functions/main.go`, `internal/client/onboard.go`, `internal/server/server.go`, and `internal/server/auth.go`. Other owners send small integration requirements rather than editing those files concurrently. A0 owns shared public types until contract freeze; A2 takes ownership afterward.

### A1: extraction implementation and tests

Separate repository analysis from the existing single-file metrics code. Use symbols for declared types and signatures, and Roslyn operations for calls/construction. Filter generated code using both path rules and appropriate generated-source indicators; avoid excluding authored code solely because of a broad name match. Prune build/cache directories during traversal, not only after enumerating everything.

Fixture expectations must include: two referenced projects; class/interface/struct/record class/record struct/enum/delegate; isolated node; nested and partial types; all eight dependency kinds; overloads and generic methods; constructed generics and internal type arguments; target-typed `new`; external/unresolved symbols; generated/build/cache content; deterministic repeat runs; missing/failed project; ambiguous solutions; target-framework selection. Compare canonical expected facts, not just counts or valid JSON. Keep fixtures small, offline-capable, and free of third-party package restore requirements.

### A2: builder and validator implementation and tests

Test shuffled input, duplicate facts, conflicting IDs, missing endpoints, isolated graphs, stable relationship IDs after unrelated insertions, and metadata round-tripping. Add negative CALM documents with missing required fields, invalid endpoint shape, duplicate relationship IDs, unsupported schema, and invalid relationship variants. Test a valid `composed-of` document to prevent the current connects-only validator from becoming a permanent limitation.

Ensure one test independently runs FINOS schema validation; a builder test calling only the builder's own validator is insufficient evidence. Unknown fitness measurements must be absent, not zero or one.

### A3: store and API implementation and tests

Separate filesystem replacement from HTTP parsing so failures can be injected. Test malformed/schema-invalid/integrity-invalid PUT preserving previous bytes; GET before/after replacement; authenticated remote rejection; non-loopback local rejection through existing middleware; unknown/invalid repo; oversized requests; unsupported methods; concurrent readers/writers. Concurrency assertions allow documented last-writer outcomes but require every read to be one complete accepted document. Consider directory fsync for crash durability and document platform guarantees. Do not modify the violation ledger.

### A4: refresh implementation and tests

Inject analyzer execution and transport for focused tests. Cover accepted JSON-only stdout; diagnostics on stderr; missing Roslyn; unknown analyzer contract version; incomplete analysis; cancellation; strict missing path; moved checkout; custom governance key; PUT rejection; GET failure; receipt mismatch; and output write failure. Buffer and validate retrieved bytes before printing to avoid exposing malformed/truncated HTTP responses as accepted JSON. Preserve the explicit error if stdout itself fails mid-write.

### A5: integration and release evidence

Add a committed tiny C# fixture repository with no remote package dependencies. In a temporary checkout and isolated governance root, build the analyzer, onboard, retrieve stored architecture, explicitly refresh to a file, and validate that export with FINOS CALM. Change fixture source and verify explicit refresh changes the appropriate graph facts while unrelated IDs stay stable.

Also prove non-C# onboarding skips Roslyn, repeated onboarding preserves an existing baseline, analysis failure leaves the last baseline intact, and no hook performs a repository scan. Exercise the existing `/check` contract and single-file Roslyn output.

Update the quickstart, operator runbook, engineering specification, CLI help, and any example-generation instructions. Explain working-tree observation, latest-only persistence, scope/completeness, explicit refresh, isolated-node warnings, and the local-only transport boundary. A large third-party example is optional demonstration data, not the acceptance fixture.

## Milestone B: explicit C4 reconciliation

Begin only after A passes and the coordinator opens a separate local implementation slice. Keep observed CALM as the stored baseline; generate reconciled views as separate local artifacts initially. Do not turn the baseline endpoint into a policy or findings store implicitly.

### Modeling contract

| C4 concept | Proposed CALM representation | Evidence/ownership |
| --- | --- | --- |
| System/person | `system` / `actor` nodes | Declared by an architect/operator |
| Container | Appropriate `service`, `webclient`, `database`, etc.; `c4-level: container` | Explicit runtime boundary; deployment evidence later |
| Component | Project-defined `component`; `c4-level: component` | Responsibility and public-boundary declaration |
| Code | Project-defined `code-element`; `c4-level: code` | Observed compiler facts |

These custom types and metadata are this project's convention, not built-in CALM/C4 equivalences. A project/assembly/namespace is a selector or packaging fact, not automatically a C4 container/component. Use `composed-of` for logical containment and `deployed-in` for deployment. Support `details.detailed-architecture` for optional lower-level document links; do not implement remote fetching in this milestone.

Define a versioned local mapping document (proposed `.calm/architecture-map.json`) containing declared entities, stable IDs, parent relations, responsibilities, public boundaries, and selectors over canonical symbol/project/namespace/path. Specify selector matching precisely and make explicit per-symbol overrides take precedence. Ambiguous mappings and unmapped code become diagnostics. Require acyclic containment and one component owner per symbol within a selected runtime context; shared libraries can have different mappings in separate contexts. Never silently infer deployment context.

### Parallel slices

- B0, coordinator: mapping schema, runtime-context semantics, projection contract and example fixture.
- B1, mapping agent: parser/validator, selectors, containment and ambiguity diagnostics in a proposed `internal/architecture/mapping` package.
- B2, projection agent: component graph projection in a proposed `internal/architecture/projection` package, using B0 fixtures.
- B3, integration owner: local view export, docs, and reconciliation end-to-end fixture after B1/B2.

Projection maps each observed edge's endpoints to their owning components. Aggregate cross-component edges while retaining contributing observed edge IDs, kinds, and source locations. Do not expose internal edges as component self-loops. Keep raw direction; do not reinterpret references to interfaces as runtime dispatch or dependency injection resolution. Preserve unused declared entities and distinguish permitted relationships from observed ones. Report mapping coverage in every projected view.

Tests: stable projection under input permutations, multiple code edges becoming one component edge, evidence drill-down, internal edges, ambiguous/unmapped symbols, containment cycles, shared-library contexts, and checkout relocation. A golden example must show a forbidden `API -> Persistence` edge with exact contributing type dependencies.

## Milestone C: advisory architectural fitness

Define a graph-evaluation boundary independent of `/check`. Proposed inputs are observed document digest, mapping digest, selected level/context, policy version, and rule configuration. Proposed output has rule ID, evaluated level, subject node/relationship IDs, status, measured value where meaningful, requirement, and source evidence.

Statuses distinguish pass, violation, and not-evaluable. Incomplete extraction or relevant unmapped evidence cannot produce a passing result. Evaluation remains an explicit local command with no commit/tool-hook wiring or violation-ledger writes. The integration owner defines its CLI contract before parallel rule implementation.

Initial independent rule slices:

| Rule | Scope and metric | Essential tests |
| --- | --- | --- |
| Allowed dependency direction | Component edges violating configured source/destination constraints | Allowed edge, forbidden reverse edge, exact source witnesses |
| Component cycles | Strongly connected components over selected dependency kinds | Acyclic graph, multi-node cycle, internal edges excluded, reproducible cycle witness |
| Public-boundary bypass | Cross-component references to symbols outside a declared public boundary | Public reference allowed, internal reference flagged, unknown mapping not evaluable |

Do not reuse file-level scores as component scores. Define aggregation for each metric: coupling counts distinct architectural dependencies; complexity summaries use actual method measurements and their distribution. Do not invent a universal aggregate fitness score.

Use CALM patterns for structural shape constraints and controls for applicable obligations. Execute graph algorithms in a dedicated evaluator. Export results in separate CALM 1.2 decorators where appropriate, bound to stable element IDs plus architecture/mapping/policy digests. Validate decorator shape and reject or mark stale results when any binding changes. A decorator records evaluator evidence; it does not itself prove that a control was satisfied.

Container/system evaluation requires additional endpoint, deployment, messaging, data-ownership, or runtime evidence. Compiler references alone do not prove network calls, protocols, actual data flows, or absence of external communication. External dependency exclusion in A limits later claims and must remain visible in coverage metadata.

## Coordinator and subagent protocol

Use this dispatch template for each bounded slice:

> Implement slice [ID] of this plan in the assigned worktree. Read AGENTS.md and `bd prime`, inspect existing dirty changes, then read the frozen A0/B0 contracts and relevant fixture expectations. Own only [files/packages]. Depend on [interfaces]. Reach [observable acceptance behavior] and run [targeted tests]. Do not change shared contracts, weaken expected behavior, commit, push, sync Beads remotely, or edit another stream's files. Send proposed contract changes to the coordinator and continue independent work. Report changed files, exact test results, skipped checks, integration needs, and unresolved risks. Keep all issue updates local.

The coordinator supplies focused failing behavioral contracts before implementation. Implementors may add edge-case tests but may not weaken shared assertions to achieve green. For each completed slice, dispatch a bounded specification review and code review; resolve findings before integration. Reviewers report severity, source location, observable failure, and a suggested correction. Do not launch implementation agents merely to prepare this plan.

A handoff must contain the contract version used, changed files, precise validation commands/results, environment limitations, and any API/wiring request. No agent reports the entire milestone complete based only on its own slice.

## Verification commands and stop conditions

Use the target worktree's repo-local caches, as required by AGENTS.md:

```sh
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/architecture ./internal/analyzer ./internal/client ./internal/server ./cmd/agent-fitness-functions
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test . ./configs ./cmd/agent-fitness-functions ./internal/server
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./...
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod CGO_ENABLED=0 go build ./cmd/agent-fitness-functions
```

Run focused tests during each slice; run the required quality gate and full regression suite at integration. Add a focused race test for concurrent store access and execute the new C# end-to-end test with the actual supported SDK and CALM CLI. Verify minimum Go 1.25 with the repository-prescribed toolchain. Do not equate skipped integration tests with passing them.

Known review-environment issues: default Python lacked PyYAML; the Python type-alias test failed; sandbox restrictions initially blocked local test sockets and Roslyn output/build-host pipes. Resolve or clearly separate these from implementation regressions. Set `DOTNET_ROOT` to the actual installed SDK location where needed; do not copy stale machine paths into tracked configuration.

Milestone A is releasable only when every promised node/dependency kind has exact fixture coverage, a real end-to-end refresh produces schema-valid accepted output, failure paths preserve the previous baseline, non-C# onboarding is unaffected, and required regression evidence is available. B/C remain advisory and separately scoped. All artifacts and Beads changes remain local unless the user later changes that instruction.

## Reference material

- [CALM 1.2 schema](https://calm.finos.org/release/1.2/meta/calm.json)
- [CALM nodes and detail links](https://calm.finos.org/core-concepts/nodes/)
- [CALM relationships](https://calm.finos.org/core-concepts/relationships/)
- [CALM controls](https://calm.finos.org/core-concepts/controls/)
- [CALM decorators](https://calm.finos.org/core-concepts/decorators/)
- [C4 abstractions](https://c4model.com/abstractions)
- [C4 container semantics](https://c4model.com/abstractions/container)
- [C4 component semantics](https://c4model.com/abstractions/component)
