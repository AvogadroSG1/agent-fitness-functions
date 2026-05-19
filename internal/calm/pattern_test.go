package calm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPatternParsesFitnessFunctions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "governance.json")
	content := `{
  "$schema": "https://calm.finos.org/release/1.2/meta/calm.json",
  "$id": "https://example.invalid/governance.json",
  "title": "CALM PoC Fitness Functions",
  "type": "object",
  "properties": {
    "nodes": {
      "type": "array",
      "items": {
        "properties": {
          "metadata": {
            "properties": {
              "fitness": {
                "properties": {
                  "cyclomatic-complexity": {
                    "description": "Maximum cyclomatic complexity per function",
                    "type": "number",
                    "maximum": 9
                  },
                  "logic-density": {
                    "description": "Minimum logic density ratio per file",
                    "type": "number",
                    "minimum": 0.255
                  }
                }
              }
            }
          }
        }
      }
    },
    "relationships": {"type": "array"}
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pattern: %v", err)
	}

	pattern, err := LoadPattern(path)
	if err != nil {
		t.Fatalf("LoadPattern returned error: %v", err)
	}

	rule, ok := pattern.FitnessFunctions["cyclomatic-complexity"]
	if !ok {
		t.Fatalf("fitness functions = %+v, want cyclomatic-complexity", pattern.FitnessFunctions)
	}
	if pattern.Title != "CALM PoC Fitness Functions" || rule.Threshold != 9 || rule.Operator != "lte" || rule.Unit != "function" {
		t.Fatalf("pattern = %+v, rule = %+v", pattern, rule)
	}
	ldr := pattern.FitnessFunctions["logic-density"]
	if ldr.Threshold != 0.255 || ldr.Operator != "gte" || ldr.Unit != "file" {
		t.Fatalf("logic density rule = %+v, want gte 0.255 file", ldr)
	}
}

func TestLoadPatternRejectsMissingFitnessFunctions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "governance.json")
	if err := os.WriteFile(path, []byte(`{"title":"empty","properties":{"nodes":{},"relationships":{}}}`), 0o644); err != nil {
		t.Fatalf("write pattern: %v", err)
	}

	_, err := LoadPattern(path)
	if err == nil {
		t.Fatal("LoadPattern returned nil error, want missing fitness-functions error")
	}
}

func TestLoadPatternParsesRepositoryGovernance(t *testing.T) {
	pattern, err := LoadPattern("../../patterns/governance.json")
	if err != nil {
		t.Fatalf("LoadPattern returned error: %v", err)
	}

	assertRule(t, pattern, "cyclomatic-complexity", 9, "lte", "function")
	assertRule(t, pattern, "interface-width", 20, "lte", "module")
	assertRule(t, pattern, "implementation-depth", 0.722, "gte", "module")
	assertRule(t, pattern, "logic-density", 0.255, "gte", "file")
	assertRule(t, pattern, "dependency-discipline", 0.8, "gte", "file")
}

func assertRule(t *testing.T, pattern Pattern, name string, threshold float64, operator, unit string) {
	t.Helper()
	rule, ok := pattern.FitnessFunctions[name]
	if !ok {
		t.Fatalf("rule %q not found in %+v", name, pattern.FitnessFunctions)
	}
	if rule.Threshold != threshold || rule.Operator != operator || rule.Unit != unit {
		t.Fatalf("rule %q = %+v, want threshold %v operator %s unit %s", name, rule, threshold, operator, unit)
	}
}
