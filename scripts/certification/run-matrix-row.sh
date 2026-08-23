#!/usr/bin/env bash
# Runs one fresh-project certification matrix row (calm-poc-q8d.6) per
# docs/certification/fresh-project-matrix.md: isolated HOME/XDG state,
# checksummed release install with provisioned runtimes, onboarding in
# block mode, bad-edit block, corrected-edit pass, --no-verify bypass
# blocked at push, clean push, idempotent re-onboarding, and cleanup.
# Usage: run-matrix-row.sh <go|python|csharp> <source-checkout> <evidence-file>
set -euo pipefail

language=${1:?usage: run-matrix-row.sh <go|python|csharp> <source-checkout> <evidence-file>}
source_checkout=${2:?source checkout path required}
evidence=${3:?evidence file path required}

case "$language" in
go)
  bad_fixture=fixtures/violations/go/cyclomatic-complexity.go
  green_fixture=fixtures/green/go/cyclomatic-complexity.go
  target_file=complexity.go
  ;;
python)
  bad_fixture=fixtures/violations/python/cyclomatic_complexity.py
  green_fixture=fixtures/green/python/cyclomatic_complexity.py
  target_file=complexity.py
  ;;
csharp)
  bad_fixture=fixtures/violations/csharp/CyclomaticComplexity.cs
  green_fixture=fixtures/green/csharp/CyclomaticComplexity.cs
  target_file=Complexity.cs
  ;;
*)
  echo "unsupported language: $language" >&2
  exit 2
  ;;
esac

work=$(mktemp -d "${TMPDIR:-/tmp}/matrix-$language.XXXXXX")
repo="$work/matrix-$language"
remote="$work/remote.git"
daemon_pids=()

note() { printf '\n=== %s ===\n' "$1" | tee -a "$evidence"; }
run_logged() { "$@" 2>&1 | tee -a "$evidence"; }

cleanup() {
  status=$?
  set +e
  if [[ ${#daemon_pids[@]} -gt 0 ]]; then
    kill "${daemon_pids[@]}" 2>/dev/null
  fi
  pkill -f "$work/state/agent-fitness-functions" 2>/dev/null
  sleep 1
  if pgrep -f "$work/state/agent-fitness-functions" >/dev/null 2>&1; then
    echo "cleanup: agent-fitness-functions process survived" | tee -a "$evidence"
    status=1
  fi
  rm -rf "$work"
  if [[ -d "$work" ]]; then
    echo "cleanup: work directory survived" | tee -a "$evidence"
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT

: > "$evidence"
note "matrix row: $language · source commit $(git -C "$source_checkout" rev-parse --short HEAD) · $(date -u +%Y-%m-%dT%H:%M:%SZ)"

note "step 1: package release candidate"
run_logged env DOTNET_ROOT="$HOME/.dotnet" bash "$source_checkout/scripts/package-release.sh" --output "$work/pkg" | tail -2
archive=$(find "$work/pkg" -name '*.tar.gz' | head -1)

note "step 2: isolated install with provisioned runtimes"
export HOME="$work/home" XDG_STATE_HOME="$work/state"
mkdir -p "$HOME"
unset AGENT_FITNESS_FUNCTIONS_BIN 2>/dev/null || true
run_logged bash "$source_checkout/scripts/install.sh" --archive "$archive" --checksums "$work/pkg/SHA256SUMS" --provision-runtimes | tail -4
export PATH="$XDG_STATE_HOME/agent-fitness-functions/current/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
run_logged agent-fitness-functions runtime doctor

note "step 3: fresh $language repo, native baseline, bare remote, onboard"
mkdir -p "$repo" && cd "$repo"
git init -q . && git config user.email demo@example.com && git config user.name "Matrix Demo"
case "$language" in
go)
  printf 'module example.com/matrix-go\n\ngo 1.22\n' > go.mod
  printf 'package main\n\nfunc main() {}\n' > main.go
  ;;
python)
  printf 'def baseline() -> int:\n    return 1\n' > baseline.py
  python3 -m py_compile baseline.py
  ;;
csharp)
  printf '// matrix baseline\n' > Baseline.cs
  ;;
esac
git add -A && git commit -qm baseline
git init -q --bare "$remote"
git remote add origin "$remote"
git push -q origin HEAD
run_logged agent-fitness-functions client onboard --enforcement block | tail -6
while IFS= read -r daemon_pid; do
  daemon_pids+=("$daemon_pid")
done < <(pgrep -f "$work/state/agent-fitness-functions.*server start" || true)

note "step 4: bad edit blocks"
cp "$source_checkout/$bad_fixture" "$target_file"
git add "$target_file"
if git commit -m "bad edit" >>"$evidence" 2>&1; then
  echo "FAIL: bad edit was committed" | tee -a "$evidence"
  exit 1
fi
echo "blocked as expected; hook output above" | tee -a "$evidence"

note "step 5: corrected edit passes"
cp "$source_checkout/$green_fixture" "$target_file"
git add "$target_file"
run_logged git commit -m "corrected edit" | tail -2

note "step 6: --no-verify bypass blocked at push"
cp "$source_checkout/$bad_fixture" "$target_file"
git add "$target_file"
git commit -q --no-verify -m "bypass attempt"
if git push origin HEAD >>"$evidence" 2>&1; then
  echo "FAIL: bypass commit was pushed" | tee -a "$evidence"
  exit 1
fi
echo "push blocked as expected; hook output above" | tee -a "$evidence"
git reset -q --hard HEAD~1

note "step 7: clean push passes"
run_logged git push origin HEAD | tail -2

note "step 8: re-onboarding idempotent"
hooks_before=$(cat .git/hooks/pre-commit .git/hooks/pre-push .claude/settings.json | shasum -a 256)
run_logged agent-fitness-functions client onboard --enforcement block | tail -2
hooks_after=$(cat .git/hooks/pre-commit .git/hooks/pre-push .claude/settings.json | shasum -a 256)
if [[ "$hooks_before" != "$hooks_after" ]]; then
  echo "FAIL: re-onboarding changed installed hooks" | tee -a "$evidence"
  exit 1
fi
echo "re-onboarding byte-stable" | tee -a "$evidence"

note "row result: $language PASS"
