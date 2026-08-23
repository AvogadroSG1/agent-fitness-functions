package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// calm-poc-phk.7: given hooks installed by the immediate predecessor
// generation, when install-hooks runs, then every predecessor artifact is
// recognized, replaced with the agent-fitness-functions generation, stale
// predecessor files are cleaned up, and the upgrade is idempotent.

func stackGeneration(part string) string {
	// Assembled from fragments so tracked sources never trip the
	// rename-phase predecessor sweeps.
	return "stack-fitness" + "-functions" + part
}

func TestRunInstallHooksUpgradesStackGenerationManagedHook(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	seed := "#!/usr/bin/env bash\n# " + stackGeneration("") + " pre-commit hook\necho stack generation\n"
	if err := os.WriteFile(hook, []byte(seed), 0o755); err != nil {
		t.Fatalf("seed predecessor hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read upgraded hook: %v", err)
	}
	if strings.Contains(string(content), "echo stack generation") {
		t.Fatalf("predecessor hook was not overwritten (stack marker not recognized):\n%s", content)
	}
	if !strings.Contains(string(content), "# agent-fitness-functions pre-commit hook") {
		t.Fatalf("upgraded hook missing current marker:\n%s", content)
	}
}

func TestRunInstallHooksUpgradesStackGenerationGitGuardAndAgentHook(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	staleGuard := filepath.Join(hooksDir, stackGeneration("-git-guard"))
	staleAgent := filepath.Join(hooksDir, stackGeneration("-pre-tool-use"))
	for _, stale := range []string{staleGuard, staleAgent} {
		if err := os.WriteFile(stale, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("seed stale %s: %v", stale, err)
		}
	}
	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	settings := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + staleGuard + `"}]},{"matcher":"Edit|Write","hooks":[{"type":"command","command":"` + staleAgent + `"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644); err != nil {
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
	if strings.Contains(string(content), stackGeneration("-")) {
		t.Fatalf("settings still reference the predecessor generation:\n%s", content)
	}
	if count := strings.Count(string(content), "agent-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("agent-fitness-functions-git-guard appears %d times, want 1:\n%s", count, content)
	}
	if count := strings.Count(string(content), "agent-fitness-functions-pre-tool-use"); count != 1 {
		t.Fatalf("agent-fitness-functions-pre-tool-use appears %d times, want 1:\n%s", count, content)
	}
	for _, stale := range []string{staleGuard, staleAgent} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("stale predecessor artifact survived upgrade: %s", stale)
		}
	}
}

func TestRunInstallHooksUpgradesStackGenerationSidecarChain(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	staleSidecar := filepath.Join(hooksDir, stackGeneration("-pre-commit"))
	if err := os.WriteFile(staleSidecar, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("seed stale sidecar: %v", err)
	}
	userHook := filepath.Join(hooksDir, "pre-commit")
	userContent := "#!/usr/bin/env bash\necho user-owned hook\n"
	chained := userContent + "\n# " + stackGeneration("") + " pre-commit hook (sidecar)\n\"" + staleSidecar + "\"\n"
	if err := os.WriteFile(userHook, []byte(chained), 0o755); err != nil {
		t.Fatalf("seed chained hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(userHook)
	if err != nil {
		t.Fatalf("read chained hook: %v", err)
	}
	if !strings.Contains(string(content), "echo user-owned hook") {
		t.Fatalf("user-owned hook content was destroyed during upgrade:\n%s", content)
	}
	if strings.Contains(string(content), stackGeneration("")) {
		t.Fatalf("chained hook still references the predecessor generation:\n%s", content)
	}
	if !strings.Contains(string(content), "agent-fitness-functions pre-commit hook (sidecar)") {
		t.Fatalf("chained hook missing upgraded sidecar marker:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(hooksDir, "agent-fitness-functions-pre-commit")); err != nil {
		t.Errorf("upgraded sidecar script missing: %v", err)
	}
	if _, err := os.Stat(staleSidecar); !os.IsNotExist(err) {
		t.Errorf("stale predecessor sidecar survived upgrade: %s", staleSidecar)
	}

	before, err := snapshotHooksDir(hooksDir)
	if err != nil {
		t.Fatalf("snapshot hooks dir: %v", err)
	}
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("second RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}
	after, err := snapshotHooksDir(hooksDir)
	if err != nil {
		t.Fatalf("snapshot hooks dir after rerun: %v", err)
	}
	if before != after {
		t.Fatalf("upgrade is not idempotent:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// calm-poc-phk.7 / ADR-0006 regression fixture (a): simulate bd init's
// `git config core.hooksPath` redirect without requiring bd itself. The
// existing gitHookPath helper resolves hook targets via
// `git rev-parse --git-path hooks/<name>`, which already follows
// core.hooksPath (including relative values) because it delegates to git
// itself rather than re-deriving git's resolution rules. install-hooks must
// therefore compose into the redirected directory and never fall back to
// .git/hooks.
func TestRunInstallHooksComposesInRedirectedCoreHooksPath(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	runGitClientTest(t, repo, "config", "core.hooksPath", "custom-hooks")

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	redirected := filepath.Join(repo, "custom-hooks", "pre-commit")
	if _, err := os.Stat(redirected); err != nil {
		t.Fatalf("hook not composed into redirected core.hooksPath directory: %v", err)
	}
	unredirected := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if _, err := os.Stat(unredirected); !os.IsNotExist(err) {
		t.Errorf("hook was also written to .git/hooks despite core.hooksPath redirect: %s", unredirected)
	}
}

// calm-poc-phk.7 / ADR-0006 regression fixture (b): known-owner detection
// matches the version-independent substring "BEGIN BEADS INTEGRATION" so a
// Beads version bump the product has never seen still composes
// automatically (no escape-hatch env var required), and a rerun neither
// duplicates nor destroys the Beads-owned content.
func TestRunInstallHooksComposesWithBeadsRegardlessOfVersionStamp(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	beadsBlock := "#!/usr/bin/env bash\n" +
		"# --- BEGIN BEADS INTEGRATION v9.9.9 ---\n" +
		"echo beads stuff\n" +
		"# --- END BEADS INTEGRATION v9.9.9 ---\n"
	if err := os.WriteFile(hook, []byte(beadsBlock), 0o755); err != nil {
		t.Fatalf("seed beads hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error (want automatic known-owner composition): %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read composed hook: %v", err)
	}
	if !strings.Contains(string(content), "BEGIN BEADS INTEGRATION v9.9.9") ||
		!strings.Contains(string(content), "END BEADS INTEGRATION v9.9.9") ||
		!strings.Contains(string(content), "echo beads stuff") {
		t.Fatalf("Beads-owned content was destroyed during composition:\n%s", content)
	}
	if !strings.Contains(string(content), "agent-fitness-functions pre-commit hook (sidecar)") {
		t.Fatalf("composed hook missing sidecar marker:\n%s", content)
	}

	before, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read before rerun: %v", err)
	}
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("second RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}
	after, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read after rerun: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("rerun changed the composed hook (not idempotent):\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if count := strings.Count(string(after), "BEGIN BEADS INTEGRATION"); count != 1 {
		t.Fatalf("BEGIN BEADS INTEGRATION appears %d times after rerun, want 1 (no duplication):\n%s", count, after)
	}
}

func snapshotHooksDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return "", err
		}
		builder.WriteString(entry.Name())
		builder.WriteString("\n")
		builder.Write(content)
		builder.WriteString("\n---\n")
	}
	return builder.String(), nil
}
