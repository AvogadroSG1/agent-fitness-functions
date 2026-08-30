package client

// Red contract for calm-poc-mx5.2 (ADR-0007): all managed local governance
// state defaults to one machine-scoped root, <installer StateRoot>/governance,
// instead of per-repository <repo>/certs and <repo>/configs directories. The
// environment selectors keep their exact override semantics.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

// governanceStateHome pins XDG_STATE_HOME to a temp dir and clears every
// selector that could shadow the machine default, returning the expected
// governance root for assertions.
func governanceStateHome(t *testing.T) string {
	t.Helper()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv(envDevCertDir, "")
	t.Setenv(envConfigsDir, "")
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	return filepath.Join(stateHome, "agent-fitness-functions", "governance")
}

func TestResolveDevCertDirDefaultsToGovernanceRoot(t *testing.T) {
	govRoot := governanceStateHome(t)
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "certs"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveDevCertDir(repoRoot)

	want := filepath.Join(govRoot, "certs")
	if got != want {
		t.Fatalf("resolveDevCertDir(%q) = %q, want machine governance root %q (repo-local certs/ must no longer be consulted)", repoRoot, got, want)
	}
}

func TestResolveDevCertDirIgnoresEmptyRepoRoot(t *testing.T) {
	govRoot := governanceStateHome(t)

	got := resolveDevCertDir("")

	want := filepath.Join(govRoot, "certs")
	if got != want {
		t.Fatalf("resolveDevCertDir(\"\") = %q, want %q (machine default must not depend on a resolved repo root)", got, want)
	}
}

func TestResolveDevCertDirSelectorStillWins(t *testing.T) {
	governanceStateHome(t)
	selector := filepath.Join(t.TempDir(), "pinned-certs")
	t.Setenv(envDevCertDir, selector)

	if got := resolveDevCertDir(t.TempDir()); got != selector {
		t.Fatalf("resolveDevCertDir with selector = %q, want selector %q", got, selector)
	}
}

func TestResolveDevCertDirFallsBackToHomeStateWithoutXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv(envDevCertDir, "")

	got := resolveDevCertDir(t.TempDir())

	want := filepath.Join(home, ".local", "state", "agent-fitness-functions", "governance", "certs")
	if got != want {
		t.Fatalf("resolveDevCertDir without XDG_STATE_HOME = %q, want %q", got, want)
	}
}

func TestResolveConfigsDirDefaultsToGovernanceRootEvenWhenRepoConfigsExists(t *testing.T) {
	govRoot := governanceStateHome(t)
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "configs", "some-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveConfigsDir(repoRoot)

	want := filepath.Join(govRoot, "configs")
	if got != want {
		t.Fatalf("resolveConfigsDir(%q) = %q, want machine governance root %q (repo-local configs/ is the production handoff artifact, not the daemon's dir)", repoRoot, got, want)
	}
}

func TestResolveConfigsDirEnvOverrideStillWins(t *testing.T) {
	governanceStateHome(t)
	override := filepath.Join(t.TempDir(), "mounted-configs")
	t.Setenv(envConfigsDir, override)

	if got := resolveConfigsDir(t.TempDir()); got != override {
		t.Fatalf("resolveConfigsDir with env override = %q, want %q", got, override)
	}
}

func TestResolveDevCertVersionUsesGovernanceRootWhenSelectorUnset(t *testing.T) {
	govRoot := governanceStateHome(t)
	certsDir := filepath.Join(govRoot, "certs")
	// The publisher requires an existing parent; production code owns creating
	// the governance root before first publication.
	if err := os.MkdirAll(govRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := devcerts.Publish(certsDir, false); err != nil {
		t.Fatalf("publish governance certs: %v", err)
	}
	version, err := devcerts.ResolveManagedVersion(certsDir)
	if err != nil {
		t.Fatalf("resolve published version: %v", err)
	}
	// Run from a non-git directory: the machine default must not require a
	// resolvable repo root at all.
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	var stdout bytes.Buffer
	if err := RunResolveDevCertVersion(nil, &stdout); err != nil {
		t.Fatalf("RunResolveDevCertVersion outside a repo = %v, want governance-root resolution", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != version.RelativePath() {
		t.Fatalf("resolved version = %q, want governance version %q", got, version.RelativePath())
	}
}

func TestStartDaemonMissingConfigsDirRemediationNamesOnboard(t *testing.T) {
	governanceStateHome(t)

	err := StartDaemon(DaemonStartConfig{Addr: "https://127.0.0.1:7890", Local: true})

	if err == nil {
		t.Fatal("StartDaemon with no configs dir = nil error, want failure")
	}
	if !strings.Contains(err.Error(), "client onboard") {
		t.Fatalf("StartDaemon error = %q, want remediation naming `client onboard` (the command that registers a repo with the shared governance root)", err)
	}
}
