package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPiOMPExtensionsPreservesUnrelatedFilesAndRefreshesManagedFiles(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)

	unrelatedPi := filepath.Join(repo, ".pi", "extensions", "unrelated", "index.ts")
	unrelatedOMP := filepath.Join(repo, ".omp", "extensions", "unrelated", "index.ts")
	for _, path := range []string{unrelatedPi, unrelatedOMP} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("unrelated extension\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}
	managed := []string{
		filepath.Join(repo, ".pi", "extensions", hookProductPrefix, "index.ts"),
		filepath.Join(repo, ".pi", "extensions", hookProductPrefix, "native-checker.mjs"),
		filepath.Join(repo, ".omp", "extensions", hookProductPrefix, "index.ts"),
		filepath.Join(repo, ".omp", "extensions", hookProductPrefix, "native-checker.mjs"),
	}
	for _, path := range managed {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat installed extension %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("mode for %s = %o, want 644", path, info.Mode().Perm())
		}
	}
	for _, path := range []string{unrelatedPi, unrelatedOMP} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read unrelated %s: %v", path, err)
		}
		if string(content) != "unrelated extension\n" {
			t.Fatalf("unrelated extension %s changed to %q", path, content)
		}
	}

	managedEntry := managed[0]
	if err := os.WriteFile(managedEntry, []byte("// agent-fitness-functions-pi-extension: managed by agent-fitness-functions client install-hooks\ncustom\n"), 0o644); err != nil {
		t.Fatalf("change managed entry: %v", err)
	}
	var stdout2, stderr2 bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout2, &stderr2); err != nil {
		t.Fatalf("RunInstallHooks second pass: %v\nstderr=%s", err, stderr2.String())
	}
	want, err := embeddedHooks.ReadFile("hookassets/pi-extension.ts")
	if err != nil {
		t.Fatalf("read embedded Pi extension: %v", err)
	}
	got, err := os.ReadFile(managedEntry)
	if err != nil {
		t.Fatalf("read refreshed Pi extension: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("managed Pi extension was not refreshed from embedded asset")
	}
}

func TestInstallPiOMPExtensionsChecksAllConflictsBeforeWriting(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	conflict := filepath.Join(repo, ".omp", "extensions", hookProductPrefix, "index.ts")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o755); err != nil {
		t.Fatalf("mkdir conflict directory: %v", err)
	}
	original := []byte("export default function unmanaged() {}\n")
	if err := os.WriteFile(conflict, original, 0o644); err != nil {
		t.Fatalf("write conflict: %v", err)
	}

	installer := hookInstaller{repoRoot: repo, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := installer.installPiOMPExtensions()
	if err == nil || !strings.Contains(err.Error(), conflict) {
		t.Fatalf("installPiOMPExtensions error = %v, want path-specific conflict", err)
	}
	got, err := os.ReadFile(conflict)
	if err != nil {
		t.Fatalf("read conflict after refusal: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("unmanaged conflict changed from %q to %q", original, got)
	}
	for _, path := range []string{
		filepath.Join(repo, ".pi", "extensions", hookProductPrefix, "index.ts"),
		filepath.Join(repo, ".pi", "extensions", hookProductPrefix, "native-checker.mjs"),
		filepath.Join(repo, ".omp", "extensions", hookProductPrefix, "native-checker.mjs"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("conflict install wrote %s, stat error=%v", path, statErr)
		}
	}
}

func TestInstallPiOMPExtensionsUsesConfiguredRelativeHooksPath(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	runGitClientTest(t, repo, "config", "--local", "core.hooksPath", ".governance-hooks")

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks: %v\nstderr=%s", err, stderr.String())
	}
	for _, name := range []string{gitGuardName, agentHookName} {
		if _, err := os.Stat(filepath.Join(repo, ".governance-hooks", name)); err != nil {
			t.Fatalf("configured hook %s not installed under relative core.hooksPath: %v", name, err)
		}
	}
}
