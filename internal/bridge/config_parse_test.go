package bridge

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
