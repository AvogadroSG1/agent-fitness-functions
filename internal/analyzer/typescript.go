package analyzer

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode"
)

var (
	tsImportRE       = regexp.MustCompile(`(?m)^\s*import\s+(?:(?:type\s+)?[^;]+?\s+from\s+)?["']([^"']+)["']`)
	tsNameRE         = regexp.MustCompile(`[A-Za-z_$][\w$]*`)
	tsImportClauseRE = regexp.MustCompile(`(?m)^\s*import\s+(.+?)\s+from\s+["']`)
	tsTypeDeclRE     = regexp.MustCompile(`^\s*(?:(?:export|declare|abstract)\s+)*(?:interface|type|enum|namespace|module)\b`)
	tsCalmNodeRE     = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?(?:class|namespace|module)\s+([A-Za-z_$][\w$]*)`)

	// tsFunctionDeclRE matches function declarations/expressions:
	//   function foo(a: string): string {
	//   export default async function* bar<T>(x: T): Promise<T> {
	tsFunctionDeclRE = regexp.MustCompile(`(?m)^[ \t]*(?:(?:export|default|async|static|public|private|protected|override|abstract|readonly)\s+)*function(?:\s*\*)?\s*([A-Za-z_$][\w$]*)?\s*(?:<[^>]*>)?\s*\(`)

	// tsArrowDeclRE matches assigned arrow functions:
	//   const add = (a: number, b: number): number => ...
	//   export const render = async <T>(item: T): Promise<void> => ...
	tsArrowDeclRE = regexp.MustCompile(`(?m)^[ \t]*(?:(?:export|default|const|let|var)\s+)*([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:<[^>]*>)?\s*(?:\((?:[^)(]+|\((?:[^)(]+|\([^)(]*\))*\))*\)|[A-Za-z_$][\w$]*)(?:\s*:\s*[^=]+)?\s*=>`)

	// tsMethodDeclRE matches class/object methods, constructors, getters, setters:
	//   public async getUser(id: string): Promise<User> {
	//   get name(): string {
	//   constructor(id: string) {
	tsMethodDeclRE = regexp.MustCompile(`(?m)^[ \t]*(?:(?:export|default|public|private|protected|static|override|abstract|async|get|set)\s+)*([A-Za-z_$][\w$]*)\s*(?:<[^>]*>)?\s*\((?:[^)(]+|\((?:[^)(]+|\([^)(]*\))*\))*\)(?:\s*:\s*[^{]+)?\s*\{`)
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
		CALMNode:     typeScriptCALMNode(masked, file),
		Language:     "typescript",
		File:         file,
		Functions:    functions,
		ModuleMetric: BuildModuleMetric(metric, functions),
		FileMetric:   metric,
		Imports:      imports,
	}, nil
}

type rawFunctionMatch struct {
	start        int
	signatureEnd int
	name         string
	isArrow      bool
	isPublic     bool
}

func typeScriptFunctions(source, masked string) []FunctionMetric {
	rawMatches := findTypeScriptFunctions(masked)
	functions := make([]FunctionMetric, 0, len(rawMatches))

	for _, raw := range rawMatches {
		if isTypeScriptControlName(raw.name) {
			continue
		}
		end := functionEnd(masked, raw.start, raw.signatureEnd, raw.isArrow)
		if end < raw.start {
			end = len(masked) - 1
		}
		startLine := lineNumber(source, raw.start)
		endLine := lineNumber(source, end)
		bodyMasked := masked[raw.start : end+1]
		bodySource := source[raw.start : end+1]
		complexity := computeTypeScriptComplexity(bodySource, bodyMasked)

		functions = append(functions, FunctionMetric{
			Name:                 raw.name,
			CyclomaticComplexity: complexity,
			IsPublic:             raw.isPublic,
			LOC:                  endLine - startLine + 1,
		})
	}
	return functions
}

func findTypeScriptFunctions(masked string) []rawFunctionMatch {
	var results []rawFunctionMatch
	seenStarts := make(map[int]bool)

	// 1. Function declarations:
	for _, match := range tsFunctionDeclRE.FindAllStringSubmatchIndex(masked, -1) {
		start := match[0]
		if seenStarts[start] {
			continue
		}
		name := "anonymous"
		if match[2] >= 0 && match[3] >= 0 {
			name = masked[match[2]:match[3]]
		}
		prefix := masked[start:match[1]]
		sigEnd := findSignatureEndBrace(masked, match[1]-1)
		seenStarts[start] = true
		results = append(results, rawFunctionMatch{
			start:        start,
			signatureEnd: sigEnd,
			name:         name,
			isArrow:      false,
			isPublic:     isTypeScriptPublic(prefix),
		})
	}

	// 2. Arrow functions:
	for _, match := range tsArrowDeclRE.FindAllStringSubmatchIndex(masked, -1) {
		start := match[0]
		if seenStarts[start] {
			continue
		}
		name := "anonymous"
		if match[2] >= 0 && match[3] >= 0 {
			name = masked[match[2]:match[3]]
		}
		prefix := masked[start:match[1]]
		seenStarts[start] = true
		results = append(results, rawFunctionMatch{
			start:        start,
			signatureEnd: match[1],
			name:         name,
			isArrow:      true,
			isPublic:     isTypeScriptPublic(prefix),
		})
	}

	// 3. Method declarations:
	for _, match := range tsMethodDeclRE.FindAllStringSubmatchIndex(masked, -1) {
		start := match[0]
		if seenStarts[start] {
			continue
		}
		name := "anonymous"
		if match[2] >= 0 && match[3] >= 0 {
			name = masked[match[2]:match[3]]
		}
		if isTypeScriptReservedMethodName(name) {
			continue
		}
		prefix := masked[start:match[1]]
		seenStarts[start] = true
		results = append(results, rawFunctionMatch{
			start:        start,
			signatureEnd: match[1],
			name:         name,
			isArrow:      false,
			isPublic:     isTypeScriptPublic(prefix),
		})
	}

	// Sort by start position
	slicesSortRawMatches(results)
	return results
}

func slicesSortRawMatches(matches []rawFunctionMatch) {
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].start < matches[j-1].start; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
}

func findSignatureEndBrace(masked string, searchFrom int) int {
	brace := strings.IndexByte(masked[searchFrom:], '{')
	if brace >= 0 {
		return searchFrom + brace + 1
	}
	return searchFrom
}

func isTypeScriptReservedMethodName(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "catch", "with", "return", "throw", "new", "typeof", "import", "export", "from", "type", "interface", "class":
		return true
	default:
		return false
	}
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

// computeTypeScriptComplexity calculates cyclomatic complexity for a function body.
// Base complexity is 1. Control flow keywords (if, for, while, case, catch), logical
// operators (&&, ||, ??), and ternary expressions (?) add 1.
// Optional chaining (?.) and optional parameters/properties (?:) do NOT add complexity.
func computeTypeScriptComplexity(bodySource, bodyMasked string) int {
	complexity := 1
	n := len(bodyMasked)
	for i := 0; i < n; i++ {
		b := bodyMasked[i]

		// Check keywords on identifier boundary:
		if isIdentStart(b) {
			start := i
			for i < n && isIdentChar(bodyMasked[i]) {
				i++
			}
			word := bodyMasked[start:i]
			i-- // step back for loop increment
			switch word {
			case "if", "for", "while", "case", "catch":
				complexity++
			}
			continue
		}

		// Logical AND: && and &&=
		if b == '&' && i+1 < n && bodyMasked[i+1] == '&' {
			complexity++
			i++
			if i+1 < n && bodyMasked[i+1] == '=' {
				i++
			}
			continue
		}

		// Logical OR: || and ||=
		if b == '|' && i+1 < n && bodyMasked[i+1] == '|' {
			complexity++
			i++
			if i+1 < n && bodyMasked[i+1] == '=' {
				i++
			}
			continue
		}

		// Nullish coalescing: ?? and ??=
		if b == '?' && i+1 < n && bodyMasked[i+1] == '?' {
			complexity++
			i++
			if i+1 < n && bodyMasked[i+1] == '=' {
				i++
			}
			continue
		}

		// Optional chaining: ?. (e.g. obj?.prop or arr?.[0] or fn?.())
		if b == '?' && i+1 < n && bodyMasked[i+1] == '.' {
			i++
			continue
		}

		// Ternary operator vs optional property/type:
		if b == '?' {
			// Check if this is an optional parameter/property type annotation like `foo?: string`
			// In bodySource, optional property/param is `?` immediately followed by `:` (ignoring whitespace).
			afterQ := i + 1
			for afterQ < len(bodySource) && (bodySource[afterQ] == ' ' || bodySource[afterQ] == '\t' || bodySource[afterQ] == '\n' || bodySource[afterQ] == '\r') {
				afterQ++
			}
			if afterQ < len(bodySource) && bodySource[afterQ] == ':' {
				// Pattern is `?:` -> optional type/property, not a ternary branch
				i = afterQ
				continue
			}
			// Otherwise it's a ternary condition `?`
			complexity++
		}
	}
	return complexity
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b == '$'
}

func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
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

	// Check for named bindings inside { ... }
	openBrace := strings.IndexByte(clause, '{')
	closeBrace := strings.IndexByte(clause, '}')
	if openBrace >= 0 && closeBrace > openBrace {
		// Default import preceding the braces: `import defaultExport, { ... } from "mod"`
		beforeBrace := strings.TrimSpace(clause[:openBrace])
		beforeBrace = strings.TrimSuffix(beforeBrace, ",")
		beforeBrace = strings.TrimSpace(beforeBrace)
		if beforeBrace != "" && beforeBrace != "type" {
			fields := strings.Fields(beforeBrace)
			if len(fields) > 0 && fields[0] != "type" {
				bindings = append(bindings, fields[0])
			}
		}

		insideBraces := clause[openBrace+1 : closeBrace]
		for _, part := range strings.Split(insideBraces, ",") {
			part = strings.TrimSpace(part)
			part = strings.TrimPrefix(part, "type ")
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.Contains(part, " as ") {
				fields := strings.Fields(part)
				if len(fields) >= 3 {
					bindings = append(bindings, fields[len(fields)-1])
				}
			} else {
				fields := strings.Fields(part)
				if len(fields) > 0 {
					bindings = append(bindings, fields[0])
				}
			}
		}
		return bindings
	}

	// Namespace import: `import * as name from "mod"`
	if strings.Contains(clause, "* as ") {
		parts := strings.Split(clause, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "* as ") {
				fields := strings.Fields(part)
				if len(fields) >= 3 {
					bindings = append(bindings, fields[2])
				}
			} else if part != "" && part != "type" {
				fields := strings.Fields(part)
				if len(fields) > 0 {
					bindings = append(bindings, fields[0])
				}
			}
		}
		return bindings
	}

	// Default only: `import name from "mod"`
	fields := strings.Fields(clause)
	if len(fields) > 0 && fields[0] != "type" {
		bindings = append(bindings, fields[0])
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

func functionEnd(masked string, start, signatureEnd int, isArrow bool) int {
	if isArrow {
		arrow := strings.Index(masked[start:signatureEnd], "=>")
		if arrow >= 0 {
			arrow += start + 2
			for arrow < len(masked) && (masked[arrow] == ' ' || masked[arrow] == '\t' || masked[arrow] == '\n' || masked[arrow] == '\r') {
				arrow++
			}
			if arrow < len(masked) && masked[arrow] == '{' {
				return matchingBrace(masked, arrow)
			}
			if arrow < len(masked) && masked[arrow] == '(' {
				return matchingParen(masked, arrow)
			}
			if end := strings.IndexByte(masked[arrow:], ';'); end >= 0 {
				return arrow + end
			}
			if end := strings.IndexByte(masked[arrow:], '\n'); end >= 0 {
				if end == 0 {
					return arrow
				}
				return arrow + end - 1
			}
			return len(masked) - 1
		}
	}
	if brace := strings.IndexByte(masked[signatureEnd-1:], '{'); brace >= 0 {
		return matchingBrace(masked, signatureEnd-1+brace)
	}
	return signatureEnd
}

func typeScriptLineMetrics(source, masked string) (int, int) {
	total, logic := 0, 0
	inTypeDecl := false
	sourceLines := strings.Split(source, "\n")
	maskedLines := strings.Split(masked, "\n")

	for i, line := range sourceLines {
		maskedLineStr := ""
		if i < len(maskedLines) {
			maskedLineStr = maskedLines[i]
		}
		if strings.TrimSpace(line) == "" || strings.TrimSpace(maskedLineStr) == "" {
			continue
		}
		total++
		trimmed := strings.TrimSpace(maskedLineStr)
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

func typeScriptCALMNode(masked, file string) string {
	matches := tsCalmNodeRE.FindAllStringSubmatch(masked, -1)
	if len(matches) > 0 && len(matches[0]) > 1 {
		return matches[0][1]
	}
	return strings.TrimSuffix(path.Base(file), path.Ext(file))
}

// maskTypeScript blanks out comments, single/double quoted strings, regex literals,
// and template literal contents outside `${...}` while preserving exact length and line breaks.
func maskTypeScript(source string) string {
	var b strings.Builder
	b.Grow(len(source))

	n := len(source)
	var lastToken string
	tokStart := -1

	i := 0
	for i < n {
		c := source[i]

		// 1. Line comment // ...
		if c == '/' && i+1 < n && source[i+1] == '/' {
			b.WriteString("  ")
			i += 2
			for i < n && source[i] != '\n' {
				b.WriteByte(' ')
				i++
			}
			continue
		}

		// 2. Block comment /* ... */
		if c == '/' && i+1 < n && source[i+1] == '*' {
			b.WriteString("  ")
			i += 2
			for i < n {
				if source[i] == '*' && i+1 < n && source[i+1] == '/' {
					b.WriteString("  ")
					i += 2
					break
				}
				if source[i] == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
				i++
			}
			continue
		}

		// Track previous non-whitespace token for regex disambiguation
		if !unicode.IsSpace(rune(c)) && c != '/' {
			if tokStart < 0 {
				tokStart = i
			}
		} else {
			if tokStart >= 0 {
				lastToken = source[tokStart:i]
				tokStart = -1
			}
		}

		// 3. Regex literal:
		if c == '/' && canFollowRegex(lastToken) {
			if maskRegexAt(source, &i, &b) {
				lastToken = "re"
				continue
			}
		}

		// 4. String literal: '...' or "..."
		if c == '\'' || c == '"' {
			quote := c
			b.WriteByte(' ')
			i++
			for i < n {
				qc := source[i]
				if qc == '\\' && i+1 < n {
					if source[i+1] == '\n' {
						b.WriteString(" \n")
					} else {
						b.WriteString("  ")
					}
					i += 2
					continue
				}
				if qc == quote {
					b.WriteByte(' ')
					i++
					break
				}
				if qc == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
				i++
			}
			continue
		}

		// 5. Template literal: `...`
		if c == '`' {
			maskTemplateAt(source, &i, &b)
			lastToken = "`"
			continue
		}

		b.WriteByte(c)
		i++
	}

	return b.String()
}

func canFollowRegex(token string) bool {
	if token == "" {
		return true
	}
	switch token {
	case "(", "[", "{", ";", ",", "=", ":", "!", "&", "|", "?", "+", "-", "*", "%", "^", "~", "<", ">",
		"return", "case", "throw", "yield", "await", "delete", "typeof", "void", "instanceof", "in", "of", "do", "else", "default":
		return true
	default:
		return false
	}
}

func maskRegexAt(source string, index *int, b *strings.Builder) bool {
	start := *index
	n := len(source)
	i := start + 1
	inCharClass := false

	for i < n {
		c := source[i]
		if c == '\n' {
			return false
		}
		if c == '\\' && i+1 < n {
			i += 2
			continue
		}
		if c == '[' && !inCharClass {
			inCharClass = true
			i++
			continue
		}
		if c == ']' && inCharClass {
			inCharClass = false
			i++
			continue
		}
		if c == '/' && !inCharClass {
			i++
			for i < n && isIdentChar(source[i]) {
				i++
			}
			for k := start; k < i; k++ {
				b.WriteByte(' ')
			}
			*index = i
			return true
		}
		i++
	}
	return false
}

func maskTemplateAt(source string, index *int, b *strings.Builder) {
	n := len(source)
	b.WriteByte(' ')
	*index = *index + 1

	for *index < n {
		c := source[*index]
		if c == '\\' && *index+1 < n {
			if source[*index+1] == '\n' {
				b.WriteString(" \n")
			} else {
				b.WriteString("  ")
			}
			*index += 2
			continue
		}
		if c == '`' {
			b.WriteByte(' ')
			*index = *index + 1
			return
		}
		if c == '$' && *index+1 < n && source[*index+1] == '{' {
			b.WriteString("${")
			*index += 2
			maskTemplateExprAt(source, index, b)
			continue
		}
		if c == '\n' {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
		*index = *index + 1
	}
}

func maskTemplateExprAt(source string, index *int, b *strings.Builder) {
	n := len(source)
	depth := 1

	for *index < n && depth > 0 {
		c := source[*index]

		if c == '/' && *index+1 < n && source[*index+1] == '/' {
			b.WriteString("  ")
			*index += 2
			for *index < n && source[*index] != '\n' {
				b.WriteByte(' ')
				*index = *index + 1
			}
			continue
		}

		if c == '/' && *index+1 < n && source[*index+1] == '*' {
			b.WriteString("  ")
			*index += 2
			for *index < n {
				if source[*index] == '*' && *index+1 < n && source[*index+1] == '/' {
					b.WriteString("  ")
					*index += 2
					break
				}
				if source[*index] == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
				*index = *index + 1
			}
			continue
		}

		if c == '\'' || c == '"' {
			quote := c
			b.WriteByte(' ')
			*index = *index + 1
			for *index < n {
				qc := source[*index]
				if qc == '\\' && *index+1 < n {
					if source[*index+1] == '\n' {
						b.WriteString(" \n")
					} else {
						b.WriteString("  ")
					}
					*index += 2
					continue
				}
				if qc == quote {
					b.WriteByte(' ')
					*index = *index + 1
					break
				}
				if qc == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
				*index = *index + 1
			}
			continue
		}

		if c == '`' {
			maskTemplateAt(source, index, b)
			continue
		}

		if c == '{' {
			depth++
			b.WriteByte(c)
			*index = *index + 1
			continue
		}

		if c == '}' {
			depth--
			b.WriteByte(c)
			*index = *index + 1
			if depth == 0 {
				return
			}
			continue
		}

		b.WriteByte(c)
		*index = *index + 1
	}
}

func lineNumber(source string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	return 1 + strings.Count(source[:minInt(offset, len(source))], "\n")
}

func matchingBrace(source string, start int) int {
	if start < 0 || start >= len(source) {
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
	return len(source) - 1
}

func matchingParen(source string, start int) int {
	if start < 0 || start >= len(source) {
		return -1
	}
	depth := 0
	for i := start; i < len(source); i++ {
		if source[i] == '(' {
			depth++
		}
		if source[i] == ')' {
			depth--
			if depth == 0 {
				// Also include trailing semicolon if present
				k := i + 1
				for k < len(source) && (source[k] == ' ' || source[k] == '\t') {
					k++
				}
				if k < len(source) && source[k] == ';' {
					return k
				}
				return i
			}
		}
	}
	return len(source) - 1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

