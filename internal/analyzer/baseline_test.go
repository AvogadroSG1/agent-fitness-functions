package analyzer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const densityGoEmbedSource = `// Package configs embeds static configuration assets.
// Profiles are bundled with the executable.
// Assets are read by the configuration layer.
// Runtime overrides are handled by consumers.
// This file declares assets; it executes no functions.
package configs

import "embed"

//go:embed assets/*
var FS embed.FS
`

func analyzeDensitySource(t *testing.T, source string) AnalysisResult {
	t.Helper()
	path := filepath.Join(t.TempDir(), "embed.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLogicDensityBaselineApplicability(t *testing.T) {
	pure := analyzeDensitySource(t, densityGoEmbedSource)
	executable := analyzeDensitySource(t, densityGoEmbedSource+"func load() string { return \"asset\" }\n")
	for _, tc := range []struct {
		name    string
		results []AnalysisResult
		density []float64
		p10     float64
	}{
		{"mixed", []AnalysisResult{pure, executable}, []float64{0.2}, 0.2},
		{"pure", []AnalysisResult{pure}, []float64{}, 0},
		{"empty", nil, []float64{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "baseline.json")
			if err := WriteBaselineReport(path, "repo-embed", "go", tc.results); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var report BaselineReport
			if err := json.Unmarshal(content, &report); err != nil {
				t.Fatal(err)
			}
			if report.Summary.FileCount != len(tc.results) || len(report.Results) != len(tc.results) || report.Summary.P10LogicDensityRatio != tc.p10 || !reflect.DeepEqual(report.Distributions.LogicDensityRatio, tc.density) {
				t.Fatalf("report = %+v, want %d files, density %v P10 %g", report, len(tc.results), tc.density, tc.p10)
			}
			if got := distributions(report.Results); !reflect.DeepEqual(got, report.Distributions) {
				t.Fatalf("reloaded distributions = %+v, want %+v", got, report.Distributions)
			}
			for i, result := range report.Results {
				want := tc.results[i]
				if result.SourceKind != want.SourceKind || result.FileMetric != want.FileMetric || result.Imports.DDC != want.Imports.DDC {
					t.Fatalf("raw result changed = %+v, want %+v", result, want)
				}
			}
			// Asset classification affects density only: widths and imports still
			// have one observation per analyzed file, functions remain executable.
			if len(report.Distributions.PublicMethods) != len(tc.results) || len(report.Distributions.DependencyDiscipline) != len(tc.results) {
				t.Fatalf("non-density observations changed: %+v", report.Distributions)
			}
			if tc.name == "mixed" {
				if report.Results[0].FileMetric.TotalLOC != 9 || report.Results[0].FileMetric.LogicLOC != 1 || report.Results[0].FileMetric.LDR != 1.0/9 || report.Results[1].FileMetric.LDR != 0.2 || !reflect.DeepEqual(report.Distributions.CyclomaticComplexity, []int{1}) {
					t.Fatalf("raw counts or complexity changed: %+v", report)
				}
			}
		})
	}
}

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
