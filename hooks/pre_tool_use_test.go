package hooks

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreToolUseBlocksWriteViolation(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALM_BRIDGE_LOG"
printf '{"status":"block","violations":[{"message":"too complex"}]}\n'
`)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Run() {}\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want block; output=%s", output)
	}
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "blocking") {
		t.Fatalf("output = %s, want YAML calm_check block with blocking mode", output)
	}
	logContent := readFile(t, logPath)
	for _, want := range []string{"check", "--file sample.go", "--repo ", "--content-file ", "--language go"} {
		if !strings.Contains(logContent, want) {
			t.Fatalf("calm log = %s, want %s", logContent, want)
		}
	}
}

func TestPreToolUseAllowsEditAdvisory(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.py"), "print('old')\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALM_BRIDGE_LOG"
printf '{"status":"advisory","violations":[{"message":"warning only"}]}\n'
`)
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"sample.py","old_string":"old","new_string":"print('new')\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
	if !strings.Contains(readFile(t, logPath), "--language python") {
		t.Fatalf("calm log = %s, want python language", readFile(t, logPath))
	}
}

func TestPreToolUseAllowsPassWithAbsolutePath(t *testing.T) {
	repo := initGitRepo(t)
	path := filepath.Join(repo, "src", "Widget.cs")
	writeFile(t, path, "namespace Demo;\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALM_BRIDGE_LOG"
printf '{"status":"pass"}\n'
`)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"` + path + `","content":"namespace Demo;\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "http://127.0.0.1:9999")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	logContent := readFile(t, logPath)
	for _, want := range []string{"--file src/Widget.cs", "--addr http://127.0.0.1:9999", "--language csharp"} {
		if !strings.Contains(logContent, want) {
			t.Fatalf("calm log = %s, want %s", logContent, want)
		}
	}
}

func TestPreToolUseChecksRunningDaemonKnownBadAndGood(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, ".calm", "config.json"), `{
  "enforcement-mode": "block",
  "fitness-functions": {
    "cyclomatic-complexity": true,
    "interface-width": false,
    "implementation-depth": false,
    "logic-density": false,
    "dependency-discipline": false
  }
}`)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	calmBridge := buildCalmBridge(t)
	serverURL := startBridgeDaemon(t, calmBridge)

	badPayload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Score(kind string, retries int, urgent bool) int {\nscore := 0\nif kind == \"create\" { score++ }\nif kind == \"update\" { score++ }\nif kind == \"delete\" { score++ }\nif kind == \"manual\" { score++ }\nif kind == \"batch\" { score++ }\nif kind == \"sync\" { score++ }\nif retries > 0 { score++ }\nif retries > 1 { score++ }\nif retries > 2 { score++ }\nif urgent { score++ }\nreturn score\n}\n"}}`
	output, err := runPreToolUseWithBin(t, repo, badPayload, calmBridge, "", "", serverURL)
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want running daemon block; output=%s", output)
	}
	if !strings.Contains(string(output), "cyclomatic-complexity") {
		t.Fatalf("output = %s, want YAML cyclomatic-complexity violation", output)
	}

	goodPayload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Score(kind string, retries int, urgent bool) int {\nscore := map[string]int{\"create\": 1, \"update\": 1, \"delete\": 1, \"manual\": 1, \"batch\": 1, \"sync\": 1}[kind]\nif urgent { score++ }\nif retries > 0 { score += min(retries, 3) }\nreturn score\n}\n"}}`
	output, err = runPreToolUseWithBin(t, repo, goodPayload, calmBridge, "", "", serverURL)
	if err != nil {
		t.Fatalf("pre-tool-use failed for known-good content: %v\n%s", err, output)
	}
}

func TestPreToolUsePreservesEmptyAndTrailingNewlineContent(t *testing.T) {
	repo := initGitRepo(t)
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    --content-file)
      shift
      python3 - "$1" "$CALM_BRIDGE_LOG" <<'PY'
import pathlib
import sys

content = pathlib.Path(sys.argv[1]).read_text()
with open(sys.argv[2], "a", encoding="utf-8") as handle:
    handle.write(repr(content) + "\n")
PY
      ;;
  esac
  shift
done
printf '{"status":"pass"}\n'
`)
	firstPayload := `{"tool_name":"Write","tool_input":{"file_path":"empty.go","content":""}}`
	if output, err := runPreToolUse(t, repo, firstPayload, fakeBin, logPath, ""); err != nil {
		t.Fatalf("empty pre-tool-use failed: %v\n%s", err, output)
	}
	secondPayload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\n\n"}}`
	if output, err := runPreToolUse(t, repo, secondPayload, fakeBin, logPath, ""); err != nil {
		t.Fatalf("newline pre-tool-use failed: %v\n%s", err, output)
	}
	logContent := readFile(t, logPath)
	if !strings.Contains(logContent, "''") || !strings.Contains(logContent, "'package sample\\n\\n'") {
		t.Fatalf("content log = %s, want empty and trailing newline content preserved", logContent)
	}
}

func TestPreToolUseBlocksBinarySourceContent(t *testing.T) {
	repo := initGitRepo(t)
	payload := "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"sample.go\",\"content\":\"abc\\u0000def\"}}"
	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "blocked binary content") {
		t.Fatalf("output = %s, want binary block", output)
	}
}

func TestPreToolUseSkipsFilesOutsideRepo(t *testing.T) {
	repo := initGitRepo(t)
	outside := filepath.Join(t.TempDir(), "sample.go")
	payload := `{"tool_name":"Write","tool_input":{"file_path":"` + outside + `","content":"package outside\n"}}`
	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "outside repository") {
		t.Fatalf("output = %s, want outside repo skip", output)
	}
}

func TestPreToolUseRejectsRemoteBridgeWithoutOptIn(t *testing.T) {
	repo := initGitRepo(t)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "http://127.0.0.1:80@evil.example")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want remote bridge rejection; output=%s", output)
	}
	if !strings.Contains(string(output), "must be loopback") {
		t.Fatalf("output = %s, want loopback diagnostic", output)
	}
}

func TestPreToolUseReportsMalformedJSONWithoutTraceback(t *testing.T) {
	repo := initGitRepo(t)
	output, err := runPreToolUse(t, repo, `{"tool_input":`, t.TempDir(), "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "Invalid PreToolUse payload") || strings.Contains(string(output), "Traceback") {
		t.Fatalf("output = %s, want clean invalid payload diagnostic", output)
	}
}

func TestPreToolUseBlocksInvalidBridgeJSONWithoutTraceback(t *testing.T) {
	repo := initGitRepo(t)
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
printf 'not json\n'
`)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	output, err := runPreToolUse(t, repo, payload, fakeBin, "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "invalid JSON") || strings.Contains(string(output), "Traceback") {
		t.Fatalf("output = %s, want clean invalid bridge JSON diagnostic", output)
	}
}

func TestPreToolUseHandlesLargeContentThroughContentFile(t *testing.T) {
	repo := initGitRepo(t)
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeCalmBridge(t, `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    --content-file)
      shift
      wc -c < "$1" >> "$CALM_BRIDGE_LOG"
      ;;
  esac
  shift
done
printf '{"status":"pass"}\n'
`)
	large := strings.Repeat("x", 512*1024)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"large.go","content":"` + large + `"}}`
	if output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, ""); err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(readFile(t, logPath), "524288") {
		t.Fatalf("log = %s, want content file byte count", readFile(t, logPath))
	}
}

func runPreToolUse(t *testing.T, repo, payload, fakeBin, logPath, addr string) ([]byte, error) {
	t.Helper()
	return runPreToolUseWithBin(t, repo, payload, "", fakeBin, logPath, addr)
}

func runPreToolUseWithBin(t *testing.T, repo, payload, calmBridge, pathDir, logPath, addr string) ([]byte, error) {
	t.Helper()
	command := exec.Command("bash", hookScriptPathFor(t, "pre-tool-use.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader(payload)
	env := os.Environ()
	if pathDir != "" {
		env = append(env, "PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if calmBridge != "" {
		env = append(env, "CALM_BRIDGE_BIN="+calmBridge)
	}
	if logPath != "" {
		env = append(env, "CALM_BRIDGE_LOG="+logPath)
	}
	if addr != "" {
		env = append(env, "CALM_BRIDGE_ADDR="+addr)
	}
	command.Env = env
	return command.CombinedOutput()
}

func hookScriptPathFor(t *testing.T, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	return filepath.Join(cwd, name)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func startBridgeDaemon(t *testing.T, calmBridge string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	command := exec.Command(calmBridge, "serve", "--addr", addr)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	command.Dir = filepath.Dir(cwd)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start bridge daemon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = http.Post("http://"+addr+"/shutdown", "application/json", nil)
		_ = command.Wait()
	})
	client := &http.Client{Timeout: time.Second}
	for range 50 {
		response, err := client.Get("http://" + addr + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return "http://" + addr
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("bridge daemon did not become healthy")
	return ""
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
