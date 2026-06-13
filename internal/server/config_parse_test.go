package server

import "testing"

func TestParseConfigContentDefaultsEnforcementOnErrorToBlock(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(`{
		"enforcement-mode": "advisory",
		"fitness-functions": {
			"cyclomatic-complexity": true
		}
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	if config.EnforcementOnError != EnforcementOnErrorBlock {
		t.Fatalf("EnforcementOnError = %q, want %q", config.EnforcementOnError, EnforcementOnErrorBlock)
	}
}

func TestParseConfigContentRejectsUnsupportedEnforcementOnError(t *testing.T) {
	t.Parallel()

	_, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"enforcement-on-error": "panic",
		"fitness-functions": {
			"cyclomatic-complexity": true
		}
	}`))
	if err == nil {
		t.Fatal("parseConfigContent() error = nil, want invalid enforcement-on-error error")
	}
}

func TestParseConfigContentParsesExcludePatterns(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {},
		"exclude-patterns": ["*_test.go", "test_*.py", "*_test.py"]
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	want := []string{"*_test.go", "test_*.py", "*_test.py"}
	if len(config.ExcludePatterns) != len(want) {
		t.Fatalf("ExcludePatterns = %v, want %v", config.ExcludePatterns, want)
	}
	for i, p := range want {
		if config.ExcludePatterns[i] != p {
			t.Errorf("ExcludePatterns[%d] = %q, want %q", i, config.ExcludePatterns[i], p)
		}
	}
}

func TestConfigIsExcludedMatchesGoTestFile(t *testing.T) {
	t.Parallel()

	config := Config{ExcludePatterns: []string{"*_test.go", "test_*.py", "*_test.py"}}
	if !config.isExcluded("internal/server/checker_test.go") {
		t.Error("isExcluded() = false for *_test.go pattern, want true")
	}
}

func TestConfigIsExcludedMatchesPythonTestFile(t *testing.T) {
	t.Parallel()

	config := Config{ExcludePatterns: []string{"*_test.go", "test_*.py", "*_test.py"}}
	if !config.isExcluded("analyzers/test_format_violations.py") {
		t.Error("isExcluded() = false for test_*.py pattern, want true")
	}
	if !config.isExcluded("analyzers/format_violations_test.py") {
		t.Error("isExcluded() = false for *_test.py pattern, want true")
	}
}

func TestConfigIsExcludedDoesNotMatchProductionFile(t *testing.T) {
	t.Parallel()

	config := Config{ExcludePatterns: []string{"*_test.go", "test_*.py", "*_test.py"}}
	if config.isExcluded("internal/server/checker.go") {
		t.Error("isExcluded() = true for production .go file, want false")
	}
	if config.isExcluded("analyzers/format_violations.py") {
		t.Error("isExcluded() = true for production .py file, want false")
	}
}

func TestConfigIsExcludedWithNoPatterns(t *testing.T) {
	t.Parallel()

	config := Config{}
	if config.isExcluded("internal/server/checker_test.go") {
		t.Error("isExcluded() = true with no patterns, want false")
	}
}

func TestParseConfigContentDefaultsExcludePatternsToEmpty(t *testing.T) {
	t.Parallel()

	config, err := parseConfigContent([]byte(`{
		"enforcement-mode": "block",
		"fitness-functions": {}
	}`))
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}
	if len(config.ExcludePatterns) != 0 {
		t.Fatalf("ExcludePatterns = %v, want empty", config.ExcludePatterns)
	}
}
