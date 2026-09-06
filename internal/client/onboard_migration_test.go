package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// S7 red-test contract (calm-poc-cwcf): onboard heals pre-ADR-0007 repo-local
// state instead of warning about it — quarantine, never delete, idempotent,
// every action reported.

func plantManagedCertLayout(t *testing.T, repoRoot string) string {
	t.Helper()
	versionDir := filepath.Join(repoRoot, "certs", "versions", "v-0123456789abcdef0123456789abcdef")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "ca.crt"), []byte("legacy ca"), 0o644); err != nil {
		t.Fatalf("write ca.crt: %v", err)
	}
	current := filepath.Join(repoRoot, "certs", "current")
	if err := os.Symlink(filepath.Join("versions", "v-0123456789abcdef0123456789abcdef"), current); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
	return filepath.Join(repoRoot, "certs")
}

func migrationReports(t *testing.T, repoRoot string) []string {
	t.Helper()
	var lines []string
	if err := migrateLegacyState(repoRoot, func(line string) { lines = append(lines, line) }); err != nil {
		t.Fatalf("migrateLegacyState: %v", err)
	}
	return lines
}

func TestMigrateLegacyStateQuarantinesManagedCertLayout(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	certsDir := plantManagedCertLayout(t, repoRoot)

	lines := migrationReports(t, repoRoot)

	if _, err := os.Lstat(certsDir); !os.IsNotExist(err) {
		t.Fatalf("certs/ still present after migration (err=%v)", err)
	}
	quarantine := filepath.Join(repoRoot, "certs.pre-adr-0007.bak")
	if _, err := os.Stat(filepath.Join(quarantine, "versions", "v-0123456789abcdef0123456789abcdef", "ca.crt")); err != nil {
		t.Fatalf("quarantined contents missing: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "migrated:") {
		t.Errorf("reports %q must carry a migrated: line", lines)
	}
}

func TestMigrateLegacyStateLeavesForeignCertsDirAlone(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	foreign := filepath.Join(repoRoot, "certs")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "notes.txt"), []byte("hand-made"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines := migrationReports(t, repoRoot)

	if _, err := os.Stat(filepath.Join(foreign, "notes.txt")); err != nil {
		t.Fatalf("foreign certs dir was disturbed: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "certs") {
		t.Errorf("reports %q must warn about the unrecognized certs directory", lines)
	}
}

func TestMigrateLegacyStateQuarantinesUntrackedCallerBindings(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	bindings := filepath.Join(repoRoot, "caller-repos.json")
	if err := os.WriteFile(bindings, []byte(`{"callers":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	migrationReports(t, repoRoot)

	if _, err := os.Stat(bindings); !os.IsNotExist(err) {
		t.Fatalf("untracked caller-repos.json still present (err=%v)", err)
	}
	if _, err := os.Stat(bindings + ".pre-adr-0007.bak"); err != nil {
		t.Fatalf("quarantined bindings missing: %v", err)
	}
}

func TestMigrateLegacyStateWarnsButKeepsTrackedCallerBindings(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	bindings := filepath.Join(repoRoot, "caller-repos.json")
	if err := os.WriteFile(bindings, []byte(`{"callers":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	add := exec.Command("git", "-C", repoRoot, "add", "caller-repos.json")
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, output)
	}

	lines := migrationReports(t, repoRoot)

	if _, err := os.Stat(bindings); err != nil {
		t.Fatalf("tracked caller-repos.json was removed: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "caller-repos.json") {
		t.Errorf("reports %q must warn about the tracked bindings file", lines)
	}
}

func TestMigrateLegacyStateIsIdempotentAndTimestampsCollisions(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	plantManagedCertLayout(t, repoRoot)
	migrationReports(t, repoRoot)

	if lines := migrationReports(t, repoRoot); strings.Contains(strings.Join(lines, "\n"), "migrated:") {
		t.Fatalf("second run reported migrations on a clean tree: %q", lines)
	}

	plantManagedCertLayout(t, repoRoot)
	migrationReports(t, repoRoot)
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatalf("read repo root: %v", err)
	}
	quarantines := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "certs.pre-adr-0007.bak") {
			quarantines++
		}
	}
	if quarantines != 2 {
		t.Fatalf("quarantine count = %d, want 2 (collision must gain a distinct suffix, never overwrite)", quarantines)
	}
}

// The old-generation sidecar hard-coded cert_dir=$repo/certs; install-hooks
// must replace such a marker-matched body outright.
func TestInstallHooksReplacesLegacyCertDirSidecar(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	sidecar := filepath.Join(hooksDir, "agent-fitness-functions-pre-commit")
	legacyBody := "#!/bin/bash\n# agent-fitness-functions pre-commit hook (sidecar)\ncert_dir=\"${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR:-$repo/certs}\"\n"
	if err := os.WriteFile(sidecar, []byte(legacyBody), 0o755); err != nil {
		t.Fatalf("write legacy sidecar: %v", err)
	}

	var stdout, stderr strings.Builder
	if err := RunInstallHooks([]string{repoRoot}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\n%s", err, stderr.String())
	}
	content, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if strings.Contains(string(content), "$repo/certs") {
		t.Fatal("legacy cert_dir=$repo/certs body survived install-hooks")
	}
}
