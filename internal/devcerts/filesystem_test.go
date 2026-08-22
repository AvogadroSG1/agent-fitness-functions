package devcerts

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEnsureDirectoryRevalidatesConcurrentMkdirEEXIST(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		t.Run(map[bool]string{false: "safe directory", true: "unsafe identity"}[unsafe], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "versions")
			mkdir := func(candidate string, mode os.FileMode) error {
				var err error
				if unsafe {
					err = os.WriteFile(candidate, []byte("not a directory"), 0o755)
				} else {
					err = os.Mkdir(candidate, mode)
				}
				if err != nil {
					t.Fatalf("create concurrent winner: %v", err)
				}
				return &os.PathError{Op: "mkdir", Path: candidate, Err: os.ErrExist}
			}

			created, err := ensureDirectory(path, 0o755, mkdir)
			if unsafe {
				if !errors.Is(err, ErrUnsupportedForBootstrap) {
					t.Fatalf("ensureDirectory unsafe winner error = %v, want ErrUnsupportedForBootstrap", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ensureDirectory safe winner: %v", err)
			}
			if created {
				t.Fatal("created = true, want false for concurrent EEXIST winner")
			}
		})
	}
}

func TestOpenPinnedDirectoryRejectsSymlinkAndIdentitySubstitution(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "managed")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	symlink := filepath.Join(dir, "managed-link")
	if err := os.Symlink(original, symlink); err != nil {
		t.Fatalf("symlink managed: %v", err)
	}
	if _, _, err := openPinnedDirectory(symlink, exactDirectoryMode(0o755), os.Open); !errors.Is(err, ErrUnsupportedForBootstrap) {
		t.Fatalf("openPinnedDirectory(symlink) error = %v, want ErrUnsupportedForBootstrap", err)
	}

	displaced := filepath.Join(dir, "displaced")
	opener := func(path string) (*os.File, error) {
		if err := os.Rename(path, displaced); err != nil {
			t.Fatalf("displace observed directory: %v", err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("create substituted directory: %v", err)
		}
		return os.Open(path)
	}
	if _, _, err := openPinnedDirectory(original, exactDirectoryMode(0o755), opener); !errors.Is(err, ErrUnsupportedForBootstrap) {
		t.Fatalf("openPinnedDirectory(identity substitution) error = %v, want ErrUnsupportedForBootstrap", err)
	}
}

func TestReadAndSyncPinnedDirectoryUseDescriptorAfterValidation(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "managed")
	if err := os.Mkdir(managed, 0o755); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, "b"), nil, 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, "a"), nil, 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	entries, err := readPinnedDirectory(managed, 0o755)
	if err != nil {
		t.Fatalf("readPinnedDirectory: %v", err)
	}
	if len(entries) != 2 || entries[0].Name() != "a" || entries[1].Name() != "b" {
		t.Fatalf("entries = %v, want sorted [a b]", entries)
	}
	if err := syncPinnedDirectory(managed, exactDirectoryMode(0o755)); err != nil {
		t.Fatalf("syncPinnedDirectory: %v", err)
	}
}

func TestReadPinnedRegularRejectsSymlinkAndModeMismatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("content"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	symlink := filepath.Join(dir, "link")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, _, err := readPinnedRegular(symlink, 0o644); !errors.Is(err, ErrUnsupportedForBootstrap) {
		t.Fatalf("readPinnedRegular(symlink) error = %v, want ErrUnsupportedForBootstrap", err)
	}
	if _, _, err := readPinnedRegular(target, 0o600); !errors.Is(err, ErrUnsupportedForBootstrap) {
		t.Fatalf("readPinnedRegular(mode mismatch) error = %v, want ErrUnsupportedForBootstrap", err)
	}
}

func TestWriteExclusiveRejectsExistingSymlinkUnchanged(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "exclusive")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := writeExclusive(link, []byte("replacement"), 0o600); err == nil {
		t.Fatal("writeExclusive(existing symlink) succeeded")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(content) != "preserve" {
		t.Fatalf("target content = %q, want preserve", content)
	}
}

func TestAuthoritativeRenameAndRemoveRejectSymlinkSources(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := renamePinnedRegular(link, filepath.Join(dir, "renamed"), 0o600); !errors.Is(err, ErrUnsupportedForBootstrap) {
		t.Fatalf("renamePinnedRegular(symlink) error = %v, want ErrUnsupportedForBootstrap", err)
	}
	if err := removePinnedRegular(link, 0o600); !errors.Is(err, ErrUnsupportedForBootstrap) {
		t.Fatalf("removePinnedRegular(symlink) error = %v, want ErrUnsupportedForBootstrap", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("rejected symlink source was changed: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "preserve" {
		t.Fatalf("target after rejected operations = %q, %v", content, err)
	}
}

func TestWriteRandomExclusiveRetriesCollisionWithoutTouchingEvidence(t *testing.T) {
	dir := t.TempDir()
	const collisionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const successID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	collision := filepath.Join(dir, "owner."+collisionID+".tmp")
	if err := os.WriteFile(collision, []byte("collision evidence"), 0o600); err != nil {
		t.Fatalf("write collision evidence: %v", err)
	}
	next := sequenceRandomID(t, collisionID, successID)
	id, path, err := writeRandomExclusive(next, func(id string) string {
		return filepath.Join(dir, "owner."+id+".tmp")
	}, func(id string) ([]byte, error) {
		return []byte("owner " + id), nil
	}, 0o600)
	if err != nil {
		t.Fatalf("writeRandomExclusive: %v", err)
	}
	if id != successID || path != filepath.Join(dir, "owner."+successID+".tmp") {
		t.Fatalf("result = (%q, %q), want success ID/path", id, path)
	}
	evidence, err := os.ReadFile(collision)
	if err != nil || string(evidence) != "collision evidence" {
		t.Fatalf("collision evidence = %q, %v", evidence, err)
	}
}

func TestWriteRandomExclusiveStopsAtBoundedCollisionLimit(t *testing.T) {
	dir := t.TempDir()
	const collisionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path := filepath.Join(dir, collisionID)
	if err := os.WriteFile(path, []byte("evidence"), 0o600); err != nil {
		t.Fatalf("write collision: %v", err)
	}
	calls := 0
	next := func() (string, error) {
		calls++
		return collisionID, nil
	}
	_, _, err := writeRandomExclusive(next, func(id string) string { return filepath.Join(dir, id) }, func(string) ([]byte, error) { return []byte("new"), nil }, 0o600)
	if !errors.Is(err, ErrRandomNameCollisions) {
		t.Fatalf("collision exhaustion error = %v, want ErrRandomNameCollisions", err)
	}
	if calls != maxNameAttempts {
		t.Fatalf("random calls = %d, want %d", calls, maxNameAttempts)
	}
	evidence, readErr := os.ReadFile(path)
	if readErr != nil || string(evidence) != "evidence" {
		t.Fatalf("collision evidence = %q, %v", evidence, readErr)
	}
}

func TestCreateCandidateDirectoryRetriesAllTransactionPathCollisions(t *testing.T) {
	root := t.TempDir()
	versions := filepath.Join(root, "versions")
	if err := os.Mkdir(versions, 0o755); err != nil {
		t.Fatalf("mkdir versions: %v", err)
	}
	ids := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"dddddddddddddddddddddddddddddddd",
	}
	if err := os.Mkdir(filepath.Join(versions, ".candidate-"+ids[0]), 0o755); err != nil {
		t.Fatalf("seed candidate collision: %v", err)
	}
	if err := os.Mkdir(filepath.Join(versions, "v-"+ids[1]), 0o755); err != nil {
		t.Fatalf("seed version collision: %v", err)
	}
	if err := os.Symlink("versions/v-"+ids[2], filepath.Join(root, ".current-"+ids[2])); err != nil {
		t.Fatalf("seed current collision: %v", err)
	}
	id, candidate, err := createCandidateDirectory(root, sequenceRandomID(t, ids...))
	if err != nil {
		t.Fatalf("createCandidateDirectory: %v", err)
	}
	if id != ids[3] || candidate != filepath.Join(versions, ".candidate-"+ids[3]) {
		t.Fatalf("candidate = (%q, %q), want final fresh ID", id, candidate)
	}
	for _, collision := range []string{filepath.Join(versions, ".candidate-"+ids[0]), filepath.Join(versions, "v-"+ids[1]), filepath.Join(root, ".current-"+ids[2])} {
		if _, err := os.Lstat(collision); err != nil {
			t.Fatalf("collision evidence %s changed: %v", collision, err)
		}
	}
}

func TestCreateRandomSymlinkRetriesCollisionWithoutTouchingEvidence(t *testing.T) {
	dir := t.TempDir()
	const collisionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const successID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	collision := filepath.Join(dir, ".current-"+collisionID)
	if err := os.Symlink("collision-target", collision); err != nil {
		t.Fatalf("seed symlink collision: %v", err)
	}
	id, path, err := createRandomSymlink(sequenceRandomID(t, collisionID, successID), func(id string) string {
		return filepath.Join(dir, ".current-"+id)
	}, "versions/v-target")
	if err != nil {
		t.Fatalf("createRandomSymlink: %v", err)
	}
	if id != successID || path != filepath.Join(dir, ".current-"+successID) {
		t.Fatalf("result = (%q, %q), want fresh symlink", id, path)
	}
	target, err := os.Readlink(collision)
	if err != nil || target != "collision-target" {
		t.Fatalf("collision evidence target = %q, %v", target, err)
	}
}

func TestRandomSerialRetriesZeroAndRequiresPositiveValue(t *testing.T) {
	draws := []*big.Int{big.NewInt(0), big.NewInt(7)}
	calls := 0
	serial, err := randomSerialWith(func(*big.Int) (*big.Int, error) {
		value := draws[calls]
		calls++
		return value, nil
	})
	if err != nil {
		t.Fatalf("randomSerialWith: %v", err)
	}
	if serial.Sign() <= 0 || serial.Cmp(big.NewInt(7)) != 0 || calls != 2 {
		t.Fatalf("serial = %v, calls = %d; want positive 7 after zero retry", serial, calls)
	}
}

func TestRandomSerialFailsAfterBoundedZeroDraws(t *testing.T) {
	calls := 0
	serial, err := randomSerialWith(func(*big.Int) (*big.Int, error) {
		calls++
		return big.NewInt(0), nil
	})
	if err == nil || serial != nil {
		t.Fatalf("randomSerialWith(all zero) = %v, %v; want nil error result", serial, err)
	}
	if calls != maxNameAttempts {
		t.Fatalf("draw calls = %d, want %d", calls, maxNameAttempts)
	}
}

func sequenceRandomID(t *testing.T, ids ...string) func() (string, error) {
	t.Helper()
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			t.Fatalf("random ID sequence exhausted")
		}
		id := ids[index]
		index++
		return id, nil
	}
}

func TestSuccessfulPublicationCanonicalSchemasAndDurabilityOrder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	var events []string
	var transactionID string
	observer := func(event publicationEvent) {
		events = append(events, event.name)
		switch event.name {
		case "lock_owner_published":
			content, mode := readObservedFile(t, filepath.Join(root, lockName, "owner.json"))
			if mode != 0o600 || !regexp.MustCompile(`^\{"schema":1,"hostname":"[^"]+","pid":[1-9][0-9]*,"token":"[0-9a-f]{32}","acquired_at":"[^"]+"\}\n$`).Match(content) {
				t.Fatalf("owner document mode/content is not canonical: mode=%04o content=%q", mode, content)
			}
		case "candidate_owner_published":
			entries, err := os.ReadDir(filepath.Join(root, "versions"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("candidate entries = %v, %v", entries, err)
			}
			transactionID = strings.TrimPrefix(entries[0].Name(), ".candidate-")
			content, mode := readObservedFile(t, filepath.Join(root, "versions", entries[0].Name(), ".candidate-owner.json"))
			want := []byte(`{"schema":1,"transaction_id":"` + transactionID + `","candidate_path":"versions/.candidate-` + transactionID + `","version_path":"versions/v-` + transactionID + `"}` + "\n")
			if mode != 0o600 || !bytes.Equal(content, want) {
				t.Fatalf("candidate owner mode/content = %04o %q, want %04o %q", mode, content, 0o600, want)
			}
		case "journal_candidate_ready", "journal_version_ready", "journal_current_published", "journal_retaining":
			content, mode := readObservedFile(t, filepath.Join(root, journalName))
			stage := strings.TrimPrefix(event.name, "journal_")
			want := []byte(`{"schema":1,"transaction_id":"` + transactionID + `","candidate_path":"versions/.candidate-` + transactionID + `","version_path":"versions/v-` + transactionID + `","predecessor_version":null,"stage":"` + stage + `","direct_root_sha256":null}` + "\n")
			if mode != 0o600 || !bytes.Equal(content, want) {
				t.Fatalf("journal %s mode/content = %04o %q, want %04o %q", stage, mode, content, 0o600, want)
			}
		}
	}
	if err := publish(root, false, observer); err != nil {
		t.Fatalf("publish with observer: %v", err)
	}
	wantOrder := []string{
		"lock_owner_published",
		"candidate_created",
		"candidate_owner_published",
		"candidate_material_synced",
		"candidate_validated",
		"versions_synced_before_candidate_ready",
		"journal_candidate_ready",
		"candidate_owner_removed",
		"version_renamed",
		"journal_version_ready",
		"current_temp_created",
		"current_renamed",
		"journal_current_published",
		"journal_retaining",
		"retention_synced",
		"journal_removed",
		"lock_released",
	}
	if !equalStrings(events, wantOrder) {
		t.Fatalf("publication events = %v, want %v", events, wantOrder)
	}
}

func TestExactTargetProfilesRejectEveryExtraAndWrongLifetime(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*x509.Certificate, *x509.Certificate, *x509.Certificate)
	}{
		{name: "CA SAN", mutate: func(ca, _, _ *x509.Certificate) { ca.DNSNames = []string{"extra"} }},
		{name: "CA EKU", mutate: func(ca, _, _ *x509.Certificate) { ca.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }},
		{name: "CA subject extra", mutate: func(ca, _, _ *x509.Certificate) { ca.Subject.Organization = []string{"extra"} }},
		{name: "server DNS extra", mutate: func(_, server, _ *x509.Certificate) { server.DNSNames = append(server.DNSNames, "extra") }},
		{name: "server CA", mutate: func(_, server, _ *x509.Certificate) { server.IsCA = true }},
		{name: "server EKU extra", mutate: func(_, server, _ *x509.Certificate) {
			server.ExtKeyUsage = append(server.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
		}},
		{name: "client SAN", mutate: func(_, _, client *x509.Certificate) { client.DNSNames = []string{"extra"} }},
		{name: "client EKU extra", mutate: func(_, _, client *x509.Certificate) {
			client.ExtKeyUsage = append(client.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
		}},
		{name: "lifetime one second long", mutate: func(ca, _, _ *x509.Certificate) { ca.NotAfter = ca.NotAfter.Add(time.Second) }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			ca, server, client := generatedProfiles(t)
			if !exactTargetProfiles(ca, server, client, time.Now()) {
				t.Fatal("generated baseline does not satisfy exact target profiles")
			}
			mutation.mutate(ca, server, client)
			if exactTargetProfiles(ca, server, client, time.Now()) {
				t.Fatalf("exactTargetProfiles accepted %s", mutation.name)
			}
		})
	}
}

func generatedProfiles(t *testing.T) (*x509.Certificate, *x509.Certificate, *x509.Certificate) {
	t.Helper()
	files, err := generateMaterial()
	if err != nil {
		t.Fatalf("generateMaterial: %v", err)
	}
	certs := make(map[string]*x509.Certificate)
	for _, file := range files {
		if file.name != "ca.crt" && file.name != "server.crt" && file.name != "client.crt" {
			continue
		}
		block, rest := pem.Decode(file.data)
		if block == nil || len(rest) != 0 {
			t.Fatalf("decode %s", file.name)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse %s: %v", file.name, err)
		}
		certs[file.name] = cert
	}
	return certs["ca.crt"], certs["server.crt"], certs["client.crt"]
}

func readObservedFile(t *testing.T, path string) ([]byte, os.FileMode) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", path, err)
	}
	return content, info.Mode().Perm()
}
