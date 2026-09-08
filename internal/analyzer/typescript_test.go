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

func TestAnalyzeTypeScriptFileClassMethodsAndComplexSignatures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "user_service.ts")
	source := `import { Logger } from "./logger";

export class UserService {
  constructor(private readonly logger: Logger) {}

  public async getUser(
    id: string,
    opts?: { detail?: boolean }
  ): Promise<User | null> {
    if (!id) {
      return null;
    }
    const user = id ? { id, name: "test" } : null;
    return user?.name ?? "unknown";
  }

  private validate(user: User): boolean {
    return user.id !== "";
  }

  get isReady(): boolean {
    return true;
  }
}
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if result.CALMNode != "UserService" {
		t.Fatalf("CALMNode = %q, want UserService", result.CALMNode)
	}
	// Functions: constructor, getUser, validate, isReady
	if len(result.Functions) < 3 {
		t.Fatalf("functions count = %d, want >= 3: %+v", len(result.Functions), result.Functions)
	}

	var getUserFn *FunctionMetric
	for i := range result.Functions {
		if result.Functions[i].Name == "getUser" {
			getUserFn = &result.Functions[i]
			break
		}
	}
	if getUserFn == nil {
		t.Fatalf("getUser method not found in %+v", result.Functions)
	}
	if !getUserFn.IsPublic {
		t.Fatalf("getUser IsPublic = false, want true")
	}
	// getUser has: `if (!id)` (+1), ternary `id ? ... : ...` (+1), `user?.name ?? "unknown"` (?? is +1, ?. is +0), opts?: is +0
	// Base 1 + 1 (if) + 1 (ternary) + 1 (??) = 4
	if getUserFn.CyclomaticComplexity != 4 {
		t.Fatalf("getUser CyclomaticComplexity = %d, want 4", getUserFn.CyclomaticComplexity)
	}
}

func TestAnalyzeTypeScriptFileHandlesRegexLiteralsAndNestedTemplates(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regex_template.ts")
	source := `export function parseUrl(input: string): string {
  // Regex literal containing quotes, slashes, and asterisk
  const urlRe = /https?:\/\/[^\/]+\/(.*)/i;
  const quoteRe = /'[^']*'/g;
  const match = input.match(urlRe);
  const tag = ` + "`" + `outer ${` + "`" + `nested ${input ? "yes" : "no"}` + "`" + `}` + "`" + `;
  return match ? match[1] : tag;
}
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Functions) != 1 {
		t.Fatalf("functions = %+v, want 1 function", result.Functions)
	}
	fn := result.Functions[0]
	if fn.Name != "parseUrl" || !fn.IsPublic {
		t.Fatalf("parseUrl fn = %+v", fn)
	}
	// Base 1 + 1 (nested ternary `input ?`) + 1 (`match ?`) = 3
	if fn.CyclomaticComplexity != 3 {
		t.Fatalf("CyclomaticComplexity = %d, want 3", fn.CyclomaticComplexity)
	}
}

func TestAnalyzeTypeScriptFileHandlesAliasedAndNamespaceImports(t *testing.T) {
	file := filepath.Join(t.TempDir(), "complex_imports.ts")
	source := `import DefaultLogger, { debug as logDebug, info, type LogLevel } from "logger";
import * as React from "react";
import type { Config } from "config";

const config: Config = { env: "prod" };
logDebug("starting app");
React.createElement("div");
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// 3 imports: "logger" (used bindings: logDebug; unused: DefaultLogger, info, LogLevel),
	//            "react" (used: React),
	//            "config" (used: Config)
	if result.Imports.Total != 3 {
		t.Fatalf("Imports.Total = %d, want 3", result.Imports.Total)
	}
	if result.Imports.Used != 3 {
		t.Fatalf("Imports.Used = %d, want 3", result.Imports.Used)
	}
	// Unused bindings list should identify logger unused parts
	if len(result.Imports.Unused) != 1 {
		t.Fatalf("Imports.Unused = %v, want 1 partial unused module", result.Imports.Unused)
	}
}

func TestAnalyzeTypeScriptFileHandlesMultilineConciseArrows(t *testing.T) {
	file := filepath.Join(t.TempDir(), "multiline_arrow.tsx")
	source := `export const Component = (props: { name: string }) => (
  <div>
    <h1>{props.name}</h1>
  </div>
);
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeTypeScriptFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Functions) != 1 {
		t.Fatalf("functions = %+v, want 1 function", result.Functions)
	}
	fn := result.Functions[0]
	if fn.Name != "Component" || !fn.IsPublic || fn.LOC != 5 {
		t.Fatalf("Component = %+v, want 5 LOC", fn)
	}
}
