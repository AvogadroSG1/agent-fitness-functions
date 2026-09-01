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
	"sort"
	"strings"
	"time"
	"unicode"

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
	functions, err := parseRadonCC(file, ccOutput)
	if err != nil {
		return nil, FileMetric{}, err
	}
	fileMetric, err := parseRadonRaw(file, rawOutput)
	if err != nil {
		return nil, FileMetric{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, FileMetric{}, err
	}
	return functions, fileMetric, nil
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

type radonAPIOutput struct {
	CC  map[string]json.RawMessage `json:"cc"`
	Raw map[string]radonRawItem    `json:"raw"`
	// Findings carries the ast findings scan that shares this subprocess, so
	// the fast path costs one interpreter launch for metrics and findings
	// together. A radon python stand-in that omits the section contributes no
	// findings.
	Findings map[string]pythonFindingsItem `json:"findings"`
}

const radonAPIScript = pythonFindingsLibrary + `
import json
import pathlib
import sys

from radon.complexity import cc_visit
from radon.raw import analyze

path = sys.argv[1]
source = pathlib.Path(path).read_text()
payload = {"cc": {}, "raw": {}, "findings": {}}
try:
    payload["cc"][path] = [
        {
            "type": getattr(item, "letter", ""),
            "name": item.name,
            "complexity": item.complexity,
            "lineno": item.lineno,
            "endline": item.endline,
        }
        for item in cc_visit(source)
    ]
except Exception as exc:
    payload["cc"][path] = {"error": str(exc)}
try:
    raw = analyze(source)
    payload["raw"][path] = {"loc": raw.loc, "lloc": raw.lloc}
except Exception as exc:
    payload["raw"][path] = {"error": str(exc)}
try:
    payload["findings"][path] = {"findings": scan_findings(source)}
except Exception as exc:
    payload["findings"][path] = {"error": str(exc)}
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
	findings, err := pythonFindingsFor(payload.Findings, file)
	if err != nil {
		return AnalysisResult{}, err
	}
	return pythonAnalysisResult(file, pythonFunctions(ccPayload[file]), fileMetric, findings)
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
	path, err := exec.LookPath(radonPath)
	if err != nil {
		return "", nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, false
	}
	defer func() { _ = file.Close() }()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && line == "" {
		return "", nil, false
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#!") {
		return "", nil, false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
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
// Every scan returns a list of {"rule", "kind", "line", "detail"} objects, so
// a new generalized fitness function is added by appending its scan to _SCANS
// without reshaping the output.
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


def _sql_composition_kind(call):
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
        return "py-str-format-execute"
    return None


def _sql_composition(tree):
    found = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Attribute):
            continue
        if node.func.attr not in ("execute", "executemany"):
            continue
        kind = _sql_composition_kind(node)
        if kind is not None:
            found.append(_finding("sql-composition-safety", kind, node))
    return found


_SCANS = (_temporal_purity, _sql_composition)


def scan_findings(source):
    tree = ast.parse(source)
    found = []
    for scan in _SCANS:
        found.extend(scan(tree))
    found.sort(key=lambda item: (item["line"], item["kind"]))
    return found
`

// pythonFindingsScript scans every path passed on the command line and prints
// one JSON entry per analyzed path, mirroring radon's payload shape:
// {"<path>": {"findings": [...]}} for a scanned file, {"<path>": {"error":
// "..."}} for one that could not be read or parsed.
const pythonFindingsScript = pythonFindingsLibrary + `
import json
import pathlib
import sys

payload = {}
for path in sys.argv[1:]:
    try:
        payload[path] = {"findings": scan_findings(pathlib.Path(path).read_text())}
    except Exception as exc:
        payload[path] = {"error": str(exc)}
json.dump(payload, sys.stdout)
`

// pythonFindingsItem is one analyzed path's findings-scan outcome.
type pythonFindingsItem struct {
	Error    string    `json:"error"`
	Findings []Finding `json:"findings"`
}

// pythonFileFindings runs the findings scan for a single file.
func pythonFileFindings(ctx context.Context, file, radonPath string) ([]Finding, error) {
	payload, err := pythonFindingsPayload(ctx, radonPath, file)
	if err != nil {
		return nil, err
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

// pythonFindingsFor returns one file's findings from a scan payload keyed by
// analyzed path. An entry carrying an error means that file could not be read
// or parsed, which is an analysis error rather than an empty result — the same
// way a radon analysis error surfaces. A payload with no entry for the file
// contributes no findings.
func pythonFindingsFor(payload map[string]pythonFindingsItem, file string) ([]Finding, error) {
	item, ok := payload[file]
	if !ok && len(payload) == 1 {
		for _, value := range payload {
			item = value
		}
	}
	if item.Error != "" {
		return nil, fmt.Errorf("python findings error for %s: %s", file, item.Error)
	}
	return item.Findings, nil
}

// findingsPythonCommand resolves the interpreter that runs the findings scan.
// radon's own launcher shebang is preferred so findings and metrics come from
// the same Python, but only when that shebang names a Python: a shell-script
// radon wrapper cannot run the scan. A PATH python3/python is the fallback.
func findingsPythonCommand(radonPath string) (string, []string, bool) {
	if radonPath == "" {
		radonPath = "radon"
	}
	if python, args, ok := radonPythonCommand(radonPath); ok && isPythonInterpreter(python) {
		return python, args, true
	}
	for _, candidate := range []string{"python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil, true
		}
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
	findings, err := pythonFindingsFor(findingsPayload, file)
	if err != nil {
		return AnalysisResult{}, err
	}
	fileMetric := FileMetric{TotalLOC: rawItem.LOC, LogicLOC: rawItem.LLOC}
	return pythonAnalysisResult(file, pythonFunctions(items), fileMetric, findings)
}

// pythonAnalysisResult assembles one analyzed Python file's result: it derives
// the ratios the radon metrics do not carry, reads the source for import
// metrics, and attaches the findings the caller scanned. Every Python analysis
// path — radon CLI, radon API fast path, and repository batch — ends here, so
// they cannot drift apart.
func pythonAnalysisResult(file string, functions []FunctionMetric, fileMetric FileMetric, findings []Finding) (AnalysisResult, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return AnalysisResult{}, err
	}
	fileMetric.LDR = ratio(fileMetric.LogicLOC, fileMetric.TotalLOC)
	fileMetric.PublicMethods = publicFunctionCount(functions)
	return AnalysisResult{
		CALMNode:     strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
		Language:     "python",
		File:         file,
		Functions:    functions,
		ModuleMetric: BuildModuleMetric(fileMetric, functions),
		FileMetric:   fileMetric,
		Imports:      pythonImportMetric(string(source)),
		Findings:     findings,
	}, nil
}

func pythonImportMetric(source string) ImportMetric {
	names := pythonImportNames(source)
	identifiers := pythonUsedIdentifiers(source)
	used := 0
	unused := make([]string, 0)
	for _, name := range names {
		if _, ok := identifiers[name]; ok {
			used++
			continue
		}
		unused = append(unused, name)
	}
	return ImportMetric{Total: len(names), Used: used, Unused: unused, DDC: ratio(used, len(names))}
}

// pythonUsedIdentifiers returns every identifier the module body reads. Locally
// bound names — assignments, parameters, loop variables, aliases — are excluded
// scope by scope, so only names that could be import uses remain.
func pythonUsedIdentifiers(source string) map[string]struct{} {
	body := stripPythonStringsAndComments(pythonImportBody(source))
	used := make(map[string]struct{})
	scopes := []pythonScope{{indent: -1, bound: make(map[string]struct{})}}
	for _, line := range pythonTokenLines(body) {
		scopes = popPythonScopes(scopes, line.indent)
		bindings := pythonLineBindings(line.tokens)
		collectPythonUses(used, line.tokens, bindings, pythonShadowedNames(scopes, bindings))
		scopes = applyPythonBindings(scopes, line, bindings)
	}
	return used
}

// popPythonScopes drops every scope a line's indentation has closed.
func popPythonScopes(scopes []pythonScope, indent int) []pythonScope {
	for len(scopes) > 1 && indent <= scopes[len(scopes)-1].indent {
		scopes = scopes[:len(scopes)-1]
	}
	return scopes
}

// pythonShadowedNames is the set of names a line's identifier uses must ignore:
// everything bound by an enclosing scope plus everything this line binds.
func pythonShadowedNames(scopes []pythonScope, bindings pythonBindings) map[string]struct{} {
	shadowed := pythonVisibleBindings(scopes, bindings.lineBound)
	for name := range bindings.outerBound {
		shadowed[name] = struct{}{}
	}
	return shadowed
}

// collectPythonUses records the identifiers a line reads rather than binds.
func collectPythonUses(
	used map[string]struct{},
	tokens []string,
	bindings pythonBindings,
	shadowed map[string]struct{},
) {
	for index, token := range tokens {
		if !isPythonIdentifierToken(token) ||
			pythonKeywords[token] ||
			bindings.positions[index] {
			continue
		}
		if _, ok := shadowed[token]; ok {
			continue
		}
		used[token] = struct{}{}
	}
}

// applyPythonBindings records a line's bindings in the innermost scope and
// opens the scope a def or class line starts.
func applyPythonBindings(scopes []pythonScope, line pythonTokenLine, bindings pythonBindings) []pythonScope {
	bound := scopes[len(scopes)-1].bound
	for name := range bindings.outerBound {
		bound[name] = struct{}{}
	}
	for name := range bindings.lineBound {
		bound[name] = struct{}{}
	}
	if bindings.startsScope {
		scopes = append(scopes, pythonScope{indent: line.indent, bound: bindings.nextScopeBound})
	}
	return scopes
}

type pythonScope struct {
	indent int
	bound  map[string]struct{}
}

type pythonTokenLine struct {
	indent int
	tokens []string
}

type pythonBindings struct {
	positions      map[int]bool
	lineBound      map[string]struct{}
	outerBound     map[string]struct{}
	nextScopeBound map[string]struct{}
	startsScope    bool
}

func pythonImportBody(source string) string {
	bodyLines := make([]string, 0)
	inImportBlock := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if inImportBlock {
			if strings.Contains(trimmed, ")") {
				inImportBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
			if strings.HasSuffix(trimmed, "(") || (strings.Contains(trimmed, " import (") && !strings.Contains(trimmed, ")")) {
				inImportBlock = true
			}
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	return strings.Join(bodyLines, "\n")
}

// pythonStripper blanks out Python string literals and comments so the
// identifier scan never mistakes their contents for code. Every replacement is
// as long as what it replaces and newlines are preserved, so byte offsets and
// line numbering survive the pass unchanged.
type pythonStripper struct {
	source   string
	builder  strings.Builder
	quote    byte
	inString bool
	triple   bool
}

func stripPythonStringsAndComments(source string) string {
	stripper := pythonStripper{source: source}
	for index := 0; index < len(source); index++ {
		index = stripper.step(index)
	}
	return stripper.builder.String()
}

// step consumes the run of source starting at index and returns the last index
// it consumed; the caller's loop advances past it.
func (s *pythonStripper) step(index int) int {
	if s.inString {
		return s.stepInString(index)
	}
	switch s.source[index] {
	case '#':
		return s.stepComment(index)
	case '\'', '"':
		return s.stepStringStart(index)
	}
	s.builder.WriteByte(s.source[index])
	return index
}

// stepInString blanks one byte of an open literal. A single-quoted literal also
// ends at a newline, which is how an unterminated quote fails to swallow the
// rest of the file.
func (s *pythonStripper) stepInString(index int) int {
	value := s.source[index]
	if value == '\n' {
		s.builder.WriteByte('\n')
		if !s.triple {
			s.inString = false
		}
		return index
	}
	if value == '\\' && !s.triple && index+1 < len(s.source) {
		return s.stepEscape(index)
	}
	if value == s.quote {
		return s.stepQuote(index)
	}
	s.builder.WriteByte(' ')
	return index
}

// stepEscape blanks a backslash escape inside a single-quoted literal, keeping
// a line continuation's newline so line numbering survives.
func (s *pythonStripper) stepEscape(index int) int {
	s.builder.WriteByte(' ')
	index++
	if s.source[index] == '\n' {
		s.builder.WriteByte('\n')
	} else {
		s.builder.WriteByte(' ')
	}
	return index
}

// stepQuote handles a quote byte inside an open literal: a triple-quoted
// literal closes only on its full closing triple, a single-quoted one here.
func (s *pythonStripper) stepQuote(index int) int {
	if !s.triple {
		s.inString = false
		s.builder.WriteByte(' ')
		return index
	}
	if index+2 < len(s.source) && s.source[index+1] == s.quote && s.source[index+2] == s.quote {
		s.builder.WriteString("   ")
		s.inString = false
		return index + 2
	}
	s.builder.WriteByte(' ')
	return index
}

// stepComment blanks a comment through the end of its line, preserving the
// newline that terminates it.
func (s *pythonStripper) stepComment(index int) int {
	for index < len(s.source) && s.source[index] != '\n' {
		s.builder.WriteByte(' ')
		index++
	}
	if index < len(s.source) {
		s.builder.WriteByte('\n')
	}
	return index
}

// stepStringStart opens a literal, blanking its opening quote or triple quote.
func (s *pythonStripper) stepStringStart(index int) int {
	s.quote = s.source[index]
	s.triple = index+2 < len(s.source) && s.source[index+1] == s.quote && s.source[index+2] == s.quote
	s.inString = true
	if !s.triple {
		s.builder.WriteByte(' ')
		return index
	}
	s.builder.WriteString("   ")
	return index + 2
}

func pythonTokensByLine(source string) map[int][]string {
	tokens := make(map[int][]string)
	line := 0
	for index := 0; index < len(source); {
		value := rune(source[index])
		if value == '\n' {
			line++
			index++
			continue
		}
		if isPythonIdentifierStart(value) {
			start := index
			index++
			for index < len(source) && isPythonIdentifierPart(rune(source[index])) {
				index++
			}
			tokens[line] = append(tokens[line], source[start:index])
			continue
		}
		if strings.ContainsRune("()[]{}.,:+-*/%=<>!", value) {
			if index+1 < len(source) {
				two := source[index : index+2]
				if pythonTwoCharOperators[two] {
					tokens[line] = append(tokens[line], two)
					index += 2
					continue
				}
			}
			tokens[line] = append(tokens[line], string(value))
		}
		index++
	}
	return tokens
}

func pythonTokenLines(source string) []pythonTokenLine {
	lines := strings.Split(source, "\n")
	result := make([]pythonTokenLine, 0, len(lines))
	for _, line := range lines {
		tokens := pythonTokensByLine(line)[0]
		if len(tokens) == 0 {
			continue
		}
		result = append(result, pythonTokenLine{
			indent: pythonIndent(line),
			tokens: tokens,
		})
	}
	return result
}

func pythonIndent(line string) int {
	indent := 0
	for _, value := range line {
		switch value {
		case ' ':
			indent++
		case '\t':
			indent += 4
		default:
			return indent
		}
	}
	return indent
}

func pythonLineBindings(tokens []string) pythonBindings {
	bindings := pythonBindings{
		positions:      make(map[int]bool),
		lineBound:      make(map[string]struct{}),
		outerBound:     make(map[string]struct{}),
		nextScopeBound: make(map[string]struct{}),
	}
	for index, token := range tokens {
		if token == "as" {
			bindNextIdentifierAt(tokens, index+1, bindings.positions, bindings.lineBound)
		}
		if token == "for" {
			bindBeforeTokenAt(tokens, index+1, "in", bindings.positions, bindings.lineBound)
		}
	}
	if len(tokens) == 0 {
		return bindings
	}
	switch tokens[0] {
	case "def":
		bindNextIdentifierAt(tokens, 1, bindings.positions, bindings.outerBound)
		bindFunctionParametersAt(tokens, bindings.positions, bindings.nextScopeBound)
		bindings.startsScope = true
	case "class":
		bindNextIdentifierAt(tokens, 1, bindings.positions, bindings.outerBound)
		bindings.startsScope = true
	default:
		if assignment := firstPythonAssignment(tokens); assignment > 0 {
			bindIdentifiersAt(tokens[:assignment], 0, bindings.positions, bindings.lineBound)
		}
	}
	return bindings
}

func pythonVisibleBindings(scopes []pythonScope, extra map[string]struct{}) map[string]struct{} {
	visible := make(map[string]struct{})
	for _, scope := range scopes {
		for name := range scope.bound {
			visible[name] = struct{}{}
		}
	}
	for name := range extra {
		visible[name] = struct{}{}
	}
	return visible
}

func bindNextIdentifierAt(tokens []string, start int, positions map[int]bool, bound map[string]struct{}) {
	for index := start; index < len(tokens); index++ {
		if isPythonIdentifierToken(tokens[index]) && !pythonKeywords[tokens[index]] {
			positions[index] = true
			bound[tokens[index]] = struct{}{}
			return
		}
	}
}

func bindFunctionParametersAt(tokens []string, positions map[int]bool, bound map[string]struct{}) {
	depth := 0
	inParams := false
	for index, token := range tokens {
		switch token {
		case "(":
			depth++
			inParams = true
		case ")":
			depth--
			if depth <= 0 {
				return
			}
		default:
			if inParams && depth == 1 && isPythonIdentifierToken(token) && !pythonKeywords[token] {
				positions[index] = true
				bound[token] = struct{}{}
			}
		}
	}
}

func bindBeforeTokenAt(tokens []string, start int, stop string, positions map[int]bool, bound map[string]struct{}) {
	for index := start; index < len(tokens); index++ {
		if tokens[index] == stop {
			bindIdentifiersAt(tokens[start:index], start, positions, bound)
			return
		}
	}
}

func bindIdentifiersAt(tokens []string, offset int, positions map[int]bool, bound map[string]struct{}) {
	for index, token := range tokens {
		if !isPythonIdentifierToken(token) || pythonKeywords[token] {
			continue
		}
		if index > 0 && tokens[index-1] == "." {
			continue
		}
		positions[offset+index] = true
		bound[token] = struct{}{}
	}
}

func firstPythonAssignment(tokens []string) int {
	for index, token := range tokens {
		if pythonAssignmentOperators[token] {
			return index
		}
	}
	return -1
}

func isPythonIdentifierToken(token string) bool {
	if token == "" {
		return false
	}
	runes := []rune(token)
	if !isPythonIdentifierStart(runes[0]) {
		return false
	}
	for _, value := range runes[1:] {
		if !isPythonIdentifierPart(value) {
			return false
		}
	}
	return true
}

func isPythonIdentifierStart(value rune) bool {
	return value == '_' || unicode.IsLetter(value)
}

func isPythonIdentifierPart(value rune) bool {
	return isPythonIdentifierStart(value) || unicode.IsDigit(value)
}

var pythonKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true,
	"assert": true, "async": true, "await": true, "break": true, "class": true,
	"continue": true, "def": true, "del": true, "elif": true, "else": true,
	"except": true, "finally": true, "for": true, "from": true, "global": true,
	"if": true, "import": true, "in": true, "is": true, "lambda": true,
	"nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
}

var pythonAssignmentOperators = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true,
	"//=": true, "%=": true, "**=": true, ":=": true,
}

var pythonTwoCharOperators = map[string]bool{
	"==": true, "!=": true, "<=": true, ">=": true, "+=": true,
	"-=": true, "*=": true, "/=": true, "//": true, "%=": true,
	"**": true, ":=": true,
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

// pythonImportNames returns the local name every import statement in source
// binds, following a parenthesized "from ... import (" list across lines.
func pythonImportNames(source string) []string {
	names := make([]string, 0)
	collectingFrom := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := stripPythonComment(strings.TrimSpace(line))
		if trimmed == "" {
			continue
		}
		var lineNames []string
		if collectingFrom {
			lineNames, collectingFrom = pythonImportBlockNames(trimmed)
		} else {
			lineNames, collectingFrom = pythonImportStatementNames(trimmed)
		}
		names = append(names, lineNames...)
	}
	return names
}

// pythonImportStatementNames reads one "import ..." or "from ... import ..."
// statement, reporting whether a parenthesized list stays open on the lines
// that follow. A line that is neither statement binds nothing.
func pythonImportStatementNames(trimmed string) ([]string, bool) {
	if strings.HasPrefix(trimmed, "import ") {
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "import "))
		return parsePythonImportList("import", rest), false
	}
	if strings.HasPrefix(trimmed, "from ") {
		return pythonFromImportNames(trimmed)
	}
	return nil, false
}

// pythonFromImportNames reads a "from ... import ..." line. A list opened with
// "(" and not closed on the same line leaves the block open for the lines that
// follow; a line with no " import " at all binds nothing.
func pythonFromImportNames(trimmed string) ([]string, bool) {
	importIndex := strings.Index(trimmed, " import ")
	if importIndex < 0 {
		return nil, false
	}
	rest := strings.TrimSpace(trimmed[importIndex+len(" import "):])
	if rest == "(" {
		return nil, true
	}
	if !strings.HasPrefix(rest, "(") {
		return parsePythonImportList("from", rest), false
	}
	rest = strings.TrimPrefix(rest, "(")
	if !strings.Contains(rest, ")") {
		return parsePythonImportList("from", rest), true
	}
	return parsePythonImportList("from", strings.Split(rest, ")")[0]), false
}

// pythonImportBlockNames reads one continuation line of an open parenthesized
// import list, reporting whether the block is still open afterwards.
func pythonImportBlockNames(trimmed string) ([]string, bool) {
	if strings.HasPrefix(trimmed, ")") {
		return nil, false
	}
	if !strings.Contains(trimmed, ")") {
		return parsePythonImportList("from", trimmed), true
	}
	closed := strings.TrimSpace(strings.TrimSuffix(strings.Split(trimmed, ")")[0], ","))
	return parsePythonImportList("from", closed), false
}

// parsePythonImportList splits a comma-separated import clause list into the
// names it binds. kind is "import" or "from" and decides whether a dotted
// module path contributes its root package name.
func parsePythonImportList(kind, list string) []string {
	list = strings.TrimSpace(strings.Trim(list, "()"))
	names := make([]string, 0)
	for _, part := range strings.Split(list, ",") {
		if name, ok := pythonImportName(kind, part); ok {
			names = append(names, name)
		}
	}
	return names
}

// pythonImportName resolves the name one clause binds: its alias when it has
// one, and for a plain "import a.b.c" the root package that lands in scope.
func pythonImportName(kind, part string) (string, bool) {
	part = stripPythonComment(strings.TrimSpace(part))
	if pythonBindsNoName(part) {
		return "", false
	}
	fields := strings.Fields(part)
	name := fields[0]
	if len(fields) >= 3 && fields[len(fields)-2] == "as" {
		return fields[len(fields)-1], true
	}
	if dot := strings.Index(name, "."); dot >= 0 && kind == "import" {
		name = name[:dot]
	}
	return name, true
}

// pythonBindsNoName reports whether an import clause binds nothing: an empty
// clause, a star import, or a stray parenthesis left by the list split.
func pythonBindsNoName(part string) bool {
	return part == "" || part == "*" || part == "(" || part == ")"
}

func stripPythonComment(value string) string {
	if index := strings.Index(value, "#"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
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
