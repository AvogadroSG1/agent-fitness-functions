package devcerts

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveManagedVersionReturnsPinnedPathsWithOneReadlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}

	currentPath := filepath.Join(root, "current")
	currentLstats, readlinks, fingerprints := 0, 0, 0
	operations := defaultManagedOperations
	originalLstat, originalReadlink, originalOpen := operations.lstat, operations.readlink, operations.open
	operations.lstat = func(path string) (os.FileInfo, error) {
		if path == currentPath {
			currentLstats++
		}
		return originalLstat(path)
	}
	operations.readlink = func(path string) (string, error) {
		readlinks++
		return originalReadlink(path)
	}
	operations.open = func(path string) (*os.File, error) {
		for _, spec := range managedFiles {
			if filepath.Base(path) == spec.name {
				fingerprints++
				break
			}
		}
		return originalOpen(path)
	}

	version, err := resolveManagedVersionWithOperations(root, operations)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(%q): %v", root, err)
	}
	if currentLstats != 1 || readlinks != 1 || fingerprints != 5 {
		t.Fatalf("current Lstat/Readlink/file fingerprints = %d/%d/%d, want 1/1/5", currentLstats, readlinks, fingerprints)
	}
	if !versionPattern.MatchString(filepath.Base(version.RelativePath())) || filepath.Dir(version.RelativePath()) != "versions" {
		t.Fatalf("RelativePath() = %q, want versions/v-<32 lowercase hex>", version.RelativePath())
	}
	paths := version.Paths()
	versionRoot := filepath.Join(root, filepath.FromSlash(version.RelativePath()))
	if paths.CA != filepath.Join(versionRoot, "ca.crt") ||
		paths.ClientCertificate != filepath.Join(versionRoot, "client.crt") ||
		paths.ClientKey != filepath.Join(versionRoot, "client.key") ||
		paths.ServerCertificate != filepath.Join(versionRoot, "server.crt") ||
		paths.ServerKey != filepath.Join(versionRoot, "server.key") {
		t.Fatalf("Paths() = %+v, want all five paths beneath %q", paths, versionRoot)
	}
}

func TestResolveManagedVersionRejectsUnsafeCurrentTargets(t *testing.T) {
	tests := []struct {
		name   string
		target func(string) string
	}{
		{name: "absolute", target: func(target string) string { return "/" + target }},
		{name: "dot component", target: func(target string) string { return "versions/./" + filepath.Base(target) }},
		{name: "duplicate separator", target: func(target string) string { return "versions//" + filepath.Base(target) }},
		{name: "nested", target: func(target string) string { return "versions/nested/" + filepath.Base(target) }},
		{name: "traversal", target: func(target string) string { return "versions/../" + target }},
		{name: "wrong grammar", target: func(string) string { return "versions/current" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish(%q): %v", root, err)
			}
			publishedTarget, err := os.Readlink(filepath.Join(root, "current"))
			if err != nil {
				t.Fatalf("Readlink(current fixture): %v", err)
			}
			rawTarget := tt.target(publishedTarget)
			operations := defaultManagedOperations
			readlinks := 0
			operations.readlink = func(path string) (string, error) {
				readlinks++
				if path != filepath.Join(root, "current") {
					t.Fatalf("Readlink path = %q, want current", path)
				}
				return rawTarget, nil
			}
			if _, err := resolveManagedVersionWithOperations(root, operations); err == nil || err.Error() != "invalid managed certificate current target" {
				t.Fatalf("resolveManagedVersionWithOperations raw target %q error = %v, want current-target grammar error", rawTarget, err)
			}
			if readlinks != 1 {
				t.Fatalf("Readlink calls = %d, want 1", readlinks)
			}
		})
	}
}

func TestResolveManagedVersionRejectsEmptyInjectedCurrentTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	operations := defaultManagedOperations
	operations.readlink = func(string) (string, error) { return "", nil }
	if _, err := resolveManagedVersionWithOperations(root, operations); err == nil {
		t.Fatal("resolveManagedVersionWithOperations accepted an empty current target")
	}
}

func TestResolveManagedVersionRejectsMissingExtraAndUnsafeMaterial(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "missing", mutate: func(t *testing.T, versionRoot string) {
			if err := os.Remove(filepath.Join(versionRoot, "server.crt")); err != nil {
				t.Fatalf("Remove(server.crt): %v", err)
			}
		}},
		{name: "extra", mutate: func(t *testing.T, versionRoot string) {
			if err := os.WriteFile(filepath.Join(versionRoot, "extra"), []byte("extra"), 0o644); err != nil {
				t.Fatalf("WriteFile(extra): %v", err)
			}
		}},
		{name: "symlink", mutate: func(t *testing.T, versionRoot string) {
			path := filepath.Join(versionRoot, "client.crt")
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove(client.crt): %v", err)
			}
			if err := os.Symlink("ca.crt", path); err != nil {
				t.Fatalf("Symlink(client.crt): %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish(%q): %v", root, err)
			}
			target, err := os.Readlink(filepath.Join(root, "current"))
			if err != nil {
				t.Fatalf("Readlink(current): %v", err)
			}
			tt.mutate(t, filepath.Join(root, filepath.FromSlash(target)))
			if _, err := ResolveManagedVersion(root); err == nil {
				t.Fatal("ResolveManagedVersion accepted invalid material")
			}
		})
	}
}

func TestLoadManagedClientOwnsPinnedCertificateAndRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	version, err := ResolveManagedVersion(root)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(%q): %v", root, err)
	}

	material, err := LoadManagedClient(version)
	if err != nil {
		t.Fatalf("LoadManagedClient: %v", err)
	}
	certificate, roots := material.Certificate, material.RootCAs
	if len(certificate.Certificate) != 1 || certificate.Leaf == nil {
		t.Fatalf("certificate chain/leaf = %d/%v, want one parsed leaf", len(certificate.Certificate), certificate.Leaf)
	}
	if certificate.Leaf.Subject.CommonName != "dev-hook-pool" {
		t.Fatalf("client CN = %q, want dev-hook-pool", certificate.Leaf.Subject.CommonName)
	}
	if _, err := certificate.Leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("verify managed client certificate: %v", err)
	}

	certificate.Certificate[0][0] ^= 0xff
	otherMaterial, err := LoadManagedClient(version)
	if err != nil {
		t.Fatalf("second LoadManagedClient: %v", err)
	}
	if otherMaterial.Certificate.Certificate[0][0] == certificate.Certificate[0][0] {
		t.Fatal("LoadManagedClient returned shared certificate bytes")
	}
	if roots == otherMaterial.RootCAs {
		t.Fatal("LoadManagedClient returned a shared root pool")
	}
}

func TestLoadManagedClientUsesResolvedVersionAfterCurrentChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	version, err := ResolveManagedVersion(root)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(%q): %v", root, err)
	}

	if err := os.Remove(filepath.Join(root, "current")); err != nil {
		t.Fatalf("Remove(current): %v", err)
	}
	if err := os.Symlink("versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", filepath.Join(root, "current")); err != nil {
		t.Fatalf("replace current: %v", err)
	}
	material, err := LoadManagedClient(version)
	if err != nil {
		t.Fatalf("LoadManagedClient after current changed: %v", err)
	}
	if _, err := material.Certificate.Leaf.Verify(x509.VerifyOptions{Roots: material.RootCAs, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("verify certificate from resolved version: %v", err)
	}
}

func TestLoadManagedClientDoesNotReopenWhenCurrentRotatesAfterFirstOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	reads := map[string]int{}
	operations := defaultManagedOperations
	originalOpen := operations.open
	materialOpens := 0
	resolving := true
	operations.open = func(path string) (*os.File, error) {
		file, err := originalOpen(path)
		if !resolving && (filepath.Base(path) == "ca.crt" || filepath.Base(path) == "client.crt" || filepath.Base(path) == "client.key") {
			reads[path]++
			materialOpens++
		}
		if materialOpens == 1 {
			if removeErr := os.Remove(filepath.Join(root, "current")); removeErr != nil {
				t.Fatalf("Remove(current): %v", removeErr)
			}
			if linkErr := os.Symlink("versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", filepath.Join(root, "current")); linkErr != nil {
				t.Fatalf("replace current: %v", linkErr)
			}
		}
		return file, err
	}
	version, err := resolveManagedVersionWithOperations(root, operations)
	if err != nil {
		t.Fatalf("resolveManagedVersionWithOperations: %v", err)
	}
	resolving = false

	if _, err := LoadManagedClient(version); err != nil {
		t.Fatalf("LoadManagedClient during current rotation: %v", err)
	}
	paths := version.Paths()
	for _, path := range []string{paths.CA, paths.ClientCertificate, paths.ClientKey} {
		if reads[path] != 1 {
			t.Fatalf("managed file %q read %d times, want exactly once", path, reads[path])
		}
	}
}

func TestLoadManagedClientRejectsReplacedResolvedDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	version, err := ResolveManagedVersion(root)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(%q): %v", root, err)
	}
	versionRoot := filepath.Join(root, filepath.FromSlash(version.RelativePath()))
	replacement := filepath.Join(root, "replacement")
	if err := os.Rename(versionRoot, replacement); err != nil {
		t.Fatalf("Rename(version): %v", err)
	}
	if err := os.Mkdir(versionRoot, 0o755); err != nil {
		t.Fatalf("Mkdir(replacement version): %v", err)
	}
	if _, err := LoadManagedClient(version); err == nil {
		t.Fatal("LoadManagedClient accepted a replaced resolved directory")
	}
}

func TestResolveManagedVersionDoesNotLeakPathsWhenMaterialOpenFailsAfterLstat(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}

	operations := defaultManagedOperations
	originalOpen := operations.open
	operations.open = func(path string) (*os.File, error) {
		if filepath.Base(path) == "client.crt" {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
		}
		return originalOpen(path)
	}

	_, err := resolveManagedVersionWithOperations(root, operations)
	if err == nil {
		t.Fatal("resolveManagedVersionWithOperations accepted a version whose material open failed after lstat")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("resolveManagedVersionWithOperations error leaks managed root path: %q", err)
	}
	if strings.Contains(err.Error(), string(filepath.Separator)+"client.crt") {
		t.Fatalf("resolveManagedVersionWithOperations error leaks material file path: %q", err)
	}
}

func TestLoadManagedClientDoesNotLeakPathsWhenMaterialOpenFailsAfterResolve(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}

	resolved := false
	operations := defaultManagedOperations
	originalOpen := operations.open
	operations.open = func(path string) (*os.File, error) {
		if resolved && filepath.Base(path) == "client.key" {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
		}
		return originalOpen(path)
	}

	version, err := resolveManagedVersionWithOperations(root, operations)
	if err != nil {
		t.Fatalf("resolveManagedVersionWithOperations(%q): %v", root, err)
	}
	resolved = true

	_, err = LoadManagedClient(version)
	if err == nil {
		t.Fatal("LoadManagedClient accepted material whose open failed after resolution")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("LoadManagedClient error leaks managed root path: %q", err)
	}
	if strings.Contains(err.Error(), string(filepath.Separator)+"client.key") {
		t.Fatalf("LoadManagedClient error leaks material file path: %q", err)
	}
}

func TestLoadManagedClientDoesNotLeakPathsWhenMaterialReadFailsAfterOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}

	resolved := false
	operations := defaultManagedOperations
	originalOpen := operations.open
	operations.open = func(path string) (*os.File, error) {
		if resolved && filepath.Base(path) == "ca.crt" {
			// Write-only descriptor: Stat and SameFile succeed, but the
			// subsequent read fails with a path-bearing *os.PathError.
			return os.OpenFile(path, os.O_WRONLY, 0)
		}
		return originalOpen(path)
	}

	version, err := resolveManagedVersionWithOperations(root, operations)
	if err != nil {
		t.Fatalf("resolveManagedVersionWithOperations(%q): %v", root, err)
	}
	resolved = true

	_, err = LoadManagedClient(version)
	if err == nil {
		t.Fatal("LoadManagedClient accepted material whose content read failed after open")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("LoadManagedClient error leaks managed root path: %q", err)
	}
	if strings.Contains(err.Error(), string(filepath.Separator)+"ca.crt") {
		t.Fatalf("LoadManagedClient error leaks material file path: %q", err)
	}
}
