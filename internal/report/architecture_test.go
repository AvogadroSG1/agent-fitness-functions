package report

import (
	"encoding/json"
	"testing"

	"github.com/poconnor/calm-poc/internal/analyzer"
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
	if fitness.LogicDensity != 0.5 {
		t.Fatalf("logic density = %v, want LDR", fitness.LogicDensity)
	}
	if fitness.DependencyDiscipline != 0.5 {
		t.Fatalf("dependency discipline = %v, want DDC", fitness.DependencyDiscipline)
	}
	if node.Metadata.ModuleMetrics == nil {
		t.Fatal("module_metrics missing from analyzed node metadata")
	}
	moduleMetrics := *node.Metadata.ModuleMetrics
	if moduleMetrics.PublicMethodCount != 1 || moduleMetrics.TotalLOC != 80 || moduleMetrics.PrivateLOC != 56 || moduleMetrics.AverageLOCPublicMethod != 40 {
		t.Fatalf("module metrics = %+v, want public=1 total=80 private=56 avg=40", moduleMetrics)
	}
	if node.Metadata.FileMetrics == nil || node.Metadata.FileMetrics.TotalLines != 80 || node.Metadata.FileMetrics.LogicLines != 40 {
		t.Fatalf("file metrics = %+v, want total=80 logic=40", node.Metadata.FileMetrics)
	}
	if node.Metadata.ImportMetrics == nil || node.Metadata.ImportMetrics.TotalImports != 2 || node.Metadata.ImportMetrics.UsedImports != 1 {
		t.Fatalf("import metrics = %+v, want total=2 used=1", node.Metadata.ImportMetrics)
	}
	if _, err := json.Marshal(document); err != nil {
		t.Fatalf("marshal architecture document: %v", err)
	}
}
