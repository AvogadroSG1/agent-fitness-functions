package devcerts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireLockWaitsBoundedWithoutAttemptAfterDeadline(t *testing.T) {
	tests := []struct {
		name      string
		hostname  string
		ownerHost string
		process   processState
		owner     string
	}{
		{name: "live same-host", hostname: "local", ownerHost: "local", process: processLive, owner: "canonical"},
		{name: "PID reuse is live", hostname: "local", ownerHost: "local", process: processLive, owner: "canonical"},
		{name: "foreign host", hostname: "local", ownerHost: "foreign", process: processDead, owner: "canonical"},
		{name: "unverifiable process", hostname: "local", ownerHost: "local", process: processUnverifiable, owner: "canonical"},
		{name: "permission error", hostname: "local", ownerHost: "local", process: processUnverifiable, owner: "canonical"},
		{name: "ownerless", hostname: "local", ownerHost: "local", process: processDead, owner: "missing"},
		{name: "malformed", hostname: "local", ownerHost: "local", process: processDead, owner: "malformed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := preparedLockRoot(t)
			seedLock(t, root, test.ownerHost, test.owner)
			operations, elapsed, sleeps := fakeLockOperations(test.hostname, test.process)
			attempts := 0
			mkdir := operations.mkdir
			operations.mkdir = func(path string, mode os.FileMode) error {
				attempts++
				return mkdir(path, mode)
			}
			_, err := acquireLockWithWait(root, operations)
			if err == nil {
				t.Fatal("acquireLockWithWait succeeded")
			}
			if *elapsed != lockWait {
				t.Fatalf("elapsed = %v, want %v", *elapsed, lockWait)
			}
			if len(*sleeps) != int(lockWait/lockInterval) {
				t.Fatalf("sleep count = %d, want %d", len(*sleeps), int(lockWait/lockInterval))
			}
			if attempts != int(lockWait/lockInterval) {
				t.Fatalf("acquisition attempts = %d, want %d with no attempt at deadline", attempts, int(lockWait/lockInterval))
			}
			for _, sleep := range *sleeps {
				if sleep <= 0 || sleep > lockInterval {
					t.Fatalf("sleep = %v, want (0, %v]", sleep, lockInterval)
				}
			}
			if _, statErr := os.Lstat(filepath.Join(root, lockName)); statErr != nil {
				t.Fatalf("unverifiable lock was not preserved: %v", statErr)
			}
		})
	}
}

func TestAcquireLockReapsOnlyProvenDeadSameHostOwner(t *testing.T) {
	root := preparedLockRoot(t)
	seedLock(t, root, "local", "canonical")
	operations, elapsed, _ := fakeLockOperations("local", processDead)
	token, err := acquireLockWithWait(root, operations)
	if err != nil {
		t.Fatalf("acquireLockWithWait(proven dead): %v", err)
	}
	if *elapsed != 0 {
		t.Fatalf("elapsed = %v, want immediate reap and acquisition", *elapsed)
	}
	if err := releaseLock(root, token); err != nil {
		t.Fatalf("releaseLock: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(root): %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "versions" {
			t.Fatalf("artifact remains after reap/acquire/release: %s", entry.Name())
		}
	}
}

func TestAcquireLockRealTimeoutIsNotShorterThanTenSeconds(t *testing.T) {
	root := preparedLockRoot(t)
	seedLock(t, root, "local", "missing")
	started := time.Now()
	if _, err := acquireLockWithWait(root, defaultOperations()); err == nil {
		t.Fatal("acquireLockWithWait(ownerless) succeeded")
	}
	elapsed := time.Since(started)
	if elapsed < lockWait {
		t.Fatalf("real lock timeout = %v, want at least %v", elapsed, lockWait)
	}
	if elapsed > 11*time.Second {
		t.Fatalf("real lock timeout = %v, want scheduler overrun bounded through 11s", elapsed)
	}
}

func TestCompetingReapArtifactPreventsDeletion(t *testing.T) {
	root := preparedLockRoot(t)
	seedLock(t, root, "local", "canonical")
	const token = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.Mkdir(filepath.Join(root, ".certificate-publication.reap-"+token), 0o700); err != nil {
		t.Fatalf("Mkdir(competing reap): %v", err)
	}
	operations, _, _ := fakeLockOperations("local", processDead)
	_, err := acquireLockWithWait(root, operations)
	if err == nil {
		t.Fatal("acquireLockWithWait with competing reap succeeded")
	}
	if _, err := os.Lstat(filepath.Join(root, lockName)); err != nil {
		t.Fatalf("source lock changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".certificate-publication.reap-"+token)); err != nil {
		t.Fatalf("competing reap evidence changed: %v", err)
	}
}

func preparedLockRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "certs")
	if err := prepareRootForLifecycle(root); err != nil {
		t.Fatalf("prepareRootForLifecycle: %v", err)
	}
	return root
}

func seedLock(t *testing.T, root, hostname, ownerKind string) {
	t.Helper()
	lock := filepath.Join(root, lockName)
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatalf("Mkdir(lock): %v", err)
	}
	if ownerKind == "missing" {
		return
	}
	content := []byte("not canonical\n")
	if ownerKind == "canonical" {
		var err error
		content, err = canonicalJSON(ownerDocument{Schema: 1, Hostname: hostname, PID: 4242, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AcquiredAt: "2026-08-22T12:00:00Z"})
		if err != nil {
			t.Fatalf("canonicalJSON(owner): %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(lock, "owner.json"), content, 0o600); err != nil {
		t.Fatalf("WriteFile(owner): %v", err)
	}
}

func fakeLockOperations(hostname string, state processState) (publicationOperations, *time.Duration, *[]time.Duration) {
	operations := defaultOperations()
	start := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	elapsed := time.Duration(0)
	var sleeps []time.Duration
	operations.now = func() time.Time { return start.Add(elapsed) }
	operations.sleep = func(delay time.Duration) {
		sleeps = append(sleeps, delay)
		elapsed += delay
	}
	operations.hostname = func() (string, error) { return hostname, nil }
	operations.process = func(int) processState { return state }
	operations.nextID = func() (string, error) { return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil }
	return operations, &elapsed, &sleeps
}

func TestReadCanonicalOwnerRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, content := range []string{
		`{"schema":1,"hostname":"local","pid":1,"token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","acquired_at":"2026-08-22T12:00:00Z","unknown":true}` + "\n",
		`{"schema":1,"schema":1,"hostname":"local","pid":1,"token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","acquired_at":"2026-08-22T12:00:00Z"}` + "\n",
	} {
		path := filepath.Join(t.TempDir(), "owner.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(owner): %v", err)
		}
		if _, err := readCanonicalOwner(path); !errors.Is(err, ErrLockOwnerMismatch) {
			t.Fatalf("readCanonicalOwner(%q) error = %v, want ErrLockOwnerMismatch", content, err)
		}
	}
}

func TestReleaseLockRemovesOnlyMatchingOwnerTemp(t *testing.T) {
	root := preparedLockRoot(t)
	operations := defaultOperations()
	token, err := acquireLockOnce(root, operations)
	if err != nil {
		t.Fatalf("acquireLockOnce: %v", err)
	}
	temp := filepath.Join(root, lockName, "owner."+token+".tmp")
	if err := os.WriteFile(temp, []byte("durable owner temp evidence\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(owner temp): %v", err)
	}
	if err := releaseLock(root, token); err != nil {
		t.Fatalf("releaseLock with matching owner temp: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released lock remains: %v", err)
	}
}

func TestReleaseLockPreservesMismatchedOwnerTemp(t *testing.T) {
	root := preparedLockRoot(t)
	operations := defaultOperations()
	token, err := acquireLockOnce(root, operations)
	if err != nil {
		t.Fatalf("acquireLockOnce: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, lockName, "owner.cccccccccccccccccccccccccccccccc.tmp"), []byte("foreign evidence\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(foreign owner temp): %v", err)
	}
	if err := releaseLock(root, token); !errors.Is(err, ErrLockOwnerMismatch) {
		t.Fatalf("releaseLock mismatch error = %v, want ErrLockOwnerMismatch", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".certificate-publication.release-"+token)); err != nil {
		t.Fatalf("mismatched release evidence was not preserved: %v", err)
	}
}
