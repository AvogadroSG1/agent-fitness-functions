package analyzer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCommittedBaselineReportsPreserveCALMNodeWireContract(t *testing.T) {
	reports, err := filepath.Glob(filepath.Join("..", "..", "baseline-report-*.json"))
	if err != nil {
		t.Fatalf("filepath.Glob(baseline reports) error = %v, want nil", err)
	}
	if len(reports) == 0 {
		t.Skip("baseline reports = 0, skipping committed baseline contract check")
	}
	for _, path := range reports {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
			}
			var report BaselineReport
			if err := json.Unmarshal(content, &report); err != nil {
				t.Fatalf("json.Unmarshal(BaselineReport) error = %v, want nil", err)
			}
			var raw struct {
				Results []map[string]json.RawMessage `json:"results"`
			}
			if err := json.Unmarshal(content, &raw); err != nil {
				t.Fatalf("json.Unmarshal(raw baseline report) error = %v, want nil", err)
			}
			if len(report.Results) == 0 || len(report.Results) != len(raw.Results) {
				t.Fatalf("decoded results = %d, raw results = %d, want equal non-zero counts", len(report.Results), len(raw.Results))
			}
			for i, result := range report.Results {
				if _, ok := raw.Results[i]["stack_node"]; ok {
					t.Fatalf("results[%d] contains stack_node, want only calm_node", i)
				}
				calmNode, ok := raw.Results[i]["calm_node"]
				if !ok || string(calmNode) == `""` || string(calmNode) == "null" {
					t.Fatalf("raw results[%d] calm_node = %s, %v, want non-empty value", i, calmNode, ok)
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					t.Fatalf("json.Marshal(results[%d]) error = %v, want nil", i, err)
				}
				var roundTrip map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &roundTrip); err != nil {
					t.Fatalf("json.Unmarshal(results[%d] round trip) error = %v, want nil", i, err)
				}
				if string(roundTrip["calm_node"]) != string(calmNode) {
					t.Fatalf("round-trip results[%d] calm_node = %s, want %s", i, roundTrip["calm_node"], calmNode)
				}
				if _, ok := roundTrip["stack_node"]; ok {
					t.Fatalf("round-trip results[%d] contains stack_node, want only calm_node", i)
				}
			}
		})
	}
}

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

func TestAnalyzeRepositoryAggregatesModuleMetricsByCALMNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "first.go"), []byte(`package sample

func First() string {
	return "first"
}
`), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "second.go"), []byte(`package sample

func Second() string {
	return "second"
}
`), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	results, err := AnalyzeRepository(context.Background(), dir, "go", RepositoryOptions{})
	if err != nil {
		t.Fatalf("AnalyzeRepository returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.CALMNode != "sample" || result.ModuleMetric.PublicMethods != 2 {
			t.Fatalf("result = %+v, want aggregated sample module with two public methods", result)
		}
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
