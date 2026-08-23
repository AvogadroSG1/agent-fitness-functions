package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func envLookup(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestStateRootHonorsXDGStateHomeOverride(t *testing.T) {
	got := StateRoot(envLookup(map[string]string{"XDG_STATE_HOME": "/custom/state"}))
	want := filepath.Join("/custom/state", "agent-fitness-functions")
	if got != want {
		t.Fatalf("StateRoot = %q, want %q", got, want)
	}
}

func TestStateRootDefaultsUnderHomeWhenXDGStateHomeUnset(t *testing.T) {
	got := StateRoot(envLookup(map[string]string{"HOME": "/home/example"}))
	want := filepath.Join("/home/example", ".local", "state", "agent-fitness-functions")
	if got != want {
		t.Fatalf("StateRoot = %q, want %q", got, want)
	}
}

func TestCacheRootHonorsXDGCacheHomeOverride(t *testing.T) {
	got := cacheRoot(envLookup(map[string]string{"XDG_CACHE_HOME": "/custom/cache"}))
	want := filepath.Join("/custom/cache", "agent-fitness-functions")
	if got != want {
		t.Fatalf("cacheRoot = %q, want %q", got, want)
	}
}

func TestCacheRootDefaultsUnderHomeWhenXDGCacheHomeUnset(t *testing.T) {
	got := cacheRoot(envLookup(map[string]string{"HOME": "/home/example"}))
	want := filepath.Join("/home/example", ".cache", "agent-fitness-functions")
	if got != want {
		t.Fatalf("cacheRoot = %q, want %q", got, want)
	}
}

// buildFakeStateRoot creates versions/<version>/.verified, current -> versions/<version>,
// and runtimes/, mirroring a real installed state root, so uninstall tests
// exercise real removal logic rather than assumptions about its shape.
func buildFakeStateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	stateRoot := filepath.Join(root, "agent-fitness-functions")
	versionDir := filepath.Join(stateRoot, "versions", "1.0.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, verifiedSentinelName), nil, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if err := os.Symlink(filepath.Join("versions", "1.0.0"), filepath.Join(stateRoot, "current")); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, "runtimes", "calm"), 0o755); err != nil {
		t.Fatalf("mkdir runtimes: %v", err)
	}
	return root
}

func TestRunUninstallRequiresYes(t *testing.T) {
	root := buildFakeStateRoot(t)
	getenv := envLookup(map[string]string{"XDG_STATE_HOME": root})
	var stdout, stderr bytes.Buffer

	err := RunUninstall(nil, &stdout, &stderr, getenv)
	if err == nil {
		t.Fatal("RunUninstall without --yes = nil error, want a usage error")
	}
	if !IsUsageError(err) {
		t.Fatalf("RunUninstall without --yes error = %v, want a usage error", err)
	}

	stateRoot := filepath.Join(root, "agent-fitness-functions")
	if _, statErr := os.Lstat(filepath.Join(stateRoot, "versions")); statErr != nil {
		t.Fatalf("uninstall without --yes must not remove state: %v", statErr)
	}
}

func TestRunUninstallRemovesOnlyProductOwnedState(t *testing.T) {
	root := buildFakeStateRoot(t)
	stateRoot := filepath.Join(root, "agent-fitness-functions")

	// A sibling directory next to the state root stands in for unrelated host
	// state (e.g. another XDG_STATE_HOME-rooted product); uninstall must never
	// touch it.
	sibling := filepath.Join(root, "some-other-product")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	siblingFile := filepath.Join(sibling, "keepme")
	if err := os.WriteFile(siblingFile, []byte("do not touch"), 0o644); err != nil {
		t.Fatalf("write sibling file: %v", err)
	}

	getenv := envLookup(map[string]string{"XDG_STATE_HOME": root})
	var stdout, stderr bytes.Buffer
	if err := RunUninstall([]string{"--yes"}, &stdout, &stderr, getenv); err != nil {
		t.Fatalf("RunUninstall failed: %v (%s)", err, stderr.String())
	}

	for _, removed := range []string{"versions", "current", "runtimes"} {
		if _, err := os.Lstat(filepath.Join(stateRoot, removed)); !os.IsNotExist(err) {
			t.Errorf("uninstall left %s behind (err=%v)", removed, err)
		}
	}
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("uninstall touched unrelated sibling state: %v", err)
	}
}

func TestRunUninstallOnMissingStateRootIsANoOpNotAnError(t *testing.T) {
	root := t.TempDir()
	getenv := envLookup(map[string]string{"XDG_STATE_HOME": root})
	var stdout, stderr bytes.Buffer
	if err := RunUninstall([]string{"--yes"}, &stdout, &stderr, getenv); err != nil {
		t.Fatalf("RunUninstall on a never-installed state root failed: %v (%s)", err, stderr.String())
	}
}

func TestRunUninstallRejectsPositionalArguments(t *testing.T) {
	root := buildFakeStateRoot(t)
	getenv := envLookup(map[string]string{"XDG_STATE_HOME": root})
	var stdout, stderr bytes.Buffer
	err := RunUninstall([]string{"--yes", "unexpected"}, &stdout, &stderr, getenv)
	if err == nil || !IsUsageError(err) {
		t.Fatalf("RunUninstall with a positional argument = %v, want a usage error", err)
	}
}

func TestRunRollbackFailsCleanlyWithNoInstallation(t *testing.T) {
	root := t.TempDir() // no state root at all yet
	getenv := envLookup(map[string]string{"XDG_STATE_HOME": root})
	var stdout, stderr bytes.Buffer
	if err := RunRollback(nil, &stdout, &stderr, getenv); err == nil {
		t.Fatal("RunRollback with no installation = nil error, want a failure")
	}
}

func TestParseArchiveVersionRejectsUnrecognizedNames(t *testing.T) {
	if _, err := parseArchiveVersion("not-a-release-archive.tar.gz"); err == nil {
		t.Fatal("parseArchiveVersion accepted an unrecognized archive name")
	}
	version, err := parseArchiveVersion("agent-fitness-functions-1.2.3-darwin-arm64.tar.gz")
	if err != nil {
		t.Fatalf("parseArchiveVersion failed: %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("parseArchiveVersion = %q, want %q", version, "1.2.3")
	}
}
