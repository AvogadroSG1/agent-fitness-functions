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
	foundPi := false
	foundOMP := false

	for _, res := range results {
		switch res.name {
		case "codex PreToolUse hooks", "codex PreToolUse hooks (optional)":
			foundCodex = true
			if !res.passed {
				t.Errorf("codex hook check failed: detail=%s", res.detail)
			}
		case "opencode plugin", "opencode plugin (optional)":
			foundOpenCode = true
			if !res.passed {
				t.Errorf("opencode plugin check failed: detail=%s", res.detail)
			}
		case "pi extension (optional)":
			foundPi = true
			if !res.passed {
				t.Errorf("Pi extension check failed: detail=%s", res.detail)
			}
		case "omp extension (optional)":
			foundOMP = true
			if !res.passed {
				t.Errorf("OMP extension check failed: detail=%s", res.detail)
			}
		}
	}

	if !foundCodex {
		t.Errorf("checkHooksInstalled did not return a codex hook check result")
	}
	if !foundOpenCode {
		t.Errorf("checkHooksInstalled did not return an opencode plugin check result")
	}
	if !foundPi {
		t.Errorf("checkHooksInstalled did not return a Pi extension check result")
	}
	if !foundOMP {
		t.Errorf("checkHooksInstalled did not return an OMP extension check result")
	}
}

func TestDoctorDegradedMultiAgentHooks(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	cfg := doctorConfig{
		repoRoot: repo,
		repo:     "testrepo",
	}

	// Without running install-hooks, optional agent integrations should return warnings.
	results := checkHooksInstalled(cfg)
	for _, res := range results {
		switch res.name {
		case "codex PreToolUse hooks (optional)", "opencode plugin (optional)", "pi extension (optional)", "omp extension (optional)":
			if !res.warning || res.passed {
				t.Errorf("unconfigured %s should be a warning, got passed=%v warning=%v", res.name, res.passed, res.warning)
			}
		}
	}
}
