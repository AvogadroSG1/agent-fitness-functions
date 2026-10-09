package analyzer

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/fzipp/gocyclo"
)

// AnalyzeGoFile analyzes one Go source file using gocyclo and go/ast.
func AnalyzeGoFile(file string) (AnalysisResult, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file, nil, parser.ParseComments)
	if err != nil {
		return AnalysisResult{}, err
	}
	source, err := os.ReadFile(file)
	if err != nil {
		return AnalysisResult{}, err
	}

	functions := make([]FunctionMetric, 0)
	publicMethods := 0
	var interfaces goInterfaceCollector
	stats := gocyclo.AnalyzeASTFile(parsed, fileSet, nil)
	statsByLine := make(map[int]gocyclo.Stat, len(stats))
	for _, stat := range stats {
		statsByLine[stat.Pos.Line] = stat
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fn.Name.Name
		isPublic := ast.IsExported(name)
		if isPublic {
			publicMethods++
			interfaces.add(fn, fileSet)
		}
		stat, ok := statsByLine[fileSet.Position(fn.Pos()).Line]
		if !ok {
			continue
		}
		functions = append(functions, FunctionMetric{
			Name:                 name,
			CyclomaticComplexity: stat.Complexity,
			IsPublic:             isPublic,
			LOC:                  nodeLOC(fileSet, fn),
		})
	}

	interfaceMetrics := buildGoInterfaceMetrics(interfaces.packageFunctions, interfaces.receivers, fileSet.Position(parsed.Package).Line)

	totalLOC, logicLOC := lineMetrics(string(source))
	fileMetric := FileMetric{
		TotalLOC:      totalLOC,
		LogicLOC:      logicLOC,
		PublicMethods: publicMethods,
		LDR:           ratio(logicLOC, totalLOC),
	}
	importMetric := goImportMetric(parsed)
	sourceKind := SourceKind("")
	if isGoEmbedAssetFile(parsed, fileSet, source) {
		sourceKind = SourceKindGoEmbedAssets
	}
	return AnalysisResult{
		CALMNode:     parsed.Name.Name,
		Language:     "go",
		File:         file,
		SourceKind:   sourceKind,
		Functions:    functions,
		ModuleMetric: BuildModuleMetric(fileMetric, functions),
		FileMetric:   fileMetric,
		Interfaces:   interfaceMetrics,
		Imports:      importMetric,
	}, nil
}

// goReceiverMetric accumulates exported operations for one receiver base type.
type goReceiverMetric struct {
	name        string
	line        int
	methodNames map[string]struct{}
}

// goInterfaceCollector groups exported package functions and receiver methods
// during the existing declaration pass.
type goInterfaceCollector struct {
	packageFunctions int
	receivers        []goReceiverMetric
	receiverIndexes  map[string]int
}

// add records one exported operation; callers already checked its visibility.
func (c *goInterfaceCollector) add(fn *ast.FuncDecl, fileSet *token.FileSet) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		c.packageFunctions++
		return
	}
	name := goReceiverName(fn.Recv.List[0].Type)
	if name == "" {
		return
	}
	index, exists := c.receiverIndexes[name]
	if !exists {
		if c.receiverIndexes == nil {
			c.receiverIndexes = make(map[string]int)
		}
		index = len(c.receivers)
		c.receiverIndexes[name] = index
		c.receivers = append(c.receivers, goReceiverMetric{
			name:        name,
			line:        fileSet.Position(fn.Pos()).Line,
			methodNames: make(map[string]struct{}),
		})
	}
	c.receivers[index].methodNames[fn.Name.Name] = struct{}{}
}

func buildGoInterfaceMetrics(packageFunctions int, receivers []goReceiverMetric, packageLine int) []InterfaceMetric {
	if packageFunctions == 0 && len(receivers) == 0 {
		return nil
	}
	interfaces := make([]InterfaceMetric, 0, len(receivers)+1)
	interfaces = append(interfaces, InterfaceMetric{
		Kind:          "module",
		Line:          packageLine,
		PublicMethods: packageFunctions,
	})
	for i := range receivers {
		receiver := &receivers[i]
		interfaces = append(interfaces, InterfaceMetric{
			Name:          receiver.name,
			Kind:          "class",
			Line:          receiver.line,
			PublicMethods: len(receiver.methodNames),
		})
	}
	return interfaces
}

func goReceiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return goReceiverName(expr.X)
	case *ast.IndexExpr:
		return goReceiverName(expr.X)
	case *ast.IndexListExpr:
		return goReceiverName(expr.X)
	case *ast.ParenExpr:
		return goReceiverName(expr.X)
	default:
		return ""
	}
}

type goEmbedImport struct {
	alias string
	dot   bool
	blank bool
}

func isGoEmbedAssetFile(file *ast.File, fileSet *token.FileSet, source []byte) bool {
	embedImport, ok := soleGoEmbedImport(file)
	if !ok {
		return false
	}
	found := false
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			return false
		}
		if genDecl.Tok == token.IMPORT {
			continue
		}
		if genDecl.Tok != token.VAR || !isGoEmbedVarDecl(genDecl, embedImport, file, fileSet, source) {
			return false
		}
		found = true
	}
	return found
}

func isGoEmbedVarDecl(genDecl *ast.GenDecl, embedImport goEmbedImport, file *ast.File, fileSet *token.FileSet, source []byte) bool {
	grouped := genDecl.Lparen.IsValid()
	for _, spec := range genDecl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok || !isGoEmbedValueSpec(valueSpec, embedImport) {
			return false
		}
		anchor := genDecl.TokPos
		if grouped {
			anchor = valueSpec.Pos()
		}
		if !hasGoEmbedDirective(file.Comments, fileSet, source, anchor) {
			return false
		}
	}
	return true
}

func isGoEmbedValueSpec(valueSpec *ast.ValueSpec, embedImport goEmbedImport) bool {
	return len(valueSpec.Names) == 1 &&
		valueSpec.Names[0].Name != "_" &&
		len(valueSpec.Values) == 0 &&
		goEmbedSupportedType(valueSpec.Type, embedImport)
}

func soleGoEmbedImport(file *ast.File) (goEmbedImport, bool) {
	if len(file.Imports) != 1 {
		return goEmbedImport{}, false
	}
	spec := file.Imports[0]
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil || importPath != "embed" {
		return goEmbedImport{}, false
	}
	if spec.Name == nil {
		return goEmbedImport{alias: "embed"}, true
	}
	switch spec.Name.Name {
	case ".":
		return goEmbedImport{dot: true}, true
	case "_":
		return goEmbedImport{blank: true}, true
	default:
		return goEmbedImport{alias: spec.Name.Name}, true
	}
}

func goEmbedSupportedType(expr ast.Expr, embedImport goEmbedImport) bool {
	switch expr := unwrapGoParens(expr).(type) {
	case *ast.SelectorExpr:
		return goEmbedFSSelector(expr, embedImport)
	case *ast.Ident:
		return expr.Obj == nil && (expr.Name == "string" || embedImport.dot && expr.Name == "FS")
	case *ast.ArrayType:
		return goEmbedByteSlice(expr)
	default:
		return false
	}
}

func goEmbedFSSelector(selector *ast.SelectorExpr, embedImport goEmbedImport) bool {
	packageIdent, ok := selector.X.(*ast.Ident)
	return ok && !embedImport.dot && !embedImport.blank &&
		packageIdent.Name == embedImport.alias && packageIdent.Obj == nil &&
		selector.Sel.Name == "FS" && selector.Sel.Obj == nil
}

func goEmbedByteSlice(array *ast.ArrayType) bool {
	if array.Len != nil {
		return false
	}
	element, ok := unwrapGoParens(array.Elt).(*ast.Ident)
	return ok && element.Obj == nil && (element.Name == "byte" || element.Name == "uint8")
}

func unwrapGoParens(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func hasGoEmbedDirective(comments []*ast.CommentGroup, fileSet *token.FileSet, source []byte, anchor token.Pos) bool {
	anchorOffset := fileSet.Position(anchor).Offset
	groupIndex := sort.Search(len(comments), func(index int) bool {
		return fileSet.Position(comments[index].End()).Offset > anchorOffset
	}) - 1
	if groupIndex < 0 {
		return false
	}
	laterStart := anchorOffset
	for index := groupIndex; index >= 0; index-- {
		found, stop := scanGoEmbedCommentGroup(comments[index], fileSet, source, laterStart)
		if found {
			return true
		}
		if stop {
			return false
		}
		laterStart = fileSet.Position(comments[index].Pos()).Offset
	}
	return false
}

func scanGoEmbedCommentGroup(group *ast.CommentGroup, fileSet *token.FileSet, source []byte, laterStart int) (bool, bool) {
	barrier := false
	laterCommentStart := laterStart
	for commentIndex := len(group.List) - 1; commentIndex >= 0; commentIndex-- {
		comment := group.List[commentIndex]
		start := fileSet.Position(comment.Pos()).Offset
		end := fileSet.Position(comment.End()).Offset
		if !goSourceWhitespace(source, end, laterCommentStart) {
			return false, true
		}
		raw := source[start:end]
		if bytes.HasPrefix(raw, []byte("/*")) {
			barrier = true
			laterCommentStart = start
			continue
		}
		if !barrier && goEmbedDirectiveLine(source, start, raw) {
			return true, true
		}
		laterCommentStart = start
	}
	return false, barrier
}

func goSourceWhitespace(source []byte, start, end int) bool {
	if start < 0 || end < start || end > len(source) {
		return false
	}
	return len(bytes.TrimSpace(source[start:end])) == 0
}

func goEmbedDirectiveLine(source []byte, start int, raw []byte) bool {
	if !bytes.HasPrefix(raw, []byte("//go:embed ")) {
		return false
	}
	lineStart := bytes.LastIndex(source[:start], []byte{'\n'}) + 1
	for _, char := range source[lineStart:start] {
		if char != ' ' && char != '\t' {
			return false
		}
	}
	return len(bytes.TrimSpace(raw[len("//go:embed "):])) != 0
}

func nodeLOC(fileSet *token.FileSet, node ast.Node) int {
	start := fileSet.Position(node.Pos()).Line
	end := fileSet.Position(node.End()).Line
	if end < start {
		return 0
	}
	return end - start + 1
}

func lineMetrics(source string) (int, int) {
	total := 0
	logic := 0
	var state goLineState
	for line := range strings.SplitSeq(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		total++
		if state.isLogic(trimmed) {
			logic++
		}
	}
	return total, logic
}

type goLineState struct {
	inBlockComment bool
	inImportBlock  bool
}

func (s *goLineState) isLogic(line string) bool {
	if s.inImportBlock {
		s.inImportBlock = line != ")"
		return false
	}
	if s.inBlockComment {
		s.inBlockComment = !strings.Contains(line, "*/")
		return false
	}
	if strings.HasPrefix(line, "/*") {
		s.inBlockComment = !strings.Contains(line, "*/")
		return false
	}
	if line == "import (" {
		s.inImportBlock = true
		return false
	}
	return !isGoStructuralLine(line)
}

func isGoStructuralLine(line string) bool {
	return strings.HasPrefix(line, "//") ||
		strings.HasPrefix(line, "package ") ||
		strings.HasPrefix(line, "import ") ||
		strings.HasPrefix(line, "type ") ||
		line == ")" || line == "{" || line == "}"
}

func goImportName(spec *ast.ImportSpec) string {
	if spec.Name != nil && spec.Name.Name != "" {
		return spec.Name.Name
	}
	return path.Base(strings.Trim(spec.Path.Value, `"`))
}

func goImportMetric(file *ast.File) ImportMetric {
	names := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		names = append(names, goImportName(spec))
	}
	usedNames := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := selector.X.(*ast.Ident); ok {
			usedNames[ident.Name] = true
		}
		return true
	})
	used := 0
	unused := make([]string, 0)
	for _, name := range names {
		if name == "_" || name == "." || usedNames[name] {
			used++
			continue
		}
		unused = append(unused, name)
	}
	return ImportMetric{
		Total:  len(names),
		Used:   used,
		Unused: unused,
		DDC:    ratio(used, len(names)),
	}
}

// DiscoverGoFiles returns non-generated Go files under root.
func DiscoverGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".worktrees", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") &&
			!strings.HasSuffix(entry.Name(), "_generated.go") &&
			!strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, current)
		}
		return nil
	})
	return files, err
}
