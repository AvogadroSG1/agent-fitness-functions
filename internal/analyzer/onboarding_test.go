package analyzer

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
)

func testRules() map[string]calm.FitnessRule {
	return map[string]calm.FitnessRule{
		"cyclomatic-complexity": {Threshold: 9, Operator: "lte"},
		"interface-width":       {Threshold: 20, Operator: "lte"},
		"implementation-depth":  {Threshold: 0.722, Operator: "gte"},
		"logic-density":         {Threshold: 0.255, Operator: "gte"},
		"dependency-discipline": {Threshold: 0.8, Operator: "gte"},
	}
}

func greenResult() AnalysisResult {
	return AnalysisResult{
		StackNode:     "sample",
		Language:     "go",
		File:         "green.go",
		Functions:    []FunctionMetric{{Name: "Run", CyclomaticComplexity: 2, IsPublic: true, LOC: 10}},
		ModuleMetric: ModuleMetric{PublicMethods: 3, TotalLOC: 30, AverageLOCPerPublicMethod: 10},
		FileMetric:   FileMetric{TotalLOC: 30, LogicLOC: 27, PublicMethods: 3, LDR: 0.9},
		Imports:      ImportMetric{Total: 2, Used: 2, DDC: 1},
	}
}

func TestBuildOnboardingRecommendationBlockWhenNoViolations(t *testing.T) {
	results := []AnalysisResult{greenResult()}

	rec := BuildOnboardingRecommendation("greenrepo", results, testRules())

	if rec.EnforcementMode != "block" {
		t.Fatalf("enforcement-mode = %q, want block", rec.EnforcementMode)
	}
	if rec.TotalViolations != 0 {
		t.Fatalf("total violations = %d, want 0", rec.TotalViolations)
	}
	if len(rec.Deltas) != 5 {
		t.Fatalf("deltas = %d, want 5", len(rec.Deltas))
	}
}

func TestBuildOnboardingRecommendationAdvisoryWhenViolations(t *testing.T) {
	green := greenResult()
	dirty := greenResult()
	dirty.File = "dirty.go"
	dirty.Functions = []FunctionMetric{{Name: "Hot", CyclomaticComplexity: 24, IsPublic: true, LOC: 40}}

	rec := BuildOnboardingRecommendation("dirtyrepo", []AnalysisResult{green, dirty}, testRules())

	if rec.EnforcementMode != "advisory" {
		t.Fatalf("enforcement-mode = %q, want advisory", rec.EnforcementMode)
	}
	if rec.TotalViolations != 1 {
		t.Fatalf("total violations = %d, want 1", rec.TotalViolations)
	}
}

func TestThresholdDeltaComputation(t *testing.T) {
	dirty := greenResult()
	dirty.Functions = []FunctionMetric{{Name: "Hot", CyclomaticComplexity: 12, IsPublic: true, LOC: 40}}

	rec := BuildOnboardingRecommendation("repo", []AnalysisResult{dirty}, testRules())

	cc := findDelta(t, rec.Deltas, "cyclomatic-complexity")
	if cc.Operator != "lte" || cc.GlobalThreshold != 9 {
		t.Fatalf("cc delta = %+v, want lte threshold 9", cc)
	}
	if cc.RepositoryValue != 12 {
		t.Fatalf("cc repository percentile = %v, want 12 (only function)", cc.RepositoryValue)
	}
	if cc.Delta != 3 {
		t.Fatalf("cc delta value = %v, want 3", cc.Delta)
	}
	if !cc.NeedsLooser {
		t.Fatalf("cc needs-looser = false, want true (P90 12 > threshold 9)")
	}
	if cc.ViolatingCount != 1 || cc.ViolationUnit != "functions" {
		t.Fatalf("cc violations = %d %q, want 1 functions", cc.ViolatingCount, cc.ViolationUnit)
	}

	density := findDelta(t, rec.Deltas, "logic-density")
	if density.Operator != "gte" || density.NeedsLooser {
		t.Fatalf("density delta = %+v, want gte and needs-looser false (0.9 >= 0.255)", density)
	}
}

func TestWriteOnboardingConfigShapeAndParse(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := WriteOnboardingConfig(path, "advisory"); err != nil {
		t.Fatalf("WriteOnboardingConfig returned error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	// Round-trip through a struct mirroring the server Config shape.
	var parsed EmittedConfig
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("emitted config does not parse: %v\n%s", err, content)
	}
	if parsed.EnforcementMode != "advisory" {
		t.Fatalf("enforcement-mode = %q, want advisory", parsed.EnforcementMode)
	}
	if parsed.EnforcementOnError != "block" {
		t.Fatalf("enforcement-on-error = %q, want block", parsed.EnforcementOnError)
	}
	wantFunctions := []string{
		"cyclomatic-complexity", "interface-width", "implementation-depth",
		"logic-density", "dependency-discipline",
	}
	if len(parsed.FitnessFunctions) != len(wantFunctions) {
		t.Fatalf("fitness functions = %v, want all five", parsed.FitnessFunctions)
	}
	for _, name := range wantFunctions {
		if !parsed.FitnessFunctions[name] {
			t.Fatalf("fitness function %q not enabled in %v", name, parsed.FitnessFunctions)
		}
	}
}

func TestWriteOnboardingReportIncludesLimitationNote(t *testing.T) {
	rec := BuildOnboardingRecommendation("repo", []AnalysisResult{greenResult()}, testRules())

	var buf bytes.Buffer
	if err := WriteOnboardingReport(&buf, rec); err != nil {
		t.Fatalf("WriteOnboardingReport returned error: %v", err)
	}
	out := buf.String()
	for _, needle := range []string{
		"enforcement-mode: block",
		"Threshold delta",
		"GLOBAL and compiled into the binary",
		"docs/threshold-calibration.md",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("report missing %q:\n%s", needle, out)
		}
	}
}

func TestGlobalThresholdsMatchesEmbeddedPattern(t *testing.T) {
	rules, err := GlobalThresholds()
	if err != nil {
		t.Fatalf("GlobalThresholds returned error: %v", err)
	}
	cc, ok := rules["cyclomatic-complexity"]
	if !ok || cc.Operator != "lte" || cc.Threshold != 9 {
		t.Fatalf("cyclomatic-complexity rule = %+v, want lte 9", cc)
	}
	if len(rules) != 5 {
		t.Fatalf("rules = %d, want 5 fitness functions", len(rules))
	}
}

func findDelta(t *testing.T, deltas []ThresholdDelta, name string) ThresholdDelta {
	t.Helper()
	for _, delta := range deltas {
		if delta.FitnessFunction == name {
			return delta
		}
	}
	t.Fatalf("delta %q not found in %+v", name, deltas)
	return ThresholdDelta{}
}
