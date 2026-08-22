package server

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishManagedRuntimeWritesExactPublicArtifactsAndReplacesOnRestart(t *testing.T) {
	dir := t.TempDir()
	first := managedRuntime{version: "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ca: []byte("first public CA\n")}
	if err := publishManagedRuntime(dir, first, defaultRuntimeOperations); err != nil {
		t.Fatalf("publishManagedRuntime(first): %v", err)
	}
	second := managedRuntime{version: "versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ca: []byte("second public CA\n")}
	if err := publishManagedRuntime(dir, second, defaultRuntimeOperations); err != nil {
		t.Fatalf("publishManagedRuntime(second): %v", err)
	}

	want := map[string][]byte{
		"pinned-dev-cert-version": []byte(second.version + "\n"),
		"health-ca.crt":           second.ca,
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(runtime): %v", err)
	}
	if len(entries) != len(want) {
		t.Fatalf("runtime entries = %v, want exactly two", entries)
	}
	for name, content := range want {
		path := filepath.Join(dir, name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s = %q, want %q", name, got, content)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode/type = %v, %v; want regular 0644", name, info, err)
		}
	}
	combined := string(want["pinned-dev-cert-version"]) + string(want["health-ca.crt"])
	if strings.Contains(combined, "PRIVATE KEY") || strings.Contains(combined, "owner") {
		t.Fatal("runtime artifacts contain secret material")
	}
}

func TestPublishManagedRuntimeFailsBeforeReplacementOnRenameFault(t *testing.T) {
	dir := t.TempDir()
	operations := defaultRuntimeOperations
	operations.rename = func(string, string) error { return errors.New("injected rename failure") }
	err := publishManagedRuntime(dir, managedRuntime{
		version: "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ca:      []byte("public CA\n"),
	}, operations)
	if err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("publishManagedRuntime error = %v, want rename failure", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "pinned-dev-cert-version")); !os.IsNotExist(err) {
		t.Fatalf("destination published after rename fault: %v", err)
	}
}

func TestPublishManagedRuntimeFailsBeforeRenameOnSyncFault(t *testing.T) {
	dir := t.TempDir()
	operations := defaultRuntimeOperations
	operations.sync = func(*os.File) error { return errors.New("injected fsync failure") }
	err := publishManagedRuntime(dir, managedRuntime{
		version: "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ca:      []byte("public CA\n"),
	}, operations)
	if err == nil || !strings.Contains(err.Error(), "sync") {
		t.Fatalf("publishManagedRuntime error = %v, want sync failure", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir(runtime): %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("runtime entries after sync failure = %v, want no destination or temp", entries)
	}
}

func TestPublishManagedRuntimeRejectsUnsafeDirectoryAndDestination(t *testing.T) {
	parent := t.TempDir()
	realDir := filepath.Join(parent, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir(real): %v", err)
	}
	link := filepath.Join(parent, "runtime")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("Symlink(runtime): %v", err)
	}
	material := managedRuntime{version: "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ca: []byte("CA")}
	if err := publishManagedRuntime(link, material, defaultRuntimeOperations); err == nil {
		t.Fatal("publishManagedRuntime accepted symlink runtime directory")
	}

	if err := os.Symlink("elsewhere", filepath.Join(realDir, "pinned-dev-cert-version")); err != nil {
		t.Fatalf("Symlink(destination): %v", err)
	}
	if err := publishManagedRuntime(realDir, material, defaultRuntimeOperations); err == nil {
		t.Fatal("publishManagedRuntime replaced unsafe destination")
	}
}
