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
	LOC  int `json:"loc"`
	LLOC int `json:"lloc"`
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
		return nil, fmt.Errorf("parsing radon cc: %w", err)
	}
	var rawPayload map[string]radonRawItem
	if err := json.Unmarshal(rawOutput, &rawPayload); err != nil {
		return nil, fmt.Errorf("parsing radon raw: %w", err)
	}
	files = make([]string, 0, len(rawPayload))
	for file := range rawPayload {
		files = append(files, file)
	}
	sort.Strings(files)
	results := make([]AnalysisResult, 0, len(files))
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		fileMetric := FileMetric{TotalLOC: rawPayload[file].LOC, LogicLOC: rawPayload[file].LLOC}
		fileMetric.LDR = ratio(fileMetric.LogicLOC, fileMetric.TotalLOC)
		functions := pythonFunctions(ccPayload[file])
		publicMethods := 0
		for _, fn := range functions {
			if fn.IsPublic {
				publicMethods++
			}
		}
		fileMetric.PublicMethods = publicMethods
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
	importPattern := regexp.MustCompile(`(?m)^\s*(?:import\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?|from\s+[A-Za-z0-9_.]+\s+import\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?)`)
	matches := importPattern.FindAllStringSubmatch(source, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		name := firstNonEmpty(match[2], match[1], match[4], match[3])
		if name != "" {
			names = append(names, name)
		}
	}
	bodyLines := make([]string, 0)
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
