package client

import (
	"bytes"
	"testing"
)

func TestDoctorChecksMultiAgentHooks(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}

	cfg := doctorConfig{
		repoRoot: repo,
		repo:     "testrepo",
	}

	results := checkHooksInstalled(cfg)

	foundCodex := false
	foundOpenCode := false

	for _, res := range results {
		if res.name == "codex PreToolUse hooks" || res.name == "codex PreToolUse hooks (optional)" {
			foundCodex = true
			if !res.passed {
				t.Errorf("codex hook check failed: detail=%s", res.detail)
			}
		}
		if res.name == "opencode plugin" || res.name == "opencode plugin (optional)" {
			foundOpenCode = true
			if !res.passed {
				t.Errorf("opencode plugin check failed: detail=%s", res.detail)
			}
		}
	}

	if !foundCodex {
		t.Errorf("checkHooksInstalled did not return a codex hook check result")
	}
	if !foundOpenCode {
		t.Errorf("checkHooksInstalled did not return an opencode plugin check result")
	}
}

func TestDoctorDegradedMultiAgentHooks(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	cfg := doctorConfig{
		repoRoot: repo,
		repo:     "testrepo",
	}

	// Without running install-hooks, both codex and opencode should return warning=true
	results := checkHooksInstalled(cfg)
	for _, res := range results {
		if res.name == "codex PreToolUse hooks (optional)" {
			if !res.warning || res.passed {
				t.Errorf("unconfigured codex should be a warning, got passed=%v warning=%v", res.passed, res.warning)
			}
		}
		if res.name == "opencode plugin (optional)" {
			if !res.warning || res.passed {
				t.Errorf("unconfigured opencode should be a warning, got passed=%v warning=%v", res.passed, res.warning)
			}
		}
	}
}
