package calm

import "testing"

// Red test for calm-poc-6w6 (S1): the repository governance pattern must define
// the four generalized fitness functions as count metrics with threshold 0.
func TestLoadPatternParsesGeneralizedCountFunctions(t *testing.T) {
	pattern, err := LoadPattern("../../patterns/governance.json")
	if err != nil {
		t.Fatalf("LoadPattern returned error: %v", err)
	}

	assertRule(t, pattern, "layer-sovereignty", 0, "lte", "file")
	assertRule(t, pattern, "temporal-purity", 0, "lte", "file")
	assertRule(t, pattern, "sql-composition-safety", 0, "lte", "file")
	assertRule(t, pattern, "deterministic-ordering", 0, "lte", "file")
}
