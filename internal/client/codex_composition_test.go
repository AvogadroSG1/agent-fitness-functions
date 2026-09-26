package client

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooksUpsertsCodexHooks(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	codexDir := filepath.Join(repo, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}

	initialContent := `{
  "hooks": {
    "PreCompact": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "bd prime"
          }
        ]
      }
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(initialContent), 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}

	installer := hookInstaller{repoRoot: repo, stdout: os.Stdout}
	if err := installer.installGitGuard(); err != nil {
		t.Fatalf("installGitGuard: %v", err)
	}
	if err := installer.installAgentHook(); err != nil {
		t.Fatalf("installAgentHook: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}

	var parsed struct {
		Hooks struct {
			PreCompact []json.RawMessage `json:"PreCompact"`
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("unmarshal hooks.json: %v\ncontent:\n%s", err, string(content))
	}

	if len(parsed.Hooks.PreCompact) != 1 {
		t.Errorf("PreCompact hook was clobbered, got %d entries", len(parsed.Hooks.PreCompact))
	}
	if len(parsed.Hooks.PreToolUse) != 2 {
		t.Fatalf("PreToolUse hooks count = %d, want 2 (Bash + Edit|Write)", len(parsed.Hooks.PreToolUse))
	}

	foundBash := false
	foundEditWrite := false
	for _, entry := range parsed.Hooks.PreToolUse {
		if entry.Matcher == "Bash" {
			foundBash = true
			if len(entry.Hooks) != 1 || !strings.Contains(entry.Hooks[0].Command, "agent-fitness-functions-git-guard") {
				t.Errorf("expected git-guard command in Bash hook, got: %+v", entry.Hooks)
			}
		}
		if entry.Matcher == "Edit|Write" {
			foundEditWrite = true
			if len(entry.Hooks) != 1 || !strings.Contains(entry.Hooks[0].Command, "agent-fitness-functions-pre-tool-use") {
				t.Errorf("expected pre-tool-use command in Edit|Write hook, got: %+v", entry.Hooks)
			}
		}
	}
	if !foundBash {
		t.Errorf("missing PreToolUse hook for Matcher 'Bash'")
	}
	if !foundEditWrite {
		t.Errorf("missing PreToolUse hook for Matcher 'Edit|Write'")
	}

	// Verify idempotency
	if err := installer.installGitGuard(); err != nil {
		t.Fatalf("second installGitGuard: %v", err)
	}
	if err := installer.installAgentHook(); err != nil {
		t.Fatalf("second installAgentHook: %v", err)
	}

	contentAfter, err := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json after second install: %v", err)
	}
	if err := json.Unmarshal(contentAfter, &parsed); err != nil {
		t.Fatalf("unmarshal hooks.json after second install: %v", err)
	}
	if len(parsed.Hooks.PreToolUse) != 2 {
		t.Fatalf("idempotency violation: PreToolUse hooks count = %d after re-install, want 2", len(parsed.Hooks.PreToolUse))
	}
}

func TestInstallHooksRestoresDisabledCodexContentValidationMatcher(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	productCommand := `AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL=codex "$(git rev-parse --git-path hooks/agent-fitness-functions-pre-tool-use)"`
	initial, err := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{
		map[string]any{"matcher": "DISABLED-Edit|Write", "hooks": []any{map[string]any{"type": "command", "command": productCommand}}},
		map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "echo unrelated"}}},
	}}})
	if err != nil {
		t.Fatalf("marshal hooks.json: %v", err)
	}
	hooksJSON := filepath.Join(repo, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksJSON), 0o755); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	if err := os.WriteFile(hooksJSON, initial, 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}

	installer := hookInstaller{repoRoot: repo, stdout: os.Stdout}
	var runs [2][]byte
	for run := range runs {
		if err := installer.installGitGuard(); err != nil {
			t.Fatalf("installGitGuard run %d: %v", run, err)
		}
		if err := installer.installAgentHook(); err != nil {
			t.Fatalf("installAgentHook run %d: %v", run, err)
		}
		if runs[run], err = os.ReadFile(hooksJSON); err != nil {
			t.Fatalf("read hooks.json run %d: %v", run, err)
		}
	}
	if string(runs[0]) != string(runs[1]) {
		t.Fatalf("re-install changed hooks.json:\nfirst:\n%s\nsecond:\n%s", runs[0], runs[1])
	}
	assertRestoredCodexEntries(t, runs[1], productCommand)

	scriptPath, err := installer.gitHookPath(agentHookName)
	if err != nil {
		t.Fatalf("gitHookPath: %v", err)
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(scriptPath), "apply-patch-proposals.py"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("apply_patch helper not installed executable: info=%v err=%v", info, err)
	}
	assertInstalledHookValidatesApplyPatch(t, repo, scriptPath)
}

func assertRestoredCodexEntries(t *testing.T, content []byte, productCommand string) {
	t.Helper()
	var parsed struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("unmarshal hooks.json: %v\n%s", err, content)
	}
	var productMatchers []string
	unrelated := false
	for _, entry := range parsed.Hooks.PreToolUse {
		for _, hook := range entry.Hooks {
			if hook.Command == productCommand {
				productMatchers = append(productMatchers, entry.Matcher)
			}
			unrelated = unrelated || (entry.Matcher == "Bash" && hook.Command == "echo unrelated")
		}
	}
	if len(productMatchers) != 1 || productMatchers[0] != "Edit|Write" {
		t.Fatalf("product entry matchers = %v, want exactly [Edit|Write]\n%s", productMatchers, content)
	}
	if !unrelated {
		t.Fatalf("unrelated Bash entry was not preserved:\n%s", content)
	}
}

func assertInstalledHookValidatesApplyPatch(t *testing.T, repo, scriptPath string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "sample.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write sample.go: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := filepath.Join(t.TempDir(), "agent-fitness-functions")
	fakeScript := `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "--file" ]]; then printf -- '--file %s\n' "$2" >> "$AGENT_FITNESS_FUNCTIONS_LOG"; fi
  shift
done
printf '{"status":"pass"}\n'
`
	if err := os.WriteFile(fakeBin, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"tool_name": "apply_patch",
		"cwd":       repo,
		"tool_input": map[string]any{
			"command": "*** Begin Patch\n*** Update File: sample.go\n@@\n package sample\n+func A() {}\n*** End Patch",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	command := exec.Command("bash", scriptPath)
	command.Dir = repo
	command.Stdin = strings.NewReader(string(payload))
	command.Env = append(os.Environ(), "AGENT_FITNESS_FUNCTIONS_BIN="+fakeBin, "AGENT_FITNESS_FUNCTIONS_LOG="+logPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installed hook rejected apply_patch: %v\n%s", err, output)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(logContent), "--file sample.go") {
		t.Fatalf("installed hook did not validate sample.go: log=%q err=%v", logContent, err)
	}
}
