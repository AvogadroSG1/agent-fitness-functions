package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrePushReadsCommitContentWithRepoScopedGitShow(t *testing.T) {
	_, worktree := initNamedWorktree(t)
	writeFile(t, filepath.Join(worktree, "sample.go"), "package sample\n")
	runGit(t, worktree, "add", "sample.go")
	runGit(t, worktree, "commit", "-m", "add sample")
	headSHA := gitRevParse(t, worktree, "HEAD")
	baseSHA := gitRevParse(t, worktree, "HEAD~1")

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	gitShimDir := t.TempDir()
	gitShimPath := filepath.Join(gitShimDir, "git")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("locate git: %v", err)
	}
	if err := os.WriteFile(gitShimPath, []byte(fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "show" ]]; then
  echo "expected repo-scoped git show" >&2
  exit 97
fi
if [[ "${1:-}" == "-C" && "${3:-}" == "show" ]]; then
  printf 'git %%s\n' "$*" >> "%s"
fi
exec "%s" "$@"
`, logPath, realGit)), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}

	command := exec.Command("bash", hookScriptPathFor(t, "pre-push.sh"))
	command.Dir = worktree
	command.Stdin = strings.NewReader("refs/heads/main " + headSHA + " refs/heads/main " + baseSHA + "\n")
	command.Env = append(os.Environ(),
		"PATH="+gitShimDir+string(os.PathListSeparator)+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-push failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	if !strings.Contains(got, "git -C "+worktree+" show") {
		t.Fatalf("expected repo-scoped git show in log:\n%s", got)
	}
}

func TestPrePushLocalModeSendsLogicalRepoNameAndGitDir(t *testing.T) {
	_, worktree := initNamedWorktree(t)
	writeFile(t, filepath.Join(worktree, "sample.go"), "package sample\n")
	runGit(t, worktree, "add", "sample.go")
	runGit(t, worktree, "commit", "-m", "add sample")
	headSHA := gitRevParse(t, worktree, "HEAD")
	baseSHA := gitRevParse(t, worktree, "HEAD~1")

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPathFor(t, "pre-push.sh"))
	command.Dir = worktree
	command.Stdin = strings.NewReader("refs/heads/main " + headSHA + " refs/heads/main " + baseSHA + "\n")
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-push failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	for _, want := range []string{"--repo relocate", "--git-dir " + worktree} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in invocation:\n%s", want, got)
		}
	}
	if strings.Contains(got, "--repo "+worktree) || strings.Contains(got, "--repo feature+relocate-stats") {
		t.Errorf("logical repo name leaked the worktree path/dir name:\n%s", got)
	}
}

func gitRevParse(t *testing.T, repo, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", rev).CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPrePushForwardsDiscoveredMTLSCerts(t *testing.T) {
	repo := initGitRepo(t)
	// git rev-parse --show-toplevel resolves symlinks (macOS /var -> /private/var),
	// so resolve here too to match the cert paths the hook forwards.
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")

	writeFile(t, filepath.Join(repo, "README.md"), "init\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	baseSHA := gitRevParse(t, repo, "HEAD")

	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")
	runGit(t, repo, "commit", "-m", "add sample")
	headSHA := gitRevParse(t, repo, "HEAD")

	certDir := filepath.Join(repo, "certs")
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		writeFile(t, filepath.Join(certDir, name), "x")
	}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPathFor(t, "pre-push.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader("refs/heads/main " + headSHA + " refs/heads/main " + baseSHA + "\n")
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-push failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	for _, want := range []string{
		"--addr https://127.0.0.1:7890",
		"--client-cert " + filepath.Join(certDir, "client.crt"),
		"--client-key " + filepath.Join(certDir, "client.key"),
		"--client-ca " + filepath.Join(certDir, "ca.crt"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in invocations:\n%s", want, got)
		}
	}
}
