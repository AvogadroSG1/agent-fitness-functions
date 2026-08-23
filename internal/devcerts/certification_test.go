package devcerts

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Certification for calm-poc-q8d.11.5: given a recognized predecessor
// direct-root layout and concurrent publishers plus client/server readers,
// publication serializes, the predecessor identity is forcibly rotated,
// every successful read observes exactly one coherent generation, readers
// after publication settles always succeed, and no error exposes the
// managed root path.
func TestConcurrentPublishersAndReadersObserveOneCoherentGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("certification concurrency test is not short")
	}
	root := newManagedRoot(t)
	predecessor, err := generateMaterialForIdentity(time.Now(), "calm-poc-dev-ca", "stack-fitness-functions")
	if err != nil {
		t.Fatalf("generate predecessor material: %v", err)
	}
	writeMaterial(t, root, predecessor)

	var (
		writersDone   atomic.Bool
		publishes     atomic.Int64
		coherentReads atomic.Int64
		unsafeErrors  sync.Map
	)
	recordError := func(stage string, err error) {
		if err != nil && strings.Contains(err.Error(), root) {
			unsafeErrors.Store(stage+": "+err.Error(), true)
		}
	}

	var writers sync.WaitGroup
	for writer := 0; writer < 2; writer++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for iteration := 0; iteration < 3; iteration++ {
				if err := Publish(root, true); err != nil {
					recordError("publish", err)
					continue
				}
				publishes.Add(1)
			}
		}()
	}

	var readers sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				settled := writersDone.Load()
				version, err := ResolveManagedVersion(root)
				if err != nil {
					recordError("resolve", err)
					if settled {
						t.Errorf("settled resolve failed: %v", err)
						return
					}
					continue
				}
				client, err := LoadManagedClient(version)
				if err != nil {
					recordError("client", err)
					if settled {
						t.Errorf("settled LoadManagedClient failed: %v", err)
						return
					}
					continue
				}
				server, err := LoadManagedServer(version)
				if err != nil {
					recordError("server", err)
					if settled {
						t.Errorf("settled LoadManagedServer failed: %v", err)
						return
					}
					continue
				}
				generationCA, err := parseOneCertificate(server.CA)
				if err != nil {
					t.Errorf("parse server CA for %s: %v", version.RelativePath(), err)
					return
				}
				pool := x509.NewCertPool()
				pool.AddCert(generationCA)
				if _, err := client.Certificate.Leaf.Verify(x509.VerifyOptions{
					Roots:     pool,
					KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
				}); err != nil {
					t.Errorf("mixed generation observed at %s: client leaf does not verify against the same read's server CA: %v", version.RelativePath(), err)
					return
				}
				coherentReads.Add(1)
				if settled {
					return
				}
			}
		}()
	}

	writers.Wait()
	writersDone.Store(true)
	readers.Wait()

	if publishes.Load() == 0 {
		t.Fatal("no publication succeeded; publishers cannot all fail from a recognized predecessor layout")
	}
	if coherentReads.Load() == 0 {
		t.Fatal("no reader completed a coherent generation read")
	}
	unsafeErrors.Range(func(key, _ any) bool {
		t.Errorf("error leaked managed root path under concurrency: %v", key)
		return true
	})

	final, err := ResolveManagedVersion(root)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(final): %v", err)
	}
	versionRoot := filepath.Join(root, filepath.FromSlash(final.RelativePath()))
	_, identity, fresh, err := readAndClassifyMaterial(versionRoot, time.Now())
	if err != nil || identity != identityTarget || !fresh {
		t.Fatalf("published generation = (%v, %v, %v), want fresh rotated target identity", identity, fresh, err)
	}
	for _, file := range managedFiles {
		if _, err := os.Lstat(filepath.Join(root, file.name)); !os.IsNotExist(err) {
			t.Errorf("direct-root predecessor file %s survived publication: %v", file.name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		t.Fatalf("ReadDir(versions): %v", err)
	}
	if len(entries) == 0 || len(entries) > 2 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("retention invariant violated: versions/ holds %v, want current plus at most one previous", names)
	}
	if _, err := LoadManagedClient(final); err != nil {
		t.Fatalf("LoadManagedClient(final): %v", err)
	}
	if _, err := LoadManagedServer(final); err != nil {
		t.Fatalf("LoadManagedServer(final): %v", err)
	}
}
