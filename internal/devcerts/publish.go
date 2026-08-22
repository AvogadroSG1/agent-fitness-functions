// Package devcerts publishes local development certificates managed by stack-fitness-functions.
package devcerts

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"time"
)

const (
	lockName        = ".certificate-publication.lock"
	journalName     = ".certificate-publication-transaction.json"
	validity        = 365 * 24 * time.Hour
	maxNameAttempts = 16
)

var (
	// ErrUnsupportedForBootstrap reports state intentionally deferred to later certificate lifecycle slices.
	ErrUnsupportedForBootstrap = errors.New("managed certificate state is unsupported for bootstrap")
	// ErrLockOwnerMismatch reports that lock release could not prove ownership and preserved the lock.
	ErrLockOwnerMismatch = errors.New("certificate publication lock ownership could not be verified")
	// ErrRandomNameCollisions reports exhaustion of bounded fresh-name retries.
	ErrRandomNameCollisions = errors.New("certificate publication random name collisions exhausted")

	hex128Pattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	versionPattern = regexp.MustCompile(`^v-[0-9a-f]{32}$`)
	managedFiles   = []fileSpec{
		{name: "ca.crt", mode: 0o644},
		{name: "server.crt", mode: 0o644},
		{name: "server.key", mode: 0o600},
		{name: "client.crt", mode: 0o644},
		{name: "client.key", mode: 0o600},
	}
)

type fileSpec struct {
	name string
	mode os.FileMode
	data []byte
}

type ownerDocument struct {
	Schema     int    `json:"schema"`
	Hostname   string `json:"hostname"`
	PID        int    `json:"pid"`
	Token      string `json:"token"`
	AcquiredAt string `json:"acquired_at"`
}

type candidateOwnerDocument struct {
	Schema        int    `json:"schema"`
	TransactionID string `json:"transaction_id"`
	CandidatePath string `json:"candidate_path"`
	VersionPath   string `json:"version_path"`
}

type journalDocument struct {
	Schema             int               `json:"schema"`
	TransactionID      string            `json:"transaction_id"`
	CandidatePath      string            `json:"candidate_path"`
	VersionPath        string            `json:"version_path"`
	PredecessorVersion *string           `json:"predecessor_version"`
	Stage              string            `json:"stage"`
	DirectRootSHA256   map[string]string `json:"direct_root_sha256"`
}

type publicationEvent struct {
	name string
}

type publicationObserver func(publicationEvent)

type randomIDFunc func() (string, error)

// Publish creates one target certificate publication in an empty managed root.
// Force is accepted for CLI compatibility but does not broaden this bootstrap slice.
func Publish(root string, force bool) (resultErr error) {
	return publish(root, force, nil)
}

func publish(root string, force bool, observer publicationObserver) (resultErr error) {
	return publishWithRandom(root, force, observer, randomID)
}

func publishWithRandom(root string, force bool, observer publicationObserver, nextID randomIDFunc) (resultErr error) {
	_ = force
	state, err := inspectRoot(root)
	if err != nil {
		return err
	}
	switch state {
	case rootPublished:
		return nil
	case rootEmpty:
		// Continue only for the bootstrap state implemented by this slice.
	default:
		return ErrUnsupportedForBootstrap
	}

	if err := prepareEmptyRoot(root); err != nil {
		return err
	}
	token, err := acquireLock(root, observer, nextID)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := releaseLock(root, token); releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		} else {
			emit(observer, "lock_released")
		}
	}()

	// Reclassification under the lock prevents bootstrap over evidence created by a competing writer.
	state, err = inspectRootWithLock(root)
	if err != nil {
		return err
	}
	if state != rootEmpty {
		return ErrUnsupportedForBootstrap
	}
	return publishFirst(root, observer, nextID)
}

func emit(observer publicationObserver, name string) {
	if observer != nil {
		observer(publicationEvent{name: name})
	}
}

type rootState uint8

const (
	rootUnsupported rootState = iota
	rootEmpty
	rootPublished
)

func inspectRoot(root string) (rootState, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return rootEmpty, validateParent(root)
	}
	if err != nil {
		return rootUnsupported, fmt.Errorf("inspect managed certificate root: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o755 {
		return rootUnsupported, nil
	}
	if _, ok := pinnedDirectory(root, 0o755); !ok {
		return rootUnsupported, nil
	}
	entries, err := readPinnedDirectory(root, 0o755)
	if err != nil {
		return rootUnsupported, fmt.Errorf("read managed certificate root: %w", err)
	}
	if len(entries) == 0 || onlySafeGitignore(root, entries) {
		return rootEmpty, nil
	}
	if ((len(entries) == 1 && entries[0].Name() == "versions") ||
		(len(entries) == 2 && entries[0].Name() == ".gitignore" && entries[1].Name() == "versions" && safeGitignore(root))) && validEmptyVersions(root) {
		return rootEmpty, nil
	}
	cleanPublishedNames := len(entries) == 2 && entries[0].Name() == "current" && entries[1].Name() == "versions"
	cleanPublishedNames = cleanPublishedNames || (len(entries) == 3 && entries[0].Name() == ".gitignore" && entries[1].Name() == "current" && entries[2].Name() == "versions" && safeGitignore(root))
	if cleanPublishedNames {
		if fastPathPublished(root) {
			return rootPublished, nil
		}
	}
	return rootUnsupported, nil
}

func inspectRootWithLock(root string) (rootState, error) {
	entries, err := readPinnedDirectory(root, 0o755)
	if err != nil {
		return rootUnsupported, fmt.Errorf("read managed certificate root under lock: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() != lockName {
			names = append(names, entry.Name())
		}
	}
	cleanEmptyNames := len(names) == 1 && names[0] == "versions"
	cleanEmptyNames = cleanEmptyNames || (len(names) == 2 && names[0] == ".gitignore" && names[1] == "versions" && safeGitignore(root))
	if cleanEmptyNames && validEmptyVersions(root) {
		return rootEmpty, nil
	}
	return rootUnsupported, nil
}

func onlySafeGitignore(root string, entries []os.DirEntry) bool {
	return len(entries) == 1 && entries[0].Name() == ".gitignore" && safeGitignore(root)
}

func safeGitignore(root string) bool {
	info, err := os.Lstat(filepath.Join(root, ".gitignore"))
	return err == nil && info.Mode().IsRegular()
}

func validateParent(root string) error {
	parent := filepath.Dir(filepath.Clean(root))
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect managed certificate root parent: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsupportedForBootstrap
	}
	if _, ok := pinnedDirectoryAnyMode(parent); !ok {
		return ErrUnsupportedForBootstrap
	}
	return nil
}

func validEmptyVersions(root string) bool {
	path := filepath.Join(root, "versions")
	if _, ok := pinnedDirectory(path, 0o755); !ok {
		return false
	}
	entries, err := readPinnedDirectory(path, 0o755)
	return err == nil && len(entries) == 0
}

func prepareEmptyRoot(root string) error {
	_, err := ensureDirectory(root, 0o755, os.Mkdir)
	if err != nil {
		return fmt.Errorf("prepare managed certificate root: %w", err)
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	if err := syncPinnedDirectory(filepath.Dir(root), anyDirectoryMode()); err != nil {
		return err
	}
	versions := filepath.Join(root, "versions")
	_, err = ensureDirectory(versions, 0o755, os.Mkdir)
	if err != nil {
		return fmt.Errorf("prepare certificate versions directory: %w", err)
	}
	if err := syncPinnedDirectory(versions, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	return syncPinnedDirectory(root, exactDirectoryMode(0o755))
}

func ensureDirectory(path string, mode os.FileMode, mkdir func(string, os.FileMode) error) (bool, error) {
	err := mkdir(path, mode)
	created := err == nil
	if err != nil && !errors.Is(err, os.ErrExist) {
		return false, err
	}
	if created {
		if err := os.Chmod(path, mode); err != nil {
			return false, err
		}
	}
	if _, ok := pinnedDirectory(path, mode); !ok {
		return false, ErrUnsupportedForBootstrap
	}
	return created, nil
}

func pinnedDirectory(path string, mode os.FileMode) (os.FileInfo, bool) {
	file, observed, err := openPinnedDirectory(path, exactDirectoryMode(mode), os.Open)
	if err != nil {
		return nil, false
	}
	if err := file.Close(); err != nil {
		return nil, false
	}
	return observed, true
}

func pinnedDirectoryAnyMode(path string) (os.FileInfo, bool) {
	file, observed, err := openPinnedDirectory(path, anyDirectoryMode(), os.Open)
	if err != nil {
		return nil, false
	}
	if err := file.Close(); err != nil {
		return nil, false
	}
	return observed, true
}

type directoryMode struct {
	exact bool
	mode  os.FileMode
}

func exactDirectoryMode(mode os.FileMode) directoryMode {
	return directoryMode{exact: true, mode: mode}
}

func anyDirectoryMode() directoryMode {
	return directoryMode{}
}

func openPinnedDirectory(path string, required directoryMode, opener func(string) (*os.File, error)) (*os.File, os.FileInfo, error) {
	observed, err := os.Lstat(path)
	if err != nil || !observed.IsDir() || observed.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrUnsupportedForBootstrap
	}
	if required.exact && observed.Mode().Perm() != required.mode {
		return nil, nil, ErrUnsupportedForBootstrap
	}
	directory, err := opener(path)
	if err != nil {
		return nil, nil, err
	}
	pinned, err := directory.Stat()
	if err != nil || !pinned.IsDir() || pinned.Mode()&os.ModeSymlink != 0 || required.exact && pinned.Mode().Perm() != required.mode || !os.SameFile(observed, pinned) {
		_ = directory.Close()
		return nil, nil, ErrUnsupportedForBootstrap
	}
	reobserved, err := os.Lstat(path)
	if err != nil || !reobserved.IsDir() || reobserved.Mode()&os.ModeSymlink != 0 || required.exact && reobserved.Mode().Perm() != required.mode || !os.SameFile(observed, reobserved) {
		_ = directory.Close()
		return nil, nil, ErrUnsupportedForBootstrap
	}
	return directory, observed, nil
}

func readPinnedDirectory(path string, mode os.FileMode) ([]os.DirEntry, error) {
	directory, _, err := openPinnedDirectory(path, exactDirectoryMode(mode), os.Open)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func syncPinnedDirectory(path string, required directoryMode) error {
	directory, _, err := openPinnedDirectory(path, required, os.Open)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !(runtime.GOOS == "windows" && errors.Is(err, os.ErrInvalid)) {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func acquireLock(root string, observer publicationObserver, nextID randomIDFunc) (string, error) {
	lock := filepath.Join(root, lockName)
	if err := os.Mkdir(lock, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrUnsupportedForBootstrap
		}
		return "", fmt.Errorf("acquire certificate publication lock: %w", err)
	}
	if err := os.Chmod(lock, 0o700); err != nil {
		return "", errors.New("set certificate publication lock mode")
	}
	if _, ok := pinnedDirectory(lock, 0o700); !ok {
		return "", ErrUnsupportedForBootstrap
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "", errors.New("determine certificate publication hostname")
	}
	acquiredAt := time.Now().UTC().Format(time.RFC3339Nano)
	token, temp, err := writeRandomExclusive(nextID, func(id string) string {
		return filepath.Join(lock, "owner."+id+".tmp")
	}, func(id string) ([]byte, error) {
		return canonicalJSON(ownerDocument{Schema: 1, Hostname: hostname, PID: os.Getpid(), Token: id, AcquiredAt: acquiredAt})
	}, 0o600)
	if err != nil {
		if errors.Is(err, ErrRandomNameCollisions) {
			return "", ErrRandomNameCollisions
		}
		return "", errors.New("write certificate publication owner")
	}
	if err := renamePinnedRegular(temp, filepath.Join(lock, "owner.json"), 0o600); err != nil {
		return "", errors.New("publish certificate publication owner")
	}
	if err := syncPinnedDirectory(lock, exactDirectoryMode(0o700)); err != nil {
		return "", errors.New("sync certificate publication lock")
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return "", err
	}
	emit(observer, "lock_owner_published")
	return token, nil
}

func releaseLock(root, token string) error {
	lock := filepath.Join(root, lockName)
	if !canonicalOwnerMatches(filepath.Join(lock, "owner.json"), token) {
		return ErrLockOwnerMismatch
	}
	release := filepath.Join(root, ".certificate-publication.release-"+token)
	if err := renamePinnedDirectory(lock, release, 0o700); err != nil {
		return errors.New("rename certificate publication lock for release")
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	if !canonicalOwnerMatches(filepath.Join(release, "owner.json"), token) {
		return ErrLockOwnerMismatch
	}
	entries, err := readPinnedDirectory(release, 0o700)
	if err != nil || len(entries) != 1 || entries[0].Name() != "owner.json" {
		return ErrLockOwnerMismatch
	}
	if err := removePinnedRegular(filepath.Join(release, "owner.json"), 0o600); err != nil {
		return errors.New("remove certificate publication owner")
	}
	if err := removePinnedDirectory(release, 0o700); err != nil {
		return errors.New("remove certificate publication lock")
	}
	return syncPinnedDirectory(root, exactDirectoryMode(0o755))
}

func canonicalOwnerMatches(path, token string) bool {
	content, _, err := readPinnedRegular(path, 0o600)
	if err != nil {
		return false
	}
	var document ownerDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	canonical, err := canonicalJSON(document)
	if err != nil || !bytes.Equal(content, canonical) {
		return false
	}
	if document.Schema != 1 || document.Hostname == "" || document.PID <= 0 || document.Token != token || !hex128Pattern.MatchString(document.Token) {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, document.AcquiredAt)
	return err == nil && parsed.Location() == time.UTC
}

func publishFirst(root string, observer publicationObserver, nextID randomIDFunc) error {
	transactionID, candidate, err := createCandidateDirectory(root, nextID)
	if err != nil {
		return err
	}
	candidateRelative := "versions/.candidate-" + transactionID
	versionRelative := "versions/v-" + transactionID
	version := filepath.Join(root, filepath.FromSlash(versionRelative))
	emit(observer, "candidate_created")
	owner := candidateOwnerDocument{Schema: 1, TransactionID: transactionID, CandidatePath: candidateRelative, VersionPath: versionRelative}
	ownerContent, err := canonicalJSON(owner)
	if err != nil {
		return fmt.Errorf("encode certificate candidate owner: %w", err)
	}
	_, ownerTemp, err := writeRandomExclusive(nextID, func(id string) string {
		return filepath.Join(candidate, ".candidate-owner."+id+".tmp")
	}, func(string) ([]byte, error) { return ownerContent, nil }, 0o600)
	if err != nil {
		if errors.Is(err, ErrRandomNameCollisions) {
			return ErrRandomNameCollisions
		}
		return errors.New("write certificate candidate owner")
	}
	ownerPath := filepath.Join(candidate, ".candidate-owner.json")
	if err := renamePinnedRegular(ownerTemp, ownerPath, 0o600); err != nil {
		return errors.New("publish certificate candidate owner")
	}
	if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "candidate_owner_published")

	files, err := generateMaterial()
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := writeExclusive(filepath.Join(candidate, file.name), file.data, file.mode); err != nil {
			return fmt.Errorf("write generated development certificate %s: %w", file.name, err)
		}
	}
	if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "candidate_material_synced")
	if err := validateMaterial(candidate, true); err != nil {
		return fmt.Errorf("validate certificate candidate: %w", err)
	}
	pinnedOwner, _, err := readPinnedRegular(ownerPath, 0o600)
	if err != nil || !bytes.Equal(pinnedOwner, ownerContent) {
		return ErrUnsupportedForBootstrap
	}
	emit(observer, "candidate_validated")
	if err := syncPinnedDirectory(filepath.Join(root, "versions"), exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "versions_synced_before_candidate_ready")

	journal := journalDocument{
		Schema:             1,
		TransactionID:      transactionID,
		CandidatePath:      candidateRelative,
		VersionPath:        versionRelative,
		PredecessorVersion: nil,
		Stage:              "candidate_ready",
		DirectRootSHA256:   nil,
	}
	if err := replaceJournal(root, journal, observer, nextID); err != nil {
		return err
	}
	if err := removePinnedRegular(ownerPath, 0o600); err != nil {
		return fmt.Errorf("remove certificate candidate owner: %w", err)
	}
	if err := syncPinnedDirectory(candidate, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "candidate_owner_removed")
	if err := renamePinnedDirectory(candidate, version, 0o755); err != nil {
		return fmt.Errorf("publish certificate version: %w", err)
	}
	if err := syncPinnedDirectory(filepath.Join(root, "versions"), exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "version_renamed")
	journal.Stage = "version_ready"
	if err := replaceJournal(root, journal, observer, nextID); err != nil {
		return err
	}

	_, currentTemp, err := createRandomSymlink(nextID, func(id string) string {
		return filepath.Join(root, ".current-"+id)
	}, versionRelative)
	if err != nil {
		return fmt.Errorf("create current certificate publication: %w", err)
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "current_temp_created")
	if err := renamePinnedSymlink(currentTemp, filepath.Join(root, "current"), versionRelative); err != nil {
		return fmt.Errorf("publish current certificate version: %w", err)
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "current_renamed")
	journal.Stage = "current_published"
	if err := replaceJournal(root, journal, observer, nextID); err != nil {
		return err
	}
	journal.Stage = "retaining"
	if err := replaceJournal(root, journal, observer, nextID); err != nil {
		return err
	}
	if err := syncPinnedDirectory(filepath.Join(root, "versions"), exactDirectoryMode(0o755)); err != nil {
		return err
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "retention_synced")
	if err := removePinnedRegular(filepath.Join(root, journalName), 0o600); err != nil {
		return fmt.Errorf("complete certificate publication journal: %w", err)
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "journal_removed")
	return nil
}

func replaceJournal(root string, document journalDocument, observer publicationObserver, nextID randomIDFunc) error {
	content, err := canonicalJSON(document)
	if err != nil {
		return fmt.Errorf("encode certificate publication journal: %w", err)
	}
	_, temp, err := writeRandomExclusive(nextID, func(id string) string {
		return filepath.Join(root, ".certificate-publication.transaction-"+document.TransactionID+"-"+id+".tmp")
	}, func(string) ([]byte, error) { return content, nil }, 0o600)
	if err != nil {
		return fmt.Errorf("write certificate publication journal: %w", err)
	}
	if err := renamePinnedRegular(temp, filepath.Join(root, journalName), 0o600); err != nil {
		return fmt.Errorf("replace certificate publication journal: %w", err)
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return err
	}
	emit(observer, "journal_"+document.Stage)
	return nil
}

func validatePublished(root string) error {
	current := filepath.Join(root, "current")
	currentInfo, err := os.Lstat(current)
	if err != nil || currentInfo.Mode()&os.ModeSymlink == 0 {
		return ErrUnsupportedForBootstrap
	}
	target, err := os.Readlink(current)
	if err != nil || filepath.ToSlash(target) != target {
		return ErrUnsupportedForBootstrap
	}
	if filepath.IsAbs(target) || filepath.Dir(target) != "versions" || !versionPattern.MatchString(filepath.Base(target)) {
		return ErrUnsupportedForBootstrap
	}
	versions := filepath.Join(root, "versions")
	if _, ok := pinnedDirectory(versions, 0o755); !ok {
		return ErrUnsupportedForBootstrap
	}
	entries, err := readPinnedDirectory(versions, 0o755)
	if err != nil || len(entries) < 1 || len(entries) > 2 {
		return ErrUnsupportedForBootstrap
	}
	currentFound := false
	for _, entry := range entries {
		if !versionPattern.MatchString(entry.Name()) || entry.Name() == filepath.Base(target) && currentFound {
			return ErrUnsupportedForBootstrap
		}
		if entry.Name() == filepath.Base(target) {
			currentFound = true
		}
		if err := validateVersion(filepath.Join(versions, entry.Name())); err != nil {
			return err
		}
	}
	if !currentFound {
		return ErrUnsupportedForBootstrap
	}
	return nil
}

type publishedObservation struct {
	target   string
	current  os.FileInfo
	versions []versionObservation
}

type versionObservation struct {
	name  string
	dir   os.FileInfo
	files []os.FileInfo
}

// fastPathPublished follows ADR-0004's lock-free validation order. Any unstable
// observation falls back to the conservative bootstrap refusal path.
func fastPathPublished(root string) bool {
	journal := filepath.Join(root, journalName)
	lock := filepath.Join(root, lockName)
	if !pathAbsent(journal) || !pathAbsent(lock) {
		return false
	}
	first, ok := observePublished(root)
	if !ok || !pathAbsent(lock) || !pathAbsent(journal) {
		return false
	}
	second, ok := observePublished(root)
	if !ok || first.target != second.target || !os.SameFile(first.current, second.current) || len(first.versions) != len(second.versions) {
		return false
	}
	for i := range first.versions {
		if first.versions[i].name != second.versions[i].name || !os.SameFile(first.versions[i].dir, second.versions[i].dir) || len(first.versions[i].files) != len(second.versions[i].files) {
			return false
		}
		for j := range first.versions[i].files {
			if !os.SameFile(first.versions[i].files[j], second.versions[i].files[j]) {
				return false
			}
		}
	}
	return pathAbsent(lock)
}

func observePublished(root string) (publishedObservation, bool) {
	if err := validatePublished(root); err != nil {
		return publishedObservation{}, false
	}
	currentPath := filepath.Join(root, "current")
	current, err := os.Lstat(currentPath)
	if err != nil || current.Mode()&os.ModeSymlink == 0 {
		return publishedObservation{}, false
	}
	target, err := os.Readlink(currentPath)
	if err != nil {
		return publishedObservation{}, false
	}
	entries, err := readPinnedDirectory(filepath.Join(root, "versions"), 0o755)
	if err != nil {
		return publishedObservation{}, false
	}
	observation := publishedObservation{target: target, current: current, versions: make([]versionObservation, 0, len(entries))}
	for _, entry := range entries {
		versionPath := filepath.Join(root, "versions", entry.Name())
		version, ok := pinnedDirectory(versionPath, 0o755)
		if !ok {
			return publishedObservation{}, false
		}
		observedVersion := versionObservation{name: entry.Name(), dir: version, files: make([]os.FileInfo, 0, len(managedFiles))}
		for _, spec := range managedFiles {
			_, info, err := readPinnedRegular(filepath.Join(versionPath, spec.name), spec.mode)
			if err != nil {
				return publishedObservation{}, false
			}
			observedVersion.files = append(observedVersion.files, info)
		}
		observation.versions = append(observation.versions, observedVersion)
	}
	return observation, true
}

func pathAbsent(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, os.ErrNotExist)
}

func validateVersion(path string) error {
	return validateMaterial(path, false)
}

func validateMaterial(path string, candidate bool) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
		return ErrUnsupportedForBootstrap
	}
	entries, err := readPinnedDirectory(path, 0o755)
	wantNames := []string{"ca.crt", "client.crt", "client.key", "server.crt", "server.key"}
	if candidate {
		wantNames = append([]string{".candidate-owner.json"}, wantNames...)
	}
	if err != nil || len(entries) != len(wantNames) {
		return ErrUnsupportedForBootstrap
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	if !equalStrings(names, wantNames) {
		return ErrUnsupportedForBootstrap
	}
	if candidate {
		if _, _, err := readPinnedRegular(filepath.Join(path, ".candidate-owner.json"), 0o600); err != nil {
			return ErrUnsupportedForBootstrap
		}
	}
	for _, spec := range managedFiles {
		fileInfo, err := os.Lstat(filepath.Join(path, spec.name))
		if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != spec.mode {
			return ErrUnsupportedForBootstrap
		}
	}
	ca, err := parseCertificateFile(filepath.Join(path, "ca.crt"))
	if err != nil {
		return ErrUnsupportedForBootstrap
	}
	server, err := parseCertificateFile(filepath.Join(path, "server.crt"))
	if err != nil {
		return ErrUnsupportedForBootstrap
	}
	client, err := parseCertificateFile(filepath.Join(path, "client.crt"))
	if err != nil {
		return ErrUnsupportedForBootstrap
	}
	now := time.Now()
	if !exactTargetProfiles(ca, server, client, now) {
		return ErrUnsupportedForBootstrap
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := server.Verify(x509.VerifyOptions{Roots: pool, DNSName: "agent-fitness-functions", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return ErrUnsupportedForBootstrap
	}
	if _, err := client.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return ErrUnsupportedForBootstrap
	}
	if !keyMatches(filepath.Join(path, "server.key"), server) || !keyMatches(filepath.Join(path, "client.key"), client) {
		return ErrUnsupportedForBootstrap
	}
	return nil
}

func generateMaterial() ([]fileSpec, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errors.New("generate development CA key")
	}
	now := time.Now().UTC().Truncate(time.Second)
	notBefore := now.Add(-time.Hour)
	notAfter := notBefore.Add(validity)
	caSerial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	serverSerial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	clientSerial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "agent-fitness-functions-dev-ca"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, errors.New("create development CA certificate")
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, errors.New("parse generated development CA certificate")
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial, Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "agent-fitness-functions"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: clientSerial, Subject: pkix.Name{CommonName: "dev-hook-pool"},
		NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	serverCert, serverKey, err := createLeaf(serverTemplate, ca, caKey)
	if err != nil {
		return nil, err
	}
	clientCert, clientKey, err := createLeaf(clientTemplate, ca, caKey)
	if err != nil {
		return nil, err
	}
	return []fileSpec{
		{name: "ca.crt", mode: 0o644, data: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})},
		{name: "server.crt", mode: 0o644, data: serverCert},
		{name: "server.key", mode: 0o600, data: serverKey},
		{name: "client.crt", mode: 0o644, data: clientCert},
		{name: "client.key", mode: 0o600, data: clientKey},
	}, nil
}

func exactCertificateLifetime(cert *x509.Certificate, now time.Time) bool {
	return cert.NotAfter.Sub(cert.NotBefore) == validity && !now.Before(cert.NotBefore) && now.Before(cert.NotAfter)
}

func exactTargetProfiles(ca, server, client *x509.Certificate, now time.Time) bool {
	validCA := exactCertificateLifetime(ca, now) && exactCommonName(ca.Subject, "agent-fitness-functions-dev-ca") && ca.IsCA && ca.BasicConstraintsValid && ca.KeyUsage == x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature && len(ca.ExtKeyUsage) == 0 && len(ca.UnknownExtKeyUsage) == 0 && !hasNames(ca)
	validServer := exactCertificateLifetime(server, now) && exactCommonName(server.Subject, "localhost") && !server.IsCA && server.KeyUsage == x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment && equalExtUsage(server.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) && len(server.UnknownExtKeyUsage) == 0 && equalStrings(server.DNSNames, []string{"localhost", "agent-fitness-functions"}) && equalIPs(server.IPAddresses, []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}) && len(server.EmailAddresses) == 0 && len(server.URIs) == 0
	validClient := exactCertificateLifetime(client, now) && exactCommonName(client.Subject, "dev-hook-pool") && !client.IsCA && client.KeyUsage == x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment && equalExtUsage(client.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}) && len(client.UnknownExtKeyUsage) == 0 && !hasNames(client)
	return validCA && validServer && validClient
}

func exactCommonName(name pkix.Name, commonName string) bool {
	return name.CommonName == commonName && len(name.Country) == 0 && len(name.Organization) == 0 && len(name.OrganizationalUnit) == 0 && len(name.Locality) == 0 && len(name.Province) == 0 && len(name.StreetAddress) == 0 && len(name.PostalCode) == 0 && name.SerialNumber == "" && len(name.Names) == 1 && len(name.ExtraNames) == 0
}

func hasNames(cert *x509.Certificate) bool {
	return len(cert.DNSNames) != 0 || len(cert.IPAddresses) != 0 || len(cert.EmailAddresses) != 0 || len(cert.URIs) != 0
}

func createLeaf(template, ca *x509.Certificate, caKey *rsa.PrivateKey) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, errors.New("generate development leaf key")
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, errors.New("create development leaf certificate")
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), nil
}

func randomSerial() (*big.Int, error) {
	return randomSerialWith(func(limit *big.Int) (*big.Int, error) {
		return rand.Int(rand.Reader, limit)
	})
}

func randomSerialWith(draw func(*big.Int) (*big.Int, error)) (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	for range maxNameAttempts {
		serial, err := draw(limit)
		if err != nil {
			return nil, errors.New("generate development certificate serial")
		}
		if serial != nil && serial.Sign() > 0 {
			return serial, nil
		}
	}
	return nil, errors.New("generate positive development certificate serial")
}

func parseCertificateFile(path string) (*x509.Certificate, error) {
	content, _, err := readPinnedRegular(path, 0o644)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return nil, errors.New("invalid certificate PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

func keyMatches(path string, cert *x509.Certificate) bool {
	content, _, err := readPinnedRegular(path, 0o600)
	if err != nil {
		return false
	}
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "RSA PRIVATE KEY" || len(rest) != 0 {
		return false
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return false
	}
	public, ok := cert.PublicKey.(*rsa.PublicKey)
	return ok && key.PublicKey.N.Cmp(public.N) == 0 && key.PublicKey.E == public.E
}

func writeExclusive(path string, content []byte, mode os.FileMode) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return os.ErrExist
		}
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	observed, err := os.Lstat(path)
	if err != nil || !observed.Mode().IsRegular() || observed.Mode().Perm() != mode || observed.Mode()&os.ModeSymlink != 0 {
		return ErrUnsupportedForBootstrap
	}
	pinned, err := file.Stat()
	if err != nil || !pinned.Mode().IsRegular() || pinned.Mode().Perm() != mode || !os.SameFile(observed, pinned) {
		return ErrUnsupportedForBootstrap
	}
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	reobserved, err := os.Lstat(path)
	if err != nil || !reobserved.Mode().IsRegular() || reobserved.Mode().Perm() != mode || !os.SameFile(observed, reobserved) {
		return ErrUnsupportedForBootstrap
	}
	repinned, err := file.Stat()
	if err != nil || !repinned.Mode().IsRegular() || repinned.Mode().Perm() != mode || !os.SameFile(observed, repinned) {
		return ErrUnsupportedForBootstrap
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func readPinnedRegular(path string, mode os.FileMode) ([]byte, os.FileInfo, error) {
	observed, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !observed.Mode().IsRegular() || observed.Mode().Perm() != mode || observed.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrUnsupportedForBootstrap
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	pinned, err := file.Stat()
	if err != nil || !pinned.Mode().IsRegular() || pinned.Mode().Perm() != mode || !os.SameFile(observed, pinned) {
		return nil, nil, ErrUnsupportedForBootstrap
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}
	reobserved, err := os.Lstat(path)
	if err != nil || !reobserved.Mode().IsRegular() || reobserved.Mode().Perm() != mode || !os.SameFile(observed, reobserved) {
		return nil, nil, ErrUnsupportedForBootstrap
	}
	repinned, err := file.Stat()
	if err != nil || !repinned.Mode().IsRegular() || repinned.Mode().Perm() != mode || !os.SameFile(observed, repinned) {
		return nil, nil, ErrUnsupportedForBootstrap
	}
	return content, observed, nil
}

func renamePinnedRegular(source, destination string, mode os.FileMode) error {
	if _, _, err := readPinnedRegular(source, mode); err != nil {
		return err
	}
	if err := validateRegularDestination(destination, mode); err != nil {
		return err
	}
	return os.Rename(source, destination)
}

func validateRegularDestination(path string, mode os.FileMode) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, _, err := readPinnedRegular(path, mode); err != nil {
		return ErrUnsupportedForBootstrap
	}
	return nil
}

func removePinnedRegular(path string, mode os.FileMode) error {
	if _, _, err := readPinnedRegular(path, mode); err != nil {
		return err
	}
	return os.Remove(path)
}

func renamePinnedDirectory(source, destination string, mode os.FileMode) error {
	if _, ok := pinnedDirectory(source, mode); !ok {
		return ErrUnsupportedForBootstrap
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return ErrUnsupportedForBootstrap
	}
	return os.Rename(source, destination)
}

func removePinnedDirectory(path string, mode os.FileMode) error {
	if _, ok := pinnedDirectory(path, mode); !ok {
		return ErrUnsupportedForBootstrap
	}
	return os.Remove(path)
}

func renamePinnedSymlink(source, destination, target string) error {
	observed, err := os.Lstat(source)
	if err != nil || observed.Mode()&os.ModeSymlink == 0 {
		return ErrUnsupportedForBootstrap
	}
	gotTarget, err := os.Readlink(source)
	if err != nil || gotTarget != target {
		return ErrUnsupportedForBootstrap
	}
	reobserved, err := os.Lstat(source)
	if err != nil || reobserved.Mode()&os.ModeSymlink == 0 || !os.SameFile(observed, reobserved) {
		return ErrUnsupportedForBootstrap
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return ErrUnsupportedForBootstrap
	}
	return os.Rename(source, destination)
}

func canonicalJSON(value any) ([]byte, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func writeRandomExclusive(nextID randomIDFunc, pathForID func(string) string, contentForID func(string) ([]byte, error), mode os.FileMode) (string, string, error) {
	for range maxNameAttempts {
		id, err := nextID()
		if err != nil {
			return "", "", err
		}
		if !hex128Pattern.MatchString(id) {
			return "", "", errors.New("invalid certificate publication random identifier")
		}
		content, err := contentForID(id)
		if err != nil {
			return "", "", err
		}
		path := pathForID(id)
		if err := writeExclusive(path, content, mode); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", "", err
		}
		return id, path, nil
	}
	return "", "", ErrRandomNameCollisions
}

func createCandidateDirectory(root string, nextID randomIDFunc) (string, string, error) {
	versions := filepath.Join(root, "versions")
	for range maxNameAttempts {
		id, err := nextID()
		if err != nil {
			return "", "", err
		}
		if !hex128Pattern.MatchString(id) {
			return "", "", errors.New("invalid certificate publication random identifier")
		}
		candidate := filepath.Join(versions, ".candidate-"+id)
		version := filepath.Join(versions, "v-"+id)
		currentTemp := filepath.Join(root, ".current-"+id)
		collision, err := anyPathExists(candidate, version, currentTemp)
		if err != nil {
			return "", "", err
		}
		if collision {
			continue
		}
		if err := os.Mkdir(candidate, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", "", err
		}
		if err := os.Chmod(candidate, 0o755); err != nil {
			return "", "", err
		}
		if _, ok := pinnedDirectory(candidate, 0o755); !ok {
			return "", "", ErrUnsupportedForBootstrap
		}
		return id, candidate, nil
	}
	return "", "", ErrRandomNameCollisions
}

func createRandomSymlink(nextID randomIDFunc, pathForID func(string) string, target string) (string, string, error) {
	for range maxNameAttempts {
		id, err := nextID()
		if err != nil {
			return "", "", err
		}
		if !hex128Pattern.MatchString(id) {
			return "", "", errors.New("invalid certificate publication random identifier")
		}
		path := pathForID(id)
		if err := os.Symlink(target, path); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", "", err
		}
		return id, path, nil
	}
	return "", "", ErrRandomNameCollisions
}

func anyPathExists(paths ...string) (bool, error) {
	for _, path := range paths {
		_, err := os.Lstat(path)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return false, err
		}
	}
	return false, nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("generate certificate publication identifier")
	}
	return hex.EncodeToString(value), nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalExtUsage(left, right []x509.ExtKeyUsage) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalIPs(left, right []net.IP) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !left[i].Equal(right[i]) {
			return false
		}
	}
	return true
}
