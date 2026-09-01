#!/usr/bin/env python3
"""Format CALM bridge JSON as agent-readable YAML.

Usage:
  agent-fitness-functions client validate ... | python3 hooks/format-violations.py \
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

    POC: data is inline. To externalize, replace this body with one that reads
    the path named by the AGENT_FITNESS_FUNCTIONS_GUIDANCE_FILE environment
    variable (falling back to a default path) and returns yaml.safe_load of
    that file. Call sites never change.
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
        "layer-sovereignty": {
            "operator": "<=",
            "meaning": (
                "The file belongs to an architectural layer that referenced a "
                "schema/namespace it must not touch; layer boundaries keep data "
                "flow unidirectional and contracts explicit."
            ),
            "remediation": [
                "Route access through the layer's sanctioned interface (e.g. "
                "the next layer's models/views).",
                "Remove direct references to forbidden schemas.",
                "If the reference is genuinely needed, move the code to the "
                "layer that owns that data.",
                "Ask the repo owner before widening a layer's allowed surface.",
            ],
        },
        "temporal-purity": {
            "operator": "<=",
            "meaning": (
                "Naive datetime construction produces timezone-ambiguous values "
                "that break cross-source joins and time arithmetic."
            ),
            "remediation": [
                "Use datetime.now(timezone.utc) instead of datetime.now() or "
                "datetime.utcnow().",
                "Carry tz-aware values end to end.",
                "Store UTC and convert at display time.",
            ],
        },
        "sql-composition-safety": {
            "operator": "<=",
            "meaning": (
                "Dynamic SQL built with f-strings/format/% interpolation can "
                "inject identifiers or values and corrupt statements."
            ),
            "remediation": [
                "Use the driver's parameterized queries for values.",
                "Use the SQL composition API (e.g. psycopg2.sql.SQL/Identifier) "
                "for identifiers.",
                "Never interpolate user or introspected strings directly.",
            ],
        },
        "deterministic-ordering": {
            "operator": "<=",
            "meaning": (
                "Window functions or chunk aggregations ordered without a "
                "unique tie-breaker produce non-deterministic results when "
                "timestamps collide."
            ),
            "remediation": [
                "Append a unique column (primary key/id) to every ORDER BY "
                "inside OVER(...).",
                "Sort aggregation inputs deterministically.",
                "Re-run twice and diff outputs to verify determinism.",
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


def _resolve_location(function_name: str, calm_node: str) -> str:
    """Build the human-readable location string for a violation entry."""
    if function_name and calm_node:
        return f"{function_name} ({calm_node})"
    return function_name or calm_node


def _warn_missing_guidance(fn: str) -> None:
    """Log that no remediation guidance exists for a fitness function key."""
    print(
        f"format-violations: no guidance for fitness function {fn!r}",
        file=sys.stderr,
    )


def _make_violation_entry(
    v: dict[str, Any],
    guidance: dict[str, dict[str, Any]],
    mode_label: str,
) -> dict[str, Any]:
    """Build one violation entry dict. Raises on malformed input (caller catches)."""
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
        _warn_missing_guidance(fn)

    # sort_keys=False preserves this insertion order — agent reads top to bottom
    entry: dict[str, Any] = {
        "fitness_function": fn,
        "mode": mode_label,
        "result": format_value(value),
        "target": f"{operator} {format_value(limit)}",
    }
    location = _resolve_location(function_name, calm_node)
    if location:
        entry["location"] = location
    if fn_guidance.get("meaning"):
        entry["meaning"] = fn_guidance["meaning"]
    if fn_guidance.get("remediation"):
        entry["remediation"] = fn_guidance["remediation"]

    return entry


def _try_make_violation_entry(
    i: int,
    v: dict[str, Any],
    guidance: dict[str, dict[str, Any]],
    mode_label: str,
) -> dict[str, Any] | None:
    """Build one entry, or log and return None for a malformed violation."""
    try:
        return _make_violation_entry(v, guidance, mode_label)
    except Exception as exc:  # noqa: BLE001
        print(
            f"format-violations: skipping violation[{i}]: {exc}",
            file=sys.stderr,
        )
        return None


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
    entries: list[dict[str, Any]] = [
        entry
        for i, v in enumerate(violations)
        if (entry := _try_make_violation_entry(i, v, guidance, mode_label)) is not None
    ]

    if not entries:
        return None

    return {
        "calm_check": {
            "file": file or "(unknown)",
            "status": status,
        },
        "violations": entries,
    }


def _warn_if_yaml_missing() -> None:
    """Exit cleanly (code 0) so the hook never blocks on a missing dependency."""
    if not _YAML_AVAILABLE:
        print(
            "format-violations: pyyaml is not installed — "
            "run: python3 -m pip install pyyaml",
            file=sys.stderr,
        )
        sys.exit(0)


def _build_arg_parser() -> argparse.ArgumentParser:
    """Build the CLI argument parser."""
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
    return parser


def _warn_if_file_missing(file_arg: str | None) -> None:
    """Log a warning when --file was not supplied."""
    if not file_arg:
        print(
            "format-violations: --file not provided; file path will be empty",
            file=sys.stderr,
        )


def _read_payload() -> Any:
    """Read and parse the JSON payload from stdin. Exits 0 on invalid JSON."""
    try:
        return json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"format-violations: invalid JSON on stdin: {exc}", file=sys.stderr)
        sys.exit(0)


def _extract_status_and_violations(
    payload: dict[str, Any],
) -> tuple[str, list[dict[str, Any]]] | None:
    """Return (status, violations) worth reporting, or None if there is nothing to do."""
    status = payload.get("status", "pass")
    violations_raw = payload.get("violations") or []

    if not isinstance(violations_raw, list):
        print(
            f"format-violations: unexpected violations type: "
            f"{type(violations_raw).__name__}",
            file=sys.stderr,
        )
        return None

    if status == "pass" or not violations_raw:
        return None

    return status, violations_raw


def _print_plain_fallback(status: str, violations_raw: list[Any]) -> None:
    """Degraded fallback: plain-text summary so the hook does not block."""
    print("calm_check:", file=sys.stdout)
    print(f"  status: {status}", file=sys.stdout)
    print("  violations:", file=sys.stdout)
    for v in violations_raw:
        fn = v.get("fitness_function", "unknown") if isinstance(v, dict) else "unknown"
        print(f"    - {fn}", file=sys.stdout)


def _render_yaml_or_fallback(
    output: dict[str, Any], violations_raw: list[Any], status: str
) -> None:
    """Print the YAML report, falling back to plain text if yaml.dump fails."""
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
        _print_plain_fallback(status, violations_raw)
        print(
            f"format-violations: yaml.dump failed ({exc}); used plain-text fallback",
            file=sys.stderr,
        )
        sys.stdout.flush()


def main() -> None:
    """Entry point."""
    _warn_if_yaml_missing()

    parser = _build_arg_parser()
    args = parser.parse_args()
    _warn_if_file_missing(args.file)

    payload = _read_payload()

    try:
        extracted = _extract_status_and_violations(payload)
        if extracted is None:
            sys.exit(0)
        status, violations_raw = extracted

        output = _build_output(
            violations=violations_raw,
            file=args.file or "",
            mode=args.mode,
            status=status,
        )
        if output is None:
            sys.exit(0)

        _render_yaml_or_fallback(output, violations_raw, status)

    except Exception as exc:  # noqa: BLE001
        print(f"format-violations: unexpected error: {exc}", file=sys.stderr)
        sys.exit(0)


if __name__ == "__main__":
    main()
