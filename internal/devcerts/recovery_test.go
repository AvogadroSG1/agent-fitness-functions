package devcerts

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errInjectedCrash = errors.New("injected publication crash")

func TestPublicationRetriesAtRecoverableDurabilityBoundaries(t *testing.T) {
	checkpoints := []string{
		"candidate_created",
		"candidate_owner_temp_synced",
		"journal_temp_synced_candidate_ready",
		"journal_renamed_candidate_ready",
		"journal_candidate_ready",
		"candidate_owner_removed",
		"version_renamed",
		"journal_temp_synced_version_ready",
		"journal_renamed_version_ready",
		"journal_version_ready",
		"current_temp_created",
		"current_renamed",
		"journal_temp_synced_current_published",
		"journal_renamed_current_published",
		"journal_current_published",
		"journal_temp_synced_retaining",
		"journal_renamed_retaining",
		"journal_retaining",
		"journal_removed",
	}
	for _, checkpoint := range checkpoints {
		t.Run(checkpoint, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			operations := operationsCrashingOnce(checkpoint)
			if err := publishWithOperations(root, false, operations); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("publishWithOperations crash at %s = %v, want injected crash", checkpoint, err)
			}
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish retry after %s: %v", checkpoint, err)
			}
			state, err := classifyRoot(root, operations.now(), false)
			if err != nil || state.state != statePublishedTarget || !state.fresh {
				t.Fatalf("state after retry = (%v, %v, %v), want fresh Published target", state.state, state.fresh, err)
			}
		})
	}
}

func TestPublicationPreservesUnjournaledDurableCandidateEvidence(t *testing.T) {
	checkpoints := []string{"candidate_owner_published", "candidate_material_synced"}
	for _, spec := range managedFiles {
		checkpoints = append(checkpoints, "candidate_file_synced_"+spec.name)
	}
	for _, checkpoint := range checkpoints {
		t.Run(checkpoint, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if err := publishWithOperations(root, false, operationsCrashingOnce(checkpoint)); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash at %s = %v", checkpoint, err)
			}
			before := snapshotLifecycleTree(t, root)
			if err := Publish(root, true); err == nil {
				t.Fatalf("Publish retry after %s succeeded", checkpoint)
			}
			if after := snapshotLifecycleTree(t, root); !bytes.Equal(before, after) {
				t.Fatalf("durable candidate evidence changed after %s", checkpoint)
			}
		})
	}
}

func TestRecoveryRejectsProtectedMaterialCorruptionAtEveryJournalStage(t *testing.T) {
	tests := []struct {
		name       string
		checkpoint string
		direct     bool
	}{
		{name: "candidate ready", checkpoint: "journal_candidate_ready"},
		{name: "candidate owner removed", checkpoint: "candidate_owner_removed"},
		{name: "candidate renamed before stage advance", checkpoint: "version_renamed"},
		{name: "version ready", checkpoint: "journal_version_ready"},
		{name: "current renamed before stage advance", checkpoint: "current_renamed"},
		{name: "current published", checkpoint: "journal_current_published"},
		{name: "cleaning direct root", checkpoint: "journal_cleaning_direct_root", direct: true},
		{name: "retaining", checkpoint: "journal_retaining"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if test.direct {
				root = newManagedRoot(t)
				material, err := generateMaterialAt(defaultOperations().now())
				if err != nil {
					t.Fatalf("generate direct material: %v", err)
				}
				writeMaterial(t, root, material)
			}
			if err := publishWithOperations(root, false, operationsCrashingOnce(test.checkpoint)); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash at %s = %v", test.checkpoint, err)
			}
			document, err := readJournal(filepath.Join(root, journalName))
			if err != nil {
				t.Fatalf("readJournal: %v", err)
			}
			protected := filepath.Join(root, filepath.FromSlash(document.VersionPath))
			if document.Stage == "candidate_ready" && pathExists(filepath.Join(root, filepath.FromSlash(document.CandidatePath))) {
				protected = filepath.Join(root, filepath.FromSlash(document.CandidatePath))
			}
			if err := os.WriteFile(filepath.Join(protected, "server.crt"), []byte("corrupted protected material\n"), 0o644); err != nil {
				t.Fatalf("corrupt protected material: %v", err)
			}
			if err := Publish(root, true); err == nil {
				t.Fatalf("Publish recovered corrupted %s material", document.Stage)
			}
			if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
				t.Fatalf("journal was removed after corruption: %v", err)
			}
		})
	}
}

func TestRecoveryAcceptsStaleTargetIdentityAtEveryJournalStageThenRotates(t *testing.T) {
	tests := []struct {
		name       string
		checkpoint string
		direct     bool
	}{
		{name: "candidate ready", checkpoint: "journal_candidate_ready"},
		{name: "version ready", checkpoint: "journal_version_ready"},
		{name: "current published", checkpoint: "journal_current_published"},
		{name: "cleaning direct root", checkpoint: "journal_cleaning_direct_root", direct: true},
		{name: "retaining", checkpoint: "journal_retaining"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if test.direct {
				root = newManagedRoot(t)
				material, err := generateMaterialAt(defaultOperations().now())
				if err != nil {
					t.Fatalf("generate direct material: %v", err)
				}
				writeMaterial(t, root, material)
			}
			if err := publishWithOperations(root, false, operationsCrashingOnce(test.checkpoint)); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash at %s = %v", test.checkpoint, err)
			}
			document, err := readJournal(filepath.Join(root, journalName))
			if err != nil {
				t.Fatalf("readJournal: %v", err)
			}
			protected := filepath.Join(root, filepath.FromSlash(document.VersionPath))
			if document.Stage == "candidate_ready" && pathExists(filepath.Join(root, filepath.FromSlash(document.CandidatePath))) {
				protected = filepath.Join(root, filepath.FromSlash(document.CandidatePath))
			}
			stale, err := generateMaterialAt(time.Now().Add(-2 * validity))
			if err != nil {
				t.Fatalf("generate stale target: %v", err)
			}
			writeMaterial(t, protected, stale)
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish recovered stale %s material: %v", document.Stage, err)
			}
			current, err := readSafeCurrent(root)
			if err != nil {
				t.Fatalf("readSafeCurrent: %v", err)
			}
			_, identity, fresh, err := readAndClassifyMaterial(filepath.Join(root, filepath.FromSlash(current)), time.Now())
			if err != nil || identity != identityTarget || !fresh {
				t.Fatalf("current after stale recovery = (%v, %v, %v), want fresh target", identity, fresh, err)
			}
		})
	}
}

func TestCandidateReadyRecoveryRejectsMalformedSymlinkedOrMismatchedOwner(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string, journalDocument)
	}{
		{name: "malformed", mutate: func(t *testing.T, owner string, _ journalDocument) {
			if err := os.WriteFile(owner, []byte("{}\n"), 0o600); err != nil {
				t.Fatalf("WriteFile(owner): %v", err)
			}
		}},
		{name: "symlinked", mutate: func(t *testing.T, owner string, _ journalDocument) {
			if err := os.Remove(owner); err != nil {
				t.Fatalf("Remove(owner): %v", err)
			}
			target := filepath.Join(filepath.Dir(owner), "owner-target")
			if err := os.WriteFile(target, []byte("evidence\n"), 0o600); err != nil {
				t.Fatalf("WriteFile(owner target): %v", err)
			}
			if err := os.Symlink(target, owner); err != nil {
				t.Fatalf("Symlink(owner): %v", err)
			}
		}},
		{name: "transaction mismatch", mutate: func(t *testing.T, owner string, document journalDocument) {
			content, err := canonicalJSON(candidateOwnerDocument{Schema: 1, TransactionID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CandidatePath: document.CandidatePath, VersionPath: document.VersionPath})
			if err != nil {
				t.Fatalf("canonicalJSON(owner): %v", err)
			}
			if err := os.WriteFile(owner, content, 0o600); err != nil {
				t.Fatalf("WriteFile(owner): %v", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if err := publishWithOperations(root, false, operationsCrashingOnce("journal_candidate_ready")); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash at candidate_ready: %v", err)
			}
			document, err := readJournal(filepath.Join(root, journalName))
			if err != nil {
				t.Fatalf("readJournal: %v", err)
			}
			owner := filepath.Join(root, filepath.FromSlash(document.CandidatePath), ".candidate-owner.json")
			test.mutate(t, owner, document)
			if err := Publish(root, true); err == nil {
				t.Fatal("Publish accepted invalid candidate owner")
			}
			if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
				t.Fatalf("journal evidence changed: %v", err)
			}
		})
	}
}

func TestCandidateReadyRecoveryRejectsReplacedOwnerAndMaterialEvidence(t *testing.T) {
	for _, target := range []string{"owner", "material"} {
		t.Run(target, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "certs")
			if err := publishWithOperations(root, false, operationsCrashingOnce("journal_candidate_ready")); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash at candidate_ready: %v", err)
			}
			document, err := readJournal(filepath.Join(root, journalName))
			if err != nil {
				t.Fatalf("readJournal: %v", err)
			}
			candidate := filepath.Join(root, filepath.FromSlash(document.CandidatePath))
			operations := defaultOperations()
			replaced := false
			operations.checkpoint = func(name string) error {
				want := "candidate_owner_validated"
				path := filepath.Join(candidate, ".candidate-owner.json")
				if target == "material" {
					want = "candidate_recovery_material_validated"
					path = filepath.Join(candidate, "server.crt")
				}
				if name != want || replaced {
					return nil
				}
				replaced = true
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("ReadFile(evidence): %v", err)
				}
				mode := os.FileMode(0o600)
				if target == "material" {
					mode = 0o644
				}
				replacement := path + ".replacement"
				if err := os.WriteFile(replacement, content, mode); err != nil {
					t.Fatalf("WriteFile(replacement): %v", err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatalf("Rename(replacement): %v", err)
				}
				return nil
			}
			if err := publishWithOperations(root, true, operations); err == nil {
				t.Fatalf("recovery accepted replaced %s evidence", target)
			}
			if !replaced {
				t.Fatalf("%s replacement checkpoint was not reached", target)
			}
		})
	}
}

func TestDirectRootCleanupDebtDoesNotRollBackPublishedCurrent(t *testing.T) {
	root := newManagedRoot(t)
	files, err := generateMaterialAt(defaultOperations().now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	writeMaterial(t, root, files)
	operations := operationsCrashingOnce("journal_cleaning_direct_root")
	if err := publishWithOperations(root, false, operations); !errors.Is(err, errInjectedCrash) {
		t.Fatalf("publishWithOperations cleaning crash = %v, want injected crash", err)
	}
	currentBefore, err := readSafeCurrent(root)
	if err != nil {
		t.Fatalf("read current after publication: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "ca.crt"), []byte("changed after publication\n"), 0o644); err != nil {
		t.Fatalf("mutate direct-root cleanup evidence: %v", err)
	}
	if err := Publish(root, true); err == nil {
		t.Fatal("Publish with digest conflict succeeded")
	}
	currentAfter, err := readSafeCurrent(root)
	if err != nil || currentAfter != currentBefore {
		t.Fatalf("current after cleanup conflict = %q, %v; want %q", currentAfter, err, currentBefore)
	}
	if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
		t.Fatalf("cleanup debt journal was not preserved: %v", err)
	}
}

func TestDirectRootReplacementAfterDigestValidationBecomesCleanupDebt(t *testing.T) {
	root := newManagedRoot(t)
	files, err := generateMaterialAt(defaultOperations().now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	writeMaterial(t, root, files)
	replaced := false
	operations := defaultOperations()
	operations.checkpoint = func(name string) error {
		if name != "direct_root_digest_validated_ca.crt" || replaced {
			return nil
		}
		replaced = true
		path := filepath.Join(root, "ca.crt")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(ca.crt): %v", err)
		}
		replacement := filepath.Join(root, ".replacement-ca")
		if err := os.WriteFile(replacement, content, 0o644); err != nil {
			t.Fatalf("WriteFile(replacement): %v", err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatalf("Rename(replacement): %v", err)
		}
		return nil
	}
	if err := publishWithOperations(root, false, operations); err == nil {
		t.Fatal("publishWithOperations accepted replaced direct-root file")
	}
	if !replaced {
		t.Fatal("direct-root replacement checkpoint was not reached")
	}
	if _, err := os.Lstat(filepath.Join(root, "ca.crt")); err != nil {
		t.Fatalf("replacement direct-root evidence was deleted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
		t.Fatalf("cleanup debt journal missing: %v", err)
	}
	if _, err := readSafeCurrent(root); err != nil {
		t.Fatalf("published current was rolled back: %v", err)
	}
}

func TestCurrentTempReplacementImmediatelyBeforeRenameIsRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	operations := defaultOperations()
	operations.checkpoint = func(name string) error {
		if name != "current_temp_validated" {
			return nil
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("ReadDir(root): %v", err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".current-") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			target, err := os.Readlink(path)
			if err != nil {
				t.Fatalf("Readlink(current temp): %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove(current temp): %v", err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatalf("replace current temp: %v", err)
			}
			return nil
		}
		t.Fatal("current temp was not found")
		return nil
	}
	if err := publishWithOperations(root, false, operations); err == nil {
		t.Fatal("publishWithOperations accepted replaced current temp")
	}
	if _, err := os.Lstat(filepath.Join(root, "current")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current was published from replaced temp: %v", err)
	}
}

func TestDirectRootCleanupJournalBoundariesAreRetryable(t *testing.T) {
	for _, checkpoint := range []string{"journal_temp_synced_cleaning_direct_root", "journal_renamed_cleaning_direct_root"} {
		t.Run(checkpoint, func(t *testing.T) {
			root := newManagedRoot(t)
			files, err := generateMaterialAt(defaultOperations().now())
			if err != nil {
				t.Fatalf("generate material: %v", err)
			}
			writeMaterial(t, root, files)
			if err := publishWithOperations(root, false, operationsCrashingOnce(checkpoint)); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash at %s = %v, want injected crash", checkpoint, err)
			}
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish retry after %s: %v", checkpoint, err)
			}
		})
	}
}

func TestDirectRootCleanupRetriesAfterEachGuardedDeletion(t *testing.T) {
	for _, spec := range managedFiles {
		t.Run(spec.name, func(t *testing.T) {
			root := newManagedRoot(t)
			files, err := generateMaterialAt(defaultOperations().now())
			if err != nil {
				t.Fatalf("generate material: %v", err)
			}
			writeMaterial(t, root, files)
			if err := publishWithOperations(root, false, operationsCrashingOnce("direct_root_removed_"+spec.name)); !errors.Is(err, errInjectedCrash) {
				t.Fatalf("crash after deleting %s = %v", spec.name, err)
			}
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish cleanup retry after %s: %v", spec.name, err)
			}
			for _, managed := range managedFiles {
				if _, err := os.Lstat(filepath.Join(root, managed.name)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("direct-root %s remains after cleanup retry: %v", managed.name, err)
				}
			}
		})
	}
}

func TestForcedPartialCleanupIsJournaledAndRetryable(t *testing.T) {
	root := newManagedRoot(t)
	if err := os.WriteFile(filepath.Join(root, "ca.crt"), []byte("partial managed evidence\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(partial): %v", err)
	}
	if err := publishWithOperations(root, true, operationsCrashingOnce("direct_root_removed_ca.crt")); !errors.Is(err, errInjectedCrash) {
		t.Fatalf("forced partial cleanup crash = %v, want injected crash", err)
	}
	if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
		t.Fatalf("partial cleanup journal missing: %v", err)
	}
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish partial cleanup retry without force: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "ca.crt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial evidence remains after retry: %v", err)
	}
}

func operationsCrashingOnce(checkpoint string) publicationOperations {
	operations := defaultOperations()
	crashed := false
	operations.checkpoint = func(got string) error {
		if got == checkpoint && !crashed {
			crashed = true
			return errInjectedCrash
		}
		return nil
	}
	return operations
}

func TestRetentionRetryPreservesCurrentAndRecordedPredecessor(t *testing.T) {
	checkpoints := []string{"retained_version_claimed_v-cccccccccccccccccccccccccccccccc"}
	for _, spec := range managedFiles {
		checkpoints = append(checkpoints, "retained_version_file_removed_v-cccccccccccccccccccccccccccccccc_"+spec.name)
	}
	checkpoints = append(checkpoints, "retained_version_directory_removed_v-cccccccccccccccccccccccccccccccc")
	for _, checkpoint := range checkpoints {
		t.Run(checkpoint, func(t *testing.T) {
			testRetentionRetryAtBoundary(t, checkpoint)
		})
	}
}

func TestJournalRemovalRejectsFinalMaterialChangedAfterRetention(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	operations := defaultOperations()
	changed := false
	operations.checkpoint = func(name string) error {
		if name != "retention_complete" || changed {
			return nil
		}
		changed = true
		document, err := readJournal(filepath.Join(root, journalName))
		if err != nil {
			t.Fatalf("readJournal: %v", err)
		}
		path := filepath.Join(root, filepath.FromSlash(document.VersionPath), "client.crt")
		if err := os.WriteFile(path, []byte("changed before journal removal\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(client.crt): %v", err)
		}
		return nil
	}
	if err := publishWithOperations(root, false, operations); err == nil {
		t.Fatal("publishWithOperations removed journal after final material changed")
	}
	if !changed {
		t.Fatal("retention completion checkpoint was not reached")
	}
	if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
		t.Fatalf("journal was removed after final material changed: %v", err)
	}
}

func testRetentionRetryAtBoundary(t *testing.T, checkpoint string) {
	t.Helper()
	root := newManagedRoot(t)
	if err := os.Mkdir(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatalf("Mkdir(versions): %v", err)
	}
	const (
		transactionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		finalRelative = "versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		priorRelative = "versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		extraRelative = "versions/v-cccccccccccccccccccccccccccccccc"
	)
	material, err := generateMaterialAt(defaultOperations().now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	for _, relative := range []string{finalRelative, priorRelative, extraRelative} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", relative, err)
		}
		writeMaterial(t, path, material)
	}
	if err := os.Symlink(finalRelative, filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(current): %v", err)
	}
	predecessor := priorRelative
	document := journalDocument{Schema: 1, TransactionID: transactionID, CandidatePath: "versions/.candidate-" + transactionID, VersionPath: finalRelative, PredecessorVersion: &predecessor, Stage: "retaining"}
	writeJournalFixture(t, filepath.Join(root, journalName), document)
	if err := recoverPublication(root, operationsCrashingOnce(checkpoint)); !errors.Is(err, errInjectedCrash) {
		t.Fatalf("recoverPublication retention crash = %v", err)
	}
	if err := Publish(root, false); err != nil {
		t.Fatalf("Publish retention retry: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("versions after retention retry = %v, %v", entries, err)
	}
	for _, relative := range []string{finalRelative, priorRelative} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("protected version %s missing: %v", relative, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(extraRelative))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("excess version remains: %v", err)
	}
}

func TestRecoveryRemovesOnlyExactTokenMatchedLockArtifacts(t *testing.T) {
	for _, prefix := range []string{".certificate-publication.release-", ".certificate-publication.reap-"} {
		t.Run(prefix, func(t *testing.T) {
			root := preparedLockRoot(t)
			const token = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			artifact := filepath.Join(root, prefix+token)
			if err := os.Mkdir(artifact, 0o700); err != nil {
				t.Fatalf("Mkdir(artifact): %v", err)
			}
			owner, _ := canonicalJSON(ownerDocument{Schema: 1, Hostname: "local", PID: 1, Token: token, AcquiredAt: "2026-08-22T12:00:00Z"})
			if err := os.WriteFile(filepath.Join(artifact, "owner.json"), owner, 0o600); err != nil {
				t.Fatalf("WriteFile(owner): %v", err)
			}
			if prefix == ".certificate-publication.release-" {
				if err := os.WriteFile(filepath.Join(artifact, "owner."+token+".tmp"), []byte("matching temp\n"), 0o600); err != nil {
					t.Fatalf("WriteFile(owner temp): %v", err)
				}
			}
			if err := Publish(root, false); err != nil {
				t.Fatalf("Publish with exact cleanup artifact: %v", err)
			}
			if _, err := os.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("exact cleanup artifact remains: %v", err)
			}
		})
	}
}

func TestRecoveryPreservesTokenMismatchedLockArtifact(t *testing.T) {
	root := preparedLockRoot(t)
	artifact := filepath.Join(root, ".certificate-publication.release-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(artifact, 0o700); err != nil {
		t.Fatalf("Mkdir(artifact): %v", err)
	}
	owner, _ := canonicalJSON(ownerDocument{Schema: 1, Hostname: "local", PID: 1, Token: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AcquiredAt: "2026-08-22T12:00:00Z"})
	if err := os.WriteFile(filepath.Join(artifact, "owner.json"), owner, 0o600); err != nil {
		t.Fatalf("WriteFile(owner): %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(artifact, "owner.json"))
	if err := Publish(root, true); !errors.Is(err, ErrLockOwnerMismatch) {
		t.Fatalf("Publish(token mismatch) error = %v, want ErrLockOwnerMismatch", err)
	}
	after, err := os.ReadFile(filepath.Join(artifact, "owner.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("mismatched lock artifact changed: %v", err)
	}
}
