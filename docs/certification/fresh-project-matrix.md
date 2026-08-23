# Fresh-project certification matrix (calm-poc-q8d.6)

Orchestrator-owned definition of the fixed language matrix for the
fresh-project agent-fitness-functions release. Implementors solve assigned
rows and record evidence; they MUST NOT redefine fixtures or expected
outcomes. The orchestrator independently reruns every row before sign-off.

## Isolation requirements (every row)

- Isolated `HOME` and `XDG_STATE_HOME` (temp directories), fresh shell.
- The release candidate is a checksummed archive produced by
  `scripts/package-release.sh`; installation happens only through
  `scripts/install.sh --archive <path> --checksums <path>
  --provision-runtimes`.
- `AGENT_FITNESS_FUNCTIONS_BIN` MUST be unset. No source-relative
  binaries, no scratch venvs, no pre-existing daemons. `PATH` gains only
  the installed `current/bin`.
- Server-side state for each row repo: `configs/<repo>/config.json` with
  `enforcement-mode: block` (from `fixtures/block-template.json`) and a
  caller-repos authorization entry, per
  `docs/runbooks/onboard-new-repository.md`.
- Cleanup MUST kill any daemon started by the row, remove the temp repos,
  state root, and server config additions, and verify no
  `agent-fitness-functions` process survives.

## Row steps (Given/When/Then, identical for every language)

1. **Native baseline** — the fresh project builds/passes its native
   toolchain before governance is added (`go build ./...`, `python3 -m
   py_compile *.py`, `dotnet build` is NOT required for C# — the analyzer
   is self-contained; native baseline for C# is `csc`-free file creation
   plus git init).
2. **Install** — `install.sh` succeeds; `current/bin/agent-fitness-functions --help`
   exits 0; `runtime doctor` exits 0.
3. **Onboard** — `agent-fitness-functions client onboard --enforcement block`
   (per runbook) succeeds; `doctor` passes; hooks installed (pre-commit,
   pre-push, git-guard, pre-tool-use present and executable).
4. **Bad edit blocks** — copy the row's violation fixture into the repo;
   `git add` + `git commit` MUST be blocked by the pre-commit hook with a
   violation naming the expected fitness function (see table).
5. **Corrected edit passes** — replace with the row's green fixture;
   commit succeeds.
6. **Bypass blocks** — commit the violation fixture with
   `git commit --no-verify` (bypassing pre-commit), then `git push`; the
   pre-push hook MUST block with the same violation class. (The demo
   remote is a local bare repo created during setup.)
7. **Clean commit passes** — restore the green fixture state; commit and
   push succeed.
8. **Re-onboarding idempotent** — rerun `client onboard`; zero duplicate
   hooks, sidecars, or settings entries; hook files byte-stable.
9. **Cleanup** — per isolation requirements.

## Fixture and expected-outcome table

| Row | Violation fixture | Expected blocking fitness function | Green fixture |
|---|---|---|---|
| Go | `fixtures/violations/go/cyclomatic-complexity.go` | cyclomatic complexity | `fixtures/green/go/cyclomatic-complexity.go` |
| Python | `fixtures/violations/python/cyclomatic_complexity.py` | cyclomatic complexity | `fixtures/green/python/cyclomatic_complexity.py` |
| C# | `fixtures/violations/csharp/CyclomaticComplexity.cs` | cyclomatic complexity | `fixtures/green/csharp/CyclomaticComplexity.cs` |
| Forge Go (optional) | as Go row, inside a Forge-generated repo with Beads hooks installed | cyclomatic complexity; Forge guards and Beads block survive composition | as Go row |

Fixture files are copied from the release-candidate checkout at the
pinned integration commit; rows MUST record the commit hash they copied
from. The violation fixtures are calibrated by `fixtures/fixtures_test.go`
to trip exactly their named function — a row observing a different
function name is a defect, not a tolerance.

## Evidence format (per row)

Each row records, verbatim: install output tail, onboard output, the
blocked commit's hook output (with the named fitness function), the
passing commit hash, the blocked push output, the idempotency diff
(empty), and the cleanup verification. Evidence lands in the q8d.4
readiness report.

*Authored By Peter O'Connor with Assistance from Claude Code (claude-fable-5) · 2026-08-23 · calm-poc-q8d.6 matrix definition*
