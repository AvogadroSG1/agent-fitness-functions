# calm-poc-r69 Task 1 Report

## Summary

I fixed SARIF URI rooting so already-relative violation paths stay stable, while absolute paths still flow through `filepath.Rel` when a repo root is available.

## RED

I added a regression test for a repo-relative violation path:

- `internal/foo/bar.go`

The first pass reproduced the bug with a relative repo root and failed with:

- `uri = "../../internal/foo/bar.go", want internal/foo/bar.go`

The brief’s absolute-root example did not fail in this environment, so I used the relative-root case that actually exposed the bad re-rooting behavior.

## GREEN

I updated `relURI` in `internal/sarif/sarif.go` to return already-relative file paths unchanged before attempting `filepath.Rel`.

That keeps repo-relative violation paths stable and preserves the existing absolute-path relativization behavior.

## Verification

- `GOCACHE=$PWD/.cache/go-build go test ./internal/sarif -run TestConvertKeepsRelativeViolationPathsStable -count=1`
- `GOCACHE=$PWD/.cache/go-build go test ./internal/sarif -count=1`

## Notes

- Scope stayed limited to `internal/sarif/sarif.go` and `internal/sarif/sarif_test.go`.
- No unrelated worktree files were modified.

*Authored By Peter O'Connor with Assistance from Claude Code (gpt-5) · 2026-06-19 · calm-poc-r69 Task 1 SARIF relative-path rooting*
