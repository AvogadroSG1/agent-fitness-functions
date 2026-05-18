package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnforcementBlock rejects active violations.
	EnforcementBlock EnforcementMode = "block"
	// EnforcementAdvisory reports active violations without rejecting the change.
	EnforcementAdvisory EnforcementMode = "advisory"
	// EnforcementOff disables checks for the repository.
	EnforcementOff EnforcementMode = "off"
)

// EnforcementMode controls how active violations are routed.
type EnforcementMode string

// Config is the repository-local .calm/config.json shape used by the bridge.
type Config struct {
	EnforcementMode  EnforcementMode `json:"enforcement-mode"`
	FitnessFunctions map[string]bool `json:"fitness-functions"`
}

func loadConfig(repo string) (Config, string, error) {
	cleanRepo, err := validateRepoPath(repo)
	if err != nil {
		return Config{}, "", err
	}
	path := filepath.Join(cleanRepo, ".calm", "config.json")
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultConfig(), cleanRepo, nil
	}
	if err != nil {
		return Config{}, "", infrastructureError("reading repository config", err)
	}
	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, "", inputError("parsing repository config", err)
	}
	if config.EnforcementMode == "" {
		config.EnforcementMode = EnforcementBlock
	}
	switch config.EnforcementMode {
	case EnforcementBlock, EnforcementAdvisory, EnforcementOff:
	default:
		return Config{}, "", inputError(fmt.Sprintf("unsupported enforcement mode %q", config.EnforcementMode), nil)
	}
	config.FitnessFunctions, err = normalizeFitnessFunctions(config.FitnessFunctions)
	if err != nil {
		return Config{}, "", err
	}
	return config, cleanRepo, nil
}

func defaultConfig() Config {
	return Config{
		EnforcementMode: EnforcementBlock,
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

func validateRepoPath(repo string) (string, error) {
	if repo == "" || strings.ContainsRune(repo, 0) {
		return "", inputError("invalid repository path", nil)
	}
	clean := filepath.Clean(repo)
	info, err := os.Stat(clean)
	if err != nil {
		return "", inputError("invalid repository path", err)
	}
	if !info.IsDir() {
		return "", inputError("invalid repository path", nil)
	}
	return clean, nil
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
