package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// FitnessFunctionSettings holds structured configuration for the fitness
// functions that need more than an enabled/disabled flag. Each field is
// optional and independent of whether the corresponding function is enabled
// in fitness-functions, so a repository can pre-stage settings before
// opting in.
type FitnessFunctionSettings struct {
	LayerSovereignty      *LayerSovereigntySettings      `json:"layer-sovereignty,omitempty"`
	DeterministicOrdering *DeterministicOrderingSettings `json:"deterministic-ordering,omitempty"`
	TemporalPurity        *TemporalPuritySettings        `json:"temporal-purity,omitempty"`
}

// LayerSovereigntySettings configures the layer-sovereignty fitness function:
// the set of architectural layers a repository recognizes.
type LayerSovereigntySettings struct {
	Layers []LayerRule `json:"layers"`
}

// LayerRule defines one architectural layer: the path globs that place a
// file in the layer, and the content patterns forbidden from appearing in
// those files (e.g. a bronze-layer file referencing silver/gold objects).
// pathPatterns and forbiddenPatterns are compiled from Paths and
// ForbiddenPatterns at parse time and are not serialized.
type LayerRule struct {
	Name              string   `json:"name"`
	Paths             []string `json:"paths"`
	ForbiddenPatterns []string `json:"forbidden-patterns"`

	pathPatterns      []*regexp.Regexp
	forbiddenPatterns []*regexp.Regexp
}

// DeterministicOrderingSettings configures the deterministic-ordering
// fitness function.
type DeterministicOrderingSettings struct {
	TieBreakerTokens []string `json:"tie-breaker-tokens"`
}

// TemporalPuritySettings configures the temporal-purity fitness function.
type TemporalPuritySettings struct {
	CSharpPolicy string `json:"csharp-policy"`
}

// defaultTieBreakerTokens is used when deterministic-ordering settings are
// absent or specify no tie-breaker-tokens.
var defaultTieBreakerTokens = []string{"_id", "_key", "_pk", "_sk", "id"}

// defaultTemporalPurityCSharpPolicy is used when temporal-purity settings
// are absent or specify no csharp-policy.
const defaultTemporalPurityCSharpPolicy = "naive-only"

var allowedFitnessFunctionSettingsKeys = map[string]bool{
	"layer-sovereignty":      true,
	"deterministic-ordering": true,
	"temporal-purity":        true,
}

// layerRules returns the compiled layer-sovereignty layers, or nil if none
// are configured.
func (c Config) layerRules() []LayerRule {
	if c.FitnessFunctionSettings == nil || c.FitnessFunctionSettings.LayerSovereignty == nil {
		return nil
	}
	return c.FitnessFunctionSettings.LayerSovereignty.Layers
}

// tieBreakerTokens returns the configured deterministic-ordering
// tie-breaker tokens, falling back to defaultTieBreakerTokens when absent.
func (c Config) tieBreakerTokens() []string {
	if c.FitnessFunctionSettings != nil &&
		c.FitnessFunctionSettings.DeterministicOrdering != nil &&
		len(c.FitnessFunctionSettings.DeterministicOrdering.TieBreakerTokens) > 0 {
		return c.FitnessFunctionSettings.DeterministicOrdering.TieBreakerTokens
	}
	return defaultTieBreakerTokens
}

// temporalPurityCSharpPolicy returns the configured temporal-purity
// csharp-policy, falling back to defaultTemporalPurityCSharpPolicy when
// absent.
func (c Config) temporalPurityCSharpPolicy() string {
	if c.FitnessFunctionSettings != nil &&
		c.FitnessFunctionSettings.TemporalPurity != nil &&
		c.FitnessFunctionSettings.TemporalPurity.CSharpPolicy != "" {
		return c.FitnessFunctionSettings.TemporalPurity.CSharpPolicy
	}
	return defaultTemporalPurityCSharpPolicy
}

// matchesPath reports whether path falls within this layer, per its
// compiled path globs. path separators are normalized to "/" before
// matching.
func (r LayerRule) matchesPath(path string) bool {
	normalized := strings.ReplaceAll(path, `\`, "/")
	for _, pattern := range r.pathPatterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}
	return false
}

// forbiddenMatches returns the source text of every forbidden pattern that
// matches content, each at most once, in declaration order.
func (r LayerRule) forbiddenMatches(content string) []string {
	var matches []string
	for i, pattern := range r.forbiddenPatterns {
		if pattern.MatchString(content) {
			matches = append(matches, r.ForbiddenPatterns[i])
		}
	}
	return matches
}

// rejectUnknownFitnessFunctionSettingsKeys decodes the raw
// fitness-function-settings object and fails closed on any key outside the
// known set, mirroring normalizeFitnessFunctions. json.Unmarshal into a
// typed struct silently ignores unknown object keys, so this must inspect
// the raw content directly.
func rejectUnknownFitnessFunctionSettingsKeys(content []byte) error {
	var wrapper struct {
		Settings map[string]json.RawMessage `json:"fitness-function-settings"`
	}
	if err := json.Unmarshal(content, &wrapper); err != nil {
		return inputError("parsing fitness function settings", err)
	}
	for name := range wrapper.Settings {
		if !allowedFitnessFunctionSettingsKeys[name] {
			return inputError(fmt.Sprintf("unsupported fitness function settings %q", name), nil)
		}
	}
	return nil
}

// validateFitnessFunctionSettings compiles and validates
// config.FitnessFunctionSettings in place (fail closed), then enforces the
// layer-sovereignty enablement invariant: enabling the function without at
// least one configured layer is an input error. Settings staged for a
// disabled function are always allowed.
func validateFitnessFunctionSettings(config *Config) error {
	if config.FitnessFunctionSettings != nil {
		settings := config.FitnessFunctionSettings
		if settings.LayerSovereignty != nil {
			layers := make([]LayerRule, 0, len(settings.LayerSovereignty.Layers))
			for _, layer := range settings.LayerSovereignty.Layers {
				compiled, err := compileLayerRule(layer)
				if err != nil {
					return err
				}
				layers = append(layers, compiled)
			}
			settings.LayerSovereignty.Layers = layers
		}
		if settings.TemporalPurity != nil {
			switch settings.TemporalPurity.CSharpPolicy {
			case "", "naive-only", "require-offset":
			default:
				return inputError(
					fmt.Sprintf("unsupported temporal-purity csharp-policy %q", settings.TemporalPurity.CSharpPolicy),
					nil,
				)
			}
		}
	}

	if config.enabled("layer-sovereignty") {
		layers := config.layerRules()
		if len(layers) == 0 {
			return inputError(
				"layer-sovereignty is enabled but fitness-function-settings.layer-sovereignty.layers is empty",
				nil,
			)
		}
	}
	return nil
}

// compileLayerRule validates one layer rule and compiles its path globs and
// forbidden-pattern regexes.
func compileLayerRule(rule LayerRule) (LayerRule, error) {
	if rule.Name == "" {
		return LayerRule{}, inputError("layer-sovereignty layer is missing a name", nil)
	}
	if len(rule.Paths) == 0 {
		return LayerRule{}, inputError(fmt.Sprintf("layer-sovereignty layer %q has no paths", rule.Name), nil)
	}
	if len(rule.ForbiddenPatterns) == 0 {
		return LayerRule{}, inputError(fmt.Sprintf("layer-sovereignty layer %q has no forbidden-patterns", rule.Name), nil)
	}

	compiled := rule
	compiled.pathPatterns = make([]*regexp.Regexp, 0, len(rule.Paths))
	for _, glob := range rule.Paths {
		pattern, err := globToRegexp(glob)
		if err != nil {
			return LayerRule{}, inputError(
				fmt.Sprintf("layer-sovereignty layer %q has invalid path glob %q", rule.Name, glob),
				err,
			)
		}
		compiled.pathPatterns = append(compiled.pathPatterns, pattern)
	}

	compiled.forbiddenPatterns = make([]*regexp.Regexp, 0, len(rule.ForbiddenPatterns))
	for _, source := range rule.ForbiddenPatterns {
		pattern, err := regexp.Compile(source)
		if err != nil {
			return LayerRule{}, inputError(
				fmt.Sprintf("layer-sovereignty layer %q has invalid forbidden-pattern %q", rule.Name, source),
				err,
			)
		}
		compiled.forbiddenPatterns = append(compiled.forbiddenPatterns, pattern)
	}

	return compiled, nil
}

// globToRegexp compiles a path glob into an anchored regexp. "**" matches
// any characters including "/"; "*" matches any characters except "/"; "?"
// matches exactly one non-"/" character; every other character is matched
// literally.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
