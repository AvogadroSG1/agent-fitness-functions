package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/fitness"
)

func TestPackageImportsFitnessContractNotServerInternals(t *testing.T) {
	command := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", "./internal/client")
	command.Dir = projectRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list internal/client: %v\n%s", err, output)
	}
	imports := string(output)
	if !strings.Contains(imports, "github.com/poconnor/calm-poc/internal/fitness") {
		t.Fatalf("imports = %s, want internal/fitness", imports)
	}
	if strings.Contains(imports, "github.com/poconnor/calm-poc/internal/server") {
		t.Fatalf("imports = %s, must not include internal/server", imports)
	}
}

func TestRunCheckPostsValidationRequestThroughPublicClientInterface(t *testing.T) {
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := RunCheck([]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"}, &stdout, &http.Client{Timeout: time.Second}, func(string) error { return nil })
	if err != nil {
		t.Fatalf("RunCheck returned error: %v", err)
	}

	if received.File != "x.go" || received.Repo != "/tmp/repo" || received.Language != "go" {
		t.Fatalf("received request = %+v", received)
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

func TestResolveContentReadsRelativeToRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	content, err := resolveContent(repo, "x.go", "", "", false)
	if err != nil {
		t.Fatalf("resolveContent returned error: %v", err)
	}
	if content != "package main\n" {
		t.Fatalf("content = %q, want repo-relative file content", content)
	}
}

func TestRunInstallHooksInstallsEmbeddedHooksIntoFreshRepo(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	for _, hook := range []string{"pre-commit", "pre-push", "stack-fitness-functions-git-guard"} {
		hookPath := filepath.Join(repo, ".git", "hooks", hook)
		info, err := os.Stat(hookPath)
		if err != nil {
			t.Fatalf("stat %s: %v", hookPath, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s mode = %v, want executable", hookPath, info.Mode())
		}
	}

	content, err := os.ReadFile(filepath.Join(repo, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatalf("read pre-commit: %v", err)
	}
	if !strings.Contains(string(content), "stack-fitness-functions") {
		t.Fatalf("pre-commit does not invoke stack-fitness-functions:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "format-violations.py")); err != nil {
		t.Fatalf("formatter not installed: %v", err)
	}

	settingsPath := filepath.Join(repo, ".claude", "settings.json")
	settingsContent, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(settingsContent), "stack-fitness-functions-git-guard") {
		t.Fatalf("settings missing git guard entry:\n%s", settingsContent)
	}
}

func TestRunInstallHooksIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	for range 2 {
		var stdout, stderr bytes.Buffer
		if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
			t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
		}
	}

	settingsContent, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if count := strings.Count(string(settingsContent), "stack-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("stack-fitness-functions-git-guard appears %d times, want 1:\n%s", count, settingsContent)
	}
}

func TestRunInstallHooksRefusesExistingNonCalmHook(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := RunInstallHooks([]string{repo}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("RunInstallHooks succeeded, want refusal; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "STACK_FITNESS_FUNCTIONS_HOOK_APPEND=1") {
		t.Fatalf("stderr = %s, want append option", stderr.String())
	}
	content, err := os.ReadFile(existingHook)
	if err != nil {
		t.Fatalf("read existing hook: %v", err)
	}
	if !strings.Contains(string(content), "echo custom") {
		t.Fatalf("existing hook was replaced:\n%s", content)
	}
}

func TestRunInstallHooksAppendModeInstallsSidecar(t *testing.T) {
	t.Setenv("STACK_FITNESS_FUNCTIONS_HOOK_APPEND", "1")
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	sidecar := filepath.Join(repo, ".git", "hooks", "stack-fitness-functions-pre-commit")
	if info, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar not found at %s: %v", sidecar, err)
	} else if info.Mode()&0o111 == 0 {
		t.Fatalf("sidecar mode = %v, want executable", info.Mode())
	}
	existing, err := os.ReadFile(existingHook)
	if err != nil {
		t.Fatalf("read existing hook: %v", err)
	}
	if !strings.Contains(string(existing), "echo custom") {
		t.Fatalf("existing hook content was replaced:\n%s", existing)
	}
	if !strings.Contains(string(existing), "# stack-fitness-functions pre-commit hook (sidecar)") {
		t.Fatalf("existing hook missing sidecar block:\n%s", existing)
	}
}

func TestRunInstallHooksUpgradesLegacyCalmHook(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	legacy := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("#!/usr/bin/env bash\n# CALM pre-commit hook\necho legacy\n"), 0o755); err != nil {
		t.Fatalf("seed legacy hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatalf("read upgraded hook: %v", err)
	}
	if strings.Contains(string(content), "echo legacy") {
		t.Fatalf("legacy hook was not overwritten (legacy CALM marker not recognized):\n%s", content)
	}
	if !strings.Contains(string(content), "# stack-fitness-functions pre-commit hook") {
		t.Fatalf("upgraded hook missing new marker:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "format-violations.py")); err != nil {
		t.Fatalf("formatter not installed during legacy upgrade: %v", err)
	}
}

func TestRunInstallHooksUpgradesLegacyGitGuardSettings(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	legacySettings := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/legacy/.git/hooks/calm-git-guard"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(legacySettings), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(content), "calm-git-guard") {
		t.Fatalf("legacy calm-git-guard still present after upgrade:\n%s", content)
	}
	if count := strings.Count(string(content), "stack-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("stack-fitness-functions-git-guard appears %d times, want 1:\n%s", count, content)
	}
}

type alwaysErrTransport struct{}

func (alwaysErrTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unreachable")
}

func TestRunCheckDefaultsToHTTPSLoopback(t *testing.T) {
	var captured string
	starter := func(addr string) error {
		captured = addr
		return errors.New("stop after capture")
	}
	client := &http.Client{Transport: alwaysErrTransport{}}

	err := RunCheck(
		[]string{"--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		io.Discard, client, starter,
	)
	if err == nil {
		t.Fatalf("RunCheck succeeded, want starter error")
	}
	if captured != "https://127.0.0.1:7890" {
		t.Fatalf("daemon addr = %q, want https://127.0.0.1:7890", captured)
	}
}

func TestHookInstallerFunctionsStayWithinCyclomaticComplexityBudget(t *testing.T) {
	result, err := analyzer.AnalyzeGoFile(filepath.Join(projectRoot(t), "internal", "client", "client.go"))
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}

	for _, want := range []struct {
		name  string
		maxCC int
	}{
		{name: "installGitHook", maxCC: 9},
		{name: "installGitGuard", maxCC: 9},
		{name: "upsertGitGuard", maxCC: 9},
	} {
		function := findClientFunction(t, result, want.name)
		if function.CyclomaticComplexity > want.maxCC {
			t.Fatalf("%s cyclomatic complexity = %d, want <= %d", want.name, function.CyclomaticComplexity, want.maxCC)
		}
	}
}

func runGitClientTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for current := cwd; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("could not find go.mod from %s", cwd)
		}
	}
}

func findClientFunction(t *testing.T, result analyzer.AnalysisResult, want string) analyzer.FunctionMetric {
	t.Helper()
	for _, function := range result.Functions {
		if function.Name == want {
			return function
		}
	}
	t.Fatalf("function %q not found in %+v", want, result.Functions)
	return analyzer.FunctionMetric{}
}
