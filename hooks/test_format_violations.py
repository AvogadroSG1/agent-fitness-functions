# hooks/test_format_violations.py
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
