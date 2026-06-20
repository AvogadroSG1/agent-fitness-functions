# calm-poc-r69 Task 2 Report

## Scope

Aligned the remaining local validate callers with the established `--repo` and `--git-dir` split already used by `hooks/pre-commit.sh` and `hooks/pre-push.sh`.

Files changed:
- `hooks/pre-tool-use.sh`
- `hooks/pre_tool_use_test.go`
- `.vscode/tasks.json`

## RED

Command:

```bash
GOCACHE=$PWD/.cache/go-build go test ./hooks -run TestPreToolUseLocalModeSendsLogicalRepoNameAndGitDir -count=1
```

Result:

```text
--- FAIL: TestPreToolUseLocalModeSendsLogicalRepoNameAndGitDir (1.11s)
    pre_tool_use_test.go:151: missing "--repo relocate" in invocation:
        client validate --file sample.go --repo /private/var/folders/b8/tlzxl2w93ds2jhx4ljzdsdsm0000gp/T/TestPreToolUseLocalModeSendsLogicalRepoNameAndGitDir2733973119/001/feature+relocate-stats --content-file /var/folders/b8/tlzxl2w93ds2jhx4ljzdsdsm0000gp/T/tmp.Wn15AVyH6W --language go --addr https://127.0.0.1:7890
FAIL
FAIL	github.com/poconnor/calm-poc/hooks	1.358s
FAIL
```

This confirmed the bug from the brief: `pre-tool-use.sh` leaked the worktree path into `--repo` and omitted `--git-dir`.

## GREEN

Implementation:
- Added `TestPreToolUseLocalModeSendsLogicalRepoNameAndGitDir` to prove the worktree contract.
- Updated `hooks/pre-tool-use.sh` to derive `logical_repo_name()` from `git rev-parse --git-common-dir` when `STACK_FITNESS_FUNCTIONS_REPO_NAME` is not set.
- Updated `hooks/pre-tool-use.sh` to pass `--git-dir "$repo"` alongside the logical repo name.
- Updated `.vscode/tasks.json` so both validate tasks pass `--repo ${workspaceFolderBasename}` and `--git-dir ${workspaceFolder}`.

Focused re-run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./hooks -run TestPreToolUseLocalModeSendsLogicalRepoNameAndGitDir -count=1
```

```text
ok  	github.com/poconnor/calm-poc/hooks	1.385s
```

Cross-hook contract coverage:

```bash
GOCACHE=$PWD/.cache/go-build go test ./hooks -run 'TestPreToolUseLocalModeSendsLogicalRepoNameAndGitDir|TestPreCommitLocalModeSendsLogicalRepoNameAndGitDir|TestPrePushLocalModeSendsLogicalRepoNameAndGitDir' -count=1
```

```text
ok  	github.com/poconnor/calm-poc/hooks	3.591s
```

## Notes

- I left unrelated untracked planning documents under `docs/superpowers/plans/` unchanged.
- I did not modify any files outside the requested caller/test/task scope, except this report file.

*Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-06-19 · calm-poc-r69 Task 2 implementation report*
