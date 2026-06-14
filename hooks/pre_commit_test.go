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

	"github.com/poconnor/calm-poc/internal/fitness"
)

func TestPreCommitBlocksStagedViolations(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "bad.go"), "package sample\n")
	writeFile(t, filepath.Join(repo, "warn.py"), "print('ok')\n")
	runGit(t, repo, "add", "bad.go")
	runGit(t, repo, "add", "warn.py")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
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
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want block; output=%s", output)
	}
	if strings.Count(string(output), "calm_check:") != 2 {
		t.Fatalf("output = %s, want two calm_check YAML blocks (one block, one advisory)", output)
	}
	if !strings.Contains(string(output), "blocking") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want both blocking and advisory mode labels", output)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calm log: %v", err)
	}
	if strings.Count(string(logContent), "client validate --file") != 2 {
		t.Fatalf("calm log = %s, want two staged file checks", logContent)
	}
}

func TestPreCommitAllowsAdvisoryStagedViolations(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "warn.py"), "print('ok')\n")
	runGit(t, repo, "add", "warn.py")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"advisory","violations":[{"message":"warning only"}]}\n'
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calm log: %v", err)
	}
	for _, want := range []string{"client validate", "--file warn.py", "--staged", "--language python"} {
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
	calmBridge := buildFitnessBin(t)
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode check request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{
				Status:     fitness.StatusBlock,
				Violations: []fitness.Violation{{Message: "daemon validated staged violation"}},
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
		"STACK_FITNESS_FUNCTIONS_BIN="+calmBridge,
		"STACK_FITNESS_FUNCTIONS_ADDR="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit succeeded, want block; output=%s", output)
	}
	if received.ProposedContent != "package staged\n" {
		t.Fatalf("proposed content = %q, want staged index content", received.ProposedContent)
	}
	if !strings.Contains(string(output), "calm_check:") {
		t.Fatalf("output = %s, want YAML calm_check block", output)
	}
}

func TestPreCommitForwardsAddressToRunningDaemon(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, ".calm", "config.json"), `{"enforcement-mode":"advisory","fitness-functions":{"cyclomatic-complexity":true}}`)
	writeFile(t, filepath.Join(repo, "warn.go"), "package sample\n")
	runGit(t, repo, "add", ".calm/config.json")
	runGit(t, repo, "add", "warn.go")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
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
		"STACK_FITNESS_FUNCTIONS_ADDR=http://127.0.0.1:9999",
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
}

func TestPreCommitRemoteModeUsesBasenameRepoAndContentFile(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "remote.go"), "package staged\n")
	runGit(t, repo, "add", "remote.go")
	writeFile(t, filepath.Join(repo, "remote.go"), "package worktree\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --content-file)
      shift
      printf 'content=%s\n' "$(cat "$1")" >> "$STACK_FITNESS_FUNCTIONS_LOG"
      ;;
  esac
  shift
done
printf '{"status":"pass"}\n'
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_ADDR=https://calm-governance.example:7890",
		"STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1",
		"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=/certs/client.crt",
		"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=/certs/client.key",
		"STACK_FITNESS_FUNCTIONS_CLIENT_CA=/certs/ca.crt",
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit remote mode failed: %v\n%s", err, output)
	}
	logContent := readFile(t, logPath)
	for _, want := range []string{
		"--addr https://calm-governance.example:7890",
		"--repo " + filepath.Base(repo),
		"--content-file ",
		"--client-cert /certs/client.crt",
		"--client-key /certs/client.key",
		"--client-ca /certs/ca.crt",
		"content=package staged",
	} {
		if !strings.Contains(logContent, want) {
			t.Fatalf("calm log = %s, want %s", logContent, want)
		}
	}
	if strings.Contains(logContent, "--staged") || strings.Contains(logContent, "--repo "+repo) {
		t.Fatalf("calm log = %s, want remote mode to avoid --staged and filesystem repo path", logContent)
	}
}

func TestPreCommitRemoteModeUsesCALMRepoNameOverride(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "remote.go"), "package staged\n")
	runGit(t, repo, "add", "remote.go")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"pass"}\n'
`)
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_ADDR=https://calm-governance.example:7890",
		"STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1",
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=graft",
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit remote override failed: %v\n%s", err, output)
	}
	logContent := readFile(t, logPath)
	if !strings.Contains(logContent, "--repo graft") {
		t.Fatalf("calm log = %s, want STACK_FITNESS_FUNCTIONS_REPO_NAME override", logContent)
	}
}

func TestPreCommitRejectsRemoteHTTPBridge(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")
	script := hookScriptPath(t)

	command := exec.Command("bash", script)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_ADDR=http://calm-governance.example:7890",
		"STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1",
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit accepted remote HTTP bridge, want rejection; output=%s", output)
	}
	if !strings.Contains(string(output), "remote STACK_FITNESS_FUNCTIONS_ADDR must use https") {
		t.Fatalf("output = %s, want HTTPS diagnostic", output)
	}
}

func TestPreCommitBlocksUnknownStatus(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "weird.go"), "package sample\n")
	runGit(t, repo, "add", "weird.go")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
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
	command.Env = append(os.Environ(), "STACK_FITNESS_FUNCTIONS_ADDR=https://example.com")
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
	command.Env = append(os.Environ(), "STACK_FITNESS_FUNCTIONS_ADDR=http://127.0.0.1:80@evil.example")
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

func fakeFitnessBin(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack-fitness-functions")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake stack-fitness-functions: %v", err)
	}
	return dir
}

func buildFitnessBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack-fitness-functions")
	command := exec.Command("go", "build", "-o", path, "../cmd/stack-fitness-functions")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build stack-fitness-functions: %v\n%s", err, output)
	}
	return path
}
