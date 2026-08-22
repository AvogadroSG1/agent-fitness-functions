package devcerts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileJournalTempAcceptsOnlyDuplicateOrLegalAdjacentStage(t *testing.T) {
	tests := []struct {
		name   string
		from   string
		to     string
		direct bool
	}{
		{name: "duplicate", from: "candidate_ready", to: "candidate_ready"},
		{name: "candidate to version", from: "candidate_ready", to: "version_ready"},
		{name: "version to current", from: "version_ready", to: "current_published"},
		{name: "current to cleanup", from: "current_published", to: "cleaning_direct_root", direct: true},
		{name: "current to retaining", from: "current_published", to: "retaining"},
		{name: "cleanup to retaining", from: "cleaning_direct_root", to: "retaining", direct: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := preparedLockRoot(t)
			fixed := testJournal(test.from, test.direct)
			temporary := fixed
			temporary.Stage = test.to
			writeJournalFixture(t, filepath.Join(root, journalName), fixed)
			temp := journalTempPath(root, fixed.TransactionID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
			writeJournalFixture(t, temp, temporary)

			got, exists, err := reconcileJournalTemps(root, defaultOperations())
			if err != nil || !exists || got.Stage != test.to {
				t.Fatalf("reconcileJournalTemps = (%s, %v, %v), want stage %s", got.Stage, exists, err, test.to)
			}
			if _, err := os.Lstat(temp); !os.IsNotExist(err) {
				t.Fatalf("reconciled temp remains: %v", err)
			}
		})
	}
}

func TestReconcileJournalTempPreservesConflictingEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*journalDocument)
	}{
		{name: "nonadjacent", mutate: func(document *journalDocument) { document.Stage = "retaining" }},
		{name: "immutable mismatch", mutate: func(document *journalDocument) { document.VersionPath = "versions/v-cccccccccccccccccccccccccccccccc" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := preparedLockRoot(t)
			fixed := testJournal("candidate_ready", false)
			temporary := fixed
			test.mutate(&temporary)
			writeJournalFixture(t, filepath.Join(root, journalName), fixed)
			temp := journalTempPath(root, fixed.TransactionID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
			writeJournalFixture(t, temp, temporary)
			beforeFixed, _ := os.ReadFile(filepath.Join(root, journalName))
			beforeTemp, _ := os.ReadFile(temp)

			if _, _, err := reconcileJournalTemps(root, defaultOperations()); err == nil {
				t.Fatal("reconcileJournalTemps conflicting evidence succeeded")
			}
			afterFixed, _ := os.ReadFile(filepath.Join(root, journalName))
			afterTemp, _ := os.ReadFile(temp)
			if !bytes.Equal(beforeFixed, afterFixed) || !bytes.Equal(beforeTemp, afterTemp) {
				t.Fatal("conflicting journal evidence changed")
			}
		})
	}
}

func TestReconcileJournalTempPromotesSingleMatchedCandidateTemp(t *testing.T) {
	root := preparedLockRoot(t)
	document := testJournal("candidate_ready", false)
	candidate := filepath.Join(root, filepath.FromSlash(document.CandidatePath))
	if err := os.Mkdir(candidate, 0o755); err != nil {
		t.Fatalf("Mkdir(candidate): %v", err)
	}
	owner, _ := canonicalJSON(candidateOwnerDocument{Schema: 1, TransactionID: document.TransactionID, CandidatePath: document.CandidatePath, VersionPath: document.VersionPath})
	if err := os.WriteFile(filepath.Join(candidate, ".candidate-owner.json"), owner, 0o600); err != nil {
		t.Fatalf("WriteFile(candidate owner): %v", err)
	}
	material, err := generateMaterialAt(defaultOperations().now())
	if err != nil {
		t.Fatalf("generate material: %v", err)
	}
	writeMaterial(t, candidate, material)
	temp := journalTempPath(root, document.TransactionID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	writeJournalFixture(t, temp, document)
	got, exists, err := reconcileJournalTemps(root, defaultOperations())
	if err != nil || !exists || got.Stage != "candidate_ready" {
		t.Fatalf("reconcileJournalTemps(single matched temp) = (%v, %v, %v)", got, exists, err)
	}
	if _, err := os.Lstat(filepath.Join(root, journalName)); err != nil {
		t.Fatalf("fixed journal was not promoted: %v", err)
	}
}

func TestReconcileJournalTempRejectsMultipleTempsUnchanged(t *testing.T) {
	root := preparedLockRoot(t)
	document := testJournal("candidate_ready", false)
	paths := []string{
		journalTempPath(root, document.TransactionID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		journalTempPath(root, document.TransactionID, "cccccccccccccccccccccccccccccccc"),
	}
	for _, path := range paths {
		writeJournalFixture(t, path, document)
	}
	if _, _, err := reconcileJournalTemps(root, defaultOperations()); err == nil {
		t.Fatal("reconcileJournalTemps(multiple) succeeded")
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("multiple-temp evidence changed: %v", err)
		}
	}
}

func TestReconcileJournalTempRejectsMalformedTempUnchanged(t *testing.T) {
	root := preparedLockRoot(t)
	fixed := testJournal("candidate_ready", false)
	writeJournalFixture(t, filepath.Join(root, journalName), fixed)
	temp := journalTempPath(root, fixed.TransactionID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	malformed := []byte("{}\n")
	if err := os.WriteFile(temp, malformed, 0o600); err != nil {
		t.Fatalf("WriteFile(temp): %v", err)
	}
	beforeFixed, _ := os.ReadFile(filepath.Join(root, journalName))
	if _, _, err := reconcileJournalTemps(root, defaultOperations()); err == nil {
		t.Fatal("reconcileJournalTemps(malformed) succeeded")
	}
	afterFixed, _ := os.ReadFile(filepath.Join(root, journalName))
	afterTemp, err := os.ReadFile(temp)
	if err != nil || !bytes.Equal(beforeFixed, afterFixed) || !bytes.Equal(malformed, afterTemp) {
		t.Fatalf("malformed journal evidence changed: %v", err)
	}
}

func TestReconcileJournalTempRejectsUnmatchedFilenameOrCandidateMaterial(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		withFiles bool
	}{
		{name: "filename transaction mismatch", filename: "cccccccccccccccccccccccccccccccc", withFiles: true},
		{name: "candidate material missing", filename: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := preparedLockRoot(t)
			document := testJournal("candidate_ready", false)
			candidate := filepath.Join(root, filepath.FromSlash(document.CandidatePath))
			if err := os.Mkdir(candidate, 0o755); err != nil {
				t.Fatalf("Mkdir(candidate): %v", err)
			}
			owner, _ := canonicalJSON(candidateOwnerDocument{Schema: 1, TransactionID: document.TransactionID, CandidatePath: document.CandidatePath, VersionPath: document.VersionPath})
			if err := os.WriteFile(filepath.Join(candidate, ".candidate-owner.json"), owner, 0o600); err != nil {
				t.Fatalf("WriteFile(owner): %v", err)
			}
			if test.withFiles {
				material, err := generateMaterialAt(defaultOperations().now())
				if err != nil {
					t.Fatalf("generate material: %v", err)
				}
				writeMaterial(t, candidate, material)
			}
			temp := journalTempPath(root, test.filename, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
			writeJournalFixture(t, temp, document)
			before, _ := os.ReadFile(temp)
			if _, _, err := reconcileJournalTemps(root, defaultOperations()); err == nil {
				t.Fatal("reconcileJournalTemps(unmatched) succeeded")
			}
			after, err := os.ReadFile(temp)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("unmatched temp evidence changed: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(root, journalName)); !os.IsNotExist(err) {
				t.Fatalf("unmatched temp was promoted: %v", err)
			}
		})
	}
}

func testJournal(stage string, direct bool) journalDocument {
	const transactionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	document := journalDocument{Schema: 1, TransactionID: transactionID, CandidatePath: "versions/.candidate-" + transactionID, VersionPath: "versions/v-" + transactionID, Stage: stage}
	if direct {
		document.DirectRootSHA256 = map[string]string{}
		for _, spec := range managedFiles {
			document.DirectRootSHA256[spec.name] = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}
	}
	return document
}

func writeJournalFixture(t *testing.T, path string, document journalDocument) {
	t.Helper()
	content, err := canonicalJSON(document)
	if err != nil {
		t.Fatalf("canonicalJSON(journal): %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile(journal): %v", err)
	}
}

func journalTempPath(root, transactionID, tempID string) string {
	return filepath.Join(root, ".certificate-publication.transaction-"+transactionID+"-"+tempID+".tmp")
}
