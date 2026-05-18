package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBaselineReportSummarizesResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline-report.json")
	results := []AnalysisResult{
		{
			CALMNode:   "sample",
			Language:   "go",
			File:       "sample.go",
			Functions:  []FunctionMetric{{Name: "Run", CyclomaticComplexity: 7, IsPublic: true, LOC: 20}},
			FileMetric: FileMetric{TotalLOC: 40, LogicLOC: 20, PublicMethods: 1, LDR: 0.5},
			Imports:    ImportMetric{Total: 2, Used: 2, DDC: 1},
		},
	}

	if err := WriteBaselineReport(path, "graft", "go", results); err != nil {
		t.Fatalf("WriteBaselineReport returned error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !containsAll(
		string(content),
		`"repository": "graft"`,
		`"p90_cyclomatic_complexity": 7`,
		`"distributions": {`,
		`"cyclomatic_complexity": [`,
		`"public_methods": [`,
		`"avg_loc_per_public_method": [`,
		`"logic_density_ratio": [`,
		`"dependency_discipline": [`,
	) {
		t.Fatalf("report content missing expected fields:\n%s", content)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !stringsContains(value, needle) {
			return false
		}
	}
	return true
}

func stringsContains(value, needle string) bool {
	return len(needle) == 0 || (len(value) >= len(needle) && index(value, needle) >= 0)
}

func index(value, needle string) int {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
