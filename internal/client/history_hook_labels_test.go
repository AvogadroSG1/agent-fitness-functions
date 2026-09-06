package client

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryGeneratedAgentLabels(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	if err := RunInstallHooks([]string{repo}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	for path, label := range map[string]string{".codex/hooks.json": "codex", ".claude/settings.json": "claude-code"} {
		body, err := os.ReadFile(filepath.Join(repo, path))
		if err != nil {
			t.Fatal(err)
		}
		var settings struct {
			Hooks map[string][]struct {
				Matcher string
				Hooks   []struct{ Command string }
			}
		}
		if err := json.Unmarshal(body, &settings); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range settings.Hooks["PreToolUse"] {
			if entry.Matcher != "Edit|Write" {
				continue
			}
			for _, command := range entry.Hooks {
				found = strings.Contains(command.Command, "AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL="+label)
			}
		}
		if !found {
			t.Errorf("%s lacks %s history attribution: %s", path, label, body)
		}
	}
}
