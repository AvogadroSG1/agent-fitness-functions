package calm_poc_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

func TestInstalledEmbeddedHooksConsumeGeneratedFirstPublication(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "test")
	if err := client.RunInstallHooks([]string{repo}, io.Discard, io.Discard); err != nil {
		t.Fatalf("RunInstallHooks: %v", err)
	}
	certRoot := filepath.Join(t.TempDir(), "managed-certs")
	if err := devcerts.Publish(certRoot, false); err != nil {
		t.Fatalf("Publish(first generation): %v", err)
	}
	if _, err := os.Readlink(filepath.Join(certRoot, "current")); err != nil {
		t.Fatalf("generated current is not a symlink: %v", err)
	}

	record := filepath.Join(t.TempDir(), "calls")
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "stack-fitness-functions")
	stubContent := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >>\"$STACK_FITNESS_FUNCTIONS_LOG\"\nprintf '{\"status\":\"pass\"}\\n'\n"
	if err := os.WriteFile(stub, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	env := append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+stub,
		"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certRoot,
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"STACK_FITNESS_FUNCTIONS_LOG="+record,
	)

	source := filepath.Join(repo, "sample.go")
	if err := os.WriteFile(source, []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runGitCommand(t, repo, "add", "sample.go")
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "pre-commit"), env, nil)

	payload := []byte(`{"tool_input":{"file_path":"sample.go","content":"package sample\n"}}`)
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "stack-fitness-functions-pre-tool-use"), env, payload)

	runGitCommand(t, repo, "commit", "--no-verify", "-m", "initial")
	base := gitRevision(t, repo, "HEAD")
	if err := os.WriteFile(source, []byte("package sample\n\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatalf("update source: %v", err)
	}
	runGitCommand(t, repo, "add", "sample.go")
	runGitCommand(t, repo, "commit", "--no-verify", "-m", "update")
	head := gitRevision(t, repo, "HEAD")
	pushInput := []byte("refs/heads/main " + head + " refs/heads/main " + base + "\n")
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "pre-push"), env, pushInput)

	calls, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read hook calls: %v", err)
	}
	for _, path := range []string{
		filepath.Join(certRoot, "current", "client.crt"),
		filepath.Join(certRoot, "current", "client.key"),
		filepath.Join(certRoot, "current", "ca.crt"),
	} {
		if count := bytes.Count(calls, []byte(path)); count < 3 {
			t.Fatalf("generated path %q appeared %d times, want all three installed hook kinds; calls:\n%s", path, count, calls)
		}
	}
}

func runInstalledHook(t *testing.T, repo, hook string, env []string, input []byte) {
	t.Helper()
	command := exec.Command("bash", hook)
	command.Dir = repo
	command.Env = env
	command.Stdin = bytes.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run installed hook %s: %v\n%s", filepath.Base(hook), err, output)
	}
}

func runGitCommand(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitRevision(t *testing.T, repo, revision string) string {
	t.Helper()
	command := exec.Command("git", "-C", repo, "rev-parse", revision)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", revision, err)
	}
	return strings.TrimSpace(string(output))
}
