package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	configFileName                 = "config.json"
	defaultConfigStoreDebounce     = 500 * time.Millisecond
	defaultConfigStoreRetryBackoff = 200 * time.Millisecond
	defaultConfigStoreReadRetries  = 3
	callerPolicyReloadKey          = "__caller_repo_policy__"
)

// ConfigEntry stores one logical repository config snapshot.
type ConfigEntry struct {
	Config      Config
	Valid       bool
	Error       string
	LastValidAt time.Time
}

// ConfigStore keeps mounted repository configs in memory.
type ConfigStore struct {
	mu               sync.RWMutex
	watchMu          sync.Mutex
	registerMu       sync.Mutex
	entries          map[string]ConfigEntry
	repoDirs         map[string]string
	callerPolicy     CallerRepoPolicy
	loadedAt         time.Time
	dir              string
	callerPolicyPath string
	watcher          configWatcher
	readFile         func(string) ([]byte, error)
	debounceDelay    time.Duration
	retryBackoff     time.Duration
	readRetries      int
	watchedPaths     map[string]struct{}
	cancel           context.CancelFunc
	closeOnce        sync.Once
	closeErr         error
	loopDone         chan struct{}
}

type configWatcher interface {
	Add(string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

type fsnotifyWatcher struct {
	*fsnotify.Watcher
}

func (w *fsnotifyWatcher) Events() <-chan fsnotify.Event { return w.Watcher.Events }

func (w *fsnotifyWatcher) Errors() <-chan error { return w.Watcher.Errors }

type configStoreOptions struct {
	watcherFactory func() (configWatcher, error)
	readFile       func(string) ([]byte, error)
	debounceDelay  time.Duration
	retryBackoff   time.Duration
	readRetries    int
}

// NewConfigStore loads repository configs from dir and starts the store lifecycle.
func NewConfigStore(ctx context.Context, dir string) (*ConfigStore, error) {
	return newConfigStore(ctx, dir, configStoreOptions{})
}

func newConfigStore(ctx context.Context, dir string, opts configStoreOptions) (*ConfigStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = withConfigStoreDefaults(opts)
	cleanDir := filepath.Clean(dir)
	entries, repoDirs, _, err := scanMountedConfigs(cleanDir, opts.readFile)
	if err != nil {
		return nil, err
	}
	callerPolicyPath := callerRepoBindingsPath(cleanDir)
	callerPolicy, err := loadCallerRepoPolicy(opts.readFile, callerPolicyPath)
	if err != nil {
		return nil, err
	}
	watcher, err := opts.watcherFactory()
	if err != nil {
		return nil, fmt.Errorf("creating config watcher: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	store := &ConfigStore{
		entries:          entries,
		repoDirs:         repoDirs,
		callerPolicy:     callerPolicy,
		loadedAt:         time.Now().UTC(),
		dir:              cleanDir,
		callerPolicyPath: callerPolicyPath,
		watcher:          watcher,
		readFile:         opts.readFile,
		debounceDelay:    opts.debounceDelay,
		retryBackoff:     opts.retryBackoff,
		readRetries:      opts.readRetries,
		watchedPaths:     map[string]struct{}{},
		cancel:           cancel,
		loopDone:         make(chan struct{}),
	}
	if err := store.watchInitialPaths(repoDirs); err != nil {
		cancel()
		_ = watcher.Close()
		return nil, err
	}
	go store.watch(ctx)
	return store, nil
}

// watchInitialPaths registers filesystem watches for the config root, every
// discovered repository directory, and the caller policy directory.
func (s *ConfigStore) watchInitialPaths(repoDirs map[string]string) error {
	if err := s.ensureWatch(s.dir); err != nil {
		return fmt.Errorf("watch config directory %q: %w", s.dir, err)
	}
	for _, repoDir := range repoDirs {
		if err := s.ensureWatch(filepath.Join(s.dir, repoDir)); err != nil {
			return fmt.Errorf("watch repository config directory %q: %w", repoDir, err)
		}
	}
	callerPolicyDir := filepath.Dir(s.callerPolicyPath)
	if callerPolicyDir == s.dir {
		return nil
	}
	if err := s.ensureWatch(callerPolicyDir); err != nil {
		return fmt.Errorf("watch caller policy directory %q: %w", callerPolicyDir, err)
	}
	return nil
}

// Close releases store resources.
func (s *ConfigStore) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.watcher != nil {
			s.closeErr = s.watcher.Close()
		}
		if s.loopDone != nil {
			<-s.loopDone
		}
	})
	return s.closeErr
}

// Lookup returns one repository config snapshot.
func (s *ConfigStore) Lookup(repo string) (ConfigEntry, bool) {
	if s == nil {
		return ConfigEntry{}, false
	}
	s.mu.RLock()
	entry, ok := s.entries[repo]
	s.mu.RUnlock()
	if !ok {
		return ConfigEntry{}, false
	}
	return cloneConfigEntry(entry), true
}

// SnapshotState returns the loaded timestamp and a copy of every repository config snapshot under one read lock.
func (s *ConfigStore) SnapshotState() (time.Time, map[string]ConfigEntry) {
	if s == nil {
		return time.Time{}, map[string]ConfigEntry{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[string]ConfigEntry, len(s.entries))
	for repo, entry := range s.entries {
		snapshot[repo] = cloneConfigEntry(entry)
	}
	return s.loadedAt, snapshot
}

// Snapshot returns a copy of every repository config snapshot.
func (s *ConfigStore) Snapshot() map[string]ConfigEntry {
	_, snapshot := s.SnapshotState()
	return snapshot
}

// LoadedAt returns when the current in-memory config set last changed.
func (s *ConfigStore) LoadedAt() time.Time {
	loadedAt, _ := s.SnapshotState()
	return loadedAt
}

// CallerRepoPolicy returns a copy of the caller authorization policy.
func (s *ConfigStore) CallerRepoPolicy() CallerRepoPolicy {
	if s == nil {
		return emptyCallerRepoPolicy()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCallerRepoPolicy(s.callerPolicy)
}

func (s *ConfigStore) CallerAllowed(caller, repo string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.callerPolicy.Allows(caller, repo)
}

func (s *ConfigStore) CallerIsAdmin(caller string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.callerPolicy.IsAdmin(caller)
}

func (s *ConfigStore) watch(ctx context.Context) {
	defer close(s.loopDone)
	timer := time.NewTimer(s.debounceDelay)
	drainTimer(timer)
	pending := map[string]struct{}{}
	timerActive := false
	for {
		timerC := activeTimerChannel(timer, timerActive)
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.watcher.Events():
			if !ok {
				return
			}
			timerActive = s.recordWatchEvent(event, pending, timer, timerActive)
		case _, ok := <-s.watcher.Errors():
			if !ok {
				return
			}
		case <-timerC:
			timerActive = false
			if !s.flushPendingReloads(ctx, pending) {
				return
			}
		}
	}
}

// activeTimerChannel returns timer's fire channel when the debounce timer is
// active, or nil (which blocks forever in a select) otherwise.
func activeTimerChannel(timer *time.Timer, active bool) <-chan time.Time {
	if !active {
		return nil
	}
	return timer.C
}

// drainTimer stops timer and discards an already-fired tick, leaving it idle
// and safe to Reset from the watch loop.
func drainTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

// recordWatchEvent folds one fsnotify event into pending and returns whether
// the debounce timer is now active.
func (s *ConfigStore) recordWatchEvent(event fsnotify.Event, pending map[string]struct{}, timer *time.Timer, timerActive bool) bool {
	key, reload := s.handleEvent(event)
	if !reload {
		return timerActive
	}
	pending[key] = struct{}{}
	return resetConfigStoreTimer(timer, timerActive, s.debounceDelay)
}

// flushPendingReloads reloads every pending key once the debounce timer
// fires, draining pending as it goes. It reports false when a reload failed
// because ctx was cancelled, signaling the caller to stop the watch loop.
func (s *ConfigStore) flushPendingReloads(ctx context.Context, pending map[string]struct{}) bool {
	for key := range pending {
		if err := s.reloadPendingKey(ctx, key); err != nil && ctx.Err() != nil {
			return false
		}
		delete(pending, key)
	}
	return true
}

// reloadPendingKey reloads the caller policy or one repository config,
// depending on which pending key fired.
func (s *ConfigStore) reloadPendingKey(ctx context.Context, key string) error {
	if key == callerPolicyReloadKey {
		return s.reloadCallerRepoPolicy(ctx)
	}
	return s.reloadRepo(ctx, key)
}

func (s *ConfigStore) handleEvent(event fsnotify.Event) (string, bool) {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return "", false
	}
	if filepath.Clean(event.Name) == filepath.Clean(s.callerPolicyPath) {
		return callerPolicyReloadKey, true
	}
	if repo, ok := repoNameForConfigPath(s.dir, event.Name); ok {
		s.setRepoDir(repo, filepath.Base(filepath.Dir(filepath.Clean(event.Name))))
		return repo, true
	}
	repo, ok := repoNameForRepoDir(s.dir, event.Name)
	if !ok {
		return "", false
	}
	s.setRepoDir(repo, filepath.Base(filepath.Clean(event.Name)))
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		s.forgetWatch(filepath.Clean(event.Name))
		_, exists := s.Lookup(repo)
		return repo, exists
	}
	if err := s.ensureWatch(filepath.Clean(event.Name)); err != nil {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(filepath.Clean(event.Name), configFileName)); err == nil {
		return repo, true
	}
	return "", false
}

func (s *ConfigStore) reloadRepo(ctx context.Context, repo string) error {
	path := filepath.Join(s.dir, s.repoDir(repo), configFileName)
	content, err := s.readConfigWithRetry(ctx, path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		s.storeEntry(repo, ConfigEntry{Valid: false, Error: configReadError(err)})
		return nil
	}
	config, err := parseConfigContent(content)
	if err != nil {
		s.storeEntry(repo, ConfigEntry{Valid: false, Error: err.Error()})
		return nil
	}
	s.storeEntry(repo, ConfigEntry{Config: config, Valid: true})
	return nil
}

func (s *ConfigStore) reloadCallerRepoPolicy(ctx context.Context) error {
	content, err := s.readConfigWithRetry(ctx, s.callerPolicyPath)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			s.storeCallerRepoPolicy(emptyCallerRepoPolicy())
			return nil
		}
		s.storeCallerRepoPolicy(emptyCallerRepoPolicy())
		return nil
	}
	policy, err := parseCallerRepoPolicy(content)
	if err != nil {
		s.storeCallerRepoPolicy(emptyCallerRepoPolicy())
		return nil
	}
	s.storeCallerRepoPolicy(policy)
	return nil
}

func loadCallerRepoPolicy(readFile func(string) ([]byte, error), path string) (CallerRepoPolicy, error) {
	content, err := readFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyCallerRepoPolicy(), nil
		}
		return CallerRepoPolicy{}, fmt.Errorf("read caller policy %q: %w", path, err)
	}
	policy, err := parseCallerRepoPolicy(content)
	if err != nil {
		return CallerRepoPolicy{}, err
	}
	return policy, nil
}

func (s *ConfigStore) readConfigWithRetry(ctx context.Context, path string) ([]byte, error) {
	var err error
	for attempt := 0; attempt <= s.readRetries; attempt++ {
		content, readErr := s.readFile(path)
		if readErr == nil {
			return content, nil
		}
		err = readErr
		if errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		if attempt == s.readRetries {
			return nil, readErr
		}
		if sleepErr := sleepWithContext(ctx, s.retryBackoff); sleepErr != nil {
			return nil, sleepErr
		}
	}
	return nil, err
}

func (s *ConfigStore) storeEntry(repo string, entry ConfigEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	cloned := cloneConfigEntry(entry)
	if cloned.Valid {
		cloned.LastValidAt = now
	} else if previous, ok := s.entries[repo]; ok && !previous.LastValidAt.IsZero() {
		cloned.LastValidAt = previous.LastValidAt
	}
	s.entries[repo] = cloned
	s.loadedAt = now
}

func (s *ConfigStore) storeCallerRepoPolicy(policy CallerRepoPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callerPolicy = cloneCallerRepoPolicy(policy)
}

func (s *ConfigStore) setRepoDir(repo, dirName string) {
	if s == nil || repo == "" || dirName == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repoDirs == nil {
		s.repoDirs = map[string]string{}
	}
	s.repoDirs[repo] = dirName
}

func (s *ConfigStore) repoDir(repo string) string {
	if s == nil {
		return repo
	}
	s.mu.RLock()
	dirName := s.repoDirs[repo]
	s.mu.RUnlock()
	if dirName == "" {
		return repo
	}
	return dirName
}

func (s *ConfigStore) ensureWatch(path string) error {
	cleanPath := filepath.Clean(path)
	info, err := os.Stat(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if _, ok := s.watchedPaths[cleanPath]; ok {
		return nil
	}
	if err := s.watcher.Add(cleanPath); err != nil {
		return err
	}
	s.watchedPaths[cleanPath] = struct{}{}
	return nil
}

func (s *ConfigStore) forgetWatch(path string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	delete(s.watchedPaths, path)
}

func scanMountedConfigs(dir string, readFile func(string) ([]byte, error)) (map[string]ConfigEntry, map[string]string, int, error) {
	if err := statConfigDir(dir); err != nil {
		return nil, nil, 0, err
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read config directory %q: %w", dir, err)
	}
	entries := make(map[string]ConfigEntry, len(children))
	repoDirs := make(map[string]string, len(children))
	validCount := 0
	for _, child := range children {
		repo, entry, ok, err := loadChildConfig(dir, child, repoDirs, readFile)
		if err != nil {
			return nil, nil, 0, err
		}
		if !ok {
			continue
		}
		entries[repo] = entry
		if entry.Valid {
			validCount++
		}
	}
	return entries, repoDirs, validCount, nil
}

// statConfigDir confirms the mounted config root exists and is a directory.
func statConfigDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat config directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config directory %q is not a directory", dir)
	}
	return nil
}

// loadChildConfig resolves one config-root child into a repository config
// entry. It reports ok=false for entries to skip (non-directories, invalid
// repo names, or a missing config.json) and a non-nil error only for a
// duplicate normalized repository name, which aborts the scan.
func loadChildConfig(dir string, child os.DirEntry, repoDirs map[string]string, readFile func(string) ([]byte, error)) (string, ConfigEntry, bool, error) {
	if !child.IsDir() {
		return "", ConfigEntry{}, false, nil
	}
	repo, nameErr := validateRepoName(child.Name())
	if nameErr != nil {
		return "", ConfigEntry{}, false, nil
	}
	if previous, ok := repoDirs[repo]; ok && previous != child.Name() {
		return "", ConfigEntry{}, false, fmt.Errorf("duplicate normalized repository name %q from %q and %q", repo, previous, child.Name())
	}
	repoDirs[repo] = child.Name()
	content, readErr := readFile(filepath.Join(dir, child.Name(), configFileName))
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return "", ConfigEntry{}, false, nil
		}
		return repo, ConfigEntry{Valid: false, Error: configReadError(readErr)}, true, nil
	}
	config, parseErr := parseConfigContent(content)
	if parseErr != nil {
		return repo, ConfigEntry{Valid: false, Error: parseErr.Error()}, true, nil
	}
	return repo, ConfigEntry{Config: config, Valid: true, LastValidAt: time.Now().UTC()}, true, nil
}

func withConfigStoreDefaults(opts configStoreOptions) configStoreOptions {
	if opts.watcherFactory == nil {
		opts.watcherFactory = func() (configWatcher, error) {
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return nil, err
			}
			return &fsnotifyWatcher{Watcher: watcher}, nil
		}
	}
	if opts.readFile == nil {
		opts.readFile = os.ReadFile
	}
	if opts.debounceDelay <= 0 {
		opts.debounceDelay = defaultConfigStoreDebounce
	}
	if opts.retryBackoff <= 0 {
		opts.retryBackoff = defaultConfigStoreRetryBackoff
	}
	if opts.readRetries == 0 {
		opts.readRetries = defaultConfigStoreReadRetries
	}
	if opts.readRetries < 0 {
		opts.readRetries = 0
	}
	return opts
}

func cloneConfigEntry(entry ConfigEntry) ConfigEntry {
	clone := entry
	clone.Config = cloneConfig(entry.Config)
	return clone
}

func cloneConfig(config Config) Config {
	clone := config
	if config.FitnessFunctions == nil {
		return clone
	}
	clone.FitnessFunctions = make(map[string]bool, len(config.FitnessFunctions))
	for name, enabled := range config.FitnessFunctions {
		clone.FitnessFunctions[name] = enabled
	}
	return clone
}

func repoNameForConfigPath(root, path string) (string, bool) {
	return repoNameForPath(root, path, true)
}

func repoNameForRepoDir(root, path string) (string, bool) {
	return repoNameForPath(root, path, false)
}

func repoNameForPath(root, path string, wantsConfig bool) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if invalidRelConfigPath(rel, err) {
		return "", false
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if !relPathPartsMatchShape(parts, wantsConfig) {
		return "", false
	}
	repoName, nameErr := validateRepoName(parts[0])
	if nameErr != nil {
		return "", false
	}
	return repoName, true
}

// invalidRelConfigPath reports whether rel (as produced by filepath.Rel
// against the config root) cannot possibly name something under that root.
func invalidRelConfigPath(rel string, err error) bool {
	return err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// relPathPartsMatchShape reports whether the path segments below the config
// root match a repo config file path (repo/config.json) or a bare repo
// directory path (repo), depending on wantsConfig.
func relPathPartsMatchShape(parts []string, wantsConfig bool) bool {
	if wantsConfig {
		return len(parts) == 2 && parts[1] == configFileName
	}
	return len(parts) == 1
}

func resetConfigStoreTimer(timer *time.Timer, active bool, delay time.Duration) bool {
	if active && !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
	return true
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func configReadError(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "config file missing"
	}
	return fmt.Sprintf("read config: %v", err)
}
