package server

import (
	"errors"
	"strings"
	"testing"
)

// TestLoadConfigReturnsNotConfiguredForUnknownRepo locks the removal of the silent
// test-repo fallback: an unconfigured repo (whether named or given as a path) must
// surface a clean not-configured error rather than resolving to some other config.
func TestLoadConfigReturnsNotConfiguredForUnknownRepo(t *testing.T) {
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, "repo-one", EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	// A config literally named test-repo must NOT be used as a fallback for other names.
	writeRepoConfig(t, store, "test-repo", EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})

	for _, repo := range []string{"repo-two", "/tmp/repo-two"} {
		_, _, err := loadConfig(store, repo)
		if err == nil {
			t.Fatalf("loadConfig(%q) succeeded, want not-configured error", repo)
		}
		var checkErr *CheckError
		if !errors.As(err, &checkErr) || checkErr.Kind != ErrorKindNotFound {
			t.Fatalf("loadConfig(%q) err = %v, want CheckError Kind=not_found", repo, err)
		}
		if !strings.Contains(err.Error(), "is not configured") {
			t.Fatalf("loadConfig(%q) err = %q, want \"is not configured\"", repo, err.Error())
		}
	}
}

// TestLoadConfigResolvesConfiguredRepoByNameAndPath confirms both a bare name and a
// repository path still resolve to the configured repo after the refactor.
func TestLoadConfigResolvesConfiguredRepoByNameAndPath(t *testing.T) {
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, "repo-one", EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})

	for _, repo := range []string{"repo-one", "/home/user/repo-one"} {
		config, name, err := loadConfig(store, repo)
		if err != nil {
			t.Fatalf("loadConfig(%q) err = %v", repo, err)
		}
		if name != "repo-one" {
			t.Fatalf("loadConfig(%q) name = %q, want repo-one", repo, name)
		}
		if config.EnforcementMode != EnforcementAdvisory {
			t.Fatalf("loadConfig(%q) mode = %q, want advisory", repo, config.EnforcementMode)
		}
	}
}
