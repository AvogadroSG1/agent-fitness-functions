package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryPathsNormalizeProposalsWithoutCreatingStorage(t *testing.T) {
	repo := t.TempDir()
	gitHistory(t, repo, "init")
	nested := filepath.Join(repo, "src")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"src/../src/example.go", filepath.Join(repo, "src", "example.go")} {
		location, err := Resolve(context.Background(), nested, file)
		if err != nil {
			t.Fatal(err)
		}
		if location.File != "src/example.go" || !filepath.IsAbs(location.CommonGitDir) {
			t.Errorf("proposal location = %+v, want normalized file and absolute common directory", location)
		}
		want := filepath.Join(location.CommonGitDir, "agent-fitness-functions", "history.sqlite3")
		if location.DatabasePath != want {
			t.Errorf("database path = %q, want %q", location.DatabasePath, want)
		}
		if _, err := os.Stat(filepath.Dir(location.DatabasePath)); !os.IsNotExist(err) {
			t.Errorf("path resolution created a history directory: %v", err)
		}
	}
	for _, file := range []string{"", ".", "src/..", filepath.Dir(repo)} {
		if _, err := Resolve(context.Background(), repo, file); err == nil {
			t.Errorf("invalid file %q accepted", file)
		}
	}
}

func TestHistoryPathsRejectBareRepositoryAndHonorCancellation(t *testing.T) {
	root := t.TempDir()
	gitHistory(t, root, "init", "--bare")
	if _, err := Resolve(context.Background(), root, "example.go"); err == nil {
		t.Error("bare repository accepted as checkout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, root, "example.go"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled resolution error = %v, want context.Canceled", err)
	}
}

func TestHistoryPathsIgnoreInheritedGitLocation(t *testing.T) {
	repo, other := t.TempDir(), t.TempDir()
	gitHistory(t, repo, "init")
	gitHistory(t, other, "init")
	want, err := Resolve(context.Background(), repo, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_COMMON_DIR", filepath.Join(other, ".git"))
	got, err := Resolve(context.Background(), repo, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Worktree != want.Worktree || got.CommonGitDir != want.CommonGitDir {
		t.Errorf("inherited Git environment changed location: got %+v, want %+v", got, want)
	}
}

func TestHistoryPathsRejectProposalsBeneathEscapingSymlink(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	gitHistory(t, repo, "init")
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"link/proposal.go", filepath.Join(repo, "link", "proposal.go")} {
		if _, err := Resolve(context.Background(), repo, file); err == nil {
			t.Errorf("proposal beneath escaping symlink accepted: %q", file)
		}
	}
}

func TestHistoryPathsResolveSymlinksBeforeParentTraversal(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	gitHistory(t, repo, "init")
	child := filepath.Join(outside, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(child, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	// Concatenation preserves the traversal that filepath.Join would remove.
	for _, file := range []string{"link/../proposal.go", repo + "/link/../proposal.go"} {
		if _, err := Resolve(context.Background(), repo, file); err == nil {
			t.Errorf("parent traversal after escaping symlink accepted: %q", file)
		}
	}
}

func TestHistoryPathsRejectUnresolvableAncestors(t *testing.T) {
	repo := t.TempDir()
	gitHistory(t, repo, "init")
	if err := os.Symlink(filepath.Join(repo, "missing"), filepath.Join(repo, "dangling")); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"dangling/proposal.go", "missing/../proposal.go", "missing/./proposal.go", "proposal.go/", "proposal.go/."} {
		if _, err := Resolve(context.Background(), repo, file); err == nil {
			t.Errorf("unresolvable proposal path accepted: %q", file)
		}
	}
}
