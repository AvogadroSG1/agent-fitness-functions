package client

// Red contract for calm-poc-bzch: doctor told the user to "fix the JSON in
// configs/<repo>/config.json" for any invalid config. When the failure is a
// governance invariant rather than a syntax error — the observatory case —
// that sends people hunting for a JSON error that does not exist.
//
// doctor MUST report the reason the server gave, and MUST NOT claim the JSON
// is at fault when it parses cleanly.

import (
	"strings"
	"testing"
)

const layersMissingReason = "layer-sovereignty is enabled but fitness-function-settings.layer-sovereignty.layers is empty"

func TestRepoConfiguredResultReportsTheServersReason(t *testing.T) {
	cfg := doctorConfig{repo: "observatory"}
	result := repoConfiguredResult(cfg, preflightReport{
		RepoConfigured:  true,
		RepoConfigValid: false,
		RepoConfigError: layersMissingReason,
	})

	if result.passed {
		t.Fatal("invalid-config result passed = true, want false")
	}
	if !strings.Contains(result.detail, "layer-sovereignty") {
		t.Fatalf("detail = %q, want the server's reason surfaced", result.detail)
	}
	if strings.Contains(result.remediation, "fix the JSON") {
		t.Fatalf("remediation = %q, want no claim that the JSON is malformed — it parses cleanly", result.remediation)
	}
}

func TestRepoConfiguredResultStillGuidesWhenServerGaveNoReason(t *testing.T) {
	cfg := doctorConfig{repo: "observatory"}
	result := repoConfiguredResult(cfg, preflightReport{RepoConfigured: true, RepoConfigValid: false})

	if result.passed {
		t.Fatal("invalid-config result passed = true, want false")
	}
	if !strings.Contains(result.remediation, "configs/observatory/config.json") {
		t.Fatalf("remediation = %q, want it to name the config path", result.remediation)
	}
}
