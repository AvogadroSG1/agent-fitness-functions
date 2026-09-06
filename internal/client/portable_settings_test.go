package client

// Red contract for calm-poc-vrk: PreToolUse entries written into the tracked
// .claude/settings.json must contain no absolute machine path — otherwise
// onboarding the same repo on a second machine rewrites the entry and breaks
// the first machine on the next pull. The generated command self-locates via
// `git rev-parse --git-path`, exactly the way the installed hook scripts do,
// and doctor must stat the resolved script instead of trusting the marker.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settingsPreToolUseCommands returns every PreToolUse command string in the
// repo's .claude/settings.json.
func settingsPreToolUseCommands(t *testing.T, repo string) []string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	var commands []string
	for _, rawEntry := range preToolUseEntries(ensureHooksSection(settings)) {
		if command, ok := hookEntryCommand(rawEntry); ok {
			commands = append(commands, command)
		}
	}
	return commands
}

func portableCommandFor(hookName string) string {
	return `"$(git rev-parse --git-path hooks/` + hookName + `)"`
}

func TestInstallHooksWritesPortableClaudeCommands(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}

	commands := settingsPreToolUseCommands(t, repo)
	if len(commands) != 2 {
		t.Fatalf("PreToolUse commands = %v, want git-guard and Edit/Write entries", commands)
	}
	wants := map[string]bool{
		portableCommandFor(gitGuardName): false,
		"AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL=claude-code " + portableCommandFor(agentHookName): false,
	}
	for _, command := range commands {
		if strings.Contains(command, repo) {
			t.Errorf("command %q embeds the machine-local repo path %q; the tracked settings.json must stay machine-portable", command, repo)
		}
		if _, ok := wants[command]; ok {
			wants[command] = true
		}
	}
	for want, seen := range wants {
		if !seen {
			t.Errorf("missing portable entry %q in %v", want, commands)
		}
	}
}

func TestInstallHooksUpgradesAbsoluteEntryToPortable(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	// A pre-fix install on another machine left an absolute path behind.
	stale := map[string]any{"hooks": map[string]any{"PreToolUse": []any{
		claudeCommandEntry("Edit|Write", "/Users/somebody-else/src/repo/.git/hooks/"+agentHookName),
		claudeCommandEntry("Bash", "/Users/somebody-else/src/repo/.git/hooks/"+gitGuardName),
	}}}
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeSettings(filepath.Join(repo, ".claude", "settings.json"), stale); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}

	commands := settingsPreToolUseCommands(t, repo)
	if len(commands) != 2 {
		t.Fatalf("PreToolUse commands = %v, want exactly two upserted entries (no duplicates)", commands)
	}
	for _, command := range commands {
		if strings.Contains(command, "/Users/somebody-else/") {
			t.Errorf("stale absolute entry survived: %q", command)
		}
	}
}

func TestGitGuardSettingsResultFailsWhenResolvedScriptMissing(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	// Entry present (marker matches) but the script it names was never
	// installed on THIS machine — the exact fresh-clone state doctor
	// previously reported as a false pass.
	settings := map[string]any{"hooks": map[string]any{"PreToolUse": []any{
		claudeCommandEntry("Bash", portableCommandFor(gitGuardName)),
	}}}
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeSettings(filepath.Join(repo, ".claude", "settings.json"), settings); err != nil {
		t.Fatal(err)
	}

	result := gitGuardSettingsResult(repo)

	if result.passed {
		t.Fatalf("gitGuardSettingsResult = passed for an entry whose script does not exist; doctor must stat the resolved command path")
	}
	if !strings.Contains(result.remediation, "install-hooks") {
		t.Fatalf("remediation = %q, want install-hooks", result.remediation)
	}
}

func TestEditWriteHookResultWarnsWhenResolvedScriptMissing(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	settings := map[string]any{"hooks": map[string]any{"PreToolUse": []any{
		claudeCommandEntry("Edit|Write", portableCommandFor(agentHookName)),
	}}}
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeSettings(filepath.Join(repo, ".claude", "settings.json"), settings); err != nil {
		t.Fatal(err)
	}

	result := editWriteHookResult(repo)

	if result.passed {
		t.Fatalf("editWriteHookResult = passed for an entry whose script does not exist; doctor must stat the resolved command path")
	}
	if !result.warning {
		t.Fatalf("editWriteHookResult must stay advisory (warning), got %+v", result)
	}
}
