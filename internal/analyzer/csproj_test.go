package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindNearestCsprojReturnsFileInSameDir(t *testing.T) {
	dir := t.TempDir()
	csproj := filepath.Join(dir, "src", "MyApp.csproj")
	if err := os.MkdirAll(filepath.Dir(csproj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csproj, []byte("<Project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	csFile := filepath.Join(dir, "src", "Widget.cs")
	got, err := FindNearestCsproj(csFile, dir)
	if err != nil {
		t.Fatalf("FindNearestCsproj returned error: %v", err)
	}
	if got != csproj {
		t.Fatalf("FindNearestCsproj = %q, want %q", got, csproj)
	}
}

func TestFindNearestCsprojReturnsFileInParentDir(t *testing.T) {
	dir := t.TempDir()
	csproj := filepath.Join(dir, "MyApp.csproj")
	if err := os.WriteFile(csproj, []byte("<Project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	csFile := filepath.Join(dir, "src", "deep", "Widget.cs")
	if err := os.MkdirAll(filepath.Dir(csFile), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindNearestCsproj(csFile, dir)
	if err != nil {
		t.Fatalf("FindNearestCsproj returned error: %v", err)
	}
	if got != csproj {
		t.Fatalf("FindNearestCsproj = %q, want %q", got, csproj)
	}
}

func TestFindNearestCsprojReturnsEmptyWhenNoneFound(t *testing.T) {
	dir := t.TempDir()
	csFile := filepath.Join(dir, "src", "Widget.cs")
	if err := os.MkdirAll(filepath.Dir(csFile), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindNearestCsproj(csFile, dir)
	if err != nil {
		t.Fatalf("FindNearestCsproj returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("FindNearestCsproj = %q, want empty string when no .csproj exists", got)
	}
}

func TestFindNearestCsprojDoesNotEscapeRoot(t *testing.T) {
	dir := t.TempDir()
	csFile := filepath.Join(dir, "src", "Widget.cs")
	if err := os.MkdirAll(filepath.Dir(csFile), 0o755); err != nil {
		t.Fatal(err)
	}
	subRoot := filepath.Join(dir, "src")
	got, err := FindNearestCsproj(csFile, subRoot)
	if err != nil {
		t.Fatalf("FindNearestCsproj returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("FindNearestCsproj = %q, want empty string when .csproj is outside root", got)
	}
}
