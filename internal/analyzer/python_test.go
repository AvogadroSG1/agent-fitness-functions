package analyzer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestAnalyzePythonFile_Python312TypeAliasSyntax(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourceFile := filepath.Join(dir, "google_source.py")
	sourceContent := `"""Google source module."""

from typing import Any
import os

type FlowCredentials = dict[str, Any]
type GoogleCredentials = FlowCredentials | None

class GoogleConnector:
    def __init__(self, credentials: GoogleCredentials) -> None:
        self.credentials = credentials

    def connect(self) -> bool:
        return self.credentials is not None
`
	if err := os.WriteFile(sourceFile, []byte(sourceContent), 0o644); err != nil {
		t.Fatalf("failed to write test source: %v", err)
	}

	result, err := AnalyzePythonFile(ctx, sourceFile, "")
	if err != nil {
		t.Fatalf("AnalyzePythonFile failed on Python 3.12 syntax: %v", err)
	}
	if result.FileMetric.TotalLOC == 0 {
		t.Errorf("expected non-zero TotalLOC, got %d", result.FileMetric.TotalLOC)
	}
	if len(result.Functions) == 0 {
		t.Errorf("expected at least 1 function, got %d", len(result.Functions))
	}
}

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
	if result.ModuleMetric.PublicMethods != 1 || result.ModuleMetric.TotalLOC != 10 || result.ModuleMetric.AverageLOCPerPublicMethod != 4 {
		t.Fatalf("module metrics = %+v, want public methods 1, total LOC 10, avg LOC/public 4", result.ModuleMetric)
	}
	if result.Imports.Total != 4 || result.Imports.Used != 2 || len(result.Imports.Unused) != 2 {
		t.Fatalf("imports = %+v, want two unused imports from four imported names", result.Imports)
	}
}

func TestPythonImportMetricCountsSingleLineParenthesizedImportUsage(t *testing.T) {
	source := `from pathlib import (Path, PurePath)

def public_choice(value):
    return str(Path(value))
`
	imports := pythonImportScan(t, source)
	if imports.Total != 2 || imports.Used != 1 || len(imports.Unused) != 1 || imports.Unused[0] != "PurePath" {
		t.Fatalf("imports = %+v, want Path used and PurePath unused", imports)
	}
}

func TestPythonImportMetricIgnoresCommentsStringsAndLocalBindings(t *testing.T) {
	source := `import json
import unused_module
from pathlib import Path

# unused_module appears here but is not used.
def public_choice(unused_module):
    text = "json and Path are only words in this string"
    json = {"shadowed": unused_module}
    return text
`
	imports := pythonImportScan(t, source)
	if imports.Total != 3 || imports.Used != 0 || imports.DDC != 0 {
		t.Fatalf("imports = %+v, want no imports used", imports)
	}
	if len(imports.Unused) != 3 || imports.Unused[0] != "json" || imports.Unused[1] != "unused_module" || imports.Unused[2] != "Path" {
		t.Fatalf("unused imports = %+v, want json, unused_module, and Path", imports.Unused)
	}
}

func TestPythonImportMetricKeepsBindingScopeLocal(t *testing.T) {
	source := `import json

def parse(json):
    return json

def dump(value):
    return json.dumps(value)
`
	imports := pythonImportScan(t, source)
	if imports.Total != 1 || imports.Used != 1 || len(imports.Unused) != 0 {
		t.Fatalf("imports = %+v, want json used outside shadowing function scope", imports)
	}
}

func TestPythonImportMetricTreatsComprehensionTargetsAsLocal(t *testing.T) {
	source := `import unused_module

def build(items):
    return [unused_module for unused_module in items]
`
	imports := pythonImportScan(t, source)
	if imports.Total != 1 || imports.Used != 0 || len(imports.Unused) != 1 || imports.Unused[0] != "unused_module" {
		t.Fatalf("imports = %+v, want comprehension target to shadow unused import", imports)
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

func TestAnalyzePythonFileUsesSingleRadonAPISubprocessWhenRadonHasPythonShebang(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "fast.py")
	if err := os.WriteFile(file, []byte("def one():\n    return 1\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	logPath := filepath.Join(dir, "radon-python.log")
	fakePython := fakeRadonPython(t, dir, logPath)
	fakeRadonWithShebang(t, dir, fakePython)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A managed runtime installed on this machine (state root resolved via
	// XDG_STATE_HOME, per ADR-0005) MUST NOT shadow the PATH shim under test;
	// pin an empty state root so managedRadonPath finds nothing.
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	result, err := AnalyzePythonFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("AnalyzePythonFile returned error: %v", err)
	}

	gotOneFunction := len(result.Functions) == 1
	gotExpectedFunction := gotOneFunction &&
		result.Functions[0].Name == "one" &&
		result.Functions[0].CyclomaticComplexity == 1
	if !gotExpectedFunction {
		t.Fatalf("functions = %+v, want one complexity-1 function", result.Functions)
	}
	if result.FileMetric.TotalLOC != 2 || result.FileMetric.LogicLOC != 1 {
		t.Fatalf("file metrics = %+v, want LOC 2 and LLOC 1", result.FileMetric)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Count(string(logContent), "invoke\n") != 1 {
		t.Fatalf("radon python log = %q, want one subprocess invocation", string(logContent))
	}
}

func TestAnalyzePythonFileFallsBackToRadonCLIWhenFastPathSubprocessFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "fallback_error.py")
	if err := os.WriteFile(file, []byte("def fallback():\n    return 1\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	logPath := filepath.Join(dir, "radon-degraded.log")
	fakeShellRadonWithCLIFallback(t, dir, logPath, file)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A managed runtime installed on this machine (state root resolved via
	// XDG_STATE_HOME, per ADR-0005) MUST NOT shadow the PATH shim under test;
	// pin an empty state root so managedRadonPath finds nothing.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	python, _, ok := radonPythonCommand("radon")
	if !ok || !strings.HasSuffix(python, "bash") {
		t.Fatalf("radonPythonCommand = %q, %t; want bash fast-path command", python, ok)
	}

	result, err := AnalyzePythonFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("AnalyzePythonFile returned error: %v", err)
	}

	gotOneFunction := len(result.Functions) == 1
	gotFallbackFunction := gotOneFunction &&
		result.Functions[0].Name == "fallback" &&
		result.Functions[0].CyclomaticComplexity == 1
	if !gotFallbackFunction {
		t.Fatalf("functions = %+v, want fallback function from radon CLI output", result.Functions)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logContent)
	if strings.Count(logText, " cc ") != 1 ||
		strings.Count(logText, " raw ") != 1 {
		t.Fatalf("radon degraded log = %q, want cc/raw fallback", logText)
	}
}

func TestAnalyzePythonFileFallsBackToRadonCLIWhenFastPathUnavailable(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "fallback.py")
	if err := os.WriteFile(file, []byte("def fallback():\n    return 1\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	logPath := filepath.Join(dir, "radon-cli.log")
	fakeRadonCLI(t, dir, logPath, file)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A managed runtime installed on this machine (state root resolved via
	// XDG_STATE_HOME, per ADR-0005) MUST NOT shadow the PATH shim under test;
	// pin an empty state root so managedRadonPath finds nothing.
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	result, err := AnalyzePythonFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("AnalyzePythonFile returned error: %v", err)
	}

	gotOneFunction := len(result.Functions) == 1
	gotFallbackFunction := gotOneFunction &&
		result.Functions[0].Name == "fallback" &&
		result.Functions[0].CyclomaticComplexity == 1
	if !gotFallbackFunction {
		t.Fatalf("functions = %+v, want fallback function from radon CLI output", result.Functions)
	}
	if result.FileMetric.TotalLOC != 2 || result.FileMetric.LogicLOC != 1 {
		t.Fatalf("file metrics = %+v, want LOC 2 and LLOC 1 from radon CLI output", result.FileMetric)
	}
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logContent)
	if strings.Count(logText, " cc ") != 1 || strings.Count(logText, " raw ") != 1 {
		t.Fatalf("radon CLI log = %q, want one cc and one raw fallback invocation", logText)
	}
}

func TestRadonPythonCommandParsesEnvShebang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "radon")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env python3 -I\n"), 0o755); err != nil {
		t.Fatalf("write fake radon: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	python, args, ok := radonPythonCommand("radon")
	if !ok {
		t.Fatalf("radonPythonCommand did not parse shebang")
	}
	gotExpectedPython := python == "python3"
	gotExpectedArgs := len(args) == 1 && args[0] == "-I"
	if !gotExpectedPython || !gotExpectedArgs {
		t.Fatalf("command = %q %v, want python3 [-I]", python, args)
	}
}

func TestParseRadonCCPayloadReturnsAnalysisErrors(t *testing.T) {
	_, err := parseRadonCCPayload([]byte(`{"broken.py":{"error":"invalid syntax"}}`))
	if err == nil || !strings.Contains(err.Error(), "radon cc error for broken.py: invalid syntax") {
		t.Fatalf("error = %v, want radon cc analysis error", err)
	}
}

func TestParseRadonRawReturnsAnalysisErrors(t *testing.T) {
	_, err := parseRadonRaw("broken.py", []byte(`{"broken.py":{"error":"invalid syntax"}}`))
	if err == nil || !strings.Contains(err.Error(), "radon raw error for broken.py: invalid syntax") {
		t.Fatalf("error = %v, want radon raw analysis error", err)
	}
}

func TestParseRadonRawIncludesMalformedOutputContext(t *testing.T) {
	_, err := parseRadonRaw("broken.py", []byte(`not-json`))
	if err == nil || !strings.Contains(err.Error(), "parsing radon raw") || !strings.Contains(err.Error(), "not-json") {
		t.Fatalf("error = %v, want malformed radon raw output context", err)
	}
}

func TestAnalyzePythonRepositoryReturnsRadonRawErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "broken.py")
	if err := os.WriteFile(file, []byte("def broken(:\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	radon := fakeRawErrorRadon(t, dir, file)

	_, err := AnalyzePythonRepository(context.Background(), dir, radon)
	if err == nil || !strings.Contains(err.Error(), "radon raw error for "+file+": invalid syntax") {
		t.Fatalf("error = %v, want repository radon raw analysis error", err)
	}
}

func TestRunToolPreservesCancellationSemantics(t *testing.T) {
	dir := t.TempDir()
	tool := fakeSleepTool(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runTool(ctx, tool)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
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

func TestAnalyzePythonFileWithRealRadonAPIFastPathWhenAvailable(t *testing.T) {
	if _, err := exec.LookPath("radon"); err != nil {
		t.Skip("radon not installed")
	}
	file := filepath.Join(t.TempDir(), "known_api_complexity.py")
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
	result, err := analyzePythonFileWithRadonAPI(context.Background(), file, "radon")
	if err != nil {
		t.Fatalf("analyzePythonFileWithRadonAPI returned error: %v", err)
	}
	if len(result.Functions) != 1 || result.Functions[0].CyclomaticComplexity != 3 {
		t.Fatalf("functions = %+v, want known complexity 3", result.Functions)
	}
	if result.FileMetric.TotalLOC != 6 || result.FileMetric.LogicLOC != 6 {
		t.Fatalf("file metrics = %+v, want LOC 6 and LLOC 6", result.FileMetric)
	}
}

func TestAnalyzePythonFileWithRadonAPI_IncompatibleSyntaxFailsAnalysis(t *testing.T) {
	if _, err := exec.LookPath("radon"); err != nil {
		t.Skip("radon not installed")
	}
	file := filepath.Join(t.TempDir(), "incompatible_syntax.py")
	source := `def broken_syntax(
    return "unclosed paren"
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := analyzePythonFileWithRadonAPI(context.Background(), file, "radon")
	if err == nil || !strings.Contains(err.Error(), "python findings error") {
		t.Fatalf("result = %+v, error = %v; want python findings error", result, err)
	}
	if result.FileMetric != (FileMetric{}) || result.Imports.Total != 0 || result.Imports.Used != 0 || len(result.Imports.Unused) != 0 {
		t.Fatalf("result = %+v, want no successful analysis", result)
	}
}

func TestPythonInterfaceMetricsPaths(t *testing.T) {
	radon, err := exec.LookPath("radon")
	if err != nil {
		t.Skip("radon not installed")
	}
	ctx := context.Background()
	root := t.TempDir()
	semanticFile := filepath.Join(root, "semantic.py")
	semanticSource := `from typing import overload

def module_sync():
    return 1

async def module_async():
    return 2

def _module_private():
    return 3

class _Outer:
    def __init__(self):
        self.value = 0

    @property
    def value(self):
        return self._value

    @value.setter
    def value(self, value):
        self._value = value

    @overload
    def overloaded(self, value: int):
        ...

    @overload
    def overloaded(self, value: str):
        ...

    def overloaded(self, value):
        return value

    @staticmethod
    def static_op():
        return 1

    @classmethod
    def class_op(cls):
        return cls

    def _private(self):
        return None

    class Inner:
        def nested_op(self):
            return 1

    class _Inner:
        def nested_private(self):
            return 1
    def factory(self):
        def closure():
            return 1

        class _Worker:
            def work(self):
                return 1

        return _Worker()

class _Private:
    def public(self):
        return 1

def make_local():
    class Local:
        def local(self):
            return 1
    return Local
`
	if err := os.WriteFile(semanticFile, []byte(semanticSource), 0o644); err != nil {
		t.Fatalf("write semantic source: %v", err)
	}

	apiResult, err := analyzePythonFileWithRadonAPI(ctx, semanticFile, radon)
	if err != nil {
		t.Fatalf("analyzePythonFileWithRadonAPI returned error: %v", err)
	}
	cliResult, err := AnalyzePythonFile(ctx, semanticFile, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonFile returned error: %v", err)
	}
	assertSameInterfaceMetrics(t, apiResult.Interfaces, cliResult.Interfaces)

	wantInterfaces := []InterfaceMetric{
		{Kind: "module", Line: 1, PublicMethods: 3},
		{Name: "_Outer", Kind: "class", Line: 12, PublicMethods: 5},
		{Name: "_Outer.Inner", Kind: "class", Line: 46, PublicMethods: 1},
		{Name: "_Outer._Inner", Kind: "class", Line: 50, PublicMethods: 1},
		{Name: "_Outer.factory.<locals>._Worker", Kind: "class", Line: 57, PublicMethods: 1},
		{Name: "_Private", Kind: "class", Line: 63, PublicMethods: 1},
		{Name: "make_local.<locals>.Local", Kind: "class", Line: 68, PublicMethods: 1},
	}
	if len(cliResult.Interfaces) != len(wantInterfaces) {
		t.Fatalf("interfaces = %+v, want %+v", cliResult.Interfaces, wantInterfaces)
	}
	for i, want := range wantInterfaces {
		if cliResult.Interfaces[i] != want {
			t.Errorf("interface %d = %+v, want %+v", i, cliResult.Interfaces[i], want)
		}
	}

	emptyFile := filepath.Join(root, "empty.py")
	if err := os.WriteFile(emptyFile, nil, 0o644); err != nil {
		t.Fatalf("write empty source: %v", err)
	}
	emptyResult, err := AnalyzePythonFile(ctx, emptyFile, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonFile(empty) returned error: %v", err)
	}
	if len(emptyResult.Interfaces) != 1 ||
		emptyResult.Interfaces[0].Kind != "module" ||
		emptyResult.Interfaces[0].Name != "" ||
		emptyResult.Interfaces[0].Line != 1 ||
		emptyResult.Interfaces[0].PublicMethods != 0 {
		t.Fatalf("empty interfaces = %+v, want one zero-width module record", emptyResult.Interfaces)
	}

	repeatedFile := filepath.Join(root, "repeated.py")
	repeatedSource := `class Repeat:
    def first(self):
        return 1

class Repeat:
    def second(self):
        return 2
`
	if err := os.WriteFile(repeatedFile, []byte(repeatedSource), 0o644); err != nil {
		t.Fatalf("write repeated source: %v", err)
	}
	repeatedResult, err := AnalyzePythonFile(ctx, repeatedFile, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonFile(repeated) returned error: %v", err)
	}
	repeated := interfaceMetricsNamed(repeatedResult.Interfaces, "Repeat")
	if len(repeated) != 2 || repeated[0].Line != 1 || repeated[1].Line != 5 ||
		repeated[0].PublicMethods != 1 || repeated[1].PublicMethods != 1 {
		t.Fatalf("repeated class interfaces = %+v, want two distinct one-method declarations", repeated)
	}

	conditionalFile := filepath.Join(root, "conditional.py")
	conditionalSource := `if enabled:
    def module_op():
        return 1
else:
    async def module_op():
        return 2

class Base:
    def inherited(self):
        return 1

class Derived(Base):
    callback = lambda: None
    if enabled:
        def own(self):
            def closure():
                return 1
            return closure()
    else:
        async def own(self):
            return 2

Derived.assigned = module_op
`
	if err := os.WriteFile(conditionalFile, []byte(conditionalSource), 0o644); err != nil {
		t.Fatal(err)
	}
	conditionalResult, err := AnalyzePythonFile(ctx, conditionalFile, radon)
	if err != nil {
		t.Fatal(err)
	}
	wantConditional := []InterfaceMetric{
		{Kind: "module", Line: 1, PublicMethods: 1},
		{Name: "Base", Kind: "class", Line: 8, PublicMethods: 1},
		{Name: "Derived", Kind: "class", Line: 12, PublicMethods: 1},
	}
	assertSameInterfaceMetrics(t, conditionalResult.Interfaces, wantConditional)
	conditionalAPI, err := analyzePythonFileWithRadonAPI(ctx, conditionalFile, radon)
	if err != nil {
		t.Fatal(err)
	}
	assertSameInterfaceMetrics(t, conditionalAPI.Interfaces, wantConditional)

	leftDir := filepath.Join(root, "left")
	rightDir := filepath.Join(root, "right")
	if err := os.MkdirAll(leftDir, 0o755); err != nil {
		t.Fatalf("make left directory: %v", err)
	}
	if err := os.MkdirAll(rightDir, 0o755); err != nil {
		t.Fatalf("make right directory: %v", err)
	}
	leftFile := filepath.Join(leftDir, "shared.py")
	rightFile := filepath.Join(rightDir, "shared.py")
	if err := os.WriteFile(leftFile, []byte("class Twin:\n    def left(self):\n        return 1\n"), 0o644); err != nil {
		t.Fatalf("write left shared source: %v", err)
	}
	if err := os.WriteFile(rightFile, []byte("class Twin:\n    def right_one(self):\n        return 1\n    def right_two(self):\n        return 2\n"), 0o644); err != nil {
		t.Fatalf("write right shared source: %v", err)
	}
	repositoryResults, err := AnalyzePythonRepository(ctx, root, radon)
	if err != nil {
		t.Fatalf("AnalyzePythonRepository returned error: %v", err)
	}
	repositorySemantic := findResult(t, repositoryResults, semanticFile)
	assertSameInterfaceMetrics(t, repositorySemantic.Interfaces, cliResult.Interfaces)
	assertSameInterfaceMetrics(t, findResult(t, repositoryResults, conditionalFile).Interfaces, wantConditional)
	leftResult := findResult(t, repositoryResults, leftFile)
	rightResult := findResult(t, repositoryResults, rightFile)
	leftTwin := interfaceMetricsNamed(leftResult.Interfaces, "Twin")
	rightTwin := interfaceMetricsNamed(rightResult.Interfaces, "Twin")
	if len(leftTwin) != 1 || leftTwin[0].PublicMethods != 1 {
		t.Fatalf("left shared interfaces = %+v, want Twin width 1", leftResult.Interfaces)
	}
	if len(rightTwin) != 1 || rightTwin[0].PublicMethods != 2 {
		t.Fatalf("right shared interfaces = %+v, want Twin width 2", rightResult.Interfaces)
	}
}

func TestPythonInterfaceMetricsMissingScan(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.py")
	for name, payload := range map[string]map[string]pythonFindingsItem{
		"absent-entry":     nil,
		"absent-metrics":   {file: {}},
		"empty-metrics":    {file: {Interfaces: []InterfaceMetric{}}},
		"ordinary-finding": {file: {Findings: []Finding{{Rule: "temporal-purity", Line: 1}}}},
		"unresolved-entry": {
			"other.py":   {Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}},
			"another.py": {Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			missing, err := pythonFindingsFor(payload, file)
			if err == nil || err.Error() != "python findings error for "+file+": missing interface metrics" {
				t.Fatalf("missing scan result = %+v, error = %v, want missing interface metrics error", missing, err)
			}
		})
	}
	zeroModule := InterfaceMetric{Kind: "module", Line: 1}
	zeroImports := &ImportMetric{}
	for _, key := range []string{file, "rewritten.py"} {
		scan, err := pythonFindingsFor(map[string]pythonFindingsItem{
			key: {Interfaces: []InterfaceMetric{zeroModule}, Imports: zeroImports},
		}, file)
		if err != nil || len(scan.Interfaces) != 1 || scan.Interfaces[0] != zeroModule {
			t.Fatalf("zero-width module scan = %+v/%v, want preserved zero-width module", scan, err)
		}
	}

	warning := pythonFindingsItem{
		Findings:   []Finding{{Rule: "syntax-warning", Kind: "syntax-warning", Line: 1}},
		Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}},
		Imports:    &ImportMetric{},
	}
	if got, err := pythonFindingsFor(map[string]pythonFindingsItem{file: warning}, file); err == nil ||
		!strings.Contains(err.Error(), "python findings error for "+file) {
		t.Fatalf("syntax-warning scan = %+v/%v, want warning failure", got, err)
	}

	radon, err := exec.LookPath("radon")
	if err != nil {
		t.Skip("radon not installed")
	}
	syntaxFile := filepath.Join(t.TempDir(), "syntax-warning.py")
	if err := os.WriteFile(syntaxFile, []byte("def broken(\n    return 1\n"), 0o644); err != nil {
		t.Fatalf("write syntax-warning source: %v", err)
	}
	result, err := analyzePythonFileWithRadonAPI(context.Background(), syntaxFile, radon)
	if err == nil || !strings.Contains(err.Error(), "python findings error") {
		t.Fatalf("analyzePythonFileWithRadonAPI(syntax warning) result = %+v, error = %v; want failure", result, err)
	}
}

func assertSameInterfaceMetrics(t *testing.T, left, right []InterfaceMetric) {
	t.Helper()
	left = append([]InterfaceMetric(nil), left...)
	right = append([]InterfaceMetric(nil), right...)
	sort.Slice(left, func(i, j int) bool {
		if left[i].Name != left[j].Name {
			return left[i].Name < left[j].Name
		}
		return left[i].Line < left[j].Line
	})
	sort.Slice(right, func(i, j int) bool {
		if right[i].Name != right[j].Name {
			return right[i].Name < right[j].Name
		}
		return right[i].Line < right[j].Line
	})
	if len(left) != len(right) {
		t.Fatalf("interface record lengths differ: API=%d CLI=%d", len(left), len(right))
	}
	for index := range left {
		if left[index] != right[index] {
			t.Errorf("interface record %d differs: API=%+v CLI=%+v", index, left[index], right[index])
		}
	}
}

func interfaceMetricsNamed(metrics []InterfaceMetric, name string) []InterfaceMetric {
	var records []InterfaceMetric
	for _, metric := range metrics {
		if metric.Name == name {
			records = append(records, metric)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Line < records[j].Line })
	return records
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

func fakeRadonWithShebang(t *testing.T, dir, pythonPath string) string {
	t.Helper()
	path := filepath.Join(dir, "radon")
	script := fmt.Sprintf("#!%s\n", pythonPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake radon: %v", err)
	}
	return path
}

func fakeRadonCLI(t *testing.T, dir, logPath, file string) string {
	t.Helper()
	path := filepath.Join(dir, "radon")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf ' %%s %%s\n' "$1" "$*" >> %[1]q
if [ "$1" = "cc" ]; then
  printf '{"%[2]s":[{"type":"F","name":"fallback","complexity":1,"lineno":1,"endline":2}]}'
elif [ "$1" = "raw" ]; then
  printf '{"%[2]s":{"loc":2,"lloc":1}}'
else
  exit 2
fi
`, logPath, file)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake radon CLI: %v", err)
	}
	return path
}

func fakeRadonPython(t *testing.T, dir, logPath string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-python")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf 'invoke\n' >> %[1]q
if [ "$1" != "-c" ]; then
  exit 2
fi
file="${@: -1}"
printf '{"cc":{"%%s":[{"type":"F","name":"one","complexity":1,"lineno":1,"endline":2}]},"raw":{"%%s":{"loc":2,"lloc":1}},"findings":{"%%s":{"findings":[],"interfaces":[{"name":"","kind":"module","line":1,"public_methods":1}],"imports":{"total":0,"used":0,"unused":[],"ddc":1}}}}' "$file" "$file" "$file"
`, logPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake radon python: %v", err)
	}
	return path
}

func fakeShellRadonWithCLIFallback(t *testing.T, dir, logPath, file string) string {
	t.Helper()
	path := filepath.Join(dir, "radon")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf ' %%s %%s\n' "$1" "$*" >> %[1]q
if [ "$1" = "cc" ]; then
  printf '{"%[2]s":[{"type":"F","name":"fallback","complexity":1,"lineno":1,"endline":2}]}'
elif [ "$1" = "raw" ]; then
  printf '{"%[2]s":{"loc":2,"lloc":1}}'
else
  exit 2
fi
`, logPath, file)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake shell radon: %v", err)
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

func fakeRawErrorRadon(t *testing.T, dir, file string) string {
	t.Helper()
	path := filepath.Join(dir, "raw-error-radon")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "cc" ]; then
  printf '{"%[1]s":[]}'
elif [ "$1" = "raw" ]; then
  printf '{"%[1]s":{"error":"invalid syntax"}}'
else
  exit 2
fi
`, file)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake raw-error radon: %v", err)
	}
	return path
}

func fakeSleepTool(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "sleep-tool")
	script := `#!/usr/bin/env bash
sleep 5
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake sleep tool: %v", err)
	}
	return path
}
