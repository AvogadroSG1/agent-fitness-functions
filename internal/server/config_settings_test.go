package server

import (
	"strings"
	"testing"
)

// Red tests for calm-poc-0l9 (S2): the fitness-function-settings config
// extension. Contract:
//   - Optional top-level "fitness-function-settings" object keyed by function
//     name; unknown keys are input errors (mirrors normalizeFitnessFunctions).
//   - layer-sovereignty settings define layers: name + path globs + forbidden
//     content regexes, compiled at parse time (invalid regex is an input
//     error). Globs support "**" (crosses directory separators), "*" (does
//     not cross "/"), and "?"; every other character is literal.
//   - Enabling layer-sovereignty without at least one layer is an input error
//     (fail closed); settings for a disabled function are allowed
//     (pre-staging).
//   - deterministic-ordering settings define tie-breaker-tokens with defaults
//     when absent; temporal-purity settings define csharp-policy
//     ("naive-only" default, "require-offset" opt-in).
//   - The four generalized functions are registered in defaultConfig as
//     disabled, so existing repo configs keep parsing and repos opt in.

const settingsConfig = `{
	"enforcement-mode": "block",
	"fitness-functions": {
		"layer-sovereignty": true,
		"deterministic-ordering": true,
		"temporal-purity": true
	},
	"fitness-function-settings": {
		"layer-sovereignty": {
			"layers": [
				{
					"name": "bronze",
					"paths": ["src/bronze/**"],
					"forbidden-patterns": ["\\bsilver\\.", "\\bgold\\."]
				},
				{
					"name": "consumer",
					"paths": ["consumers/*.cs"],
					"forbidden-patterns": ["\\braw_[a-z_]+\\b"]
				}
			]
		},
		"deterministic-ordering": {
			"tie-breaker-tokens": ["_id", "_pk"]
		},
		"temporal-purity": {
			"csharp-policy": "require-offset"
		}
	}
}`

func TestParseConfigContentParsesLayerSovereigntySettings(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(settingsConfig))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}

	layers := config.layerRules()
	if len(layers) != 2 {
		t.Fatalf("layerRules() = %d layers, want 2", len(layers))
	}
	if layers[0].Name != "bronze" || layers[1].Name != "consumer" {
		t.Fatalf("layer names = %q, %q, want bronze, consumer", layers[0].Name, layers[1].Name)
	}
}

func TestLayerRuleGlobMatching(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(settingsConfig))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	layers := config.layerRules()
	bronze, consumer := layers[0], layers[1]

	if !bronze.matchesPath("src/bronze/models/orders.py") {
		t.Error("bronze ** glob did not cross directory separators, want match")
	}
	if !bronze.matchesPath("src/bronze/orders.py") {
		t.Error("bronze ** glob did not match direct child, want match")
	}
	if bronze.matchesPath("src/silver/orders.py") {
		t.Error("bronze glob matched src/silver path, want no match")
	}
	if !consumer.matchesPath("consumers/App.cs") {
		t.Error("consumer * glob did not match direct child, want match")
	}
	if consumer.matchesPath("consumers/web/App.cs") {
		t.Error("consumer * glob crossed directory separator, want no match")
	}
}

func TestLayerRuleForbiddenMatches(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(settingsConfig))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	bronze := config.layerRules()[0]

	matches := bronze.forbiddenMatches("SELECT * FROM silver.orders JOIN gold.dim_person USING (person_key)")
	if len(matches) != 2 {
		t.Fatalf("forbiddenMatches() = %v, want the silver and gold patterns to match", matches)
	}
	if len(bronze.forbiddenMatches("SELECT * FROM raw_bamboo.employees")) != 0 {
		t.Error("forbiddenMatches() flagged raw_ reference for bronze layer, want none")
	}
}

func TestParseConfigContentRejectsUnknownSettingsKey(t *testing.T) {
	t.Parallel()

	_, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {},
		"fitness-function-settings": {
			"cyclomatic-complexity": {"threshold": 5}
		}
	}`))
	if err == nil {
		t.Fatal("parseConfigContent() error = nil, want unsupported settings key error")
	}
}

func TestParseConfigContentRejectsInvalidForbiddenPatternRegex(t *testing.T) {
	t.Parallel()

	_, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {"layer-sovereignty": true},
		"fitness-function-settings": {
			"layer-sovereignty": {
				"layers": [
					{"name": "bronze", "paths": ["src/bronze/**"], "forbidden-patterns": ["[unclosed"]}
				]
			}
		}
	}`))
	if err == nil {
		t.Fatal("parseConfigContent() error = nil, want invalid forbidden-pattern regex error")
	}
}

func TestParseConfigContentRejectsLayerSovereigntyEnabledWithoutLayers(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"no settings": `{
			"enforcement-mode": "block",
			"fitness-functions": {"layer-sovereignty": true}
		}`,
		"empty layers": `{
			"enforcement-mode": "block",
			"fitness-functions": {"layer-sovereignty": true},
			"fitness-function-settings": {"layer-sovereignty": {"layers": []}}
		}`,
	} {
		if _, err := parseConfigContent([]byte(content)); err == nil {
			t.Errorf("parseConfigContent() error = nil for %s, want layer-sovereignty misconfiguration error", name)
		}
	}
}

func TestParseConfigContentRejectsIncompleteLayer(t *testing.T) {
	t.Parallel()

	for name, layer := range map[string]string{
		"missing name":               `{"paths": ["src/**"], "forbidden-patterns": ["x"]}`,
		"missing paths":              `{"name": "bronze", "forbidden-patterns": ["x"]}`,
		"missing forbidden-patterns": `{"name": "bronze", "paths": ["src/**"]}`,
	} {
		content := `{
			"enforcement-mode": "block",
			"fitness-functions": {"layer-sovereignty": true},
			"fitness-function-settings": {"layer-sovereignty": {"layers": [` + layer + `]}}
		}`
		if _, err := parseConfigContent([]byte(content)); err == nil {
			t.Errorf("parseConfigContent() error = nil for %s, want incomplete layer error", name)
		}
	}
}

func TestParseConfigContentAllowsSettingsForDisabledFunction(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {"layer-sovereignty": false},
		"fitness-function-settings": {
			"layer-sovereignty": {
				"layers": [
					{"name": "bronze", "paths": ["src/bronze/**"], "forbidden-patterns": ["\\bsilver\\."]}
				]
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v, want pre-staged settings accepted", err)
	}
	if config.enabled("layer-sovereignty") {
		t.Error("enabled(layer-sovereignty) = true, want false")
	}
}

func TestDefaultConfigRegistersGeneralizedFunctionsDisabled(t *testing.T) {
	t.Parallel()

	defaults := defaultConfig().FitnessFunctions
	for _, name := range []string{
		"layer-sovereignty",
		"temporal-purity",
		"sql-composition-safety",
		"deterministic-ordering",
	} {
		enabled, ok := defaults[name]
		if !ok {
			t.Errorf("defaultConfig() missing %q", name)
			continue
		}
		if enabled {
			t.Errorf("defaultConfig()[%q] = true, want opt-in default false", name)
		}
	}

	config, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {"temporal-purity": true}
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v, want temporal-purity accepted", err)
	}
	if !config.enabled("temporal-purity") {
		t.Error("enabled(temporal-purity) = false, want true")
	}
	if config.enabled("layer-sovereignty") {
		t.Error("enabled(layer-sovereignty) = true, want default false")
	}
}

func TestConfigTieBreakerTokens(t *testing.T) {
	t.Parallel()

	custom, err := parseConfigContent([]byte(settingsConfig))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	tokens := custom.tieBreakerTokens()
	if len(tokens) != 2 || tokens[0] != "_id" || tokens[1] != "_pk" {
		t.Fatalf("tieBreakerTokens() = %v, want configured [_id _pk]", tokens)
	}

	defaulted, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {"deterministic-ordering": true}
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	want := []string{"_id", "_key", "_pk", "_sk", "id"}
	got := defaulted.tieBreakerTokens()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tieBreakerTokens() = %v, want defaults %v", got, want)
	}
}

func TestConfigTemporalPurityCSharpPolicy(t *testing.T) {
	t.Parallel()

	custom, err := parseConfigContent([]byte(settingsConfig))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	if policy := custom.temporalPurityCSharpPolicy(); policy != "require-offset" {
		t.Fatalf("temporalPurityCSharpPolicy() = %q, want require-offset", policy)
	}

	defaulted, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {"temporal-purity": true}
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	if policy := defaulted.temporalPurityCSharpPolicy(); policy != "naive-only" {
		t.Fatalf("temporalPurityCSharpPolicy() = %q, want default naive-only", policy)
	}

	_, err = parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {"temporal-purity": true},
		"fitness-function-settings": {"temporal-purity": {"csharp-policy": "yolo"}}
	}`))
	if err == nil {
		t.Fatal("parseConfigContent() error = nil, want unsupported csharp-policy error")
	}
}
