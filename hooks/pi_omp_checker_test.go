package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeCheckerReconstructsOMPReplaceLosslessly(t *testing.T) {
	cases := []struct {
		name    string
		current string
		edits   []map[string]any
		want    string
	}{
		{
			name:    "BOM CRLF and literal replacement markers",
			current: "\ufeffa\r\nb\r\nb\r\n",
			edits:   []map[string]any{{"old_string": "b\nb", "new_string": "$&$'", "replace_all": false}},
			want:    "\ufeffa\r\n$&$'\r\n",
		},
		{
			name:    "replace all",
			current: "a\nb\nb\n",
			edits:   []map[string]any{{"old_string": "b", "new_string": "c", "replace_all": true}},
			want:    "a\nc\nc\n",
		},
		{
			name:    "sequential batch",
			current: "a\nb\n",
			edits: []map[string]any{
				{"old_string": "a", "new_string": "b"},
				{"old_string": "b\nb", "new_string": "c\nb"},
			},
			want: "c\nb\n",
		},
		{
			name:    "lone CR normalizes to LF",
			current: "a\rb",
			edits:   []map[string]any{{"old_string": "a\nb", "new_string": "x\r\ny"}},
			want:    "x\ny",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := runNativeChecker(t, "reconstruct", map[string]any{
				"current": testCase.current,
				"edits":   testCase.edits,
			})
			if got != testCase.want {
				t.Fatalf("reconstruct = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestNativeCheckerRejectsInvalidOMPReplaceProposals(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		edits []map[string]any
		want  string
	}{
		{name: "ambiguous", edits: []map[string]any{{"old_string": "b", "new_string": "c"}}, want: "matched multiple"},
		{name: "missing", edits: []map[string]any{{"old_string": "z", "new_string": "c"}}, want: "not found"},
		{name: "empty", edits: []map[string]any{{"old_string": "", "new_string": "c"}}, want: "empty old_string"},
		{name: "no-op", edits: []map[string]any{{"old_string": "a", "new_string": "a"}}, want: "no-op"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := runNativeChecker(t, "reconstruct-error", map[string]any{
				"current": "a\nb\nb\n",
				"edits":   testCase.edits,
			})
			if !strings.Contains(got, testCase.want) {
				t.Fatalf("error = %q, want substring %q", got, testCase.want)
			}
		})
	}
}

func TestNativeCheckerUsesResolvedHooksAndPreservesDiagnostics(t *testing.T) {
	repo := initGitRepo(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	envPath := filepath.Join(t.TempDir(), "environment.txt")
	preToolHook := filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-tool-use")
	writeExecutable(t, preToolHook, "#!/bin/sh\ncat > \"$NATIVE_CHECKER_PAYLOAD\"\nprintf '%s|%s|%s|%s\\n' \"$AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL\" \"$AGENT_FITNESS_FUNCTIONS_HISTORY_ACTION\" \"$AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID\" \"$AGENT_FITNESS_FUNCTIONS_WORKTREE\" > \"$NATIVE_CHECKER_ENV\"\nprintf 'advisory from native hook\\n'\n")
	t.Setenv("NATIVE_CHECKER_PAYLOAD", payloadPath)
	t.Setenv("NATIVE_CHECKER_ENV", envPath)

	result := runNativeCheckerResult(t, "check-proposal", repo, map[string]any{
		"agent":    "pi",
		"action":   "edit",
		"session":  "session-1",
		"filePath": "sample.go",
		"content":  "package sample\n",
	})
	if !result.Allowed || result.Diagnostic != "advisory from native hook" {
		t.Fatalf("checkProposal result = %+v, want allowed advisory", result)
	}
	var payload map[string]any
	decodeJSONFile(t, payloadPath, &payload)
	if payload["tool_name"] != "edit" {
		t.Fatalf("payload tool_name = %v, want edit", payload["tool_name"])
	}
	toolInput, ok := payload["tool_input"].(map[string]any)
	if !ok || toolInput["file_path"] != "sample.go" || toolInput["content"] != "package sample\n" {
		t.Fatalf("payload tool_input = %#v", payload["tool_input"])
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("resolve fixture repository path: %v", err)
	}
	if env := readTextFile(t, envPath); !strings.Contains(env, "pi|edit|session-1|"+resolvedRepo) {
		t.Fatalf("adapter environment = %q, want Pi/action/session/worktree metadata", env)
	}

	writeExecutable(t, preToolHook, "#!/bin/sh\nprintf 'blocked by native hook\\n' >&2\nexit 2\n")
	blocked := runNativeCheckerResult(t, "check-proposal", repo, map[string]any{
		"agent":    "pi",
		"action":   "write",
		"session":  "session-2",
		"filePath": "sample.go",
		"content":  "package sample\nfunc Bad() {}\n",
	})
	if blocked.Allowed || blocked.Diagnostic != "blocked by native hook" {
		t.Fatalf("blocked checkProposal result = %+v", blocked)
	}
}

func TestNativeCheckerResolvesOMPAuthoredPaths(t *testing.T) {
	repo := t.TempDir()
	for _, testCase := range []struct {
		authored string
		want     string
	}{
		{authored: "[src/main.go#ABCD]", want: filepath.Join(repo, "src", "main.go")},
		{authored: ":src/main.go", want: filepath.Join(repo, "src", "main.go")},
		{authored: "@/tmp/main.go", want: "/tmp/main.go"},
		{authored: "file:///tmp/main%20file.go", want: "/tmp/main file.go"},
	} {
		got := runNativeChecker(t, "resolve-path", map[string]any{"cwd": repo, "path": testCase.authored})
		if got != testCase.want {
			t.Errorf("resolve %q = %q, want %q", testCase.authored, got, testCase.want)
		}
	}
}

type nativeCheckerResult struct {
	Allowed    bool   `json:"allowed"`
	Diagnostic string `json:"diagnostic"`
}

func runNativeCheckerResult(t *testing.T, operation, cwd string, input map[string]any) nativeCheckerResult {
	t.Helper()
	input["cwd"] = cwd
	var result nativeCheckerResult
	decodeJSON(t, []byte(runNativeChecker(t, operation, input)), &result)
	return result
}

func runNativeChecker(t *testing.T, operation string, input map[string]any) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for native checker tests")
	}
	helperPath, err := filepath.Abs("native-checker.mjs")
	if err != nil {
		t.Fatalf("resolve native-checker.mjs: %v", err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal helper input: %v", err)
	}
	script := `import { pathToFileURL } from "node:url";
const helper = await import(pathToFileURL(process.argv[1]).href);
const input = JSON.parse(process.argv[2]);
try {
  let output;
  if (process.argv[3] === "reconstruct") output = helper.reconstructOMPReplace(input.current, input.edits);
  else if (process.argv[3] === "reconstruct-error") { try { helper.reconstructOMPReplace(input.current, input.edits); output = "no error"; } catch (error) { output = error.message; } }
  else if (process.argv[3] === "resolve-path") output = helper.resolveOMPFilePath(input.cwd, input.path);
  else if (process.argv[3] === "check-proposal") output = await helper.checkProposal(input.cwd, input.agent, input.action, input.session, input.filePath, input.content);
  else throw new Error("unknown operation");
  process.stdout.write(typeof output === "string" ? output : JSON.stringify(output));
} catch (error) {
  process.stderr.write(error.stack || String(error));
  process.exitCode = 1;
}`
	command := exec.Command("node", "--input-type=module", "-e", script, helperPath, string(encoded), operation)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native checker operation %s failed: %v\n%s", operation, err, output)
	}
	return string(output)
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func decodeJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	decodeJSON(t, []byte(readTextFile(t, path)), target)
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, data)
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
