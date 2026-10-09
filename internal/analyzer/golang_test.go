package analyzer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	if result.ModuleMetric.PublicMethods != 2 || result.ModuleMetric.TotalLOC != 27 || result.ModuleMetric.AverageLOCPerPublicMethod != 7 {
		t.Fatalf("module metrics = %+v, want public methods 2, total LOC 27, avg LOC/public 7", result.ModuleMetric)
	}
	if result.Imports.Total != 2 || result.Imports.Used != 2 {
		t.Fatalf("imports = %+v, want 2/2 used", result.Imports)
	}
}

func TestAnalyzeGoFileScopesInterfaceMetricsByReceiver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interfaces.go")
	source := `package sample

type Box[T any] struct{}
type hidden struct{}

func ExportedPackage() {}

func (b Box[T]) Get() {}
func (b *Box[T]) Put() {}
func (h hidden) Open() {}
func (h *hidden) Close() {}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	result, err := AnalyzeGoFile(path)
	if err != nil {
		t.Fatalf("AnalyzeGoFile returned error: %v", err)
	}
	if result.FileMetric.PublicMethods != 5 {
		t.Fatalf("file public methods = %d, want 5", result.FileMetric.PublicMethods)
	}
	if len(result.Interfaces) != 3 {
		t.Fatalf("interfaces = %+v, want module, Box, and hidden", result.Interfaces)
	}
	if result.Interfaces[0] != (InterfaceMetric{Kind: "module", Line: 1, PublicMethods: 1}) {
		t.Fatalf("module interface = %+v, want one package function", result.Interfaces[0])
	}
	want := map[string]int{"Box": 2, "hidden": 2}
	for _, metric := range result.Interfaces[1:] {
		if metric.Kind != "class" {
			t.Errorf("receiver %q kind = %q, want class", metric.Name, metric.Kind)
		}
		if metric.PublicMethods != want[metric.Name] {
			t.Errorf("receiver %q width = %d, want %d", metric.Name, metric.PublicMethods, want[metric.Name])
		}
		delete(want, metric.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing receiver interfaces: %v", want)
	}
}

func TestAggregateGoInterfaceMetricsAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) AnalysisResult {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		result, err := AnalyzeGoFile(path)
		if err != nil {
			t.Fatalf("AnalyzeGoFile(%s): %v", name, err)
		}
		return result
	}
	results := AggregateModuleMetrics([]AnalysisResult{
		write("left.go", `package sample

type State struct{}

func PackageA() {}
func (s State) Start() {}
`),
		write("right.go", `package sample

func PackageB() {}
func (s *State) Stop() {}
`),
	})
	if len(results) != 2 {
		t.Fatalf("aggregated result count = %d, want 2", len(results))
	}
	for _, result := range results {
		if len(result.Interfaces) != 2 {
			t.Fatalf("interfaces for %s = %+v, want module and State", result.File, result.Interfaces)
		}
		if result.Interfaces[0].Kind != "module" || result.Interfaces[0].PublicMethods != 2 {
			t.Errorf("module interface for %s = %+v, want width 2", result.File, result.Interfaces[0])
		}
		if result.Interfaces[1].Name != "State" || result.Interfaces[1].PublicMethods != 2 {
			t.Errorf("State interface for %s = %+v, want width 2", result.File, result.Interfaces[1])
		}
	}
}

func TestGoInterfaceWidthUsesIndependentSurfaces(t *testing.T) {
	buildSource := func(receiverMethods int) string {
		source := "package sample\n\ntype State struct{}\n\n"
		for index := range 11 {
			source += "func Package" + strconv.Itoa(index) + "() {}\n"
		}
		for index := range receiverMethods {
			source += "func (s State) Method" + strconv.Itoa(index) + "() {}\n"
		}
		return source
	}
	analyze := func(source string) AnalysisResult {
		t.Helper()
		path := filepath.Join(t.TempDir(), "width.go")
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("write width source: %v", err)
		}
		result, err := AnalyzeGoFile(path)
		if err != nil {
			t.Fatalf("AnalyzeGoFile: %v", err)
		}
		return result
	}

	split := analyze(buildSource(10))
	if InterfaceWidth(split) != 11 {
		t.Fatalf("split interface width = %d, want module width 11 rather than combined 21", InterfaceWidth(split))
	}
	wide := analyze(buildSource(21))
	if InterfaceWidth(wide) != 21 {
		t.Fatalf("wide receiver interface width = %d, want 21", InterfaceWidth(wide))
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

func TestDiscoverGoFilesExcludesTestAndGeneratedFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	stub := "package sample\n"
	writeFile("main.go", stub)
	writeFile("util.go", stub)
	writeFile("util_test.go", stub)
	writeFile("wire_generated.go", stub)
	writeFile("schema_generated.go", stub)

	files, err := DiscoverGoFiles(dir)
	if err != nil {
		t.Fatalf("DiscoverGoFiles returned error: %v", err)
	}

	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, "_test.go") {
			t.Errorf("DiscoverGoFiles included test file: %s", base)
		}
		if strings.HasSuffix(base, "_generated.go") {
			t.Errorf("DiscoverGoFiles included generated file: %s", base)
		}
	}

	if len(files) != 2 {
		t.Errorf("DiscoverGoFiles returned %d files, want 2 (main.go and util.go)", len(files))
	}
}

func TestGoEmbedAssetClassification(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "exact nine-line source",
			src: `// Package configs embeds static configuration assets.
// Profiles are bundled with the executable.
// Assets are read by the configuration layer.
// Runtime overrides are handled by consumers.
// This file declares assets; it executes no functions.
package configs

import "embed"

//go:embed assets/*
var FS embed.FS
`,
			want: true,
		},
		{
			name: "repository embed source",
			src:  "",
			want: true,
		},
		{
			name: "named alias",
			src: `package configs
import e "embed"
//go:embed assets/*
var FS e.FS
`,
			want: true,
		},
		{
			name: "blank import string and bytes",
			src: `package configs
import _ "embed"
//go:embed text
var Text string
//go:embed raw
var Raw []byte
//go:embed raw8
var Raw8 []uint8
`,
			want: true,
		},
		{
			name: "dot import FS",
			src: `package configs
import . "embed"
//go:embed assets/*
var Assets FS
`,
			want: true,
		},
		{
			name: "parenthesized supported types",
			src: `package configs
import e "embed"
//go:embed text
var Text (string)
//go:embed raw
var Raw ([](byte))
//go:embed raw8
var Raw8 ([](uint8))
//go:embed fs
var FS (e.FS)
`,
			want: true,
		},
		{
			name: "directives inside var group",
			src: `package configs
import _ "embed"
var (
	//go:embed one
	One string

	// ordinary comment
	//go:embed two
	Two []byte
)
`,
			want: true,
		},
		{
			name: "repeated directives and line comments",
			src: `package configs
import _ "embed"
//go:embed first
// an ordinary line comment

//go:embed second
var Files []byte
`,
			want: true,
		},
		{
			name: "import only",
			src: `package configs
import _ "embed"
`,
		},
		{
			name: "no directive",
			src: `package configs
import _ "embed"
var Asset string
`,
		},
		{
			name: "package only",
			src:  "package configs\n",
		},
		{
			name: "empty directive arguments",
			src: `package configs
import _ "embed"
//go:embed
var Asset string
`,
		},
		{
			name: "directive spelling has space",
			src: `package configs
import _ "embed"
// go:embed asset
var Asset string
`,
		},
		{
			name: "directive spelling has tab",
			src:  "package configs\nimport _ \"embed\"\n//go:embed\tasset\nvar Asset string\n",
		},
		{
			name: "directive prefix is not exact",
			src: `package configs
import _ "embed"
//go:embedding asset
var Asset string
`,
		},
		{
			name: "directive in block comment",
			src: `package configs
import _ "embed"
/* //go:embed asset */
var Asset string
`,
		},
		{
			name: "trailing on code",
			src:  "package configs\nimport _ \"embed\"\nvar Prior string //go:embed prior\n//go:embed asset\nvar Asset string\n",
		},
		{
			name: "directive belongs to previous declaration",
			src: `package configs
import _ "embed"
//go:embed prior
var Prior string
var Asset string
`,
		},
		{
			name: "block before directive is harmless",
			src: `package configs
import _ "embed"
/* documentation */
//go:embed asset
var Asset string
`,
			want: true,
		},
		{
			name: "directive above whole var block",
			src: `package configs
import _ "embed"
//go:embed all
var (
	First string
	Second []byte
)
`,
		},
		{
			name: "block comment barrier",
			src: `package configs
import _ "embed"
//go:embed asset
/* barrier */
var Asset string
`,
		},
		{
			name: "function including gocyclo ignore",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset string
//gocyclo:ignore
func init() {}
`,
		},
		{
			name: "method declaration",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset string
type Loader struct{}
func (Loader) Load() {}
`,
		},
		{
			name: "function literal initializer",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset = func() string { return "value" }
`,
		},
		{
			name: "call initializer",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset = load()
func load() string { return "value" }
`,
		},
		{
			name: "initializer",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset = "value"
`,
		},
		{
			name: "extra import",
			src: `package configs
import (
	_ "embed"
	"fmt"
)
//go:embed asset
var Asset string
`,
		},
		{
			name: "ordinary variable",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset string
var Count int
`,
		},
		{
			name: "const declaration",
			src: `package configs
import _ "embed"
const Count = 1
//go:embed asset
var Asset string
`,
		},
		{
			name: "type declaration",
			src: `package configs
import _ "embed"
type AssetType string
//go:embed asset
var Asset AssetType
`,
		},
		{
			name: "shadowed builtin type",
			src: `package configs
import _ "embed"
type string = int
//go:embed asset
var Asset string
`,
		},
		{
			name: "shadowed dot-import FS",
			src: `package configs
import . "embed"
type FS = int
//go:embed asset
var Asset FS
`,
		},
		{
			name: "unrelated package alias",
			src: `package configs
import embed "fmt"
//go:embed asset
var Asset embed.Stringer
`,
		},
		{
			name: "unsupported type",
			src: `package configs
import _ "embed"
//go:embed asset
var Asset int
`,
		},
		{
			name: "multiple variable names",
			src: `package configs
import _ "embed"
//go:embed asset
var First, Second string
`,
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var file string
			if test.name == "repository embed source" {
				file = filepath.Join("..", "..", "patterns", "embed.go")
			} else {
				file = filepath.Join(t.TempDir(), "embed.go")
				if err := os.WriteFile(file, []byte(test.src), 0o644); err != nil {
					t.Fatalf("write source: %v", err)
				}
			}
			result, err := AnalyzeGoFile(file)
			if err != nil {
				t.Fatalf("AnalyzeGoFile returned error: %v", err)
			}
			got := result.SourceKind == SourceKindGoEmbedAssets
			if got != test.want {
				t.Fatalf("source kind = %q, classified = %v, want %v", result.SourceKind, got, test.want)
			}
			if test.name == "exact nine-line source" {
				if result.FileMetric.TotalLOC != 9 || result.FileMetric.LogicLOC != 1 ||
					result.FileMetric.LDR != float64(1)/float64(9) {
					t.Fatalf("metrics = %+v, want LOC 9/1 and LDR 1/9", result.FileMetric)
				}
				if len(result.Functions) != 0 || result.Imports.Total != 1 || result.Imports.Used != 1 {
					t.Fatalf("functions/imports = %d/%+v, want no functions and one used import", len(result.Functions), result.Imports)
				}
			}
		})
	}
}

func TestGoEmbedClassificationErrorsAndJSONApplicability(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.go")
	if err := os.WriteFile(malformed, []byte("package configs\nvar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AnalyzeGoFile(malformed); err == nil {
		t.Fatal("malformed source unexpectedly analyzed")
	}
	_, err := AnalyzeGoFile(filepath.Join(dir, "missing.go"))
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error = %v, want not-exist error", err)
	}

	assetPath := filepath.Join(dir, "asset.go")
	assetSource := `package configs
import _ "embed"
//go:embed asset
var Asset []byte
`
	if err := os.WriteFile(assetPath, []byte(assetSource), 0o644); err != nil {
		t.Fatal(err)
	}
	asset, err := AnalyzeGoFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(asset)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip AnalysisResult
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.SourceKind != SourceKindGoEmbedAssets || LogicDensityApplicable(roundTrip) {
		t.Fatalf("round trip source kind/applicability = %q/%v", roundTrip.SourceKind, LogicDensityApplicable(roundTrip))
	}

	var legacy AnalysisResult
	if err := json.Unmarshal([]byte(`{"language":"go","file_metrics":{"total_loc":1,"logic_loc":1,"ldr":1}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	unknown := legacy
	unknown.SourceKind = SourceKind("other")
	nonGo := asset
	nonGo.Language = "python"
	for name, result := range map[string]AnalysisResult{
		"legacy": legacy, "unknown": unknown, "non-go": nonGo,
	} {
		wire, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("%s marshal: %v", name, err)
		}
		var decoded AnalysisResult
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatalf("%s unmarshal: %v", name, err)
		}
		if !LogicDensityApplicable(decoded) {
			t.Errorf("%s result incorrectly marked inapplicable after round trip: %+v", name, decoded)
		}
	}
	marked := legacy
	marked.Language = "go"
	marked.SourceKind = SourceKindGoEmbedAssets
	if LogicDensityApplicable(marked) {
		t.Error("exact Go asset marker remained applicable")
	}

	peerPath := filepath.Join(dir, "peer.go")
	peerSource := "package configs\nfunc load() string { return \"x\" }\n"
	if err := os.WriteFile(peerPath, []byte(peerSource), 0o644); err != nil {
		t.Fatal(err)
	}
	peer, err := AnalyzeGoFile(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	aggregated := AggregateModuleMetrics([]AnalysisResult{asset, peer})
	if len(aggregated) != 2 {
		t.Fatalf("aggregated result count = %d, want 2", len(aggregated))
	}
	if aggregated[0].SourceKind != asset.SourceKind || aggregated[0].FileMetric.LDR != asset.FileMetric.LDR {
		t.Fatalf("asset aggregate lost source/raw density: before=%+v after=%+v", asset, aggregated[0])
	}
	if aggregated[1].SourceKind != peer.SourceKind || aggregated[1].FileMetric.LDR != peer.FileMetric.LDR {
		t.Fatalf("peer aggregate changed source/raw density: before=%+v after=%+v", peer, aggregated[1])
	}
	if aggregated[0].ModuleMetric.TotalLOC != asset.FileMetric.TotalLOC+peer.FileMetric.TotalLOC {
		t.Fatalf("aggregate module LOC = %d, want %d", aggregated[0].ModuleMetric.TotalLOC, asset.FileMetric.TotalLOC+peer.FileMetric.TotalLOC)
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
