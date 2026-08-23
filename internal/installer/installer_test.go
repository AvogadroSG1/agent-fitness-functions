package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

// TestParseArchiveVersionRejectsUnsafeVersionSegments guards the version
// captured out of an archive name from ever being used as an unsafe
// versions/<version>/ path segment: "." and ".." must be rejected rather
// than silently joined into a filesystem path. (An empty or "/"-containing
// captured version cannot reach validateVersionSegment through this
// function, since filepath.Base strips path separators and the regex
// requires a non-empty capture before "-darwin-arm64.tar.gz"; those cases are
// covered directly against validateVersionSegment below.)
func TestParseArchiveVersionRejectsUnsafeVersionSegments(t *testing.T) {
	cases := []string{
		"agent-fitness-functions-.-darwin-arm64.tar.gz",  // "."
		"agent-fitness-functions-..-darwin-arm64.tar.gz", // ".."
	}
	for _, name := range cases {
		if version, err := parseArchiveVersion(name); err == nil {
			t.Errorf("parseArchiveVersion(%q) = %q, nil, want a rejection", name, version)
		}
	}
}

func TestValidateVersionSegmentRejectsUnsafeInputs(t *testing.T) {
	for _, version := range []string{"", ".", "..", "a/b", "/etc", "../../escape"} {
		if err := validateVersionSegment(version); err == nil {
			t.Errorf("validateVersionSegment(%q) = nil, want a rejection", version)
		}
	}
	if err := validateVersionSegment("1.2.3"); err != nil {
		t.Errorf("validateVersionSegment(%q) = %v, want nil", "1.2.3", err)
	}
}

// buildTarGz writes a gzip-compressed tar archive at path containing exactly
// the given entries (each written as a regular file with trivial content),
// for tests that need to craft a hostile archive extractArchive must reject.
func buildTarGz(t *testing.T, path string, entryNames []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range entryNames {
		content := []byte("payload")
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write tar header for %q: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar content for %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
}

// TestExtractArchiveRejectsPathTraversalEntry is a security regression test:
// a crafted archive containing a ".." entry that would extract outside
// destDir must be rejected rather than silently written past it.
func TestExtractArchiveRejectsPathTraversalEntry(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "traversal.tar.gz")
	buildTarGz(t, archivePath, []string{"../escaped.txt"})

	destDir := filepath.Join(dir, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir destDir: %v", err)
	}
	if err := extractArchive(archivePath, destDir); err == nil {
		t.Fatal("extractArchive accepted a \"..\" path-traversal entry")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("path-traversal entry escaped destDir: stat err = %v", err)
	}
}

// TestExtractArchiveRejectsAbsolutePathEntry is a security regression test:
// a crafted archive containing an absolute-path entry must be rejected
// rather than silently written to that absolute location.
func TestExtractArchiveRejectsAbsolutePathEntry(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "absolute.tar.gz")
	target := filepath.Join(dir, "outside", "planted.txt")
	buildTarGz(t, archivePath, []string{target})

	destDir := filepath.Join(dir, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir destDir: %v", err)
	}
	if err := extractArchive(archivePath, destDir); err == nil {
		t.Fatal("extractArchive accepted an absolute-path entry")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("absolute-path entry was written outside destDir: stat err = %v", err)
	}
}

// TestRunUninstallWithVersionsAsSymlinkRemovesOnlyTheSymlink is a security
// regression test: if versions/ under the state root has been replaced by a
// symlink pointing at an external directory (e.g. via a compromised or
// mis-provisioned state root), uninstall's os.RemoveAll(target) must remove
// only the symlink itself, never traverse through it and delete the external
// directory's contents.
func TestRunUninstallWithVersionsAsSymlinkRemovesOnlyTheSymlink(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "agent-fitness-functions")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}

	external := filepath.Join(root, "external-data")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external dir: %v", err)
	}
	sentinel := filepath.Join(external, "keepme")
	if err := os.WriteFile(sentinel, []byte("do not touch"), 0o644); err != nil {
		t.Fatalf("write external sentinel: %v", err)
	}

	versionsLink := filepath.Join(stateRoot, "versions")
	if err := os.Symlink(external, versionsLink); err != nil {
		t.Fatalf("symlink versions -> external: %v", err)
	}

	getenv := envLookup(map[string]string{"XDG_STATE_HOME": root})
	var stdout, stderr bytes.Buffer
	if err := RunUninstall([]string{"--yes"}, &stdout, &stderr, getenv); err != nil {
		t.Fatalf("RunUninstall failed: %v (%s)", err, stderr.String())
	}

	if _, err := os.Lstat(versionsLink); !os.IsNotExist(err) {
		t.Errorf("uninstall left the versions/ symlink behind: stat err = %v", err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("uninstall removed the external directory itself: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("uninstall traversed the symlink and deleted external content: %v", err)
	}
}
