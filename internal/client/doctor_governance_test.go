package client

// Red contract for calm-poc-mx5.5 (ADR-0007): doctor reports the machine
// governance root and flags legacy per-repo layouts. Legacy findings are
// advisory (⚠) — they never fail the run — while a missing governance root is
// a hard failure whose remediation is `client onboard`.

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

// governanceDoctorConfig is a minimal offline-ish doctor config: HTTP checks
// run against a stub that always answers 200 so filesystem checks stay the
// focus.
func governanceDoctorConfig(repoRoot, repoName string) doctorConfig {
	return doctorConfig{
		addr:     "https://127.0.0.1:7890",
		repo:     repoName,
		repoRoot: repoRoot,
		httpClient: &http.Client{Transport: clientRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		})},
	}
}

// findCheck returns the first check whose name contains fragment.
func findCheck(results []checkResult, fragment string) (checkResult, bool) {
	for _, result := range results {
		if strings.Contains(result.name, fragment) {
			return result, true
		}
	}
	return checkResult{}, false
}

// healthyGovernanceRoot publishes certs and registers repoName under the
// governance root, returning the root path.
func healthyGovernanceRoot(t *testing.T, repoName string) string {
	t.Helper()
	govRoot := governanceStateHome(t)
	if err := os.MkdirAll(govRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := devcerts.Publish(filepath.Join(govRoot, "certs"), false); err != nil {
		t.Fatal(err)
	}
	sharedConfig := filepath.Join(govRoot, "configs", repoName, "config.json")
	if err := os.MkdirAll(filepath.Dir(sharedConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharedConfig, []byte("{\n  \"enforcement-mode\": \"advisory\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(govRoot, "caller-repos.json"), []byte(`{"callers":{"dev-hook-pool":["`+repoName+`"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return govRoot
}

func TestDoctorReportsHealthyGovernanceRoot(t *testing.T) {
	govRoot := healthyGovernanceRoot(t, "sample")
	repoRoot := t.TempDir()

	results := runDoctorChecks(governanceDoctorConfig(repoRoot, "sample"))

	check, found := findCheck(results, "governance root")
	if !found {
		t.Fatalf("no governance-root check in doctor output; ADR-0007 requires doctor to report the machine root")
	}
	if !check.passed {
		t.Fatalf("governance-root check = %+v, want pass for a healthy root", check)
	}
	if !strings.Contains(check.detail, govRoot) {
		t.Fatalf("governance-root detail = %q, want it to name %q", check.detail, govRoot)
	}
}

func TestDoctorFailsGovernanceRootMissingWithOnboardRemediation(t *testing.T) {
	governanceStateHome(t) // fresh machine: no governance root published

	results := runDoctorChecks(governanceDoctorConfig(t.TempDir(), "sample"))

	check, found := findCheck(results, "governance root")
	if !found {
		t.Fatal("no governance-root check in doctor output")
	}
	if check.passed || check.warning {
		t.Fatalf("governance-root check on a never-onboarded machine = %+v, want hard failure", check)
	}
	if !strings.Contains(check.remediation, "client onboard") {
		t.Fatalf("remediation = %q, want `client onboard`", check.remediation)
	}
}

func TestDoctorWarnsOnLegacyRepoLocalCerts(t *testing.T) {
	healthyGovernanceRoot(t, "sample")
	repoRoot := t.TempDir()
	legacyVersion := filepath.Join(repoRoot, "certs", "versions", "v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(legacyVersion, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacyVersion, filepath.Join(repoRoot, "certs", "current")); err != nil {
		t.Fatal(err)
	}

	results := runDoctorChecks(governanceDoctorConfig(repoRoot, "sample"))

	check, found := findCheck(results, "legacy")
	if !found {
		t.Fatalf("no legacy-layout check despite <repo>/certs/current present; doctor must flag pre-ADR-0007 dead weight")
	}
	if !check.warning || check.passed {
		t.Fatalf("legacy certs check = %+v, want advisory warning (never a hard failure)", check)
	}
	if !strings.Contains(check.detail+check.remediation, "client onboard") {
		t.Fatalf("legacy certs check %+v, want re-run `client onboard` guidance", check)
	}
}

func TestDoctorWarnsOnRepoConfigDriftFromSharedCopy(t *testing.T) {
	govRoot := healthyGovernanceRoot(t, "sample")
	repoRoot := t.TempDir()
	localConfig := filepath.Join(repoRoot, "configs", "sample", "config.json")
	if err := os.MkdirAll(filepath.Dir(localConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localConfig, []byte("{\n  \"enforcement-mode\": \"block\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = govRoot // shared copy holds advisory content, differing from repo-local block

	results := runDoctorChecks(governanceDoctorConfig(repoRoot, "sample"))

	check, found := findCheck(results, "config sync")
	if !found {
		t.Fatal("no config-sync check despite repo-local config differing from the shared copy")
	}
	if !check.warning || check.passed {
		t.Fatalf("config drift check = %+v, want advisory warning", check)
	}
	if !strings.Contains(check.detail+check.remediation, "client onboard") {
		t.Fatalf("config drift check %+v, want re-run `client onboard` guidance", check)
	}
}

func TestDoctorWarnsWhenRepoConfigNotRegisteredWithSharedRoot(t *testing.T) {
	govRoot := healthyGovernanceRoot(t, "other-repo") // root healthy, but "sample" unregistered
	repoRoot := t.TempDir()
	localConfig := filepath.Join(repoRoot, "configs", "sample", "config.json")
	if err := os.MkdirAll(filepath.Dir(localConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localConfig, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = govRoot

	results := runDoctorChecks(governanceDoctorConfig(repoRoot, "sample"))

	check, found := findCheck(results, "config sync")
	if !found {
		t.Fatal("no config-sync check despite repo-local config missing from the shared root")
	}
	if !check.warning || check.passed {
		t.Fatalf("unregistered-repo check = %+v, want advisory warning naming client onboard", check)
	}
	if !strings.Contains(check.detail+check.remediation, "client onboard") {
		t.Fatalf("unregistered-repo check %+v, want `client onboard` guidance", check)
	}
}
