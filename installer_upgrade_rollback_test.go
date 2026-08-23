package calm_poc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// calm-poc-phk.2 slice 3 (ADR-0005): `upgrade` installs a new version through
// the same verified atomic path install.sh uses, retaining exactly one
// predecessor; `rollback` repoints current at that retained predecessor and
// fails cleanly when none exists.

// buildPackage packages the current source tree into outputDir with the given
// --version override so two distinguishable "releases" can be produced from
// one worktree, and returns the archive and checksums paths.
func buildPackage(t *testing.T, outputDir, version string) (archivePath, checksumsPath string) {
	t.Helper()
	output, err := runInstallerCommand(t, nil, "bash", filepath.Join("scripts", "package-release.sh"), "--output", outputDir, "--version", version)
	if err != nil {
		t.Fatalf("package-release.sh --version %s failed: %v\n%s", version, err, output)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), version+"-darwin-arm64.tar.gz") {
			archivePath = filepath.Join(outputDir, entry.Name())
		}
	}
	if archivePath == "" {
		t.Fatalf("no archive produced for version %s in %v", version, entries)
	}
	checksumsPath = filepath.Join(outputDir, "SHA256SUMS")
	return archivePath, checksumsPath
}

func TestUpgradeIsIdempotentAndRetainsExactlyOnePredecessor(t *testing.T) {
	packageDirV1 := t.TempDir()
	archiveV1, checksumsV1 := buildPackage(t, packageDirV1, "v1.0.0-test")

	stateHome := t.TempDir()
	env := []string{"XDG_STATE_HOME=" + stateHome}

	if output, err := runInstallerCommand(t, env, "bash", filepath.Join("scripts", "install.sh"), "--archive", archiveV1, "--checksums", checksumsV1); err != nil {
		t.Fatalf("install.sh failed: %v\n%s", output, err)
	}
	stateRoot := filepath.Join(stateHome, "agent-fitness-functions")
	binary := filepath.Join(stateRoot, "current", "bin", "agent-fitness-functions")

	// Re-running upgrade with the already-current archive must be a no-op.
	beforeTree := snapshotInstallerTree(t, stateRoot)
	if output, err := runInstallerCommand(t, env, binary, "upgrade", "--archive", archiveV1, "--checksums", checksumsV1); err != nil {
		t.Fatalf("upgrade (no-op case) failed: %v\n%s", err, output)
	}
	if afterTree := snapshotInstallerTree(t, stateRoot); beforeTree != afterTree {
		t.Fatalf("idempotent upgrade changed state:\nbefore:\n%s\nafter:\n%s", beforeTree, afterTree)
	}

	// Upgrade to a new version: exactly one predecessor (v1) must be retained.
	packageDirV2 := t.TempDir()
	archiveV2, checksumsV2 := buildPackage(t, packageDirV2, "v2.0.0-test")
	if output, err := runInstallerCommand(t, env, binary, "upgrade", "--archive", archiveV2, "--checksums", checksumsV2); err != nil {
		t.Fatalf("upgrade to v2 failed: %v\n%s", err, output)
	}
	versions, err := os.ReadDir(filepath.Join(stateRoot, "versions"))
	if err != nil {
		t.Fatalf("read versions dir: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions after upgrade = %v, want exactly 2 (new current + one retained predecessor)", versions)
	}
	target, err := os.Readlink(filepath.Join(stateRoot, "current"))
	if err != nil || !strings.Contains(target, "v2.0.0-test") {
		t.Fatalf("current -> %q (err %v), want it to point at the v2.0.0-test version", target, err)
	}

	// A tampered upgrade archive must abort without touching current or any
	// existing version directory.
	beforeTree = snapshotInstallerTree(t, stateRoot)
	packageDirV3 := t.TempDir()
	archiveV3, checksumsV3 := buildPackage(t, packageDirV3, "v3.0.0-test")
	tampered := filepath.Join(t.TempDir(), filepath.Base(archiveV3))
	original, err := os.ReadFile(archiveV3)
	if err != nil {
		t.Fatalf("read v3 archive: %v", err)
	}
	if err := os.WriteFile(tampered, append(original, 0x00), 0o644); err != nil {
		t.Fatalf("write tampered v3 archive: %v", err)
	}
	if output, err := runInstallerCommand(t, env, binary, "upgrade", "--archive", tampered, "--checksums", checksumsV3); err == nil {
		t.Fatalf("upgrade accepted a tampered archive:\n%s", output)
	}
	if afterTree := snapshotInstallerTree(t, stateRoot); beforeTree != afterTree {
		t.Fatal("failed upgrade of a tampered archive mutated the state root")
	}
}

func TestRollbackRepointsToRetainedPredecessorAndFailsCleanlyWithoutOne(t *testing.T) {
	packageDirV1 := t.TempDir()
	archiveV1, checksumsV1 := buildPackage(t, packageDirV1, "r1.0.0-test")

	stateHome := t.TempDir()
	env := []string{"XDG_STATE_HOME=" + stateHome}
	if output, err := runInstallerCommand(t, env, "bash", filepath.Join("scripts", "install.sh"), "--archive", archiveV1, "--checksums", checksumsV1); err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}
	stateRoot := filepath.Join(stateHome, "agent-fitness-functions")
	binary := filepath.Join(stateRoot, "current", "bin", "agent-fitness-functions")

	// No predecessor yet: rollback must fail cleanly without mutating state.
	beforeTree := snapshotInstallerTree(t, stateRoot)
	if output, err := runInstallerCommand(t, env, binary, "rollback"); err == nil {
		t.Fatalf("rollback succeeded with no predecessor retained:\n%s", output)
	}
	if afterTree := snapshotInstallerTree(t, stateRoot); beforeTree != afterTree {
		t.Fatal("failed rollback (no predecessor) mutated the state root")
	}

	// Upgrade to a second version, then roll back to the first.
	packageDirV2 := t.TempDir()
	archiveV2, checksumsV2 := buildPackage(t, packageDirV2, "r2.0.0-test")
	if output, err := runInstallerCommand(t, env, binary, "upgrade", "--archive", archiveV2, "--checksums", checksumsV2); err != nil {
		t.Fatalf("upgrade to r2 failed: %v\n%s", err, output)
	}
	if output, err := runInstallerCommand(t, env, binary, "rollback"); err != nil {
		t.Fatalf("rollback failed: %v\n%s", err, output)
	}
	target, err := os.Readlink(filepath.Join(stateRoot, "current"))
	if err != nil || !strings.Contains(target, "r1.0.0-test") {
		t.Fatalf("current -> %q (err %v), want it repointed at r1.0.0-test after rollback", target, err)
	}

	// After the binary at "current" changed (rollback repointed it to r1's
	// binary), that binary must still be runnable.
	binaryAfterRollback := filepath.Join(stateRoot, "current", "bin", "agent-fitness-functions")
	if output, err := runInstallerCommand(t, nil, binaryAfterRollback, "--help"); err != nil {
		t.Fatalf("binary at rolled-back current is not runnable: %v\n%s", err, output)
	}
}
