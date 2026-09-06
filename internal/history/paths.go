package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Location identifies a clone's history database and one worktree's context.
type Location struct {
	DatabasePath string
	CommonGitDir string
	Worktree     string
	File         string
	Branch       *string
	HeadOID      *string
}

// Resolve locates a real Git checkout without creating any history storage.
// Relative file paths are rooted at the worktree, even when checkout is nested.
// Proposed files need not exist; their normalized path MUST remain in the worktree.
func Resolve(ctx context.Context, checkout, file string) (Location, error) {
	location, err := ResolveCheckout(ctx, checkout)
	if err != nil {
		return Location{}, err
	}
	location.File, err = relativeFile(location.Worktree, file)
	if err != nil {
		return Location{}, err
	}
	location.Branch, err = optionalGitRef(ctx, checkout, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return Location{}, fmt.Errorf("resolve history branch: %w", err)
	}
	location.HeadOID, err = optionalGitRef(ctx, checkout, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return Location{}, fmt.Errorf("resolve history HEAD: %w", err)
	}
	return location, nil
}

// ResolveCheckout locates clone storage for readers without requiring a file or
// collecting validation-only branch and HEAD metadata. It MUST NOT create files.
func ResolveCheckout(ctx context.Context, checkout string) (Location, error) {
	inside, err := gitOutput(ctx, checkout, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return Location{}, fmt.Errorf("resolve history checkout: %w", err)
	}
	if inside != "true" {
		return Location{}, errors.New("history requires a Git worktree")
	}
	worktree, err := gitOutput(ctx, checkout, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return Location{}, fmt.Errorf("resolve history worktree: %w", err)
	}
	common, err := gitOutput(ctx, checkout, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Location{}, fmt.Errorf("resolve history common Git directory: %w", err)
	}
	if !filepath.IsAbs(worktree) || !filepath.IsAbs(common) {
		return Location{}, errors.New("Git returned a relative history location")
	}
	return Location{
		DatabasePath: filepath.Join(common, "agent-fitness-functions", "history.sqlite3"),
		CommonGitDir: filepath.Clean(common), Worktree: filepath.Clean(worktree),
	}, nil
}

// FileKey normalizes a file filter using the same rules as captured proposals.
// Historical files and their parent directories MAY have been deleted.
func FileKey(worktree, file string) (string, error) {
	return relativeFile(worktree, file)
}

// WorktreeKey normalizes a worktree filter without requiring a live checkout.
// Existing ancestors resolve filesystem aliases; deleted worktrees remain usable.
func WorktreeKey(worktree string) (string, error) {
	absolute, err := filepath.Abs(worktree)
	if err != nil {
		return "", err
	}
	return proposalPath(absolute)
}

func validateFileName(file string) error {
	if file == "" {
		return errors.New("history file is missing")
	}
	name := file[strings.LastIndexByte(file, filepath.Separator)+1:]
	if name == "" || name == "." || name == ".." {
		return errors.New("history path must identify a file")
	}
	return nil
}

func relativeFile(worktree, file string) (string, error) {
	if err := validateFileName(file); err != nil {
		return "", err
	}
	if !filepath.IsAbs(file) {
		// Joining lexically would erase link/.. before resolving the link.
		file = worktree + string(filepath.Separator) + file
	}
	canonical, err := proposalPath(file)
	if err != nil {
		return "", fmt.Errorf("resolve history file path: %w", err)
	}
	file, err = filepath.Rel(worktree, canonical)
	if err != nil {
		return "", fmt.Errorf("resolve history file: %w", err)
	}
	if !filepath.IsLocal(file) || file == "." {
		return "", errors.New("history file must be inside the worktree")
	}
	return filepath.ToSlash(file), nil
}

// proposalPath resolves existing ancestors so aliases such as macOS /var and
// /private/var agree with Git, while allowing proposed files to be absent.
func proposalPath(file string) (string, error) {
	ancestor := file
	var suffix []string
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent, name, err := splitMissingComponent(ancestor)
		if err != nil {
			return "", err
		}
		if name != "" {
			suffix = append(suffix, name)
		}
		ancestor = parent
	}
	canonical, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		canonical = filepath.Join(canonical, suffix[i])
	}
	return canonical, nil
}

// splitMissingComponent preserves raw ancestor traversal for EvalSymlinks.
func splitMissingComponent(file string) (string, string, error) {
	separator := strings.LastIndexByte(file, filepath.Separator)
	if separator < 0 {
		return "", "", errors.New("history file has no resolvable ancestor")
	}
	name := file[separator+1:]
	if name == "." || name == ".." {
		return "", "", errors.New("history file traverses a missing ancestor")
	}
	parent := file[:separator+1]
	if separator > 0 {
		parent = file[:separator]
	}
	return parent, name, nil
}

func optionalGitRef(ctx context.Context, checkout string, args ...string) (*string, error) {
	value, err := gitOutput(ctx, checkout, args...)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		// Git uses exit 1 for detached symbolic refs and an unborn HEAD.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func gitOutput(ctx context.Context, checkout string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = checkout
	for _, entry := range os.Environ() {
		// Hooks may export these for their repository. The supplied checkout
		// MUST determine history identity independently of that environment.
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}
