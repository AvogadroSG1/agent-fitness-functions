package client

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// calm-poc-q8d.3: given hooks owned by Beads and Lefthook, when
// install-hooks runs without overwrite/append escape hatches, then the
// governance call composes exactly once, lands before any terminal exec,
// preserves both known owners, propagates failure codes, and repeated
// onboarding never duplicates anything.

const beadsBlock = "# --- BEGIN BEADS INTEGRATION v1.2.2 ---\n" +
	"echo beads >> \"$COMPOSITION_LOG\"\n" +
	"# --- END BEADS INTEGRATION v1.2.2 ---\n"

func seedBeadsLefthookHook(t *testing.T, hooksDir string) string {
	t.Helper()
	hook := filepath.Join(hooksDir, "pre-commit")
	content := "#!/usr/bin/env bash\n" +
		beadsBlock +
		"exec lefthook run \"pre-commit\" \"$@\"\n"
	if err := os.WriteFile(hook, []byte(content), 0o755); err != nil {
		t.Fatalf("seed beads+lefthook hook: %v", err)
	}
	return hook
}

func TestInstallHooksComposesKnownOwnersWithoutEscapeHatch(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	hook := seedBeadsLefthookHook(t, hooksDir)

	t.Setenv("AGENT_FITNESS_FUNCTIONS_HOOK_APPEND", "")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE", "")

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks refused a known-owner chain without escape hatches: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read composed hook: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "BEGIN BEADS INTEGRATION") {
		t.Fatalf("Beads block destroyed during composition:\n%s", text)
	}
	if !strings.Contains(text, "lefthook run") {
		t.Fatalf("Lefthook invocation destroyed during composition:\n%s", text)
	}
	sidecarCall := "agent-fitness-functions-pre-commit"
	if count := strings.Count(text, sidecarCall); count != 1 {
		t.Fatalf("delegated sidecar call appears %d times, want exactly 1:\n%s", count, text)
	}
	execIndex := strings.Index(text, "exec lefthook")
	sidecarIndex := strings.Index(text, sidecarCall)
	if execIndex == -1 || sidecarIndex == -1 || sidecarIndex > execIndex {
		t.Fatalf("delegated call must execute before the terminal exec (sidecar at %d, exec at %d):\n%s", sidecarIndex, execIndex, text)
	}

	before := text
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("second RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}
	after, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read hook after rerun: %v", err)
	}
	if before != string(after) {
		t.Fatalf("repeated onboarding changed the composed hook:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestComposedHookRunsEachOwnerOnceAndPropagatesFailure(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	hook := seedBeadsLefthookHook(t, hooksDir)

	t.Setenv("AGENT_FITNESS_FUNCTIONS_HOOK_APPEND", "")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE", "")
	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}

	// Stub the delegated sidecar and lefthook so the composed chain is
	// executable: each participant records one line, and the sidecar's
	// exit code drives the chain's.
	logPath := filepath.Join(t.TempDir(), "composition.log")
	binDir := t.TempDir()
	lefthookStub := "#!/usr/bin/env bash\necho lefthook >> \"$COMPOSITION_LOG\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "lefthook"), []byte(lefthookStub), 0o755); err != nil {
		t.Fatalf("stub lefthook: %v", err)
	}
	sidecarPath := filepath.Join(hooksDir, "agent-fitness-functions-pre-commit")
	sidecarStub := "#!/usr/bin/env bash\necho agent >> \"$COMPOSITION_LOG\"\nexit \"${SIDECAR_EXIT:-0}\"\n"
	if err := os.WriteFile(sidecarPath, []byte(sidecarStub), 0o755); err != nil {
		t.Fatalf("stub sidecar: %v", err)
	}

	run := func(sidecarExit string) (string, error) {
		if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("reset log: %v", err)
		}
		command := exec.Command("bash", hook)
		command.Dir = repo
		command.Env = append(os.Environ(),
			"PATH="+binDir+":"+os.Getenv("PATH"),
			"COMPOSITION_LOG="+logPath,
			"SIDECAR_EXIT="+sidecarExit,
		)
		runErr := command.Run()
		content, readErr := os.ReadFile(logPath)
		if readErr != nil {
			t.Fatalf("read composition log: %v", readErr)
		}
		return string(content), runErr
	}

	log, err := run("0")
	if err != nil {
		t.Fatalf("composed hook failed on the success path: %v\nlog:\n%s", err, log)
	}
	for _, owner := range []string{"beads", "agent", "lefthook"} {
		if count := strings.Count(log, owner+"\n"); count != 1 {
			t.Fatalf("owner %q executed %d times, want exactly once:\n%s", owner, count, log)
		}
	}

	if _, err := run("7"); err == nil {
		t.Fatal("composed hook exited zero although the governance sidecar failed")
	}
}

func TestInstallHooksPreservesForgeSessionStartSettings(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	settings := `{"hooks":{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"forge-session-start"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge-guard"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	text := string(content)
	for _, preserved := range []string{"forge-session-start", "forge-guard"} {
		if count := strings.Count(text, preserved); count != 1 {
			t.Fatalf("pre-existing Forge entry %q appears %d times, want exactly 1:\n%s", preserved, count, text)
		}
	}
	for _, installed := range []string{"agent-fitness-functions-git-guard", "agent-fitness-functions-pre-tool-use"} {
		if count := strings.Count(text, installed); count != 1 {
			t.Fatalf("installed entry %q appears %d times, want exactly 1:\n%s", installed, count, text)
		}
	}
}
