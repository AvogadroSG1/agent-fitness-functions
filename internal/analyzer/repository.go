package analyzer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryOptions configures external analyzer tools for repository baselines.
type RepositoryOptions struct {
	RadonPath  string
	RoslynPath string
}

// AnalyzeRepository analyzes all supported source files for one language.
func AnalyzeRepository(ctx context.Context, root, language string, options RepositoryOptions) ([]AnalysisResult, error) {
	if language == "python" {
		return AnalyzePythonRepository(ctx, root, options.RadonPath)
	}
	files, err := discoverFiles(root, language)
	if err != nil {
		return nil, err
	}
	results := make([]AnalysisResult, 0, len(files))
	for _, file := range files {
		var result AnalysisResult
		switch language {
		case "go":
			result, err = AnalyzeGoFile(file)
		case "python":
			result, err = AnalyzePythonFile(ctx, file, options.RadonPath)
		case "csharp":
			csprojPath, _ := FindNearestCsproj(file, root)
			result, err = AnalyzeCSharpFileWithProject(ctx, file, csprojPath, options.RoslynPath)
		default:
			return nil, fmt.Errorf("unsupported language %q", language)
		}
		if err != nil {
			return nil, fmt.Errorf("analyzing %s: %w", file, err)
		}
		results = append(results, result)
	}
	return AggregateModuleMetrics(results), nil
}

func discoverFiles(root, language string) ([]string, error) {
	if language == "go" {
		return DiscoverGoFiles(root)
	}
	extension := map[string]string{
		"python": ".py",
		"csharp": ".cs",
	}[language]
	if extension == "" {
		return nil, fmt.Errorf("unsupported language %q", language)
	}
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".venv", ".pytest_cache", ".worktrees", "node_modules", "bin", "obj":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), extension) {
			files = append(files, current)
		}
		return nil
	})
	return files, err
}
