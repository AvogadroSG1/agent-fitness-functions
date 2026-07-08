package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// EnforcementBlock rejects active violations.
	EnforcementBlock EnforcementMode = "block"
	// EnforcementAdvisory reports active violations without rejecting the change.
	EnforcementAdvisory EnforcementMode = "advisory"
	// EnforcementOff disables checks for the repository.
	EnforcementOff EnforcementMode = "off"

	// EnforcementOnErrorBlock fails closed when an analyzer cannot produce a verdict.
	EnforcementOnErrorBlock ErrorEnforcementMode = "block"
	// EnforcementOnErrorAdvisory reports analyzer failures without blocking the request.
	EnforcementOnErrorAdvisory ErrorEnforcementMode = "advisory"
	// EnforcementOnErrorPass suppresses analyzer failures.
	EnforcementOnErrorPass ErrorEnforcementMode = "pass"
)

// EnforcementMode controls how active violations are routed.
type EnforcementMode string

// ErrorEnforcementMode controls how analyzer failures are routed.
type ErrorEnforcementMode string

// Config is the mounted governance config shape loaded by the server.
type Config struct {
	EnforcementMode    EnforcementMode      `json:"enforcement-mode"`
	EnforcementOnError ErrorEnforcementMode `json:"enforcement-on-error,omitempty"`
	FitnessFunctions   map[string]bool      `json:"fitness-functions"`
	ExcludePatterns    []string             `json:"exclude-patterns,omitempty"`
}

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
		return Config{}, "", infrastructureError(
			fmt.Sprintf("repository %q has an invalid config", repoName),
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

func parseConfigContent(content []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, inputError("parsing repository config", err)
	}
	if config.EnforcementMode == "" {
		config.EnforcementMode = EnforcementBlock
	}
	switch config.EnforcementMode {
	case EnforcementBlock, EnforcementAdvisory, EnforcementOff:
	default:
		return Config{}, inputError(fmt.Sprintf("unsupported enforcement mode %q", config.EnforcementMode), nil)
	}
	if config.EnforcementOnError == "" {
		config.EnforcementOnError = EnforcementOnErrorBlock
	}
	switch config.EnforcementOnError {
	case EnforcementOnErrorBlock, EnforcementOnErrorAdvisory, EnforcementOnErrorPass:
	default:
		return Config{}, inputError(fmt.Sprintf("unsupported enforcement-on-error mode %q", config.EnforcementOnError), nil)
	}
	fitnessFunctions, err := normalizeFitnessFunctions(config.FitnessFunctions)
	if err != nil {
		return Config{}, err
	}
	config.FitnessFunctions = fitnessFunctions
	return config, nil
}

func defaultConfig() Config {
	return Config{
		EnforcementMode:    EnforcementBlock,
		EnforcementOnError: EnforcementOnErrorBlock,
		FitnessFunctions: map[string]bool{
			"cyclomatic-complexity": true,
			"interface-width":       true,
			"implementation-depth":  true,
			"logic-density":         true,
			"dependency-discipline": true,
		},
	}
}

func (c Config) enabled(name string) bool {
	enabled, ok := c.FitnessFunctions[name]
	return ok && enabled
}

func (c Config) isExcluded(file string) bool {
	base := filepath.Base(file)
	for _, pattern := range c.ExcludePatterns {
		matched, err := filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
	}
	return false
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

func normalizeFitnessFunctions(functions map[string]bool) (map[string]bool, error) {
	defaults := defaultConfig().FitnessFunctions
	normalized := make(map[string]bool, len(defaults))
	for name, enabled := range defaults {
		normalized[name] = enabled
	}
	if functions == nil {
		return normalized, nil
	}
	for name, enabled := range functions {
		if _, ok := defaults[name]; !ok {
			return nil, inputError(fmt.Sprintf("unsupported fitness function %q", name), nil)
		}
		normalized[name] = enabled
	}
	return normalized, nil
}
