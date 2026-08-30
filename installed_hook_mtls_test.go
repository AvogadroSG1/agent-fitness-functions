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
)

// TestInstalledEmbeddedHooksPassSelectorThroughWithoutTLSMaterial pins the
// ADR-0007 hook contract on the INSTALLED artifacts end to end: none of the
// three embedded hooks resolves or forwards managed TLS material, and the
// AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR selector reaches the child `client
// validate` untouched — client validate owns managed resolution, which is what
// keeps daemon auto-start working after a daemon death (calm-poc-wgi).
func TestInstalledEmbeddedHooksPassSelectorThroughWithoutTLSMaterial(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "maintenance.auto", "false")
	runGitCommand(t, repo, "config", "maintenance.autoDetach", "false")
	runGitCommand(t, repo, "config", "gc.auto", "0")
	runGitCommand(t, repo, "config", "gc.autoDetach", "false")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "test")
	if err := client.RunInstallHooks([]string{repo}, io.Discard, io.Discard); err != nil {
		t.Fatalf("RunInstallHooks: %v", err)
	}
	certRoot := filepath.Join(t.TempDir(), "managed-certs")

	record := filepath.Join(t.TempDir(), "calls")
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "agent-fitness-functions")
	stubContent := "#!/usr/bin/env bash\nif [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then printf 'resolver-invoked\\n' >>\"$AGENT_FITNESS_FUNCTIONS_LOG\"; exit 9; fi\nprintf 'selector=%s|%s\\n' \"${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}\" \"$*\" >>\"$AGENT_FITNESS_FUNCTIONS_LOG\"\nprintf '{\"status\":\"pass\"}\\n'\n"
	if err := os.WriteFile(stub, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	env := append(os.Environ(),
		"AGENT_FITNESS_FUNCTIONS_BIN="+stub,
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certRoot,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"AGENT_FITNESS_FUNCTIONS_LOG="+record,
	)

	source := filepath.Join(repo, "sample.go")
	if err := os.WriteFile(source, []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runGitCommand(t, repo, "add", "sample.go")
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "pre-commit"), env, nil)

	payload := []byte(`{"tool_input":{"file_path":"sample.go","content":"package sample\n"}}`)
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-tool-use"), env, payload)

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

	assertSelectorPassthrough(t, record, certRoot, 3)
}

// assertSelectorPassthrough checks the recorded child invocations for the
// ADR-0007 contract: no resolver call, no client TLS flags, and the selector
// visible to every child exactly as the hook received it.
func assertSelectorPassthrough(t *testing.T, record, certRoot string, wantCalls int) {
	t.Helper()
	calls, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read hook calls: %v", err)
	}
	if bytes.Contains(calls, []byte("resolver-invoked")) {
		t.Fatalf("an installed hook still invokes client resolve-dev-cert-version; calls:\n%s", calls)
	}
	if bytes.Contains(calls, []byte("--client-cert")) {
		t.Fatalf("an installed hook still forwards managed TLS flags; calls:\n%s", calls)
	}
	if count := bytes.Count(calls, []byte("selector="+certRoot+"|")); count < wantCalls {
		t.Fatalf("selector passthrough count = %d, want %d installed hook kinds; calls:\n%s", count, wantCalls, calls)
	}
}

// TestInstalledSidecarAndAgentHooksPassSelectorThrough pins the same ADR-0007
// contract for the append-mode sidecar copies of the hooks.
func TestInstalledSidecarAndAgentHooksPassSelectorThrough(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HOOK_APPEND", "1")
	repo := t.TempDir()
	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "maintenance.auto", "false")
	runGitCommand(t, repo, "config", "maintenance.autoDetach", "false")
	runGitCommand(t, repo, "config", "gc.auto", "0")
	runGitCommand(t, repo, "config", "gc.autoDetach", "false")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "test")
	for _, name := range []string{"pre-commit", "pre-push"} {
		path := filepath.Join(repo, ".git", "hooks", name)
		if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write existing %s: %v", name, err)
		}
	}
	if err := client.RunInstallHooks([]string{repo}, io.Discard, io.Discard); err != nil {
		t.Fatalf("RunInstallHooks append mode: %v", err)
	}
	certRoot := filepath.Join(t.TempDir(), "managed-certs")

	record := filepath.Join(t.TempDir(), "calls")
	stub := filepath.Join(t.TempDir(), "agent-fitness-functions")
	stubContent := "#!/usr/bin/env bash\nif [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then printf 'resolver-invoked\\n' >>\"$AGENT_FITNESS_FUNCTIONS_LOG\"; exit 9; fi\nprintf 'selector=%s|%s\\n' \"${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}\" \"$*\" >>\"$AGENT_FITNESS_FUNCTIONS_LOG\"\nprintf '{\"status\":\"pass\"}\\n'\n"
	if err := os.WriteFile(stub, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	env := append(os.Environ(),
		"AGENT_FITNESS_FUNCTIONS_BIN="+stub,
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certRoot,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"AGENT_FITNESS_FUNCTIONS_LOG="+record,
	)
	source := filepath.Join(repo, "sample.go")
	if err := os.WriteFile(source, []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runGitCommand(t, repo, "add", "sample.go")
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-commit"), env, nil)
	payload := []byte(`{"tool_input":{"file_path":"sample.go","content":"package sample\n"}}`)
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-tool-use"), env, payload)
	runGitCommand(t, repo, "commit", "--no-verify", "-m", "base")
	base := gitRevision(t, repo, "HEAD")
	if err := os.WriteFile(source, []byte("package sample\n\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatalf("update source: %v", err)
	}
	runGitCommand(t, repo, "add", "sample.go")
	runGitCommand(t, repo, "commit", "--no-verify", "-m", "head")
	head := gitRevision(t, repo, "HEAD")
	pushInput := []byte("refs/heads/main " + head + " refs/heads/main " + base + "\n")
	runInstalledHook(t, repo, filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-push"), env, pushInput)

	assertSelectorPassthrough(t, record, certRoot, 3)
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
