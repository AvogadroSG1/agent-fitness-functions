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
    result = subprocess.run(
        cmd, input=stdin_data, capture_output=True, text=True, check=False
    )
    return result.stdout, result.stderr, result.returncode


def test_subprocess_preserves_interface_locations() -> None:
    import yaml

    first = _viol("interface_width", 21, 20, "", "retrieval")
    first["interface"] = "_Outer._Repository"
    second = _viol("interface-width", 22, 20, "", "retrieval")
    second["interface"] = "RepositoryB"
    module = _viol("interface_width", 23, 20, "", "retrieval")
    function = _viol("cyclomatic_complexity", 10, 9, "parse", "retrieval")
    function["interface"] = "IgnoredForOtherRules"
    stdout, stderr, code = _run(
        json.dumps({"status": "block", "violations": [first, second, module, function]}),
        ["--mode", "block", "--checked-file", "retrieval.py"],
    )
    assert code == 0, stderr
    output = yaml.safe_load(stdout)
    assert output["calm_check"]["checked_file"] == "retrieval.py"
    entries = output["violations"]
    assert [(entry["location"], entry["result"], entry["target"]) for entry in entries] == [
        ("_Outer._Repository (retrieval)", 21, "<= 20"),
        ("RepositoryB (retrieval)", 22, "<= 20"),
        ("retrieval", 23, "<= 20"),
        ("parse (retrieval)", 10, "<= 9"),
    ]


@pytest.mark.parametrize("status,mode_label", [("block", "blocking"), ("advisory", "advisory")])
def test_subprocess_python_export_facade_guidance(status: str, mode_label: str) -> None:
    import yaml

    file = "data-pipeline/src/observatory_pipeline/retrieval/adapters/__init__.py"
    violation = {
        "fitness_function": "logic_density",
        "file": file,
        "calm_node": "__init__",
        "value": 4 / 21,
        "limit": 0.255,
        "source_kind": "python-export-facade",
        "message": "Explicit Python export facade; raw density floor still applies.",
    }
    stdout, stderr, code = _run(
        json.dumps({"status": status, "violations": [violation]}),
        ["--mode", status, "--checked-file", file],
    )
    assert code == 0, stderr
    output = yaml.safe_load(stdout)
    assert output["calm_check"]["status"] == status
    entry = output["violations"][0]
    assert entry["mode"] == mode_label
    assert entry["source_kind"] == "python-export-facade"
    assert entry["result"] == 0.19
    assert entry["target"] == ">= 0.255"
    assert entry["location"] == "__init__"
    assert "declarative API surface" in entry["meaning"]
    advice = " ".join(entry["remediation"])
    assert "Preserve required public exports" in advice
    assert "keep implementations in their adapter modules" in advice
    assert "Do not add fake logic" in advice
    assert "move business logic into the facade" in advice
    assert "remove exports solely to raise density" in advice
    assert "governance policy owner" in advice
    assert "do not change thresholds, enforcement, or add a consumer waiver" in advice
    for generic in ("Remove unused or dead code", "Move configuration and constants",
                    "Reduce scaffolding", "should be refactored"):
        assert generic not in advice
    if status == "advisory":
        assert "blocking" not in stdout


@pytest.mark.parametrize(
    "fn,marker",
    [("logic_density", None), ("logic-density", "unknown"),
     ("dependency_discipline", "python-export-facade")],
)
def test_subprocess_unclassified_findings_keep_ordinary_guidance(fn: str, marker: str | None) -> None:
    import yaml

    violation = _viol(fn, 4 / 21, 0.255, "", "__init__")
    if marker is not None:
        violation["source_kind"] = marker
    stdout, stderr, code = _run(
        json.dumps({"status": "block", "violations": [violation]}),
        ["--mode", "block", "--checked-file", "pkg/__init__.py"],
    )
    assert code == 0, stderr
    entry = yaml.safe_load(stdout)["violations"][0]
    advice = " ".join(entry["remediation"])
    assert "governance policy owner" not in advice
    if fn.replace("_", "-") == "logic-density":
        assert "Remove unused or dead code" in advice
    else:
        assert "Remove all unused imports" in advice


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


def test_build_output_unknown_function_omits_guidance(
    capsys: pytest.CaptureFixture[str],
) -> None:
    v = _viol(fitness_function="unknown_function")
    result = _fmt._build_output([v], "sample.go", "block", "block")
    assert result is not None
    entry = result["violations"][0]
    assert "meaning" not in entry
    assert "remediation" not in entry
    captured = capsys.readouterr()
    assert "no guidance" in captured.err


def test_build_output_skips_malformed_entry_continues_with_good() -> None:
    malformed: dict[str, Any] = {
        "fitness_function": None,
        "value": object(),
        "limit": None,
    }
    good = _viol()
    result = _fmt._build_output([malformed, good], "sample.go", "block", "block")
    assert result is not None
    fn_names = [e["fitness_function"] for e in result["violations"]]
    assert "cyclomatic-complexity" in fn_names


def test_build_output_all_malformed_returns_none() -> None:
    malformed: dict[str, Any] = {
        "fitness_function": None,
        "value": object(),
        "limit": None,
    }
    result = _fmt._build_output([malformed], "sample.go", "block", "block")
    assert result is None


# ---------------------------------------------------------------------------
# main() via subprocess (integration)
# ---------------------------------------------------------------------------


def test_main_pass_status_produces_no_output() -> None:
    stdout, _, code = _run(
        '{"status":"pass"}', ["--mode", "block", "--checked-file", "sample.go"]
    )
    assert code == 0
    assert stdout == ""


def test_main_block_produces_valid_yaml() -> None:
    payload = json.dumps(
        {
            "status": "block",
            "violations": [
                {
                    "fitness_function": "cyclomatic_complexity",
                    "value": 15.0,
                    "limit": 10.0,
                    "function": "Run",
                    "calm_node": "checker",
                }
            ],
        }
    )
    stdout, _, code = _run(payload, ["--mode", "block", "--checked-file", "checker.go"])
    assert code == 0
    assert "calm_check:" in stdout
    assert "fitness_function: cyclomatic-complexity" in stdout
    assert "mode: blocking" in stdout
    assert "result: 15" in stdout


def test_main_formats_persisted_calm_node_validation_result() -> None:
    fixture = (
        Path(__file__).parents[1]
        / "internal/fitness/testdata/persisted-calm-node-validation-result.json"
    ).read_text()

    stdout, _, code = _run(
        fixture,
        ["--mode", "block", "--checked-file", "internal/parser/parser.go"],
    )

    assert code == 0
    assert "location: Parse (go-module)" in stdout


def test_main_advisory_produces_yaml_with_advisory_label() -> None:
    payload = json.dumps(
        {
            "status": "advisory",
            "violations": [
                {
                    "fitness_function": "logic_density",
                    "value": 0.15,
                    "limit": 0.20,
                }
            ],
        }
    )
    stdout, _, code = _run(payload, ["--mode", "advisory", "--checked-file", "sample.go"])
    assert code == 0
    assert "calm_check:" in stdout
    assert "advisory" in stdout


def test_main_invalid_json_exits_zero_with_warning() -> None:
    _, stderr, code = _run("not json at all", ["--mode", "block", "--checked-file", "x.go"])
    assert code == 0
    assert "invalid JSON" in stderr


def test_main_violations_wrong_type_exits_zero() -> None:
    _, _, code = _run(
        '{"status":"block","violations":"bad"}', ["--mode", "block", "--checked-file", "x.go"]
    )
    assert code == 0


def test_main_empty_violations_exits_zero_with_no_output() -> None:
    stdout, _, code = _run(
        '{"status":"block","violations":[]}', ["--mode", "block", "--checked-file", "x.go"]
    )
    assert code == 0
    assert stdout == ""


def test_main_no_file_arg_still_exits_zero() -> None:
    payload = json.dumps({"status": "pass"})
    _, _, code = _run(payload, ["--mode", "block"])
    assert code == 0


def _attributed_violation() -> dict[str, Any]:
    return {
        "fitness_function": "logic_density",
        "file": "configs/embed.go",
        "function": "load",
        "interface": "Assets",
        "calm_node": "configs",
        "value": 1 / 9,
        "limit": 0.255,
        "message": "density too low",
        "source_kind": "ordinary",
        "rule": "optional-input-rule",
    }


def _parse_report(stdout: str) -> dict[str, Any]:
    import yaml

    report = yaml.safe_load(stdout)
    assert isinstance(report, dict)
    return report


def _run_seen(
    payload: dict[str, Any],
    cache: Path,
    checked_file: str = "internal/clean.go",
) -> tuple[str, str, int]:
    return _run(
        json.dumps(payload),
        [
            "--mode",
            "block",
            "--checked-file",
            checked_file,
            "--seen-file",
            str(cache),
        ],
    )


def test_seen_file_suppresses_known_finding_across_checked_paths(tmp_path: Path) -> None:
    violation = _attributed_violation()
    cache = tmp_path / "seen.json"

    first_stdout, first_stderr, first_code = _run_seen(
        {"status": "block", "violations": [violation]},
        cache,
        "internal/clean.go",
    )
    second = dict(violation)
    second["fitness_function"] = "logic-density"
    second_stdout, second_stderr, second_code = _run_seen(
        {"status": "block", "violations": [second]},
        cache,
        "cmd/main.go",
    )

    assert first_code == second_code == 0
    assert first_stderr == ""
    assert second_stderr == ""
    first = _parse_report(first_stdout)
    assert first["calm_check"]["checked_file"] == "internal/clean.go"
    assert first["calm_check"]["scope"] == "repository"
    entry = first["violations"][0]
    assert entry["affected_file"] == "configs/embed.go"
    assert entry["value"] == 1 / 9
    assert entry["limit"] == 0.255
    assert second_stdout == ""


def test_seen_file_keeps_distinct_attributed_diagnostics_and_normalizes_function(
    tmp_path: Path,
) -> None:
    base = _attributed_violation()
    affected = dict(base)
    affected["file"] = "internal/other.go"
    function = dict(base)
    function["function"] = "render"
    interface = dict(base)
    interface["interface"] = "OtherAssets"
    value = dict(base)
    value["value"] = 2 / 9
    normalized_duplicate = dict(base)
    normalized_duplicate["fitness_function"] = "logic-density"
    cache = tmp_path / "seen.json"

    stdout, stderr, code = _run_seen(
        {"status": "block", "violations": [base, affected, function, interface, value,
                                            normalized_duplicate]},
        cache,
    )

    assert code == 0, stderr
    assert stderr == ""
    report = _parse_report(stdout)
    entries = report["violations"]
    assert len(entries) == 5
    observed = {
        (
            entry["affected_file"],
            entry.get("function", ""),
            entry.get("interface", ""),
            entry["value"],
            entry["fitness_function"],
        )
        for entry in entries
    }
    assert observed == {
        ("configs/embed.go", "load", "Assets", 1 / 9, "logic-density"),
        ("internal/other.go", "load", "Assets", 1 / 9, "logic-density"),
        ("configs/embed.go", "render", "Assets", 1 / 9, "logic-density"),
        ("configs/embed.go", "load", "OtherAssets", 1 / 9, "logic-density"),
        ("configs/embed.go", "load", "Assets", 2 / 9, "logic-density"),
    }
    assert sum(entry["value"] == 1 / 9 for entry in entries) == 4
    assert sum(entry["value"] == 2 / 9 for entry in entries) == 1


def test_seen_file_deduplicates_repeated_entries_in_response_in_first_order(
    tmp_path: Path,
) -> None:
    first = _attributed_violation()
    second = dict(first)
    second["file"] = "internal/other.go"
    third = dict(first)
    third["function"] = "render"
    cache = tmp_path / "seen.json"

    stdout, stderr, code = _run_seen(
        {"status": "block", "violations": [first, second, third, dict(first)]},
        cache,
    )

    assert code == 0, stderr
    assert stderr == ""
    entries = _parse_report(stdout)["violations"]
    assert [
        (entry["affected_file"], entry["function"], entry["value"])
        for entry in entries
    ] == [
        ("configs/embed.go", "load", 1 / 9),
        ("internal/other.go", "load", 1 / 9),
        ("configs/embed.go", "render", 1 / 9),
    ]


def test_seen_file_keeps_unknown_attribution_distinct_by_checked_path(tmp_path: Path) -> None:
    violation = _attributed_violation()
    violation.pop("file")
    cache = tmp_path / "seen.json"

    first_stdout, first_stderr, first_code = _run_seen(
        {"status": "block", "violations": [violation]},
        cache,
        "internal/clean.go",
    )
    second_stdout, second_stderr, second_code = _run_seen(
        {"status": "block", "violations": [violation]},
        cache,
        "cmd/main.go",
    )

    assert first_code == second_code == 0
    assert first_stderr == second_stderr == ""
    first = _parse_report(first_stdout)
    second = _parse_report(second_stdout)
    assert first["calm_check"]["checked_file"] == "internal/clean.go"
    assert second["calm_check"]["checked_file"] == "cmd/main.go"
    assert first["violations"][0]["affected_file"] == "(unknown)"
    assert second["violations"][0]["affected_file"] == "(unknown)"
    assert first["violations"][0]["value"] == second["violations"][0]["value"] == 1 / 9
    assert first["violations"][0]["limit"] == second["violations"][0]["limit"] == 0.255


def test_seen_file_fresh_cache_displays_finding_again(tmp_path: Path) -> None:
    payload = {"status": "block", "violations": [_attributed_violation()]}
    first_stdout, first_stderr, first_code = _run_seen(payload, tmp_path / "first.json")
    second_stdout, second_stderr, second_code = _run_seen(payload, tmp_path / "second.json")

    assert first_code == second_code == 0
    assert first_stderr == second_stderr == ""
    for stdout, checked_file in (
        (first_stdout, "internal/clean.go"),
        (second_stdout, "internal/clean.go"),
    ):
        report = _parse_report(stdout)
        assert report["calm_check"]["checked_file"] == checked_file
        entry = report["violations"][0]
        assert entry["affected_file"] == "configs/embed.go"
        assert entry["value"] == 1 / 9
        assert entry["limit"] == 0.255


@pytest.mark.parametrize("initial_contents", [None, ""])
def test_seen_file_absent_or_empty_cache_is_usable(
    tmp_path: Path,
    initial_contents: str | None,
) -> None:
    cache = tmp_path / "seen.json"
    if initial_contents is not None:
        cache.write_text(initial_contents)

    stdout, stderr, code = _run_seen(
        {"status": "block", "violations": [_attributed_violation()]},
        cache,
    )

    assert code == 0, stderr
    assert stderr == ""
    report = _parse_report(stdout)
    assert report["calm_check"]["checked_file"] == "internal/clean.go"
    entry = report["violations"][0]
    assert entry["affected_file"] == "configs/embed.go"
    assert entry["value"] == 1 / 9
    assert entry["limit"] == 0.255


@pytest.mark.parametrize(
    "initial_contents,make_directory",
    [
        ("not json", False),
        ("{}", False),
        ('["not-a-canonical-diagnostic-key"]', False),
        ("", True),
    ],
)
def test_seen_file_read_failures_print_full_attributed_diagnostic(
    tmp_path: Path,
    initial_contents: str,
    make_directory: bool,
) -> None:
    cache = tmp_path / "seen"
    if make_directory:
        cache.mkdir()
    else:
        cache.write_text(initial_contents)

    stdout, stderr, code = _run_seen(
        {"status": "block", "violations": [_attributed_violation()]},
        cache,
    )

    assert code == 0
    assert "format-violations: cannot read seen-file" in stderr
    report = _parse_report(stdout)
    assert report["calm_check"]["checked_file"] == "internal/clean.go"
    assert report["calm_check"]["scope"] == "repository"
    entry = report["violations"][0]
    assert entry["affected_file"] == "configs/embed.go"
    assert entry["value"] == 1 / 9
    assert entry["limit"] == 0.255


def test_seen_file_write_failure_prints_full_attributed_diagnostic(tmp_path: Path) -> None:
    cache = tmp_path / "missing-parent" / "seen.json"

    stdout, stderr, code = _run_seen(
        {"status": "block", "violations": [_attributed_violation()]},
        cache,
    )

    assert code == 0
    assert "format-violations: cannot write seen-file" in stderr
    report = _parse_report(stdout)
    assert report["calm_check"]["checked_file"] == "internal/clean.go"
    assert report["calm_check"]["scope"] == "repository"
    entry = report["violations"][0]
    assert entry["affected_file"] == "configs/embed.go"
    assert entry["value"] == 1 / 9
    assert entry["limit"] == 0.255


@pytest.mark.parametrize("status,scope", [("block", "repository"), ("advisory", "file")])
def test_subprocess_checked_and_affected_attribution(status: str, scope: str) -> None:
    import yaml

    violation = _attributed_violation()
    stdout, stderr, code = _run(
        json.dumps({"status": status, "violations": [violation]}),
        ["--mode", "advisory", "--checked-file", "internal/clean.go"],
    )
    assert code == 0, stderr
    output = yaml.safe_load(stdout)
    assert output["calm_check"]["checked_file"] == "internal/clean.go"
    assert output["calm_check"]["scope"] == scope
    assert "file" not in output["calm_check"]
    entry = output["violations"][0]
    assert entry["affected_file"] == "configs/embed.go"
    for field in ("function", "interface", "calm_node", "value", "limit", "message", "source_kind", "rule"):
        assert entry[field] == violation[field]


def test_subprocess_missing_attribution_stays_unknown() -> None:
    import yaml

    stdout, stderr, code = _run(json.dumps({"status": "block", "violations": [_viol()]}))
    assert code == 0
    output = yaml.safe_load(stdout)
    assert output["calm_check"]["checked_file"] == "(unknown)"
    assert output["violations"][0]["affected_file"] == "(unknown)"
    assert "--checked-file not provided; checked file is unknown" in stderr


def test_yaml_failure_preserves_attributed_diagnostic(monkeypatch: Any, capsys: Any) -> None:
    def fail_dump(*args: Any, **kwargs: Any) -> None:
        raise RuntimeError("renderer unavailable")

    output = _fmt._build_output([_attributed_violation()], "internal/clean.go", "block", "block")
    monkeypatch.setattr(_fmt.yaml, "dump", fail_dump)
    _fmt._render_yaml_or_fallback(output)
    rendered = json.loads(capsys.readouterr().out)
    assert rendered["calm_check"]["checked_file"] == "internal/clean.go"
    assert rendered["calm_check"]["scope"] == "repository"
    entry = rendered["violations"][0]
    assert entry["affected_file"] == "configs/embed.go"
    assert entry["value"] == 1 / 9
    assert entry["limit"] == 0.255
