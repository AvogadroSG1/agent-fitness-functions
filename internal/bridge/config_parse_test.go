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
