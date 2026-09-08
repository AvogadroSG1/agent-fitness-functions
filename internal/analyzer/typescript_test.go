package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeTypeScriptFileReturnsNormalizedMetrics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "service.ts")
	source := `import { readFile } from "fs";
import unused from "unused";

export function load(value: string): string {
  if (value === "") return "empty";
  return readFile(value);
}

function hidden(value: string): string {
  return value;
}
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if result.Language != "typescript" || result.CALMNode != "service" {
		t.Fatalf("identity = %q/%q, want typescript/service", result.Language, result.CALMNode)
	}
	if len(result.Functions) != 2 {
		t.Fatalf("functions = %+v, want two functions", result.Functions)
	}
	if result.Functions[0].Name != "load" || !result.Functions[0].IsPublic || result.Functions[0].CyclomaticComplexity != 2 {
		t.Fatalf("load = %+v, want exported complexity 2", result.Functions[0])
	}
	if result.FileMetric.PublicMethods != 1 || result.Imports.Total != 2 || result.Imports.Used != 1 || len(result.Imports.Unused) != 1 {
		t.Fatalf("metrics = file=%+v imports=%+v, want one public method and one unused import", result.FileMetric, result.Imports)
	}
}

func TestDiscoverTypeScriptFilesIncludesTSXAndSkipsDependencies(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app.ts", "view.tsx", filepath.Join("node_modules", "ignored.ts"), filepath.Join("dist", "ignored.tsx")} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("export function run() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := discoverFiles(dir, "typescript")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %v, want app.ts and view.tsx", files)
	}
}

func TestAnalyzeTypeScriptFileHandlesTypedConciseArrowsAndTernaries(t *testing.T) {
	file := filepath.Join(t.TempDir(), "arrows.ts")
	source := `const add = (a: number, b: number): number => a + b;
export const choose = (value: boolean): string => {
  return value ? "yes" : "no";
};
function privateValue(): string {
  return "private";
}
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Functions) != 3 {
		t.Fatalf("functions = %+v, want three functions", result.Functions)
	}
	if result.Functions[0].Name != "add" || result.Functions[0].LOC != 1 {
		t.Fatalf("add = %+v, want one-line concise arrow", result.Functions[0])
	}
	if result.Functions[1].Name != "choose" || result.Functions[1].CyclomaticComplexity != 2 || !result.Functions[1].IsPublic {
		t.Fatalf("choose = %+v, want exported ternary complexity 2", result.Functions[1])
	}
	if result.Functions[2].IsPublic {
		t.Fatalf("privateValue = %+v, want private", result.Functions[2])
	}
}

func TestAnalyzeTypeScriptFileTracksNamedImportsByIdentifier(t *testing.T) {
	file := filepath.Join(t.TempDir(), "imports.ts")
	source := `import { used, unused as renamed } from "library";
const userId = used;
console.log(userId);
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imports.Total != 1 || result.Imports.Used != 1 || result.Imports.DDC != 1 || len(result.Imports.Unused) != 1 {
		t.Fatalf("imports = %+v, want used module with one unused named binding reported", result.Imports)
	}
}

func TestAnalyzeTypeScriptFileExcludesTemplateTextAndTypeDeclarationsFromLogic(t *testing.T) {
	file := filepath.Join(t.TempDir(), "declarations.ts")
	source := "type Item = { id: string; };\ninterface Config { enabled: boolean; }\nconst render = (item: Item): string => `value ${item.id ? item.id : \"none\"}`;\n"
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Functions) != 1 || result.Functions[0].CyclomaticComplexity != 2 {
		t.Fatalf("functions = %+v, want template expression ternary counted", result.Functions)
	}
	if result.FileMetric.LogicLOC != 1 {
		t.Fatalf("logic LOC = %d, want only executable declaration", result.FileMetric.LogicLOC)
	}
}
