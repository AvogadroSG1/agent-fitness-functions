package hooks

import (
	"os"
	"strings"
	"testing"
)

// Red test for calm-poc-t16 (S9): the hook violation formatter must carry a
// guidance entry for every configured fitness function, or hook output
// degrades to raw JSON for that rule. Guards the four generalized functions
// and any future addition.
func TestGuidanceCoversEveryFitnessFunction(t *testing.T) {
	content, err := os.ReadFile("format-violations.py")
	if err != nil {
		t.Fatalf("read format-violations.py: %v", err)
	}
	for _, name := range []string{
		"cyclomatic-complexity",
		"interface-width",
		"implementation-depth",
		"logic-density",
		"dependency-discipline",
		"layer-sovereignty",
		"temporal-purity",
		"sql-composition-safety",
		"deterministic-ordering",
	} {
		if !strings.Contains(string(content), `"`+name+`": {`) {
			t.Errorf("format-violations.py guidance missing entry for %q", name)
		}
	}
}
