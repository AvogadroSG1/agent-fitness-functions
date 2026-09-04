package client

import (
	"encoding/json"
	"os"
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
