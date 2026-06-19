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
	fitnessBin := buildFitnessBin(t)
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
		"STACK_FITNESS_FUNCTIONS_BIN="+fitnessBin,
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
	// The hook forwards mTLS cert flags only when the files exist on disk, so
	// stage real credential files and point the explicit env overrides at them.
	certDir := t.TempDir()
	clientCert := filepath.Join(certDir, "client.crt")
	clientKey := filepath.Join(certDir, "client.key")
	clientCA := filepath.Join(certDir, "ca.crt")
	for _, path := range []string{clientCert, clientKey, clientCA} {
		writeFile(t, path, "x")
	}
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
		"STACK_FITNESS_FUNCTIONS_CLIENT_CERT="+clientCert,
		"STACK_FITNESS_FUNCTIONS_CLIENT_KEY="+clientKey,
		"STACK_FITNESS_FUNCTIONS_CLIENT_CA="+clientCA,
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
		"--client-cert " + clientCert,
		"--client-key " + clientKey,
		"--client-ca " + clientCA,
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

func TestPreCommitForwardsDiscoveredMTLSCerts(t *testing.T) {
	repo := initGitRepo(t)
	// git rev-parse --show-toplevel resolves symlinks (e.g. macOS /var -> /private/var),
	// so resolve here too to match the cert paths the hook forwards.
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")

	certDir := filepath.Join(repo, "certs")
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		writeFile(t, filepath.Join(certDir, name), "x")
	}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	for _, want := range []string{
		"--addr https://127.0.0.1:7890",
		"--client-cert " + filepath.Join(certDir, "client.crt"),
		"--client-key " + filepath.Join(certDir, "client.key"),
		"--client-ca " + filepath.Join(certDir, "ca.crt"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in invocations:\n%s", want, got)
		}
	}
}

func TestPreCommitOmitsCertFlagsWhenAbsent(t *testing.T) {
	repo := initGitRepo(t)
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	if !strings.Contains(got, "--addr https://127.0.0.1:7890") {
		t.Errorf("missing https default addr:\n%s", got)
	}
	if strings.Contains(got, "--client-cert") {
		t.Errorf("expected no --client-cert when certs absent:\n%s", got)
	}
}

// calm-poc-cyd: local-mode hooks must send the logical repo name (worktree-safe)
// as --repo while reading staged content from the actual working tree (--git-dir).

func initNamedWorktree(t *testing.T) (mainRepo, worktree string) {
	t.Helper()
	parent := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolved
	}
	mainRepo = filepath.Join(parent, "relocate")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	runGit(t, mainRepo, "init")
	runGit(t, mainRepo, "config", "user.email", "t@example.com")
	runGit(t, mainRepo, "config", "user.name", "t")
	writeFile(t, filepath.Join(mainRepo, "README.md"), "init\n")
	runGit(t, mainRepo, "add", "README.md")
	runGit(t, mainRepo, "commit", "-m", "init")
	worktree = filepath.Join(parent, "feature+relocate-stats")
	runGit(t, mainRepo, "worktree", "add", "-b", "feature/relocate-stats", worktree)
	return mainRepo, worktree
}

func TestPreCommitLocalModeSendsLogicalRepoNameAndGitDir(t *testing.T) {
	_, worktree := initNamedWorktree(t)
	writeFile(t, filepath.Join(worktree, "bad.go"), "package sample\n")
	runGit(t, worktree, "add", "bad.go")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"pass"}\n'
`)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = worktree
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	for _, want := range []string{"--repo relocate", "--git-dir " + worktree, "--staged"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in invocation:\n%s", want, got)
		}
	}
	if strings.Contains(got, "--repo "+worktree) || strings.Contains(got, "--repo feature+relocate-stats") {
		t.Errorf("logical repo name leaked the worktree path/dir name:\n%s", got)
	}
}

func TestPreCommitLocalModeReadsStagedContentFromWorktree(t *testing.T) {
	_, worktree := initNamedWorktree(t)
	writeFile(t, filepath.Join(worktree, "bad.go"), "package staged\n")
	runGit(t, worktree, "add", "bad.go")
	// Dirty the worktree so a worktree read would differ from the staged index.
	writeFile(t, filepath.Join(worktree, "bad.go"), "package worktree\n")
	fitnessBin := buildFitnessBin(t)
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode check request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = worktree
	command.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+fitnessBin,
		"STACK_FITNESS_FUNCTIONS_ADDR="+server.URL,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
	}
	if received.Repo != "relocate" {
		t.Fatalf("received.Repo = %q, want logical name \"relocate\"", received.Repo)
	}
	if received.ProposedContent != "package staged\n" {
		t.Fatalf("received.ProposedContent = %q, want staged index content from worktree", received.ProposedContent)
	}
}

func TestPreCommitRepoNameOverrideStillWinsLocally(t *testing.T) {
	_, worktree := initNamedWorktree(t)
	writeFile(t, filepath.Join(worktree, "bad.go"), "package sample\n")
	runGit(t, worktree, "add", "bad.go")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"pass"}\n'
`)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = worktree
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=graft",
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
	}
	got := readFile(t, logPath)
	if !strings.Contains(got, "--repo graft") {
		t.Errorf("missing STACK_FITNESS_FUNCTIONS_REPO_NAME override --repo graft:\n%s", got)
	}
	if !strings.Contains(got, "--git-dir "+worktree) {
		t.Errorf("missing --git-dir even with name override:\n%s", got)
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
