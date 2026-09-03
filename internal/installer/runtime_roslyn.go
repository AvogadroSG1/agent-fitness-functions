package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RepairRoslynAnalyzer builds the Roslyn analyzer from source and installs it
// to the managed share directory under root.
func RepairRoslynAnalyzer(root, assetDir string) error {
	dotnetPath, err := findDotnet()
	if err != nil {
		return fmt.Errorf("cannot repair Roslyn analyzer: %w", err)
	}
	projPath, err := findRoslynProject(assetDir)
	if err != nil {
		return fmt.Errorf("cannot repair Roslyn analyzer: %w", err)
	}

	targetDir := filepath.Join(root, "current", "share", "roslyn-analyzer")
	if version, err := readCurrentVersion(root); err == nil && version != "" {
		targetDir = filepath.Join(root, "versions", version, "share", "roslyn-analyzer")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create roslyn-analyzer share directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, dotnetPath, "publish", "-c", "Release", "-o", targetDir, projPath)
	if dotnetRoot := os.Getenv("DOTNET_ROOT"); dotnetRoot != "" {
		cmd.Env = append(os.Environ(), "DOTNET_ROOT="+dotnetRoot)
	} else if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(dotnetPath, filepath.Join(home, ".dotnet")) {
		cmd.Env = append(os.Environ(), "DOTNET_ROOT="+filepath.Join(home, ".dotnet"))
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("building Roslyn analyzer: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	binaryName := "CalmRoslynAnalyzer"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(targetDir, binaryName)
	if info, err := os.Stat(binaryPath); err != nil || info.IsDir() {
		return fmt.Errorf("published binary %s not found: %w", binaryPath, err)
	} else if info.Mode()&0o111 == 0 {
		_ = os.Chmod(binaryPath, 0o755)
	}

	return nil
}

func findDotnet() (string, error) {
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p, nil
	}
	if dotnetRoot := os.Getenv("DOTNET_ROOT"); dotnetRoot != "" {
		p := filepath.Join(dotnetRoot, "dotnet")
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	if home := os.Getenv("HOME"); home != "" {
		p := filepath.Join(home, ".dotnet", "dotnet")
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("dotnet SDK executable not found on PATH or in DOTNET_ROOT")
}

func findRoslynProject(assetDir string) (string, error) {
	if assetDir != "" {
		candidates := []string{
			filepath.Join(assetDir, "roslyn-analyzer", "CalmRoslynAnalyzer.csproj"),
			filepath.Join(assetDir, "..", "..", "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		for cur := cwd; ; cur = filepath.Dir(cur) {
			proj := filepath.Join(cur, "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj")
			if _, err := os.Stat(proj); err == nil {
				return proj, nil
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
		}
	}
	candidates := []string{
		filepath.Join("tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj"),
		filepath.Join("..", "..", "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("could not find tools/roslyn-analyzer/CalmRoslynAnalyzer.csproj")
}
