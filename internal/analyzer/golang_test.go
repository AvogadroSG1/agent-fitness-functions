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
	if result.FileMetric.TotalLOC != 27 || result.FileMetric.LogicLOC != 14 || result.FileMetric.LDR != float64(14)/float64(27) {
		t.Fatalf("file metrics = %+v, want total LOC 27, logic LOC 14, LDR 14/27", result.FileMetric)
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

func TestAnalyzeGoFileCountsAliasedImportsOnlyWhenUsedInBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "imports.go")
	source := `package sample

import (
	aliasbytes "bytes"
	aliasfmt "fmt"
)

func UseImport() string {
	return aliasfmt.Sprint("ok")
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}

	if result.Imports.Total != 2 || result.Imports.Used != 1 || result.Imports.DDC != 0.5 {
		t.Fatalf("imports = %+v, want 1/2 aliased imports used", result.Imports)
	}
	if len(result.Imports.Unused) != 1 || result.Imports.Unused[0] != "aliasbytes" {
		t.Fatalf("unused imports = %+v, want aliasbytes", result.Imports.Unused)
	}
}

func TestAnalyzeGoFileDoesNotTreatLocalIdentifierAsImportUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shadowed.go")
	source := `package sample

import (
	aliasbytes "bytes"
	"fmt"
)

func Shadowed(aliasbytes string) string {
	fmtValue := "not a package usage"
	return fmtValue + aliasbytes
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}

	if result.Imports.Total != 2 || result.Imports.Used != 0 || result.Imports.DDC != 0 {
		t.Fatalf("imports = %+v, want 0/2 imports used", result.Imports)
	}
	if len(result.Imports.Unused) != 2 || result.Imports.Unused[0] != "aliasbytes" || result.Imports.Unused[1] != "fmt" {
		t.Fatalf("unused imports = %+v, want aliasbytes and fmt", result.Imports.Unused)
	}
}

func TestAnalyzeGoFileReturnsDisciplinedMetricsForNoImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.go")
	source := `package sample

func Run() string {
	return "ok"
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}

	if result.Imports.Total != 0 || result.Imports.Used != 0 || result.Imports.DDC != 1 {
		t.Fatalf("imports = %+v, want no imports with DDC 1", result.Imports)
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
