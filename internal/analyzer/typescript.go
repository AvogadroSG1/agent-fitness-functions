package analyzer

import (
	"os"
	"path"
	"regexp"
	"strings"
)

var (
	tsFunctionRE = regexp.MustCompile(`(?m)^[ \t]*(?:(?:export|default|async|public|private|protected|static|override|abstract|const|let|var)\s+)*(?:function\s+([A-Za-z_$][\w$]*)|([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>|([A-Za-z_$][\w$]*)\s*\([^;{}]*\)\s*\{)`)
	tsImportRE   = regexp.MustCompile(`(?m)^\s*import\s+(?:(?:type\s+)?[^;]+?\s+from\s+)?["']([^"']+)["']`)
	tsControlRE  = regexp.MustCompile(`\b(?:if|for|while|case|catch|&&|\|\||\?)\b|&&|\|\|`)
	tsNameRE     = regexp.MustCompile(`[A-Za-z_$][\w$]*`)
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
		end := matchingBrace(masked, strings.Index(masked[start:], "{")+start)
		if end < start {
			end = len(masked)
		}
		startLine := lineNumber(source, start)
		endLine := lineNumber(source, end)
		body := masked[start:end]
		complexity := 1 + len(tsControlRE.FindAllString(body, -1))
		functions = append(functions, FunctionMetric{Name: name, CyclomaticComplexity: complexity, IsPublic: isTypeScriptPublic(masked[start : start+match[0]-match[0]+minInt(len(masked)-start, 100)]), LOC: endLine - startLine + 1})
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
		statement := match[0]
		parts := strings.Fields(strings.TrimPrefix(strings.TrimSpace(statement), "import"))
		binding := ""
		if len(parts) > 0 && parts[0] != "{" && parts[0] != "*" && parts[0] != "type" {
			binding = strings.Trim(parts[0], "{},")
		}
		if binding != "" && strings.Count(masked, binding) <= 1 {
			unused = append(unused, module)
		} else {
			used++
		}
	}
	return ImportMetric{Total: len(matches), Used: used, Unused: unused, DDC: ratio(used, len(matches))}
}

func typeScriptLineMetrics(source, masked string) (int, int) {
	total, logic := 0, 0
	for i, line := range strings.Split(source, "\n") {
		if strings.TrimSpace(line) == "" || strings.TrimSpace(maskedLine(masked, i)) == "" {
			continue
		}
		total++
		trimmed := strings.TrimSpace(maskedLine(masked, i))
		if !strings.HasPrefix(trimmed, "import") && !strings.HasPrefix(trimmed, "export interface") && trimmed != "{" && trimmed != "}" {
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
		if c == '\'' || c == '"' || c == '`' {
			b.WriteByte(' ')
			inString = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func maskedLine(masked string, index int) string {
	lines := strings.Split(masked, "\n")
	if index >= len(lines) {
		return ""
	}
	return lines[index]
}
func lineNumber(source string, offset int) int {
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
