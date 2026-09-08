package analyzer

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

var (
	tsFunctionRE     = regexp.MustCompile(`(?m)^[ \t]*(?:(?:export|default|async|public|private|protected|static|override|abstract|const|let|var)\s+)*(?:function\s+([A-Za-z_$][\w$]*)|([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)(?:\s*:\s*[^=\n]+)?\s*=>|([A-Za-z_$][\w$]*)\s*\([^;{}]*\)\s*\{)`)
	tsImportRE       = regexp.MustCompile(`(?m)^\s*import\s+(?:(?:type\s+)?[^;]+?\s+from\s+)?["']([^"']+)["']`)
	tsControlRE      = regexp.MustCompile(`\b(?:if|for|while|case|catch)\b|&&|\|\||\?`)
	tsNameRE         = regexp.MustCompile(`[A-Za-z_$][\w$]*`)
	tsImportClauseRE = regexp.MustCompile(`(?m)^\s*import\s+(.+?)\s+from\s+["']`)
	tsTypeDeclRE     = regexp.MustCompile(`^\s*(?:(?:export|declare|abstract)\s+)*(?:interface|type|enum|namespace|module)\b`)
)

// AnalyzeTypeScriptFile computes the common fitness metrics for TypeScript and
// TSX without requiring a Node.js installation. It deliberately uses a
// comment/string-masked lexical scan: the audit contract needs stable metrics
// across repositories, while full TypeScript type-checking is outside the
// fitness-function boundary.
func AnalyzeTypeScriptFile(file string) (AnalysisResult, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return AnalysisResult{}, err
	}
	text := string(source)
	masked := maskTypeScript(text)
	functions := typeScriptFunctions(text, masked)
	imports := typeScriptImports(text, masked)
	total, logic := typeScriptLineMetrics(text, masked)
	publicMethods := 0
	for _, fn := range functions {
		if fn.IsPublic {
			publicMethods++
		}
	}
	metric := FileMetric{TotalLOC: total, LogicLOC: logic, PublicMethods: publicMethods, LDR: ratio(logic, total)}
	return AnalysisResult{
		CALMNode:     typeScriptCALMNode(text, file),
		Language:     "typescript",
		File:         file,
		Functions:    functions,
		ModuleMetric: BuildModuleMetric(metric, functions),
		FileMetric:   metric,
		Imports:      imports,
	}, nil
}

func typeScriptFunctions(source, masked string) []FunctionMetric {
	matches := tsFunctionRE.FindAllStringSubmatchIndex(masked, -1)
	functions := make([]FunctionMetric, 0, len(matches))
	for _, match := range matches {
		name := "anonymous"
		for _, pair := range [][2]int{{match[2], match[3]}, {match[4], match[5]}, {match[6], match[7]}} {
			if pair[0] >= 0 {
				name = masked[pair[0]:pair[1]]
				break
			}
		}
		if isTypeScriptControlName(name) {
			continue
		}
		start := match[0]
		signatureEnd := match[1]
		end := functionEnd(masked, start, signatureEnd)
		if end < start {
			end = len(masked) - 1
		}
		startLine := lineNumber(source, start)
		endLine := lineNumber(source, end)
		body := masked[start:end]
		complexity := 1 + len(tsControlRE.FindAllString(body, -1))
		functions = append(functions, FunctionMetric{Name: name, CyclomaticComplexity: complexity, IsPublic: isTypeScriptPublic(masked[start:signatureEnd]), LOC: endLine - startLine + 1})
	}
	return functions
}

func isTypeScriptControlName(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "catch", "with":
		return true
	default:
		return false
	}
}

func isTypeScriptPublic(prefix string) bool {
	trimmed := strings.TrimSpace(prefix)
	return strings.HasPrefix(trimmed, "export") || strings.Contains(trimmed, " public ") || strings.HasPrefix(trimmed, "public ")
}

func typeScriptImports(source, masked string) ImportMetric {
	matches := tsImportRE.FindAllStringSubmatch(source, -1)
	used := 0
	unused := make([]string, 0)
	for _, match := range matches {
		module := path.Base(match[1])
		bindings := typeScriptImportBindings(match[0])
		if len(bindings) == 0 {
			used++ // side-effect imports have no local binding to audit
			continue
		}
		anyUsed := false
		unusedBindings := make([]string, 0)
		for _, binding := range bindings {
			if countTypeScriptName(masked, binding) > 1 {
				anyUsed = true
			} else {
				unusedBindings = append(unusedBindings, binding)
			}
		}
		if anyUsed {
			used++
		}
		if len(unusedBindings) > 0 {
			if len(unusedBindings) == len(bindings) {
				unused = append(unused, module)
			} else {
				unused = append(unused, fmt.Sprintf("%s (%s)", module, strings.Join(unusedBindings, ", ")))
			}
		}
	}
	return ImportMetric{Total: len(matches), Used: used, Unused: unused, DDC: ratio(used, len(matches))}
}

func typeScriptImportBindings(statement string) []string {
	match := tsImportClauseRE.FindStringSubmatch(statement)
	if len(match) < 2 {
		return nil
	}
	clause := strings.TrimSpace(match[1])
	clause = strings.TrimPrefix(clause, "type ")
	bindings := make([]string, 0)
	for _, part := range strings.Split(clause, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "{") {
			part = strings.Trim(strings.TrimSpace(part), "{}")
			for _, named := range strings.Split(part, ",") {
				fields := strings.Fields(strings.TrimSpace(named))
				if len(fields) > 0 {
					binding := fields[len(fields)-1]
					if binding != "type" {
						bindings = append(bindings, binding)
					}
				}
			}
		} else if strings.HasPrefix(part, "*") {
			fields := strings.Fields(part)
			if len(fields) >= 3 {
				bindings = append(bindings, fields[2])
			}
		} else if part != "type" && part != "" {
			bindings = append(bindings, strings.Fields(part)[0])
		}
	}
	return bindings
}

func countTypeScriptName(masked, name string) int {
	count := 0
	for _, match := range tsNameRE.FindAllStringIndex(masked, -1) {
		if masked[match[0]:match[1]] == name {
			count++
		}
	}
	return count
}

func functionEnd(masked string, start, signatureEnd int) int {
	arrow := strings.Index(masked[start:signatureEnd], "=>")
	if arrow >= 0 {
		arrow += start + 2
		for arrow < len(masked) && (masked[arrow] == ' ' || masked[arrow] == '\t') {
			arrow++
		}
		if arrow < len(masked) && masked[arrow] == '{' {
			return matchingBrace(masked, arrow)
		}
		if end := strings.IndexByte(masked[arrow:], '\n'); end >= 0 {
			if end == 0 {
				return arrow
			}
			return arrow + end - 1
		}
		return len(masked) - 1
	}
	if brace := strings.Index(masked[signatureEnd:], "{"); brace >= 0 {
		return matchingBrace(masked, signatureEnd+brace)
	}
	return signatureEnd
}

func typeScriptLineMetrics(source, masked string) (int, int) {
	total, logic := 0, 0
	inTypeDecl := false
	for i, line := range strings.Split(source, "\n") {
		if strings.TrimSpace(line) == "" || strings.TrimSpace(maskedLine(masked, i)) == "" {
			continue
		}
		total++
		trimmed := strings.TrimSpace(maskedLine(masked, i))
		if inTypeDecl {
			if strings.Contains(trimmed, "}") {
				inTypeDecl = false
			}
			continue
		}
		if tsTypeDeclRE.MatchString(trimmed) {
			if strings.Contains(trimmed, "{") && !strings.Contains(trimmed, "}") {
				inTypeDecl = true
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "import") && trimmed != "{" && trimmed != "}" && trimmed != "};" && trimmed != "}," {
			logic++
		}
	}
	return total, logic
}

func typeScriptCALMNode(source, file string) string {
	for _, match := range regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?(?:class|namespace|module)\s+([A-Za-z_$][\w$]*)`).FindAllStringSubmatch(source, -1) {
		return match[1]
	}
	return strings.TrimSuffix(path.Base(file), path.Ext(file))
}

func maskTypeScript(source string) string {
	var b strings.Builder
	b.Grow(len(source))
	inBlock, inString := false, byte(0)
	for i := 0; i < len(source); i++ {
		c := source[i]
		if inBlock {
			if c == '*' && i+1 < len(source) && source[i+1] == '/' {
				b.WriteString("  ")
				i++
				inBlock = false
			} else if c == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if inString != 0 {
			if c == '\\' && i+1 < len(source) {
				b.WriteString("  ")
				i++
				continue
			}
			if c == inString {
				inString = 0
			}
			if c == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '*' {
			b.WriteString("  ")
			i++
			inBlock = true
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '/' {
			b.WriteString("  ")
			i++
			for i+1 < len(source) && source[i+1] != '\n' {
				b.WriteByte(' ')
				i++
			}
			continue
		}
		if c == '`' {
			maskTemplateLiteral(source, &i, &b)
			continue
		}
		if c == '\'' || c == '"' {
			b.WriteByte(' ')
			inString = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// maskTemplateLiteral preserves ${...} expressions while masking literal text.
// The expression contents remain available to the lexical metric scanner.
func maskTemplateLiteral(source string, index *int, output *strings.Builder) {
	output.WriteByte(' ')
	for *index = *index + 1; *index < len(source); *index++ {
		c := source[*index]
		if c == '\\' && *index+1 < len(source) {
			output.WriteString("  ")
			*index++
			continue
		}
		if c == '`' {
			output.WriteByte(' ')
			return
		}
		if c == '$' && *index+1 < len(source) && source[*index+1] == '{' {
			output.WriteString("${")
			*index += 2
			depth := 1
			for *index < len(source) && depth > 0 {
				if source[*index] == '{' {
					depth++
				}
				if source[*index] == '}' {
					depth--
				}
				if depth > 0 {
					output.WriteByte(source[*index])
				}
				*index++
			}
			*index--
			continue
		}
		if c == '\n' {
			output.WriteByte('\n')
		} else {
			output.WriteByte(' ')
		}
	}
}

func maskedLine(masked string, index int) string {
	lines := strings.Split(masked, "\n")
	if index >= len(lines) {
		return ""
	}
	return lines[index]
}
func lineNumber(source string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	return 1 + strings.Count(source[:minInt(offset, len(source))], "\n")
}
func matchingBrace(source string, start int) int {
	if start < 0 {
		return -1
	}
	depth := 0
	for i := start; i < len(source); i++ {
		if source[i] == '{' {
			depth++
		}
		if source[i] == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
