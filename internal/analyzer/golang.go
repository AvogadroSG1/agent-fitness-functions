package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
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
		if ast.IsExported(name) {
			publicMethods++
		}
		stat, ok := statsByLine[fileSet.Position(fn.Pos()).Line]
		if !ok {
			continue
		}
		functions = append(functions, FunctionMetric{
			Name:                 name,
			CyclomaticComplexity: stat.Complexity,
			IsPublic:             ast.IsExported(name),
			LOC:                  nodeLOC(fileSet, fn),
		})
	}

	totalLOC, logicLOC := lineMetrics(string(source))
	importMetric := goImportMetric(parsed)
	return AnalysisResult{
		CALMNode:  parsed.Name.Name,
		Language:  "go",
		File:      file,
		Functions: functions,
		FileMetric: FileMetric{
			TotalLOC:      totalLOC,
			LogicLOC:      logicLOC,
			PublicMethods: publicMethods,
			LDR:           ratio(logicLOC, totalLOC),
		},
		Imports: importMetric,
	}, nil
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
	lines := strings.Split(source, "\n")
	total := 0
	logic := 0
	inBlockComment := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		total++
		if inBlockComment {
			if strings.Contains(trimmed, "*/") {
				inBlockComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			if !strings.Contains(trimmed, "*/") {
				inBlockComment = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "package ") ||
			strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "type ") ||
			trimmed == "import (" ||
			trimmed == ")" ||
			trimmed == "{" ||
			trimmed == "}" {
			continue
		}
		logic++
	}
	return total, logic
}

func goImportMetric(file *ast.File) ImportMetric {
	names := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "" {
			name = path.Base(strings.Trim(spec.Path.Value, `"`))
		}
		names = append(names, name)
	}
	usedNames := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
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
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_generated.go") {
			files = append(files, current)
		}
		return nil
	})
	return files, err
}
