package analyzer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
