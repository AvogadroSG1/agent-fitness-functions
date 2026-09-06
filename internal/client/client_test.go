package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

func TestPackageImportsFitnessContractNotServerInternals(t *testing.T) {
	command := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", "./internal/client")
	command.Dir = projectRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list internal/client: %v\n%s", err, output)
	}
	imports := string(output)
	if !strings.Contains(imports, "github.com/AvogadroSG1/agent-fitness-functions/internal/fitness") {
		t.Fatalf("imports = %s, want internal/fitness", imports)
	}
	if strings.Contains(imports, "github.com/AvogadroSG1/agent-fitness-functions/internal/server") {
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
	err := RunCheck([]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"}, &stdout, &http.Client{Timeout: time.Second}, func(DaemonStartConfig) error { return nil })
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

	for _, hook := range []string{"pre-commit", "pre-push", "agent-fitness-functions-git-guard", "agent-fitness-functions-pre-tool-use"} {
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
	if !strings.Contains(string(content), "agent-fitness-functions") {
		t.Fatalf("pre-commit does not invoke agent-fitness-functions:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "format-violations.py")); err != nil {
		t.Fatalf("formatter not installed: %v", err)
	}

	settingsPath := filepath.Join(repo, ".claude", "settings.json")
	settingsContent, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(settingsContent), "agent-fitness-functions-git-guard") {
		t.Fatalf("settings missing git guard entry:\n%s", settingsContent)
	}
	assertBothPreToolUseEntries(t, settingsContent)
}

// assertBothPreToolUseEntries verifies settings.json wires both PreToolUse entries:
// the Bash git-guard and the Edit|Write agent content-validation hook.
func assertBothPreToolUseEntries(t *testing.T, settingsContent []byte) {
	t.Helper()
	matchers := preToolUseMatchers(t, settingsContent)
	for _, want := range []string{"Bash", "Edit|Write"} {
		if !matchers[want] {
			t.Fatalf("settings missing PreToolUse matcher %q:\n%s", want, settingsContent)
		}
	}
	if !strings.Contains(string(settingsContent), "agent-fitness-functions-pre-tool-use") {
		t.Fatalf("settings missing agent hook command:\n%s", settingsContent)
	}
}

func preToolUseMatchers(t *testing.T, settingsContent []byte) map[string]bool {
	t.Helper()
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settingsContent, &settings); err != nil {
		t.Fatalf("parse settings: %v\n%s", err, settingsContent)
	}
	matchers := map[string]bool{}
	for _, entry := range settings.Hooks.PreToolUse {
		matchers[entry.Matcher] = true
	}
	return matchers
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
	if count := strings.Count(string(settingsContent), "agent-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("agent-fitness-functions-git-guard appears %d times, want 1:\n%s", count, settingsContent)
	}
	if count := preToolUseEntryCount(t, settingsContent); count != 2 {
		t.Fatalf("PreToolUse has %d entries, want 2 (git-guard + agent hook):\n%s", count, settingsContent)
	}
	assertBothPreToolUseEntries(t, settingsContent)
}

func preToolUseEntryCount(t *testing.T, settingsContent []byte) int {
	t.Helper()
	var settings struct {
		Hooks struct {
			PreToolUse []json.RawMessage `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settingsContent, &settings); err != nil {
		t.Fatalf("parse settings: %v\n%s", err, settingsContent)
	}
	return len(settings.Hooks.PreToolUse)
}

func TestRunInstallHooksAddsAgentHookToGitGuardOnlySettings(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	// Seed a settings.json that already wires only the Bash git-guard entry, as an
	// install predating the agent hook would have left it.
	existing := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/x/.git/hooks/agent-fitness-functions-git-guard"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(existing), 0o644); err != nil {
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
	if count := preToolUseEntryCount(t, content); count != 2 {
		t.Fatalf("PreToolUse has %d entries, want 2:\n%s", count, content)
	}
	assertBothPreToolUseEntries(t, content)
}

func TestRunInstallHooksErrorsOnMalformedSettings(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := RunInstallHooks([]string{repo}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("RunInstallHooks succeeded on malformed settings, want error; stdout=%s", stdout.String())
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("error = %v, want mention of settings.json", err)
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
	if !strings.Contains(stderr.String(), "AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1") {
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
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HOOK_APPEND", "1")
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	sidecar := filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-commit")
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
	if !strings.Contains(string(existing), "# agent-fitness-functions pre-commit hook (sidecar)") {
		t.Fatalf("existing hook missing sidecar block:\n%s", existing)
	}
}

func TestRunInstallHooksUpgradesLegacyCalmHook(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

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
	if !strings.Contains(string(content), "# agent-fitness-functions pre-commit hook") {
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
	if count := strings.Count(string(content), "agent-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("agent-fitness-functions-git-guard appears %d times, want 1:\n%s", count, content)
	}
}

func TestRunCheckAutoStartsWithHTTPSLoopbackAddr(t *testing.T) {
	// Isolate dev-cert discovery in a temp dir, and target a guaranteed-dead loopback
	// port so the health probe is a deterministic connection-refused (not a TLS error
	// from any foreign daemon that may occupy the default 7890). A refused probe is the
	// zero-config path that should invoke auto-start with the https loopback addr.
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", t.TempDir())
	deadAddr := "https://" + reservedDeadLoopbackAddr(t)
	var captured string
	starter := func(cfg DaemonStartConfig) error {
		captured = cfg.Addr
		return errors.New("stop after capture")
	}

	err := RunCheck(
		[]string{"--addr", deadAddr, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		io.Discard, &http.Client{Timeout: time.Second}, starter,
	)
	if err == nil {
		t.Fatalf("RunCheck succeeded, want starter error")
	}
	if captured != deadAddr {
		t.Fatalf("daemon addr = %q, want %q", captured, deadAddr)
	}
}

// reservedDeadLoopbackAddr binds an ephemeral loopback port, then closes it so nothing
// listens there — a deterministic connection-refused target.
func reservedDeadLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved listener: %v", err)
	}
	return addr
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
		{name: "installAgentHook", maxCC: 9},
		{name: "applyClaudeHook", maxCC: 9},
		{name: "upsertClaudeHook", maxCC: 9},
	} {
		function := findClientFunction(t, result, want.name)
		if function.CyclomaticComplexity > want.maxCC {
			t.Fatalf("%s cyclomatic complexity = %d, want <= %d", want.name, function.CyclomaticComplexity, want.maxCC)
		}
	}
}

// TestEmbeddedHookAssetsMatchAuthoritativeHooks guards the embedded install-time
// copies against drift from the authoritative scripts under hooks/. hooks/*.sh stays
// the source of truth (this repo's own .claude/settings.json points at it); the
// embedded twin is what `client install-hooks` writes into a governed repo. If they
// diverge, re-copy the changed file into internal/client/hookassets/.
func TestEmbeddedHookAssetsMatchAuthoritativeHooks(t *testing.T) {
	hooksDir := filepath.Join(projectRoot(t), "hooks")
	for _, name := range []string{
		"pre-commit.sh",
		"pre-push.sh",
		"git-guard.sh",
		"pre-tool-use.sh",
		"format-violations.py",
		"opencode-plugin.js",
	} {
		authoritative, err := os.ReadFile(filepath.Join(hooksDir, name))
		if err != nil {
			t.Fatalf("read hooks/%s: %v", name, err)
		}
		embedded, err := embeddedHooks.ReadFile("hookassets/" + name)
		if err != nil {
			t.Fatalf("read embedded hookassets/%s: %v", name, err)
		}
		if !bytes.Equal(authoritative, embedded) {
			t.Fatalf("hookassets/%s has drifted from hooks/%s; re-copy hooks/%s into internal/client/hookassets/", name, name, name)
		}
	}
}

func runGitClientTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	if len(args) > 0 && args[0] == "init" {
		// Must precede the init itself: git init is the command most likely to
		// wake a machine-level trace2 consumer that writes into .git/.
		t.Setenv("GIT_TRACE2_EVENT", "0")
	}
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	if len(args) > 0 && args[0] == "init" {
		disableGitBackgroundMaintenance(t, repo)
	}
}

// disableGitBackgroundMaintenance stops git from detaching auto-gc and
// maintenance into a repository t.TempDir() is about to remove. A background
// git process writing into .git races that removal and fails the test with
// "TempDir RemoveAll cleanup: ... .git: directory not empty" long after its
// assertions passed — the same settings hooks/pre_commit_test.go pins.
func disableGitBackgroundMaintenance(t *testing.T, repo string) {
	t.Helper()
	// A developer-machine trace2.eventtarget (e.g. the git-ai daemon) makes every
	// git command feed an external process that then writes .git/ai/ into the
	// repo asynchronously — racing t.TempDir() cleanup. The env var overrides
	// the config for every git this test spawns.
	t.Setenv("GIT_TRACE2_EVENT", "0")
	for _, setting := range [][2]string{
		{"maintenance.auto", "false"},
		{"maintenance.autoDetach", "false"},
		{"gc.auto", "0"},
		{"gc.autoDetach", "false"},
	} {
		command := exec.Command("git", "-C", repo, "config", setting[0], setting[1])
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v\n%s", setting[0], err, output)
		}
	}
}

func useDeterministicGitClientTest(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll(fake git hooks): %v", err)
	}
	binDir := t.TempDir()
	fakeGit := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "-C" ]]; then shift 2; fi
if [[ "${1:-}" == "rev-parse" && "${2:-}" == "--show-toplevel" ]]; then
  printf '%s\n' "$FAKE_GIT_REPO"
  exit 0
fi
if [[ "${1:-}" == "rev-parse" && "${2:-}" == "--git-path" && "${3:-}" == hooks/* ]]; then
  printf '.git/%s\n' "$3"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(fakeGit), 0o755); err != nil {
		t.Fatalf("WriteFile(fake git): %v", err)
	}
	t.Setenv("FAKE_GIT_REPO", repo)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
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

func TestRunCheckTimeoutFlagAndEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Run("succeeds when timeout flag is longer than latency", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunCheck(
			[]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go", "--timeout", "500ms"},
			&stdout, &http.Client{Timeout: 5 * time.Second}, func(DaemonStartConfig) error { return nil },
		)
		if err != nil {
			t.Fatalf("RunCheck returned error: %v", err)
		}
		if !strings.Contains(stdout.String(), `"status":"pass"`) {
			t.Fatalf("stdout = %q, want pass JSON", stdout.String())
		}
	})

	t.Run("succeeds when timeout env var is longer than latency", func(t *testing.T) {
		t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT", "500ms")
		var stdout bytes.Buffer
		err := RunCheck(
			[]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
			&stdout, &http.Client{Timeout: 5 * time.Second}, func(DaemonStartConfig) error { return nil },
		)
		if err != nil {
			t.Fatalf("RunCheck returned error: %v", err)
		}
		if !strings.Contains(stdout.String(), `"status":"pass"`) {
			t.Fatalf("stdout = %q, want pass JSON", stdout.String())
		}
	})

	t.Run("fails with check_timeout when timeout flag is shorter than latency", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunCheck(
			[]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go", "--timeout", "10ms"},
			&stdout, &http.Client{Timeout: 5 * time.Second}, func(DaemonStartConfig) error { return nil },
		)
		if !IsInfraError(err) {
			t.Fatalf("RunCheck error = %v, want infra error", err)
		}
		var report infraErrorReport
		if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
			t.Fatalf("stdout not JSON: %v\n%s", jsonErr, stdout.String())
		}
		if report.ErrorKind != errorKindTimeout {
			t.Fatalf("kind = %q, want %s", report.ErrorKind, errorKindTimeout)
		}
	})
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
