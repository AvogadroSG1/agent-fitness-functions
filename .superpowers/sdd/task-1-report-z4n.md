# calm-poc-z4n Task 1 Report

## Outcome

Task 1 is complete. `RunInstallHooks` now refreshes legacy sidecar-backed hooks so the active hook file points at the new `stack-fitness-functions-*` sidecar path instead of the old `calm-*` sidecar path.

## TDD Trace

### RED

I added `TestRunInstallHooksRefreshesLegacySidecarReferences` in `internal/client/client_test.go`. The first run failed as expected because the hook marker was updated but the active hook still referenced the legacy `calm-pre-commit` / `calm-pre-push` sidecar paths.

### GREEN

I updated `internal/client/client.go` so the sidecar refresh path now:

- writes the new embedded sidecar executable
- rewrites the active hook to the canonical `stack-fitness-functions` sidecar wrapper
- rewrites the formatter
- removes the old `calm-*` sidecar file after the rewrite succeeds

I kept the installer logic small by isolating the new hook-content generation in `managedSidecarHookContent` and keeping the refresh path in `refreshHookSidecar`.

## Verification

- `GOCACHE=$PWD/.cache/go-build go test ./internal/client -run TestRunInstallHooksRefreshesLegacySidecarReferences -count=1`
- `GOCACHE=$PWD/.cache/go-build go test ./internal/client -run 'TestRunInstallHooks|TestHookInstallerFunctionsStayWithinCyclomaticComplexityBudget' -count=1`

I also attempted `GOCACHE=$PWD/.cache/go-build go test ./internal/client -count=1`, but it failed in an unrelated `httptest` case because this environment cannot bind the local IPv6 listener used by `httptest.NewServer`.

## Notes

The regression test uses the canonical repo path for expectations so the `/var` versus `/private/var` symlink split on macOS does not cause a false failure.

