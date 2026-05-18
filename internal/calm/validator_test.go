package calm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidatorPassesArchitectureAndPatternToCalmCLI(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calm.log")
	cli := fakeCalm(t, dir, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "$CALM_LOG"
printf '{"status":"pass"}'
`)
	validator := Validator{CLIPath: cli, Env: []string{"CALM_LOG=" + logPath}}

	result, err := validator.Validate(context.Background(), "current-architecture.json", "governance.json")
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if !result.Valid || strings.TrimSpace(result.Output) != `{"status":"pass"}` {
		t.Fatalf("result = %+v, want valid pass with stdout", result)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.TrimSpace(string(logContent)) != "validate --architecture current-architecture.json --pattern governance.json" {
		t.Fatalf("args = %q, want calm validate invocation", logContent)
	}
}

func TestValidatorReturnsViolationJSONOnFailure(t *testing.T) {
	dir := t.TempDir()
	cli := fakeCalm(t, dir, `#!/usr/bin/env bash
set -euo pipefail
printf 'schema warning\n' >&2
printf '{"violations":[{"message":"too complex"}]}'
exit 3
`)
	validator := Validator{CLIPath: cli}

	result, err := validator.Validate(context.Background(), "bad-architecture.json", "governance.json")
	if err == nil {
		t.Fatal("Validate returned nil error, want calm failure")
	}
	if result.Valid {
		t.Fatalf("result = %+v, want invalid result", result)
	}
	if !strings.Contains(result.Output, "too complex") || !strings.Contains(result.ErrorOutput, "schema warning") {
		t.Fatalf("result = %+v, want stdout violation JSON and stderr", result)
	}
	if !strings.Contains(err.Error(), "calm validate failed") || !strings.Contains(err.Error(), "schema warning") {
		t.Fatalf("error = %v, want failure context", err)
	}
}

func TestValidatorReturnsMissingExecutableErrors(t *testing.T) {
	validator := Validator{CLIPath: filepath.Join(t.TempDir(), "missing-calm")}

	result, err := validator.Validate(context.Background(), "current-architecture.json", "governance.json")
	if err == nil {
		t.Fatal("Validate returned nil error, want missing executable error")
	}
	if result.Valid {
		t.Fatalf("result = %+v, want invalid result", result)
	}
	if !strings.Contains(err.Error(), "calm validate failed") {
		t.Fatalf("error = %v, want calm validate failure context", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want wrapped os.ErrNotExist", err)
	}
}

func TestValidatorPreservesDeadlineExceeded(t *testing.T) {
	dir := t.TempDir()
	cli := fakeCalm(t, dir, `#!/usr/bin/env bash
sleep 5
`)
	validator := Validator{CLIPath: cli, Timeout: 10}

	_, err := validator.Validate(context.Background(), "current-architecture.json", "governance.json")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestValidatorPreservesParentContextCancellation(t *testing.T) {
	dir := t.TempDir()
	cli := fakeCalm(t, dir, `#!/usr/bin/env bash
sleep 5
`)
	validator := Validator{CLIPath: cli}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := validator.Validate(ctx, "current-architecture.json", "governance.json")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestValidatorFailureFallsBackToStdoutWhenStderrIsBlank(t *testing.T) {
	dir := t.TempDir()
	cli := fakeCalm(t, dir, `#!/usr/bin/env bash
set -euo pipefail
printf '   \n' >&2
printf '{"violations":[{"message":"too complex"}]}'
exit 3
`)
	validator := Validator{CLIPath: cli}

	_, err := validator.Validate(context.Background(), "bad-architecture.json", "governance.json")
	if err == nil || !strings.Contains(err.Error(), "too complex") {
		t.Fatalf("error = %v, want stdout violation detail", err)
	}
}

func fakeCalm(t *testing.T, dir, script string) string {
	t.Helper()
	name := "calm"
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake calm: %v", err)
	}
	return path
}
