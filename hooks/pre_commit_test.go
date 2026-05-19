package hooks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/poconnor/calm-poc/internal/bridge"
)

func TestPreCommitBlocksStagedViolations(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "bad.go"), "package sample\n")
	writeFile(t, filepath.Join(repo, "warn.py"), "print('ok')\n")
	runGit(t, repo, "add", "bad.go")
	runGit(t, repo, "add", "warn.py")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALM_BRIDGE_LOG"
if [[ "$*" == *"bad.go"* ]]; then
printf '{"status":"block","violations":[{"message":"too complex"}]}\n'
else
printf '{"status":"advisory","violations":[{"message":"warning only"}]}\n'
fi
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CALM_BRIDGE_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want block; output=%s", output)
	}
	if !strings.Contains(string(output), "CALM violation in bad.go") ||
		!strings.Contains(string(output), "too complex") ||
		!strings.Contains(string(output), "CALM advisory for warn.py") ||
		!strings.Contains(string(output), "warning only") {
		t.Fatalf("output = %s, want block and advisory messages", output)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calm log: %v", err)
	}
	if strings.Count(string(logContent), "check --file") != 2 {
		t.Fatalf("calm log = %s, want two staged file checks", logContent)
	}
}

func TestPreCommitAllowsAdvisoryStagedViolations(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "warn.py"), "print('ok')\n")
	runGit(t, repo, "add", "warn.py")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALM_BRIDGE_LOG"
printf '{"status":"advisory","violations":[{"message":"warning only"}]}\n'
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CALM_BRIDGE_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "CALM advisory for warn.py") || !strings.Contains(string(output), "warning only") {
		t.Fatalf("output = %s, want advisory message", output)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calm log: %v", err)
	}
	for _, want := range []string{"check", "--file warn.py", "--staged", "--language python"} {
		if !strings.Contains(string(logContent), want) {
			t.Fatalf("calm log = %s, want %s", logContent, want)
		}
	}
}

func TestPreCommitBlocksStagedViolationThroughRunningDaemon(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "bad.go"), "package staged\n")
	runGit(t, repo, "add", "bad.go")
	writeFile(t, filepath.Join(repo, "bad.go"), "package worktree\n")
	calmBridge := buildCalmBridge(t)
	var received bridge.CheckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode check request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{
				Status:     bridge.StatusBlock,
				Violations: []bridge.Violation{{Message: "daemon validated staged violation"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"CALM_BRIDGE_BIN="+calmBridge,
		"CALM_BRIDGE_ADDR="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want block; output=%s", output)
	}
	if received.ProposedContent != "package staged\n" {
		t.Fatalf("proposed content = %q, want staged index content", received.ProposedContent)
	}
	if !strings.Contains(string(output), "daemon validated staged violation") {
		t.Fatalf("output = %s, want daemon violation message", output)
	}
}

func TestPreCommitForwardsAddressToRunningDaemon(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, ".calm", "config.json"), `{"enforcement-mode":"advisory","fitness-functions":{"cyclomatic-complexity":true}}`)
	writeFile(t, filepath.Join(repo, "warn.go"), "package sample\n")
	runGit(t, repo, "add", ".calm/config.json")
	runGit(t, repo, "add", "warn.go")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALM_BRIDGE_LOG"
if [[ "$*" != *"--addr http://127.0.0.1:9999"* ]]; then
  echo "missing addr" >&2
  exit 1
fi
printf '{"status":"advisory","violations":[{"message":"configured advisory"}]}\n'
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CALM_BRIDGE_ADDR=http://127.0.0.1:9999",
		"CALM_BRIDGE_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "configured advisory") {
		t.Fatalf("output = %s, want advisory from running daemon path", output)
	}
}

func TestPreCommitBlocksUnknownStatus(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "weird.go"), "package sample\n")
	runGit(t, repo, "add", "weird.go")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '{"status":"mystery","violations":[]}\n'
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want unknown status block; output=%s", output)
	}
	if !strings.Contains(string(output), "unknown status") {
		t.Fatalf("output = %s, want unknown status diagnostic", output)
	}
}

func TestPreCommitRejectsRemoteBridgeWithoutOptIn(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(), "CALM_BRIDGE_ADDR=https://example.com")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want remote bridge rejection; output=%s", output)
	}
	if !strings.Contains(string(output), "must be loopback") {
		t.Fatalf("output = %s, want loopback diagnostic", output)
	}
}

func TestPreCommitRejectsLoopbackUserinfoBypass(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(), "CALM_BRIDGE_ADDR=http://127.0.0.1:80@evil.example")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want userinfo bypass rejection; output=%s", output)
	}
	if !strings.Contains(string(output), "must be loopback") {
		t.Fatalf("output = %s, want loopback diagnostic", output)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	return repo
}

func hookScriptPath(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	return filepath.Join(cwd, "pre-commit.sh")
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fakeCalmBridge(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "calm-bridge")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake calm-bridge: %v", err)
	}
	return dir
}

func buildCalmBridge(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "calm-bridge")
	command := exec.Command("go", "build", "-o", path, "../cmd/calm-bridge")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build calm-bridge: %v\n%s", err, output)
	}
	return path
}
