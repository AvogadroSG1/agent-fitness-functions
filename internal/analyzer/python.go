package analyzer

import (
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
	if err := ctx.Err(); err != nil {
		return AnalysisResult{}, err
	}

	functions, err := parseRadonCC(file, ccOutput)
	if err != nil {
		return AnalysisResult{}, err
	}
	fileMetric, err := parseRadonRaw(file, rawOutput)
	if err != nil {
		return AnalysisResult{}, err
	}
	if err := ctx.Err(); err != nil {
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

func parseRadonCCPayload(output []byte) (map[string][]radonCCItem, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parsing radon cc: %w: %s", err, trimOutput(output))
	}
	payload := make(map[string][]radonCCItem, len(raw))
	for file, message := range raw {
		var errorItem radonErrorItem
		if err := json.Unmarshal(message, &errorItem); err == nil && errorItem.Error != "" {
			return nil, fmt.Errorf("radon cc error for %s: %s", file, errorItem.Error)
		}
		var items []radonCCItem
		if err := json.Unmarshal(message, &items); err != nil {
			return nil, fmt.Errorf("parsing radon cc for %s: %w", file, err)
		}
		payload[file] = items
	}
	return payload, nil
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
	ccPayload, err := parseRadonCCPayload(ccOutput)
	if err != nil {
		return nil, err
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

func pythonUsedIdentifiers(source string) map[string]struct{} {
	body := stripPythonStringsAndComments(pythonImportBody(source))
	tokensByLine := pythonTokensByLine(body)
	bound := pythonBoundIdentifiers(tokensByLine)
	used := make(map[string]struct{})
	for _, tokens := range tokensByLine {
		for index, token := range tokens {
			if !isPythonIdentifierToken(token) ||
				pythonKeywords[token] ||
				isPythonBindingPosition(tokens, index) {
				continue
			}
			if _, ok := bound[token]; ok {
				continue
			}
			used[token] = struct{}{}
		}
	}
	return used
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

func stripPythonStringsAndComments(source string) string {
	var builder strings.Builder
	var quote byte
	inString := false
	triple := false
	for index := 0; index < len(source); index++ {
		value := source[index]
		if inString {
			if value == '\n' {
				builder.WriteByte('\n')
				if !triple {
					inString = false
				}
				continue
			}
			if value == '\\' && !triple && index+1 < len(source) {
				builder.WriteByte(' ')
				index++
				if source[index] == '\n' {
					builder.WriteByte('\n')
				} else {
					builder.WriteByte(' ')
				}
				continue
			}
			if value == quote {
				if triple {
					if index+2 < len(source) && source[index+1] == quote && source[index+2] == quote {
						builder.WriteString("   ")
						index += 2
						inString = false
						continue
					}
				} else {
					inString = false
				}
			}
			builder.WriteByte(' ')
			continue
		}
		if value == '#' {
			for index < len(source) && source[index] != '\n' {
				builder.WriteByte(' ')
				index++
			}
			if index < len(source) {
				builder.WriteByte('\n')
			}
			continue
		}
		if value == '\'' || value == '"' {
			quote = value
			triple = index+2 < len(source) && source[index+1] == quote && source[index+2] == quote
			inString = true
			if triple {
				builder.WriteString("   ")
				index += 2
			} else {
				builder.WriteByte(' ')
			}
			continue
		}
		builder.WriteByte(value)
	}
	return builder.String()
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

func pythonBoundIdentifiers(tokensByLine map[int][]string) map[string]struct{} {
	bound := make(map[string]struct{})
	for _, tokens := range tokensByLine {
		for index, token := range tokens {
			if token == "def" || token == "class" || token == "as" {
				bindNextIdentifier(tokens, index+1, bound)
			}
		}
		if len(tokens) > 0 && tokens[0] == "def" {
			bindFunctionParameters(tokens, bound)
			continue
		}
		if len(tokens) > 0 && tokens[0] == "for" {
			bindBeforeToken(tokens, "in", bound)
		}
		if assignment := firstPythonAssignment(tokens); assignment > 0 {
			bindIdentifiers(tokens[:assignment], bound)
		}
	}
	return bound
}

func bindNextIdentifier(tokens []string, start int, bound map[string]struct{}) {
	for index := start; index < len(tokens); index++ {
		if isPythonIdentifierToken(tokens[index]) && !pythonKeywords[tokens[index]] {
			bound[tokens[index]] = struct{}{}
			return
		}
	}
}

func bindFunctionParameters(tokens []string, bound map[string]struct{}) {
	depth := 0
	inParams := false
	for _, token := range tokens {
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
				bound[token] = struct{}{}
			}
		}
	}
}

func bindBeforeToken(tokens []string, stop string, bound map[string]struct{}) {
	for index, token := range tokens {
		if token == stop {
			bindIdentifiers(tokens[:index], bound)
			return
		}
	}
}

func bindIdentifiers(tokens []string, bound map[string]struct{}) {
	for index, token := range tokens {
		if !isPythonIdentifierToken(token) || pythonKeywords[token] {
			continue
		}
		if index > 0 && tokens[index-1] == "." {
			continue
		}
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

func isPythonBindingPosition(tokens []string, index int) bool {
	if index > 0 {
		switch tokens[index-1] {
		case "def", "class", "as":
			return true
		}
	}
	if index+1 < len(tokens) && pythonAssignmentOperators[tokens[index+1]] {
		return true
	}
	return false
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

func pythonIdentifiers(source string) map[string]struct{} {
	identifiers := make(map[string]struct{})
	start := -1
	for index, value := range source {
		if start == -1 {
			if isPythonIdentifierStart(value) {
				start = index
			}
			continue
		}
		if isPythonIdentifierPart(value) {
			continue
		}
		identifiers[source[start:index]] = struct{}{}
		start = -1
	}
	if start != -1 {
		identifiers[source[start:]] = struct{}{}
	}
	return identifiers
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
