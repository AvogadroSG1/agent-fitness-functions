package analyzer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
)

type radonCCItem struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Complexity int    `json:"complexity"`
	LineNo     int    `json:"lineno"`
	EndLine    int    `json:"endline"`
}

type radonRawItem struct {
	Error string `json:"error"`
	LOC   int    `json:"loc"`
	LLOC  int    `json:"lloc"`
}

type radonErrorItem struct {
	Error string `json:"error"`
}

// AnalyzePythonFile analyzes one Python source file using radon.
func AnalyzePythonFile(ctx context.Context, file, radonPath string) (AnalysisResult, error) {
	if radonPath == "" {
		var managed bool
		radonPath, managed = resolvePythonRadonPath(os.Getenv)
		if !managed {
			if result, err := analyzePythonFileWithRadonAPI(ctx, file, radonPath); err == nil {
				return result, nil
			}
		}
	}
	functions, fileMetric, err := pythonRadonCLIMetrics(ctx, file, radonPath)
	if err != nil {
		return AnalysisResult{}, err
	}
	findings, err := pythonFileFindings(ctx, file, radonPath)
	if err != nil {
		return AnalysisResult{}, err
	}
	return pythonAnalysisResult(file, functions, fileMetric, findings)
}

// resolvePythonRadonPath picks the radon binary for a caller that named none.
// The shipped managed runtime's pinned radon wins when one is installed;
// otherwise a bare PATH lookup does. The bool reports whether the managed
// binary won, which is what suppresses the host-python radon API fast path.
func resolvePythonRadonPath(getenv func(string) string) (string, bool) {
	if managed := managedRadonPath(getenv); managed != "" {
		return managed, true
	}
	return "radon", false
}

// pythonRadonCLIMetrics runs radon's cc and raw passes over one file and parses
// both. Cancellation is checked between the subprocess work and the parsing, so
// a cancelled analysis never reports a partial verdict.
func pythonRadonCLIMetrics(ctx context.Context, file, radonPath string) ([]FunctionMetric, FileMetric, error) {
	ccOutput, err := runTool(ctx, radonPath, "cc", "-j", file)
	if err != nil {
		return nil, FileMetric{}, fmt.Errorf("running radon cc: %w", err)
	}
	rawOutput, err := runTool(ctx, radonPath, "raw", "-j", file)
	if err != nil {
		return nil, FileMetric{}, fmt.Errorf("running radon raw: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, FileMetric{}, err
	}
	functions, err := tolerantRadonCC(file, ccOutput)
	if err != nil {
		return nil, FileMetric{}, err
	}
	fileMetric, err := tolerantRadonRaw(file, rawOutput)
	if err != nil {
		return nil, FileMetric{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, FileMetric{}, err
	}
	return functions, fileMetric, nil
}

// isRadonSourceError reports whether err is radon failing to parse the source
// rather than radon itself being broken.
func isRadonSourceError(err error, toolMarker string) bool {
	return strings.Contains(err.Error(), "invalid syntax") || strings.Contains(err.Error(), toolMarker)
}

// tolerantRadonCC treats a source file radon cannot parse as having no
// functions, so the file is still scored on its remaining metrics.
func tolerantRadonCC(file string, output []byte) ([]FunctionMetric, error) {
	functions, err := parseRadonCC(file, output)
	if err == nil {
		return functions, nil
	}
	if isRadonSourceError(err, "radon cc error") {
		return []FunctionMetric{}, nil
	}
	return nil, err
}

// tolerantRadonRaw falls back to counting the file directly when radon cannot
// parse it, surfacing the original parse error if that fallback also fails.
func tolerantRadonRaw(file string, output []byte) (FileMetric, error) {
	fileMetric, err := parseRadonRaw(file, output)
	if err == nil {
		return fileMetric, nil
	}
	if !isRadonSourceError(err, "radon raw error") {
		return FileMetric{}, err
	}
	fallback, fallbackErr := fallbackFileMetric(file)
	if fallbackErr != nil {
		return FileMetric{}, err
	}
	return fallback, nil
}

func fallbackFileMetric(file string) (FileMetric, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return FileMetric{}, err
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)
	logic := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			logic++
		}
	}
	return FileMetric{TotalLOC: total, LogicLOC: logic}, nil
}

// managedRadonPath returns the shipped managed Python runtime's pinned
// radon binary under this install's state root
// ($XDG_STATE_HOME/agent-fitness-functions/runtimes/python/current/bin/radon),
// per ADR-0005 slice 10 parity with managedRoslynAnalyzerPath's C# analyzer
// resolution: an installed managed runtime's own radon takes precedence
// over both the host-python radon API fallback and a bare PATH lookup.
// Returns "" when running from a source checkout with no such installed
// root (the common dev/test case), leaving the existing API fallback and
// PATH resolution in effect.
func managedRadonPath(getenv func(string) string) string {
	path := filepath.Join(installer.StateRoot(getenv), "runtimes", "python", "current", "bin", "radon")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return ""
	}
	return path
}

// managedPythonInterpreterPath returns the shipped managed Python runtime's
// python binary under this install's state root.
func managedPythonInterpreterPath(getenv func(string) string) string {
	candidates := []string{
		filepath.Join(installer.StateRoot(getenv), "runtimes", "python", "current", "bin", "python3"),
		filepath.Join(installer.StateRoot(getenv), "runtimes", "python", "current", "bin", "python"),
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			filepath.Join(installer.StateRoot(getenv), "runtimes", "python", "current", "Scripts", "python.exe"),
			filepath.Join(installer.StateRoot(getenv), "runtimes", "python", "current", "python.exe"),
		)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
			return candidate
		}
	}
	return ""
}

var modernPythonCandidates = []string{"python3.13", "python3.12", "python3", "python"}

type radonAPIOutput struct {
	CC  map[string]json.RawMessage `json:"cc"`
	Raw map[string]radonRawItem    `json:"raw"`
	// Findings carries the single-AST findings and scoped-interface scan that
	// shares this subprocess, so the fast path costs one interpreter launch for
	// metrics and findings together.
	Findings map[string]pythonFindingsItem `json:"findings"`
}

const radonAPIScript = pythonFindingsLibrary + `
import json
import pathlib
import sys

try:
    from radon.complexity import cc_visit
    from radon.raw import analyze
except ImportError:
    import glob, os, shutil
    radon_bin = shutil.which("radon")
    if radon_bin:
        real_bin = os.path.realpath(radon_bin)
        venv_dir = os.path.dirname(os.path.dirname(real_bin))
        for sp in glob.glob(os.path.join(venv_dir, "lib", "python*", "site-packages")):
            if sp not in sys.path:
                sys.path.insert(0, sp)
    try:
        from radon.complexity import cc_visit
        from radon.raw import analyze
    except ImportError:
        cc_visit = None
        analyze = None

def _try_modern_python_cc(path, source):
    import os, shutil, subprocess
    for candidate in ["python3.13", "python3.12", "python3"]:
        py_path = shutil.which(candidate)
        if not py_path:
            continue
        try:
            ver = subprocess.check_output([py_path, "-c", "import sys; print(sys.version_info[:2] >= (3, 12))"]).decode().strip()
            if ver != "True":
                continue
            env = dict(os.environ)
            sp_paths = [p for p in sys.path if "site-packages" in p]
            if sp_paths:
                env["PYTHONPATH"] = os.pathsep.join(sp_paths) + (os.pathsep + env.get("PYTHONPATH", "") if env.get("PYTHONPATH") else "")
            helper = """
import sys, json, pathlib
try:
    from radon.complexity import cc_visit
    src = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace")
    items = [
        {
            "type": getattr(item, "letter", ""),
            "name": item.name,
            "complexity": item.complexity,
            "lineno": item.lineno,
            "endline": item.endline,
        }
        for item in cc_visit(src)
    ]
    print(json.dumps(items))
except Exception:
    print(json.dumps([]))
"""
            out = subprocess.check_output([py_path, "-c", helper, path], env=env)
            return json.loads(out)
        except Exception:
            continue
    return None

path = sys.argv[1]
source = pathlib.Path(path).read_text(encoding="utf-8", errors="replace")
payload = {"cc": {}, "raw": {}, "findings": {}}

cc_items = None
if cc_visit is not None:
    try:
        cc_items = [
            {
                "type": getattr(item, "letter", ""),
                "name": item.name,
                "complexity": item.complexity,
                "lineno": item.lineno,
                "endline": item.endline,
            }
            for item in cc_visit(source)
        ]
    except Exception:
        cc_items = _try_modern_python_cc(path, source)

if cc_items is None:
    cc_items = []
payload["cc"][path] = cc_items

raw_done = False
if analyze is not None:
    try:
        raw = analyze(source)
        payload["raw"][path] = {"loc": raw.loc, "lloc": raw.lloc}
        raw_done = True
    except Exception:
        pass

if not raw_done:
    lines = source.splitlines()
    loc = len(lines)
    lloc = sum(1 for line in lines if line.strip() and not line.strip().startswith("#"))
    payload["raw"][path] = {"loc": loc, "lloc": lloc}

try:
    payload["findings"][path] = scan_source(source)
except Exception as exc:
    payload["findings"][path] = {
        "findings": [
            {
                "rule": "syntax-warning",
                "kind": "syntax-warning",
                "line": 1,
                "detail": f"AST scan failed: {exc}",
            }
        ],
        "error": f"AST scan failed: {exc}",
    }
json.dump(payload, sys.stdout)
`

// analyzePythonFileWithRadonAPI is the fast path: one interpreter launch that
// returns radon's cc and raw metrics together with the ast findings scan.
func analyzePythonFileWithRadonAPI(ctx context.Context, file, radonPath string) (AnalysisResult, error) {
	payload, err := runRadonAPI(ctx, file, radonPath)
	if err != nil {
		return AnalysisResult{}, err
	}
	ccPayload, err := decodeRadonCCPayload(payload.CC)
	if err != nil {
		return AnalysisResult{}, err
	}
	rawOutput, err := json.Marshal(payload.Raw)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("encoding radon raw payload: %w", err)
	}
	fileMetric, err := parseRadonRaw(file, rawOutput)
	if err != nil {
		return AnalysisResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return AnalysisResult{}, err
	}
	scan, err := pythonFindingsFor(payload.Findings, file)
	if err != nil {
		return AnalysisResult{}, err
	}
	return pythonAnalysisResult(file, pythonFunctions(ccPayload[file]), fileMetric, scan)
}

// runRadonAPI launches the embedded radon API script and decodes its payload.
// An unresolvable interpreter, a failing launch, and unparsable output are all
// errors, which is what makes AnalyzePythonFile fall back to the radon CLI.
func runRadonAPI(ctx context.Context, file, radonPath string) (radonAPIOutput, error) {
	python, args, ok := radonPythonCommand(radonPath)
	if !ok {
		return radonAPIOutput{}, fmt.Errorf("radon python interpreter unavailable")
	}
	args = append(args, "-c", radonAPIScript, file)
	output, err := runTool(ctx, python, args...)
	if err != nil {
		return radonAPIOutput{}, fmt.Errorf("running radon python api: %w", err)
	}
	var payload radonAPIOutput
	if err := json.Unmarshal(output, &payload); err != nil {
		return radonAPIOutput{}, fmt.Errorf("parsing radon python api: %w: %s", err, trimOutput(output))
	}
	return payload, nil
}

func radonPythonCommand(radonPath string) (string, []string, bool) {
	if radonPath == "" {
		radonPath = "radon"
	}
	path, err := exec.LookPath(radonPath)
	if err != nil {
		return fallbackPythonInterpreter()
	}
	line, ok := readFirstLine(path)
	if !ok {
		return "", nil, false
	}
	if !strings.HasPrefix(line, "#!") {
		return fallbackPythonInterpreter()
	}
	return interpreterFromShebang(strings.TrimPrefix(line, "#!"))
}

// fallbackPythonInterpreter resolves an interpreter when radon is absent from
// PATH or is not a shebang script: the managed interpreter first, then the
// known-modern candidates.
func fallbackPythonInterpreter() (string, []string, bool) {
	if managed := managedPythonInterpreterPath(os.Getenv); managed != "" {
		return managed, nil, true
	}
	for _, candidate := range modernPythonCandidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil, true
		}
	}
	return "", nil, false
}

// readFirstLine returns the first line of path, trimmed. It reports false when
// the file cannot be opened or yields nothing.
func readFirstLine(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
}

// interpreterFromShebang splits a shebang body into interpreter and arguments,
// unwrapping "/usr/bin/env python3" style indirection.
func interpreterFromShebang(shebang string) (string, []string, bool) {
	fields := strings.Fields(shebang)
	if len(fields) == 0 {
		return "", nil, false
	}
	if strings.HasSuffix(fields[0], "/env") && len(fields) > 1 {
		return fields[1], fields[2:], true
	}
	return fields[0], fields[1:], true
}

// pythonFindingsLibrary is the shared, I/O-free half of the embedded Python
// findings scanner, prepended both to the standalone findings script and to
// the radon API fast-path script so one detection implementation serves both.
// Successful scans return findings, interfaces, and an imports object with
// total/used/unused/ddc metrics; AST failures return an error and no metrics.
const pythonFindingsLibrary = `
import ast


def _dotted(node):
    parts = []
    while isinstance(node, ast.Attribute):
        parts.append(node.attr)
        node = node.value
    if isinstance(node, ast.Name):
        parts.append(node.id)
    return ".".join(reversed(parts))


def _finding(rule, kind, node):
    return {
        "rule": rule,
        "kind": kind,
        "line": node.lineno,
        "detail": _dotted(node.func) + "()",
    }


def _temporal_purity(tree):
    found = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Attribute):
            continue
        if node.func.attr == "utcnow":
            found.append(_finding("temporal-purity", "py-datetime-utcnow", node))
        elif node.func.attr == "now" and not node.args and not node.keywords:
            found.append(_finding("temporal-purity", "py-datetime-now-naive", node))
    return found


# psycopg2's sql.SQL("... {x} ...").format(x=sql.Identifier(col)) is a SAFE
# composition API: it quotes identifiers instead of splicing text, and it is
# exactly what this rule's own remediation message tells callers to adopt.
# Only .format() on a receiver that is not a SQL composable is genuine string
# interpolation.
_SQL_COMPOSABLE_FACTORIES = ("sql.SQL", "psycopg2.sql.SQL", "SQL")


def _is_sql_composable(node):
    return isinstance(node, ast.Call) and _dotted(node.func) in _SQL_COMPOSABLE_FACTORIES


def _sql_composable_names(tree):
    names = set()
    for node in ast.walk(tree):
        if not isinstance(node, ast.Assign) or not _is_sql_composable(node.value):
            continue
        for target in node.targets:
            if isinstance(target, ast.Name):
                names.add(target.id)
    return names


def _sql_composition_receiver_is_safe(receiver, composable_names):
    if _is_sql_composable(receiver):
        return True
    return isinstance(receiver, ast.Name) and receiver.id in composable_names


def _sql_composition_kind(call, composable_names):
    first = call.args[0] if call.args else None
    if isinstance(first, ast.JoinedStr):
        return "py-fstring-execute"
    if isinstance(first, ast.BinOp) and isinstance(first.op, ast.Mod):
        return "py-percent-format-execute"
    if (
        isinstance(first, ast.Call)
        and isinstance(first.func, ast.Attribute)
        and first.func.attr == "format"
    ):
        if _sql_composition_receiver_is_safe(first.func.value, composable_names):
            return None
        return "py-str-format-execute"
    return None


def _sql_composition(tree):
    found = []
    composable_names = _sql_composable_names(tree)
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Attribute):
            continue
        if node.func.attr not in ("execute", "executemany"):
            continue
        kind = _sql_composition_kind(node, composable_names)
        if kind is not None:
            found.append(_finding("sql-composition-safety", kind, node))
    return found


_SCANS = (_temporal_purity, _sql_composition)


class _InterfaceVisitor(ast.NodeVisitor):
    def __init__(self):
        self.module = {
            "name": "",
            "kind": "module",
            "line": 1,
            "public_methods": 0,
            "_operations": set(),
        }
        self.interfaces = [self.module]
        self.path = []
        self.class_stack = []
        self.function_depth = 0

    def _current_interface(self):
        if self.class_stack:
            interface = self.class_stack[-1]
            if self.function_depth == interface["_function_depth"]:
                return interface
            return None
        if self.function_depth == 0:
            return self.module
        return None

    def _add_operation(self, name):
        if not name.startswith("_"):
            interface = self._current_interface()
            if interface is not None:
                interface["_operations"].add(name)

    def _visit_function(self, node):
        self._add_operation(node.name)
        self.path.extend((node.name, "<locals>"))
        self.function_depth += 1
        self.generic_visit(node)
        self.function_depth -= 1
        del self.path[-2:]

    def visit_FunctionDef(self, node):
        self._visit_function(node)

    def visit_AsyncFunctionDef(self, node):
        self._visit_function(node)

    def visit_ClassDef(self, node):
        interface = {
            "name": ".".join(self.path + [node.name]),
            "kind": "class",
            "line": node.lineno,
            "public_methods": 0,
            "_operations": set(),
            "_function_depth": self.function_depth,
        }
        self.interfaces.append(interface)
        self.path.append(node.name)
        self.class_stack.append(interface)
        self.generic_visit(node)
        self.class_stack.pop()
        self.path.pop()

    def finish(self):
        for interface in self.interfaces:
            interface["public_methods"] = len(interface.pop("_operations"))
            interface.pop("_function_depth", None)
        return self.interfaces


class _ImportRecord:
    def __init__(self, name, scope, lineno, col, index):
        self.name = name
        self.scope = scope
        self.pos = (lineno, col, index)
        self.used = False


class _Scope:
    def __init__(self, kind, parent):
        self.kind = kind
        self.parent = parent
        self.locals = set()
        self.global_names = set()
        self.nonlocal_names = set()
        self.node = None


_REMOVED = object()
def _target_names(node):
    if isinstance(node, ast.Name):
        return [node.id]
    if isinstance(node, (ast.Tuple, ast.List)):
        result = []
        for elt in node.elts:
            result.extend(_target_names(elt))
        return result
    if isinstance(node, ast.Starred):
        return _target_names(node.value)
    return []


class _ImportCollector(ast.NodeVisitor):
    def __init__(self, tree):
        super().__init__()
        self.module = _Scope("module", None)
        self.module.node = tree
        self.scope = self.module
        self.scope_by_node = {}
        self.records = []
        self.record_by_alias = {}
        self._walk_statements(tree.body, self.module)

    def _target_scope(self, scope, name):
        if name in scope.global_names:
            return self.module
        if name in scope.nonlocal_names:
            parent = scope.parent
            while parent is not None:
                if parent.kind in ("function", "lambda") and name in parent.locals:
                    return parent
                parent = parent.parent
            return scope
        return scope

    def _bind(self, scope, name):
        self._target_scope(scope, name).locals.add(name)

    def _walk_expr(self, node, scope):
        if node is None:
            return
        old = self.scope
        self.scope = scope
        self.visit(node)
        self.scope = old

    def _walk_statements(self, nodes, scope):
        old = self.scope
        self.scope = scope
        for node in nodes:
            self.visit(node)
        self.scope = old

    def generic_visit(self, node):
        if node is None:
            return
        scope = self.scope
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            self._bind(scope, node.name)
            self._walk_expr_list(node.decorator_list, scope)
            self._walk_arguments(node.args, scope)
            self._walk_expr(node.returns, scope)
            self._walk_type_params(node, scope)
            child = _Scope("function", scope)
            child.node = node
            self.scope_by_node[node] = child
            for arg in list(node.args.posonlyargs) + list(node.args.args) + list(node.args.kwonlyargs):
                child.locals.add(arg.arg)
            if node.args.vararg:
                child.locals.add(node.args.vararg.arg)
            if node.args.kwarg:
                child.locals.add(node.args.kwarg.arg)
            self._walk_statements(node.body, child)
            return
        if isinstance(node, ast.Lambda):
            self._walk_arguments(node.args, scope)
            child = _Scope("lambda", scope)
            child.node = node
            self.scope_by_node[node] = child
            for arg in list(node.args.posonlyargs) + list(node.args.args) + list(node.args.kwonlyargs):
                child.locals.add(arg.arg)
            if node.args.vararg:
                child.locals.add(node.args.vararg.arg)
            if node.args.kwarg:
                child.locals.add(node.args.kwarg.arg)
            self._walk_expr(node.body, child)
            return
        if isinstance(node, ast.ClassDef):
            self._bind(scope, node.name)
            self._walk_expr_list(node.decorator_list, scope)
            self._walk_expr_list(node.bases, scope)
            for keyword in node.keywords:
                self._walk_expr(keyword.value, scope)
            self._walk_type_params(node, scope)
            child = _Scope("class", scope)
            child.node = node
            self.scope_by_node[node] = child
            self._walk_statements(node.body, child)
            return
        if isinstance(node, (ast.ListComp, ast.SetComp, ast.GeneratorExp, ast.DictComp)):
            generators = node.generators
            if generators:
                self._walk_expr(generators[0].iter, scope)
            child = _Scope("comprehension", scope)
            child.node = node
            self.scope_by_node[node] = child
            for generator in generators:
                if generator is not generators[0]:
                    self._walk_expr(generator.iter, child)
                self._walk_expr(generator.target, child)
                for name in _target_names(generator.target):
                    child.locals.add(name)
                self._walk_expr_list(generator.ifs, child)
            if isinstance(node, ast.DictComp):
                self._walk_expr(node.key, child)
                self._walk_expr(node.value, child)
            else:
                self._walk_expr(node.elt, child)
            return
        if node.__class__.__name__ == "TypeAlias":
            if isinstance(node.name, ast.Name):
                self._bind(scope, node.name.id)
            self._walk_type_params(node, scope)
            self._walk_expr(node.value, scope)
            return
        if isinstance(node, ast.Import):
            for index, alias in enumerate(node.names):
                name = alias.asname or alias.name.split(".")[0]
                self._bind(scope, name)
                record = _ImportRecord(name, self._target_scope(scope, name),
                                       node.lineno, node.col_offset, index)
                self.record_by_alias[(node, index)] = len(self.records)
                self.records.append(record)
                if scope is self.module and alias.asname == alias.name:
                    record.used = True
            return
        if isinstance(node, ast.ImportFrom):
            if node.module == "__future__":
                return
            for index, alias in enumerate(node.names):
                if alias.name == "*":
                    continue
                name = alias.asname or alias.name
                self._bind(scope, name)
                record = _ImportRecord(name, self._target_scope(scope, name),
                                       node.lineno, node.col_offset, index)
                self.record_by_alias[(node, index)] = len(self.records)
                self.records.append(record)
                if scope is self.module and alias.asname == alias.name:
                    record.used = True
            return
        if isinstance(node, (ast.Global, ast.Nonlocal)):
            target = scope.global_names if isinstance(node, ast.Global) else scope.nonlocal_names
            target.update(node.names)
            return
        if isinstance(node, ast.Assign):
            self._walk_expr(node.value, scope)
            for target in node.targets:
                for name in _target_names(target):
                    self._bind(scope, name)
                self._walk_expr(target, scope)
            return
        if isinstance(node, ast.AnnAssign):
            self._walk_expr(node.annotation, scope)
            self._walk_expr(node.value, scope)
            if node.value is not None or scope.kind not in ("module", "class"):
                for name in _target_names(node.target):
                    self._bind(scope, name)
            self._walk_expr(node.target, scope)
            return
        if isinstance(node, ast.Delete):
            for target in node.targets:
                for name in _target_names(target):
                    self._bind(scope, name)
            return
        if isinstance(node, ast.AugAssign):
            self._walk_expr(node.target, scope)
            self._walk_expr(node.value, scope)
            for name in _target_names(node.target):
                self._bind(scope, name)
            return
        if isinstance(node, (ast.For, ast.AsyncFor)):
            self._walk_expr(node.iter, scope)
            for name in _target_names(node.target):
                self._bind(scope, name)
            self._walk_expr(node.target, scope)
            self._walk_statements(node.body, scope)
            self._walk_statements(node.orelse, scope)
            return
        if isinstance(node, (ast.With, ast.AsyncWith)):
            for item in node.items:
                self._walk_expr(item.context_expr, scope)
                if item.optional_vars:
                    for name in _target_names(item.optional_vars):
                        self._bind(scope, name)
                    self._walk_expr(item.optional_vars, scope)
            self._walk_statements(node.body, scope)
            return
        if isinstance(node, ast.ExceptHandler):
            self._walk_expr(node.type, scope)
            if node.name:
                self._bind(scope, node.name)
            self._walk_statements(node.body, scope)
            return
        if isinstance(node, ast.NamedExpr):
            self._walk_expr(node.value, scope)
            target_scope = scope
            while target_scope.kind == "comprehension":
                target_scope = target_scope.parent
            if isinstance(node.target, ast.Name):
                self._bind(target_scope, node.target.id)
            return
        if isinstance(node, ast.Match):
            self._walk_expr(node.subject, scope)
            for case in node.cases:
                self._walk_pattern(case.pattern, scope)
                self._walk_expr(case.guard, scope)
                self._walk_statements(case.body, scope)
            return
        if node.__class__.__name__ == "TypeAlias":
            target = getattr(node, "name", None)
            if isinstance(target, ast.Name):
                self._bind(scope, target.id)
            self._walk_type_params(node, scope)
            self._walk_expr(getattr(node, "value", None), scope)
            return
        for child in ast.iter_child_nodes(node):
            self.visit(child)

    def _walk_pattern(self, node, scope):
        if isinstance(node, (ast.MatchAs, ast.MatchStar)) and node.name:
            self._bind(scope, node.name)
        if isinstance(node, ast.MatchMapping) and node.rest:
            self._bind(scope, node.rest)
        for child in ast.iter_child_nodes(node):
            self._walk_pattern(child, scope)

    def _walk_expr_list(self, nodes, scope):
        for node in nodes:
            self._walk_expr(node, scope)

    def _walk_arguments(self, args, scope):
        self._walk_expr_list(args.defaults, scope)
        self._walk_expr_list([item for item in args.kw_defaults if item], scope)
        for arg in list(args.posonlyargs) + list(args.args) + list(args.kwonlyargs):
            self._walk_expr(arg.annotation, scope)
        self._walk_expr(args.vararg.annotation if args.vararg else None, scope)
        self._walk_expr(args.kwarg.annotation if args.kwarg else None, scope)

    def _walk_type_params(self, node, scope):
        for param in getattr(node, "type_params", ()):
            for child in ast.iter_child_nodes(param):
                self._walk_expr(child, scope)


def _import_candidates(value):
    return value if isinstance(value, set) else {value}


def _merge_import_states(left, right):
    return {
        name: _import_candidates(left.get(name, _REMOVED)) |
              _import_candidates(right.get(name, _REMOVED))
        for name in left.keys() | right.keys()
    }


class _ImportResolver:
    def __init__(self, collector):
        self.collector = collector
        self.records = collector.records
        self.states = {}
        self.tasks = []

    def _target_scope(self, scope, name):
        if name in scope.global_names:
            return self.collector.module
        if name in scope.nonlocal_names:
            parent = scope.parent
            while parent is not None:
                if parent.kind in ("function", "lambda") and name in parent.locals:
                    return parent
                parent = parent.parent
            return scope
        return scope

    def _initial(self, scope):
        if scope.kind in ("function", "lambda", "comprehension"):
            return {name: None for name in scope.locals}
        return {}

    def _lookup(self, scope, name):
        current = self._target_scope(scope, name)
        closure = scope.kind in ("class", "function", "lambda", "comprehension")
        candidates = set()
        while current is not None:
            state = self.states.get(current, {})
            if name in state:
                values = _import_candidates(state[name])
                candidates.update(values - {_REMOVED})
                if current.kind != "class" or _REMOVED not in values:
                    return candidates
            if name in current.locals and current.kind in ("function", "lambda", "annotation"):
                return candidates
            current = current.parent
            if closure:
                while current is not None and current.kind == "class":
                    current = current.parent
        return candidates

    def _load(self, scope, name):
        for index in self._lookup(scope, name):
            if index is not None:
                self.records[index].used = True

    def _write(self, scope, name, value):
        target = self._target_scope(scope, name)
        state = self.states.setdefault(target, self._initial(target))
        state[name] = value

    def _imports(self, node, index):
        record = self.collector.record_by_alias.get((node, index))
        return set() if record is None else {record}

    def _snapshot(self):
        return {scope: dict(state) for scope, state in self.states.items()}

    def _restore(self, flow):
        self.states = {scope: dict(state) for scope, state in flow.items()}

    def _merge_flows(self, flows):
        scopes = set().union(*(flow.keys() for flow in flows))
        merged = {}
        for scope in scopes:
            states = [flow.get(scope, {}) for flow in flows]
            state = states[0]
            for other in states[1:]:
                state = _merge_import_states(state, other)
            merged[scope] = state
        self.states = merged

    def _expr(self, node, scope, annotation=False):
        if node is None:
            return
        if annotation and isinstance(node, ast.Constant) and isinstance(node.value, str):
            try:
                self._expr(ast.parse(node.value, mode="eval").body, scope)
            except SyntaxError:
                pass
            return
        if isinstance(node, ast.Lambda):
            if node not in self.collector.scope_by_node:
                self.collector._walk_expr(node, scope)
            child = self.collector.scope_by_node[node]
            child.parent = scope
            self._arguments(node.args, scope)
            self.tasks.append((node, child, scope))
            return
        if isinstance(node, ast.Name):
            if isinstance(node.ctx, ast.Load):
                self._load(scope, node.id)
            return
        if isinstance(node, ast.NamedExpr):
            self._expr(node.value, scope)
            if isinstance(node.target, ast.Name):
                target_scope = scope
                while target_scope.kind == "comprehension":
                    target_scope = target_scope.parent
                self._write(target_scope, node.target.id, None)
            return
        if isinstance(node, (ast.ListComp, ast.SetComp, ast.GeneratorExp, ast.DictComp)):
            self._comprehension(node, scope)
            return
        for child in ast.iter_child_nodes(node):
            self._expr(child, scope)

    def _annotation(self, node, scope):
        self._expr(node, scope, annotation=True)

    def _comprehension(self, node, scope):
        generators = node.generators
        if not generators:
            return
        self._expr(generators[0].iter, scope)
        if node not in self.collector.scope_by_node:
            self.collector._walk_expr(node, scope)
        child = self.collector.scope_by_node[node]
        child.parent = scope
        state = self._initial(child)
        self.states[child] = state
        for generator in generators:
            if generator is not generators[0]:
                self._expr(generator.iter, child)
            self._target(child, generator.target)
            for condition in generator.ifs:
                self._expr(condition, child)
        if isinstance(node, ast.DictComp):
            self._expr(node.key, child)
            self._expr(node.value, child)
        else:
            self._expr(node.elt, child)

    def _target(self, scope, node):
        if isinstance(node, ast.Name):
            self._write(scope, node.id, None)
        elif isinstance(node, (ast.Attribute, ast.Subscript)):
            self._expr(node.value, scope)
            if isinstance(node, ast.Subscript):
                self._expr(node.slice, scope)
        elif isinstance(node, (ast.Tuple, ast.List)):
            for child in node.elts:
                self._target(scope, child)
        elif isinstance(node, ast.Starred):
            self._target(scope, node.value)

    def _literal_exports(self, node, scope):
        if scope is not self.collector.module:
            return
        if not isinstance(node, (ast.List, ast.Tuple)):
            return
        if not all(isinstance(item, ast.Constant) and isinstance(item.value, str) for item in node.elts):
            return
        for item in node.elts:
            self._load(scope, item.value)

    def _stmt_list(self, nodes, scope, state):
        self.states[scope] = state
        for node in nodes:
            state = self._stmt(node, scope, state)
            self.states[scope] = state
        return state

    def _stmt(self, node, scope, state):
        if isinstance(node, (ast.Import, ast.ImportFrom)):
            if isinstance(node, ast.ImportFrom) and node.module == "__future__":
                return state
            for index, alias in enumerate(node.names):
                if alias.name == "*":
                    continue
                name = alias.asname or (alias.name if isinstance(node, ast.ImportFrom) else alias.name.split(".")[0])
                ids = self._imports(node, index)
                self._write(scope, name, ids)
            return self.states.get(scope, state)
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            for decorator in node.decorator_list:
                self._expr(decorator, scope)
            type_scope = self._type_scope(node, scope)
            self._arguments(node.args, scope, type_scope)
            self._annotation(node.returns, type_scope)
            if getattr(node, "type_params", ()):
                self.collector.scope_by_node[node].parent = type_scope
            self._write(scope, node.name, None)
            self.tasks.append((node, self.collector.scope_by_node[node], scope))
            return self.states.get(scope, state)
        if isinstance(node, ast.ClassDef):
            for decorator in node.decorator_list:
                self._expr(decorator, scope)
            type_scope = self._type_scope(node, scope)
            for base in node.bases:
                self._expr(base, type_scope)
            for keyword in node.keywords:
                self._expr(keyword.value, type_scope)
            child = self.collector.scope_by_node[node]
            if getattr(node, "type_params", ()):
                child.parent = type_scope
            self._stmt_list(node.body, child, self._initial(child))
            self._write(scope, node.name, None)
            return self.states.get(scope, state)
        if node.__class__.__name__ == "TypeAlias":
            type_scope = self._type_scope(node, scope)
            self._expr(getattr(node, "value", None), type_scope)
            target = getattr(node, "name", None)
            if isinstance(target, ast.Name):
                self._write(scope, target.id, None)
            return self.states.get(scope, state)
        if isinstance(node, ast.Assign):
            self._expr(node.value, scope)
            if any(isinstance(target, ast.Name) and target.id == "__all__" for target in node.targets):
                self._literal_exports(node.value, scope)
            for target in node.targets:
                self._target(scope, target)
            return self.states.get(scope, state)
        if isinstance(node, ast.AnnAssign):
            self._annotation(node.annotation, scope)
            if node.value is not None:
                self._expr(node.value, scope)
                if isinstance(node.target, ast.Name) and node.target.id == "__all__":
                    self._literal_exports(node.value, scope)
                self._target(scope, node.target)
            return self.states.get(scope, state)
        if isinstance(node, ast.AugAssign):
            if isinstance(node.target, ast.Name):
                self._load(scope, node.target.id)
            else:
                self._expr(node.target, scope)
            self._expr(node.value, scope)
            if isinstance(node.target, ast.Name) and node.target.id == "__all__" and isinstance(node.op, ast.Add):
                self._literal_exports(node.value, scope)
            self._target(scope, node.target)
            return self.states.get(scope, state)
        if isinstance(node, ast.Delete):
            for target in node.targets:
                self._expr(target, scope)
                for name in _target_names(target):
                    self._write(scope, name, _REMOVED)
            return self.states.get(scope, state)
        if isinstance(node, ast.If):
            self._expr(node.test, scope)
            incoming = self._snapshot()
            branches = []
            for arm in (node.body, node.orelse):
                self._restore(incoming)
                self._stmt_list(arm, scope, self.states[scope])
                branches.append(self._snapshot())
            self._merge_flows(branches)
            return self.states[scope]
        if isinstance(node, (ast.For, ast.AsyncFor, ast.While)):
            is_while = isinstance(node, ast.While)
            self._expr(node.test if is_while else node.iter, scope)
            incoming = self._snapshot()
            if not is_while:
                self._target(scope, node.target)
            self._stmt_list(node.body, scope, self.states[scope])
            body = self._snapshot()
            self._merge_flows((incoming, body))
            if is_while:
                self._expr(node.test, scope)
            self._stmt_list(node.orelse, scope, self.states[scope])
            self._merge_flows((body, self._snapshot()))
            return self.states[scope]
        if isinstance(node, (ast.Try, getattr(ast, "TryStar", ast.Try))):
            incoming = self._snapshot()
            self._stmt_list(node.body, scope, self.states[scope])
            body = self._snapshot()
            self._stmt_list(node.orelse, scope, self.states[scope])
            branches = [incoming, self._snapshot()]
            self._merge_flows((incoming, body))
            handler_incoming = self._snapshot()
            for handler in node.handlers:
                self._restore(handler_incoming)
                self._expr(handler.type, scope)
                if handler.name:
                    self._write(scope, handler.name, None)
                self._stmt_list(handler.body, scope, self.states[scope])
                if handler.name:
                    self._write(scope, handler.name, _REMOVED)
                branches.append(self._snapshot())
            self._merge_flows(branches)
            self._stmt_list(node.finalbody, scope, self.states[scope])
            return self.states[scope]
        if isinstance(node, (ast.With, ast.AsyncWith)):
            for item in node.items:
                self._expr(item.context_expr, scope)
                if item.optional_vars:
                    self._target(scope, item.optional_vars)
            return self._stmt_list(node.body, scope, state)
        if isinstance(node, ast.Match):
            self._expr(node.subject, scope)
            incoming = self._snapshot()
            branches = []
            exhaustive = False
            for case in node.cases:
                self._restore(incoming)
                self._pattern(case.pattern, scope)
                self._expr(case.guard, scope)
                self._stmt_list(case.body, scope, self.states[scope])
                branches.append(self._snapshot())
                if case.guard is None and isinstance(case.pattern, ast.MatchAs) and case.pattern.pattern is None:
                    exhaustive = True
            if not exhaustive:
                branches.append(incoming)
            self._merge_flows(branches)
            return self.states[scope]
        if isinstance(node, ast.Raise):
            self._expr(node.exc, scope)
            self._expr(node.cause, scope)
            return state
        if isinstance(node, (ast.Return, ast.Assert, ast.Expr, ast.Yield, ast.YieldFrom)):
            self._expr(node.value if hasattr(node, "value") else None, scope)
            if isinstance(node, ast.Assert):
                self._expr(node.test, scope)
                self._expr(node.msg, scope)
            return state
        self._expr(node, scope)
        return self.states.get(scope, state)

    def _pattern(self, node, scope):
        if isinstance(node, ast.MatchValue):
            self._expr(node.value, scope)
            return
        if isinstance(node, ast.MatchClass):
            self._expr(node.cls, scope)
            for child in node.patterns:
                self._pattern(child, scope)
            for child in node.kwd_patterns:
                self._pattern(child, scope)
            return
        if isinstance(node, ast.MatchMapping):
            for key in node.keys:
                self._expr(key, scope)
            for child in node.patterns:
                self._pattern(child, scope)
            if node.rest:
                self._write(scope, node.rest, None)
            return
        if isinstance(node, ast.MatchStar):
            if node.name:
                self._write(scope, node.name, None)
            return
        if isinstance(node, ast.MatchAs) and node.name:
            self._write(scope, node.name, None)
        for child in ast.iter_child_nodes(node):
            self._pattern(child, scope)

    def _arguments(self, args, scope, annotation_scope=None):
        for item in args.defaults:
            self._expr(item, scope)
        for item in args.kw_defaults:
            self._expr(item, scope)
        ann_scope = annotation_scope or scope
        for arg in list(args.posonlyargs) + list(args.args) + list(args.kwonlyargs):
            self._annotation(arg.annotation, ann_scope)
        self._annotation(args.vararg.annotation if args.vararg else None, ann_scope)
        self._annotation(args.kwarg.annotation if args.kwarg else None, ann_scope)

    def _type_scope(self, node, parent):
        params = getattr(node, "type_params", ())
        if not params:
            return parent
        result = _Scope("annotation", parent)
        self.states[result] = {}
        for param in params:
            name = getattr(param, "name", None)
            if isinstance(name, str):
                result.locals.add(name)
        for param in params:
            for child in ast.iter_child_nodes(param):
                self._expr(child, result)
        return result

    def _deferred_body(self, node, scope):
        saved = self._snapshot()
        state = self._initial(scope)
        self.states[scope] = state
        body = [node.body] if isinstance(node, ast.Lambda) else node.body
        self._stmt_list(body, scope, state)
        # Nested bodies see this completed environment, but their redirected
        # writes must not affect the next sibling body.
        nested, self.tasks = self.tasks, []
        for child, child_scope, _ in nested:
            self._deferred_body(child, child_scope)
        completed = self.states[scope]
        self._restore(saved)
        self.states[scope] = completed

    def run(self):
        self._stmt_list(self.collector.module.node.body, self.collector.module, {})
        bodies, self.tasks = self.tasks, []
        for node, scope, _ in bodies:
            self._deferred_body(node, scope)


def _import_metric(tree):
    collector = _ImportCollector(tree)
    collector.module.node = tree
    resolver = _ImportResolver(collector)
    resolver.run()
    records = sorted(collector.records, key=lambda record: record.pos)
    unused = [record.name for record in records if not record.used]
    total = len(records)
    used = total - len(unused)
    return {"total": total, "used": used, "unused": unused,
            "ddc": 1.0 if total == 0 else float(used) / total}


def _python_export_facade(tree):
    body = tree.body
    index = 0
    if (body and isinstance(body[0], ast.Expr)
            and isinstance(body[0].value, ast.Constant)
            and isinstance(body[0].value.value, str)):
        index += 1
    while (index < len(body) and isinstance(body[index], ast.ImportFrom)
           and body[index].level == 0 and body[index].module == "__future__"):
        if any(alias.asname is not None or alias.name == "*" for alias in body[index].names):
            return False
        index += 1
    bindings = set()
    import_count = 0
    while index < len(body) and isinstance(body[index], (ast.Import, ast.ImportFrom)):
        node = body[index]
        if isinstance(node, ast.ImportFrom) and node.level == 0 and node.module == "__future__":
            return False
        for alias in node.names:
            if alias.name == "*":
                return False
            binding = alias.asname or (alias.name.split(".")[0] if isinstance(node, ast.Import) else alias.name)
            if binding == "__all__" or binding in bindings:
                return False
            bindings.add(binding)
        import_count += 1
        index += 1
    if import_count == 0 or index != len(body) - 1:
        return False
    exports = body[index]
    if (not isinstance(exports, ast.Assign) or len(exports.targets) != 1
            or not isinstance(exports.targets[0], ast.Name)
            or exports.targets[0].id != "__all__"
            or not isinstance(exports.value, (ast.List, ast.Tuple))
            or not exports.value.elts):
        return False
    names = set()
    for item in exports.value.elts:
        if (not isinstance(item, ast.Constant) or not isinstance(item.value, str)
                or not item.value.isidentifier() or item.value in names):
            return False
        names.add(item.value)
    return bindings == names


def scan_source(source):
    try:
        tree = ast.parse(source)
    except SyntaxError as exc:
        lineno = exc.lineno if exc.lineno is not None else 1
        return {
            "findings": [{
                "rule": "syntax-warning",
                "kind": "syntax-warning",
                "line": lineno,
                "detail": f"Syntax error during AST parsing: {exc}",
            }],
            "interfaces": [],
            "error": f"AST parsing failed: {exc}",
        }
    except Exception as exc:
        return {
            "findings": [{
                "rule": "syntax-warning",
                "kind": "syntax-warning",
                "line": 1,
                "detail": f"Syntax error during AST parsing: {exc}",
            }],
            "interfaces": [],
            "error": f"AST parsing failed: {exc}",
        }
    found = []
    for scan in _SCANS:
        found.extend(scan(tree))
    found.sort(key=lambda item: (item["line"], item["kind"]))
    visitor = _InterfaceVisitor()
    visitor.visit(tree)
    imports = _import_metric(tree)
    payload = {"findings": found, "interfaces": visitor.finish(), "imports": imports}
    if _python_export_facade(tree):
        payload["source_kind"] = "python-export-facade"
    return payload

`

// pythonFindingsScript scans every path passed on the command line and prints
// one JSON entry per analyzed path, including findings, interfaces, imports,
// or an explicit AST error with unavailable metrics.
const pythonFindingsScript = pythonFindingsLibrary + `
import json
import pathlib
import sys

payload = {}
for path in sys.argv[1:]:
    try:
        payload[path] = scan_source(pathlib.Path(path).read_text(encoding="utf-8", errors="replace"))
    except Exception as exc:
        payload[path] = {
            "findings": [
                {
                    "rule": "syntax-warning",
                    "kind": "syntax-warning",
                    "line": 1,
                    "detail": f"AST scan failed: {exc}",
                }
            ],
            "error": f"AST scan failed: {exc}",
        }
json.dump(payload, sys.stdout)
`

// pythonFindingsItem is one analyzed path's findings-scan outcome.
type pythonFindingsItem struct {
	Error      string            `json:"error"`
	Findings   []Finding         `json:"findings"`
	Interfaces []InterfaceMetric `json:"interfaces"`
	Imports    *ImportMetric     `json:"imports"`
	SourceKind SourceKind        `json:"source_kind,omitempty"`
}

// pythonFileFindings runs the findings scan for a single file.
func pythonFileFindings(ctx context.Context, file, radonPath string) (pythonFindingsItem, error) {
	payload, err := pythonFindingsPayload(ctx, radonPath, file)
	if err != nil {
		return pythonFindingsItem{}, err
	}
	return pythonFindingsFor(payload, file)
}

// pythonFindingsPayload runs the embedded ast findings scan over files in one
// interpreter launch. Scan failures are never swallowed: an unresolvable or
// failing interpreter, and unparsable scanner output, surface as analyzer
// errors the caller routes through enforcement-on-error.
func pythonFindingsPayload(ctx context.Context, radonPath string, files ...string) (map[string]pythonFindingsItem, error) {
	python, args, ok := findingsPythonCommand(radonPath)
	if !ok {
		return nil, fmt.Errorf("resolving python interpreter for findings scan: %w", exec.ErrNotFound)
	}
	args = append(args, "-c", pythonFindingsScript)
	args = append(args, files...)
	output, err := runTool(ctx, python, args...)
	if err != nil {
		return nil, fmt.Errorf("running python findings scan: %w", err)
	}
	var payload map[string]pythonFindingsItem
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, fmt.Errorf("parsing python findings scan: %w: %s", err, trimOutput(output))
	}
	return payload, nil
}

// pythonFindingsFor returns one file's findings and scoped interfaces from a
// scan payload keyed by analyzed path. A single-entry payload retains the
// existing path-independent lookup for interpreter wrappers that rewrite paths.
func pythonFindingsFor(payload map[string]pythonFindingsItem, file string) (pythonFindingsItem, error) {
	item, ok := payload[file]
	if !ok && len(payload) == 1 {
		for _, value := range payload {
			item = value
			ok = true
		}
	}
	if !ok {
		return pythonFindingsItem{}, fmt.Errorf("python findings error for %s: missing interface metrics", file)
	}
	if item.Error != "" {
		return pythonFindingsItem{}, fmt.Errorf("python findings error for %s: %s", file, item.Error)
	}
	if detail := pythonSyntaxWarningDetail(item.Findings); detail != "" {
		return pythonFindingsItem{}, fmt.Errorf("python findings error for %s: %s", file, detail)
	}
	if len(item.Interfaces) == 0 {
		return pythonFindingsItem{}, fmt.Errorf("python findings error for %s: missing interface metrics", file)
	}
	if item.Imports == nil {
		return pythonFindingsItem{}, fmt.Errorf("python findings error for %s: missing import metrics", file)
	}
	return item, nil
}

func pythonSyntaxWarningDetail(findings []Finding) string {
	for _, finding := range findings {
		if finding.Rule != "syntax-warning" {
			continue
		}
		if finding.Detail == "" {
			return "AST scan unavailable"
		}
		return finding.Detail
	}
	return ""
}

// findingsPythonCommand resolves the interpreter that runs the findings scan.
// Managed runtime is preferred, followed by modern Python candidates (python3.13,
// python3.12, python3, python), and finally radon's shebang interpreter.
func findingsPythonCommand(radonPath string) (string, []string, bool) {
	if managed := managedPythonInterpreterPath(os.Getenv); managed != "" {
		return managed, nil, true
	}
	for _, candidate := range modernPythonCandidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil, true
		}
	}
	if radonPath == "" {
		radonPath = "radon"
	}
	if python, args, ok := radonPythonCommand(radonPath); ok && isPythonInterpreter(python) {
		return python, args, true
	}
	return "", nil, false
}

// isPythonInterpreter reports whether a shebang interpreter path names a
// Python runtime (e.g. "python3", "/opt/homebrew/bin/python3.12", "pypy3").
func isPythonInterpreter(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.Contains(name, "python") || strings.HasPrefix(name, "pypy")
}

func parseRadonCC(file string, output []byte) ([]FunctionMetric, error) {
	payload, err := parseRadonCCPayload(output)
	if err != nil {
		return nil, err
	}
	items := payload[file]
	if items == nil && len(payload) == 1 {
		for _, value := range payload {
			items = value
		}
	}
	return pythonFunctions(items), nil
}

// parseRadonCCPayload decodes radon's "cc -j" output into typed items per path.
func parseRadonCCPayload(output []byte) (map[string][]radonCCItem, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parsing radon cc: %w: %s", err, trimOutput(output))
	}
	return decodeRadonCCPayload(raw)
}

// decodeRadonCCPayload turns radon's per-path cc messages into typed items. The
// CLI and the radon API fast path share this decoding, so the two cannot report
// a radon analysis error differently.
func decodeRadonCCPayload(raw map[string]json.RawMessage) (map[string][]radonCCItem, error) {
	payload := make(map[string][]radonCCItem, len(raw))
	for file, message := range raw {
		items, err := decodeRadonCCItems(file, message)
		if err != nil {
			return nil, err
		}
		payload[file] = items
	}
	return payload, nil
}

// decodeRadonCCItems decodes one path's cc message. radon reports a file it
// could not parse as an object carrying an error, which is an analysis failure
// rather than an empty function list.
func decodeRadonCCItems(file string, message json.RawMessage) ([]radonCCItem, error) {
	var errorItem radonErrorItem
	if err := json.Unmarshal(message, &errorItem); err == nil && errorItem.Error != "" {
		return nil, fmt.Errorf("radon cc error for %s: %s", file, errorItem.Error)
	}
	var items []radonCCItem
	if err := json.Unmarshal(message, &items); err != nil {
		return nil, fmt.Errorf("parsing radon cc for %s: %w", file, err)
	}
	return items, nil
}

func pythonFunctions(items []radonCCItem) []FunctionMetric {
	functions := make([]FunctionMetric, 0, len(items))
	for _, item := range items {
		if item.Type != "F" && item.Type != "M" && item.Type != "function" && item.Type != "method" {
			continue
		}
		loc := 0
		if item.EndLine >= item.LineNo {
			loc = item.EndLine - item.LineNo + 1
		}
		functions = append(functions, FunctionMetric{
			Name:                 item.Name,
			CyclomaticComplexity: item.Complexity,
			IsPublic:             !strings.HasPrefix(item.Name, "_"),
			LOC:                  loc,
		})
	}
	return functions
}

func parseRadonRaw(file string, output []byte) (FileMetric, error) {
	var payload map[string]radonRawItem
	if err := json.Unmarshal(output, &payload); err != nil {
		return FileMetric{}, fmt.Errorf("parsing radon raw: %w: %s", err, trimOutput(output))
	}
	item, ok := payload[file]
	if !ok && len(payload) == 1 {
		for _, value := range payload {
			item = value
			ok = true
		}
	}
	if !ok {
		return FileMetric{}, fmt.Errorf("radon raw did not include %s", file)
	}
	if item.Error != "" {
		return FileMetric{}, fmt.Errorf("radon raw error for %s: %s", file, item.Error)
	}
	return FileMetric{TotalLOC: item.LOC, LogicLOC: item.LLOC}, nil
}

// AnalyzePythonRepository analyzes all Python files under root with one radon pass per metric.
func AnalyzePythonRepository(ctx context.Context, root, radonPath string) ([]AnalysisResult, error) {
	if radonPath == "" {
		radonPath = "radon"
	}
	files, err := discoverFiles(root, "python")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Python files found under %s", root)
	}
	ccPayload, rawPayload, err := pythonRepositoryPayloads(ctx, radonPath, files)
	if err != nil {
		return nil, err
	}
	analyzed := sortedRadonRawFiles(rawPayload)
	findingsPayload, err := pythonFindingsPayload(ctx, radonPath, analyzed...)
	if err != nil {
		return nil, err
	}
	results, err := pythonRepositoryResults(analyzed, ccPayload, rawPayload, findingsPayload)
	if err != nil {
		return nil, err
	}
	return AggregateModuleMetrics(results), nil
}

// pythonRepositoryPayloads runs radon's cc and raw batches over every
// discovered file — one subprocess per metric for the whole repository — and
// returns both decoded payloads keyed by the paths radon reported.
func pythonRepositoryPayloads(
	ctx context.Context,
	radonPath string,
	files []string,
) (map[string][]radonCCItem, map[string]radonRawItem, error) {
	ccOutput, err := runTool(ctx, radonPath, append([]string{"cc", "-j"}, files...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("running radon cc: %w", err)
	}
	rawOutput, err := runTool(ctx, radonPath, append([]string{"raw", "-j"}, files...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("running radon raw: %w", err)
	}
	ccPayload, err := parseRadonCCPayload(ccOutput)
	if err != nil {
		return nil, nil, err
	}
	var rawPayload map[string]radonRawItem
	if err := json.Unmarshal(rawOutput, &rawPayload); err != nil {
		return nil, nil, fmt.Errorf("parsing radon raw: %w: %s", err, trimOutput(rawOutput))
	}
	return ccPayload, rawPayload, nil
}

// sortedRadonRawFiles returns the paths radon actually analyzed, sorted so a
// repository scan's results are deterministic rather than map-ordered.
func sortedRadonRawFiles(rawPayload map[string]radonRawItem) []string {
	files := make([]string, 0, len(rawPayload))
	for file := range rawPayload {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

// pythonRepositoryResults assembles one result per analyzed path, failing the
// whole scan on the first file radon or the findings scan could not handle.
func pythonRepositoryResults(
	files []string,
	ccPayload map[string][]radonCCItem,
	rawPayload map[string]radonRawItem,
	findingsPayload map[string]pythonFindingsItem,
) ([]AnalysisResult, error) {
	results := make([]AnalysisResult, 0, len(files))
	for _, file := range files {
		result, err := pythonRepositoryResult(file, ccPayload[file], rawPayload[file], findingsPayload)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// pythonRepositoryResult builds one file's result from a repository-wide radon
// batch. A radon raw analysis error for the file is reported before anything
// else, so a file radon could not parse fails with radon's own message.
func pythonRepositoryResult(
	file string,
	items []radonCCItem,
	rawItem radonRawItem,
	findingsPayload map[string]pythonFindingsItem,
) (AnalysisResult, error) {
	if rawItem.Error != "" {
		return AnalysisResult{}, fmt.Errorf("radon raw error for %s: %s", file, rawItem.Error)
	}
	scan, err := pythonFindingsFor(findingsPayload, file)
	if err != nil {
		return AnalysisResult{}, err
	}
	fileMetric := FileMetric{TotalLOC: rawItem.LOC, LogicLOC: rawItem.LLOC}
	return pythonAnalysisResult(file, pythonFunctions(items), fileMetric, scan)
}

func publicFunctionCount(functions []FunctionMetric) int {
	count := 0
	for _, fn := range functions {
		if fn.IsPublic {
			count++
		}
	}
	return count
}

// pythonAnalysisResult assembles one analyzed Python file's result from the
// shared scanner payload. Import metrics are required even for empty scans.
func pythonAnalysisResult(file string, functions []FunctionMetric, fileMetric FileMetric, scan pythonFindingsItem) (AnalysisResult, error) {
	if scan.Imports == nil {
		return AnalysisResult{}, fmt.Errorf("python findings error for %s: missing import metrics", file)
	}
	fileMetric.LDR = ratio(fileMetric.LogicLOC, fileMetric.TotalLOC)
	fileMetric.PublicMethods = publicFunctionCount(functions)
	return AnalysisResult{
		CALMNode:     strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
		Language:     "python",
		File:         file,
		SourceKind:   scan.SourceKind,
		Functions:    functions,
		ModuleMetric: BuildModuleMetric(fileMetric, functions),
		FileMetric:   fileMetric,
		Imports:      *scan.Imports,
		Interfaces:   scan.Interfaces,
		Findings:     scan.Findings,
	}, nil
}

func runTool(ctx context.Context, name string, args ...string) ([]byte, error) {
	stdout, _, err := runToolOutput(ctx, name, args...)
	return stdout, err
}

func runToolOutput(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		detail := strings.TrimSpace(firstNonEmpty(stderr.String(), stdout.String()))
		if runCtx.Err() != nil {
			return nil, stderr.String(), errors.Join(runCtx.Err(), fmt.Errorf("%w: %s", err, detail))
		}
		return nil, stderr.String(), fmt.Errorf("%w: %s", err, detail)
	}
	return stdout.Bytes(), stderr.String(), nil
}

func trimOutput(output []byte) string {
	const limit = 512
	text := strings.TrimSpace(string(output))
	if len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
