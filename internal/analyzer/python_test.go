package analyzer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAnalyzePythonFileParsesRadonAndImports(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "example.py")
	source := `import json
import unused_module

def public_choice(value):
    if value:
        return json.dumps({"value": value})
    return "{}"
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	radon := fakeRadon(t, dir)

	result, err := AnalyzePythonFile(context.Background(), file, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonFile returned error: %v", err)
	}

	if result.Language != "python" {
		t.Fatalf("language = %q, want python", result.Language)
	}
	if len(result.Functions) != 1 || result.Functions[0].CyclomaticComplexity != 2 {
		t.Fatalf("functions = %+v, want one complexity-2 function", result.Functions)
	}
	if result.FileMetric.TotalLOC != 7 || result.FileMetric.LogicLOC != 4 {
		t.Fatalf("file metrics = %+v, want LOC 7 and logic LOC 4", result.FileMetric)
	}
	if result.Imports.Total != 2 || result.Imports.Used != 1 || len(result.Imports.Unused) != 1 {
		t.Fatalf("imports = %+v, want one unused import", result.Imports)
	}
}

func fakeRadon(t *testing.T, dir string) string {
	t.Helper()
	name := "radon"
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	script := `#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "cc" ]; then
  printf '{"%s":[{"type":"F","name":"public_choice","complexity":2,"lineno":4,"endline":7}]}' "$3"
elif [ "$1" = "raw" ]; then
  printf '{"%s":{"loc":7,"lloc":4}}' "$3"
else
  exit 2
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake radon: %v", err)
	}
	return path
}
