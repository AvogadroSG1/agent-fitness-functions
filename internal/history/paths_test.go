package history

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHistoryPathsShareCloneAcrossWorktrees(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	gitHistory(t, root, "init", repo)
	gitHistory(t, repo, "-c", "user.name=History Test", "-c", "user.email=history@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	linked := filepath.Join(root, "linked")
	gitHistory(t, repo, "worktree", "add", "--detach", linked)
	main, err := Resolve(context.Background(), repo, "src/example.go")
	if err != nil {
		t.Fatal(err)
	}
	other, err := Resolve(context.Background(), linked, "src/example.go")
	if err != nil {
		t.Fatal(err)
	}
	if main.DatabasePath != other.DatabasePath {
		t.Errorf("worktree database = %q, want %q", other.DatabasePath, main.DatabasePath)
	}
	if other.Branch != nil || other.HeadOID == nil {
		t.Errorf("detached context = %+v, want no branch and known HEAD", other)
	}
	if main.File != "src/example.go" || main.Worktree == other.Worktree {
		t.Errorf("unexpected worktree/file context: main=%+v other=%+v", main, other)
	}
	clone := filepath.Join(root, "clone")
	gitHistory(t, root, "clone", repo, clone)
	separate, err := Resolve(context.Background(), clone, "src/example.go")
	if err != nil {
		t.Fatal(err)
	}
	if separate.DatabasePath == main.DatabasePath {
		t.Error("separate clone shares history database")
	}
	if _, err := os.Stat(main.DatabasePath); !os.IsNotExist(err) {
		t.Errorf("path resolution created storage: %v", err)
	}
}

func TestHistoryPathsRejectNonCheckoutAndEscapingFile(t *testing.T) {
	root := t.TempDir()
	if _, err := Resolve(context.Background(), root, "example.go"); err == nil {
		t.Error("ordinary directory accepted as Git checkout")
	}
	gitHistory(t, root, "init")
	unborn, err := Resolve(context.Background(), root, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	if unborn.HeadOID != nil {
		t.Errorf("unborn HEAD = %v, want unknown", unborn.HeadOID)
	}
	if _, err := Resolve(context.Background(), root, "../outside.go"); err == nil {
		t.Error("file outside checkout accepted")
	}
}

func gitHistory(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
