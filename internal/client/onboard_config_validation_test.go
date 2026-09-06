package client

// Red contract for calm-poc-hfr2: syncSharedConfig copies the repo-local
// config into the machine governance root byte-for-byte without ever parsing
// it. A config that is valid JSON but breaks a governance invariant therefore
// registers cleanly and only surfaces later as an opaque HTTP 503 at commit
// time — which is exactly how the observatory repo failed.
//
// onboard MUST reject such a config, name the invariant, and copy nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layerSovereigntyWithoutLayers is valid JSON that breaks a governance
// invariant: the function is enabled but no layers are configured for it.
const layerSovereigntyWithoutLayers = `{
  "enforcement-mode": "block",
  "fitness-functions": {
    "cyclomatic-complexity": true,
    "layer-sovereignty": true
  }
}
`

func TestOnboardRejectsSemanticallyInvalidRepoLocalConfig(t *testing.T) {
	govRoot := governanceStateHome(t)
	repoRoot := onboardTestRepo(t)

	localPath := filepath.Join(repoRoot, "configs", "repo-invalid", "config.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte(layerSovereigntyWithoutLayers), 0o644); err != nil {
		t.Fatal(err)
	}

	o := resolveOnboarderForTest(t, "repo-invalid", repoRoot)
	if err := o.ensureCerts(); err != nil {
		t.Fatalf("ensureCerts: %v", err)
	}

	err := o.scaffoldConfig()
	if err == nil {
		t.Fatal("scaffoldConfig() = nil, want rejection of a config that enables layer-sovereignty with no layers")
	}
	if !strings.Contains(err.Error(), "layer-sovereignty") {
		t.Fatalf("scaffoldConfig() error = %q, want it to name the invariant that failed (layer-sovereignty)", err)
	}

	shared := filepath.Join(govRoot, "configs", "repo-invalid", "config.json")
	if _, statErr := os.Stat(shared); statErr == nil {
		t.Fatal("invalid config was copied into the governance root; onboard must copy nothing when validation fails")
	}
}

func TestOnboardStillSyncsAValidRepoLocalConfig(t *testing.T) {
	govRoot := governanceStateHome(t)
	repoRoot := onboardTestRepo(t)

	runManagedOnboardCore(t, "repo-valid", repoRoot)

	if _, err := os.Stat(filepath.Join(govRoot, "configs", "repo-valid", "config.json")); err != nil {
		t.Fatalf("shared config for a valid onboard: %v, want it registered", err)
	}
}
