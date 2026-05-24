# CALM Hook Feedback: Agent-Readable YAML Violations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the raw message output in CALM hooks with YAML violation blocks that give an agent the measured value, target, enforcement mode, and actionable remediation guidance.

**Architecture:** A new `hooks/format-violations.py` script reads bridge JSON on stdin and emits structured YAML to stdout. Both hooks pipe their bridge result through this formatter instead of the current inline `json_messages()` Python snippet. The formatter is fail-open: any internal error exits 0 to prevent false-positive blocks.

**Tech Stack:** Python 3 (stdlib: argparse, json, sys, importlib), PyYAML 6+, pytest 8+, Go (existing test harness in `hooks/`)

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `hooks/requirements.txt` | Create | Python dependency declaration |
| `hooks/test_format_violations.py` | Create | Unit + integration tests for the formatter |
| `hooks/format-violations.py` | Create | The formatter: parses bridge JSON, emits YAML |
| `hooks/pre_tool_use_test.go` | Modify | Update output assertions for new YAML format |
| `hooks/pre-tool-use.sh` | Modify | Replace `json_messages` with formatter pipe |
| `hooks/pre_commit_test.go` | Modify | Update output assertions for new YAML format |
| `hooks/pre-commit.sh` | Modify | Replace `json_messages` with formatter pipe |
| `README.md` | Modify | Add `python3 -m pip install -r hooks/requirements.txt` to Tool Requirements |

---

## Task 1: Add hooks/requirements.txt

**Files:**
- Create: `hooks/requirements.txt`

- [ ] **Step 1: Create the file**

```
pyyaml>=6
pytest>=8
```

- [ ] **Step 2: Install the dependencies**

```bash
python3 -m pip install -r hooks/requirements.txt
```

Expected: output ends with `Successfully installed` or `already satisfied` lines.

- [ ] **Step 3: Verify pyyaml is importable**

```bash
python3 -c "import yaml; print(yaml.__version__)"
```

Expected: prints a version like `6.0.2` (no error).

- [ ] **Step 4: Commit**

```bash
git add hooks/requirements.txt
git commit -m "chore(hooks): add Python requirements for format-violations formatter

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 2: Write failing Python tests (red phase)

**Files:**
- Create: `hooks/test_format_violations.py`

- [ ] **Step 1: Create the test file**

`hooks/format-violations.py` does not exist yet. The module loader at the top
of this file will raise `FileNotFoundError` when pytest collects the tests.
That is the expected red state.

```python
# hooks/test_format_violations.py
from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
from pathlib import Path
from typing import Any

import pytest

# ---------------------------------------------------------------------------
# Module loader — imports format-violations.py by path (name has a hyphen)
# ---------------------------------------------------------------------------

_SCRIPT_PATH = Path(__file__).parent / "format-violations.py"


def _load_module() -> Any:
    spec = importlib.util.spec_from_file_location("format_violations", _SCRIPT_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"Cannot load {_SCRIPT_PATH}")
    mod = importlib.util.module_from_spec(spec)
    sys.modules["format_violations"] = mod
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod


_fmt = _load_module()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _viol(
    fitness_function: str = "cyclomatic_complexity",
    value: float = 15.0,
    limit: float = 10.0,
    function: str = "ProcessRequest",
    calm_node: str = "handler",
) -> dict[str, Any]:
    return {
        "fitness_function": fitness_function,
        "value": value,
        "limit": limit,
        "function": function,
        "calm_node": calm_node,
    }


def _run(stdin_data: str, extra_args: list[str] | None = None) -> tuple[str, str, int]:
    """Run format-violations.py as a subprocess. Returns (stdout, stderr, returncode)."""
    cmd = [sys.executable, str(_SCRIPT_PATH)] + (extra_args or [])
    result = subprocess.run(cmd, input=stdin_data, capture_output=True, text=True)
    return result.stdout, result.stderr, result.returncode


# ---------------------------------------------------------------------------
# format_value
# ---------------------------------------------------------------------------


def test_format_value_whole_float_returns_int() -> None:
    assert _fmt.format_value(10.0) == 10
    assert isinstance(_fmt.format_value(10.0), int)


def test_format_value_fractional_returns_rounded_float() -> None:
    assert _fmt.format_value(0.1234567) == 0.123


def test_format_value_integer_input_returns_int() -> None:
    assert _fmt.format_value(5) == 5
    assert isinstance(_fmt.format_value(5), int)


def test_format_value_non_numeric_returns_value() -> None:
    assert _fmt.format_value(None) is None  # type: ignore[arg-type]
    assert _fmt.format_value("bad") == "bad"  # type: ignore[arg-type]


def test_format_value_infinity_does_not_raise() -> None:
    # Should not raise; exact return value is unspecified for non-finite inputs
    _fmt.format_value(float("inf"))


def test_format_value_nan_does_not_raise() -> None:
    _fmt.format_value(float("nan"))


# ---------------------------------------------------------------------------
# _normalise_fn_key
# ---------------------------------------------------------------------------


def test_normalise_fn_key_underscores_to_hyphens() -> None:
    assert _fmt._normalise_fn_key("cyclomatic_complexity") == "cyclomatic-complexity"


def test_normalise_fn_key_hyphens_unchanged() -> None:
    assert _fmt._normalise_fn_key("cyclomatic-complexity") == "cyclomatic-complexity"


def test_normalise_fn_key_empty_string() -> None:
    assert _fmt._normalise_fn_key("") == ""


# ---------------------------------------------------------------------------
# _build_output
# ---------------------------------------------------------------------------


def test_build_output_empty_violations_returns_none() -> None:
    assert _fmt._build_output([], "sample.go", "block", "block") is None


def test_build_output_block_violation_full_structure() -> None:
    result = _fmt._build_output([_viol()], "internal/checker.go", "block", "block")
    assert result is not None
    assert result["calm_check"]["file"] == "internal/checker.go"
    assert result["calm_check"]["status"] == "block"
    entries = result["violations"]
    assert len(entries) == 1
    e = entries[0]
    assert e["fitness_function"] == "cyclomatic-complexity"
    assert e["mode"] == "blocking"
    assert e["result"] == 15
    assert e["target"] == "<= 10"
    assert e["location"] == "ProcessRequest (handler)"
    assert "meaning" in e
    assert isinstance(e["remediation"], list)
    assert len(e["remediation"]) > 0


def test_build_output_advisory_mode_label() -> None:
    result = _fmt._build_output([_viol()], "sample.go", "advisory", "advisory")
    assert result is not None
    assert result["violations"][0]["mode"] == "advisory"


def test_build_output_gte_operator_for_logic_density() -> None:
    v = _viol(fitness_function="logic_density", value=0.1, limit=0.2)
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    assert result["violations"][0]["target"] == ">= 0.2"


def test_build_output_lte_operator_for_cyclomatic_complexity() -> None:
    result = _fmt._build_output([_viol()], "sample.go", "block", "block")
    assert result is not None
    assert result["violations"][0]["target"] == "<= 10"


def test_build_output_lte_operator_for_interface_width() -> None:
    v = _viol(fitness_function="interface_width", value=20.0, limit=15.0)
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    assert result["violations"][0]["target"] == "<= 15"


def test_build_output_gte_operator_for_implementation_depth() -> None:
    v = _viol(fitness_function="implementation_depth", value=2.0, limit=5.0)
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    assert result["violations"][0]["target"] == ">= 5"


def test_build_output_gte_operator_for_dependency_discipline() -> None:
    v = _viol(fitness_function="dependency_discipline", value=0.5, limit=0.8)
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    assert result["violations"][0]["target"] == ">= 0.8"


def test_build_output_no_location_when_no_function_or_node() -> None:
    v: dict[str, Any] = {
        "fitness_function": "interface_width",
        "value": 20.0,
        "limit": 15.0,
        "function": "",
        "calm_node": "",
    }
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    assert "location" not in result["violations"][0]


def test_build_output_location_is_only_calm_node_when_no_function() -> None:
    v: dict[str, Any] = {
        "fitness_function": "interface_width",
        "value": 20.0,
        "limit": 15.0,
        "function": "",
        "calm_node": "mymodule",
    }
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    assert result["violations"][0]["location"] == "mymodule"


def test_build_output_unknown_function_omits_guidance(capsys: pytest.CaptureFixture[str]) -> None:
    v = _viol(fitness_function="unknown_function")
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    entry = result["violations"][0]
    assert "meaning" not in entry
    assert "remediation" not in entry
    captured = capsys.readouterr()
    assert "no guidance" in captured.err


def test_build_output_skips_malformed_entry_continues_with_good() -> None:
    malformed: dict[str, Any] = {"fitness_function": None, "value": object(), "limit": None}
    good = _viol()
    result = _fmt._build_output([malformed, good], "sample.go", "block", "block")
    assert result is not None
    fn_names = [e["fitness_function"] for e in result["violations"]]
    assert "cyclomatic-complexity" in fn_names


def test_build_output_all_malformed_returns_none() -> None:
    malformed: dict[str, Any] = {"fitness_function": None, "value": object(), "limit": None}
    result = _fmt._build_output([malformed], "sample.go", "block", "block")
    assert result is None


# ---------------------------------------------------------------------------
# main() via subprocess (integration)
# ---------------------------------------------------------------------------


def test_main_pass_status_produces_no_output() -> None:
    stdout, _, code = _run('{"status":"pass"}', ["--mode", "block", "--file", "sample.go"])
    assert code == 0
    assert stdout == ""


def test_main_block_produces_valid_yaml() -> None:
    payload = json.dumps({
        "status": "block",
        "violations": [{
            "fitness_function": "cyclomatic_complexity",
            "value": 15.0,
            "limit": 10.0,
            "function": "Run",
            "calm_node": "checker",
        }],
    })
    stdout, _, code = _run(payload, ["--mode", "block", "--file", "checker.go"])
    assert code == 0
    assert "calm_check:" in stdout
    assert "fitness_function: cyclomatic-complexity" in stdout
    assert "mode: blocking" in stdout
    assert "result: 15" in stdout


def test_main_advisory_produces_yaml_with_advisory_label() -> None:
    payload = json.dumps({
        "status": "advisory",
        "violations": [{
            "fitness_function": "logic_density",
            "value": 0.15,
            "limit": 0.20,
        }],
    })
    stdout, _, code = _run(payload, ["--mode", "advisory", "--file", "sample.go"])
    assert code == 0
    assert "calm_check:" in stdout
    assert "advisory" in stdout


def test_main_invalid_json_exits_zero_with_warning() -> None:
    _, stderr, code = _run("not json at all", ["--mode", "block", "--file", "x.go"])
    assert code == 0
    assert "invalid JSON" in stderr


def test_main_violations_wrong_type_exits_zero() -> None:
    _, _, code = _run('{"status":"block","violations":"bad"}', ["--mode", "block", "--file", "x.go"])
    assert code == 0


def test_main_empty_violations_exits_zero_with_no_output() -> None:
    stdout, _, code = _run('{"status":"block","violations":[]}', ["--mode", "block", "--file", "x.go"])
    assert code == 0
    assert stdout == ""


def test_main_no_file_arg_still_exits_zero() -> None:
    payload = json.dumps({"status": "pass"})
    _, _, code = _run(payload, ["--mode", "block"])
    assert code == 0
```

- [ ] **Step 2: Run the tests — verify they all fail**

```bash
cd /Users/poconnor/peter_code/calm-poc/hooks && python3 -m pytest test_format_violations.py -v 2>&1 | head -30
```

Expected: `ERROR` during collection — `FileNotFoundError` or `ImportError` for `format-violations.py`
(The script does not exist yet — this is the expected red state.)

- [ ] **Step 3: Commit the failing tests**

```bash
git add hooks/test_format_violations.py
git commit -m "test(hooks): add failing tests for format-violations formatter (red phase)

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 3: Implement `hooks/format-violations.py` (green phase)

**Files:**
- Create: `hooks/format-violations.py`

- [ ] **Step 1: Create the formatter**

```python
#!/usr/bin/env python3
"""Format CALM bridge JSON as agent-readable YAML.

Usage:
  calm-bridge check ... | python3 hooks/format-violations.py \
      --mode <block|advisory> --file <relative/path>
"""
from __future__ import annotations

import argparse
import json
import sys
from typing import Any

try:
    import yaml
    _YAML_AVAILABLE = True
except ImportError:
    _YAML_AVAILABLE = False

# Maps bridge enforcement mode to agent-readable display label.
_MODE_LABELS: dict[str, str] = {
    "block": "blocking",
    "advisory": "advisory",
}


def _load_guidance() -> dict[str, dict[str, Any]]:
    """Return per-fitness-function guidance.

    POC: data is inline. To externalize, replace this body with:
        import os
        path = os.environ.get("CALM_GUIDANCE_FILE", <default_path>)
        return yaml.safe_load(open(path))
    Call sites never change.
    """
    return {
        "cyclomatic-complexity": {
            "operator": "<=",
            "meaning": (
                "Too many conditional branches make this function hard to test, "
                "review, and reason about. Each branch is an independent execution "
                "path through the function."
            ),
            "remediation": [
                "Extract each conditional branch into a named helper function.",
                "Replace complex boolean conditions with named predicates "
                "(e.g. is_expired instead of time.Now().After(x)).",
                "Use early returns (guard clauses) to flatten nested if/else chains.",
                "Aim for each function to do one thing — one reason to change.",
            ],
        },
        "interface-width": {
            "operator": "<=",
            "meaning": (
                "The module exposes too many public methods, creating a wide "
                "surface area that is hard to understand, mock, and maintain."
            ),
            "remediation": [
                "Split the module by cohesion — group related operations into "
                "separate modules.",
                "Consolidate related operations behind a single higher-level method.",
                "Consider whether some public methods should be internal.",
                "Reduce the public surface area to what callers actually need.",
            ],
        },
        "implementation-depth": {
            "operator": ">=",
            "meaning": (
                "Public methods are too thin, averaging very few lines each. "
                "Thin methods are often unnecessary pass-throughs or scaffolding."
            ),
            "remediation": [
                "Eliminate unnecessary delegation — if a method just calls another, "
                "merge them.",
                "Move logic up — push thin wrapper logic into the callers.",
                "Remove methods that add no value beyond renaming.",
                "Check whether some public methods should be private utilities.",
            ],
        },
        "logic-density": {
            "operator": ">=",
            "meaning": (
                "The file has a low ratio of functional logic to total lines. "
                "Excessive boilerplate, comments, or scaffolding reduces density."
            ),
            "remediation": [
                "Remove unused or dead code.",
                "Move configuration and constants to a separate file.",
                "Reduce scaffolding — prefer declarative patterns over "
                "procedural setup.",
                "Consider whether comments are explaining obvious code that "
                "should be refactored instead.",
            ],
        },
        "dependency-discipline": {
            "operator": ">=",
            "meaning": (
                "The file imports more dependencies than it actively uses, or has "
                "a low ratio of used-to-total imports."
            ),
            "remediation": [
                "Remove all unused imports.",
                "Prefer explicit, narrow imports over broad wildcard or "
                "namespace imports.",
                "If a dependency is only used in tests, move it to test scope.",
                "Consider whether the dependency is the right tool — sometimes "
                "a simpler standard-library solution exists.",
            ],
        },
    }


def format_value(v: float) -> int | float:
    """Normalise a metric value: return int for whole numbers, float rounded to 3dp."""
    try:
        if isinstance(v, (int, float)) and float(v).is_integer():
            return int(v)
        return round(float(v), 3)
    except (TypeError, ValueError, OverflowError):
        return v  # type: ignore[return-value]


def _normalise_fn_key(raw: str) -> str:
    """Convert bridge key format (underscores) to guidance key format (hyphens)."""
    return raw.replace("_", "-")


def _build_output(
    violations: list[dict[str, Any]],
    file: str,
    mode: str,
    status: str,
) -> dict[str, Any] | None:
    """Build the YAML output dict from validated bridge data.

    Returns None when there are no processable violations.
    Key order is intentional — agent reads top to bottom:
    context (file, status) before action items (violations).
    """
    if not violations:
        return None

    guidance = _load_guidance()
    mode_label = _MODE_LABELS.get(mode, mode)
    entries: list[dict[str, Any]] = []

    for i, v in enumerate(violations):
        try:
            fn = _normalise_fn_key(v.get("fitness_function", "") or "")
            value = v.get("value", 0)
            limit = v.get("limit", 0)
            function_name = v.get("function", "") or ""
            calm_node = v.get("calm_node", "") or ""
            fn_guidance = guidance.get(fn, {})
            operator = fn_guidance.get("operator", "<=")

            if not fn_guidance:
                print(
                    f"format-violations: no guidance for fitness function {fn!r}",
                    file=sys.stderr,
                )

            location = calm_node
            if function_name:
                location = (
                    f"{function_name} ({calm_node})" if calm_node else function_name
                )

            # sort_keys=False preserves this insertion order — agent reads top to bottom
            entry: dict[str, Any] = {
                "fitness_function": fn,
                "mode": mode_label,
                "result": format_value(value),
                "target": f"{operator} {format_value(limit)}",
            }
            if location:
                entry["location"] = location
            if fn_guidance.get("meaning"):
                entry["meaning"] = fn_guidance["meaning"]
            if fn_guidance.get("remediation"):
                entry["remediation"] = fn_guidance["remediation"]

            entries.append(entry)
        except Exception as exc:  # noqa: BLE001
            print(
                f"format-violations: skipping violation[{i}]: {exc}",
                file=sys.stderr,
            )

    if not entries:
        return None

    return {
        "calm_check": {
            "file": file or "(unknown)",
            "status": status,
        },
        "violations": entries,
    }


def main() -> None:
    """Entry point."""
    if not _YAML_AVAILABLE:
        print(
            "format-violations: pyyaml is not installed — "
            "run: python3 -m pip install pyyaml",
            file=sys.stderr,
        )
        sys.exit(0)

    parser = argparse.ArgumentParser(
        description="Format CALM bridge JSON as agent-readable YAML."
    )
    parser.add_argument(
        "--mode",
        choices=list(_MODE_LABELS),
        default="block",
        help="Enforcement mode (block or advisory)",
    )
    parser.add_argument(
        "--file",
        default=None,
        type=str,
        help="Relative source file path for the YAML header",
    )
    args = parser.parse_args()

    if not args.file:
        print(
            "format-violations: --file not provided; file path will be empty",
            file=sys.stderr,
        )

    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"format-violations: invalid JSON on stdin: {exc}", file=sys.stderr)
        sys.exit(0)

    try:
        status = payload.get("status", "pass")
        violations_raw = payload.get("violations") or []

        if not isinstance(violations_raw, list):
            print(
                f"format-violations: unexpected violations type: "
                f"{type(violations_raw).__name__}",
                file=sys.stderr,
            )
            sys.exit(0)

        if status == "pass" or not violations_raw:
            sys.exit(0)

        output = _build_output(
            violations=violations_raw,
            file=args.file or "",
            mode=args.mode,
            status=status,
        )
        if output is None:
            sys.exit(0)

        try:
            # sort_keys=False: preserve insertion order — agent reads top to bottom
            result = yaml.dump(
                output,
                default_flow_style=False,
                allow_unicode=True,
                sort_keys=False,
            )
            print(result)
            sys.stdout.flush()
        except Exception as exc:  # noqa: BLE001
            # Degraded fallback: plain-text summary so the hook does not block
            print("calm_check:", file=sys.stdout)
            print(f"  status: {status}", file=sys.stdout)
            print("  violations:", file=sys.stdout)
            for v in violations_raw:
                fn = v.get("fitness_function", "unknown") if isinstance(v, dict) else "unknown"
                print(f"    - {fn}", file=sys.stdout)
            print(
                f"format-violations: yaml.dump failed ({exc}); used plain-text fallback",
                file=sys.stderr,
            )
            sys.stdout.flush()

    except Exception as exc:  # noqa: BLE001
        print(f"format-violations: unexpected error: {exc}", file=sys.stderr)
        sys.exit(0)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Run the Python tests — verify they all pass**

```bash
cd /Users/poconnor/peter_code/calm-poc/hooks && python3 -m pytest test_format_violations.py -v
```

Expected: all tests `PASSED`. If any fail, fix the implementation before continuing.

- [ ] **Step 3: Commit**

```bash
git add hooks/format-violations.py
git commit -m "feat(hooks): add format-violations.py — agent-readable YAML violation output

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 4: Update Go tests for pre-tool-use.sh (red phase)

**Files:**
- Modify: `hooks/pre_tool_use_test.go:28-29` (TestPreToolUseBlocksWriteViolation)
- Modify: `hooks/pre_tool_use_test.go:53-55` (TestPreToolUseAllowsEditAdvisory)
- Modify: `hooks/pre_tool_use_test.go:105-107` (TestPreToolUseChecksRunningDaemonKnownBadAndGood)

The current assertions check for the old output format (`"CALM violation in …"`, raw message strings).
Update them to match the new YAML output. The hooks have **not changed yet**, so these tests will fail after this step — that is the expected red state.

- [ ] **Step 1: Update TestPreToolUseBlocksWriteViolation**

In `hooks/pre_tool_use_test.go`, replace:

```go
	if !strings.Contains(string(output), "CALM violation in sample.go") || !strings.Contains(string(output), "too complex") {
		t.Fatalf("output = %s, want violation message", output)
	}
```

with:

```go
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "blocking") {
		t.Fatalf("output = %s, want YAML calm_check block with blocking mode", output)
	}
```

- [ ] **Step 2: Update TestPreToolUseAllowsEditAdvisory**

Replace:

```go
	if !strings.Contains(string(output), "CALM advisory for sample.py") || !strings.Contains(string(output), "warning only") {
		t.Fatalf("output = %s, want advisory message", output)
	}
```

with:

```go
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
```

- [ ] **Step 3: Update TestPreToolUseChecksRunningDaemonKnownBadAndGood**

Replace:

```go
	if !strings.Contains(string(output), "cyclomatic complexity") {
		t.Fatalf("output = %s, want analyzer-backed daemon violation", output)
	}
```

with:

```go
	if !strings.Contains(string(output), "cyclomatic-complexity") {
		t.Fatalf("output = %s, want YAML cyclomatic-complexity violation", output)
	}
```

- [ ] **Step 4: Run Go tests — verify the three updated tests now fail**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -run "TestPreToolUseBlocksWriteViolation|TestPreToolUseAllowsEditAdvisory|TestPreToolUseChecksRunningDaemonKnownBadAndGood" -v 2>&1 | tail -20
```

Expected: `FAIL` on all three tests. The other pre-tool-use tests should still pass.

- [ ] **Step 5: Commit the updated tests**

```bash
git add hooks/pre_tool_use_test.go
git commit -m "test(hooks): update pre-tool-use assertions for YAML output format (red phase)

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 5: Update pre-tool-use.sh (green phase)

**Files:**
- Modify: `hooks/pre-tool-use.sh:44-52` (remove `json_messages` function)
- Modify: `hooks/pre-tool-use.sh:149-165` (replace case branches)

- [ ] **Step 1: Remove the `json_messages` function**

In `hooks/pre-tool-use.sh`, delete these lines (the full function definition):

```bash
json_messages() {
  python3 -c 'import json,sys
payload = json.load(sys.stdin)
for violation in payload.get("violations", []):
    message = violation.get("message", "")
    if message:
        print(message)
'
}
```

- [ ] **Step 2: Replace the block and advisory case branches**

In the `case "$status" in` block, replace:

```bash
  block)
    echo "CALM violation in $file:" >&2
    printf '%s' "$result" | json_messages >&2
    exit 2
    ;;
  advisory)
    echo "CALM advisory for $file:" >&2
    printf '%s' "$result" | json_messages >&2
    ;;
```

with:

```bash
  block)
    printf '%s' "$result" | python3 "$repo/hooks/format-violations.py" \
      --mode "$status" --file "$file" >&2
    exit 2
    ;;
  advisory)
    printf '%s' "$result" | python3 "$repo/hooks/format-violations.py" \
      --mode "$status" --file "$file" >&2
    ;;
```

- [ ] **Step 3: Run the updated Go tests — verify they now pass**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -run "TestPreToolUseBlocksWriteViolation|TestPreToolUseAllowsEditAdvisory|TestPreToolUseChecksRunningDaemonKnownBadAndGood" -v 2>&1 | tail -20
```

Expected: all three tests `PASS`.

- [ ] **Step 4: Run the full pre-tool-use Go test suite**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -run "TestPreToolUse" -v 2>&1 | tail -20
```

Expected: all `TestPreToolUse*` tests pass.

- [ ] **Step 5: Commit**

```bash
git add hooks/pre-tool-use.sh
git commit -m "feat(hooks): pipe pre-tool-use violations through format-violations formatter

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 6: Update Go tests for pre-commit.sh (red phase)

**Files:**
- Modify: `hooks/pre_commit_test.go:43-48` (TestPreCommitBlocksStagedViolations)
- Modify: `hooks/pre_commit_test.go:79-81` (TestPreCommitAllowsAdvisoryStagedViolations)
- Modify: `hooks/pre_commit_test.go:132-134` (TestPreCommitBlocksStagedViolationThroughRunningDaemon)
- Modify: `hooks/pre_commit_test.go:165-167` (TestPreCommitForwardsAddressToRunningDaemon)

- [ ] **Step 1: Update TestPreCommitBlocksStagedViolations**

Replace:

```go
	if !strings.Contains(string(output), "CALM violation in bad.go") ||
		!strings.Contains(string(output), "too complex") ||
		!strings.Contains(string(output), "CALM advisory for warn.py") ||
		!strings.Contains(string(output), "warning only") {
		t.Fatalf("output = %s, want block and advisory messages", output)
	}
```

with:

```go
	if strings.Count(string(output), "calm_check:") != 2 {
		t.Fatalf("output = %s, want two calm_check YAML blocks (one block, one advisory)", output)
	}
	if !strings.Contains(string(output), "blocking") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want both blocking and advisory mode labels", output)
	}
```

- [ ] **Step 2: Update TestPreCommitAllowsAdvisoryStagedViolations**

Replace:

```go
	if !strings.Contains(string(output), "CALM advisory for warn.py") || !strings.Contains(string(output), "warning only") {
		t.Fatalf("output = %s, want advisory message", output)
	}
```

with:

```go
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
```

- [ ] **Step 3: Update TestPreCommitBlocksStagedViolationThroughRunningDaemon**

Replace:

```go
	if !strings.Contains(string(output), "daemon validated staged violation") {
		t.Fatalf("output = %s, want daemon violation message", output)
	}
```

with:

```go
	if !strings.Contains(string(output), "calm_check:") {
		t.Fatalf("output = %s, want YAML calm_check block", output)
	}
```

- [ ] **Step 4: Update TestPreCommitForwardsAddressToRunningDaemon**

Replace:

```go
	if !strings.Contains(string(output), "configured advisory") {
		t.Fatalf("output = %s, want advisory from running daemon path", output)
	}
```

with:

```go
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
```

- [ ] **Step 5: Run the four updated tests — verify they fail**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -run "TestPreCommitBlocksStagedViolations$|TestPreCommitAllowsAdvisoryStagedViolations|TestPreCommitBlocksStagedViolationThroughRunningDaemon|TestPreCommitForwardsAddressToRunningDaemon" -v 2>&1 | tail -20
```

Expected: all four fail. Other pre-commit tests should still pass.

- [ ] **Step 6: Commit**

```bash
git add hooks/pre_commit_test.go
git commit -m "test(hooks): update pre-commit assertions for YAML output format (red phase)

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 7: Update pre-commit.sh (green phase)

**Files:**
- Modify: `hooks/pre-commit.sh:46-54` (remove `json_messages` function)
- Modify: `hooks/pre-commit.sh:72-90` (replace case branches)

- [ ] **Step 1: Remove the `json_messages` function**

In `hooks/pre-commit.sh`, delete the full function definition:

```bash
json_messages() {
  python3 -c 'import json,sys
payload = json.load(sys.stdin)
for violation in payload.get("violations", []):
    message = violation.get("message", "")
    if message:
        print(message)
'
}
```

- [ ] **Step 2: Replace the block and advisory case branches**

In the `case "$status" in` block, replace:

```bash
    block)
      echo "CALM violation in $file:" >&2
      printf '%s' "$result" | json_messages >&2
      blocked=1
      ;;
    advisory)
      echo "CALM advisory for $file:" >&2
      printf '%s' "$result" | json_messages >&2
      ;;
```

with:

```bash
    block)
      printf '%s' "$result" | python3 "$repo/hooks/format-violations.py" \
        --mode "$status" --file "$file" >&2
      blocked=1
      ;;
    advisory)
      printf '%s' "$result" | python3 "$repo/hooks/format-violations.py" \
        --mode "$status" --file "$file" >&2
      ;;
```

- [ ] **Step 3: Run the four updated Go tests — verify they now pass**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -run "TestPreCommitBlocksStagedViolations$|TestPreCommitAllowsAdvisoryStagedViolations|TestPreCommitBlocksStagedViolationThroughRunningDaemon|TestPreCommitForwardsAddressToRunningDaemon" -v 2>&1 | tail -20
```

Expected: all four pass.

- [ ] **Step 4: Run the full pre-commit Go test suite**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -run "TestPreCommit" -v 2>&1 | tail -20
```

Expected: all `TestPreCommit*` tests pass.

- [ ] **Step 5: Commit**

```bash
git add hooks/pre-commit.sh
git commit -m "feat(hooks): pipe pre-commit violations through format-violations formatter

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 8: Update README.md

**Files:**
- Modify: `README.md` (Tool Requirements section)

- [ ] **Step 1: Add pyyaml install step to Tool Requirements**

In `README.md`, find the `## Tool Requirements` section and add a new bullet after the existing list:

```markdown
- `pyyaml` 6+ for hook violation formatting: `python3 -m pip install -r hooks/requirements.txt`
```

The section should read:

```markdown
## Tool Requirements

- Go 1.22 or newer for `calm-bridge`
- FINOS CALM CLI 1.40.0 via `npm install -g @finos/calm-cli@1.40.0`
- `radon` 6.0.1 on `PATH`, or pass `--radon <path>`, for Python baseline analysis
- .NET 8 SDK for `tools/roslyn-analyzer`; `calm-bridge baseline --language csharp` builds the local analyzer automatically when `--roslyn <path>` is omitted
- `pyyaml` 6+ for hook violation formatting: `python3 -m pip install -r hooks/requirements.txt`
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add pyyaml install step to Tool Requirements

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - databricks-claude-sonnet-4-6"
```

---

## Task 9: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full Go test suite**

```bash
cd /Users/poconnor/peter_code/calm-poc && go test ./hooks/... -v 2>&1 | tail -30
```

Expected: all tests pass. If any fail, do not proceed — fix and re-run.

- [ ] **Step 2: Run the full Python test suite**

```bash
cd /Users/poconnor/peter_code/calm-poc/hooks && python3 -m pytest test_format_violations.py -v
```

Expected: all tests pass.

- [ ] **Step 3: Smoke test — block output**

Build the bridge if not already built:

```bash
go build -o /tmp/calm-bridge ./cmd/calm-bridge
```

Start the bridge daemon (background):

```bash
/tmp/calm-bridge serve --addr 127.0.0.1:7890 &
BRIDGE_PID=$!
sleep 1
```

Create a high-complexity Go file and check its output:

```bash
cat > /tmp/test_complex.go << 'EOF'
package sample
func Score(kind string, retries int, urgent bool) int {
score := 0
if kind == "create" { score++ }
if kind == "update" { score++ }
if kind == "delete" { score++ }
if kind == "manual" { score++ }
if kind == "batch" { score++ }
if kind == "sync" { score++ }
if retries > 0 { score++ }
if retries > 1 { score++ }
if retries > 2 { score++ }
if urgent { score++ }
return score
}
EOF

/tmp/calm-bridge check \
  --addr http://127.0.0.1:7890 \
  --repo /Users/poconnor/peter_code/calm-poc \
  --file internal/bridge/checker.go \
  --content "$(cat /tmp/test_complex.go)" \
  --language go \
  | python3 /Users/poconnor/peter_code/calm-poc/hooks/format-violations.py \
    --mode block --file internal/bridge/checker.go
```

Expected: YAML output containing `calm_check:`, `fitness_function: cyclomatic-complexity`, `mode: blocking`, numeric `result` and `target` values, `meaning:` text, and `remediation:` list.

Shut down the bridge:

```bash
kill $BRIDGE_PID 2>/dev/null || true
```

- [ ] **Step 4: Push to remote**

```bash
git pull --rebase && git push
```

Expected: `git status` shows `Your branch is up to date with 'origin/...'`

---

*Authored by Peter O'Connor with assistance from Claude Code (databricks-claude-sonnet-4-6) · 2026-05-24 · CALM Hook Feedback Implementation Plan*
