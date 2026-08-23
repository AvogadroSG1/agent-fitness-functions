package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// infraErrorBin is a fake client that emulates `client validate` on an infrastructure
// failure: it prints the machine-readable {"status":"error",...} object and exits with
// the reserved infra exit code (3).
const infraErrorBin = `#!/usr/bin/env bash
printf '%s\n' '{"status":"error","error_kind":"not_configured","message":"repository sample is not configured on the governance server (HTTP 404)","remediation":"run agent-fitness-functions client onboard, or create configs/sample/config.json on the server"}'
exit 3
`

func runPreCommitInfra(t *testing.T, onErrorMode string) ([]byte, error) {
	t.Helper()
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")
	fakeBin := fakeFitnessBin(t, infraErrorBin)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = repo
	env := append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if onErrorMode != "" {
		env = append(env, "AGENT_FITNESS_FUNCTIONS_ON_ERROR="+onErrorMode)
	}
	command.Env = env
	return command.CombinedOutput()
}

func TestPreCommitBlocksInfraErrorByDefaultWithRemediation(t *testing.T) {
	output, err := runPreCommitInfra(t, "")
	if err == nil {
		t.Fatalf("pre-commit succeeded, want fail-closed on infra error; output=%s", output)
	}
	text := string(output)
	for _, want := range []string{"SETUP problem", "NOT an architecture violation", "not_configured", "client onboard", "configs/sample/config.json"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	// The generic "check failed" line must NOT appear — the cause is now specific.
	if strings.Contains(text, "check failed for") {
		t.Fatalf("output shows generic check-failed line instead of the specific cause:\n%s", text)
	}
}

func TestPreCommitInfraErrorAdvisoryDoesNotBlock(t *testing.T) {
	output, err := runPreCommitInfra(t, "advisory")
	if err != nil {
		t.Fatalf("pre-commit blocked in advisory on-error mode: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "SETUP problem") || !strings.Contains(text, "not_configured") {
		t.Fatalf("advisory output missing setup diagnostic:\n%s", text)
	}
	if !strings.Contains(text, "advisory") {
		t.Fatalf("advisory output missing the advisory note:\n%s", text)
	}
}

func TestPreToolUseBlocksInfraErrorByDefaultWithRemediation(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	fakeBin := fakeFitnessBin(t, infraErrorBin)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Run() {}\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want 2 (blocking infra error); output=%s", err, output)
	}
	text := string(output)
	for _, want := range []string{"SETUP problem", "not_configured", "client onboard"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestPreToolUseInfraErrorAdvisoryAllowsEdit(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	fakeBin := fakeFitnessBin(t, infraErrorBin)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Run() {}\n"}}`

	command := exec.Command("bash", hookScriptPathFor(t, "pre-tool-use.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader(payload)
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-tool-use blocked in advisory on-error mode: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "SETUP problem") || !strings.Contains(text, "allowing this edit") {
		t.Fatalf("advisory output missing setup diagnostic or allow note:\n%s", text)
	}
}
