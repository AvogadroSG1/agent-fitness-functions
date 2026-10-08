package analyzer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pythonImportScan runs the embedded findings scanner against a real temporary
// file. Import metrics must come from the scanner's AST, not a Go-side parser.
func pythonImportScan(t *testing.T, source string) ImportMetric {
	t.Helper()
	if _, _, ok := findingsPythonCommand(""); !ok {
		t.Skip("Python interpreter unavailable")
	}
	file := filepath.Join(t.TempDir(), "imports.py")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	scan, err := pythonFileFindings(context.Background(), file, "")
	if err != nil {
		t.Fatalf("pythonFileFindings: %v", err)
	}
	if scan.Imports == nil {
		t.Fatalf("pythonFileFindings returned nil imports: %+v", scan)
	}
	return *scan.Imports
}

const pythonImportAllUsedSource = `from __future__ import annotations
from typing import TYPE_CHECKING, Any
from pydantic import field_validator
from dlt.pipeline.helpers import pipeline_drop
from payloads import SENSITIVE_PAYLOAD
from .adapters import Adapter
from .errors import RetrievalRepositoryError as RetrievalRepositoryError

if TYPE_CHECKING:
    from .types import _State
else:
    from types_runtime import _State

__all__ = ["Adapter"]

@field_validator("start_date", mode="before")
def normalize(value: Any, state: _State) -> str:
    pipeline_drop(value, resources=["messages"], state_only=True)()
    return f"provider failed {SENSITIVE_PAYLOAD}"
`

const pythonImportBlockingSource = `from __future__ import annotations
from pathlib import PurePath
import time as unused_time
import json as unused_json
from typing import TYPE_CHECKING, Any
from pydantic import field_validator
from dlt.pipeline.helpers import pipeline_drop
from payloads import SENSITIVE_PAYLOAD
from .adapters import Adapter
from .errors import RetrievalRepositoryError as RetrievalRepositoryError

if TYPE_CHECKING:
    from .types import _State
else:
    from types_runtime import _State

__all__ = ["Adapter"]

@field_validator("start_date", mode="before")
def normalize(value: Any, state: _State) -> str:
    pipeline_drop(value, resources=["messages"], state_only=True)()
    return f"provider failed {SENSITIVE_PAYLOAD}"
`

func assertImportMetric(t *testing.T, got ImportMetric, total, used int, ddc float64, unused ...string) {
	t.Helper()
	if got.Total != total || got.Used != used || got.DDC != ddc {
		t.Fatalf("imports = %+v, want total=%d used=%d ddc=%v", got, total, used, ddc)
	}
	if strings.Join(got.Unused, "\x00") != strings.Join(unused, "\x00") {
		t.Fatalf("unused = %q, want %q", got.Unused, unused)
	}
}

func TestPythonImportMetricPaths(t *testing.T) {
	radon, err := exec.LookPath("radon")
	if err != nil {
		t.Skip("radon not installed")
	}
	ctx := context.Background()
	root := t.TempDir()
	usedFile := filepath.Join(root, "used.py")
	blockingFile := filepath.Join(root, "blocking.py")
	for file, source := range map[string]string{usedFile: pythonImportAllUsedSource, blockingFile: pythonImportBlockingSource} {
		if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	assertResult := func(label string, result AnalysisResult, want ImportMetric) {
		t.Helper()
		if result.Imports.Total != want.Total || result.Imports.Used != want.Used || result.Imports.DDC != want.DDC ||
			strings.Join(result.Imports.Unused, "\x00") != strings.Join(want.Unused, "\x00") {
			t.Fatalf("%s imports = %+v, want %+v", label, result.Imports, want)
		}
	}
	wantUsed := ImportMetric{Total: 9, Used: 9, DDC: 1}
	wantBlocking := ImportMetric{Total: 12, Used: 9, DDC: 0.75, Unused: []string{"PurePath", "unused_time", "unused_json"}}

	apiUsed, err := analyzePythonFileWithRadonAPI(ctx, usedFile, radon)
	if err != nil {
		t.Fatalf("API used: %v", err)
	}
	assertResult("API used", apiUsed, wantUsed)
	apiBlocking, err := analyzePythonFileWithRadonAPI(ctx, blockingFile, radon)
	if err != nil {
		t.Fatalf("API blocking: %v", err)
	}
	assertResult("API blocking", apiBlocking, wantBlocking)

	cliUsed, err := AnalyzePythonFile(ctx, usedFile, radon)
	if err != nil {
		t.Fatalf("CLI used: %v", err)
	}
	assertResult("CLI used", cliUsed, wantUsed)
	cliBlocking, err := AnalyzePythonFile(ctx, blockingFile, radon)
	if err != nil {
		t.Fatalf("CLI blocking: %v", err)
	}
	assertResult("CLI blocking", cliBlocking, wantBlocking)

	repository, err := AnalyzePythonRepository(ctx, root, radon)
	if err != nil {
		t.Fatalf("repository: %v", err)
	}
	assertResult("repository used", findResult(t, repository, usedFile), wantUsed)
	assertResult("repository blocking", findResult(t, repository, blockingFile), wantBlocking)
}

func TestPythonImportMetricSemantics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		total  int
		used   int
		ddc    float64
		unused []string
	}{
		{"future-only", "from __future__ import annotations\n", 0, 0, 1, nil},
		{"empty", "", 0, 0, 1, nil},
		{"annotations", `from __future__ import annotations
from typing import Any, Callable, Mapping, Sequence
from google import GoogleWorkspaceServices
from runs import ActiveRun
from files import RecentFilesProvider
from state import _PostgresState
from preview import _PreviewReadControl

def f(value: Any, callback: Callable[[Mapping[str, Sequence[str]]], GoogleWorkspaceServices]) -> ActiveRun:
    item: RecentFilesProvider
    state: _PostgresState
    control: _PreviewReadControl
    return value
`, 9, 9, 1, nil},
		{"decorators", `from dataclasses import dataclass
from pydantic import field_validator
@dataclass(frozen=True)
class Item:
    @field_validator("name", mode="before")
    def name(self, value):
        return value
`, 2, 2, 1, nil},
		{"keywords", `from x import option, value, consume
consume(option=value)
`, 3, 2, 2.0 / 3.0, []string{"option"}},
		{"fstring", `from payloads import SENSITIVE_PAYLOAD
from time import time
text = f"time {SENSITIVE_PAYLOAD}"
`, 2, 1, 0.5, []string{"time"}},
		{"exports", `from x import EXECUTOR_ADVISORY_LOCK_KEY, PostgresRetrievalRepository, RetrievalRepositoryError, VertexEmbeddingAdapter, VertexEmbeddingConfig, VertexEmbeddingError
__all__ = ["EXECUTOR_ADVISORY_LOCK_KEY", "PostgresRetrievalRepository", "RetrievalRepositoryError", "VertexEmbeddingAdapter", "VertexEmbeddingConfig", "VertexEmbeddingError"]
`, 6, 6, 1, nil},
		{"same-name-export", `from x import Public as Public
from x import Other as Alias
`, 2, 1, 0.5, []string{"Alias"}},
		{"type-checking-runtime", `from typing import TYPE_CHECKING
if TYPE_CHECKING:
    from x import _State
else:
    from y import _State
from z import Unused
value: _State
`, 4, 3, 0.75, []string{"Unused"}},
		{"docstring", `"""from invented import time), "paused_sources", """
import json
`, 1, 0, 0, []string{"json"}},
		{"semicolon-string", `import json; text = "json"
`, 1, 0, 0, []string{"json"}},
		{"parenthesized", `import xml.etree
import pathlib as p
xml.etree.Element("x")
p.Path("x")
`, 2, 2, 1, nil},
		{"forward-annotation", `from x import Model
def f(value: "Model") -> "Model":
    return value
`, 1, 1, 1, nil},
		{"literal-metadata", `from typing import Literal
from x import Word
value: Literal["Word"]
`, 2, 1, 0.5, []string{"Word"}},
		{"assignment-shadow", `import json
def f():
    value = json
    json = {}
    return value
`, 1, 0, 0, []string{"json"}},
		{"sibling-function", `import json
def first():
    return json.dumps({})
def second():
    return 1
`, 1, 1, 1, nil},
		{"class-method-outer", `import json
class C:
    json = {}
    def f(self):
        return json.dumps({})
`, 1, 1, 1, nil},
		{"comprehension", `import json
value = [json.dumps(x) for x in items for x in json.loads(data)]
`, 1, 1, 1, nil},
		{"comprehension-target-shadow", `import json
value = [json for json in json.loads(data)]
`, 1, 1, 1, nil},
		{"nested-closure", `def outer():
    import json
    def inner():
        return json.dumps({})
    return inner
`, 1, 1, 1, nil},
		{"global", `import json
def f():
    global json
    return json.dumps({})
`, 1, 1, 1, nil},
		{"nonlocal", `def outer():
    import json
    def inner():
        nonlocal json
        return json.dumps({})
    return inner
`, 1, 1, 1, nil},
		{"replacement", `import json
json = {}
value = json
`, 1, 0, 0, []string{"json"}},
		{"sequential-reimport", `from x import Name
from y import Name
Name()
`, 2, 1, 0.5, []string{"Name"}},
		{"lambda-parameter-shadow", `import json
f = lambda json: json
g = lambda: json
`, 1, 1, 1, nil},
		{"annotated-all", `from x import Public
__all__: list[str] = ["Public"]
`, 1, 1, 1, nil},
		{"attribute-subscript-target", `import json
json.value = 1
json["key"] = 2
`, 1, 1, 1, nil},
		{"nested-comprehension-iterable", `from x import items
value = [[x for x in x] for x in items]
`, 1, 1, 1, nil},
		{"nested-nonlocal-skip", `def outer():
    import json
    def middle():
        def inner():
            nonlocal json
            return json.dumps({})
        return inner
    return middle
`, 1, 1, 1, nil},
		{"sibling-global-rebind", `import json
def first():
    global json
    json = {}
def second():
    return json.dumps({})
`, 1, 1, 1, nil},
		{"raise-imported-exception", `from x import Error
raise Error()
`, 1, 1, 1, nil},
		{"exception-type-alias-shadow", `from x import Error
try:
    pass
except Error as Error:
    pass
`, 1, 1, 1, nil},
		{"class-delete-restores-outer-binding", `from x import Name
class C:
    from y import Name
    del Name
    value = Name()
`, 2, 1, 0.5, []string{"Name"}},
		{"nested-class-skips-outer-class", `from x import Name
class Outer:
    Name = object()
    class Inner:
        value = Name()
`, 1, 1, 1, nil},
		{"loop-else-uses-body-import", `for item in items:
    from x import Name
else:
    value = Name()
`, 1, 1, 1, nil},
		{"starred-target-rebind", `from x import Name
first, *Name = values
value = Name
`, 1, 0, 0, []string{"Name"}},
		{"tuple-delete-removes-bindings", `from x import A, B
del (A, B)
value = (A, B)
`, 2, 0, 0, []string{"A", "B"}},
		{"dynamic-all-does-not-credit-literal-prefix", `from x import A, B
__all__ = ["A", B]
`, 2, 1, 0.5, []string{"A"}},
		{"exports-tuple-and-augmentation", `from x import A, B
__all__ = ("A",)
__all__ += ["B"]
`, 2, 2, 1, nil},
		{"comprehension-walrus-prebinds-containing-function", `import json
def f():
    json.dumps({})
    [(json := {}) for item in items]
    return json
`, 1, 0, 0, []string{"json"}},
		{"quoted-lambda-annotation", `from x import Model
def f(value: "lambda: Model"):
    return value
`, 1, 1, 1, nil},
		{"global-branch-candidates", `import json
def f():
    global json
    if condition:
        from x import json
    else:
        json = {}
    return json.dumps({})
`, 2, 1, 0.5, []string{"json"}},
		{"nonlocal-branch-candidates", `def outer():
    import json
    def inner():
        nonlocal json
        if condition:
            from x import json
        else:
            json = {}
        return json.dumps({})
    return inner
`, 2, 1, 0.5, []string{"json"}},
		{"nested-global-completed-enclosing-state", `import json
def outer():
    global json
    from x import json
    def inner():
        global json
        return json.dumps({})
    return inner
`, 2, 1, 0.5, []string{"json"}},
		{"class-branch-includes-outer-candidate", `from x import Name
class C:
    if condition:
        from y import Name
    value = Name()
`, 2, 2, 1, nil},
		{"irrefutable-match-rebind", `import capture
match value:
    case capture:
        pass
result = capture
`, 1, 0, 0, []string{"capture"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertImportMetric(t, pythonImportScan(t, test.source), test.total, test.used, test.ddc, test.unused...)
		})
	}

	// Each function owns a distinct import occurrence; neither body may credit
	// the other's binding.
	local := pythonImportScan(t, `def first(value):
    from x import pipeline_drop
    return pipeline_drop(value, resources=["messages"], state_only=True)()
def second(value):
    from y import pipeline_drop
    return pipeline_drop(value, resources=["messages"], state_only=True)()
`)
	assertImportMetric(t, local, 2, 2, 1)
	isolated := pythonImportScan(t, `def first(value):
    from x import pipeline_drop
    return value
def second(value):
    from y import pipeline_drop
    return pipeline_drop(value, resources=["messages"], state_only=True)()
`)
	assertImportMetric(t, isolated, 2, 1, 0.5, "pipeline_drop")

	for _, test := range []struct {
		name   string
		source string
		total  int
		used   int
		ddc    float64
		unused []string
	}{
		{"branch-union", `if flag:
    from x import branch_name
else:
    from y import branch_name
consume(branch_name)
`, 2, 2, 1, nil},
		{"loop-zero-iteration", `import json
for item in items:
    import json
json.dumps({})
`, 2, 2, 1, nil},
		{"try-handler-union", `try:
    from x import value
except Exception:
    from y import value
consume(value)
`, 2, 2, 1, nil},
		{"module-annassign-no-value", `import json
json: object
json.dumps({})
`, 1, 1, 1, nil},
		{"function-annassign-prebind", `import json
def f():
    json: object
    return json.dumps({})
`, 1, 0, 0, []string{"json"}},
		{"augassign-read-before-write", `import json
json += value
`, 1, 1, 1, nil},
		{"delete-removes-binding", `import json
del json
value = json
`, 1, 0, 0, []string{"json"}},
		{"with-target-rebind", `import json
with context as json:
    pass
`, 1, 0, 0, []string{"json"}},
		{"exception-target-rebind", `import error
try:
    pass
except Exception as error:
    pass
`, 1, 0, 0, []string{"error"}},
		{"match-capture-rebind", `import capture
match value:
    case capture:
        pass
`, 1, 0, 0, []string{"capture"}},
		{"named-expression-rebind", `import json
value = (json := {})
result = json
`, 1, 0, 0, []string{"json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertImportMetric(t, pythonImportScan(t, test.source), test.total, test.used, test.ddc, test.unused...)
		})
	}
}

func TestPythonImportMetricModernTypeAlias(t *testing.T) {
	python, _, ok := findingsPythonCommand("")
	if !ok {
		t.Skip("Python interpreter unavailable")
	}
	versionOutput, err := exec.Command(python, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("python --version: %v", err)
	}
	version := strings.TrimSpace(string(versionOutput))
	fields := strings.Fields(version)
	if len(fields) < 2 {
		t.Fatalf("unrecognized Python version output %q", version)
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) < 2 {
		t.Fatalf("unrecognized Python version %q", version)
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major < 3 || (major == 3 && minor < 12) {
		t.Skipf("Python >=3.12 required for type-alias syntax (found %s)", version)
	}
	got := pythonImportScan(t, `from x import Imported
from y import T
type Alias[T] = tuple[T, Imported]
`)
	assertImportMetric(t, got, 2, 1, 0.5, "T")
	generic := pythonImportScan(t, `from x import T
from y import U
def f[T](value: T) -> T:
    return value
class C[U]:
    value: U
`)
	assertImportMetric(t, generic, 2, 0, 0, "T", "U")
	for _, bound := range []string{"(lambda: T)", "[T for _ in range(1)]"} {
		source := "from x import T, Imported\ntype Alias[T: " + bound + "] = Imported\n"
		assertImportMetric(t, pythonImportScan(t, source), 2, 1, 0.5, "T")
	}
	localAlias := pythonImportScan(t, `from x import Alias
def f():
    value = Alias
    type Alias = int
    return value
`)
	assertImportMetric(t, localAlias, 1, 0, 0, "Alias")
}

func TestPythonImportMetricsMissingScan(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.py")
	zero := ImportMetric{DDC: 1, Unused: []string{}}
	valid := pythonFindingsItem{Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}, Imports: &zero}
	if got, err := pythonFindingsFor(map[string]pythonFindingsItem{file: valid}, file); err != nil || got.Imports == nil {
		t.Fatalf("valid zero-import scan = %+v/%v", got, err)
	}
	cases := map[string]struct {
		payload pythonFindingsItem
		want    string
	}{
		"missing-imports": {
			payload: pythonFindingsItem{Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}},
			want:    "python findings error for " + file + ": missing import metrics",
		},
		"error-with-findings": {
			payload: pythonFindingsItem{Error: "AST scan failed: boom", Findings: []Finding{{Rule: "temporal-purity", Line: 1}}, Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}, Imports: &zero},
			want:    "python findings error for " + file + ": AST scan failed: boom",
		},
		"warning-without-error": {
			payload: pythonFindingsItem{Findings: []Finding{{Rule: "syntax-warning", Kind: "syntax-warning", Line: 1}}, Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}, Imports: &zero},
			want:    "python findings error for " + file + ": AST scan unavailable",
		},
		"warning-detail": {
			payload: pythonFindingsItem{Findings: []Finding{{Rule: "syntax-warning", Kind: "syntax-warning", Line: 1, Detail: "AST parse failed"}}, Interfaces: []InterfaceMetric{{Kind: "module", Line: 1}}, Imports: &zero},
			want:    "python findings error for " + file + ": AST parse failed",
		},
		"missing-interfaces": {
			payload: pythonFindingsItem{Imports: &zero},
			want:    "python findings error for " + file + ": missing interface metrics",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pythonFindingsFor(map[string]pythonFindingsItem{file: test.payload}, file)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	radon, err := exec.LookPath("radon")
	if err != nil {
		t.Skip("radon not installed")
	}
	bad := filepath.Join(t.TempDir(), "broken.py")
	if err := os.WriteFile(bad, []byte("def broken(\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzePythonFileWithRadonAPI(context.Background(), bad, radon); err == nil || !strings.Contains(err.Error(), "python findings error") {
		t.Fatalf("API malformed source error = %v, want python findings error", err)
	}
	if _, err := AnalyzePythonFile(context.Background(), bad, radon); err == nil || !strings.Contains(err.Error(), "python findings error") {
		t.Fatalf("CLI malformed source error = %v, want python findings error", err)
	}
}
