package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOpenCodeHistoryOrigin(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	for _, session := range []string{"session-opencode", ""} {
		t.Run(session, func(t *testing.T) {
			repo := initGitRepo(t)
			plugin, err := os.ReadFile("opencode-plugin.js")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(repo, "plugin.mjs"), string(plugin))
			writeFile(t, filepath.Join(repo, "invoke.mjs"), `import { AgentFitnessFunctionsPlugin } from "./plugin.mjs";
const plugin = await AgentFitnessFunctionsPlugin();
await plugin["tool.execute.before"]({tool: "write", sessionID: process.env.TEST_SESSION}, {args: {filePath: "sample.go", content: "package sample\n"}});
`)
			hook := filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-tool-use")
			writeFile(t, hook, `#!/usr/bin/env python3
import json, os, sys
payload = json.load(sys.stdin)
payload["source"] = os.getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOURCE", "")
payload["tool"] = os.getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL", "")
payload["action"] = os.getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_ACTION", "")
payload["session"] = os.getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID", "")
with open(os.environ["AGENT_FITNESS_FUNCTIONS_LOG"], "w") as log:
    json.dump(payload, log)
`)
			if err := os.Chmod(hook, 0o755); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(repo, "invocation.json")
			cmd := exec.Command("node", "invoke.mjs")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "AGENT_FITNESS_FUNCTIONS_LOG="+log, "TEST_SESSION="+session)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("plugin: %v: %s", err, out)
			}
			var metadata map[string]any
			if err := json.Unmarshal([]byte(readFile(t, log)), &metadata); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{"source": "agent", "tool": "opencode", "action": "write", "session": session, "session_id": session} {
				if metadata[key] != want {
					t.Errorf("%s = %v, want %q", key, metadata[key], want)
				}
			}
		})
	}
}
