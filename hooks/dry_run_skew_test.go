package hooks

// S6 (calm-poc-cpvk): the agent pre-write hook asks for a dry-run validation so
// a proposal that may never be written cannot poison the repository's violation
// ledger. The flag is younger than the binaries installed on developer machines,
// so the hook must survive binary/hook skew: a client that rejects --dry-run is
// retried without it rather than turned into a blocked edit.

import (
	"path/filepath"
	"strings"
	"testing"
)

// dryRunAwareBin is a current client: it records its arguments and passes.
const dryRunAwareBin = `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
printf '{"status":"pass"}\n'
`

// predecessorBin is a client from before --dry-run existed: Go's flag package
// rejects the unknown flag on stderr with exit code 2, and the same binary
// validates normally when the flag is absent.
const predecessorBin = `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
for arg in "$@"; do
  if [[ "$arg" == "--dry-run" ]]; then
    echo "flag provided but not defined: -dry-run" >&2
    exit 2
  fi
done
printf '{"status":"pass"}\n'
`

const dryRunPayload = `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Run() {}\n"}}`

func TestPreToolUseRequestsDryRunValidation(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	logPath := filepath.Join(t.TempDir(), "calls.log")

	output, err := runPreToolUse(t, repo, dryRunPayload, fakeFitnessBin(t, dryRunAwareBin), logPath, "")
	if err != nil {
		t.Fatalf("pre-tool-use failed on a passing validation: %v\n%s", err, output)
	}
	calls := readFile(t, logPath)
	if !strings.Contains(calls, "--dry-run") {
		t.Fatalf("client calls = %q, want the pre-write hook to request a dry run", calls)
	}
}

func TestPreToolUseFallsBackWhenBinaryPredatesDryRun(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	logPath := filepath.Join(t.TempDir(), "calls.log")

	output, err := runPreToolUse(t, repo, dryRunPayload, fakeFitnessBin(t, predecessorBin), logPath, "")
	if err != nil {
		t.Fatalf("pre-tool-use blocked the edit on binary/hook skew: %v\n%s", err, output)
	}
	calls := strings.Split(strings.TrimSpace(readFile(t, logPath)), "\n")
	if len(calls) != 2 {
		t.Fatalf("client calls = %q, want the dry-run attempt followed by one wet retry", calls)
	}
	if !strings.Contains(calls[0], "--dry-run") {
		t.Errorf("first call = %q, want the dry-run attempt", calls[0])
	}
	if strings.Contains(calls[1], "--dry-run") {
		t.Errorf("retry = %q, want the flag dropped so an older binary can validate", calls[1])
	}
	if !strings.Contains(string(output), "predates --dry-run") {
		t.Errorf("output = %q, want the skew named so the binary gets re-installed", output)
	}
}

// A usage error that is not the unknown --dry-run flag stays a blocked edit: the
// fallback exists for one specific skew, not to soften every client failure.
func TestPreToolUseDoesNotRetryUnrelatedUsageErrors(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	logPath := filepath.Join(t.TempDir(), "calls.log")
	brokenBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
echo "flag provided but not defined: -content-file" >&2
exit 2
`)

	output, err := runPreToolUse(t, repo, dryRunPayload, brokenBin, logPath, "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want 2 for an unrelated usage error; output=%s", err, output)
	}
	if calls := strings.Split(strings.TrimSpace(readFile(t, logPath)), "\n"); len(calls) != 1 {
		t.Fatalf("client calls = %q, want exactly one attempt", calls)
	}
}
