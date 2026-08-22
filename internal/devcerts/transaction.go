package devcerts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var journalTempPattern = regexp.MustCompile(`^\.certificate-publication\.transaction-([0-9a-f]{32})-([0-9a-f]{32})\.tmp$`)
var retentionDeletePattern = regexp.MustCompile(`^\.retention-delete-([0-9a-f]{32})-(v-[0-9a-f]{32})$`)

func publishTransaction(root string, material []fileSpec, predecessor *string, directDigests map[string]string, operations publicationOperations) error {
	transactionID, candidate, err := createCandidateDirectory(root, operations.nextID)
	if err != nil {
		return err
	}
	candidateRelative := "versions/.candidate-" + transactionID
	versionRelative := "versions/v-" + transactionID
	version := filepath.Join(root, filepath.FromSlash(versionRelative))
	emit(operations.observer, "candidate_created")
	if err := operations.checkpoint("candidate_created"); err != nil {
		return err
	}
	owner := candidateOwnerDocument{Schema: 1, TransactionID: transactionID, CandidatePath: candidateRelative, VersionPath: versionRelative}
	ownerContent, err := canonicalJSON(owner)
	if err != nil {
		return err
	}
	_, ownerTemp, err := writeRandomExclusive(operations.nextID, func(id string) string {
		return filepath.Join(candidate, ".candidate-owner."+id+".tmp")
	}, func(string) ([]byte, error) { return ownerContent, nil }, 0o600)
	if err != nil {
		return errors.New("write certificate candidate owner")
	}
	if err := operations.checkpoint("candidate_owner_temp_synced"); err != nil {
		return err
	}
	ownerPath := filepath.Join(candidate, ".candidate-owner.json")
	if err := renamePinnedRegular(ownerTemp, ownerPath, 0o600); err != nil {
		return errors.New("publish certificate candidate owner")
	}
	if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "candidate_owner_published")
	if err := operations.checkpoint("candidate_owner_published"); err != nil {
		return err
	}
	for _, file := range material {
		if err := writeExclusive(filepath.Join(candidate, file.name), file.data, file.mode); err != nil {
			return fmt.Errorf("write development certificate candidate: %w", err)
		}
		if err := operations.checkpoint("candidate_file_synced_" + file.name); err != nil {
			return err
		}
	}
	if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "candidate_material_synced")
	if err := operations.checkpoint("candidate_material_synced"); err != nil {
		return err
	}
	if err := validateMaterial(candidate, true); err != nil {
		return fmt.Errorf("validate certificate candidate: %w", err)
	}
	emit(operations.observer, "candidate_validated")
	if err := syncPinnedDirectory(filepath.Join(root, "versions"), exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "versions_synced_before_candidate_ready")

	journal := journalDocument{Schema: 1, TransactionID: transactionID, CandidatePath: candidateRelative, VersionPath: versionRelative, PredecessorVersion: predecessor, Stage: "candidate_ready", DirectRootSHA256: directDigests}
	if err := replaceJournalWithOperations(root, journal, operations); err != nil {
		return err
	}
	if err := removePinnedRegular(ownerPath, 0o600); err != nil {
		return err
	}
	if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "candidate_owner_removed")
	if err := operations.checkpoint("candidate_owner_removed"); err != nil {
		return err
	}
	if err := renamePinnedDirectory(candidate, version, 0o755); err != nil {
		return err
	}
	if err := syncPinnedDirectory(filepath.Join(root, "versions"), exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "version_renamed")
	if err := operations.checkpoint("version_renamed"); err != nil {
		return err
	}
	journal.Stage = "version_ready"
	if err := replaceJournalWithOperations(root, journal, operations); err != nil {
		return err
	}
	if err := publishCurrent(root, journal, operations); err != nil {
		return err
	}
	journal.Stage = "current_published"
	if err := replaceJournalWithOperations(root, journal, operations); err != nil {
		return err
	}
	return finishPublishedTransaction(root, journal, operations)
}

func replaceJournalWithOperations(root string, document journalDocument, operations publicationOperations) error {
	if err := validateJournal(document); err != nil {
		return err
	}
	content, err := canonicalJSON(document)
	if err != nil {
		return err
	}
	_, temp, err := writeRandomExclusive(operations.nextID, func(id string) string {
		return journalTempPathForDocument(root, document.TransactionID, id)
	}, func(string) ([]byte, error) { return content, nil }, 0o600)
	if err != nil {
		return fmt.Errorf("write certificate publication journal: %w", err)
	}
	if err := operations.checkpoint("journal_temp_synced_" + document.Stage); err != nil {
		return err
	}
	if err := renamePinnedRegular(temp, filepath.Join(root, journalName), 0o600); err != nil {
		return fmt.Errorf("replace certificate publication journal: %w", err)
	}
	if err := operations.checkpoint("journal_renamed_" + document.Stage); err != nil {
		return err
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "journal_"+document.Stage)
	return operations.checkpoint("journal_" + document.Stage)
}

func journalTempPathForDocument(root, transactionID, tempID string) string {
	return filepath.Join(root, ".certificate-publication.transaction-"+transactionID+"-"+tempID+".tmp")
}

func publishCurrent(root string, journal journalDocument, operations publicationOperations) error {
	version := filepath.Join(root, filepath.FromSlash(journal.VersionPath))
	material, err := observeValidatedMaterial(version, false)
	if err != nil {
		return errors.New("final material is invalid before current publication")
	}
	_, currentTemp, err := createRandomSymlink(operations.nextID, func(id string) string {
		return filepath.Join(root, ".current-"+id)
	}, journal.VersionPath)
	if err != nil {
		return err
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "current_temp_created")
	if err := operations.checkpoint("current_temp_created"); err != nil {
		return err
	}
	if !material.unchanged() {
		return errors.New("final material changed before current publication")
	}
	if err := replaceCurrentSymlink(root, currentTemp, journal, operations); err != nil {
		return err
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "current_renamed")
	return operations.checkpoint("current_renamed")
}

func replaceCurrentSymlink(root, source string, journal journalDocument, operations publicationOperations) error {
	observed, err := os.Lstat(source)
	if err != nil || observed.Mode()&os.ModeSymlink == 0 {
		return ErrUnsupportedForBootstrap
	}
	target, err := os.Readlink(source)
	if err != nil || target != journal.VersionPath {
		return ErrUnsupportedForBootstrap
	}
	if err := operations.checkpoint("current_temp_validated"); err != nil {
		return err
	}
	current := filepath.Join(root, "current")
	currentTarget, err := readSafeCurrent(root)
	if journal.PredecessorVersion == nil {
		if !errors.Is(err, os.ErrNotExist) && pathExists(current) {
			return ErrUnsupportedForBootstrap
		}
	} else if err != nil || currentTarget != *journal.PredecessorVersion {
		return ErrUnsupportedForBootstrap
	}
	reobserved, err := os.Lstat(source)
	if err != nil || reobserved.Mode()&os.ModeSymlink == 0 || !os.SameFile(observed, reobserved) {
		return ErrUnsupportedForBootstrap
	}
	revalidatedTarget, err := os.Readlink(source)
	if err != nil || revalidatedTarget != journal.VersionPath {
		return ErrUnsupportedForBootstrap
	}
	return os.Rename(source, current)
}

func finishPublishedTransaction(root string, journal journalDocument, operations publicationOperations) error {
	if journal.DirectRootSHA256 != nil {
		if journal.Stage == "current_published" {
			journal.Stage = "cleaning_direct_root"
			if err := replaceJournalWithOperations(root, journal, operations); err != nil {
				return err
			}
		}
		if err := cleanupDirectRoot(root, journal.DirectRootSHA256, operations); err != nil {
			return err
		}
	}
	journal.Stage = "retaining"
	if err := replaceJournalWithOperations(root, journal, operations); err != nil {
		return err
	}
	if err := retainRecordedVersions(root, journal, operations); err != nil {
		return err
	}
	emit(operations.observer, "retention_synced")
	if err := operations.checkpoint("retention_complete"); err != nil {
		return err
	}
	if err := validateTargetIdentity(filepath.Join(root, filepath.FromSlash(journal.VersionPath)), false); err != nil {
		return errors.New("final material changed before journal removal")
	}
	if err := removePinnedRegular(filepath.Join(root, journalName), 0o600); err != nil {
		return err
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(operations.observer, "journal_removed")
	return operations.checkpoint("journal_removed")
}

func digestDirectState(files []fileSpec) map[string]string {
	byName := make(map[string][]byte, len(files))
	for _, file := range files {
		byName[file.name] = file.data
	}
	digests := make(map[string]string, len(managedFiles))
	for _, spec := range managedFiles {
		digest := sha256.Sum256(byName[spec.name])
		digests[spec.name] = hex.EncodeToString(digest[:])
	}
	return digests
}

func cleanupDirectRoot(root string, digests map[string]string, operations publicationOperations) error {
	for _, spec := range managedFiles {
		path := filepath.Join(root, spec.name)
		content, observed, err := readPinnedRegular(path, spec.mode)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return errors.New("direct-root cleanup evidence is unverifiable")
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != digests[spec.name] {
			return errors.New("direct-root cleanup digest changed after publication")
		}
		if err := operations.checkpoint("direct_root_digest_validated_" + spec.name); err != nil {
			return err
		}
		if err := removeObservedRegular(path, spec.mode, observed); err != nil {
			return err
		}
		if err := operations.checkpoint("direct_root_removed_" + spec.name); err != nil {
			return err
		}
	}
	return syncPinnedDirectory(root, exactDirectoryMode(0o755))
}

func removeObservedRegular(path string, mode os.FileMode, observed os.FileInfo) error {
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || current.Mode().Perm() != mode || !os.SameFile(observed, current) {
		return errors.New("file identity changed before removal")
	}
	return os.Remove(path)
}

func retainRecordedVersions(root string, journal journalDocument, operations publicationOperations) error {
	versions := filepath.Join(root, "versions")
	entries, err := readPinnedDirectory(versions, 0o755)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		matches := retentionDeletePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		if matches[1] != journal.TransactionID {
			return errors.New("retention deletion artifact belongs to another transaction")
		}
		if err := resumeClaimedVersionDeletion(versions, entry.Name(), matches[2], operations); err != nil {
			return err
		}
	}
	entries, err = readPinnedDirectory(versions, 0o755)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		relative := "versions/" + entry.Name()
		if relative == journal.VersionPath || journal.PredecessorVersion != nil && relative == *journal.PredecessorVersion {
			continue
		}
		path := filepath.Join(versions, entry.Name())
		if !versionPattern.MatchString(entry.Name()) {
			return ErrUnsupportedForBootstrap
		}
		if err := validateCompleteVersionStructure(path); err != nil {
			return ErrUnsupportedForBootstrap
		}
		claimedName := ".retention-delete-" + journal.TransactionID + "-" + entry.Name()
		if err := renamePinnedDirectory(path, filepath.Join(versions, claimedName), 0o755); err != nil {
			return err
		}
		if err := syncPinnedDirectory(versions, exactDirectoryMode(0o755)); err != nil {
			return err
		}
		if err := operations.checkpoint("retained_version_claimed_" + entry.Name()); err != nil {
			return err
		}
		if err := resumeClaimedVersionDeletion(versions, claimedName, entry.Name(), operations); err != nil {
			return err
		}
	}
	if err := syncPinnedDirectory(versions, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	return syncPinnedDirectory(root, exactDirectoryMode(0o755))
}

func validateCompleteVersionStructure(path string) error {
	entries, err := readPinnedDirectory(path, 0o755)
	if err != nil || len(entries) != len(managedFiles) {
		return ErrUnsupportedForBootstrap
	}
	for _, spec := range managedFiles {
		if _, _, err := readPinnedRegular(filepath.Join(path, spec.name), spec.mode); err != nil {
			return ErrUnsupportedForBootstrap
		}
	}
	return nil
}

func resumeClaimedVersionDeletion(versions, claimedName, originalName string, operations publicationOperations) error {
	path := filepath.Join(versions, claimedName)
	if _, ok := pinnedDirectory(path, 0o755); !ok {
		return ErrUnsupportedForBootstrap
	}
	removed, err := deletionProgress(path)
	if err != nil {
		return err
	}
	for index := removed; index < len(managedFiles); index++ {
		spec := managedFiles[index]
		if err := validateDeletionRemainder(path, index); err != nil {
			return err
		}
		filePath := filepath.Join(path, spec.name)
		_, observed, err := readPinnedRegular(filePath, spec.mode)
		if err != nil {
			return err
		}
		if err := removeObservedRegular(filePath, spec.mode, observed); err != nil {
			return err
		}
		if err := syncPinnedDirectory(path, exactDirectoryMode(0o755)); err != nil {
			return err
		}
		if err := operations.checkpoint("retained_version_file_removed_" + originalName + "_" + spec.name); err != nil {
			return err
		}
	}
	if err := validateDeletionRemainder(path, len(managedFiles)); err != nil {
		return err
	}
	if err := removePinnedDirectory(path, 0o755); err != nil {
		return err
	}
	if err := syncPinnedDirectory(versions, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	return operations.checkpoint("retained_version_directory_removed_" + originalName)
}

func deletionProgress(path string) (int, error) {
	for removed := 0; removed <= len(managedFiles); removed++ {
		if validateDeletionRemainder(path, removed) == nil {
			return removed, nil
		}
	}
	return 0, errors.New("retention deletion progress is invalid")
}

func validateDeletionRemainder(path string, removed int) error {
	entries, err := readPinnedDirectory(path, 0o755)
	if err != nil || len(entries) != len(managedFiles)-removed {
		return errors.New("retention deletion remainder is invalid")
	}
	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		present[entry.Name()] = true
	}
	for index, spec := range managedFiles {
		if present[spec.name] != (index >= removed) {
			return errors.New("retention deletion remainder is noncanonical")
		}
	}
	return nil
}

func recoverPublication(root string, operations publicationOperations) error {
	if err := cleanupLockArtifacts(root); err != nil {
		return err
	}
	journal, exists, err := reconcileJournalTemps(root, operations)
	if err != nil {
		return err
	}
	if !exists {
		return cleanupIncompleteCandidates(root)
	}
	return resumeJournal(root, journal, operations)
}

func cleanupLockArtifacts(root string) error {
	entries, err := readPinnedDirectory(root, 0o755)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		prefix := ""
		for _, candidate := range []string{".certificate-publication.release-", ".certificate-publication.reap-"} {
			if strings.HasPrefix(entry.Name(), candidate) {
				prefix = candidate
				break
			}
		}
		if prefix == "" {
			continue
		}
		token := strings.TrimPrefix(entry.Name(), prefix)
		if !hex128Pattern.MatchString(token) {
			return errors.New("invalid certificate publication lock cleanup artifact")
		}
		path := filepath.Join(root, entry.Name())
		document, err := readCanonicalOwner(filepath.Join(path, "owner.json"))
		if err != nil || document.Token != token {
			return ErrLockOwnerMismatch
		}
		children, err := readPinnedDirectory(path, 0o700)
		if err != nil || len(children) < 1 || len(children) > 2 {
			return ErrLockOwnerMismatch
		}
		for _, child := range children {
			if child.Name() != "owner.json" && child.Name() != "owner."+token+".tmp" {
				return ErrLockOwnerMismatch
			}
			if err := removePinnedRegular(filepath.Join(path, child.Name()), 0o600); err != nil {
				return err
			}
		}
		if err := removePinnedDirectory(path, 0o700); err != nil {
			return err
		}
		if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
			return err
		}
	}
	return nil
}

func cleanupIncompleteCandidates(root string) error {
	versions := filepath.Join(root, "versions")
	entries, err := readPinnedDirectory(versions, 0o755)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".candidate-") || !hex128Pattern.MatchString(strings.TrimPrefix(entry.Name(), ".candidate-")) {
			continue
		}
		candidate := filepath.Join(versions, entry.Name())
		children, err := readPinnedDirectory(candidate, 0o755)
		if err != nil {
			return ErrUnsupportedForBootstrap
		}
		switch len(children) {
		case 0:
		case 1:
			name := children[0].Name()
			if !strings.HasPrefix(name, ".candidate-owner.") || !strings.HasSuffix(name, ".tmp") || !hex128Pattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, ".candidate-owner."), ".tmp")) {
				return errors.New("candidate without journal has durable evidence")
			}
			if err := removePinnedRegular(filepath.Join(candidate, name), 0o600); err != nil {
				return err
			}
		default:
			return errors.New("candidate without journal has durable evidence")
		}
		if err := removePinnedDirectory(candidate, 0o755); err != nil {
			return err
		}
		if err := syncPinnedDirectory(versions, exactDirectoryMode(0o755)); err != nil {
			return err
		}
	}
	return nil
}

func resumeJournal(root string, journal journalDocument, operations publicationOperations) error {
	candidate := filepath.Join(root, filepath.FromSlash(journal.CandidatePath))
	version := filepath.Join(root, filepath.FromSlash(journal.VersionPath))
	switch journal.Stage {
	case "candidate_ready":
		candidateExists := pathExists(candidate)
		versionExists := pathExists(version)
		if candidateExists == versionExists || !currentMatches(root, journal.PredecessorVersion) {
			return errors.New("candidate_ready recovery evidence conflicts")
		}
		if candidateExists {
			owner := filepath.Join(candidate, ".candidate-owner.json")
			var material materialObservation
			if pathExists(owner) {
				ownerInfo, err := validateCandidateOwner(owner, journal)
				if err != nil {
					return err
				}
				if err := operations.checkpoint("candidate_owner_validated"); err != nil {
					return err
				}
				material, err = observeValidatedMaterial(candidate, true)
				if err != nil {
					return errors.New("candidate_ready material is invalid")
				}
				if err := removeObservedRegular(owner, 0o600, ownerInfo); err != nil {
					return err
				}
				if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
					return err
				}
			} else {
				var err error
				material, err = observeValidatedMaterial(candidate, false)
				if err != nil {
					return errors.New("candidate_ready material without owner is invalid")
				}
			}
			if err := operations.checkpoint("candidate_recovery_material_validated"); err != nil {
				return err
			}
			if !material.unchanged() {
				return errors.New("candidate_ready material changed before rename")
			}
			if err := renamePinnedDirectory(candidate, version, 0o755); err != nil {
				return err
			}
			if err := syncPinnedDirectory(filepath.Join(root, "versions"), exactDirectoryMode(0o755)); err != nil {
				return err
			}
		} else if err := validateTargetIdentity(version, false); err != nil {
			return errors.New("candidate_ready final material is invalid")
		}
		journal.Stage = "version_ready"
		if err := replaceJournalWithOperations(root, journal, operations); err != nil {
			return err
		}
		fallthrough
	case "version_ready":
		if pathExists(candidate) || !pathExists(version) {
			return errors.New("version_ready recovery evidence conflicts")
		}
		if err := validateTargetIdentity(version, false); err != nil {
			return errors.New("version_ready material is invalid")
		}
		current, err := readSafeCurrent(root)
		if err == nil && current == journal.VersionPath {
			journal.Stage = "current_published"
			if err := replaceJournalWithOperations(root, journal, operations); err != nil {
				return err
			}
		} else {
			if !currentMatches(root, journal.PredecessorVersion) {
				return errors.New("version_ready current conflicts")
			}
			temp, err := matchingCurrentTemp(root, journal.VersionPath)
			if err != nil {
				return err
			}
			if temp == "" {
				if err := publishCurrent(root, journal, operations); err != nil {
					return err
				}
			} else {
				if err := replaceCurrentSymlink(root, temp, journal, operations); err != nil {
					return err
				}
				if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
					return err
				}
			}
			journal.Stage = "current_published"
			if err := replaceJournalWithOperations(root, journal, operations); err != nil {
				return err
			}
		}
		fallthrough
	case "current_published", "cleaning_direct_root", "retaining":
		current, err := readSafeCurrent(root)
		if err != nil || current != journal.VersionPath || !pathExists(version) {
			return errors.New("published recovery evidence conflicts")
		}
		if err := validateTargetIdentity(version, false); err != nil {
			return errors.New("published recovery material is invalid")
		}
		if journal.Stage == "retaining" {
			if err := retainRecordedVersions(root, journal, operations); err != nil {
				return err
			}
			if err := operations.checkpoint("retention_complete"); err != nil {
				return err
			}
			if err := validateTargetIdentity(version, false); err != nil {
				return errors.New("final material changed before recovered journal removal")
			}
			if err := removePinnedRegular(filepath.Join(root, journalName), 0o600); err != nil {
				return err
			}
			return syncPinnedDirectory(root, exactDirectoryMode(0o755))
		}
		return finishPublishedTransaction(root, journal, operations)
	default:
		return errors.New("unsupported certificate publication journal stage")
	}
}

func validateCandidateOwner(path string, journal journalDocument) (os.FileInfo, error) {
	content, observed, err := readPinnedRegular(path, 0o600)
	if err != nil {
		return nil, errors.New("candidate owner is unverifiable")
	}
	var document candidateOwnerDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("candidate owner is malformed")
	}
	canonical, err := canonicalJSON(document)
	if err != nil || !bytes.Equal(content, canonical) {
		return nil, errors.New("candidate owner is noncanonical")
	}
	if document.Schema != 1 || document.TransactionID != journal.TransactionID || document.CandidatePath != journal.CandidatePath || document.VersionPath != journal.VersionPath {
		return nil, errors.New("candidate owner transaction binding conflicts")
	}
	return observed, nil
}

type materialObservation struct {
	directory string
	dirInfo   os.FileInfo
	files     map[string]os.FileInfo
}

func observeValidatedMaterial(path string, candidate bool) (materialObservation, error) {
	if err := validateTargetIdentity(path, candidate); err != nil {
		return materialObservation{}, err
	}
	dirInfo, ok := pinnedDirectory(path, 0o755)
	if !ok {
		return materialObservation{}, ErrUnsupportedForBootstrap
	}
	observation := materialObservation{directory: path, dirInfo: dirInfo, files: make(map[string]os.FileInfo, len(managedFiles))}
	for _, spec := range managedFiles {
		_, info, err := readPinnedRegular(filepath.Join(path, spec.name), spec.mode)
		if err != nil {
			return materialObservation{}, err
		}
		observation.files[spec.name] = info
	}
	return observation, nil
}

func (observation materialObservation) unchanged() bool {
	currentDir, ok := pinnedDirectory(observation.directory, 0o755)
	if !ok || !os.SameFile(observation.dirInfo, currentDir) {
		return false
	}
	for _, spec := range managedFiles {
		current, err := os.Lstat(filepath.Join(observation.directory, spec.name))
		if err != nil || !current.Mode().IsRegular() || current.Mode().Perm() != spec.mode || !os.SameFile(observation.files[spec.name], current) {
			return false
		}
	}
	return true
}

func reconcileJournalTemps(root string, operations publicationOperations) (journalDocument, bool, error) {
	entries, err := readPinnedDirectory(root, 0o755)
	if err != nil {
		return journalDocument{}, false, err
	}
	var temps []string
	for _, entry := range entries {
		if journalTempPattern.MatchString(entry.Name()) {
			temps = append(temps, filepath.Join(root, entry.Name()))
		}
	}
	if len(temps) > 1 {
		return journalDocument{}, false, errors.New("multiple certificate publication journal temps")
	}
	fixed, fixedErr := readJournal(filepath.Join(root, journalName))
	fixedExists := fixedErr == nil
	if fixedErr != nil && !errors.Is(fixedErr, os.ErrNotExist) {
		return journalDocument{}, false, fixedErr
	}
	if len(temps) == 0 {
		return fixed, fixedExists, nil
	}
	temporary, err := readJournal(temps[0])
	if err != nil {
		return journalDocument{}, false, err
	}
	tempNameMatch := journalTempPattern.FindStringSubmatch(filepath.Base(temps[0]))
	if tempNameMatch == nil || tempNameMatch[1] != temporary.TransactionID {
		return journalDocument{}, false, errors.New("certificate publication journal temp filename does not match transaction")
	}
	if !fixedExists {
		if temporary.Stage != "candidate_ready" || !candidateEvidenceMatches(root, temporary) {
			return journalDocument{}, false, errors.New("unmatched certificate publication journal temp")
		}
		if err := renamePinnedRegular(temps[0], filepath.Join(root, journalName), 0o600); err != nil {
			return journalDocument{}, false, err
		}
		if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
			return journalDocument{}, false, err
		}
		return temporary, true, nil
	}
	if !sameJournalImmutable(fixed, temporary) {
		return journalDocument{}, false, errors.New("certificate publication journal temp changed immutable fields")
	}
	if fixed.Stage == temporary.Stage {
		if err := removePinnedRegular(temps[0], 0o600); err != nil {
			return journalDocument{}, false, err
		}
		return fixed, true, syncPinnedDirectory(root, exactDirectoryMode(0o755))
	}
	if !legalAdjacentStage(fixed.Stage, temporary.Stage, fixed.DirectRootSHA256 != nil) {
		return journalDocument{}, false, errors.New("certificate publication journal temp stage is nonadjacent")
	}
	if err := renamePinnedRegular(temps[0], filepath.Join(root, journalName), 0o600); err != nil {
		return journalDocument{}, false, err
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return journalDocument{}, false, err
	}
	return temporary, true, nil
}

func readJournal(path string) (journalDocument, error) {
	content, _, err := readPinnedRegular(path, 0o600)
	if err != nil {
		return journalDocument{}, err
	}
	var document journalDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return journalDocument{}, errors.New("invalid certificate publication journal")
	}
	canonical, err := canonicalJSON(document)
	if err != nil || !bytes.Equal(content, canonical) {
		return journalDocument{}, errors.New("noncanonical certificate publication journal")
	}
	if err := validateJournal(document); err != nil {
		return journalDocument{}, err
	}
	return document, nil
}

func validateJournal(document journalDocument) error {
	if document.Schema != 1 || !hex128Pattern.MatchString(document.TransactionID) || document.CandidatePath != "versions/.candidate-"+document.TransactionID || document.VersionPath != "versions/v-"+document.TransactionID {
		return errors.New("invalid certificate publication journal identity")
	}
	if document.PredecessorVersion != nil && !safeVersionPath(*document.PredecessorVersion) {
		return errors.New("invalid certificate publication predecessor")
	}
	stages := map[string]bool{"candidate_ready": true, "version_ready": true, "current_published": true, "cleaning_direct_root": true, "retaining": true}
	if !stages[document.Stage] {
		return errors.New("invalid certificate publication journal stage")
	}
	if document.DirectRootSHA256 == nil {
		if document.Stage == "cleaning_direct_root" {
			return errors.New("direct-root cleanup stage lacks digests")
		}
		return nil
	}
	if len(document.DirectRootSHA256) != len(managedFiles) {
		return errors.New("invalid direct-root digest set")
	}
	for _, spec := range managedFiles {
		value, ok := document.DirectRootSHA256[spec.name]
		if !ok || len(value) != 64 || strings.ToLower(value) != value {
			return errors.New("invalid direct-root digest")
		}
		if _, err := hex.DecodeString(value); err != nil {
			return errors.New("invalid direct-root digest")
		}
	}
	return nil
}

func safeVersionPath(path string) bool {
	return filepath.ToSlash(path) == path && filepath.Dir(path) == "versions" && versionPattern.MatchString(filepath.Base(path))
}

func sameJournalImmutable(left, right journalDocument) bool {
	left.Stage, right.Stage = "", ""
	leftBytes, _ := canonicalJSON(left)
	rightBytes, _ := canonicalJSON(right)
	return bytes.Equal(leftBytes, rightBytes)
}

func legalAdjacentStage(from, to string, direct bool) bool {
	allowed := map[string]string{"candidate_ready": "version_ready", "version_ready": "current_published", "cleaning_direct_root": "retaining"}
	if from == "current_published" {
		if direct {
			return to == "cleaning_direct_root"
		}
		return to == "retaining"
	}
	return allowed[from] == to
}

func currentMatches(root string, predecessor *string) bool {
	current, err := readSafeCurrent(root)
	if predecessor == nil {
		return errors.Is(err, os.ErrNotExist) || !pathExists(filepath.Join(root, "current"))
	}
	return err == nil && current == *predecessor
}

func matchingCurrentTemp(root, target string) (string, error) {
	entries, err := readPinnedDirectory(root, 0o755)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".current-") || !hex128Pattern.MatchString(strings.TrimPrefix(entry.Name(), ".current-")) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		got, err := os.Readlink(path)
		if err != nil || got != target {
			return "", errors.New("unmatched current publication temp")
		}
		matches = append(matches, path)
	}
	if len(matches) > 1 {
		return "", errors.New("multiple current publication temps")
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", nil
}

func candidateEvidenceMatches(root string, document journalDocument) bool {
	candidate := filepath.Join(root, filepath.FromSlash(document.CandidatePath))
	owner := filepath.Join(candidate, ".candidate-owner.json")
	_, err := validateCandidateOwner(owner, document)
	return err == nil && validateTargetIdentity(candidate, true) == nil
}
