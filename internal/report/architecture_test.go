package report

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
)

func TestBuildArchitectureMapsAnalysisMetricsToCALMNodeFitness(t *testing.T) {
	document := BuildArchitecture(analyzer.AnalysisResult{
		CALMNode: "parser",
		Language: "go",
		File:     "internal/parser/parser.go",
		Functions: []analyzer.FunctionMetric{
			{Name: "Parse", CyclomaticComplexity: 12, IsPublic: true, LOC: 24},
			{Name: "helper", CyclomaticComplexity: 3, IsPublic: false, LOC: 8},
		},
		FileMetric: analyzer.FileMetric{
			TotalLOC:      80,
			LogicLOC:      40,
			PublicMethods: 1,
			LDR:           0.5,
		},
		Imports: analyzer.ImportMetric{
			Total: 2,
			Used:  1,
			DDC:   0.5,
		},
	})

	if len(document.Nodes) != 2 {
		t.Fatalf("nodes = %d, want actor and analyzed node", len(document.Nodes))
	}
	node := document.Nodes[1]
	if node.UniqueID != "parser" || node.NodeType != "service" || node.Name != "parser" {
		t.Fatalf("node = %+v, want parser service", node)
	}
	fitness := node.Metadata.Fitness
	if fitness.CyclomaticComplexity != 12 {
		t.Fatalf("cyclomatic complexity = %v, want max function complexity 12", fitness.CyclomaticComplexity)
	}
	if fitness.InterfaceWidth != 1 {
		t.Fatalf("interface width = %v, want public method count 1", fitness.InterfaceWidth)
	}
	if fitness.ImplementationDepth != 40 {
		t.Fatalf("implementation depth = %v, want logic LOC per public method", fitness.ImplementationDepth)
	}
	if fitness.LogicDensity == nil || *fitness.LogicDensity != 0.5 {
		t.Fatalf("logic density = %v, want LDR", fitness.LogicDensity)
	}
	if fitness.DependencyDiscipline != 0.5 {
		t.Fatalf("dependency discipline = %v, want DDC", fitness.DependencyDiscipline)
	}
	if node.Metadata.ModuleMetrics == nil {
		t.Fatal("module_metrics missing from analyzed node metadata")
	}
	moduleMetrics := *node.Metadata.ModuleMetrics
	if moduleMetrics.PublicMethods != 1 || moduleMetrics.TotalLOC != 80 || moduleMetrics.PrivateLOC != 56 || moduleMetrics.AverageLOCPerPublicMethod != 40 {
		t.Fatalf("module metrics = %+v, want public=1 total=80 private=56 avg=40", moduleMetrics)
	}
	if node.Metadata.FileMetrics == nil || node.Metadata.FileMetrics.TotalLines != 80 || node.Metadata.FileMetrics.LogicLines != 40 {
		t.Fatalf("file metrics = %+v, want total=80 logic=40", node.Metadata.FileMetrics)
	}
	if node.Metadata.ImportMetrics == nil || node.Metadata.ImportMetrics.TotalImports != 2 || node.Metadata.ImportMetrics.UsedImports != 1 {
		t.Fatalf("import metrics = %+v, want total=2 used=1", node.Metadata.ImportMetrics)
	}
	content, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal architecture document: %v", err)
	}
	for _, field := range []string{
		`"interface-width":1`,
		`"implementation-depth":40`,
		`"module_metrics"`,
		`"public_method_count":1`,
		`"avg_loc_per_public_method":40`,
		`"file_metrics"`,
		`"import_metrics"`,
	} {
		if !strings.Contains(string(content), field) {
			t.Fatalf("architecture JSON = %s, want field %s", content, field)
		}
	}
}

const goEmbedAssetArchitectureSource = `// Package configs embeds static configuration assets.
// Profiles are bundled with the executable.
// Assets are read by the configuration layer.
// Runtime overrides are handled by consumers.
// This file declares assets; it executes no functions.
package configs

import "embed"

//go:embed assets/*
var FS embed.FS
`

func TestBuildArchitectureGoEmbedAssetsCALM(t *testing.T) {
	cli, err := exec.LookPath("calm")
	if err != nil {
		t.Skip("calm CLI not installed")
	}
	pattern, err := filepath.Abs("../../patterns/governance.json")
	if err != nil {
		t.Fatalf("resolve governance pattern: %v", err)
	}

	pure := analyzeArchitectureSource(t, goEmbedAssetArchitectureSource)
	if pure.SourceKind != analyzer.SourceKindGoEmbedAssets || analyzer.LogicDensityApplicable(pure) {
		t.Fatalf("pure result classification = %q, applicable=%v", pure.SourceKind, analyzer.LogicDensityApplicable(pure))
	}
	if pure.FileMetric.TotalLOC != 9 || pure.FileMetric.LogicLOC != 1 || pure.FileMetric.LDR != 1.0/9.0 {
		t.Fatalf("pure raw metrics = %+v, want total=9 logic=1 LDR=1/9", pure.FileMetric)
	}
	pureDocument := BuildArchitecture(pure)
	if len(pureDocument.Nodes) != 2 || len(pureDocument.Relationships) != 1 {
		t.Fatalf("pure document shape = nodes %d relationships %d, want 2 and 1", len(pureDocument.Nodes), len(pureDocument.Relationships))
	}
	pureService := pureDocument.Nodes[1]
	if len(pureService.Metadata.Interfaces) != 0 {
		t.Fatalf("pure asset interfaces = %d, want none", len(pureService.Metadata.Interfaces))
	}
	if pureService.Metadata.FileMetrics == nil ||
		pureService.Metadata.FileMetrics.LDR != pure.FileMetric.LDR ||
		pureService.Metadata.FileMetrics.SourceKind != analyzer.SourceKindGoEmbedAssets {
		t.Fatalf("pure asset file metrics = %+v, want raw LDR and source marker", pureService.Metadata.FileMetrics)
	}
	if pureDocument.Nodes[0].Metadata.Fitness.LogicDensity == nil ||
		*pureDocument.Nodes[0].Metadata.Fitness.LogicDensity != 1 {
		t.Fatalf("actor logic density = %v, want 1", pureDocument.Nodes[0].Metadata.Fitness.LogicDensity)
	}
	purePayload := architecturePayload(t, pureDocument)
	pureFitness := serviceFitnessPayload(t, purePayload)
	if _, ok := pureFitness["logic-density"]; ok {
		t.Fatalf("pure asset JSON emits service logic density: %v", pureFitness)
	}
	pureMetrics := serviceMetadataPayload(t, purePayload)["file_metrics"].(map[string]any)
	if pureMetrics["ldr"] != pure.FileMetric.LDR || pureMetrics["source_kind"] != string(analyzer.SourceKindGoEmbedAssets) {
		t.Fatalf("pure asset JSON file metrics = %v", pureMetrics)
	}
	actorFitness := nodeMetadataPayload(t, purePayload, 0)["fitness"].(map[string]any)
	if actorFitness["logic-density"] != float64(1) {
		t.Fatalf("actor JSON logic density = %v, want 1", actorFitness["logic-density"])
	}
	validateArchitecturePayload(t, cli, pattern, purePayload, true)

	executable := analyzeArchitectureSource(t, goEmbedAssetArchitectureSource+`func load() string { return "asset" }
`)
	if executable.FileMetric.LDR != 0.2 || executable.SourceKind != "" || !analyzer.LogicDensityApplicable(executable) {
		t.Fatalf("executable result = source kind %q, LDR %v, applicable=%v; want ordinary 0.2 result", executable.SourceKind, executable.FileMetric.LDR, analyzer.LogicDensityApplicable(executable))
	}
	executablePayload := architecturePayload(t, BuildArchitecture(executable))
	if serviceFitnessPayload(t, executablePayload)["logic-density"] != executable.FileMetric.LDR {
		t.Fatalf("executable JSON density = %v, want %v", serviceFitnessPayload(t, executablePayload)["logic-density"], executable.FileMetric.LDR)
	}
	executableMetrics := serviceMetadataPayload(t, executablePayload)["file_metrics"].(map[string]any)
	if _, ok := executableMetrics["source_kind"]; ok {
		t.Fatalf("ordinary JSON emits source marker: %v", executableMetrics)
	}
	validateArchitecturePayload(t, cli, pattern, executablePayload, false)
	ordinary := analyzeArchitectureSource(t, "package configs\nimport \"embed\"\n//go:embed assets/*\nvar FS embed.FS\nfunc load() string { return \"asset\" }\n")
	ordinaryDocument := BuildArchitecture(ordinary)
	validateArchitecturePayload(t, cli, pattern, architecturePayload(t, ordinaryDocument), true)

	t.Run("legacy document missing density", func(t *testing.T) {
		payload := architecturePayload(t, ordinaryDocument)
		delete(serviceFitnessPayload(t, payload), "logic-density")
		delete(serviceMetadataPayload(t, payload), "file_metrics")
		validateArchitecturePayload(t, cli, pattern, payload, false)
	})
	t.Run("missing source marker", func(t *testing.T) {
		payload := architecturePayload(t, ordinaryDocument)
		delete(serviceFitnessPayload(t, payload), "logic-density")
		validateArchitecturePayload(t, cli, pattern, payload, false)
	})
	t.Run("unknown source marker", func(t *testing.T) {
		payload := architecturePayload(t, ordinaryDocument)
		delete(serviceFitnessPayload(t, payload), "logic-density")
		metrics := serviceMetadataPayload(t, payload)["file_metrics"].(map[string]any)
		metrics["source_kind"] = "unknown"
		validateArchitecturePayload(t, cli, pattern, payload, false)
	})
	t.Run("marked low numeric density", func(t *testing.T) {
		payload := architecturePayload(t, pureDocument)
		serviceFitnessPayload(t, payload)["logic-density"] = 0.2
		validateArchitecturePayload(t, cli, pattern, payload, false)
	})

	zeroDocument := BuildArchitecture(analyzer.AnalysisResult{
		CALMNode: "zero",
		Language: "go",
		File:     "zero.go",
		FileMetric: analyzer.FileMetric{
			TotalLOC: 1,
			LDR:      0,
		},
		Imports: analyzer.ImportMetric{Total: 1, Used: 1, DDC: 1},
	})
	zeroFitness := serviceFitnessPayload(t, architecturePayload(t, zeroDocument))
	zeroDensity, ok := zeroFitness["logic-density"]
	if !ok || zeroDensity != float64(0) {
		t.Fatalf("zero logic density JSON = %v, present=%v; want numeric zero", zeroDensity, ok)
	}
}

func analyzeArchitectureSource(t *testing.T, source string) analyzer.AnalysisResult {
	t.Helper()
	path := filepath.Join(t.TempDir(), "embed.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write architecture source: %v", err)
	}
	result, err := analyzer.AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("analyze architecture source: %v", err)
	}
	return result
}

func architecturePayload(t *testing.T, document ArchitectureDocument) map[string]any {
	t.Helper()
	content, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal architecture document: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("unmarshal architecture document: %v", err)
	}
	return payload
}

func nodeMetadataPayload(t *testing.T, payload map[string]any, index int) map[string]any {
	t.Helper()
	nodes := payload["nodes"].([]any)
	return nodes[index].(map[string]any)["metadata"].(map[string]any)
}

func serviceMetadataPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	nodes := payload["nodes"].([]any)
	for _, value := range nodes {
		node := value.(map[string]any)
		if node["node-type"] == "service" {
			return node["metadata"].(map[string]any)
		}
	}
	t.Fatal("service node missing from architecture payload")
	return nil
}

func serviceFitnessPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	return serviceMetadataPayload(t, payload)["fitness"].(map[string]any)
}

func validateArchitecturePayload(t *testing.T, cli, pattern string, payload map[string]any, wantValid bool) {
	t.Helper()
	content, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal validation payload: %v", err)
	}
	architecture := filepath.Join(t.TempDir(), "architecture.json")
	if err := os.WriteFile(architecture, content, 0o644); err != nil {
		t.Fatalf("write validation architecture: %v", err)
	}
	result, err := (calm.Validator{CLIPath: cli}).Validate(context.Background(), architecture, pattern)
	if wantValid {
		if err != nil || !result.Valid {
			t.Fatalf("CALM validation = %+v, error=%v; want valid", result, err)
		}
		return
	}
	if err == nil || result.Valid {
		t.Fatalf("CALM validation = %+v, error=%v; want invalid", result, err)
	}
	if !strings.Contains(result.Output, "logic-density") {
		t.Fatalf("CALM rejected a different constraint: %s", result.Output)
	}
}
