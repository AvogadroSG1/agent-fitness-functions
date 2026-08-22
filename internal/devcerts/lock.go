package devcerts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

const (
	lockWait     = 10 * time.Second
	lockInterval = 100 * time.Millisecond
)

func acquireLockWithWait(root string, operations publicationOperations) (string, error) {
	deadline := operations.now().Add(lockWait)
	attempt := 0
	for {
		if attempt > 0 && !operations.now().Before(deadline) {
			return "", errors.New("certificate publication lock wait timed out")
		}
		attempt++
		token, err := acquireLockOnce(root, operations)
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		reaped, reapErr := reapDeadLock(root, operations)
		if reapErr != nil {
			return "", reapErr
		}
		if reaped {
			continue
		}
		now := operations.now()
		delay := min(lockInterval, deadline.Sub(now))
		operations.sleep(delay)
	}
}

func acquireLockOnce(root string, operations publicationOperations) (string, error) {
	lock := filepath.Join(root, lockName)
	if err := operations.mkdir(lock, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(lock, 0o700); err != nil {
		return "", errors.New("set certificate publication lock mode")
	}
	if _, ok := pinnedDirectory(lock, 0o700); !ok {
		return "", ErrUnsupportedForBootstrap
	}
	hostname, err := operations.hostname()
	if err != nil || hostname == "" {
		return "", errors.New("determine certificate publication hostname")
	}
	acquiredAt := operations.now().UTC().Format(time.RFC3339Nano)
	token, temp, err := writeRandomExclusive(operations.nextID, func(id string) string {
		return filepath.Join(lock, "owner."+id+".tmp")
	}, func(id string) ([]byte, error) {
		return canonicalJSON(ownerDocument{Schema: 1, Hostname: hostname, PID: operations.pid, Token: id, AcquiredAt: acquiredAt})
	}, 0o600)
	if err != nil {
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
	emit(operations.observer, "lock_owner_published")
	return token, operations.checkpoint("lock_owner_published")
}

func reapDeadLock(root string, operations publicationOperations) (bool, error) {
	lock := filepath.Join(root, lockName)
	document, err := readCanonicalOwner(filepath.Join(lock, "owner.json"))
	if err != nil {
		return false, nil
	}
	hostname, err := operations.hostname()
	if err != nil || hostname == "" || document.Hostname != hostname || operations.process(document.PID) != processDead {
		return false, nil
	}
	reap := filepath.Join(root, ".certificate-publication.reap-"+document.Token)
	if err := renamePinnedDirectory(lock, reap, 0o700); err != nil {
		return false, nil
	}
	if err := syncPinnedDirectory(root, exactDirectoryMode(0o755)); err != nil {
		return false, err
	}
	reaped, err := readCanonicalOwner(filepath.Join(reap, "owner.json"))
	if err != nil || reaped.Token != document.Token {
		return false, ErrLockOwnerMismatch
	}
	entries, err := readPinnedDirectory(reap, 0o700)
	if err != nil || len(entries) != 1 || entries[0].Name() != "owner.json" {
		return false, ErrLockOwnerMismatch
	}
	if err := removePinnedRegular(filepath.Join(reap, "owner.json"), 0o600); err != nil {
		return false, err
	}
	if err := removePinnedDirectory(reap, 0o700); err != nil {
		return false, err
	}
	return true, syncPinnedDirectory(root, exactDirectoryMode(0o755))
}

func readCanonicalOwner(path string) (ownerDocument, error) {
	content, _, err := readPinnedRegular(path, 0o600)
	if err != nil {
		return ownerDocument{}, err
	}
	var document ownerDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ownerDocument{}, ErrLockOwnerMismatch
	}
	canonical, err := canonicalJSON(document)
	if err != nil || !bytes.Equal(content, canonical) || document.Schema != 1 || document.Hostname == "" || document.PID <= 0 || !hex128Pattern.MatchString(document.Token) {
		return ownerDocument{}, ErrLockOwnerMismatch
	}
	parsed, err := time.Parse(time.RFC3339Nano, document.AcquiredAt)
	if err != nil || parsed.Location() != time.UTC {
		return ownerDocument{}, ErrLockOwnerMismatch
	}
	return document, nil
}

func operatingSystemProcessState(pid int) processState {
	if runtime.GOOS == "windows" {
		return processUnverifiable
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return processUnverifiable
	}
	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return processLive
	case errors.Is(err, syscall.ESRCH):
		return processDead
	default:
		return processUnverifiable
	}
}
