---
status: accepted
date: 2026-09-04
scope: repository
authors: "Peter O'Connor with OpenCode assistance"
type: decision
supplements: ADR-0006
---

# Multi-Agent Hook Composition and Payload Normalization

## Metadata

| Field | Value |
|---|---|
| Date | 2026-09-04 |
| Status | Accepted |
| Scope | Repository / Client |
| Accountable owner | Peter O'Connor, repository maintainer |
| Authors | Peter O'Connor with OpenCode assistance |
| Type | Decision |
| Context | Extends ADR-0006 to resolve Open Questions regarding Codex and OpenCode integration |

## Context and Problem Statement

ADR-0006 resolved hook composition for Git and Claude Code, but left Codex and OpenCode tool-use hook composition as open questions. In modern multi-agent engineering workflows, developers and autonomous systems utilize multiple AI harnesses (Claude Code, OpenAI Codex CLI, and OpenCode) across the same repositories.

Prior to this decision:
1. `client install-hooks` and `client onboard` only configured `.claude/settings.json` (or `.claude/settings.local.json`), leaving edits in Codex and OpenCode unchecked until Git commit or push time.
2. Hook scripts (`pre-tool-use.sh` and `git-guard.sh`) assumed Claude Code payload schemas (`tool_name`, `tool_input` with snake_case parameters such as `file_path`, `old_string`, `new_string`, `replace_all`), which failed to parse OpenCode's camelCase parameters (`args`, `filePath`, `oldString`, `newString`, `replaceAll`, `cmd`) or raw Codex content structures.
3. Diagnostic checking via `client doctor` only validated Git and Claude Code configurations.

## Decision Drivers

- Provide unified pre-tool-use architecture governance across Claude Code, OpenAI Codex, and OpenCode without requiring bespoke scripts per harness.
- Ensure strict fail-closed security for Git bypass attempts (`git-guard.sh`) and architecture violations (`pre-tool-use.sh`) across all harnesses.
- Preserve existing non-product hooks in harness configurations (e.g., Beads `bd prime` lifecycle hooks in `.codex/hooks.json`).
- Maintain idempotent installation semantics across repeated `client install-hooks` or `client onboard` executions.
- Provide clear diagnostic visibility via `client doctor` for all supported agent harnesses.

## Considered Options

### Option 1: Dedicated bespoke hook scripts per agent harness

Generate and install distinct scripts for each harness (e.g., `agent-fitness-functions-claude.sh`, `agent-fitness-functions-codex.sh`, `agent-fitness-functions-opencode.js`).

**Benefits**
- Each script handles only one JSON payload schema.

**Costs**
- Triplicates verification logic, diff reconstruction, validation payload generation, and error formatting.
- Increases surface area for drift between harness enforcement behaviors.
- Complicates embedded asset management and twin verification tests.

### Option 2 (Selected): Unified normalized hook scripts with harness-specific wiring and lifecycle plugin

Maintain a single canonical `pre-tool-use.sh` and `git-guard.sh` capable of parsing multi-harness payload schemas, wiring Claude Code via `.claude/settings.json`, Codex via `.codex/hooks.json`, and OpenCode via a native `.opencode/plugins/agent-fitness-functions.js` ESM lifecycle plugin.

**Benefits**
- Zero drift between harness validations: all agents execute the exact same underlying validation binary and rules.
- Single source of truth in `hooks/` with byte parity enforced against `internal/client/hookassets/`.
- OpenCode executes seamlessly through native `"tool.execute.before"` plugin interception.
- Codex composes cleanly using native `PreToolUse` definitions without breaking existing `PreCompact` hooks.

**Costs**
- Shell scripts MUST handle schema polymorphism (`tool_input` vs `args`, snake_case vs camelCase).
- OpenCode integration introduces an embedded JavaScript ESM asset.

### Option 3: External process interception proxy

Route tool calls through an external daemon interceptor that watches process executions.

**Benefits**
- Harness-agnostic interception without modifying individual tool configuration files.

**Costs**
- Requires OS-level process tracing (e.g., `ptrace`, eBPF) which is fragile, platform-dependent, and incompatible with sandboxed developer workstations.
- Extreme complexity and failure risk.

## Decision Outcome

1. **Multi-Harness Payload Normalization:**
   - `pre-tool-use.sh` and `git-guard.sh` (and their embedded twins in `internal/client/hookassets/`) **MUST** normalize input payloads at runtime:
     - Top-level vs nested wrappers: **MUST** extract parameters from `tool_input`, `args`, or root JSON dictionaries.
     - Target path keys: **MUST** recognize `file_path`, `filePath`, and `path`.
     - File content & diff reconstruction: **MUST** support raw `content` as well as diff pairs using both `old_string`/`new_string`/`replace_all` and `oldString`/`newString`/`replaceAll`.
     - Command keys: **MUST** extract `command` and `cmd` safely across dictionaries.

2. **OpenAI Codex Hook Composition:**
   - `client install-hooks` **MUST** idempotently update `.codex/hooks.json`.
   - It **MUST** add `PreToolUse` hooks for `Bash` (routing to `agent-fitness-functions-git-guard`) and `Edit|Write` (routing to `agent-fitness-functions-pre-tool-use`).
   - It **MUST** use portable `$(git rev-parse --git-path hooks/<hook-name>)` dynamic resolution.
   - It **MUST** preserve other hook sections (`PreCompact`, `SessionStart`) and non-matching `PreToolUse` entries without clobbering existing configuration.

3. **OpenCode Plugin Lifecycle Interception:**
   - `client install-hooks` **MUST** generate `.opencode/plugins/agent-fitness-functions.js`.
   - The plugin **MUST** implement the OpenCode ESM plugin interface with the `"tool.execute.before"` lifecycle hook.
   - The plugin **MUST** synchronously intercept `bash`, `edit`, `write`, `new_file`, and `newfile` tool calls, delegating to `agent-fitness-functions-git-guard` and `agent-fitness-functions-pre-tool-use` respectively via `git rev-parse --git-path`.
   - On non-zero exit from the hook script, the plugin **MUST** throw an `Error` containing stderr/stdout violation details to halt agent tool execution.

4. **Doctor Diagnostic Extension:**
   - `client doctor` **MUST** check and report status for:
     - Git hooks (`pre-commit`, `pre-push`)
     - Claude Code (`agent git-guard hook`, `agent Edit/Write hook (optional)`)
     - Codex (`codex PreToolUse hooks (optional)`)
     - OpenCode (`opencode plugin (optional)`)
   - Unconfigured harness integrations on single-harness repositories **MUST** report advisory warnings (`warning: true`), avoiding false-positive failures while providing clear remediation instructions (`agent-fitness-functions client install-hooks`).

## Positive Consequences

- Developers using Claude Code, Codex, or OpenCode experience consistent pre-write governance and git-guard enforcement.
- Hook assets remain unified and single-source, with byte parity guaranteed by unit tests (`TestHookScriptsAreByteIdenticalToEmbeddedTwins`).
- `client onboard` automatically arms all three agent harnesses in a single operation.

## Negative Consequences & Trade-Offs

- Hook scripts maintain runtime branching to support polymorphic JSON payload keys across agent harnesses.
- Introducing `.opencode/plugins/agent-fitness-functions.js` adds a JavaScript asset to the client embedded filesystem (`hookassets/`).
