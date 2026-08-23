package calm_poc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// calm-poc-phk.2 slices 1-3 (ADR-0005): packaging emits a checksummed
// darwin-arm64 archive; install.sh bootstraps an isolated XDG state root
// through an atomic versioned publication; upgrade is an idempotent no-op
// when current; rollback repoints to the retained predecessor; uninstall
// removes only product-owned state.

func runInstallerCommand(t *testing.T, env []string, name string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestPackageReleaseProducesChecksummedArchive(t *testing.T) {
	if _, err := os.Stat(filepath.Join("scripts", "package-release.sh")); err != nil {
		t.Fatalf("packaging entrypoint scripts/package-release.sh missing: %v", err)
	}
	outputDir := t.TempDir()
	output, err := runInstallerCommand(t, nil, "bash", filepath.Join("scripts", "package-release.sh"), "--output", outputDir)
	if err != nil {
		t.Fatalf("package-release.sh failed: %v\n%s", err, output)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	var archive string
	archivePattern := regexp.MustCompile(`^agent-fitness-functions-.+-darwin-arm64\.tar\.gz$`)
	sawChecksums := false
	for _, entry := range entries {
		if archivePattern.MatchString(entry.Name()) {
			archive = entry.Name()
		}
		if entry.Name() == "SHA256SUMS" {
			sawChecksums = true
		}
	}
	if archive == "" || !sawChecksums {
		t.Fatalf("packaging output = %v, want a darwin-arm64 archive plus SHA256SUMS", entries)
	}

	verify := exec.Command("shasum", "-a", "256", "-c", "SHA256SUMS")
	verify.Dir = outputDir
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("SHA256SUMS does not verify the archive: %v\n%s", err, output)
	}

	archivePath := filepath.Join(outputDir, archive)
	if err := os.WriteFile(archivePath, append(mustRead(t, archivePath), 0x00), 0o644); err != nil {
		t.Fatalf("tamper archive: %v", err)
	}
	verify = exec.Command("shasum", "-a", "256", "-c", "SHA256SUMS")
	verify.Dir = outputDir
	if _, err := verify.CombinedOutput(); err == nil {
		t.Fatal("SHA256SUMS verification accepted a tampered archive")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}

func TestInstallScriptBootstrapsIsolatedStateRootAndLifecycle(t *testing.T) {
	if _, err := os.Stat(filepath.Join("scripts", "install.sh")); err != nil {
		t.Fatalf("bootstrap installer scripts/install.sh missing: %v", err)
	}

	packageDir := t.TempDir()
	if output, err := runInstallerCommand(t, nil, "bash", filepath.Join("scripts", "package-release.sh"), "--output", packageDir); err != nil {
		t.Fatalf("package-release.sh failed: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var archivePath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			archivePath = filepath.Join(packageDir, entry.Name())
		}
	}
	if archivePath == "" {
		t.Fatal("no archive produced for install test")
	}

	stateHome := t.TempDir()
	env := []string{"XDG_STATE_HOME=" + stateHome}
	if output, err := runInstallerCommand(t, env, "bash", filepath.Join("scripts", "install.sh"), "--archive", archivePath, "--checksums", filepath.Join(packageDir, "SHA256SUMS")); err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}

	stateRoot := filepath.Join(stateHome, "agent-fitness-functions")
	binary := filepath.Join(stateRoot, "current", "bin", "agent-fitness-functions")
	if output, err := runInstallerCommand(t, nil, binary, "--help"); err != nil {
		t.Fatalf("installed binary is not runnable: %v\n%s", err, output)
	}
	versions, err := os.ReadDir(filepath.Join(stateRoot, "versions"))
	if err != nil || len(versions) != 1 {
		t.Fatalf("state root versions = %v (%v), want exactly one installed version", versions, err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "versions", versions[0].Name(), ".verified")); err != nil {
		t.Fatalf("installed version lacks the .verified completion sentinel: %v", err)
	}

	// Idempotent re-install: same archive, zero state change.
	beforeTree := snapshotInstallerTree(t, stateRoot)
	if output, err := runInstallerCommand(t, env, "bash", filepath.Join("scripts", "install.sh"), "--archive", archivePath, "--checksums", filepath.Join(packageDir, "SHA256SUMS")); err != nil {
		t.Fatalf("re-install failed: %v\n%s", err, output)
	}
	if afterTree := snapshotInstallerTree(t, stateRoot); beforeTree != afterTree {
		t.Fatalf("re-install with the current archive changed state:\nbefore:\n%s\nafter:\n%s", beforeTree, afterTree)
	}

	// Corrupted archive must abort without touching current.
	tampered := filepath.Join(t.TempDir(), filepath.Base(archivePath))
	if err := os.WriteFile(tampered, append(mustRead(t, archivePath), 0x00), 0o644); err != nil {
		t.Fatalf("write tampered archive: %v", err)
	}
	if output, err := runInstallerCommand(t, env, "bash", filepath.Join("scripts", "install.sh"), "--archive", tampered, "--checksums", filepath.Join(packageDir, "SHA256SUMS")); err == nil {
		t.Fatalf("install.sh accepted a tampered archive:\n%s", output)
	}
	if afterTree := snapshotInstallerTree(t, stateRoot); beforeTree != afterTree {
		t.Fatal("failed install of a tampered archive mutated the state root")
	}

	// Uninstall removes product-owned state only.
	if output, err := runInstallerCommand(t, env, binary, "uninstall", "--yes"); err != nil {
		t.Fatalf("uninstall failed: %v\n%s", err, output)
	}
	for _, removed := range []string{"versions", "current"} {
		if _, err := os.Stat(filepath.Join(stateRoot, removed)); !os.IsNotExist(err) {
			t.Errorf("uninstall left product state behind: %s", filepath.Join(stateRoot, removed))
		}
	}
}

func snapshotInstallerTree(t *testing.T, root string) string {
	t.Helper()
	var builder strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		builder.WriteString(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			builder.WriteString(" -> " + target)
		}
		builder.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walk state root: %v", err)
	}
	return builder.String()
}
