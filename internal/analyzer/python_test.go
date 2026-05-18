package analyzer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAnalyzePythonFileParsesRadonAndImports(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "example.py")
	source := `import json, unused_module
from pathlib import (
    Path,
    PurePath as PP,
)

def public_choice(value):
    if value:
        return json.dumps({"value": value, "path": str(Path(value))})
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
	if result.FileMetric.TotalLOC != 10 || result.FileMetric.LogicLOC != 4 || result.FileMetric.PublicMethods != 1 {
		t.Fatalf("file metrics = %+v, want LOC 10, logic LOC 4, public methods 1", result.FileMetric)
	}
	if result.Imports.Total != 4 || result.Imports.Used != 2 || len(result.Imports.Unused) != 2 {
		t.Fatalf("imports = %+v, want two unused imports from four imported names", result.Imports)
	}
}

func TestAnalyzePythonRepositoryBatchesRadonCalls(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.py")
	second := filepath.Join(dir, "second.py")
	if err := os.WriteFile(first, []byte("def one():\n    return 1\n"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("def two():\n    return 2\n"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}
	logPath := filepath.Join(dir, "radon.log")
	radon := fakeRepositoryRadon(t, dir, logPath, first, second)

	results, err := AnalyzePythonRepository(context.Background(), dir, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonRepository returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	firstResult := findResult(t, results, first)
	if len(firstResult.Functions) != 1 || firstResult.Functions[0].Name != "one" || firstResult.Functions[0].CyclomaticComplexity != 1 {
		t.Fatalf("first result functions = %+v, want one complexity-1 function", firstResult.Functions)
	}
	if firstResult.FileMetric.TotalLOC != 2 || firstResult.FileMetric.LogicLOC != 1 || firstResult.FileMetric.PublicMethods != 1 {
		t.Fatalf("first result file metrics = %+v, want LOC 2, LLOC 1, public methods 1", firstResult.FileMetric)
	}
	secondResult := findResult(t, results, second)
	if len(secondResult.Functions) != 1 || secondResult.Functions[0].Name != "two" || secondResult.Functions[0].CyclomaticComplexity != 1 {
		t.Fatalf("second result functions = %+v, want two complexity-1 function", secondResult.Functions)
	}
	if secondResult.FileMetric.TotalLOC != 2 || secondResult.FileMetric.LogicLOC != 1 || secondResult.FileMetric.PublicMethods != 1 {
		t.Fatalf("second result file metrics = %+v, want LOC 2, LLOC 1, public methods 1", secondResult.FileMetric)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logContent)
	if strings.Count(logText, " cc ") != 1 || strings.Count(logText, " raw ") != 1 {
		t.Fatalf("radon log = %q, want one cc and one raw invocation", logText)
	}
	if !strings.Contains(logText, first) || !strings.Contains(logText, second) {
		t.Fatalf("radon log = %q, want both files in batched invocations", logText)
	}
}

func TestParseRadonCCPayloadReturnsAnalysisErrors(t *testing.T) {
	_, err := parseRadonCCPayload([]byte(`{"broken.py":{"error":"invalid syntax"}}`))
	if err == nil || !strings.Contains(err.Error(), "radon cc error for broken.py: invalid syntax") {
		t.Fatalf("error = %v, want radon cc analysis error", err)
	}
}

func findResult(t *testing.T, results []AnalysisResult, file string) AnalysisResult {
	t.Helper()
	for _, result := range results {
		if result.File == file {
			return result
		}
	}
	t.Fatalf("result for %s not found in %+v", file, results)
	return AnalysisResult{}
}

func TestAnalyzePythonFileWithRealRadonWhenAvailable(t *testing.T) {
	radon, err := exec.LookPath("radon")
	if err != nil {
		t.Skip("radon not installed")
	}
	file := filepath.Join(t.TempDir(), "known_complexity.py")
	source := `def known(value):
    if value == "a":
        return 1
    if value == "b":
        return 2
    return 0
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := AnalyzePythonFile(context.Background(), file, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonFile returned error: %v", err)
	}
	if len(result.Functions) != 1 || result.Functions[0].CyclomaticComplexity != 3 {
		t.Fatalf("functions = %+v, want known complexity 3", result.Functions)
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
  printf '{"%s":{"loc":10,"lloc":4}}' "$3"
else
  exit 2
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake radon: %v", err)
	}
	return path
}

func fakeRepositoryRadon(t *testing.T, dir, logPath, first, second string) string {
	t.Helper()
	path := filepath.Join(dir, "repo-radon")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf ' %%s %%s\n' "$1" "$*" >> %[1]q
if [ "$1" = "cc" ]; then
  printf '{"%[2]s":[{"type":"function","name":"one","complexity":1,"lineno":1,"endline":2}],"%[3]s":[{"type":"function","name":"two","complexity":1,"lineno":1,"endline":2}]}'
elif [ "$1" = "raw" ]; then
  printf '{"%[2]s":{"loc":2,"lloc":1},"%[3]s":{"loc":2,"lloc":1}}'
else
  exit 2
fi
`, logPath, first, second)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake repo radon: %v", err)
	}
	return path
}
