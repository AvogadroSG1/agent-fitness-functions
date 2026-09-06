// Package govconfig owns the governance config schema: the shape of a
// configs/<repo>/config.json, its defaults, and every rule that decides
// whether one is acceptable.
//
// It is a leaf package so both sides of the product can apply the identical
// rule. The server parses mounted configs through it; the client validates a
// repo-local config through it before registering that config with the
// machine governance root. Without a shared owner the client could only
// discover an unacceptable config by watching the daemon reject it later as
// an opaque HTTP 503 at commit time — which is exactly how the observatory
// repo failed.
//
// Errors returned here are plain errors. Callers that map failures onto HTTP
// status codes are responsible for classifying them at their own boundary.
package govconfig

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
//
// A Config MUST be produced by Parse. LayerRule carries compiled patterns in
// unexported fields, so a hand-built Config silently matches nothing.
type Config struct {
	EnforcementMode         EnforcementMode          `json:"enforcement-mode"`
	EnforcementOnError      ErrorEnforcementMode     `json:"enforcement-on-error,omitempty"`
	FitnessFunctions        map[string]bool          `json:"fitness-functions"`
	ExcludePatterns         []string                 `json:"exclude-patterns,omitempty"`
	FitnessFunctionSettings *FitnessFunctionSettings `json:"fitness-function-settings,omitempty"`
}

// Validate reports why raw governance config content would be rejected, or
// nil when it is acceptable. It is the same check applied on load, so a
// client can refuse to register a config the daemon would only reject later.
func Validate(content []byte) error {
	_, err := Parse(content)
	return err
}

// Parse normalizes and validates raw governance config content, applying
// defaults for any omitted field.
func Parse(content []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, fmt.Errorf("parsing repository config: %w", err)
	}
	if config.EnforcementMode == "" {
		config.EnforcementMode = EnforcementBlock
	}
	switch config.EnforcementMode {
	case EnforcementBlock, EnforcementAdvisory, EnforcementOff:
	default:
		return Config{}, fmt.Errorf("unsupported enforcement mode %q", config.EnforcementMode)
	}
	if config.EnforcementOnError == "" {
		config.EnforcementOnError = EnforcementOnErrorBlock
	}
	switch config.EnforcementOnError {
	case EnforcementOnErrorBlock, EnforcementOnErrorAdvisory, EnforcementOnErrorPass:
	default:
		return Config{}, fmt.Errorf("unsupported enforcement-on-error mode %q", config.EnforcementOnError)
	}
	fitnessFunctions, err := NormalizeFitnessFunctions(config.FitnessFunctions)
	if err != nil {
		return Config{}, err
	}
	config.FitnessFunctions = fitnessFunctions
	if err := rejectUnknownFitnessFunctionSettingsKeys(content); err != nil {
		return Config{}, err
	}
	if err := validateFitnessFunctionSettings(&config); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Default returns the config applied to a repository that supplies none: the
// five metric-scored fitness functions on, the four opt-in ones off.
func Default() Config {
	return Config{
		EnforcementMode:    EnforcementBlock,
		EnforcementOnError: EnforcementOnErrorBlock,
		FitnessFunctions: map[string]bool{
			"cyclomatic-complexity":  true,
			"interface-width":        true,
			"implementation-depth":   true,
			"logic-density":          true,
			"dependency-discipline":  true,
			"layer-sovereignty":      false,
			"temporal-purity":        false,
			"sql-composition-safety": false,
			"deterministic-ordering": false,
		},
	}
}

// Enabled reports whether the named fitness function is turned on.
func (c Config) Enabled(name string) bool {
	enabled, ok := c.FitnessFunctions[name]
	return ok && enabled
}

// IsExcluded reports whether file matches any configured exclude pattern.
// Patterns are matched against the base name only.
func (c Config) IsExcluded(file string) bool {
	base := filepath.Base(file)
	for _, pattern := range c.ExcludePatterns {
		matched, err := filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// NormalizeFitnessFunctions overlays the caller's selection onto the default
// set, rejecting any name outside the known catalog (fail closed: a typo must
// not silently leave a function disabled).
func NormalizeFitnessFunctions(functions map[string]bool) (map[string]bool, error) {
	defaults := Default().FitnessFunctions
	normalized := make(map[string]bool, len(defaults))
	for name, enabled := range defaults {
		normalized[name] = enabled
	}
	if functions == nil {
		return normalized, nil
	}
	for name, enabled := range functions {
		if _, ok := defaults[name]; !ok {
			return nil, fmt.Errorf("unsupported fitness function %q", name)
		}
		normalized[name] = enabled
	}
	return normalized, nil
}
