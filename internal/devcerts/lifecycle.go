package devcerts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type publicationOperations struct {
	now        func() time.Time
	sleep      func(time.Duration)
	nextID     randomIDFunc
	observer   publicationObserver
	checkpoint func(string) error
	hostname   func() (string, error)
	pid        int
	process    func(int) processState
	mkdir      func(string, os.FileMode) error
}

type processState uint8

const (
	processUnverifiable processState = iota
	processLive
	processDead
)

func defaultOperations() publicationOperations {
	return publicationOperations{
		now:        time.Now,
		sleep:      time.Sleep,
		nextID:     randomID,
		checkpoint: func(string) error { return nil },
		hostname:   os.Hostname,
		pid:        os.Getpid(),
		process:    operatingSystemProcessState,
		mkdir:      os.Mkdir,
	}
}

func publishWithOperations(root string, force bool, operations publicationOperations) (resultErr error) {
	hasRecoveryEvidence := pathExists(filepath.Join(root, lockName)) || pathExists(filepath.Join(root, journalName)) || hasTransactionTemp(root) || hasCandidateArtifact(root) || hasLockCleanupArtifact(root)
	state, err := classifyRoot(root, operations.now(), false)
	if err != nil {
		return fmt.Errorf("classify managed certificate root: %w", err)
	}
	if state.state == statePublishedTarget && state.fresh && fastPathPublished(root) {
		return nil
	}
	if !hasRecoveryEvidence && (state.unsafe || refusalRequired(state.state, force)) {
		firstObservation, firstStable := observeManagedTree(root)
		if err := operations.checkpoint("prelock_classified"); err != nil {
			return err
		}
		reclassified, err := classifyRoot(root, operations.now(), false)
		secondObservation, secondStable := observeManagedTree(root)
		if err == nil && firstStable && secondStable && sameClassification(state, reclassified) && sameTreeObservation(firstObservation, secondObservation) {
			return ErrUnsupportedForBootstrap
		}
		state = reclassified
	}
	if pathExists(filepath.Join(root, journalName)) && !pathExists(filepath.Join(root, "versions")) {
		return ErrUnsupportedForBootstrap
	}
	if err := prepareRootForLifecycle(root); err != nil {
		return err
	}
	token, err := acquireLockWithWait(root, operations)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := releaseLock(root, token); releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		} else {
			emit(operations.observer, "lock_released")
		}
	}()

	if err := recoverPublication(root, operations); err != nil {
		return err
	}
	state, err = classifyRoot(root, operations.now(), true)
	if err != nil {
		return fmt.Errorf("classify managed certificate root under lock: %w", err)
	}
	if state.state == statePublishedTarget && state.fresh {
		return nil
	}
	if state.unsafe || refusalRequired(state.state, force) {
		return ErrUnsupportedForBootstrap
	}

	predecessor := state.current
	var material []fileSpec
	var directDigests map[string]string
	if len(state.directFiles) > 0 {
		directDigests = digestDirectState(state.directFiles)
	}
	if state.state == stateLegacyDirectRootTarget && state.fresh {
		material = state.directFiles
	} else {
		material, err = generateMaterialAt(operations.now())
		if err != nil {
			return err
		}
	}
	if err := publishTransaction(root, material, predecessor, directDigests, operations); err != nil {
		return err
	}
	return nil
}

type observedPath struct {
	relative string
	info     os.FileInfo
	target   string
}

func observeManagedTree(root string) ([]observedPath, bool) {
	var observed []observedPath
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := observedPath{relative: relative, info: info}
		if info.Mode()&os.ModeSymlink != 0 {
			item.target, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		observed = append(observed, item)
		return nil
	})
	return observed, err == nil
}

func sameTreeObservation(left, right []observedPath) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftInfo, rightInfo := left[index].info, right[index].info
		if left[index].relative != right[index].relative || left[index].target != right[index].target || leftInfo.Mode() != rightInfo.Mode() || leftInfo.Size() != rightInfo.Size() || !leftInfo.ModTime().Equal(rightInfo.ModTime()) || !os.SameFile(leftInfo, rightInfo) {
			return false
		}
	}
	return true
}

func sameClassification(left, right classifiedRoot) bool {
	if left.state != right.state || left.fresh != right.fresh || left.unsafe != right.unsafe || !sameOptionalPath(left.current, right.current) {
		return false
	}
	return materialBytesEqual(left.directFiles, right.directFiles)
}

func sameOptionalPath(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func hasTransactionTemp(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if journalTempPattern.MatchString(entry.Name()) {
			return true
		}
	}
	return false
}

func hasCandidateArtifact(root string) bool {
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".candidate-") {
			return true
		}
	}
	return false
}

func hasLockCleanupArtifact(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		for _, prefix := range []string{".certificate-publication.release-", ".certificate-publication.reap-"} {
			if strings.HasPrefix(entry.Name(), prefix) && hex128Pattern.MatchString(strings.TrimPrefix(entry.Name(), prefix)) {
				return true
			}
		}
	}
	return false
}

func refusalRequired(state lifecycleState, force bool) bool {
	return (state == statePartial || state == stateUnknownComplete) && !force
}

func prepareRootForLifecycle(root string) error {
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
