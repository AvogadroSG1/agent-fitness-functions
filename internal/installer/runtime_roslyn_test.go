package installer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/roslyntest"
)

func TestCheckRoslynAnalyzerHealth(t *testing.T) {
	root := t.TempDir()

	// Missing analyzer
	missing := checkRoslynAnalyzerHealth(root)
	if missing.healthy {
		t.Fatalf("expected missing analyzer to be unhealthy, got: %+v", missing)
	}
	if !missing.repairable {
		t.Fatalf("expected missing analyzer to be repairable, got: %+v", missing)
	}

	// Present analyzer in current/share/roslyn-analyzer
	shareDir := filepath.Join(root, "current", "share", "roslyn-analyzer")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fakeExe := filepath.Join(shareDir, "CalmRoslynAnalyzer")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\necho '{}'"), 0o755); err != nil {
		t.Fatalf("write fake exe: %v", err)
	}

	present := checkRoslynAnalyzerHealth(root)
	if !present.healthy {
		t.Fatalf("expected present analyzer to be healthy, got: %+v", present)
	}
}

func TestRepairRoslynAnalyzer_CompilesWhenDotnetAvailable(t *testing.T) {
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	assetRoot := t.TempDir()
	managedRoot := t.TempDir()
	if err := roslyntest.CopyProject(repoRoot, filepath.Join(assetRoot, "roslyn-analyzer")); err != nil {
		t.Fatalf("copy Roslyn project: %v", err)
	}
	if err := RepairRoslynAnalyzer(managedRoot, assetRoot); err != nil {
		t.Fatalf("RepairRoslynAnalyzer failed: %v", err)
	}

	health := checkRoslynAnalyzerHealth(managedRoot)
	if !health.healthy {
		t.Fatalf("expected Roslyn analyzer to be healthy after repair: %+v", health)
	}

	source := filepath.Join(t.TempDir(), "Example.cs")
	if err := os.WriteFile(source, []byte("public class Example { public int Choose(bool flag) { if (flag) return 1; return 0; } }"), 0o644); err != nil {
		t.Fatalf("write C# source: %v", err)
	}
	executable := filepath.Join(managedRoot, "current", "share", "roslyn-analyzer", "CalmRoslynAnalyzer")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	output, err := exec.Command(executable, source).CombinedOutput()
	if err != nil {
		t.Fatalf("run published Roslyn analyzer: %v\n%s", err, output)
	}
	var result struct {
		Functions []struct {
			Name                 string `json:"name"`
			CyclomaticComplexity int    `json:"cyclomatic_complexity"`
			IsPublic             bool   `json:"is_public"`
		} `json:"functions"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode Roslyn output: %v\n%s", err, output)
	}
	for _, function := range result.Functions {
		if function.Name == "Choose" {
			if !function.IsPublic || function.CyclomaticComplexity != 2 {
				t.Fatalf("Choose metrics = %+v, want public complexity 2", function)
			}
			return
		}
	}
	t.Fatalf("Roslyn output missing Choose method: %s", output)
}
