package fixtures

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
)

type fixtureCase struct {
	name     string
	path     string
	language string
	rule     string
	red      bool
}

func TestViolationAndGreenFixturesAreCalibrated(t *testing.T) {
	cases := []fixtureCase{
		{name: "go cyclomatic red", path: "violations/go/cyclomatic-complexity.go", language: "go", rule: "cyclomatic-complexity", red: true},
		{name: "go cyclomatic green", path: "green/go/cyclomatic-complexity.go", language: "go", rule: "cyclomatic-complexity"},
		{name: "go interface red", path: "violations/go/interface-width.go", language: "go", rule: "interface-width", red: true},
		{name: "go interface green", path: "green/go/interface-width.go", language: "go", rule: "interface-width"},
		{name: "go ldr red", path: "violations/go/logic-density.go", language: "go", rule: "logic-density", red: true},
		{name: "go ldr green", path: "green/go/logic-density.go", language: "go", rule: "logic-density"},
		{name: "go ddc red", path: "violations/go/dependency-discipline.go", language: "go", rule: "dependency-discipline", red: true},
		{name: "go ddc green", path: "green/go/dependency-discipline.go", language: "go", rule: "dependency-discipline"},
		{name: "python cyclomatic red", path: "violations/python/cyclomatic_complexity.py", language: "python", rule: "cyclomatic-complexity", red: true},
		{name: "python cyclomatic green", path: "green/python/cyclomatic_complexity.py", language: "python", rule: "cyclomatic-complexity"},
		{name: "python interface red", path: "violations/python/interface_width.py", language: "python", rule: "interface-width", red: true},
		{name: "python interface green", path: "green/python/interface_width.py", language: "python", rule: "interface-width"},
		{name: "python ldr red", path: "violations/python/logic_density.py", language: "python", rule: "logic-density", red: true},
		{name: "python ldr green", path: "green/python/logic_density.py", language: "python", rule: "logic-density"},
		{name: "python ddc red", path: "violations/python/dependency_discipline.py", language: "python", rule: "dependency-discipline", red: true},
		{name: "python ddc green", path: "green/python/dependency_discipline.py", language: "python", rule: "dependency-discipline"},
		{name: "csharp cyclomatic red", path: "violations/csharp/CyclomaticComplexity.cs", language: "csharp", rule: "cyclomatic-complexity", red: true},
		{name: "csharp cyclomatic green", path: "green/csharp/CyclomaticComplexity.cs", language: "csharp", rule: "cyclomatic-complexity"},
		{name: "csharp interface red", path: "violations/csharp/InterfaceWidth.cs", language: "csharp", rule: "interface-width", red: true},
		{name: "csharp interface green", path: "green/csharp/InterfaceWidth.cs", language: "csharp", rule: "interface-width"},
		{name: "csharp implementation-depth red", path: "violations/csharp/ImplementationDepth.cs", language: "csharp", rule: "implementation-depth", red: true},
		{name: "csharp implementation-depth green", path: "green/csharp/ImplementationDepth.cs", language: "csharp", rule: "implementation-depth"},
		{name: "csharp ldr red", path: "violations/csharp/LogicDensity.cs", language: "csharp", rule: "logic-density", red: true},
		{name: "csharp ldr green", path: "green/csharp/LogicDensity.cs", language: "csharp", rule: "logic-density"},
		{name: "csharp ddc red", path: "violations/csharp/DependencyDiscipline.cs", language: "csharp", rule: "dependency-discipline", red: true},
		{name: "csharp ddc green", path: "green/csharp/DependencyDiscipline.cs", language: "csharp", rule: "dependency-discipline"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := analyzeFixture(t, tc.language, tc.path)
			violates := violatesRule(result, tc.rule)
			if violates != tc.red {
				t.Fatalf("%s violation = %v, want %v; metrics = %+v %+v %+v", tc.rule, violates, tc.red, result.FileMetric, result.ModuleMetric, result.Imports)
			}
			if tc.rule == "dependency-discipline" && tc.red && len(result.Imports.Unused) == 0 {
				t.Fatalf("DDC red fixture has no unused import list: %+v", result.Imports)
			}
		})
	}
}

func analyzeFixture(t *testing.T, language, relativePath string) analyzer.AnalysisResult {
	t.Helper()
	path := filepath.Join(relativePath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch language {
	case "go":
		result, err := analyzer.AnalyzeGoFile(path)
		if err != nil {
			t.Fatalf("analyze Go fixture %s: %v", path, err)
		}
		return result
	case "python":
		if _, err := exec.LookPath("radon"); err != nil {
			t.Skipf("radon is not installed: %v", err)
		}
		result, err := analyzer.AnalyzePythonFile(ctx, path, "")
		if err != nil {
			t.Fatalf("analyze Python fixture %s: %v", path, err)
		}
		return result
	case "csharp":
		roslyn := ensureRoslynAnalyzer(t)
		result, err := analyzer.AnalyzeCSharpFile(ctx, path, roslyn)
		if err != nil {
			t.Fatalf("analyze C# fixture %s: %v", path, err)
		}
		return result
	default:
		t.Fatalf("unknown language %s", language)
		return analyzer.AnalysisResult{}
	}
}

func ensureRoslynAnalyzer(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer")
	if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
		return path
	}
	command := exec.Command("dotnet", "build", "-c", "Release", filepath.Join("..", "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build Roslyn analyzer: %v\n%s", err, output)
	}
	return path
}

func violatesRule(result analyzer.AnalysisResult, rule string) bool {
	switch rule {
	case "cyclomatic-complexity":
		for _, function := range result.Functions {
			if function.CyclomaticComplexity > 9 {
				return true
			}
		}
		return false
	case "interface-width":
		return analyzer.EnsureModuleMetric(result).ModuleMetric.PublicMethods > 20
	case "implementation-depth":
		module := analyzer.EnsureModuleMetric(result).ModuleMetric
		return module.PublicMethods > 0 && module.AverageLOCPerPublicMethod < 0.722
	case "logic-density":
		return result.FileMetric.TotalLOC > 0 && result.FileMetric.LDR < 0.255
	case "dependency-discipline":
		return result.Imports.Total > 0 && result.Imports.DDC < 0.8
	default:
		return false
	}
}
