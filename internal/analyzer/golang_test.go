package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeGoFileReturnsNormalizedMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "example.go")
	source := `package sample

import (
	"fmt"
	"strings"
)

type sample struct{}

func ExportedPackageFunction(value string) string {
	if value == "" {
		return "empty"
	}
	return value
}

func (s sample) PublicChoice(value string) string {
	if value == "" {
		return fmt.Sprint("empty")
	}
	if strings.TrimSpace(value) == "" {
		return "blank"
	}
	return value
}

func (s sample) privateReceiver() string {
	return fmt.Sprint("hidden")
}

func privatePassThrough() string {
	return fmt.Sprint("ok")
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}

	if result.CALMNode != "sample" {
		t.Fatalf("CALMNode = %q, want sample", result.CALMNode)
	}
	if len(result.Functions) != 4 {
		t.Fatalf("functions = %d, want 4", len(result.Functions))
	}
	publicChoice := findFunction(t, result, "PublicChoice")
	if publicChoice.CyclomaticComplexity != 3 || !publicChoice.IsPublic || publicChoice.LOC != 9 {
		t.Fatalf("PublicChoice = %+v, want public complexity 3 with LOC 9", publicChoice)
	}
	exportedFunction := findFunction(t, result, "ExportedPackageFunction")
	if exportedFunction.CyclomaticComplexity != 2 || !exportedFunction.IsPublic || exportedFunction.LOC != 6 {
		t.Fatalf("ExportedPackageFunction = %+v, want public complexity 2 with LOC 6", exportedFunction)
	}
	privateReceiver := findFunction(t, result, "privateReceiver")
	if privateReceiver.CyclomaticComplexity != 1 || privateReceiver.IsPublic || privateReceiver.LOC != 3 {
		t.Fatalf("privateReceiver = %+v, want private complexity 1 with LOC 3", privateReceiver)
	}
	privatePassThrough := findFunction(t, result, "privatePassThrough")
	if privatePassThrough.CyclomaticComplexity != 1 || privatePassThrough.IsPublic || privatePassThrough.LOC != 3 {
		t.Fatalf("privatePassThrough = %+v, want private complexity 1 with LOC 3", privatePassThrough)
	}
	if result.FileMetric.PublicMethods != 2 {
		t.Fatalf("public methods = %d, want 2", result.FileMetric.PublicMethods)
	}
	if result.FileMetric.TotalLOC != 27 || result.FileMetric.LogicLOC != 16 || result.FileMetric.LDR != float64(16)/float64(27) {
		t.Fatalf("file metrics = %+v, want total LOC 27, logic LOC 16, LDR 16/27", result.FileMetric)
	}
	if result.Imports.Total != 2 || result.Imports.Used != 2 {
		t.Fatalf("imports = %+v, want 2/2 used", result.Imports)
	}
}

func TestAnalyzeGoFileSkipsGocycloIgnoredFunctions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.go")
	source := `package sample

//gocyclo:ignore
func Ignored(value string) string {
	if value == "" {
		return "empty"
	}
	return value
}

func Kept(value string) string {
	return value
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}
	if len(result.Functions) != 1 {
		t.Fatalf("functions = %+v, want only non-ignored function", result.Functions)
	}
	if kept := findFunction(t, result, "Kept"); kept.CyclomaticComplexity != 1 {
		t.Fatalf("Kept = %+v, want complexity 1", kept)
	}
}

func findFunction(t *testing.T, result AnalysisResult, name string) FunctionMetric {
	t.Helper()
	for _, fn := range result.Functions {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found in %+v", name, result.Functions)
	return FunctionMetric{}
}
