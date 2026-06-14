#!/usr/bin/env python3
"""Format CALM bridge JSON as agent-readable YAML.

Usage:
  stack-fitness-functions client validate ... | python3 hooks/format-violations.py \
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
        path = os.environ.get("STACK_FITNESS_FUNCTIONS_GUIDANCE_FILE", <default_path>)
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
            # Validate that value and limit are numeric — raises TypeError for object() or None
            float(value)  # type: ignore[arg-type]
            float(limit)  # type: ignore[arg-type]
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
