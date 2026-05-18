package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
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

// AnalyzePythonFile analyzes one Python source file using radon.
func AnalyzePythonFile(ctx context.Context, file, radonPath string) (AnalysisResult, error) {
	if radonPath == "" {
		radonPath = "radon"
	}
	ccOutput, err := runTool(ctx, radonPath, "cc", "-j", file)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("running radon cc: %w", err)
	}
	rawOutput, err := runTool(ctx, radonPath, "raw", "-j", file)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("running radon raw: %w", err)
	}

	functions, err := parseRadonCC(file, ccOutput)
	if err != nil {
		return AnalysisResult{}, err
	}
	fileMetric, err := parseRadonRaw(file, rawOutput)
	if err != nil {
		return AnalysisResult{}, err
	}
	source, err := os.ReadFile(file)
	if err != nil {
		return AnalysisResult{}, err
	}
	fileMetric.LDR = ratio(fileMetric.LogicLOC, fileMetric.TotalLOC)
	fileMetric.PublicMethods = publicFunctionCount(functions)

	return AnalysisResult{
		CALMNode:   strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
		Language:   "python",
		File:       file,
		Functions:  functions,
		FileMetric: fileMetric,
		Imports:    pythonImportMetric(string(source)),
	}, nil
}

func parseRadonCC(file string, output []byte) ([]FunctionMetric, error) {
	var payload map[string][]radonCCItem
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, fmt.Errorf("parsing radon cc: %w", err)
	}
	items := payload[file]
	if items == nil && len(payload) == 1 {
		for _, value := range payload {
			items = value
		}
	}
	return pythonFunctions(items), nil
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
		return FileMetric{}, fmt.Errorf("parsing radon raw: %w", err)
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
	ccArgs := append([]string{"cc", "-j"}, files...)
	ccOutput, err := runTool(ctx, radonPath, ccArgs...)
	if err != nil {
		return nil, fmt.Errorf("running radon cc: %w", err)
	}
	rawArgs := append([]string{"raw", "-j"}, files...)
	rawOutput, err := runTool(ctx, radonPath, rawArgs...)
	if err != nil {
		return nil, fmt.Errorf("running radon raw: %w", err)
	}
	var ccPayload map[string][]radonCCItem
	if err := json.Unmarshal(ccOutput, &ccPayload); err != nil {
		return nil, fmt.Errorf("parsing radon cc: %w: %s", err, trimOutput(ccOutput))
	}
	var rawPayload map[string]radonRawItem
	if err := json.Unmarshal(rawOutput, &rawPayload); err != nil {
		return nil, fmt.Errorf("parsing radon raw: %w: %s", err, trimOutput(rawOutput))
	}
	files = make([]string, 0, len(rawPayload))
	for file := range rawPayload {
		files = append(files, file)
	}
	sort.Strings(files)
	results := make([]AnalysisResult, 0, len(files))
	for _, file := range files {
		rawItem := rawPayload[file]
		if rawItem.Error != "" {
			return nil, fmt.Errorf("radon raw error for %s: %s", file, rawItem.Error)
		}
		source, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		fileMetric := FileMetric{TotalLOC: rawItem.LOC, LogicLOC: rawItem.LLOC}
		fileMetric.LDR = ratio(fileMetric.LogicLOC, fileMetric.TotalLOC)
		functions := pythonFunctions(ccPayload[file])
		fileMetric.PublicMethods = publicFunctionCount(functions)
		results = append(results, AnalysisResult{
			CALMNode:   strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
			Language:   "python",
			File:       file,
			Functions:  functions,
			FileMetric: fileMetric,
			Imports:    pythonImportMetric(string(source)),
		})
	}
	return results, nil
}

func pythonImportMetric(source string) ImportMetric {
	names := pythonImportNames(source)
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
			if strings.HasSuffix(trimmed, "(") || strings.Contains(trimmed, " import (") {
				inImportBlock = true
			}
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	body := strings.Join(bodyLines, "\n")
	used := 0
	unused := make([]string, 0)
	for _, name := range names {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body) {
			used++
			continue
		}
		unused = append(unused, name)
	}
	return ImportMetric{Total: len(names), Used: used, Unused: unused, DDC: ratio(used, len(names))}
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

func pythonImportNames(source string) []string {
	names := make([]string, 0)
	collectingFrom := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := stripPythonComment(strings.TrimSpace(line))
		if trimmed == "" {
			continue
		}
		if collectingFrom {
			if strings.HasPrefix(trimmed, ")") {
				collectingFrom = false
				continue
			}
			if strings.Contains(trimmed, ")") {
				collectingFrom = false
				trimmed = strings.TrimSpace(strings.TrimSuffix(strings.Split(trimmed, ")")[0], ","))
			}
			names = append(names, parsePythonImportList("from", trimmed)...)
			continue
		}
		if strings.HasPrefix(trimmed, "import ") {
			names = append(names, parsePythonImportList("import", strings.TrimSpace(strings.TrimPrefix(trimmed, "import ")))...)
			continue
		}
		if strings.HasPrefix(trimmed, "from ") {
			importIndex := strings.Index(trimmed, " import ")
			if importIndex < 0 {
				continue
			}
			rest := strings.TrimSpace(trimmed[importIndex+len(" import "):])
			if rest == "(" {
				collectingFrom = true
				continue
			}
			if strings.HasPrefix(rest, "(") {
				rest = strings.TrimPrefix(rest, "(")
				if strings.Contains(rest, ")") {
					rest = strings.Split(rest, ")")[0]
				} else {
					collectingFrom = true
				}
			}
			names = append(names, parsePythonImportList("from", rest)...)
		}
	}
	return names
}

func parsePythonImportList(kind, list string) []string {
	list = strings.TrimSpace(strings.Trim(list, "()"))
	names := make([]string, 0)
	for _, part := range strings.Split(list, ",") {
		part = stripPythonComment(strings.TrimSpace(part))
		if part == "" || part == "*" || part == "(" || part == ")" {
			continue
		}
		fields := strings.Fields(part)
		name := fields[0]
		if len(fields) >= 3 && fields[len(fields)-2] == "as" {
			name = fields[len(fields)-1]
		} else if dot := strings.Index(name, "."); dot >= 0 && kind == "import" {
			name = name[:dot]
		}
		names = append(names, name)
	}
	return names
}

func stripPythonComment(value string) string {
	if index := strings.Index(value, "#"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}

func runTool(ctx context.Context, name string, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
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
