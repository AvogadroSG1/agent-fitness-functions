package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// registerConflictError reports that a repository is already registered with
// a configuration that differs from the one just requested, and the caller
// requesting the change is not a governance admin.
type registerConflictError struct {
	repo string
}

func (e *registerConflictError) Error() string {
	return fmt.Sprintf("repository %q is already registered with a different configuration", e.repo)
}

// Register persists config for repo (configs/<repo>/config.json), binds
// callerCN to it in caller-repos.json, and makes the repo servable
// synchronously — no fsnotify debounce wait. It reports created=true only
// when the repository had no prior valid config. Re-registering with an
// identical (normalized) config is a no-op beyond ensuring the caller
// binding. Re-registering with a different config requires callerIsAdmin,
// else it returns a *registerConflictError.
func (s *ConfigStore) Register(ctx context.Context, repo string, config Config, callerCN string, callerIsAdmin bool) (bool, error) {
	if s == nil {
		return false, infrastructureError("config store is not configured", nil)
	}
	s.registerMu.Lock()
	defer s.registerMu.Unlock()

	created, writeConfig, err := s.planRegistration(repo, config, callerIsAdmin)
	if err != nil {
		return false, err
	}
	if writeConfig {
		if err := s.writeRegisteredConfig(repo, config); err != nil {
			return false, err
		}
		if err := s.activateRepoConfig(ctx, repo); err != nil {
			return false, err
		}
	}
	if err := s.registerCallerBinding(ctx, callerCN, repo); err != nil {
		return false, err
	}
	return created, nil
}

// planRegistration decides, under registerMu, whether this call creates a new
// repository entry, must overwrite an existing one, is a harmless replay of
// an identical config, or conflicts with an existing different config.
func (s *ConfigStore) planRegistration(repo string, config Config, callerIsAdmin bool) (created, writeConfig bool, err error) {
	existing, ok := s.Lookup(repo)
	if !ok || !existing.Valid {
		return true, true, nil
	}
	if reflect.DeepEqual(existing.Config, config) {
		return false, false, nil
	}
	if !callerIsAdmin {
		return false, false, &registerConflictError{repo: repo}
	}
	return false, true, nil
}

// writeRegisteredConfig writes config to configs/<repo>/config.json using a
// temp-file-in-same-dir plus rename so the fsnotify watcher never observes a
// partial write.
func (s *ConfigStore) writeRegisteredConfig(repo string, config Config) error {
	dir := filepath.Join(s.dir, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return infrastructureError("creating repository config directory", err)
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return infrastructureError("encoding repository config", err)
	}
	encoded = append(encoded, '\n')
	if err := writeFileAtomic(dir, filepath.Join(dir, configFileName), encoded); err != nil {
		return infrastructureError("writing repository config", err)
	}
	return nil
}

// activateRepoConfig makes repo servable immediately: it registers the
// directory mapping, starts watching the new repo directory (when a real
// watcher is running), and synchronously reloads the config into memory.
func (s *ConfigStore) activateRepoConfig(ctx context.Context, repo string) error {
	s.setRepoDir(repo, repo)
	if s.watcher != nil {
		if err := s.ensureWatch(filepath.Join(s.dir, repo)); err != nil {
			return infrastructureError("watching repository config directory", err)
		}
	}
	return s.reloadRepo(ctx, repo)
}

// registerCallerBinding ensures callerCN is bound to repo in
// caller-repos.json, writing the file only when the binding is missing, and
// synchronously reloading the in-memory policy when it changed. The reserved
// local identity is unbindable, so registering in the local listen mode leaves
// caller-repos.json untouched.
func (s *ConfigStore) registerCallerBinding(ctx context.Context, callerCN, repo string) error {
	if !callerBindingIsPersistable(callerCN) {
		return nil
	}
	changed, err := s.bindCallerToRepo(callerCN, repo)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.reloadCallerRepoPolicy(ctx)
}

// callerBindingIsPersistable rejects the reserved local identity (ADR-0010,
// docs/adr/0010-plain-http-local-governance.md). The local listen mode
// authorizes loopback peers without ever reading caller-repos.json, so a
// persisted "local" binding buys nothing there — and it would leak: the same
// governance root can later be served over mTLS, where a stored binding would
// hand every locally registered repository to any client whose certificate
// says CN=local. Authentication reserves that name for the loopback path
// (see externallyAssertedCaller), so a callerCN of "local" reaching this point
// can only have come from a loopback peer.
func callerBindingIsPersistable(callerCN string) bool {
	return callerCN != localCallerName
}

// bindCallerToRepo appends repo to callerCN's repository list in
// caller-repos.json, preserving admins and every other existing entry. It
// reports whether the file was changed.
func (s *ConfigStore) bindCallerToRepo(callerCN, repo string) (bool, error) {
	document, err := loadCallerBindingsDocument(s.callerPolicyPath)
	if err != nil {
		return false, infrastructureError("reading caller bindings", err)
	}
	callers := callerBindingsMap(document)
	repos := callerRepoNames(callers, callerCN)
	if repoListContains(repos, repo) {
		return false, nil
	}
	callers[callerCN] = append(repos, repo)
	if err := writeCallerBindingsDocument(s.callerPolicyPath, document); err != nil {
		return false, infrastructureError("writing caller bindings", err)
	}
	return true, nil
}

// loadCallerBindingsDocument reads caller-repos.json as a generic document so
// unknown keys and the admins list survive a rewrite unchanged. A missing
// file is treated as an empty document.
func loadCallerBindingsDocument(path string) (map[string]any, error) {
	document := map[string]any{}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(content) == 0 {
		return document, nil
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return document, nil
}

func callerBindingsMap(document map[string]any) map[string]any {
	callers, ok := document["callers"].(map[string]any)
	if !ok {
		callers = map[string]any{}
		document["callers"] = callers
	}
	return callers
}

func callerRepoNames(callers map[string]any, callerCN string) []string {
	raw, ok := callers[callerCN].([]any)
	if !ok {
		return nil
	}
	repos := make([]string, 0, len(raw))
	for _, item := range raw {
		if repo, ok := item.(string); ok {
			repos = append(repos, repo)
		}
	}
	return repos
}

// writeCallerBindingsDocument writes document to path in place (os.WriteFile,
// not a rename) because production bind-mounts caller-repos.json as a single
// file rather than a directory.
func writeCallerBindingsDocument(path string, document map[string]any) error {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func repoListContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// writeFileAtomic writes content to path by creating a temp file in dir (the
// same directory as path, so the rename is on the same filesystem) and
// renaming it into place.
func writeFileAtomic(dir, path string, content []byte) error {
	temp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}
