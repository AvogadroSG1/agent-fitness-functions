package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooksGeneratesOpenCodePlugin(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}

	pluginPath := filepath.Join(repo, ".opencode", "plugins", "agent-fitness-functions.js")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}

	str := string(content)
	if !strings.Contains(str, "tool.execute.before") {
		t.Errorf("plugin missing 'tool.execute.before' hook listener")
	}
	if !strings.Contains(str, "agent-fitness-functions-git-guard") {
		t.Errorf("plugin missing git-guard hook reference")
	}
	if !strings.Contains(str, "agent-fitness-functions-pre-tool-use") {
		t.Errorf("plugin missing pre-tool-use hook reference")
	}

	// Idempotency check: run again and ensure it succeeds and remains valid
	var stdout2, stderr2 bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout2, &stderr2); err != nil {
		t.Fatalf("RunInstallHooks (second pass): %v\nstderr=%s", err, stderr2.String())
	}
	content2, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read plugin after second pass: %v", err)
	}
	if string(content2) != str {
		t.Errorf("plugin content changed after second pass")
	}
}
