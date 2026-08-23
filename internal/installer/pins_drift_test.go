package installer

import (
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
