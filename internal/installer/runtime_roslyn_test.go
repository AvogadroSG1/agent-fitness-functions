package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

	root := t.TempDir()
	err := RepairRoslynAnalyzer(root, "")
	if err != nil {
		t.Fatalf("RepairRoslynAnalyzer failed: %v", err)
	}

	health := checkRoslynAnalyzerHealth(root)
	if !health.healthy {
		t.Fatalf("expected Roslyn analyzer to be healthy after repair: %+v", health)
	}
}
