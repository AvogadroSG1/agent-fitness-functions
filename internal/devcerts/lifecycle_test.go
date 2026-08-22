package devcerts

import (
	"bytes"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaterialClassificationSeparatesIdentityFromFreshness(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		caName     string
		serverName string
		at         time.Time
		identity   materialIdentity
		fresh      bool
	}{
		{name: "fresh target", caName: "agent-fitness-functions-dev-ca", serverName: "agent-fitness-functions", at: now, identity: identityTarget, fresh: true},
		{name: "stale target", caName: "agent-fitness-functions-dev-ca", serverName: "agent-fitness-functions", at: now.Add(-2 * validity), identity: identityTarget, fresh: false},
		{name: "fresh predecessor", caName: "calm-poc-dev-ca", serverName: "stack-fitness-functions", at: now, identity: identityPredecessor, fresh: true},
		{name: "stale predecessor", caName: "calm-poc-dev-ca", serverName: "stack-fitness-functions", at: now.Add(-2 * validity), identity: identityPredecessor, fresh: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files, err := generateMaterialForIdentity(test.at, test.caName, test.serverName)
			if err != nil {
				t.Fatalf("generateMaterialForIdentity: %v", err)
			}
			identity, fresh := classifyMaterial(files, now)
			if identity != test.identity || fresh != test.fresh {
				t.Fatalf("classifyMaterial = (%v, %v), want (%v, %v)", identity, fresh, test.identity, test.fresh)
			}
		})
	}
}

func TestPublishRotatesStaleTargetAndRecognizedPredecessor(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		caName     string
		serverName string
		at         time.Time
	}{
		{name: "stale target", caName: "agent-fitness-functions-dev-ca", serverName: "agent-fitness-functions", at: now.Add(-2 * validity)},
		{name: "fresh predecessor", caName: "calm-poc-dev-ca", serverName: "stack-fitness-functions", at: now},
		{name: "stale predecessor", caName: "calm-poc-dev-ca", serverName: "stack-fitness-functions", at: now.Add(-2 * validity)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newManagedRoot(t)
			files, err := generateMaterialForIdentity(test.at, test.caName, test.serverName)
			if err != nil {
				t.Fatalf("generate fixture: %v", err)
			}
			writeMaterial(t, root, files)
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish(%s): %v", test.name, err)
			}
			target, err := readSafeCurrent(root)
			if err != nil {
				t.Fatalf("readSafeCurrent: %v", err)
			}
			_, identity, fresh, err := readAndClassifyMaterial(filepath.Join(root, filepath.FromSlash(target)), time.Now())
			if err != nil || identity != identityTarget || !fresh {
				t.Fatalf("published material = (%v, %v, %v), want fresh target", identity, fresh, err)
			}
			for _, file := range managedFiles {
				if _, err := os.Lstat(filepath.Join(root, file.name)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("direct-root %s remains: %v", file.name, err)
				}
			}
		})
	}
}

func TestPublishRotatesStalePublishedTargetAndRetainsRecordedPredecessor(t *testing.T) {
	root := newManagedRoot(t)
	if err := os.Mkdir(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatalf("Mkdir(versions): %v", err)
	}
	const oldRelative = "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	old := filepath.Join(root, filepath.FromSlash(oldRelative))
	if err := os.Mkdir(old, 0o755); err != nil {
		t.Fatalf("Mkdir(old version): %v", err)
	}
	stale, err := generateMaterialAt(time.Now().Add(-2 * validity))
	if err != nil {
		t.Fatalf("generate stale target: %v", err)
	}
	writeMaterial(t, old, stale)
	if err := os.Symlink(oldRelative, filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(current): %v", err)
	}

	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(stale published target): %v", err)
	}
	current, err := readSafeCurrent(root)
	if err != nil {
		t.Fatalf("readSafeCurrent: %v", err)
	}
	if current == oldRelative {
		t.Fatal("stale Published target did not rotate")
	}
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("versions after rotation = %v, %v; want current plus predecessor", entries, err)
	}
	if _, err := os.Lstat(old); err != nil {
		t.Fatalf("recorded predecessor was not retained: %v", err)
	}
}

func TestPublishUnknownCompleteRequiresForce(t *testing.T) {
	root := newManagedRoot(t)
	files, err := generateMaterialAt(time.Now())
	if err != nil {
		t.Fatalf("generate target: %v", err)
	}
	files[0].data = []byte("not a certificate\n")
	writeMaterial(t, root, files)
	before := snapshotLifecycleTree(t, root)
	if err := Publish(root, false); err == nil {
		t.Fatal("Publish(unknown complete, false) succeeded")
	}
	if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
		t.Fatal("unknown complete changed without force")
	}
	if err := Publish(root, true); err != nil {
		t.Fatalf("Publish(unknown complete, true): %v", err)
	}
}

func TestPublishUnknownCompleteCausesThroughPublicLifecycle(t *testing.T) {
	now := time.Now()
	base, err := generateMaterialAt(now)
	if err != nil {
		t.Fatalf("generate base material: %v", err)
	}
	other, err := generateMaterialAt(now)
	if err != nil {
		t.Fatalf("generate other material: %v", err)
	}
	foreign, err := generateMaterialForIdentity(now, "foreign-ca", "foreign-server")
	if err != nil {
		t.Fatalf("generate foreign material: %v", err)
	}
	wrongUsage, err := generateMaterialWithProfile(now, "agent-fitness-functions-dev-ca", "agent-fitness-functions", func(ca, _, _ *x509.Certificate) {
		ca.KeyUsage = x509.KeyUsageDigitalSignature
	})
	if err != nil {
		t.Fatalf("generate wrong-usage material: %v", err)
	}
	wrongEKU, err := generateMaterialWithProfile(now, "agent-fitness-functions-dev-ca", "agent-fitness-functions", func(_, server, _ *x509.Certificate) {
		server.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	})
	if err != nil {
		t.Fatalf("generate wrong-EKU material: %v", err)
	}
	wrongSAN, err := generateMaterialWithProfile(now, "agent-fitness-functions-dev-ca", "agent-fitness-functions", func(_, server, _ *x509.Certificate) {
		server.DNSNames = append(server.DNSNames, "unexpected")
	})
	if err != nil {
		t.Fatalf("generate wrong-SAN material: %v", err)
	}

	malformed := cloneMaterial(base)
	malformed[0].data = []byte("not PEM\n")
	keyMismatch := cloneMaterial(base)
	keyMismatch[2].data = other[2].data
	brokenChain := cloneMaterial(base)
	brokenChain[1].data = other[1].data
	brokenChain[2].data = other[2].data
	tests := map[string][]fileSpec{
		"malformed PEM":            malformed,
		"certificate key mismatch": keyMismatch,
		"broken chain":             brokenChain,
		"foreign identity":         foreign,
		"unexpected key usage":     wrongUsage,
		"unexpected EKU":           wrongEKU,
		"unexpected SAN":           wrongSAN,
	}
	for name, material := range tests {
		t.Run(name, func(t *testing.T) {
			root := newManagedRoot(t)
			writeMaterial(t, root, material)
			before := snapshotLifecycleTree(t, root)
			if err := Publish(root, false); err == nil {
				t.Fatal("Publish(unknown complete, false) succeeded")
			}
			if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
				t.Fatal("unknown complete changed without force")
			}
			if err := Publish(root, true); err != nil {
				t.Fatalf("Publish(unknown complete, true): %v", err)
			}
		})
	}
}

func cloneMaterial(files []fileSpec) []fileSpec {
	cloned := make([]fileSpec, len(files))
	copy(cloned, files)
	for index := range cloned {
		cloned[index].data = bytes.Clone(cloned[index].data)
	}
	return cloned
}

func TestFreshPublishedTargetWithRecognizedPredecessorUsesTotalNoOpFastPath(t *testing.T) {
	root := newManagedRoot(t)
	if err := os.Mkdir(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatalf("Mkdir(versions): %v", err)
	}
	const (
		currentRelative = "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		priorRelative   = "versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	target, err := generateMaterialAt(time.Now())
	if err != nil {
		t.Fatalf("generate target: %v", err)
	}
	predecessor, err := generateMaterialForIdentity(time.Now(), "calm-poc-dev-ca", "stack-fitness-functions")
	if err != nil {
		t.Fatalf("generate predecessor: %v", err)
	}
	for relative, material := range map[string][]fileSpec{currentRelative: target, priorRelative: predecessor} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", relative, err)
		}
		writeMaterial(t, path, material)
	}
	if err := os.Symlink(currentRelative, filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(current): %v", err)
	}
	before := snapshotLifecycleTree(t, root)
	var events []string
	if err := publish(root, true, func(event publicationEvent) { events = append(events, event.name) }); err != nil {
		t.Fatalf("publish(clean target with predecessor): %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("fast-path publication emitted events: %v", events)
	}
	if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
		t.Fatal("fresh Published target with predecessor changed")
	}
}

func TestPublishForceRotatesUnknownCompletePublishedVersionAndRetainsIt(t *testing.T) {
	root := newManagedRoot(t)
	if err := os.Mkdir(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatalf("Mkdir(versions): %v", err)
	}
	const unknownRelative = "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unknown := filepath.Join(root, filepath.FromSlash(unknownRelative))
	if err := os.Mkdir(unknown, 0o755); err != nil {
		t.Fatalf("Mkdir(unknown): %v", err)
	}
	material, err := generateMaterialAt(time.Now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	writeMaterial(t, unknown, material)
	if err := os.WriteFile(filepath.Join(unknown, "ca.crt"), []byte("unknown complete\n"), 0o644); err != nil {
		t.Fatalf("replace CA: %v", err)
	}
	if err := os.Symlink(unknownRelative, filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(current): %v", err)
	}
	if err := Publish(root, false); err == nil {
		t.Fatal("Publish(unknown published, false) succeeded")
	}
	if err := Publish(root, true); err != nil {
		t.Fatalf("Publish(unknown published, true): %v", err)
	}
	current, err := readSafeCurrent(root)
	if err != nil || current == unknownRelative {
		t.Fatalf("current after force = %q, %v", current, err)
	}
	if _, err := os.Lstat(unknown); err != nil {
		t.Fatalf("unknown recorded predecessor was not retained: %v", err)
	}
	before := snapshotLifecycleTree(t, root)
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(clean target with sanctioned unknown predecessor, false): %v", err)
	}
	if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
		t.Fatal("clean target with sanctioned unknown predecessor changed")
	}
	current, err = readSafeCurrent(root)
	if err != nil {
		t.Fatalf("read current before automatic rotation: %v", err)
	}
	stale, err := generateMaterialAt(time.Now().Add(-2 * validity))
	if err != nil {
		t.Fatalf("generate stale target: %v", err)
	}
	writeMaterial(t, filepath.Join(root, filepath.FromSlash(current)), stale)
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish(stale target with sanctioned unknown predecessor, false): %v", err)
	}
	if _, err := os.Lstat(unknown); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sanctioned unknown predecessor was not garbage-collected during automatic rotation: %v", err)
	}
}

func TestStructurallyMalformedRollbackInvalidatesCleanFastPath(t *testing.T) {
	root := newManagedRoot(t)
	if err := os.Mkdir(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatalf("Mkdir(versions): %v", err)
	}
	const (
		currentRelative = "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		priorRelative   = "versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	material, err := generateMaterialAt(time.Now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	for _, relative := range []string{currentRelative, priorRelative} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", relative, err)
		}
		writeMaterial(t, path, material)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(priorRelative), "client.key")); err != nil {
		t.Fatalf("Remove(rollback client.key): %v", err)
	}
	if err := os.Symlink(currentRelative, filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(current): %v", err)
	}
	before := snapshotLifecycleTree(t, root)
	if err := Publish(root, false); err == nil {
		t.Fatal("Publish(clean target with structurally malformed rollback, false) succeeded")
	}
	if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
		t.Fatal("structurally malformed rollback changed during refusal")
	}
}

func TestForceDoesNotBypassDirectRootFileTypeGuard(t *testing.T) {
	root := newManagedRoot(t)
	target := filepath.Join(root, "outside")
	if err := os.WriteFile(target, []byte("preserve\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside): %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "ca.crt")); err != nil {
		t.Fatalf("Symlink(ca.crt): %v", err)
	}
	before := snapshotLifecycleTree(t, root)
	if err := Publish(root, true); err == nil {
		t.Fatal("Publish(symlinked partial, true) succeeded")
	}
	if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
		t.Fatal("force changed symlinked direct-root evidence")
	}
}

func TestUnstablePrelockPartialObservationAcquiresLockAndReclassifies(t *testing.T) {
	root := newManagedRoot(t)
	material, err := generateMaterialAt(time.Now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, material[0].name), material[0].data, material[0].mode); err != nil {
		t.Fatalf("WriteFile(initial partial): %v", err)
	}
	completed := false
	var events []string
	operations := defaultOperations()
	operations.observer = func(event publicationEvent) { events = append(events, event.name) }
	operations.checkpoint = func(name string) error {
		if name != "prelock_classified" || completed {
			return nil
		}
		completed = true
		writeMaterial(t, root, material[1:])
		return nil
	}
	if err := publishWithOperations(root, false, operations); err != nil {
		t.Fatalf("publishWithOperations(unstable prelock partial): %v", err)
	}
	if !completed {
		t.Fatal("prelock observation seam was not reached")
	}
	if len(events) == 0 || events[0] != "lock_owner_published" {
		t.Fatalf("events = %v, want lock acquisition after unstable observation", events)
	}
}

func newManagedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("Chmod(root): %v", err)
	}
	return root
}

func writeMaterial(t *testing.T, path string, files []fileSpec) {
	t.Helper()
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(path, file.name), file.data, file.mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", file.name, err)
		}
	}
}

func snapshotLifecycleTree(t *testing.T, root string) []byte {
	t.Helper()
	var result bytes.Buffer
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result.WriteString(relative)
		result.WriteByte('\n')
		if entry.Type().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result.Write(content)
		}
		return nil
	}); err != nil {
		t.Fatalf("snapshot tree: %v", err)
	}
	return result.Bytes()
}
