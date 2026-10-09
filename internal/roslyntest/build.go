// Package roslyntest owns immutable, process-private Roslyn test artifacts.
package roslyntest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	buildOnce    sync.Once
	executable   string
	buildError   error
	artifactRoot string
)

// Build compiles the repository's real analyzer once per test process.
// Every caller must supply the same source repository.
func Build(repoRoot string) (string, error) {
	buildOnce.Do(func() {
		root, err := filepath.Abs(repoRoot)
		if err != nil {
			buildError = fmt.Errorf("resolve Roslyn repository root: %w", err)
			return
		}
		artifactRoot, err = os.MkdirTemp("", "agent-fitness-functions-roslyn-")
		if err != nil {
			buildError = fmt.Errorf("create Roslyn artifact root: %w", err)
			return
		}
		projectDir := filepath.Join(root, "tools", "roslyn-analyzer")
		command := exec.Command("dotnet", "build", "-c", "Release", "--artifacts-path", artifactRoot, filepath.Join(projectDir, "CalmRoslynAnalyzer.csproj"))
		command.Dir = projectDir
		if output, err := command.CombinedOutput(); err != nil {
			buildError = fmt.Errorf("build private Roslyn analyzer: %w\n%s", err, output)
			return
		}
		path := filepath.Join(artifactRoot, "bin", "CalmRoslynAnalyzer", "release", "CalmRoslynAnalyzer")
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		info, err := os.Stat(path)
		if err != nil {
			buildError = fmt.Errorf("inspect private Roslyn executable: %w", err)
			return
		}
		if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
			buildError = fmt.Errorf("private Roslyn output %s is not a regular executable", path)
			return
		}
		executable = path
	})
	return executable, buildError
}

// Cleanup removes this process's artifacts after all tests have finished.
func Cleanup() error {
	if artifactRoot == "" {
		return nil
	}
	if err := os.RemoveAll(artifactRoot); err != nil {
		return fmt.Errorf("remove private Roslyn artifacts: %w", err)
	}
	artifactRoot = ""
	return nil
}

// CopyProject copies only real project inputs into a fresh caller-owned fixture.
func CopyProject(repoRoot, projectDir string) error {
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create Roslyn project fixture: %w", err)
	}
	for _, name := range []string{"CalmRoslynAnalyzer.csproj", "Program.cs"} {
		source := filepath.Join(repoRoot, "tools", "roslyn-analyzer", name)
		content, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read Roslyn project input %s: %w", source, err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, name), content, 0o644); err != nil {
			return fmt.Errorf("copy Roslyn project input %s: %w", name, err)
		}
	}
	return nil
}
