package devcerts_test

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

var versionTargetPattern = regexp.MustCompile(`^versions/v-[0-9a-f]{32}$`)

func TestPublishBootstrapsAndThenIsMetadataTotalNoOp(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "certs")
	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish(missing root, false): %v", err)
	}

	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}
	if !versionTargetPattern.MatchString(target) {
		t.Fatalf("current target = %q, want versions/v-<32 lowercase hex>", target)
	}
	assertMode(t, root, 0o755)
	assertMode(t, filepath.Join(root, "versions"), 0o755)
	version := filepath.Join(root, filepath.FromSlash(target))
	assertMode(t, version, 0o755)
	for _, file := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "ca.crt", mode: 0o644},
		{name: "server.crt", mode: 0o644},
		{name: "server.key", mode: 0o600},
		{name: "client.crt", mode: 0o644},
		{name: "client.key", mode: 0o600},
	} {
		assertMode(t, filepath.Join(version, file.name), file.mode)
	}
	assertTargetMaterial(t, version)
	assertExactEntries(t, root, []string{"current", "versions"})
	assertExactEntries(t, filepath.Join(root, "versions"), []string{filepath.Base(version)})
	assertExactEntries(t, version, []string{"ca.crt", "client.crt", "client.key", "server.crt", "server.key"})

	before := snapshotTree(t, root)
	if err := devcerts.Publish(root, true); err != nil {
		t.Fatalf("Publish(clean published target, true): %v", err)
	}
	after := snapshotTree(t, root)
	if !bytes.Equal(before, after) {
		t.Fatalf("clean Published target was modified\nbefore:\n%safter:\n%s", before, after)
	}
}

func TestPublishBootstrapsSafeEmptyRootWithGitignore(t *testing.T) {
	root := t.TempDir()
	gitignore := filepath.Join(root, ".gitignore")
	writeFixture(t, gitignore, "*\n!.gitignore\n", 0o644)
	before, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore before publication: %v", err)
	}
	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish(safe-empty root, false): %v", err)
	}
	after, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore after publication: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf(".gitignore changed: before=%q after=%q", before, after)
	}
	assertExactEntries(t, root, []string{".gitignore", "current", "versions"})
}

func TestPublishPreservesPublishedTargetWhenDaemonLogPresent(t *testing.T) {
	root := t.TempDir()
	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("initial Publish: %v", err)
	}
	daemonLog := filepath.Join(root, "daemon.log")
	writeFixture(t, daemonLog, "starting daemon addr=https://127.0.0.1:7890\n", 0o644)
	beforeTarget, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}

	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish with daemon.log present failed: %v", err)
	}

	afterTarget, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current) after re-publish: %v", err)
	}
	if beforeTarget != afterTarget {
		t.Fatalf("target changed: before=%q after=%q", beforeTarget, afterTarget)
	}
	assertExactEntries(t, root, []string{"current", "daemon.log", "versions"})
}

func TestPublishMigratesValidDirectRootTargetWithoutChangingBytes(t *testing.T) {
	root := t.TempDir()
	seedRootWithGeneratedMaterial(t, root)
	before := readManagedBytes(t, root)

	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish(valid direct-root target, false): %v", err)
	}

	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}
	after := readManagedBytes(t, filepath.Join(root, filepath.FromSlash(target)))
	for name, want := range before {
		if !bytes.Equal(after[name], want) {
			t.Errorf("published %s bytes changed", name)
		}
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("direct-root %s remains after completed migration: %v", name, err)
		}
	}
}

func TestPublishPartialRequiresForce(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "ca.crt"), "partial", 0o644)
	before := snapshotTree(t, root)
	if err := devcerts.Publish(root, false); err == nil {
		t.Fatal("Publish(partial, false) succeeded")
	}
	if after := snapshotTree(t, root); !bytes.Equal(after, before) {
		t.Fatalf("partial state changed without force\nbefore:\n%safter:\n%s", before, after)
	}

	if err := devcerts.Publish(root, true); err != nil {
		t.Fatalf("Publish(partial, true): %v", err)
	}
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}
	assertTargetMaterial(t, filepath.Join(root, filepath.FromSlash(target)))
	if _, err := os.Lstat(filepath.Join(root, "ca.crt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forced rotation left partial direct-root evidence: %v", err)
	}
}

func TestPublishCleanTargetWithOneCompletePreviousVersionIsMetadataTotalNoOp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish(empty): %v", err)
	}
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}
	previous := filepath.Join(root, "versions", "v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	copyVersionFixture(t, filepath.Join(root, filepath.FromSlash(target)), previous)

	before := snapshotTree(t, root)
	if err := devcerts.Publish(root, true); err != nil {
		t.Fatalf("Publish(clean target with previous, true): %v", err)
	}
	if after := snapshotTree(t, root); !bytes.Equal(before, after) {
		t.Fatalf("clean target with previous was modified\nbefore:\n%safter:\n%s", before, after)
	}
}

func TestPublishRefusesExcessOrMalformedVersionsUnchanged(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "third complete version", true: "malformed previous version"}[malformed], func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if err := devcerts.Publish(root, false); err != nil {
				t.Fatalf("Publish(empty): %v", err)
			}
			target, err := os.Readlink(filepath.Join(root, "current"))
			if err != nil {
				t.Fatalf("Readlink(current): %v", err)
			}
			current := filepath.Join(root, filepath.FromSlash(target))
			previous := filepath.Join(root, "versions", "v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			copyVersionFixture(t, current, previous)
			if malformed {
				if err := os.Chmod(filepath.Join(previous, "client.key"), 0o644); err != nil {
					t.Fatalf("chmod malformed previous key: %v", err)
				}
			} else {
				copyVersionFixture(t, current, filepath.Join(root, "versions", "v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
			}
			before := snapshotTree(t, root)
			err = devcerts.Publish(root, false)
			if !errors.Is(err, devcerts.ErrUnsupportedForBootstrap) {
				t.Fatalf("Publish(excess/malformed) error = %v, want ErrUnsupportedForBootstrap", err)
			}
			if after := snapshotTree(t, root); !bytes.Equal(before, after) {
				t.Fatalf("excess/malformed versions changed\nbefore:\n%safter:\n%s", before, after)
			}
		})
	}
}

func TestPublishRefusesUnsupportedEvidenceUnchanged(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "without force", true: "with force"}[force], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "legacy.txt")
			if err := os.WriteFile(path, []byte("do not touch\n"), 0o640); err != nil {
				t.Fatalf("seed unsupported evidence: %v", err)
			}
			before := snapshotTree(t, root)
			err := devcerts.Publish(root, force)
			if !errors.Is(err, devcerts.ErrUnsupportedForBootstrap) {
				t.Fatalf("Publish(unsupported, %v) error = %v, want ErrUnsupportedForBootstrap", force, err)
			}
			after := snapshotTree(t, root)
			if !bytes.Equal(before, after) {
				t.Fatalf("unsupported evidence was modified\nbefore:\n%safter:\n%s", before, after)
			}
		})
	}
}

func TestPublishRefusesManagedArtifactsAndPartialStatesUnchanged(t *testing.T) {
	setups := []struct {
		name         string
		setup        func(*testing.T, string)
		withoutForce bool
		withForce    bool
	}{
		{name: "partial direct root", withForce: true, setup: func(t *testing.T, root string) { writeFixture(t, filepath.Join(root, "ca.crt"), "partial", 0o644) }},
		{name: "unknown complete direct root", setup: func(t *testing.T, root string) {
			for _, file := range []string{"ca.crt", "server.crt", "server.key", "client.crt", "client.key"} {
				writeFixture(t, filepath.Join(root, file), "unknown", 0o600)
			}
		}},
		{name: "journal evidence", setup: func(t *testing.T, root string) {
			writeFixture(t, filepath.Join(root, ".certificate-publication-transaction.json"), "{}\n", 0o600)
		}},
		{name: "candidate evidence", withoutForce: true, withForce: true, setup: func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "versions", ".candidate-0123456789abcdef0123456789abcdef"), 0o755); err != nil {
				t.Fatalf("mkdir candidate evidence: %v", err)
			}
		}},
		{name: "current temp evidence", setup: func(t *testing.T, root string) {
			if err := os.Symlink("versions/v-0123456789abcdef0123456789abcdef", filepath.Join(root, ".current-0123456789abcdef0123456789abcdef")); err != nil {
				t.Fatalf("symlink current temp evidence: %v", err)
			}
		}},
	}
	for _, setup := range setups {
		for _, force := range []bool{false, true} {
			t.Run(setup.name+map[bool]string{false: " without force", true: " with force"}[force], func(t *testing.T) {
				root := t.TempDir()
				setup.setup(t, root)
				before := snapshotTree(t, root)
				err := devcerts.Publish(root, force)
				wantSuccess := setup.withoutForce
				if force {
					wantSuccess = setup.withForce
				}
				if wantSuccess {
					if err != nil {
						t.Fatalf("Publish(%s, %v): %v", setup.name, force, err)
					}
					return
				}
				if err == nil {
					t.Fatalf("Publish(%s, %v) succeeded", setup.name, force)
				}
				if after := snapshotTree(t, root); !bytes.Equal(before, after) {
					t.Fatalf("%s changed with force=%v\nbefore:\n%safter:\n%s", setup.name, force, before, after)
				}
			})
		}
	}
}

func TestPublishPreservesLockWhenOwnerTokenChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	done := make(chan error, 1)
	go func() {
		done <- devcerts.Publish(root, false)
	}()
	ownerPath := filepath.Join(root, ".certificate-publication.lock", "owner.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ownerPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("publisher did not create lock owner in time")
		}
		time.Sleep(time.Millisecond)
	}
	const replacementToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	content, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatalf("read owner: %v", err)
	}
	re := regexp.MustCompile(`"token":"[0-9a-f]{32}"`)
	changed := re.ReplaceAll(content, []byte(`"token":"`+replacementToken+`"`))
	if bytes.Equal(content, changed) {
		t.Fatal("owner fixture did not contain canonical token")
	}
	if err := os.WriteFile(ownerPath, changed, 0o600); err != nil {
		t.Fatalf("replace owner token: %v", err)
	}
	err = <-done
	if !errors.Is(err, devcerts.ErrLockOwnerMismatch) {
		t.Fatalf("Publish after owner mismatch error = %v, want ErrLockOwnerMismatch", err)
	}
	if strings.Contains(err.Error(), replacementToken) || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatalf("error exposed sensitive publication data: %q", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".certificate-publication.lock")); err != nil {
		t.Fatalf("mismatched lock was not preserved: %v", err)
	}
}

func assertTargetMaterial(t *testing.T, version string) {
	t.Helper()
	ca := readCertificate(t, filepath.Join(version, "ca.crt"))
	server := readCertificate(t, filepath.Join(version, "server.crt"))
	client := readCertificate(t, filepath.Join(version, "client.crt"))
	for name, cert := range map[string]*x509.Certificate{"CA": ca, "server": server, "client": client} {
		if got := cert.NotAfter.Sub(cert.NotBefore); got != 365*24*time.Hour {
			t.Fatalf("%s lifetime = %v, want exactly 365 days", name, got)
		}
	}
	if ca.Subject.CommonName != "agent-fitness-functions-dev-ca" || !ca.IsCA || ca.KeyUsage != x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature {
		t.Fatalf("CA identity/usages = CN %q, IsCA %v, usage %v", ca.Subject.CommonName, ca.IsCA, ca.KeyUsage)
	}
	if len(ca.DNSNames) != 0 || len(ca.IPAddresses) != 0 || len(ca.EmailAddresses) != 0 || len(ca.URIs) != 0 || len(ca.ExtKeyUsage) != 0 || len(ca.UnknownExtKeyUsage) != 0 {
		t.Fatalf("CA has unexpected SAN/EKU values: DNS=%v IP=%v email=%v URI=%v EKU=%v unknown=%v", ca.DNSNames, ca.IPAddresses, ca.EmailAddresses, ca.URIs, ca.ExtKeyUsage, ca.UnknownExtKeyUsage)
	}
	if server.Subject.CommonName != "localhost" || len(server.DNSNames) != 2 || server.DNSNames[0] != "localhost" || server.DNSNames[1] != "agent-fitness-functions" {
		t.Fatalf("server identity = CN %q, DNS %v", server.Subject.CommonName, server.DNSNames)
	}
	if len(server.IPAddresses) != 2 || server.IPAddresses[0].String() != "127.0.0.1" || server.IPAddresses[1].String() != "::1" {
		t.Fatalf("server IP SANs = %v", server.IPAddresses)
	}
	if server.IsCA || len(server.ExtKeyUsage) != 1 || server.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || len(server.UnknownExtKeyUsage) != 0 || len(server.EmailAddresses) != 0 || len(server.URIs) != 0 {
		t.Fatalf("server has unexpected CA/EKU/name extras")
	}
	if client.Subject.CommonName != "dev-hook-pool" {
		t.Fatalf("client CN = %q, want dev-hook-pool", client.Subject.CommonName)
	}
	if client.IsCA || len(client.ExtKeyUsage) != 1 || client.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth || len(client.UnknownExtKeyUsage) != 0 || len(client.DNSNames) != 0 || len(client.IPAddresses) != 0 || len(client.EmailAddresses) != 0 || len(client.URIs) != 0 {
		t.Fatalf("client has unexpected CA/EKU/name extras")
	}
	if _, err := server.Verify(x509.VerifyOptions{Roots: certPool(ca), DNSName: "agent-fitness-functions", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("verify server target certificate: %v", err)
	}
	if _, err := client.Verify(x509.VerifyOptions{Roots: certPool(ca), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("verify client target certificate: %v", err)
	}
	assertKeyMatches(t, filepath.Join(version, "server.key"), server)
	assertKeyMatches(t, filepath.Join(version, "client.key"), client)
}

func readCertificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatalf("%s is not one canonical certificate PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(%s): %v", path, err)
	}
	return cert
}

func assertKeyMatches(t *testing.T, path string, cert *x509.Certificate) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "RSA PRIVATE KEY" || len(rest) != 0 {
		t.Fatalf("%s is not one canonical RSA private key PEM", path)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKCS1PrivateKey(%s): %v", path, err)
	}
	public, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || key.N.Cmp(public.N) != 0 || key.E != public.E {
		t.Fatalf("private key %s does not match certificate", path)
	}
}

func certPool(cert *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %04o, want %04o", path, got, want)
	}
}

func assertExactEntries(t *testing.T, path string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", path, err)
	}
	if len(entries) != len(want) {
		t.Fatalf("entries(%s) = %v, want %v", path, entryNames(entries), want)
	}
	for i, entry := range entries {
		if entry.Name() != want[i] {
			t.Fatalf("entries(%s) = %v, want %v", path, entryNames(entries), want)
		}
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func snapshotTree(t *testing.T, root string) []byte {
	t.Helper()
	var snapshot bytes.Buffer
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(&snapshot, "%s|mode=%s|size=%d|mtime=%d|%s", relative, info.Mode(), info.Size(), info.ModTime().UnixNano(), statIdentity(info))
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(content)
			_, _ = fmt.Fprintf(&snapshot, "|sha256=%x", digest)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = snapshot.WriteString("|target=" + target)
		}
		_ = snapshot.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot.Bytes()
}

func statIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "identity=unavailable"
	}
	return fmt.Sprintf("inode=%d|uid=%d|gid=%d|nlink=%d", stat.Ino, stat.Uid, stat.Gid, stat.Nlink)
}

func writeFixture(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func copyVersionFixture(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("Mkdir(%s): %v", destination, err)
	}
	for _, spec := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "ca.crt", mode: 0o644},
		{name: "server.crt", mode: 0o644},
		{name: "server.key", mode: 0o600},
		{name: "client.crt", mode: 0o644},
		{name: "client.key", mode: 0o600},
	} {
		content, err := os.ReadFile(filepath.Join(source, spec.name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", spec.name, err)
		}
		if err := os.WriteFile(filepath.Join(destination, spec.name), content, spec.mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", spec.name, err)
		}
	}
}

func seedRootWithGeneratedMaterial(t *testing.T, root string) {
	t.Helper()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("Chmod(%s): %v", root, err)
	}
	temporary := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(temporary, false); err != nil {
		t.Fatalf("Publish(material fixture): %v", err)
	}
	target, err := os.Readlink(filepath.Join(temporary, "current"))
	if err != nil {
		t.Fatalf("Readlink(material fixture): %v", err)
	}
	for name, content := range readManagedBytes(t, filepath.Join(temporary, filepath.FromSlash(target))) {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".key") {
			mode = 0o600
		}
		if err := os.WriteFile(filepath.Join(root, name), content, mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}
}

func readManagedBytes(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, 5)
	for _, name := range []string{"ca.crt", "server.crt", "server.key", "client.crt", "client.key"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		result[name] = content
	}
	return result
}
