package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const historyMetadataBin = `#!/usr/bin/env bash
python3 - "$@" <<'PY'
import json, os, sys
metadata = {key: os.getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_" + key, "") for key in ["WORKTREE", "SOURCE", "TOOL", "ACTION", "SESSION_ID"]}
metadata["args"] = sys.argv[1:]
if "--content-file" in sys.argv:
    with open(sys.argv[sys.argv.index("--content-file") + 1]) as content:
        metadata["content"] = content.read()
with open(os.environ["AGENT_FITNESS_FUNCTIONS_LOG"], "a") as log:
    log.write(json.dumps(metadata) + "\n")
print('{"status":"pass"}')
PY
`

func TestHistoryMetadataAgentHook(t *testing.T) {
	for _, tool := range []string{"codex", "claude-code", ""} {
		t.Run(tool, func(t *testing.T) {
			t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL", tool)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID", "")
			repo := initGitRepo(t)
			repo, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(t.TempDir(), "calls")
			bin := fakeFitnessBin(t, historyMetadataBin)
			payload := `{"session_id":"session-42","tool_name":"Write","tool_input":{"file_path":"proposal.go","content":"package proposal\n"}}`
			if out, err := runPreToolUse(t, repo, payload, bin, log, ""); err != nil {
				t.Fatalf("hook: %v: %s", err, out)
			}
			metadata := readHistoryMetadata(t, log)
			for key, want := range map[string]string{"WORKTREE": repo, "SOURCE": "agent", "TOOL": tool, "ACTION": "Write", "SESSION_ID": "session-42", "content": "package proposal\n"} {
				if metadata[key] != want {
					t.Errorf("%s = %v, want %q", key, metadata[key], want)
				}
			}
			assertHistoryArgs(t, metadata, "--dry-run")
		})
	}
}

func TestHistoryMetadataGitHooks(t *testing.T) {
	// The fixture MUST supply its own commit identity, independent of the host.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, action := range []string{"pre-commit", "pre-push"} {
		t.Run(action, func(t *testing.T) {
			repo := initGitRepo(t)
			repo, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			runGit(t, repo, "config", "user.name", "History Hook Fixture")
			runGit(t, repo, "config", "user.email", "history-hook@example.invalid")
			writeFile(t, filepath.Join(repo, "sample.go"), "package before\n")
			runGit(t, repo, "add", ".")
			runGit(t, repo, "commit", "-m", "initial")
			writeFile(t, filepath.Join(repo, "sample.go"), "package submitted\n")
			runGit(t, repo, "add", ".")
			stdin := ""
			if action == "pre-push" {
				runGit(t, repo, "commit", "-m", "submitted")
				stdin = "refs/heads/main HEAD refs/heads/main HEAD~1\n"
			}
			writeFile(t, filepath.Join(repo, "sample.go"), "package disk\n")
			log := filepath.Join(t.TempDir(), "calls")
			bin := fakeFitnessBin(t, historyMetadataBin)
			cmd := exec.Command("bash", hookScriptPathFor(t, action+".sh"))
			cmd.Dir, cmd.Stdin = repo, strings.NewReader(stdin)
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGENT_FITNESS_FUNCTIONS_LOG="+log)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("hook: %v: %s", err, out)
			}
			metadata := readHistoryMetadata(t, log)
			for key, want := range map[string]string{"WORKTREE": repo, "SOURCE": "git", "TOOL": "git", "ACTION": action} {
				if metadata[key] != want {
					t.Errorf("%s = %v, want %q", key, metadata[key], want)
				}
			}
			if action == "pre-commit" {
				assertHistoryArgs(t, metadata, "--staged")
			} else if metadata["content"] != "package submitted\n" {
				t.Errorf("committed source changed: %v", metadata["content"])
			}
		})
	}
}

func readHistoryMetadata(t *testing.T, path string) map[string]any {
	t.Helper()
	var metadata map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &metadata); err != nil {
		t.Fatalf("expected exactly one invocation: %v", err)
	}
	return metadata
}

func assertHistoryArgs(t *testing.T, metadata map[string]any, required string) {
	t.Helper()
	found := false
	for _, value := range metadata["args"].([]any) {
		arg := value.(string)
		if strings.HasPrefix(arg, "--history-") {
			t.Errorf("hook passes unsupported metadata flag %q to older binaries", arg)
		}
		found = found || arg == required
	}
	if !found {
		t.Errorf("missing %s in %v", required, metadata["args"])
	}
}
