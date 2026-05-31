package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestNewConfigStoreLoadsValidConfigsByRepoName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "advisory",
		"fitness-functions": {
			"cyclomatic-complexity": true,
			"interface-width": false
		}
	}`)

	store, err := NewConfigStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	})

	entry, ok := store.Lookup("repo-one")
	if !ok {
		t.Fatal("Lookup(repo-one) found = false, want true")
	}
	if !entry.Valid {
		t.Fatalf("Lookup(repo-one).Valid = false, want true")
	}
	if entry.Config.EnforcementMode != EnforcementAdvisory {
		t.Fatalf("Lookup(repo-one).Config.EnforcementMode = %q, want %q", entry.Config.EnforcementMode, EnforcementAdvisory)
	}
	if entry.Config.FitnessFunctions["interface-width"] {
		t.Fatalf("Lookup(repo-one).Config.FitnessFunctions[interface-width] = true, want false")
	}
}

func TestNewConfigStoreReturnsErrorForMissingDirectory(t *testing.T) {
	t.Parallel()

	_, err := NewConfigStore(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("NewConfigStore() error = nil, want error")
	}
}

func TestNewConfigStoreReturnsErrorWhenStartupHasZeroValidConfigs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{`)

	_, err := NewConfigStore(context.Background(), dir)
	if err == nil {
		t.Fatal("NewConfigStore() error = nil, want error")
	}
}

func TestConfigStoreReloadsConfigOnWriteEvent(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block",
		"fitness-functions": {
			"cyclomatic-complexity": true
		}
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "off",
		"fitness-functions": {
			"cyclomatic-complexity": false
		}
	}`)
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Write})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-one")
		return ok && entry.Valid && entry.Config.EnforcementMode == EnforcementOff
	})
}

func TestConfigStoreLoadsConfigOnCreateEvent(t *testing.T) {
	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	repoTwoDir := writeMountedRepoDir(t, dir, "repo-two")
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	path := writeMountedConfig(t, dir, "repo-two", `{
		"enforcement-mode": "advisory"
	}`)
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Create})
	watcher.send(fsnotify.Event{Name: repoTwoDir, Op: fsnotify.Create})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-two")
		return ok && entry.Valid && entry.Config.EnforcementMode == EnforcementAdvisory
	})
}

func TestConfigStoreMarksRepoInvalidWhenConfigBecomesInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	writeMountedConfig(t, dir, "repo-one", `{`)
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Write})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-one")
		return ok && !entry.Valid && entry.Error != ""
	})
}

func TestConfigStoreMarksRepoInvalidOnRemoveEvent(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove(%q): %v", path, err)
	}
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Remove})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-one")
		return ok && !entry.Valid && entry.Error == "config file missing"
	})
}

func TestConfigStoreMarksRepoInvalidOnRenameEvent(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	renamedPath := filepath.Join(dir, "repo-one", "config.old.json")
	if err := os.Rename(path, renamedPath); err != nil {
		t.Fatalf("Rename(%q, %q): %v", path, renamedPath, err)
	}
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Rename})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-one")
		return ok && !entry.Valid && entry.Error == "config file missing"
	})
}

func TestConfigStoreDebouncesBurstEventsIntoSingleReload(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	var reads atomic.Int32
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		readFile: func(name string) ([]byte, error) {
			if filepath.Clean(name) == filepath.Clean(path) {
				reads.Add(1)
			}
			return os.ReadFile(name)
		},
		debounceDelay: 20 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "off"
	}`)
	for i := 0; i < 3; i++ {
		watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Write})
	}

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-one")
		return ok && entry.Valid && entry.Config.EnforcementMode == EnforcementOff
	})
	if got := reads.Load(); got != 2 {
		t.Fatalf("read count = %d, want 2 (startup + one debounced reload)", got)
	}
}

func TestConfigStoreRetriesTransientReadFailures(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	var reads atomic.Int32
	errTransient := errors.New("transient read failure")
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		readFile: func(name string) ([]byte, error) {
			if filepath.Clean(name) != filepath.Clean(path) {
				return os.ReadFile(name)
			}
			attempt := reads.Add(1)
			if attempt > 1 && attempt <= 4 {
				return nil, errTransient
			}
			return os.ReadFile(name)
		},
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  5 * time.Millisecond,
	})

	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "off"
	}`)
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Write})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("repo-one")
		return ok && entry.Valid && entry.Config.EnforcementMode == EnforcementOff
	})
	if got := reads.Load(); got != 5 {
		t.Fatalf("read count = %d, want 5 (startup + 3 retries + success)", got)
	}
}

func TestNewConfigStoreLoadsCallerRepoPolicy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	writeMountedConfig(t, configDir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	writeCallerRepoPolicyFile(t, root, `{"callers":{"ci-runner-graft":["repo-one"]},"admins":["governance-admin"]}`)

	store, err := NewConfigStore(context.Background(), configDir)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	})

	if !store.CallerAllowed("ci-runner-graft", "repo-one") {
		t.Fatal("CallerAllowed(ci-runner-graft, repo-one) = false, want true")
	}
	if !store.CallerIsAdmin("governance-admin") {
		t.Fatal("CallerIsAdmin(governance-admin) = false, want true")
	}
}

func TestConfigStoreReloadsCallerRepoPolicyOnWriteEvent(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	writeMountedConfig(t, configDir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	callerPolicyPath := writeCallerRepoPolicyFile(t, root, `{"callers":{"ci-runner-graft":["repo-one"]}}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), configDir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	writeCallerRepoPolicyFile(t, root, `{"callers":{"ci-runner-ringstation":["repo-one"]},"admins":["governance-admin"]}`)
	watcher.send(fsnotify.Event{Name: callerPolicyPath, Op: fsnotify.Write})

	waitForCondition(t, time.Second, func() bool {
		return store.CallerAllowed("ci-runner-ringstation", "repo-one") && store.CallerIsAdmin("governance-admin")
	})
}

func TestConfigStoreCloseStopsWatcherAfterContextCancel(t *testing.T) {
	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	ctx, cancel := context.WithCancel(context.Background())
	watcher := newFakeWatcher()
	store := newTestStore(t, ctx, dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	cancel()
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-watcher.closed:
	case <-time.After(time.Second):
		t.Fatal("watcher did not close before timeout")
	}
}

func TestConfigStoreConcurrentReadWriteSnapshotSafety(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block"
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 5 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				entry, _ := store.Lookup("repo-one")
				_ = entry.Valid
				snapshot := store.Snapshot()
				if repo, ok := snapshot["repo-one"]; ok {
					_ = repo.Config.EnforcementMode
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		modes := []string{"off", "block"}
		for i := 0; ctx.Err() == nil; i++ {
			writeMountedConfig(t, dir, "repo-one", `{
				"enforcement-mode": "`+modes[i%len(modes)]+`"
			}`)
			watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Write})
			time.Sleep(2 * time.Millisecond)
		}
	}()
	wg.Wait()
}

func newTestStore(t *testing.T, ctx context.Context, dir string, watcher *fakeWatcher, opts configStoreOptions) *ConfigStore {
	t.Helper()

	opts.watcherFactory = func() (configWatcher, error) { return watcher, nil }
	store, err := newConfigStore(ctx, dir, opts)
	if err != nil {
		t.Fatalf("newConfigStore() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	})
	return store
}

func writeMountedConfig(t *testing.T, root, repo, content string) string {
	t.Helper()

	repoDir := writeMountedRepoDir(t, root, repo)
	path := filepath.Join(repoDir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func writeCallerRepoPolicyFile(t *testing.T, root, content string) string {
	t.Helper()
	path := filepath.Join(root, callerRepoBindingsFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func writeMountedRepoDir(t *testing.T, root, repo string) string {
	t.Helper()

	repoDir := filepath.Join(root, repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", repoDir, err)
	}
	return repoDir
}

type fakeWatcher struct {
	events    chan fsnotify.Event
	errs      chan error
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events: make(chan fsnotify.Event, 32),
		errs:   make(chan error, 1),
		closed: make(chan struct{}),
	}
}

func (w *fakeWatcher) Add(string) error { return nil }

func (w *fakeWatcher) Close() error {
	w.closeOnce.Do(func() {
		close(w.closed)
		close(w.events)
		close(w.errs)
	})
	return nil
}

func (w *fakeWatcher) Events() <-chan fsnotify.Event { return w.events }

func (w *fakeWatcher) Errors() <-chan error { return w.errs }

func (w *fakeWatcher) send(event fsnotify.Event) {
	w.events <- event
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !check() {
		t.Fatal("condition not met before timeout")
	}
}

func TestNewConfigStoreLoadsContainerConfigFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block",
		"enforcement-on-error": "block",
		"fitness-functions": {
			"cyclomatic-complexity": true,
			"interface-width": true,
			"implementation-depth": true,
			"logic-density": true,
			"dependency-discipline": true
		}
	}`)

	store, err := NewConfigStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	})

	entry, ok := store.Lookup("repo-one")
	if !ok {
		t.Fatal("Lookup(repo-one) found = false, want true")
	}
	if !entry.Valid {
		t.Fatal("Lookup(repo-one).Valid = false, want true")
	}
	if entry.Config.EnforcementMode != EnforcementBlock {
		t.Fatalf("Lookup(repo-one).Config.EnforcementMode = %q, want %q", entry.Config.EnforcementMode, EnforcementBlock)
	}
	if entry.Config.EnforcementOnError != EnforcementOnErrorBlock {
		t.Fatalf("Lookup(repo-one).Config.EnforcementOnError = %q, want %q", entry.Config.EnforcementOnError, EnforcementOnErrorBlock)
	}
}

func TestConfigStoreNormalizesRepoDirectoryNames(t *testing.T) {
	dir := t.TempDir()
	path := writeMountedConfig(t, dir, "SlackStatus", `{
		"enforcement-mode": "block",
		"enforcement-on-error": "block"
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})

	entry, ok := store.Lookup("slackstatus")
	if !ok {
		t.Fatal("Lookup(slackstatus) found = false, want true")
	}
	if !entry.Valid {
		t.Fatal("Lookup(slackstatus).Valid = false, want true")
	}

	writeMountedConfig(t, dir, "SlackStatus", `{
		"enforcement-mode": "off",
		"enforcement-on-error": "block"
	}`)
	watcher.send(fsnotify.Event{Name: path, Op: fsnotify.Write})

	waitForCondition(t, time.Second, func() bool {
		entry, ok := store.Lookup("slackstatus")
		return ok && entry.Valid && entry.Config.EnforcementMode == EnforcementOff
	})
}
