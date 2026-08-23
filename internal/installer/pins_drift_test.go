package installer

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// calm-poc-phk.2 slice 11 (ADR-0005): the Dockerfile, the committed
// lockfiles, and the installer's pin manifest are three provisioning
// sources for the same tools; any divergence must fail the suite.
func TestManagedToolPinsAgreeAcrossProvisioningSources(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	if !strings.Contains(string(dockerfile), "@finos/calm-cli@"+CALMCLIVersion) {
		t.Errorf("Dockerfile CALM pin disagrees with installer pin %q", CALMCLIVersion)
	}
	if !strings.Contains(string(dockerfile), "dotnet/sdk:"+DotnetSDKVersion) {
		t.Errorf("Dockerfile .NET SDK pin disagrees with installer pin %q", DotnetSDKVersion)
	}

	lock, err := os.ReadFile("../../requirements.lock")
	if err != nil {
		t.Fatalf("read requirements.lock: %v", err)
	}
	if !strings.Contains(string(lock), "radon=="+RadonVersion) {
		t.Errorf("requirements.lock radon pin disagrees with installer pin %q", RadonVersion)
	}

	npmLock, err := os.ReadFile("../../tools/calm-runtime/package-lock.json")
	if err != nil {
		t.Fatalf("read calm-runtime package-lock.json: %v", err)
	}
	if !strings.Contains(string(npmLock), "\"@finos/calm-cli\": \""+CALMCLIVersion+"\"") &&
		!strings.Contains(string(npmLock), "calm-cli/-/calm-cli-"+CALMCLIVersion+".tgz") {
		t.Errorf("calm-runtime lockfile disagrees with installer pin %q", CALMCLIVersion)
	}
}

// TestManagedToolPinsHaveNoInstallScripts guards provisionCalmRuntime's
// (runtime_calm.go) `npm ci --ignore-scripts` justification: --ignore-scripts
// is safe today only because the committed lockfile pins zero packages
// declaring an install script. If a future dependency bump introduces one,
// this test fails the suite so that change gets deliberate review rather than
// silently having its install script skipped (or --ignore-scripts silently
// removed without noticing why it was there).
func TestManagedToolPinsHaveNoInstallScripts(t *testing.T) {
	npmLock, err := os.ReadFile("../../tools/calm-runtime/package-lock.json")
	if err != nil {
		t.Fatalf("read calm-runtime package-lock.json: %v", err)
	}
	var lockFile struct {
		Packages map[string]struct {
			HasInstallScript bool `json:"hasInstallScript"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(npmLock, &lockFile); err != nil {
		t.Fatalf("parse calm-runtime package-lock.json: %v", err)
	}
	for name, pkg := range lockFile.Packages {
		if pkg.HasInstallScript {
			t.Errorf("package %q declares hasInstallScript=true; provisionCalmRuntime's --ignore-scripts assumption (runtime_calm.go) no longer holds and needs review", name)
		}
	}
}
