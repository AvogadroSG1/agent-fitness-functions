# CALM PoC — How It Works

This document answers the questions most likely to arise when someone encounters this proof of concept for the first time. It follows the conversation that shaped the implementation.

---

## Language

**stack-fitness-functions**:
The product and binary. The single tool that validates source against architecture fitness functions, in both client and server roles.
_Avoid_: calm-bridge, bridge (as a product name).

**client validate**:
The command that checks one file's fitness functions against the running server. Invoked by hooks at commit time.
_Avoid_: check.

**server start**:
The command that runs the authoritative governance service (container or k8s) that resolves config and runs analyzers.
_Avoid_: serve.

**baseline**:
The offline calibration command that bulk-analyzes a repository to derive thresholds. Belongs to neither client nor server — it is a top-level calibration concern.

**CALM**:
The FINOS Common Architecture Language Model — the external **standard** this tool enforces. Survives in `.calm/config.json`, `configs/`, the FINOS `calm` CLI dependency, and the `calm-poc` module path. Distinct from the product.
_Avoid_: using CALM to name our product or binary.

**Fitness Function**:
A single architectural metric detectable at the file boundary (cyclomatic complexity, interface width, implementation depth, logic density ratio, dependency discipline).

**Validation Request / Validation Result**:
The wire contract spoken by both client and server — the request a client sends and the verdict the server returns. Lives in `internal/fitness`, owned by neither side.
_Avoid_: CheckRequest, CheckResponse.

**Naming rule**:
Always spell the product out — `stack-fitness-functions`. No abbreviations (no `sff`, no `calm-bridge`). Env vars use the derived prefix `STACK_FITNESS_FUNCTIONS_*`; bin helpers use the full name (`stack-fitness-functions-serve`, `stack-fitness-functions-test`). Descriptive over short.

## Relationships

- The **client** sends a **Validation Request** to the **server**; the **server** returns a **Validation Result**.
- Both **client** and **server** depend on the shared **fitness** contract; the contract depends on neither.
- **baseline** calibrates the thresholds the **server** later enforces.

---

## How does CALM actually work?

When a developer runs `git commit` in a governed repository (`graft`, `ringstation`, `slackstatus`), a pre-commit hook fires. The hook identifies the logical repo name, finds every staged source file it recognizes (`.go`, `.py`, `.cs`), and sends each file's content to a running **server** started with `server start`. In container mode, the server resolves governance from mounted `configs/<repo>/config.json`; in local developer mode, the hook can still use the repository's `.calm/config.json` as a sandbox. The server analyzes the file, evaluates the enabled fitness functions against calibrated thresholds, and returns a JSON verdict. In block mode, a failing verdict aborts the commit. In advisory mode, the commit proceeds but the developer sees a warning.

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Hook as pre-commit hook
    participant Config as configs/<repo>/config.json
    participant Server as stack-fitness-functions server
    participant Analyzer as Language Analyzer

    Dev->>Hook: git commit
    Hook->>Config: read enforcement-mode, enabled functions
    loop each staged .go / .py / .cs file
        Hook->>Server: client validate(content, language, repo)
        Server->>Analyzer: parse AST, compute metrics
        Analyzer-->>Server: complexity, LDR, DDC, width, depth
        Server-->>Hook: Validation Result
        alt block mode + violation
            Hook-->>Dev: exit 1 — commit refused, violation printed
        else advisory mode + violation
            Hook-->>Dev: exit 0 — advisory warning printed
        else pass
            Hook-->>Dev: exit 0 — silent success
        end
    end
```

The **server** is the authority. The hook is the enforcement point. The governed repository cannot change thresholds — only which functions are active and what enforcement mode to use.

---

## Can a developer bypass the hook?

Yes. Git hooks are local and unversioned. A developer can delete `.git/hooks/pre-commit` or modify a local `.calm/config.json`. The hook is a **shift-left convenience**, not a security boundary.

A governed repository **cannot weaken enforcement** via a local `.calm/config.json` file. When the hook connects to the containerized server, governance is resolved exclusively from the mounted `configs/<repo>/config.json` inside the container. The local `.calm/config.json` file has no effect on the container layer; it is only consulted when the server is running in local developer sandbox mode (loopback address, no remote flag).

The real enforcement layer sits further right:

- **CI/CD** — the same `client validate` flow runs against every pull request and can fail the build
- **Deploy gate** — a deployment can require CALM attestation or reject builds with outstanding violations in the server's audit log
- **Audit trail** — the server logs every validation, so the *absence* of a validation on a commit is itself a signal

Removing the hook is detectable. The local hook saves the round-trip to CI; it does not replace CI.

---

## How is this different from a linter?

A linter such as `golangci-lint` also checks cyclomatic complexity. For a single repository, the difference is subtle. At the organizational level, it is significant.

| Concern | Linter | CALM |
|---|---|---|
| Who owns the rules | The team (rules live in the repo) | The organization (thresholds live in the stack-fitness-functions server) |
| Who can raise the bar | Any developer with a config edit | The architecture team, explicitly |
| Cross-language consistency | Separate tool per language, separate config per repo | One server, one set of thresholds, Go + Python + C# |
| Audit trail | None — linting leaves no organizational record | Server logs every Validation Result |
| What the rules represent | Code style and common bugs | Architectural principles the organization has committed to |

The practical consequence: a team cannot quietly relax a threshold when their code fails. Any threshold change requires an explicit decision from whoever owns the stack-fitness-functions server. CALM forces the conversation; a linter config edit avoids it.

```mermaid
graph TD
    subgraph org ["Organization Ownership — CALM"]
        OA["Architecture Team"]
        OB["stack-fitness-functions server\nthresholds & governance"]
        OC["graft (Go)"]
        OD["ringstation (Python)"]
        OE["slackstatus (C#)"]
        OA -->|"owns and controls"| OB
        OB -->|"enforces on commit"| OC
        OB -->|"enforces on commit"| OD
        OB -->|"enforces on commit"| OE
    end

    subgraph team ["Team Ownership — Linter"]
        TA["graft team"]
        TB[".golangci.yml\n(in repo)"]
        TC["graft only"]
        TA -->|"owns, can edit freely"| TB
        TB -->|"applies only to"| TC
    end
```

---

## Does CALM need to clone the repository to validate a file?

No. All five fitness functions are intra-file metrics. The server receives raw file content, parses the AST in memory, and returns scores. It never touches the repository on disk.

| Fitness Function | What it measures | Needs cross-file context? |
|---|---|---|
| Cyclomatic Complexity | Branching points within each function body | No |
| Interface Width | Exported method count on types in this file | No |
| Implementation Depth | Average lines of logic per public method | No |
| Logic Density Ratio | Logic lines as a fraction of total lines | No |
| Dependency Discipline | Declared imports actually referenced in this file | No |

The fitness functions were chosen because each is detectable at the file boundary. A function with cyclomatic complexity 15 is too complex regardless of what it calls. An interface with 30 methods is too wide regardless of its implementors.

What these metrics cannot catch: a function that is simple in isolation but orchestrates deep complexity through call chains, or an import that is referenced but architecturally redundant. The design accepts that tradeoff in exchange for speed and simplicity — analysis fast enough for a pre-commit hook.

---

## Quick reference

| Component | Location | Purpose |
|---|---|---|
| `stack-fitness-functions` binary | `/app/stack-fitness-functions` (built from `cmd/stack-fitness-functions`) | CLI for `client validate`, `server start`, and `baseline` |
| Container service | `docker compose up` via `bin/calm-serve` (Docker Desktop) | **Primary runtime** — starts the server on `localhost:7890` |
| Governance rules | `internal/bridge/checker.go`, `governance.json` | Thresholds and enabled functions |
| Pre-commit hook | `hooks/pre-commit.sh` (installed via `scripts/install-hooks.sh`) | Commit-time enforcement in governed repos |
| `configs/<repo>/config.json` | Mounted into the container | Governance config for the logical repo |
| `.calm/config.json` | Optional local repository sandbox | Developer sandbox only — **has no effect on container governance**; container always resolves from `configs/<repo>/config.json` |
| `calm-test` | `~/.local/bin/calm-test` | Ad-hoc file validation without committing |

---

---

## C# Dependency Discipline — calibration status (calm-poc-oeu)

The `client validate` path now resolves project-local namespaces with `--project <csproj>` when a `.csproj` is discoverable from the file being validated. This fixes the root cause of DDC = 0 on files that only imported project-local namespaces (unresolvable without compilation context).

**Baseline command limitation:** The top-level `baseline` command uses `AnalyzeRepository`, which invokes the Roslyn CLI without `--project`. The SlackStatus baseline was regenerated (153 files, 2026-06-04) but still shows P10 DDC = 0 because single-file analysis cannot resolve project-local namespaces during bulk scanning. The distribution shape will improve once `baseline` is updated to pass the nearest `.csproj` for each file — tracked separately.

**Current DDC threshold in `patterns/governance.json`:** `0.8` (unchanged — real calibration requires a project-aware baseline).

**Why `0.8` is still advisory-safe:** The validation path uses project context at runtime, so individual commits that use project-local namespaces will no longer be misclassified as DDC violations. The threshold of `0.8` is strict enough to catch genuinely unused imports; it will not false-positive on project-local namespace usage.

**Next calibration step:** Extend `AnalyzeRepository` to pass `--project <nearest-csproj>` to the Roslyn CLI for each `.cs` file, regenerate baselines, read the resulting P10 DDC distribution, and update the `minimum` in `patterns/governance.json` accordingly.

*Authored By Peter O'Connor with Assistance from Claude Code (claude-sonnet-4-6) · 2026-06-04 · CALM PoC Context & FAQ*
