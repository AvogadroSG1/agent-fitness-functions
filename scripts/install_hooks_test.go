package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooksIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	script := filepath.Join("install-hooks.sh")

	for range 2 {
		command := exec.Command("bash", script, repo)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("install-hooks failed: %v\n%s", err, output)
		}
		if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-commit"), []byte("# CALM pre-commit hook\nstale hook\n"), 0o644); err != nil {
			t.Fatalf("write stale hook: %v", err)
		}
	}
	command := exec.Command("bash", script, repo)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("final install-hooks failed: %v\n%s", err, output)
	}

	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf("stat installed hook: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("installed hook mode = %v, want executable", info.Mode())
	}
	content, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read installed hook: %v", err)
	}
	if string(content) == "# CALM pre-commit hook\nstale hook\n" {
		t.Fatal("installer did not overwrite stale hook content")
	}
}

func TestInstallHooksRefusesExistingNonCalmHook(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	command := exec.Command("bash", "install-hooks.sh", repo)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("install-hooks succeeded, want refusal; output=%s", output)
	}
	if !strings.Contains(string(output), "refusing to overwrite") {
		t.Fatalf("output = %s, want overwrite refusal", output)
	}
	content, err := os.ReadFile(existingHook)
	if err != nil {
		t.Fatalf("read existing hook: %v", err)
	}
	if !strings.Contains(string(content), "echo custom") {
		t.Fatalf("existing hook was replaced: %s", content)
	}
}

func TestInstallHooksSupportsWorktrees(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, mainRepo, "init")
	runGit(t, mainRepo, "config", "user.email", "test@example.com")
	runGit(t, mainRepo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGit(t, mainRepo, "add", "README.md")
	runGit(t, mainRepo, "commit", "-m", "init")
	runGit(t, mainRepo, "worktree", "add", worktree)

	command := exec.Command("bash", "install-hooks.sh", worktree)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install-hooks failed for worktree: %v\n%s", err, output)
	}
	hookPathBytes, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-path", "hooks/pre-commit").Output()
	if err != nil {
		t.Fatalf("resolve worktree hook path: %v", err)
	}
	hookPath := strings.TrimSpace(string(hookPathBytes))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(worktree, hookPath)
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("stat worktree hook at %s: %v", hookPath, err)
	}
}

func TestInstallHooksAppendModeInstallsSidecar(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	command := exec.Command("bash", "install-hooks.sh", repo)
	command.Env = append(os.Environ(), "CALM_HOOK_APPEND=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install-hooks failed in append mode: %v\n%s", err, output)
	}

	sidecar := filepath.Join(repo, ".git", "hooks", "calm-pre-commit")
	info, err := os.Stat(sidecar)
	if err != nil {
		t.Fatalf("sidecar not found at %s: %v", sidecar, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("sidecar mode = %v, want executable", info.Mode())
	}

	existing, err := os.ReadFile(existingHook)
	if err != nil {
		t.Fatalf("read existing hook: %v", err)
	}
	if !strings.Contains(string(existing), "echo custom") {
		t.Fatalf("existing hook content was replaced:\n%s", existing)
	}
	if !strings.Contains(string(existing), "# CALM pre-commit hook (sidecar)") {
		t.Fatalf("existing hook missing sidecar call block:\n%s", existing)
	}
}

func TestInstallHooksAppendModeIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	for range 2 {
		command := exec.Command("bash", "install-hooks.sh", repo)
		command.Env = append(os.Environ(), "CALM_HOOK_APPEND=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("install-hooks failed in append mode: %v\n%s", err, output)
		}
	}

	existing, err := os.ReadFile(existingHook)
	if err != nil {
		t.Fatalf("read existing hook: %v", err)
	}
	count := strings.Count(string(existing), "# CALM pre-commit hook (sidecar)")
	if count != 1 {
		t.Fatalf("sidecar call block appears %d times, want exactly 1:\n%s", count, existing)
	}
}

func TestInstallHooksRefusalMentionsAppendOption(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	existingHook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(existingHook, []byte("#!/usr/bin/env bash\necho custom\n"), 0o755); err != nil {
		t.Fatalf("write existing hook: %v", err)
	}

	command := exec.Command("bash", "install-hooks.sh", repo)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("install-hooks succeeded, want refusal")
	}
	if !strings.Contains(string(output), "CALM_HOOK_APPEND=1") {
		t.Fatalf("refusal message does not mention CALM_HOOK_APPEND=1:\n%s", output)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
