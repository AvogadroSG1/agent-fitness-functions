package calm_poc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonicalGovernanceKey is the logical governance repository key this
// repository governs itself under. ADR-0009 supersedes ADR-0003's Identity
// Boundaries clause and makes the product name the canonical key, so the
// key, the product, the binary, and the configs/ directory all agree.
const canonicalGovernanceKey = "agent-fitness-functions"

// productHookMarker identifies a PreToolUse entry that invokes this product's
// hook, regardless of whether it runs the tracked script or an installed
// sidecar.
const productHookMarker = "pre-tool-use"

// binPinEnvVar is the environment variable that overrides binary resolution.
// The tracked settings file must never set it: hooks/pre-tool-use.sh already
// defaults to `agent-fitness-functions`, resolved from PATH, which is the only
// location a fresh clone can rely on.
const binPinEnvVar = "AGENT_FITNESS_FUNCTIONS_BIN"

// repoNameEnvVar overrides the governance repository key sent to the server.
const repoNameEnvVar = "AGENT_FITNESS_FUNCTIONS_REPO_NAME"

type claudeSettings struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command string `json:"command"`
			Type    string `json:"type"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func readTrackedClaudeSettings(t *testing.T) claudeSettings {
	t.Helper()

	path := filepath.Join(".claude", "settings.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var settings claudeSettings
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return settings
}

// preToolUseCommands returns every PreToolUse command string paired with its
// matcher.
func preToolUseCommands(t *testing.T) map[string][]string {
	t.Helper()

	commands := make(map[string][]string)
	for _, entry := range readTrackedClaudeSettings(t).Hooks["PreToolUse"] {
		for _, hook := range entry.Hooks {
			commands[entry.Matcher] = append(commands[entry.Matcher], hook.Command)
		}
	}
	return commands
}

// calm-poc-says: given a fresh clone, when the self-governance hook runs, then
// it resolves the binary from PATH rather than a gitignored, hand-built path.
func TestSelfGovernanceHookDoesNotPinABinary(t *testing.T) {
	for matcher, commands := range preToolUseCommands(t) {
		for _, command := range commands {
			if strings.Contains(command, binPinEnvVar+"=") {
				t.Errorf("PreToolUse[%s] pins %s: %q; hooks/pre-tool-use.sh already defaults to PATH resolution, and a pinned path does not exist on a fresh clone", matcher, binPinEnvVar, command)
			}
		}
	}
}

// calm-poc-ycyk: given the tracked settings file is shared by every clone, when
// a hook entry is registered, then it names no machine-absolute path.
func TestTrackedClaudeSettingsUseNoAbsolutePaths(t *testing.T) {
	for matcher, commands := range preToolUseCommands(t) {
		for _, command := range commands {
			for _, field := range strings.Fields(command) {
				token := strings.Trim(field, `"'`)
				if strings.HasPrefix(token, "/") {
					t.Errorf("PreToolUse[%s] names absolute path %q in %q; tracked settings must stay portable across clones", matcher, token, command)
				}
			}
		}
	}
}

// calm-poc-ycyk: given both settings.json and settings.local.json can register
// hooks, when the product hook is registered, then the tracked file registers it
// at most once per matcher so an edit is not validated twice.
func TestSelfGovernanceHookRegisteredAtMostOncePerMatcher(t *testing.T) {
	for matcher, commands := range preToolUseCommands(t) {
		count := 0
		for _, command := range commands {
			if strings.Contains(command, productHookMarker) {
				count++
			}
		}
		if count > 1 {
			t.Errorf("PreToolUse[%s] registers the product hook %d times, want at most 1: %q", matcher, count, commands)
		}
	}
}

// calm-poc-sip4: given a single canonical governance key, when the hook names a
// repository, then it names that key and a mounted config exists for it.
func TestSelfGovernanceHookUsesCanonicalGovernanceKey(t *testing.T) {
	for matcher, commands := range preToolUseCommands(t) {
		for _, command := range commands {
			_, rest, found := strings.Cut(command, repoNameEnvVar+"=")
			if !found {
				continue
			}
			name := strings.Trim(strings.Fields(rest)[0], `"'`)
			if name != canonicalGovernanceKey {
				t.Errorf("PreToolUse[%s] governs this repository as %q, want the canonical key %q", matcher, name, canonicalGovernanceKey)
			}
		}
	}

	path := filepath.Join("configs", canonicalGovernanceKey, "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s: %v; the canonical governance key must have a mounted config", path, err)
	}
}
