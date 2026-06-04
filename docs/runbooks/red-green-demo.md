> **Historical document.** This runbook was written for the PoC local-only architecture.
> It describes running `calm-bridge` built to `/tmp/calm-bridge` on loopback.
> For the current container governance model, see [CONTEXT.md](../../CONTEXT.md) and
> [README.md](../../README.md). Steps in this runbook remain valid for local sandbox
> verification but must not be used as production deployment guidance.

---

# CALM PoC Red-Green Demo Runbook

This runbook validates that CALM fitness functions block known violations in block-mode repositories, allow fixed code, and produce advisory guidance in advisory-mode repositories.

## Prerequisites

- Start the bridge container: `calm-serve` (or `calm-serve --build` to force a rebuild) — this starts the Docker Desktop container on `localhost:7890`
- Install the git hook in each target repository: `scripts/install-hooks.sh <repo>`
- Run commits with `CALM_BRIDGE_ADDR=http://localhost:7890 CALM_ALLOW_REMOTE_BRIDGE=1`

> **Local binary alternative (sandbox only):** Build the bridge locally with `go build -o .tmp/calm-bridge ./cmd/calm-bridge` and start it with `.tmp/calm-bridge serve --addr 127.0.0.1:7890`. Use `CALM_BRIDGE_BIN=.tmp/calm-bridge CALM_BRIDGE_ADDR=http://127.0.0.1:7890` for commits. This path is only valid for developer sandbox iteration; it MUST NOT be used as the production path.
- Use per-demo `.calm/config.json` files that explicitly disable every non-target fitness function. Missing fitness-function keys default to enabled.

The hook refuses non-loopback bridge addresses unless `CALM_ALLOW_REMOTE_BRIDGE=1` is set. The installer overwrites prior CALM-managed hooks and refuses unrelated existing hooks unless `CALM_HOOK_OVERWRITE=1` is set.

## Fixture Matrix

| Fitness function | Go block fixture | Python advisory fixture | C# block fixture | Green fixture |
|---|---|---|---|---|
| Cyclomatic Complexity | `fixtures/violations/go/cyclomatic-complexity.go` | `fixtures/violations/python/cyclomatic_complexity.py` | `fixtures/violations/csharp/CyclomaticComplexity.cs` | Matching file under `fixtures/green/<language>/` |
| Interface Width | `fixtures/violations/go/interface-width.go` | `fixtures/violations/python/interface_width.py` | `fixtures/violations/csharp/InterfaceWidth.cs` | Matching file under `fixtures/green/<language>/` |
| Implementation Depth | Not applicable with the calibrated Go analyzer threshold | Not applicable with the calibrated Python analyzer threshold | `fixtures/violations/csharp/ImplementationDepth.cs` | `fixtures/green/csharp/ImplementationDepth.cs` |
| Logic Density Ratio | `fixtures/violations/go/logic-density.go` | `fixtures/violations/python/logic_density.py` | `fixtures/violations/csharp/LogicDensity.cs` | Matching file under `fixtures/green/<language>/` |
| Dependency Discipline | `fixtures/violations/go/dependency-discipline.go` | `fixtures/violations/python/dependency_discipline.py` | `fixtures/violations/csharp/DependencyDiscipline.cs` | Matching file under `fixtures/green/<language>/` |

The active thresholds are Cyclomatic Complexity `<= 9`, Interface Width `<= 20`, Implementation Depth `>= 0.722`, Logic Density Ratio `>= 0.255`, and Dependency Discipline `>= 0.8`.

Implementation Depth is C#-only in this runbook because the calibrated threshold is `0.722`. The current Go analyzer reports at least one logic line per public function and the current Python analyzer reports at least two logic lines per public function, so neither can produce a real source fixture below that threshold without fake analyzer data. This is an intentional runbook exception to the broad Section 11 matrix.

## Configuration Template

Use one active function per run:

```json
{
  "enforcement-mode": "block",
  "fitness-functions": {
    "cyclomatic-complexity": true,
    "interface-width": false,
    "implementation-depth": false,
    "logic-density": false,
    "dependency-discipline": false
  }
}
```

For other functions, set the target function to `true` and every non-target function to `false`. For the ringstation advisory run, set `"enforcement-mode": "advisory"` and keep the same single enabled fitness function.

## Go Block Demo: graft

1. Install the hook in `graft`.
2. Copy a Go red fixture into the target package path, for example `internal/demo/demo.go`.
3. Write `.calm/config.json` with `enforcement-mode` set to `block` and only the target function enabled.
4. Stage the config and fixture: `git add .calm/config.json internal/demo/demo.go`.
5. Attempt `git commit -m "red calm demo"`.
6. Expected result: the hook exits `1`, prints the CALM violation for the enabled function, and blocks the commit. For DDC, verify that the output includes the unused import list.
7. Replace the file with the matching green fixture from `fixtures/green/go/`.
8. Stage and commit again.
9. Expected result: the hook exits `0` and the commit succeeds.

Fast verification:

```bash
scripts/smoke-red-green-go.sh
```

The smoke script runs Cyclomatic Complexity, Interface Width, Logic Density Ratio, and Dependency Discipline for Go. It intentionally skips Implementation Depth for Go for the calibration reason documented above.

## C# Block Demo: SlackStatus

1. Install the hook in `SlackStatus`.
2. Warm the C# analyzer with a clean file before the red commit. The first cold C# check returns `pass` with `warming=true` by design, so run a clean check and wait for a non-warming response before the blocking demo:

```bash
for _ in {1..30}; do
  result=$(.tmp/calm-bridge check \
    --addr http://127.0.0.1:7890 \
    --repo <SlackStatus repo> \
    --file src/Demo/Warmup.cs \
    --content "$(cat fixtures/green/csharp/CyclomaticComplexity.cs)" \
    --language csharp)
  printf '%s\n' "$result"
  if ! printf '%s\n' "$result" | grep -q '"warming":true'; then
    break
  fi
  sleep 0.2
done
```

3. Copy the C# red fixture into a source path such as `src/Demo/DemoFixture.cs`.
4. Write `.calm/config.json` with `enforcement-mode` set to `block` and one target fitness function enabled.
5. Stage and attempt a commit.
6. Expected result: the warmed hook blocks and prints the violation message for the enabled function. For DDC, verify that the output includes the unused import list.
7. Replace the file with the matching green fixture from `fixtures/green/csharp/`.
8. Stage and commit again.
9. Expected result: the commit succeeds.

Use `fixtures/violations/csharp/ImplementationDepth.cs` for the Implementation Depth demo. The auto-property accessors produce `avg_loc_per_public_method = 0.5`, which violates the calibrated minimum of `0.722`.

C# Dependency Discipline is provisional. The fixture proves the current single-file Roslyn behavior, but project-aware C# import resolution remains tracked in `calm-poc-oeu` and C# DDC results must not be treated as final until that follow-up closes.

`StackOverflow/StackOverflow.Api.V3` is also a C# block-mode target repository. Use the same C# fixture sequence there when validating broader StackOverflow platform behavior. The concise manual acceptance path for this runbook is `SlackStatus`; `StackOverflow.Api.V3` is an additional C# parity check rather than a distinct fixture set.

## Python Advisory Demo: ringstation

1. Install the hook in `ringstation`.
2. Copy the Python red fixture into a Databricks or Python module path.
3. Write `.calm/config.json` with `enforcement-mode` set to `advisory` and one target fitness function enabled.
4. Stage and attempt a commit.
5. Expected result: the hook exits `0`, prints `CALM advisory`, and allows the commit. For DDC, verify that the output includes the unused import list.
6. Replace the file with the matching green fixture from `fixtures/green/python/`.
7. Stage and commit again.
8. Expected result: the commit succeeds without an advisory for the target rule.

The existing `fixtures/violations/python/ringstation-dd-stage-bronze.py` remains available as a realistic ringstation-style composite fixture. The focused `dependency_discipline.py` fixture is preferred when the goal is to isolate DDC only.

## Troubleshooting

- If the hook says `CALM_BRIDGE_ADDR must be loopback`, use `http://127.0.0.1:<port>` or explicitly set `CALM_ALLOW_REMOTE_BRIDGE=1` for a trusted remote daemon.
- If installation refuses to overwrite a hook, inspect the existing hook. Set `CALM_HOOK_APPEND=1` to install CALM as a sidecar alongside the existing hook (recommended when the existing hook must be preserved), or set `CALM_HOOK_OVERWRITE=1` to replace it entirely.
- If a red commit unexpectedly passes, confirm that `.calm/config.json` enables the intended function and that the staged file is the red fixture.
- If a red commit reports the wrong fitness function, confirm that every non-target function is explicitly set to `false` in `.calm/config.json`.
- If a green commit still blocks, re-stage the green file. Outstanding block-mode violations clear when the same file passes.

*Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-05-19 · CALM PoC Red-Green Demo Runbook*
