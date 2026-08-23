package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// buildFakeToolVersion creates root/runtimes/<tool>/versions/<version>/.verified,
// mirroring a provisioned-and-verified managed runtime component.
func buildFakeToolVersion(t *testing.T, root, tool, version string) string {
	t.Helper()
	dir := runtimeVersionDir(root, tool, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, verifiedSentinelName), nil, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	return dir
}

// writePinsManifest writes restoredVersionDir/share/pins.json, the per-
// release manifest scripts/package-release.sh produces.
func writePinsManifest(t *testing.T, restoredVersionDir string, pins map[string]string) {
	t.Helper()
	shareDir := filepath.Join(restoredVersionDir, "share")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", shareDir, err)
	}
	body := "{"
	first := true
	for tool, version := range pins {
		if !first {
			body += ","
		}
		first = false
		body += `"` + tool + `":"` + version + `"`
	}
	body += "}"
	if err := os.WriteFile(filepath.Join(shareDir, "pins.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write pins.json: %v", err)
	}
}

func TestReconcileRuntimePointersIsANoOpWhenAlreadyCorrect(t *testing.T) {
	root := t.TempDir()
	calmDir := buildFakeToolVersion(t, root, "calm", CALMCLIVersion)
	if err := publishCurrent(runtimeRoot(root, "calm"), calmDir); err != nil {
		t.Fatalf("seed calm current: %v", err)
	}
	pythonDir := buildFakeToolVersion(t, root, "python", RadonVersion)
	if err := publishCurrent(runtimeRoot(root, "python"), pythonDir); err != nil {
		t.Fatalf("seed python current: %v", err)
	}

	restoredVersionDir := filepath.Join(root, "versions", "1.0.0")
	writePinsManifest(t, restoredVersionDir, map[string]string{
		"calm":   CALMCLIVersion,
		"python": RadonVersion,
	})

	if err := reconcileRuntimePointers(root, restoredVersionDir); err != nil {
		t.Fatalf("reconcileRuntimePointers: %v", err)
	}

	gotCalm, err := readCurrentVersion(runtimeRoot(root, "calm"))
	if err != nil || gotCalm != CALMCLIVersion {
		t.Fatalf("calm current = %q (%v), want %q unchanged", gotCalm, err, CALMCLIVersion)
	}
	gotPython, err := readCurrentVersion(runtimeRoot(root, "python"))
	if err != nil || gotPython != RadonVersion {
		t.Fatalf("python current = %q (%v), want %q unchanged", gotPython, err, RadonVersion)
	}
}

func TestReconcileRuntimePointersRepointsAStaleCurrent(t *testing.T) {
	root := t.TempDir()
	// Two provisioned calm versions; current stale-points at a "newer" one
	// while the restored release's manifest pins the older one.
	staleDir := buildFakeToolVersion(t, root, "calm", "1.41.0")
	restoredPinDir := buildFakeToolVersion(t, root, "calm", CALMCLIVersion)
	if err := publishCurrent(runtimeRoot(root, "calm"), staleDir); err != nil {
		t.Fatalf("seed stale calm current: %v", err)
	}

	restoredVersionDir := filepath.Join(root, "versions", "1.0.0")
	writePinsManifest(t, restoredVersionDir, map[string]string{"calm": CALMCLIVersion})

	if err := reconcileRuntimePointers(root, restoredVersionDir); err != nil {
		t.Fatalf("reconcileRuntimePointers: %v", err)
	}

	got, err := readCurrentVersion(runtimeRoot(root, "calm"))
	if err != nil || got != CALMCLIVersion {
		t.Fatalf("calm current = %q (%v), want reconciled to %q", got, err, CALMCLIVersion)
	}
	_ = restoredPinDir
}

func TestReconcileRuntimePointersSkipsUnprovisionedPin(t *testing.T) {
	root := t.TempDir()
	restoredVersionDir := filepath.Join(root, "versions", "1.0.0")
	// Pin names a version that was never locally provisioned.
	writePinsManifest(t, restoredVersionDir, map[string]string{"calm": "9.9.9"})

	if err := reconcileRuntimePointers(root, restoredVersionDir); err != nil {
		t.Fatalf("reconcileRuntimePointers: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(runtimeRoot(root, "calm"), "current")); !os.IsNotExist(err) {
		t.Fatalf("reconcile created a current pointer for an unprovisioned pin (err=%v)", err)
	}
}

func TestReconcileRuntimePointersNoOpWithoutPinsManifest(t *testing.T) {
	root := t.TempDir()
	restoredVersionDir := filepath.Join(root, "versions", "1.0.0")
	if err := os.MkdirAll(restoredVersionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := reconcileRuntimePointers(root, restoredVersionDir); err != nil {
		t.Fatalf("reconcileRuntimePointers without a pins manifest should be a no-op, got: %v", err)
	}
}
