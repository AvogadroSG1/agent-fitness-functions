---
title: "CALM PoC: Engineering Technical Specification"
aliases: ["CALM PoC: Engineering Technical Specification"]
linter-yaml-title-alias: "CALM PoC: Engineering Technical Specification"
date created: Sunday, May 18th 2026
date modified: Sunday, May 18th 2026
---

> **Historical document.** This specification describes the PoC local-only architecture
> (single-machine daemon, `.calm/config.json` governance, loopback-only service).
> The current production architecture uses a containerized service with
> `configs/<repo>/config.json` governance mounted at runtime.
> See [CONTEXT.md](../../CONTEXT.md) and [README.md](../../README.md) for current architecture.

# CALM PoC: Engineering Technical Specification

> Context and goals: [why-and-what.md](why-and-what.md)

---

## 1. Overview

This document specifies the engineering design for a PoC system that enforces architectural fitness functions at two interception points: before an AI agent writes a file (Claude Code, OpenAI Codex, and OpenCode tool-use hooks/plugins) and before a developer commits code (git pre-commit hook).

The central component — `agent-fitness-functions` — runs as a Go HTTP daemon. It analyzes proposed source code changes against three fitness functions, translates results into a CALM-compliant architecture document, calls the FINOS `calm` CLI validator, and returns a pass, a block with explanation, or an advisory message. Enforcement behavior is configured per repository.

The system runs entirely locally. No cloud dependencies are required.

> **Note on terminology:** This document uses "CALM node" to mean a component, module, or service in the CALM architecture model. This is distinct from the Stack Internal ubiquitous language definition of "Node" (an atomic unit of knowledge).

---

## 2. System Architecture

```mermaid
graph TD
    subgraph Actors
        DEV[Developer]
        AGENT[AI Agent\nClaude / Codex / OpenCode]
    end

    subgraph "Hook Layer"
        PTU[Agent Pre-Tool-Use\nHooks & Plugins]
        PCH[Git Pre-Commit Hook]
    end

    subgraph agent-fitness-functions
        CLI[CLI Client\nclient validate]
        DAEMON[HTTP Daemon\nlocalhost:7890]
        DISPATCH[Analyzer Dispatcher]
    end

    subgraph "Language Analyzers"
        RADON[radon — Python]
        GOCYCLO[gocyclo — Go]
        ROSLYN[Roslyn — C#]
    end

    subgraph "CALM Layer"
        CALMCLI[FINOS calm CLI]
        GOV[governance.json]
        ARCH[current-architecture.json]
    end

    REPOCFG[.calm/config.json]

    AGENT -->|Edit / Write| PTU
    DEV -->|git commit| PCH
    PTU -->|agent-fitness-functions client validate| CLI
    PCH -->|agent-fitness-functions client validate| CLI
    CLI -->|auto-start if cold\nthen POST /check| DAEMON
    DAEMON --> DISPATCH
    DISPATCH --> RADON
    DISPATCH --> GOCYCLO
    DISPATCH --> ROSLYN
    RADON --> ARCH
    GOCYCLO --> ARCH
    ROSLYN --> ARCH
    DAEMON -->|calm validate| CALMCLI
    CALMCLI --> GOV
    CALMCLI --> ARCH
    CALMCLI -->|violations| DAEMON
    DAEMON --> REPOCFG
    REPOCFG -->|enforcement-mode| DAEMON
    DAEMON -->|block + message\nor advisory| PTU
    DAEMON -->|block + message\nor advisory| PCH
```

### Flow: Synchronous Check (Python, Go)

1. Hook captures proposed file content from tool input (Edit/Write) or staged diff.
2. Hook calls `agent-fitness-functions client validate --file <path> --content <proposed>`.
3. CLI detects daemon running; POSTs to `localhost:7890/check`.
4. Daemon writes proposed content to a temp file and invokes the language analyzer.
5. Daemon builds a `current-architecture.json` fragment for the affected CALM node (module).
6. Daemon calls `calm validate` with `governance.json` and the fragment.
7. Daemon reads `.calm/config.json` for enforcement mode.
8. Daemon returns `{"status":"pass"}`, `{"status":"block","violations":[...]}`, or `{"status":"advisory","violations":[...]}`.
9. Hook exits 0 (pass or advisory) or 1 (block), with violation messages on stderr.

### Flow: Deferred First Call (C#)

1. First hook call on a C# file: CLI detects no running daemon; starts daemon in background.
2. CLI returns immediately with `{"status":"pass","warming":true}`. Hook allows the write.
3. Daemon warms (Roslyn cold start: up to 3 s). Analyzes the file just written.
4. If violations found: daemon stores them in state, keyed by file path.
5. On the next hook call in the same repository: daemon checks state for outstanding violations and blocks regardless of what the new file contains.

---

## 3. Components

### 3.1 agent-fitness-functions

`agent-fitness-functions` is a single Go binary providing two behaviors from one entry point:

- **CLI mode:** invoked by hooks as `agent-fitness-functions client validate [flags]`. Auto-starts the daemon if not running, then delegates via HTTP. The managed-local default address is `http://127.0.0.1:7890`; a loopback request that fails on scheme is retried against the other scheme, so hooks written for either daemon generation keep working during migration.
- **Daemon mode:** HTTP server on `localhost:7890`. Manages analyzer lifecycle, state, and CALM CLI invocation.

**Listen modes** (`server start --listen-mode`, env `AGENT_FITNESS_FUNCTIONS_LISTEN_MODE`; the flag wins). Exactly two values; anything else is rejected at startup.

| Mode | Transport | Caller identity | Path |
|---|---|---|---|
| `mtls` (default) | HTTPS, client certificate required | certificate CN, authorized in `caller-repos.json`; `admins` gates `/configs` and `/shutdown` | Container / remote / production |
| `local-http` | Plain HTTP, **loopback bind only** | implicit `local` for any loopback peer — an admin, authorized for every repo; `caller-repos.json` never consulted | Machine-local developer daemon (ADR-0010) |

`local-http` fails closed rather than degrading: it refuses a non-loopback bind address, explicit server TLS inputs, managed development certificates, and trusted-proxy headers. A non-loopback peer receives 401. An mTLS peer certificate asserting `CN=local` is rejected, and registration never persists that reserved name.

**Daemon endpoints:**

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/check` | POST | authenticated + repo-authorized | Run fitness check on proposed file content |
| `/state` | GET | authenticated + repo-authorized | Return outstanding violation state for a repository |
| `/preflight` | GET | authenticated | Report readiness facts (`authenticated_cn`, `repo_configured`, `repo_config_valid`, `caller_authorized`, `enforcement_mode`) rather than 403/404-ing |
| `/functions` | GET | authenticated, unprivileged, repo-agnostic | The nine-function catalog: description, threshold, operator, unit, default-enabled |
| `/register` | POST | authenticated (admin only to overwrite a differing config) | Self-service create `configs/<repo>/config.json` and bind the caller's CN |
| `/configs` | GET | authenticated + **admin** | Operator inventory of every loaded repo config |
| `/health` | GET | **unauthenticated** | Identity body (`status`, `build_revision`, `build_modified`, `listen_mode`, `configs_dir`, `pid`, `started_at`) so a client can detect a stale daemon before attempting auth. The body shape never varies; a daemon started without an injected identity answers 200 with those fields empty rather than inventing values |
| `/shutdown` | POST | authenticated + **admin** | Graceful shutdown (`{"status":"shutting_down"}`, then `Shutdown` with a 2s drain). 403 for a non-admin caller |

**`/check` request body:**

```json
{
  "repo": "/Users/poconnor/peter_code/graft",
  "file": "internal/parser/parser.go",
  "proposed_content": "...",
  "language": "go",
  "dry_run": false
}
```

`dry_run` marks a **speculative** proposal — an agent's pre-write Edit that may never land, or a `doctor` probe for a file that never existed. The verdict is computed and returned by the identical logic; the only difference is that the repository's outstanding-violation ledger is not written. For a dry run the checker simulates the outstanding set (stored violations, minus any prior entries for this file, plus the proposed ones) instead of persisting. `ValidationResult` carries no `dry_run` field — the response shape is the same either way.

The commit-path hooks (`pre-commit`, `pre-push`) are deliberately **not** dry runs: staged content lands on disk if the commit succeeds, and the repository-wide aggregation is the product's governance model — one file's violation keeps the whole repository blocked until it is fixed (scope decision recorded on `calm-poc-cpvk`).

**Daemon restart.** Nothing restarts the daemon implicitly. `client onboard` compares the `/health` identity against the running binary's expectations across five dimensions — no identity body at all (legacy binary), build revision, build-modified flag, configs directory, listen mode — and on a mismatch takes a machine-wide `restart.lock`, `POST /shutdown`s (retrying the alternate loopback scheme and then managed dev certs, so a pre-ADR-0010 mTLS daemon can be migrated), drains the port, starts a fresh daemon, and re-probes until it observes a current identity. Success is judged by the observed identity, not by which process won the port bind. `doctor` reports staleness as an advisory only; `client validate` never restarts.

**`/check` response:**

```json
{
  "status": "block",
  "violations": [
    {
      "fitness_function": "cyclomatic_complexity",
      "calm_node": "internal/parser",
      "function": "Parse",
      "value": 14,
      "limit": 10,
      "message": "Function 'Parse' has cyclomatic complexity 14, exceeding the limit of 10. Extract conditional branches into separate functions."
    }
  ]
}
```

### 3.2 Language Analyzers

Each analyzer receives a file path (temp file with proposed content), runs analysis, and returns a normalized struct:

```go
type AnalysisResult struct {
    CALMNode     string          // module / package / namespace
    Functions    []FunctionMetric
    ModuleMetric ModuleMetric
    FileMetrics  FileMetric
    Imports      ImportMetric
}

type FunctionMetric struct {
    Name                 string
    CyclomaticComplexity int
    IsPublic             bool
    LOC                  int
}

type FileMetric struct {
    TotalLOC     int
    LogicLOC     int  // for LDR
    PublicMethods int
}

type ModuleMetric struct {
    PublicMethods             int
    TotalLOC                  int
    PrivateLOC                int
    AverageLOCPerPublicMethod float64
}

type ImportMetric struct {
    Total int
    Used  int  // for DDC
}
```

| Language | Approach |
|---|---|
| **Python** | Default synchronous hook path invokes the Python interpreter from the installed `radon` launcher once and uses Radon APIs for cyclomatic complexity and raw metrics; explicit custom `radon` paths retain CLI-compatible `radon cc -j` and `radon raw -j` subprocess behavior |
| **Go** | `github.com/fzipp/gocyclo` as library; `go/ast` for public method count, LOC, imports |
| **C#** | Lightweight Roslyn CLI (built in Step 2) as subprocess; emits JSON matching `AnalysisResult` |

### 3.3 CALM CLI Integration

The daemon calls the FINOS `calm` CLI as a subprocess:

```bash
calm validate \
  --architecture current-architecture.json \
  --pattern governance.json
```

The CALM CLI returns exit code 0 on pass; non-zero with violation JSON on stdout on failure.

> **Note:** The exact `governance.json` schema will be confirmed against FINOS documentation during Step 1. The schemas shown in Section 6 are representative; adjustments SHOULD be expected after Step 1 validation.

### 3.4 Multi-Agent Pre-Tool-Use Hooks & Plugins

Architecture governance integrates with AI agent harnesses via pre-tool-use hooks and plugins:

- **Claude Code (`.claude/settings.json`):** Configured with `PreToolUse` entries for `Bash` (routing to `agent-fitness-functions-git-guard`) and `Edit|Write` (routing to `agent-fitness-functions-pre-tool-use`).
- **OpenAI Codex (`.codex/hooks.json`):** Configured with `PreToolUse` entries mirroring Claude Code using portable `$(git rev-parse --git-path hooks/<name>)` resolution while preserving non-product sections like `PreCompact`.
- **OpenCode (`.opencode/plugins/agent-fitness-functions.js`):** Native ESM plugin registering `"tool.execute.before"` to intercept `bash`, `edit`, `write`, and `new_file` executions synchronously, throwing an `Error` on non-zero hook status to halt agent execution.

Hook scripts (`pre-tool-use.sh` and `git-guard.sh`) normalize multi-harness payload schemas:
- Supports wrappers `tool_input`, `args`, and top-level dictionaries.
- Supports target path keys `file_path`, `filePath`, and `path`.
- For `Write`/`new_file`, the proposed content is `content`. For `Edit`, the hook reconstructs the full proposed file by applying `old_string`/`oldString` → `new_string`/`newString` (respecting `replace_all`/`replaceAll`) to the current on-disk file content; it MUST NOT send the diff fragment as a whole source file.
- `PreToolUse` hooks MUST exit `2` to block tool calls; stderr is surfaced to the agent as the reason.

### 3.5 Git Pre-Commit Hook

Installed at `.git/hooks/pre-commit` in each test repository via `scripts/install-hooks.sh`:

```bash
#!/bin/bash
set -euo pipefail

REPO=$(git rev-parse --show-toplevel)
FILES=$(git diff --cached --name-only --diff-filter=ACM)

for FILE in $FILES; do
  RESULT=$(agent-fitness-functions client validate --file "$FILE" --repo "$REPO" --staged)
  STATUS=$(echo "$RESULT" | jq -r '.status')

  if [ "$STATUS" = "block" ]; then
    echo "CALM violation in $FILE:"
    echo "$RESULT" | jq -r '.violations[].message'
    exit 1
  elif [ "$STATUS" = "advisory" ]; then
    echo "CALM advisory for $FILE:"
    echo "$RESULT" | jq -r '.violations[].message'
  fi
done
```

---

## 4. Test Repositories

| Repository | Language | Enforcement Mode | First-Call Behavior | Analyzer |
|---|---|---|---|---|
| `StackOverflow/StackOverflow.Api.V3` | C# | Block + explanation | Deferred (Roslyn cold start) | Roslyn |
| `SlackStatus` | C# | Block + explanation | Deferred (Roslyn cold start) | Roslyn |
| `graft` | Go | Block + explanation | Synchronous (≤ 500 ms) | gocyclo |
| `ringstation` | Python 3.12 | Advisory | Synchronous (≤ 500 ms) | radon |

Each test repository MUST contain a `.calm/config.json` at its root. See Section 6.3 for the schema.

---

## 5. Fitness Functions

### 5.1 Cyclomatic Complexity

**What it measures:** The number of linearly independent paths through a function — a proxy for logical complexity and testability.

**Unit of analysis:** Per-function. Violations aggregate to the CALM node (module) in the architecture document.

**Threshold:** Set after Step 0 baseline. Starting point for calibration: ≤ 10 (McCabe's original recommendation).

**CALM pattern rule (representative):**

```json
{
  "id": "fitness-cyclomatic-complexity",
  "description": "No function may exceed the cyclomatic complexity threshold.",
  "constraint": {
    "property": "cyclomatic-complexity",
    "operator": "lte",
    "value": "{{CC_THRESHOLD}}"
  }
}
```

**Tooling:** `radon cc -j` (Python), `gocyclo` library (Go), Roslyn analyzer (C#).

**Violation message:**
> `Function 'X' in module 'Y' has cyclomatic complexity N, exceeding the limit of T. Extract conditional branches into separate functions.`

---

### 5.2 Deep vs. Shallow (Two Rules)

Based on John Ousterhout's cost-benefit framework from *A Philosophy of Software Design*: the best modules offer maximum functionality through a minimal interface. Ousterhout provides no mathematical formula for depth; these two proxy rules approximate the concept without misrepresenting it.

**Rule A — Interface Width Ceiling**

Enforces a maximum number of public methods per CALM node. A module with many public methods imposes high cognitive cost on callers regardless of what it does internally.

| Property | Value |
|---|---|
| Unit | Per-module (package / namespace / class) |
| Threshold | Calibrated in `patterns/governance.json`: ≤ 20 public methods. Initial planning value was ≤ 15. |
| Operator | lte |

**Violation:**
> `Module 'X' exposes N public methods, exceeding the limit of T. Consolidate related operations or reduce the public surface area.`

**Rule B — Implementation Depth Floor**

Enforces a minimum average implementation LOC per public method. For this PoC, implementation LOC is analyzer `logic_loc` divided by public method count so comments, imports, and structural scaffolding do not make a pass-through module look deep. Modules with very little logic per public method are likely pass-through facades with no real functionality.

| Property | Value |
|---|---|
| Unit | Per-module |
| Threshold | Calibrated in `patterns/governance.json`: ≥ 0.722 logic LOC per public method. Initial planning value was ≥ 5 LOC per public method. |
| Operator | gte |

**Violation:**
> `Module 'X' averages N LOC per public method, below the minimum of T. Methods with little implementation may be unnecessary pass-throughs.`

Rules A and B fire independently. A module may pass one and fail the other.

---

### 5.3 AI Slop — LDR + DDC

Two complementary metrics that detect hollow or undisciplined AI-generated code. Both are deterministic, fast, and cross-language.

**Logic Density Ratio (LDR)**

$$LDR = \frac{\text{logic\_lines}}{\text{total\_lines}}$$

- **Logic lines:** lines containing arithmetic, control flow, state mutation, or function calls.
- **Excluded:** whitespace, comments, imports, type declarations, and structural scaffolding.
- **Threshold:** LDR ≥ 0.255 (calibrated after baseline). A plummeting LDR in a large generated file signals hollow, boilerplate-heavy output.

**Violation:**
> `File 'X' has a Logic Density Ratio of N (minimum: T). The file may contain excessive boilerplate relative to functional logic.`

**Dependency Discipline Check (DDC)**

$$DDC = \frac{\text{used\_imports}}{\text{total\_imports}}$$

- **Used import:** referenced at least once in the file body.
- **Threshold:** DDC ≥ 0.8 (calibrated after baseline). Agents commonly import libraries they do not use.

**Violation:**
> `File 'X' has a Dependency Discipline ratio of N (minimum: T). Unused imports: [list].`

**Future iteration (v2):** Jensen-Shannon Divergence on AST node histograms for structural clone detection across the codebase.

---

### 5.4 Generalized Fitness Functions

Four additional fitness functions generalize the PoC from a fixed five-metric set to a
config-driven catalog. They were derived from the architecture fitness function
catalog of Observatory, a governed data-pipeline repository whose
`docs/arch-fitness-functions.md` specifies ten repo-specific functions (AFF-01
layer sovereignty, AFF-02 temporal purity, AFF-03 SQL composition, AFF-06
deterministic ordering are the four that generalize). Unlike 5.1–5.3, each of these four counts occurrences of a specific pattern
per file and enforces an **lte-0 rule**: zero occurrences pass, one or more violate.
All four are opt-in (`false` by default in `fitness-functions`) — a repository enables
them explicitly, typically after an advisory-only rollout period (see
[docs/threshold-exceptions.md](../threshold-exceptions.md) for the detection-scope
caveats that motivate advisory-first).

**Scoring location.** The five functions in 5.1–5.3 are metric-scored: a language
analyzer computes a number (complexity, method count, LOC ratio) and the checker
compares it against a threshold. The four functions here are scored differently, split
across two mechanisms:

- **Checker content-scored** (`internal/server/content_scoring.go`, the
  `contentScorers` registry) — the scorer reads the proposed file content or the
  analyzer's structured findings directly, rather than a single numeric metric.
  `layer-sovereignty` and `deterministic-ordering` fall here; both are pattern/text
  based — the matcher never parses language syntax. A request MUST still declare a
  supported `language` (`go`, `python`, `csharp`) to reach the checker at all, and
  the shipped hooks only submit `.go`/`.py`/`.cs` files, so today these two
  functions run against exactly the same file set as the other seven.
- **Analyzer findings-scored** — the language analyzer emits `analyzer.Finding`
  records (`Rule`, `Kind`, `Line`) during its normal AST walk, and the checker counts
  findings tagged with the function's rule name. `temporal-purity` and
  `sql-composition-safety` fall here; both are implemented today only in the Python
  AST scan (`internal/analyzer/python.go`). A language whose analyzer emits no
  findings for a rule contributes zero findings and therefore no violation — Go and
  C# files silently pass both today.

**Wire name vs. config key.** Every fitness function has two names: the config key
(kebab-case, e.g. `layer-sovereignty`) used in `fitness-functions` and
`fitness-function-settings`, and the wire name (snake_case, e.g. `layer_sovereignty`)
on `Violation.FitnessFunction` in the `internal/fitness` contract and in generated
architecture documents. This mirrors the existing five functions' convention.

**exclusiveMaximum encoding.** `patterns/governance.json` encodes each lte-0 rule as
`"type": "number", "exclusiveMaximum": 1` rather than `"maximum": 0`. The FINOS
`calm` CLI's `pattern-has-no-empty-properties` rule rejects a pattern property whose
value is the zero value for its type, so a literal `0` maximum is not expressible.
`exclusiveMaximum: 1` is equivalent to `lte 0` for integer-valued counts (the only
values these four functions ever produce) without tripping that rule. A file with zero
violations has the count omitted entirely from its generated architecture document
(`omitempty` on the CALM node's fitness metadata) and is absent from the pattern's
`required` list for these four keys — the document simply doesn't assert a value for a
function that found nothing to report, and CALM validation passes.

#### Layer Sovereignty (`layer-sovereignty` / `layer_sovereignty`)

**What it measures:** References to forbidden content patterns from within a file that
belongs to a configured architectural layer — e.g. a bronze-layer data file directly
referencing `silver.` or `gold.` objects instead of going through the layer's
sanctioned interface.

**Unit of analysis:** Per-file. A file is evaluated against every configured layer
whose `paths` glob matches it; matches across layers are combined into one violation
count for the file.

**Scoring:** Checker content-scored (`layerSovereigntyViolations` in
`content_scoring.go`). The scan is syntax-blind text matching, but requests are
gated on a supported `language`, so it runs only for `.go`/`.py`/`.cs` files today.

**Config:** `fitness-function-settings.layer-sovereignty.layers`, a list of
`{name, paths, forbidden-patterns}` objects. `paths` are glob patterns compiled at
config-parse time (`**` crosses `/`, `*` does not, `?` matches one non-`/` character,
every other character is literal); `forbidden-patterns` are Go `regexp` source strings
compiled at the same time. Compilation is fail-closed: an invalid glob or regex fails
config parsing, and the whole config is rejected rather than partially applied.
**Enabling `layer-sovereignty` with an empty or absent `layers` list is itself a hard
config error** — there is no meaningful "on" state with nothing to enforce. See
[docs/runbooks/onboard-new-repository.md](../runbooks/onboard-new-repository.md) for a
complete config example.

**Onboarding:** `client onboard --functions` explicitly **rejects** `layer-sovereignty`
— layers must be hand-authored in `fitness-function-settings`, which onboarding has no
way to infer from a bare repository. `baseline`'s offline threshold-delta report also
skips this function; it requires content-scoring against a live repository checkout,
not just AST metrics.

**Violation message (representative):**
> `File "X" belongs to layer(s) bronze, which must not reference N forbidden pattern(s) (limit 0): [...]. Route access through the layer's sanctioned interface instead of referencing these patterns directly.`

#### Deterministic Ordering (`deterministic-ordering` / `deterministic_ordering`)

**What it measures:** SQL window functions (`OVER (... ORDER BY ...)`) whose `ORDER
BY` clause carries no tie-breaker column, so row order is not fully deterministic when
the primary ordering expression has ties.

**Unit of analysis:** Per-file. Every window function's `OVER (...)` body is
extracted by walking characters from the opening paren to its balanced closing paren
(Go's `regexp` is RE2 and cannot balance parentheses itself), and the clause after the
last paren-depth-zero `ORDER BY` inside that body is checked for a tie-breaker token.

**Scoring:** Checker content-scored (`deterministicOrderingViolations`). Textual —
it scans SQL embedded in string literals without parsing the host language, though
the request pipeline still only accepts `.go`/`.py`/`.cs` files today.

**Config:** `fitness-function-settings.deterministic-ordering.tie-breaker-tokens`,
a list of identifier tokens (default `["_id", "_key", "_pk", "_sk", "id"]`). A token
matches a whole SQL identifier in the `ORDER BY` clause, or an identifier's `_`-boundary
suffix (`employee_id` satisfies `id` and `_id`) — never a bare substring, so
`paid_amount` does not satisfy `id`.

**Onboarding:** `client onboard --functions` accepts `deterministic-ordering` (no
settings required to enable it; the default tie-breaker tokens apply). `baseline`'s
offline report skips it for the same reason as `layer-sovereignty` — SQL text scanning
against a live checkout isn't part of AST-based analyzer metrics.

**Violation message (representative):**
> `File "X" has N window function ORDER BY clause(s) without a unique tie-breaker (limit 0): [...]. Append a unique column such as the primary key to each ORDER BY so the row order is deterministic.`

#### Temporal Purity (`temporal-purity` / `temporal_purity`)

**What it measures:** Timestamps constructed without an explicit time zone —
`datetime.utcnow()` and argument-less `.now()` calls in Python.

**Unit of analysis:** Per-file, counting analyzer findings tagged `temporal-purity`.

**Scoring:** Analyzer findings-scored (`temporalPurityViolations` reads
`analyzer.Finding` records tagged `temporal-purity`). Implemented today in the Python
embedded AST scan (`internal/analyzer/python.go`, `_temporal_purity`).

**Detection scope — read before enabling in block mode:** the Python scan is
**attribute-based with no import resolution.** It flags any `X.utcnow()` call and any
argument-less `X.now()` call regardless of what `X` actually is — `datetime.utcnow()`
and `time.time()`-adjacent `.now()` calls are flagged the same as `arrow.now()` or a
project's own custom `.now()` method. This is inherited Observatory semantics, not a
bug: it trades false positives for not having to resolve imports. See
[docs/threshold-exceptions.md](../threshold-exceptions.md) for the full write-up and
the advisory-first recommendation this implies.

**Config:** `fitness-function-settings.temporal-purity.csharp-policy` (`naive-only`
default, or `require-offset`) is **reserved for a future C# temporal-purity milestone**
— the setting parses and validates today, but no C# analyzer detection exists yet, so
it currently has no runtime effect.

**Onboarding:** `client onboard --functions` accepts `temporal-purity`. `baseline`
produces a findings-count delta row for it (see
[docs/threshold-calibration.md](../threshold-calibration.md)) —
computable offline because it's a pure AST-finding count, unlike the two content-scored
functions above.

**Violation message (representative):**
> `File "X" constructs N naive timestamp(s) (limit 0): [...]. Construct timestamps with an explicit time zone, such as datetime.now(timezone.utc), so the recorded instant is unambiguous.`

#### SQL Composition Safety (`sql-composition-safety` / `sql_composition_safety`)

**What it measures:** SQL statements composed via f-string interpolation,
`%`-formatting, or `.format()` and passed as the first argument to `.execute()` or
`.executemany()`.

**Unit of analysis:** Per-file, counting analyzer findings tagged
`sql-composition-safety`.

**Scoring:** Analyzer findings-scored (`sqlCompositionSafetyViolations`). Implemented
today in the Python embedded AST scan (`_sql_composition`).

**Detection scope — read before enabling in block mode:** matching is
**receiver-agnostic** — any object's `.execute()`/`.executemany()` call is inspected,
not just recognized DB-API clients, so a false positive is possible on an unrelated
`.execute()` method. Two gaps carried over deliberately for parity with Observatory's
original detector: **variable indirection** (`query = f"..."; cursor.execute(query)`)
and **plain string concatenation** (`cursor.execute("SELECT " + col)`) are **not**
detected — only the three composition forms named above, applied directly as the call
argument, are caught. See
[docs/threshold-exceptions.md](../threshold-exceptions.md) for the full write-up.

**Onboarding:** `client onboard --functions` accepts `sql-composition-safety`.
`baseline` produces a findings-count delta row for it, same as `temporal-purity`.

**Violation message (representative):**
> `File "X" composes N SQL statement(s) via string interpolation (limit 0): [...]. Use parameterized queries or a SQL composition API instead of building statements with f-strings, %-formatting, or .format().`

---

## 6. Data Schemas

### 6.1 governance.json

The central CALM pattern file. Defines all fitness function rules applied across all repositories. Per-repository enforcement mode lives in `.calm/config.json`, not here — the governance rules are identical for all repositories; only behavior differs.

```json
{
  "$schema": "https://raw.githubusercontent.com/finos/architecture-as-code/main/calm/draft/2024-04/meta/pattern.json",
  "title": "CALM PoC Fitness Functions",
  "version": "0.1.0",
  "fitness-functions": {
    "cyclomatic-complexity": {
      "description": "Maximum cyclomatic complexity per function",
      "threshold": "{{CC_THRESHOLD}}",
      "operator": "lte",
      "unit": "function"
    },
    "interface-width": {
      "description": "Maximum public method count per CALM node",
      "threshold": "{{IW_THRESHOLD}}",
      "operator": "lte",
      "unit": "module"
    },
    "implementation-depth": {
      "description": "Minimum average implementation LOC per public method",
      "threshold": "{{ID_THRESHOLD}}",
      "operator": "gte",
      "unit": "module"
    },
    "logic-density": {
      "description": "Minimum logic density ratio per file",
      "threshold": "{{LDR_THRESHOLD}}",
      "operator": "gte",
      "unit": "file"
    },
    "dependency-discipline": {
      "description": "Minimum used-import ratio per file",
      "threshold": "{{DDC_THRESHOLD}}",
      "operator": "gte",
      "unit": "file"
    }
  }
}
```

`{{THRESHOLD}}` placeholders are replaced with values derived from `baseline-report.json` at the end of Step 0.

### 6.2 current-architecture.json

Generated by the `agent-fitness-functions` server per analysis run. Represents the proposed state of one CALM node (module) as node metadata in a CALM architecture document.

```json
{
  "$schema": "https://calm.finos.org/release/1.2/meta/calm.json",
  "nodes": [
    {
      "unique-id": "parser",
      "node-type": "service",
      "name": "parser",
      "metadata": {
        "fitness": {
          "cyclomatic-complexity": 14,
          "interface-width": 3,
          "implementation-depth": 60,
          "logic-density": 0.75,
          "dependency-discipline": 1.0
        },
        "module_metrics": {
          "public_method_count": 3,
          "total_loc": 210,
          "private_loc": 123,
          "avg_loc_per_public_method": 60
        },
        "file_metrics": {
          "total_lines": 240,
          "logic_lines": 180,
          "ldr": 0.75
        },
        "import_metrics": {
          "total_imports": 5,
          "used_imports": 5,
          "ddc": 1.0
        }
      }
    }
  ],
  "relationships": []
}
```

### 6.3 .calm/config.json

Per-repository configuration. Declares enforcement mode, daemon connection, and which fitness functions are active for this repository at the current step.

```json
{
  "version": "0.1.0",
  "repo": "graft",
  "language": "go",
  "enforcement-mode": "block",
  "daemon": {
    "host": "localhost",
    "port": 7890,
    "startup-timeout-ms": 500
  },
  "fitness-functions": {
    "cyclomatic-complexity": true,
    "interface-width": false,
    "implementation-depth": false,
    "logic-density": false,
    "dependency-discipline": false
  }
}
```

`ringstation` uses `"enforcement-mode": "advisory"`. Fitness functions are toggled per-repository per-step — only cyclomatic complexity is enabled at Step 1.

---

## 7. Enforcement Model

### Modes

| Mode | Daemon Response | Hook Exit Code | Experience |
|---|---|---|---|
| `block` | `{"status":"block","violations":[...]}` | Git pre-commit: 1; Claude PreToolUse: 2 | Write or commit rejected. Violation message shown. Must fix before proceeding. |
| `advisory` | `{"status":"advisory","violations":[...]}` | 0 | Write proceeds. Advisory message shown. Agent or developer may self-correct. |
| `off` | `{"status":"pass"}` | 0 | No check performed. |

### Language-Specific Startup Behavior

| Language | First-call behavior | Condition |
|---|---|---|
| Python | Synchronous | Radon API fast path plus CALM validation stays below 500 ms on the target machine |
| Go | Synchronous | `startup-timeout-ms` not exceeded |
| C# | Deferred on first call; synchronous from second call onward | Roslyn cold start may exceed timeout |

### Python Latency Decision

The Python hook path MUST keep Radon analysis in a single subprocess by invoking the Python interpreter from the installed `radon` launcher and using Radon APIs for cyclomatic complexity and raw metrics together. The legacy CLI-compatible path still exists for explicit custom `radon` paths and fallback behavior.

The target-machine profile for the optimized path showed five consecutive real `/check` samples at `439.607459ms`, `425.79225ms`, `432.336625ms`, `439.290042ms`, and `436.524791ms`. Phase profiling showed the new Radon API analysis at `59.086625ms`, compared with legacy `radon cc` plus `radon raw` subprocess timings of `86.1875ms` and `123.440459ms`; `calm validate` remained the dominant phase at `350.604084ms`. Because the FINOS CALM CLI is an external Node.js subprocess, occasional host-level startup jitter can still produce isolated samples above 500 ms. The PoC decision is to log five-run latency evidence and keep functional integration gates deterministic rather than fail normal test runs on wall-clock jitter. A hard latency SLO SHOULD be revisited with a long-lived CALM validation service or in-process validator if this moves beyond PoC.

### Outstanding Violation State

The daemon maintains an in-memory map of `repo → []violation`. Any hook call against a repository with outstanding violations in `block` mode MUST return a block, regardless of whether the new file itself violates a rule. The block message lists outstanding violations and their locations.

This design prevents the agent from accumulating unresolved debt before addressing it.

---

## 8. Milestone Plan

### Step 0 — Baseline

**Goal:** Establish the current metric state of all four repositories before writing any rules. Thresholds MUST NOT be set without this data.

**Actions:**

1. Run language analyzers against all four repositories in their current state.
2. Record per-function cyclomatic complexity distributions, public method counts, LOC ratios, and import usage rates.
3. Set thresholds at the 90th percentile of current measurements — tight enough to catch new slop, loose enough to avoid flagging existing clean code.
4. Note any **known exceptions** (e.g., a legacy function with CC = 47) and exclude them from threshold calculation. Known exceptions will not trigger blocks on existing code but WILL trigger blocks if new changes worsen them.
5. Replace `{{THRESHOLD}}` placeholders in `governance.json`.

**Output:** `baseline-report.json` × 4, `governance.json` with concrete thresholds.

---

### Step 1 — Define World Rules

**Goal:** Author a working `governance.json` with cyclomatic complexity as the single active rule. Confirm the FINOS CALM CLI validates correctly.

**Actions:**

1. Install FINOS `calm` CLI: `npm install -g @finos/calm-cli@1.40.0`.
2. Author `governance.json` with only `cyclomatic-complexity` enabled.
3. Hand-craft a `current-architecture.json` that passes and one that fails the rule.
4. Run `calm validate` against both. Confirm expected outcomes.
5. Refine `governance.json` schema to match actual FINOS CLI requirements.
6. Add `.calm/config.json` to each test repository.

**Output:** Working `governance.json`, confirmed CALM CLI integration, per-repository configs.

---

### Step 2 — Build agent-fitness-functions

**Goal:** A working `agent-fitness-functions` binary that analyzes real files from all four test repositories and produces correct pass/fail results against Step 1 rules.

**Actions:**

1. Scaffold Go module with HTTP daemon and CLI entry point.
2. Implement Python analyzer (Radon API subprocess with CLI-compatible fallback).
3. Implement Go analyzer (gocyclo library).
4. Implement C# analyzer (Roslyn subprocess CLI).
5. Implement `current-architecture.json` builder.
6. Wire daemon to CALM CLI subprocess.
7. Implement per-repository config reader and enforcement mode routing.
8. Implement outstanding violation state map.
9. Write unit tests using scripted violation fixtures — one per language per fitness function.
10. Run against all four repositories manually. Verify zero false positives post-baseline.

**Output:** `agent-fitness-functions` binary, unit tests passing, clean baseline run across all four repositories.

---

### Step 3 — AI Integration Loop

**Goal:** An agent detects a cyclomatic complexity violation and self-corrects without human intervention.

**Actions:**

1. Install the Claude Code pre-tool-use hook in each test repository.
2. Install the git pre-commit hook in each test repository via `scripts/install-hooks.sh`.
3. Task the agent with a feature that will cause a CC violation in `graft` (Go, block mode).
4. Observe the agent receive the block, read the violation message, and refactor.
5. Verify the commit proceeds cleanly after self-correction.
6. Run the scripted red-green demo manually in block mode (C#) and advisory mode (`ringstation`).

**Success gate:** Agent self-corrects without human intervention. Pre-commit hook blocks a commit containing a known violation.

---

### Step 4 — Iteration and Expansion

**Goal:** Activate Deep vs. Shallow and AI Slop fitness functions. Tune thresholds based on Step 3 observations.

**Actions:**

1. Enable `interface-width` and `implementation-depth` in each repository's `.calm/config.json`.
2. Implement LDR and DDC analyzers in the server.
3. Enable `logic-density` and `dependency-discipline` in config.
4. Re-run the scripted red-green demo for each new fitness function.
5. Adjust thresholds if the false positive rate exceeds one per ten agent writes.
6. Document findings: which metrics are most effective, which thresholds need tuning, and how malleable enforcement performed across modes.

---

## 9. Threshold Calibration Strategy

Thresholds are always derived from the Step 0 baseline — never set in advance.

**Formula:**

```
threshold = percentile(baseline_measurements, 90)
```

The 90th percentile sets a ceiling that existing clean code comfortably passes (90% compliance) while catching new slop that exceeds current norms.

**Known exceptions:** Any module where existing code already exceeds a reasonable threshold is recorded in `baseline-report.json` and excluded from the percentile calculation. Known exceptions suppress blocks on existing code. They do not suppress blocks when new changes worsen the metric.

---

## 10. Repository Layout

```
calm-poc/
├── cmd/
│   └── agent-fitness-functions/
│       └── main.go              # CLI entry point, daemon auto-start logic
├── internal/
│   ├── analyzer/
│   │   ├── python.go            # Radon API fast path and CLI fallback
│   │   ├── golang.go            # gocyclo library integration
│   │   ├── csharp.go            # Roslyn subprocess wrapper
│   │   └── analyzer.go          # AnalysisResult types, dispatcher interface
│   ├── server/
│   │   ├── server.go            # HTTP daemon (port 7890)
│   │   ├── checker.go           # Fitness function dispatch and aggregation
│   │   └── state.go             # Outstanding violation state map
│   ├── calm/
│   │   ├── pattern.go           # governance.json loader and schema types
│   │   └── validator.go         # calm CLI subprocess wrapper
│   └── report/
│       └── architecture.go      # current-architecture.json builder
├── patterns/
│   └── governance.json          # CALM fitness function rules (central, all repositories)
├── configs/
│   ├── block-template.json      # .calm/config.json template for block mode
│   └── advisory-template.json   # .calm/config.json template for advisory mode
├── fixtures/
│   └── violations/
│       ├── python/              # Known-bad .py files per fitness function
│       ├── go/                  # Known-bad .go files per fitness function
│       └── csharp/              # Known-bad .cs files per fitness function
├── hooks/
│   ├── pre-tool-use.sh          # Claude Code PreToolUse hook
│   └── pre-commit.sh            # Git pre-commit hook
├── scripts/
│   ├── baseline.sh              # Step 0: run analyzers, produce baseline-report.json
│   └── install-hooks.sh         # Install pre-commit hook into a target repository
├── docs/
│   └── spec/
│       ├── why-and-what.md      # Goals, non-goals, success criteria
│       └── engineering-spec.md  # This document
└── go.mod
```

---

## 11. Demo Runbook

The scripted red-green demonstration validates each fitness function independently and proves the guardrails work for human developers — not just agents.

**Per fitness function, per enforcement mode:**

1. **Red — introduce the violation:**
   Copy the relevant file from `fixtures/violations/<language>/` into the test repository. Stage it with `git add`. Attempt `git commit`.
   *Expected:* Pre-commit hook fires. Exit code 1. Violation message printed to stderr. Commit blocked.

2. **Green — fix and resubmit:**
   Refactor the file to satisfy the fitness function threshold. Re-stage. Attempt `git commit` again.
   *Expected:* Pre-commit hook fires. Exit code 0. Commit succeeds.

3. **Advisory variant** (run against `ringstation` only):
   Repeat step 1 with the violation file.
   *Expected:* Pre-commit hook fires. Exit code 0. Advisory message printed to stderr. Commit proceeds.

Run this sequence for: Cyclomatic Complexity, Interface Width, Implementation Depth, LDR, DDC. Implementation Depth is demonstrated with C# only under the calibrated `0.722` threshold because the current Go and Python analyzers cannot produce a real source fixture below that threshold without fake analyzer data.

Run in: `graft` (Go, block), `SlackStatus` (C#, block), `ringstation` (Python, advisory).

This demonstration proves three things in sequence: the fitness function correctly identifies a violation; the block mechanism prevents bad code entering history; and the fix path is navigable — the guidance message is actionable.

---

*Authored By Peter O'Connor with Assistance from Claude Code (databricks-claude-sonnet-4-6) · 2026-05-18 · CALM PoC Engineering Technical Specification*
