package sarif_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/poconnor/calm-poc/internal/fitness"
	"github.com/poconnor/calm-poc/internal/sarif"
)

func TestConvertEmptyViolationsProducesEmptyResults(t *testing.T) {
	resp := fitness.ValidationResult{Status: fitness.StatusPass}
	out := marshalSARIF(t, sarif.Convert(resp, ""))

	if len(out.Runs) != 1 {
		t.Fatalf("runs len = %d, want 1", len(out.Runs))
	}
	if len(out.Runs[0].Results) != 0 {
		t.Fatalf("results len = %d, want 0", len(out.Runs[0].Results))
	}
}

func TestConvertBlockStatusProducesErrorLevel(t *testing.T) {
	resp := fitness.ValidationResult{
		Status: fitness.StatusBlock,
		Violations: []fitness.Violation{
			{FitnessFunction: "cyclomatic_complexity", Message: "too complex"},
		},
	}
	out := marshalSARIF(t, sarif.Convert(resp, ""))

	if out.Runs[0].Results[0].Level != "error" {
		t.Fatalf("level = %q, want error", out.Runs[0].Results[0].Level)
	}
}

func TestConvertAdvisoryStatusProducesWarningLevel(t *testing.T) {
	resp := fitness.ValidationResult{
		Status: fitness.StatusAdvisory,
		Violations: []fitness.Violation{
			{FitnessFunction: "logic_density", Message: "low density"},
		},
	}
	out := marshalSARIF(t, sarif.Convert(resp, ""))

	if out.Runs[0].Results[0].Level != "warning" {
		t.Fatalf("level = %q, want warning", out.Runs[0].Results[0].Level)
	}
}

func TestConvertViolationWithFileProducesLocation(t *testing.T) {
	resp := fitness.ValidationResult{
		Status: fitness.StatusBlock,
		Violations: []fitness.Violation{
			{FitnessFunction: "f", Message: "msg", File: "/repo/internal/foo/bar.go"},
		},
	}
	out := marshalSARIF(t, sarif.Convert(resp, "/repo"))

	r := out.Runs[0].Results[0]
	if len(r.Locations) == 0 {
		t.Fatal("expected location, got none")
	}
	uri := r.Locations[0].PhysicalLocation.ArtifactLocation.URI
	if !strings.HasSuffix(uri, "internal/foo/bar.go") {
		t.Fatalf("uri = %q, want suffix internal/foo/bar.go", uri)
	}
}

func TestConvertDuplicateRulesAreDeduped(t *testing.T) {
	resp := fitness.ValidationResult{
		Status: fitness.StatusBlock,
		Violations: []fitness.Violation{
			{FitnessFunction: "ff1", Message: "a"},
			{FitnessFunction: "ff1", Message: "b"},
			{FitnessFunction: "ff2", Message: "c"},
		},
	}
	out := marshalSARIF(t, sarif.Convert(resp, ""))

	if len(out.Runs[0].Tool.Driver.Rules) != 2 {
		t.Fatalf("rules len = %d, want 2", len(out.Runs[0].Tool.Driver.Rules))
	}
}

func TestConvertIncludesSchemaVersion(t *testing.T) {
	out := marshalSARIF(t, sarif.Convert(fitness.ValidationResult{Status: fitness.StatusPass}, ""))

	if out.Version != "2.1.0" {
		t.Fatalf("version = %q, want 2.1.0", out.Version)
	}
	if !strings.Contains(out.Schema, "sarif-schema-2.1.0") {
		t.Fatalf("schema = %q, want sarif-schema-2.1.0", out.Schema)
	}
}

// sarifDoc mirrors the top-level SARIF structure for JSON unmarshalling in tests.
type sarifDoc struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Rules []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID string `json:"id"`
}

type sarifResult struct {
	Level     string          `json:"level"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

func marshalSARIF(t *testing.T, v any) sarifDoc {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc sarifDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return doc
}
