package hooks

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// applyPatchFakeBin logs every call's arguments, --file value and full
// proposed content, and blocks only bad.go.
const applyPatchFakeBin = `#!/usr/bin/env bash
printf 'ARGS=%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
file=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --file)
      shift
      file=$1
      printf 'FILE=%s\n' "$1" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
      ;;
    --content-file)
      shift
      cat "$1" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
      ;;
  esac
  shift
done
if [[ "$file" == "bad.go" ]]; then
  printf '{"status":"block","violations":[{"message":"too complex"}]}\n'
else
  printf '{"status":"pass"}\n'
fi
`

const outsideSecret = "OUTSIDE_SECRET_CONTENT"

func applyPatchPayload(t *testing.T, cwd string, toolInput any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"tool_name":  "apply_patch",
		"cwd":        cwd,
		"session_id": "s-1",
		"tool_input": toolInput,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return string(body)
}

func patchCommand(t *testing.T, cwd string, lines ...string) string {
	t.Helper()
	return applyPatchPayload(t, cwd, map[string]any{"command": strings.Join(lines, "\n")})
}

func optionalLog(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func runApplyPatch(t *testing.T, repo, payload string) (string, string, error) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "calm.log")
	output, err := runPreToolUse(t, repo, payload, fakeFitnessBin(t, applyPatchFakeBin), logPath, "")
	return string(output), optionalLog(t, logPath), err
}

func loggedFiles(log string) []string {
	var files []string
	for _, line := range strings.Split(log, "\n") {
		if name, ok := strings.CutPrefix(line, "FILE="); ok {
			files = append(files, name)
		}
	}
	return files
}

func TestApplyPatchSingleFileUpdateSendsReconstructedContent(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n\nfunc Message() string {\n\treturn \"old\"\n}\n")
	payload := patchCommand(t, repo,
		"*** Begin Patch",
		"*** Update File: sample.go",
		"@@ func Message() string {",
		"-\treturn \"old\"",
		"+\treturn \"new\"",
		"*** End Patch")

	output, log, err := runApplyPatch(t, repo, payload)
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	want := "FILE=sample.go\npackage sample\n\nfunc Message() string {\n\treturn \"new\"\n}\n"
	if !strings.Contains(log, want) {
		t.Fatalf("log = %q, want reconstructed full file %q", log, want)
	}
	for _, arg := range []string{"--file sample.go", "--language go", "--dry-run"} {
		if !strings.Contains(log, arg) {
			t.Fatalf("log = %q, want %s", log, arg)
		}
	}
}

func TestApplyPatchBlockingViolationBlocks(t *testing.T) {
	repo := initGitRepo(t)
	payload := patchCommand(t, repo,
		"*** Begin Patch",
		"*** Add File: bad.go",
		"+package bad",
		"*** End Patch")

	output, _, err := runApplyPatch(t, repo, payload)
	if exitCode(err) != 2 {
		t.Fatalf("exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(output, "calm_check:") || !strings.Contains(output, "blocking") {
		t.Fatalf("output = %s, want YAML calm_check block with blocking mode", output)
	}
}

func TestApplyPatchValidatesEveryTargetInMultiFilePatch(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.py"), "print('old')\n")
	writeFile(t, filepath.Join(repo, "gone.go"), "package gone\n")
	payload := patchCommand(t, repo,
		"*** Begin Patch",
		"*** Add File: ok.go",
		"+package ok",
		"*** Update File: sample.py",
		"@@",
		"-print('old')",
		"+print('new')",
		"*** Add File: README.md",
		"+# readme",
		"*** Add File: bad.go",
		"+package bad",
		"*** Delete File: gone.go",
		"*** End Patch")

	output, log, err := runApplyPatch(t, repo, payload)
	if exitCode(err) != 2 {
		t.Fatalf("exit = %v, want block; output=%s", err, output)
	}
	if got, want := strings.Join(loggedFiles(log), ","), "ok.go,sample.py,bad.go"; got != want {
		t.Fatalf("validated files = %s, want %s; log=%q", got, want, log)
	}
	if !strings.Contains(log, "print('new')\n") {
		t.Fatalf("log = %q, want updated python content", log)
	}
	if !strings.Contains(output, "unsupported file type: README.md") {
		t.Fatalf("output = %s, want README.md unsupported skip", output)
	}
}

func TestApplyPatchSequentialHunksOnSameFileCompose(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n\nfunc A() {}\n")
	payload := patchCommand(t, repo,
		"*** Begin Patch",
		"*** Update File: sample.go",
		"@@",
		" func A() {}",
		"+func B() {}",
		"*** Update File: sample.go",
		"@@",
		" func B() {}",
		"+func C() {}",
		"*** End Patch")

	output, log, err := runApplyPatch(t, repo, payload)
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if files := loggedFiles(log); len(files) != 1 || files[0] != "sample.go" {
		t.Fatalf("validated files = %v, want sample.go once", files)
	}
	want := "FILE=sample.go\npackage sample\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\n"
	if !strings.Contains(log, want) {
		t.Fatalf("log = %q, want composed content %q", log, want)
	}
}

func TestApplyPatchMoveValidatesDestination(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "old.go"), "package old\n\nfunc V() int {\n\treturn 1\n}\n")
	payload := patchCommand(t, repo,
		"*** Begin Patch",
		"*** Update File: old.go",
		"*** Move to: pkg/new.go",
		"@@",
		" func V() int {",
		"-\treturn 1",
		"+\treturn 2",
		" }",
		"*** End Patch")

	output, log, err := runApplyPatch(t, repo, payload)
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if files := loggedFiles(log); len(files) != 1 || files[0] != "pkg/new.go" {
		t.Fatalf("validated files = %v, want pkg/new.go only", files)
	}
	want := "FILE=pkg/new.go\npackage old\n\nfunc V() int {\n\treturn 2\n}\n"
	if !strings.Contains(log, want) {
		t.Fatalf("log = %q, want moved content %q", log, want)
	}
}

func TestApplyPatchAcceptsHeredocWrappedPatch(t *testing.T) {
	repo := initGitRepo(t)
	payload := applyPatchPayload(t, repo, map[string]any{
		"command": "<<'EOF'\n*** Begin Patch\n*** Add File: h.go\n+package h\n*** End Patch\nEOF\n",
	})

	output, log, err := runApplyPatch(t, repo, payload)
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(log, "FILE=h.go\npackage h\n") {
		t.Fatalf("log = %q, want h.go validated", log)
	}
}

func TestApplyPatchRejectsMalformedPatch(t *testing.T) {
	cases := map[string]func(repo string) string{
		"missing begin": func(repo string) string {
			return patchCommand(t, repo, "*** Add File: a.go", "+package a", "*** End Patch")
		},
		"missing end": func(repo string) string {
			return patchCommand(t, repo, "*** Begin Patch", "*** Add File: a.go", "+package a")
		},
		"empty update hunk": func(repo string) string {
			return patchCommand(t, repo, "*** Begin Patch", "*** Update File: sample.go", "*** End Patch")
		},
		"no command": func(repo string) string {
			return applyPatchPayload(t, repo, map[string]any{"patch": "*** Begin Patch\n*** End Patch"})
		},
		"context not found": func(repo string) string {
			return patchCommand(t, repo, "*** Begin Patch", "*** Update File: sample.go", "@@ func Missing() {", "-x", "+y", "*** End Patch")
		},
	}
	for name, payloadFor := range cases {
		t.Run(name, func(t *testing.T) {
			repo := initGitRepo(t)
			writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
			output, log, err := runApplyPatch(t, repo, payloadFor(repo))
			if exitCode(err) != 2 {
				t.Fatalf("exit = %v, want block; output=%s", err, output)
			}
			if !strings.Contains(output, "Invalid apply_patch payload:") || strings.Contains(output, "Traceback") {
				t.Fatalf("output = %s, want clean invalid apply_patch diagnostic", output)
			}
			if log != "" {
				t.Fatalf("client was called for a malformed patch: %q", log)
			}
		})
	}
}

func TestApplyPatchRejectsTargetsOutsideRepository(t *testing.T) {
	cases := map[string]func(t *testing.T, repo string) []string{
		"parent relative update": func(t *testing.T, repo string) []string {
			writeFile(t, filepath.Join(filepath.Dir(repo), "escape.go"), outsideSecret+"\n")
			return []string{"*** Update File: ../escape.go", "@@", "-" + outsideSecret, "+package escape"}
		},
		"absolute outside update": func(t *testing.T, repo string) []string {
			outside := filepath.Join(t.TempDir(), "abs.go")
			writeFile(t, outside, outsideSecret+"\n")
			return []string{"*** Update File: " + outside, "@@", "-" + outsideSecret, "+package abs"}
		},
		"parent relative delete": func(t *testing.T, repo string) []string {
			return []string{"*** Delete File: ../x.go"}
		},
		"move outside": func(t *testing.T, repo string) []string {
			writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
			return []string{"*** Update File: sample.go", "*** Move to: ../x.go", "@@", "-package sample", "+package x"}
		},
		"symlink outside": func(t *testing.T, repo string) []string {
			outside := filepath.Join(t.TempDir(), "target.go")
			writeFile(t, outside, outsideSecret+"\n")
			if err := os.Symlink(outside, filepath.Join(repo, "link.go")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
			return []string{"*** Update File: link.go", "@@", "-" + outsideSecret, "+package link"}
		},
	}
	for name, escapingHunk := range cases {
		t.Run(name, func(t *testing.T) {
			repo := initGitRepo(t)
			lines := append([]string{"*** Begin Patch", "*** Add File: ok.go", "+package ok"}, escapingHunk(t, repo)...)
			payload := patchCommand(t, repo, append(lines, "*** End Patch")...)
			output, log, err := runApplyPatch(t, repo, payload)
			if exitCode(err) != 2 {
				t.Fatalf("exit = %v, want block; output=%s", err, output)
			}
			if !strings.Contains(output, "target resolves outside repository") {
				t.Fatalf("output = %s, want containment rejection", output)
			}
			if strings.Contains(output, outsideSecret) {
				t.Fatalf("output leaked outside file content: %s", output)
			}
			if log != "" {
				t.Fatalf("client was called for an escaping patch: %q", log)
			}
		})
	}
}

func TestApplyPatchInRepoParentTraversalIsAllowed(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	payload := patchCommand(t, sub,
		"*** Begin Patch",
		"*** Update File: ../sample.go",
		"@@",
		" package sample",
		"+func A() {}",
		"*** End Patch")

	output, log, err := runApplyPatch(t, repo, payload)
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if files := loggedFiles(log); len(files) != 1 || files[0] != "sample.go" {
		t.Fatalf("validated files = %v, want sample.go", files)
	}
}

func TestApplyPatchRecordsHistoryAction(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID", "")
	repo := initGitRepo(t)
	log := filepath.Join(t.TempDir(), "calls")
	payload := patchCommand(t, repo, "*** Begin Patch", "*** Add File: h.go", "+package h", "*** End Patch")
	if output, err := runPreToolUse(t, repo, payload, fakeFitnessBin(t, historyMetadataBin), log, ""); err != nil {
		t.Fatalf("hook: %v: %s", err, output)
	}
	metadata := readHistoryMetadata(t, log)
	for key, want := range map[string]string{"ACTION": "apply_patch", "SESSION_ID": "s-1", "content": "package h\n"} {
		if metadata[key] != want {
			t.Errorf("%s = %v, want %q", key, metadata[key], want)
		}
	}
	assertHistoryArgs(t, metadata, "--dry-run")
}

func TestApplyPatchMissingHelperFailsClosed(t *testing.T) {
	repo := initGitRepo(t)
	script := filepath.Join(t.TempDir(), "pre-tool-use.sh")
	writeFile(t, script, readFile(t, hookScriptPathFor(t, "pre-tool-use.sh")))
	logPath := filepath.Join(t.TempDir(), "calm.log")
	binDir := fakeFitnessBin(t, applyPatchFakeBin)
	command := exec.Command("bash", script)
	command.Dir = repo
	command.Stdin = strings.NewReader(patchCommand(t, repo, "*** Begin Patch", "*** Add File: h.go", "+package h", "*** End Patch"))
	command.Env = append(os.Environ(),
		"AGENT_FITNESS_FUNCTIONS_BIN="+filepath.Join(binDir, "agent-fitness-functions"),
		"AGENT_FITNESS_FUNCTIONS_LOG="+logPath)

	output, err := command.CombinedOutput()
	if exitCode(err) != 2 {
		t.Fatalf("exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "apply_patch support is not installed") {
		t.Fatalf("output = %s, want missing helper diagnostic", output)
	}
	if log := optionalLog(t, logPath); log != "" {
		t.Fatalf("client was called without the helper: %q", log)
	}
}

func TestApplyPatchAgainstRunningDaemonKnownBadAndGood(t *testing.T) {
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
	fitnessBin := buildFitnessBin(t)
	daemon := startFitnessDaemon(t, fitnessBin)
	t.Setenv("AGENT_FITNESS_FUNCTIONS_REPO_NAME", "repo-one")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CERT", daemon.clientCertPath)
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_KEY", daemon.clientKeyPath)
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CA", daemon.serverCAPath)

	addPatch := func(body ...string) string {
		lines := []string{"*** Begin Patch", "*** Add File: sample.go"}
		for _, line := range body {
			lines = append(lines, "+"+line)
		}
		return patchCommand(t, repo, append(lines, "*** End Patch")...)
	}
	bad := addPatch("package sample", "func Score(kind string, retries int, urgent bool) int {", "score := 0",
		`if kind == "create" { score++ }`, `if kind == "update" { score++ }`, `if kind == "delete" { score++ }`,
		`if kind == "manual" { score++ }`, `if kind == "batch" { score++ }`, `if kind == "sync" { score++ }`,
		"if retries > 0 { score++ }", "if retries > 1 { score++ }", "if retries > 2 { score++ }",
		"if urgent { score++ }", "return score", "}")
	output, err := runPreToolUseWithBin(t, repo, bad, fitnessBin, "", "", daemon.url)
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want running daemon block; output=%s", output)
	}
	if !strings.Contains(string(output), "cyclomatic-complexity") {
		t.Fatalf("output = %s, want YAML cyclomatic-complexity violation", output)
	}

	good := addPatch("package sample", "func Score(kind string, retries int, urgent bool) int {",
		`score := map[string]int{"create": 1, "update": 1, "delete": 1, "manual": 1, "batch": 1, "sync": 1}[kind]`,
		"if urgent { score++ }", "if retries > 0 { score += min(retries, 3) }", "return score", "}")
	output, err = runPreToolUseWithBin(t, repo, good, fitnessBin, "", "", daemon.url)
	if err != nil {
		t.Fatalf("pre-tool-use failed for known-good content: %v\n%s", err, output)
	}
}
