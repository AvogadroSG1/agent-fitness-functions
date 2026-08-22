package devcerts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveManagedVersionReadsCurrentOnce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}

	current := filepath.Join(root, "current")
	readlinks := 0
	operations := defaultManagedOperations
	readlink := operations.readlink
	operations.readlink = func(path string) (string, error) {
		if path == current {
			readlinks++
		}
		return readlink(path)
	}
	version, err := resolveManagedVersionWithOperations(root, operations)
	if err != nil {
		t.Fatalf("resolveManagedVersionWithOperations: %v", err)
	}
	if readlinks != 1 {
		t.Fatalf("Readlink(current) calls = %d, want 1", readlinks)
	}
	if !managedTargetPattern.MatchString(version.RelativePath()) {
		t.Fatalf("RelativePath() = %q, want exact managed version", version.RelativePath())
	}
}

func TestLoadManagedServerOwnsOneGenerationInMemory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	version, err := ResolveManagedVersion(root)
	if err != nil {
		t.Fatalf("ResolveManagedVersion: %v", err)
	}
	material, err := LoadManagedServer(version)
	if err != nil {
		t.Fatalf("LoadManagedServer: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(version.RelativePath()))); err != nil {
		t.Fatalf("remove pinned source generation: %v", err)
	}
	if len(material.Certificate.Certificate) != 1 || material.Certificate.Leaf == nil {
		t.Fatalf("loaded certificate = %+v, want one in-memory leaf", material.Certificate)
	}
	if material.Certificate.Leaf.Subject.CommonName != "localhost" {
		t.Fatalf("server common name = %q, want localhost", material.Certificate.Leaf.Subject.CommonName)
	}
	if material.ClientCAs == nil || len(material.CA) == 0 {
		t.Fatal("managed server material omitted client CA or public CA bytes")
	}
}
