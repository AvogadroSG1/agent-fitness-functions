# calm-poc-min Pre-Push Repo-Scoped Show Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the pre-push hook resolve pushed file content with `git -C "$repo" show` so content reads no longer depend on the current working directory.

**Architecture:** Keep the change narrow to the exact Boy-Scout gap Beads describes: the pre-push content read. Prove the bug with a hook-level regression test that inserts a `git` shim into `PATH` and rejects bare `git show`, then update both twin hook files so the source hook and embedded asset stay byte-identical.

**Tech Stack:** Bash hooks, Go tests, `go test`, embedded hook asset parity

## Global Constraints

- `stack-fitness-functions` MUST remain the product name in code, docs, and hook markers.
- TDD is REQUIRED: failing test first, then minimal production change, then refactor.
- Scope MUST stay limited to the bead: fix the bare `git show` content read and keep twin parity.
- `hooks/pre-push.sh` and `internal/client/hookassets/pre-push.sh` MUST remain byte-identical after the change.
- Existing pre-push local-mode behavior and mTLS forwarding behavior MUST stay green.

---

### Task 1: Add a Failing Regression Test for Repo-Scoped Content Reads

**Files:**
- Modify: `hooks/pre_push_test.go`
- Verify: `hooks/pre_push_test.go`

- [ ] **Step 1: Write the failing test**

Add this test beside the existing pre-push coverage:

```go
func TestPrePushReadsCommitContentWithRepoScopedGitShow(t *testing.T) {
	_, worktree := initNamedWorktree(t)
	writeFile(t, filepath.Join(worktree, "sample.go"), "package sample\n")
	runGit(t, worktree, "add", "sample.go")
	runGit(t, worktree, "commit", "-m", "add sample")
	headSHA := gitRevParse(t, worktree, "HEAD")
	baseSHA := gitRevParse(t, worktree, "HEAD~1")

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	gitShimDir := t.TempDir()
	gitShimPath := filepath.Join(gitShimDir, "git")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("locate git: %v", err)
	}
	if err := os.WriteFile(gitShimPath, []byte(fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "show" ]]; then
  echo "expected repo-scoped git show" >&2
  exit 97
fi
if [[ "${1:-}" == "-C" && "${3:-}" == "show" ]]; then
  printf 'git %s\n' "$*" >> "%s"
fi
exec "%s" "$@"
`, logPath, logPath, realGit)), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}

	command := exec.Command("bash", hookScriptPathFor(t, "pre-push.sh"))
	command.Dir = worktree
	command.Stdin = strings.NewReader("refs/heads/main " + headSHA + " refs/heads/main " + baseSHA + "\n")
	command.Env = append(os.Environ(),
		"PATH="+gitShimDir+string(os.PathListSeparator)+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-push failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	if !strings.Contains(got, "git -C "+worktree+" show") {
		t.Fatalf("expected repo-scoped git show in log:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./hooks -run TestPrePushReadsCommitContentWithRepoScopedGitShow -count=1
```

Expected: `FAIL` because the current hook invokes bare `git show`.

- [ ] **Step 3: Write the minimal production change**

Update both twin files to use the repo-scoped content read:

```bash
if ! git -C "$repo" show "${local_sha}:${file}" > "$content_file" 2>/dev/null; then
  continue
fi
```

Files to update:

```text
hooks/pre-push.sh
internal/client/hookassets/pre-push.sh
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./hooks -run TestPrePushReadsCommitContentWithRepoScopedGitShow -count=1
```

Expected: `PASS`

- [ ] **Step 5: Run the focused hook regression coverage**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./hooks -run 'TestPrePushReadsCommitContentWithRepoScopedGitShow|TestPrePushLocalModeSendsLogicalRepoNameAndGitDir|TestPrePushForwardsDiscoveredMTLSCerts' -count=1
GOCACHE=$PWD/.cache/go-build go test ./internal/client -run TestHookAssetsMatchSourceHooks -count=1
```

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add hooks/pre-push.sh hooks/pre_push_test.go internal/client/hookassets/pre-push.sh docs/superpowers/plans/2026-06-20-calm-poc-min-pre-push-repo-scoped-show.md
git commit -m "fix: scope pre-push git show to repo" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>\nCo-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```
