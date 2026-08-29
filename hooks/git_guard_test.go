package hooks

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The git-guard MUST block genuine hook-bypass invocations with exit 2.
func TestGitGuardBlocksGenuineBypasses(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"long no-verify (demo contract case)", `git commit --no-verify -m 'skip checks'`},
		{"short -n", `git commit -n -m x`},
		{"cluster -an", `git commit -an -m x`},
		{"cluster -amn", `git commit -amn "x"`},
		{"no-gpg-sign", `git commit --no-gpg-sign -m x`},
		{"global -C before commit", `git -C /some/repo commit -n -m x`},
		{"global -c before commit", `git -c user.name=x commit --no-verify`},
		{"cd chain before git", `cd repo && git commit -n -m x`},
		{"bypass in second segment", `git commit -m "ok" && git push -f`},
		{"merge ff-only", `git merge --ff-only feature`},
		{"pull ff-only", `git pull --ff-only origin main`},
		{"push force long", `git push --force origin main`},
		{"push force short", `git push -f`},
		{"push force trailing", `git push origin main --force`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, code := runGitGuard(t, tc.command)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (deny); output=%s", code, output)
			}
			if !strings.Contains(string(output), "git-guard") {
				t.Fatalf("output = %s, want git-guard denial diagnostic", output)
			}
		})
	}
}

// The git-guard MUST NOT deny benign commands: flags belonging to other
// commands in a compound line, or bypass-flag text quoted inside commit
// messages, are data — not bypasses.
func TestGitGuardAllowsBenignCommands(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"reported FP: git log -n after commit", `git commit --quiet --message "x" && git log -n 1`},
		{"grep -rn after commit", `git commit -m x; grep -rn foo .`},
		{"find -name before commit", `find . -name "*.md"; git commit -m x`},
		{"echo -n after commit", `git commit --quiet --message "x" && echo -n done`},
		{"no-verify text in message", `git commit -m "do not pass --no-verify here"`},
		{"-n text in message", `git commit -m "-n means dry run"`},
		{"multi-line message containing -n", "git commit -m \"line1\n-n line2\""},
		{"inline message with flag text", `git commit --message="contains -n and --no-verify"`},
		{"force-with-lease", `git push --force-with-lease origin main`},
		{"rm -rf in other segment", `rm -rf ./build && git push origin main`},
		{"git log -n alone", `git log -n 5`},
		{"plain quiet commit", `git commit --quiet --message "x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, code := runGitGuard(t, tc.command)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (allow); output=%s", code, output)
			}
		})
	}
}

// Payloads without a usable command MUST be allowed (current contract).
func TestGitGuardAllowsNonCommandPayloads(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"empty command", `{"tool_name":"Bash","tool_input":{"command":""}}`},
		{"missing command", `{"tool_name":"Bash","tool_input":{}}`},
		{"malformed JSON", `{"tool_input":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, code := runGitGuardPayload(t, tc.payload)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (allow); output=%s", code, output)
			}
		})
	}
}

func runGitGuard(t *testing.T, command string) ([]byte, int) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": command},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return runGitGuardPayload(t, string(encoded))
}

func runGitGuardPayload(t *testing.T, payload string) ([]byte, int) {
	t.Helper()
	command := exec.Command("bash", hookScriptPathFor(t, "git-guard.sh"))
	command.Dir = t.TempDir()
	command.Stdin = strings.NewReader(payload)
	output, err := command.CombinedOutput()
	return output, exitCode(err)
}
