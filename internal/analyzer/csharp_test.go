package analyzer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAnalyzeCSharpFileParsesRoslynCLIOutput(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	if err := os.WriteFile(sourcePath, []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	cli := fakeRoslyn(t, dir)

	result, err := AnalyzeCSharpFile(context.Background(), sourcePath, cli)
	if err != nil {
		t.Fatalf("AnalyzeCSharpFile returned error: %v", err)
	}

	if result.Language != "csharp" || result.CALMNode != "Example" {
		t.Fatalf("result identity = %+v, want csharp Example", result)
	}
	if len(result.Functions) != 1 || result.Functions[0].Name != "Run" || result.Functions[0].CyclomaticComplexity != 4 {
		t.Fatalf("functions = %+v, want Run complexity 4", result.Functions)
	}
}

func TestAnalyzeCSharpFileWithRealRoslynAnalyzer(t *testing.T) {
	cli := buildRoslynAnalyzer(t)
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	source := `using System;
using System.Linq;
using System.Text;

namespace Sample.App;

public interface IRunner
{
    void InterfaceOnly();
}

public class Example
{
    private readonly int _count;

    public Example(int count)
    {
        _count = count;
    }

    public int Count
    {
        get
        {
            if (_count > 0)
            {
                return _count;
            }
            return 0;
        }
    }

    public void Execute()
    {
        var builder = new StringBuilder();
        Console.WriteLine(builder.ToString());
    }

    public int Run(bool enabled, bool forced)
    {
        int LocalScore(int score)
        {
            if (score > 0)
            {
                return score;
            }
            return 0;
        }

        if (enabled && forced)
        {
            return LocalScore(Count);
        }
        return 0;
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	result, err := AnalyzeCSharpFile(context.Background(), sourcePath, cli)
	if err != nil {
		t.Fatalf("AnalyzeCSharpFile returned error: %v", err)
	}

	if result.Language != "csharp" || result.CALMNode != "Sample.App" || result.File != sourcePath {
		t.Fatalf("result identity = %+v, want csharp Sample.App", result)
	}
	assertFunction(t, result.Functions, "Run", 3, true)
	assertFunction(t, result.Functions, "Example", 1, true)
	assertFunction(t, result.Functions, "Count.get", 2, true)
	assertFunction(t, result.Functions, "Execute", 1, true)
	assertFunction(t, result.Functions, "InterfaceOnly", 1, true)
	assertFunction(t, result.Functions, "LocalScore", 2, false)
	if result.FileMetric.PublicMethods != 5 {
		t.Fatalf("public methods = %d, want constructor, property accessor, interface member, and two public methods", result.FileMetric.PublicMethods)
	}
	if result.FileMetric.TotalLOC == 0 || result.FileMetric.LogicLOC == 0 || result.FileMetric.LDR == 0 {
		t.Fatalf("file metrics = %+v, want non-zero LOC and LDR", result.FileMetric)
	}
	if result.Imports.Total != 3 || result.Imports.Used != 2 || result.Imports.DDC != float64(2)/float64(3) {
		t.Fatalf("imports = %+v, want two used imports from three using directives", result.Imports)
	}
}

func TestAnalyzeCSharpFileReturnsRoslynFailures(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	if err := os.WriteFile(sourcePath, []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	cli := fakeFailingRoslyn(t, dir)

	_, err := AnalyzeCSharpFile(context.Background(), sourcePath, cli)
	if err == nil || !strings.Contains(err.Error(), "running Roslyn analyzer") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want Roslyn failure with stderr", err)
	}
}

func TestAnalyzeCSharpFileReturnsInvalidJSONErrors(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	if err := os.WriteFile(sourcePath, []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	cli := fakeInvalidJSONRoslyn(t, dir)

	_, err := AnalyzeCSharpFile(context.Background(), sourcePath, cli)
	if err == nil || !strings.Contains(err.Error(), "parsing Roslyn analyzer output") {
		t.Fatalf("error = %v, want JSON parse error", err)
	}
}

func TestAnalyzeCSharpFileIgnoresSuccessfulStderrDiagnostics(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	if err := os.WriteFile(sourcePath, []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	cli := fakeStderrRoslyn(t, dir)

	result, err := AnalyzeCSharpFile(context.Background(), sourcePath, cli)
	if err != nil {
		t.Fatalf("AnalyzeCSharpFile returned error: %v", err)
	}
	if result.Language != "csharp" || result.CALMNode != "Example" {
		t.Fatalf("result = %+v, want valid JSON parsed despite stderr", result)
	}
}

func TestAnalyzeCSharpFileIncludesStderrOnInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	if err := os.WriteFile(sourcePath, []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	cli := fakeInvalidJSONWithStderrRoslyn(t, dir)

	_, err := AnalyzeCSharpFile(context.Background(), sourcePath, cli)
	if err == nil || !strings.Contains(err.Error(), "parsing Roslyn analyzer output") || !strings.Contains(err.Error(), "host warning") {
		t.Fatalf("error = %v, want JSON parse error with stderr context", err)
	}
}

func TestAnalyzeCSharpFileReturnsMissingExecutableErrors(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Example.cs")
	if err := os.WriteFile(sourcePath, []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	_, err := AnalyzeCSharpFile(context.Background(), sourcePath, filepath.Join(dir, "missing-roslyn"))
	if err == nil || !strings.Contains(err.Error(), "running Roslyn analyzer") {
		t.Fatalf("error = %v, want missing executable error", err)
	}
}

func assertFunction(t *testing.T, functions []FunctionMetric, name string, complexity int, isPublic bool) {
	t.Helper()
	for _, fn := range functions {
		if fn.Name == name {
			if fn.CyclomaticComplexity != complexity || fn.IsPublic != isPublic || fn.LOC == 0 {
				t.Fatalf("%s = %+v, want complexity %d public %v with LOC", name, fn, complexity, isPublic)
			}
			return
		}
	}
	t.Fatalf("function %q not found in %+v", name, functions)
}

func buildRoslynAnalyzer(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	project := filepath.Join(repoRoot, "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj")
	outputDir := filepath.Join(t.TempDir(), "roslyn-out")
	objDir := filepath.Join(t.TempDir(), "roslyn-obj")
	command := exec.Command(
		"dotnet",
		"build",
		project,
		"--output",
		outputDir,
		"-p:BaseIntermediateOutputPath="+objDir+string(os.PathSeparator),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("dotnet build failed: %v\n%s", err, output)
	}
	exe := filepath.Join(outputDir, "CalmRoslynAnalyzer")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	return exe
}

func fakeRoslyn(t *testing.T, dir string) string {
	t.Helper()
	name := "roslyn"
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	script := `#!/usr/bin/env bash
set -euo pipefail
cat <<JSON
{
  "calm_node": "Example",
  "language": "csharp",
  "file": "$1",
  "functions": [{"name":"Run","cyclomatic_complexity":4,"is_public":true,"loc":12}],
  "file_metrics": {"total_loc": 20, "logic_loc": 12, "public_methods": 1, "ldr": 0.6},
  "import_metrics": {"total": 2, "used": 2, "ddc": 1}
}
JSON
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake roslyn: %v", err)
	}
	return path
}

func fakeFailingRoslyn(t *testing.T, dir string) string {
	t.Helper()
	return writeFakeRoslyn(t, dir, "failing-roslyn", `#!/usr/bin/env bash
echo boom >&2
exit 3
`)
}

func fakeInvalidJSONRoslyn(t *testing.T, dir string) string {
	t.Helper()
	return writeFakeRoslyn(t, dir, "invalid-json-roslyn", `#!/usr/bin/env bash
echo not-json
`)
}

func fakeStderrRoslyn(t *testing.T, dir string) string {
	t.Helper()
	return writeFakeRoslyn(t, dir, "stderr-roslyn", `#!/usr/bin/env bash
echo "host warning" >&2
cat <<JSON
{
  "calm_node": "Example",
  "language": "csharp",
  "file": "$1",
  "functions": [],
  "file_metrics": {"total_loc": 1, "logic_loc": 1, "public_methods": 0, "ldr": 1},
  "import_metrics": {"total": 0, "used": 0, "ddc": 1}
}
JSON
`)
}

func fakeInvalidJSONWithStderrRoslyn(t *testing.T, dir string) string {
	t.Helper()
	return writeFakeRoslyn(t, dir, "invalid-json-stderr-roslyn", `#!/usr/bin/env bash
echo "host warning" >&2
echo not-json
`)
}

func writeFakeRoslyn(t *testing.T, dir, name, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake roslyn: %v", err)
	}
	return path
}
