package hooks_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreToolUsePayloadNormalization(t *testing.T) {
	scriptPath := filepath.Join("..", "hooks", "pre-tool-use.sh")

	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "Claude Code payload format",
			payload: `{"tool_name":"Edit","tool_input":{"file_path":"test.py","old_string":"foo","new_string":"bar"}}`,
		},
		{
			name:    "Codex payload format",
			payload: `{"tool_name":"Edit","tool_input":{"file_path":"test.py","content":"def bar(): pass\n"}}`,
		},
		{
			name:    "OpenCode payload format with camelCase",
			payload: `{"tool":"edit","args":{"filePath":"test.py","oldString":"foo","newString":"bar"}}`,
		},
		{
			name:    "OpenCode payload format with replaceAll",
			payload: `{"tool":"edit","args":{"filePath":"test.py","oldString":"foo","newString":"bar","replaceAll":true}}`,
		},
		{
			name:    "Top-level payload with path key",
			payload: `{"path":"test.py","content":"def bar(): pass\n"}`,
		},
		{
			name:    "Top-level payload with filePath key",
			payload: `{"filePath":"test.py","content":"def bar(): pass\n"}`,
		},
		{
			name:    "Args wrapper with path key",
			payload: `{"tool":"write","args":{"path":"test.py","content":"def bar(): pass\n"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", scriptPath)
			cmd.Stdin = bytes.NewReader([]byte(tc.payload))
			cmd.Env = append(cmd.Environ(), "AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory")
			out, _ := cmd.CombinedOutput()
			if bytes.Contains(out, []byte("invalid JSON payload")) || bytes.Contains(out, []byte("missing file_path")) {
				t.Fatalf("unexpected payload parsing failure for %s: %s", tc.name, string(out))
			}
		})
	}
}

func TestGitGuardPayloadNormalization(t *testing.T) {
	scriptPath := filepath.Join("..", "hooks", "git-guard.sh")

	blockingTests := []struct {
		name    string
		payload string
	}{
		{
			name:    "Claude Code tool_input with command",
			payload: `{"tool_name":"Bash","tool_input":{"command":"git commit -n -m x"}}`,
		},
		{
			name:    "OpenCode args with command",
			payload: `{"tool":"bash","args":{"command":"git commit -n -m x"}}`,
		},
		{
			name:    "OpenCode args with cmd",
			payload: `{"tool":"bash","args":{"cmd":"git commit -n -m x"}}`,
		},
		{
			name:    "Top-level payload with command",
			payload: `{"command":"git commit -n -m x"}`,
		},
		{
			name:    "Top-level payload with cmd",
			payload: `{"cmd":"git commit -n -m x"}`,
		},
		{
			name:    "tool_input with cmd",
			payload: `{"tool_name":"Bash","tool_input":{"cmd":"git commit -n -m x"}}`,
		},
	}

	for _, tc := range blockingTests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", scriptPath)
			cmd.Stdin = bytes.NewReader([]byte(tc.payload))
			out, err := cmd.CombinedOutput()
			var exitCode int
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("unexpected execution error: %v", err)
			}
			if exitCode != 2 {
				t.Fatalf("exit code = %d, want 2 (blocked); output: %s", exitCode, string(out))
			}
			if !strings.Contains(string(out), "git-guard") {
				t.Fatalf("expected git-guard in output: %s", string(out))
			}
		})
	}

	allowingTests := []struct {
		name    string
		payload string
	}{
		{
			name:    "OpenCode args with cmd benign",
			payload: `{"tool":"bash","args":{"cmd":"git log -n 5"}}`,
		},
		{
			name:    "Top-level cmd benign",
			payload: `{"cmd":"git log -n 5"}`,
		},
	}

	for _, tc := range allowingTests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", scriptPath)
			cmd.Stdin = bytes.NewReader([]byte(tc.payload))
			out, err := cmd.CombinedOutput()
			var exitCode int
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("unexpected execution error: %v", err)
			}
			if exitCode != 0 {
				t.Fatalf("exit code = %d, want 0 (allowed); output: %s", exitCode, string(out))
			}
		})
	}
}
