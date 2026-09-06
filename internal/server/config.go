package server

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

// The governance config schema lives in internal/govconfig so the client can
// apply the identical acceptance rule at onboard time without importing the
// daemon. These aliases keep that move invisible to the rest of the server.
type (
	// Config is the mounted governance config shape loaded by the server.
	Config = govconfig.Config
	// EnforcementMode controls how active violations are routed.
	EnforcementMode = govconfig.EnforcementMode
	// ErrorEnforcementMode controls how analyzer failures are routed.
	ErrorEnforcementMode = govconfig.ErrorEnforcementMode
	// FitnessFunctionSettings holds structured per-function configuration.
	FitnessFunctionSettings = govconfig.FitnessFunctionSettings
	// LayerRule defines one architectural layer for layer-sovereignty.
	LayerRule = govconfig.LayerRule
	// LayerSovereigntySettings configures the layer-sovereignty function.
	LayerSovereigntySettings = govconfig.LayerSovereigntySettings
	// DeterministicOrderingSettings configures the deterministic-ordering function.
	DeterministicOrderingSettings = govconfig.DeterministicOrderingSettings
	// TemporalPuritySettings configures the temporal-purity function.
	TemporalPuritySettings = govconfig.TemporalPuritySettings
)

const (
	// EnforcementBlock rejects active violations.
	EnforcementBlock = govconfig.EnforcementBlock
	// EnforcementAdvisory reports active violations without rejecting the change.
	EnforcementAdvisory = govconfig.EnforcementAdvisory
	// EnforcementOff disables checks for the repository.
	EnforcementOff = govconfig.EnforcementOff

	// EnforcementOnErrorBlock fails closed when an analyzer cannot produce a verdict.
	EnforcementOnErrorBlock = govconfig.EnforcementOnErrorBlock
	// EnforcementOnErrorAdvisory reports analyzer failures without blocking the request.
	EnforcementOnErrorAdvisory = govconfig.EnforcementOnErrorAdvisory
	// EnforcementOnErrorPass suppresses analyzer failures.
	EnforcementOnErrorPass = govconfig.EnforcementOnErrorPass
)

var (
	repoNamePattern        = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	invalidNameCharPattern = regexp.MustCompile(`[^a-z0-9_-]`)
)

func loadConfig(store *ConfigStore, repo string) (Config, string, error) {
	if store == nil {
		return Config{}, "", infrastructureError("config store is not configured", nil)
	}

	repoName, err := canonicalRepoName(repo)
	if err != nil {
		return Config{}, "", err
	}
	entry, ok := store.Lookup(repoName)
	if !ok {
		return Config{}, "", notFoundError(fmt.Sprintf("repository %q is not configured", repoName), nil)
	}
	if !entry.Valid {
		// The reason belongs in Message, not only in the wrapped error:
		// writeCheckError serializes Message alone, and a caller that is told
		// only "invalid config" cannot act on it.
		return Config{}, "", infrastructureError(
			fmt.Sprintf("repository %q has an invalid config: %s", repoName, entry.Error),
			errors.New(entry.Error),
		)
	}
	return entry.Config, repoName, nil
}

// canonicalRepoName resolves a repository identifier — either a bare name or a
// repository path — to the normalized name used as the config lookup key. A path is
// reduced to its last component and normalized to match repoNamePattern.
func canonicalRepoName(repo string) (string, error) {
	if name, err := validateRepoName(repo); err == nil {
		return name, nil
	}
	if repo == "" || strings.ContainsRune(repo, 0) {
		return "", inputError("invalid repository name", nil)
	}
	name, err := validateRepoName(extractRepositoryName(repo))
	if err != nil {
		return "", inputError(fmt.Sprintf("invalid repository name: %s", repo), nil)
	}
	return name, nil
}

// parseConfigContent normalizes a mounted config.json. govconfig returns plain
// errors; callers that map a failure onto an HTTP status classify it at their
// own boundary (see buildRegisterConfig).
func parseConfigContent(content []byte) (Config, error) {
	return govconfig.Parse(content)
}

func defaultConfig() Config {
	return govconfig.Default()
}

func normalizeFitnessFunctions(functions map[string]bool) (map[string]bool, error) {
	return govconfig.NormalizeFitnessFunctions(functions)
}

func extractRepositoryName(repoPath string) string {
	// Extract the last path component (e.g., /tmp/xyz123 -> xyz123)
	// and normalize it to match the pattern ^[a-z][a-z0-9_-]{0,63}$
	pathComponent := filepath.Base(strings.TrimRight(repoPath, "/"))
	normalized := strings.ToLower(pathComponent)
	// Replace invalid characters with hyphens
	normalized = invalidNameCharPattern.ReplaceAllString(normalized, "-")
	// Remove leading hyphens and non-alphanumeric characters
	normalized = strings.TrimLeft(normalized, "-_")
	// If empty after cleanup, use "test-repo"
	if normalized == "" {
		normalized = "test-repo"
	}
	// Ensure it starts with a lowercase letter; if not, prepend "repo-"
	if len(normalized) > 0 && !isLowercaseLetter(rune(normalized[0])) {
		normalized = "repo-" + normalized
	}
	// Truncate to 64 chars max
	if len(normalized) > 64 {
		normalized = normalized[:64]
	}
	return normalized
}

func isLowercaseLetter(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func validateRepoName(repo string) (string, error) {
	if repo == "" || strings.ContainsRune(repo, 0) {
		return "", inputError("invalid repository name", nil)
	}
	repoName := strings.ToLower(repo)
	if !repoNamePattern.MatchString(repoName) {
		return "", inputError("invalid repository name", nil)
	}
	return repoName, nil
}
