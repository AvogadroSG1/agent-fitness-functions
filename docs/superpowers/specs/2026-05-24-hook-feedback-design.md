# CALM Hook Feedback: Agent-Readable YAML Violations

**Date:** 2026-05-24
**Status:** Approved for implementation

---

## Problem

The CALM hooks (`pre-tool-use.sh`, `pre-commit.sh`) today emit a raw violation
message string per failure. An agent receiving this output cannot determine the
measured value, the target, whether the failure is blocking, or what to change.
The hooks must provide structured output that an agent can act on without further
inference.

---

## Goal

Replace the current `json_messages()` inline Python in both hooks with a shared
formatter that produces YAML. Every violation block must carry:

1. Pass/fail outcome
2. Measured value
3. Target value and operator
4. Blocking or advisory mode
5. What the metric means and how to fix it

---

## Architecture

```
pre-tool-use.sh ──┐                                  ┌─► stderr (agent reads)
                   ├─► bridge JSON │ format-violations.py ─┤
pre-commit.sh  ──┘                                  └─► stdout → redirected
```

The bridge response does not change. Both hooks pipe the JSON through
`hooks/format-violations.py` instead of the current inline snippet.
The formatter owns all presentation logic and guidance content.

---

## New File: `hooks/format-violations.py`

### Inputs

| Source | Value |
|--------|-------|
| stdin | Raw `CheckResponse` JSON from `calm-bridge` |
| `--mode` | `block` or `advisory` (from the repo's `.calm/config.json`) |
| `--file` | Relative file path (for the YAML header) |

### Output

YAML printed to stdout. The calling hook redirects stdout to stderr so the agent
sees it as part of the tool result. On pass or no violations, the formatter
produces no output.

### YAML shape

```yaml
calm_check:
  file: internal/bridge/checker.go
  status: block

violations:
  - fitness_function: cyclomatic-complexity
    mode: blocking
    result: 15
    target: "<= 10"
    location: "ProcessRequest (handler)"
    meaning: >
      Too many conditional branches make this function hard to test,
      review, and reason about. Each branch is an independent execution
      path through the function.
    remediation:
      - Extract each conditional branch into a named helper function.
      - Replace complex boolean conditions with named predicates.
      - Use early returns (guard clauses) to flatten nested if/else chains.
      - Aim for each function to do one thing.
```

`location` is omitted when no function name is available (file-level metrics).

---

## Guidance Data

Each fitness function carries `meaning`, `remediation` steps, and `operator`
(`<=` or `>=`). The guidance lives in `_load_guidance()` — a function that
returns a dict. Its implementation is currently inline; it is designed to be
replaced by a file loader when the POC graduates.

```python
def _load_guidance() -> dict[str, GuidanceEntry]:
    # POC: inline data. Replace body with file load when externalizing.
    # Future callers: check CALM_GUIDANCE_FILE env var for override path.
    return { ... }
```

This pattern means the call sites never change when externalization happens.

### Fitness function guidance

| Function | Operator | What it measures |
|----------|----------|-----------------|
| `cyclomatic-complexity` | `<=` | Conditional branches per function |
| `interface-width` | `<=` | Public methods on a module |
| `implementation-depth` | `>=` | Average LOC per public method |
| `logic-density` | `>=` | Ratio of functional logic to total lines |
| `dependency-discipline` | `>=` | Ratio of used imports to total imports |

---

## Hook Changes

Both hooks replace the `json_messages()` function and its call sites with a
single pipe to the formatter. The `json_messages` function definition is removed.
The `case "$status"` branches that currently call it are replaced.

**Before (both hooks):**
```bash
json_messages() { ... }          # removed

case "$status" in
  block)
    echo "CALM violation in $file:" >&2
    printf '%s' "$result" | json_messages >&2
    ;;
  advisory)
    echo "CALM advisory for $file:" >&2
    printf '%s' "$result" | json_messages >&2
    ;;
  ...
```

**After:**
```bash
case "$status" in
  block)
    printf '%s' "$result" | python3 "$repo/hooks/format-violations.py" \
      --mode "$status" --file "$file" >&2
    ;;
  advisory)
    printf '%s' "$result" | python3 "$repo/hooks/format-violations.py" \
      --mode "$status" --file "$file" >&2
    ;;
  ...
```

`$status` is already available in both hooks from the bridge response.
The formatter reads it as `--mode` and maps it to the display label (`blocking` /
`advisory`). The leading `echo "CALM violation…"` and `echo "CALM advisory…"`
lines are also removed — the formatter's YAML header replaces them.

---

## Error Handling

The formatter must not cause a false-positive block. A formatter crash is worse
than missing output. All error paths exit 0 with a warning to stderr.

| Condition | Behaviour |
|-----------|-----------|
| `pyyaml` not installed | Print install instruction to stderr; exit 0 |
| Malformed JSON on stdin | Print warning to stderr; exit 0 |
| `yaml.dump` failure | Print plain-text degraded summary; exit 0 |
| Unknown fitness function | Print warning; continue with no guidance block |
| Malformed violation entry | Log entry index to stderr; skip entry; continue |
| Any uncaught exception | Top-level handler prints to stderr; exit 0 |

The formatter never exits non-zero. The hooks continue to own blocking behaviour.

---

## Code Quality Decisions

These decisions emerged from a five-skill parallel review
(anti-patterns, code-style, configuration, design-patterns, error-handling).

### Type annotations

All public functions carry full annotations:

```python
def operator_symbol(fitness_function: str, limit: float) -> str: ...
def format_value(v: float) -> int | float: ...
def main() -> None: ...
```

### Pure output builder

`_build_output()` is a pure function that takes deserialized data and returns
the output dict (or `None` on pass). `main()` owns I/O only — reading stdin,
printing, and calling `sys.exit`.

```python
def _build_output(
    violations: list[dict[str, Any]],
    file: str,
    mode: str,
) -> dict[str, Any] | None: ...
```

### Key normalisation

`_normalise_fn_key(raw: str) -> str` centralises the `_` → `-` mapping between
bridge output and guidance keys. One place to update when the bridge changes.

### Mode label map

```python
_MODE_LABELS: dict[str, str] = {"block": "blocking", "advisory": "advisory"}
```

Replaces the inline ternary. Adding a third mode requires one line here.

### `format_value` safety

```python
def format_value(v: float) -> int | float:
    try:
        if isinstance(v, (int, float)) and float(v).is_integer():
            return int(v)
        return round(float(v), 3)
    except (TypeError, ValueError, OverflowError):
        return v
```

Handles non-numeric bridge output, `float('inf')`, and `float('nan')` without
crashing.

### YAML output order

`sort_keys=False` is intentional. Key order in each violation block is
`fitness_function → mode → result → target → location → meaning → remediation`.
An agent reading top to bottom gets context before action items.

---

## Dependency

`pyyaml>=6` added to `hooks/requirements.txt`. Installation documented under
"Tool Requirements" in the project README.

---

## Out of Scope

- Changing the bridge wire format (`Violation` struct, JSON fields)
- Externalizing guidance to a YAML file (designed for; deferred)
- `calm-test` output changes (already well-formed)
- Per-language guidance variants

---

*Authored by Peter O'Connor with assistance from Claude Code (databricks-claude-sonnet-4-6) · 2026-05-24 · CALM Hook Feedback Design*
