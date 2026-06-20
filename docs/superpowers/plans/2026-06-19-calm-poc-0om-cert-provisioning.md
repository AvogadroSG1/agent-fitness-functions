# calm-poc-0om Dev Cert Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `stack-fitness-functions client install-hooks` provision trusted developer mTLS material into the target repository’s `certs/` path without copying private keys into tracked files, so fresh repos and git worktrees can validate against the HTTPS loopback container with no manual certificate step.

**Architecture:** Reuse the hook runtime’s existing `<repo>/certs` auto-discovery instead of inventing new hook flags. During install, discover a shared trusted dev cert source from `STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR` first, then `STACK_FITNESS_FUNCTIONS_SRC/certs`, then an executable-adjacent repo root `certs/` directory when available. Provision the target repo’s `certs` path as a shared pointer (symlink) to that source, and add a repo-local ignore entry for `certs/` so the key material never appears as tracked content. Keep repo-name mismatch concerns out of this bead; `calm-poc-cyd` already owns the logical-name split.

**Tech Stack:** Go, `go test`, Git path resolution, symlinks, repo-local ignore management, Markdown docs

## Global Constraints

- `install-hooks` MUST NOT write host/secret/cert path config into tracked `.claude/settings.json`; that file remains reserved for the git guard entry.
- TDD is REQUIRED: failing test first, then minimal implementation, then refactor.
- Prefer a shared pointer over copying private keys into each repo.
- The target repo’s `certs/` path MUST become ignored automatically.
- The solution MUST work when called on a git worktree path.
- The solution MUST reuse an existing trusted dev cert chain; it MUST NOT run `scripts/generate-dev-certs.sh` or mint a fresh CA.
- Existing hook runtime behavior in `hooks/pre-commit.sh` and `hooks/pre-push.sh` MUST remain unchanged.

---

### Task 1: Add Failing Tests for Fresh Repo and Worktree Provisioning

**Files:**
- Modify: `internal/client/client_test.go`
- Verify: `internal/client/client_test.go`

- [ ] **Step 1: Write the failing tests**

Add this coverage near the existing install-hooks tests:

```go
func TestRunInstallHooksProvisionsSharedDevCerts(t *testing.T) {
	sourceRepo := t.TempDir()
	sourceCerts := filepath.Join(sourceRepo, "certs")
	if err := os.MkdirAll(sourceCerts, 0o755); err != nil {
		t.Fatalf("mkdir source certs: %v", err)
	}
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		if err := os.WriteFile(filepath.Join(sourceCerts, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	t.Setenv("STACK_FITNESS_FUNCTIONS_SRC", sourceRepo)

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	certsPath := filepath.Join(repo, "certs")
	target, err := os.Readlink(certsPath)
	if err != nil {
		t.Fatalf("readlink certs: %v", err)
	}
	if target != sourceCerts {
		t.Fatalf("certs symlink = %q, want %q", target, sourceCerts)
	}

	excludePath, err := gitOutput(repo, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		t.Fatalf("resolve info/exclude: %v", err)
	}
	excludeContent, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read info/exclude: %v", err)
	}
	if !strings.Contains(string(excludeContent), "certs/") {
		t.Fatalf("info/exclude missing certs/ entry:\n%s", excludeContent)
	}
}

func TestRunInstallHooksProvisionsSharedDevCertsForWorktree(t *testing.T) {
	sourceRepo := t.TempDir()
	sourceCerts := filepath.Join(sourceRepo, "certs")
	if err := os.MkdirAll(sourceCerts, 0o755); err != nil {
		t.Fatalf("mkdir source certs: %v", err)
	}
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		if err := os.WriteFile(filepath.Join(sourceCerts, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	mainRepo := filepath.Join(t.TempDir(), "relocate")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	runGitClientTest(t, mainRepo, "init")
	runGitClientTest(t, mainRepo, "config", "user.email", "t@example.com")
	runGitClientTest(t, mainRepo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitClientTest(t, mainRepo, "add", "README.md")
	runGitClientTest(t, mainRepo, "commit", "-m", "init")

	worktree := filepath.Join(t.TempDir(), "feature+relocate-stats")
	runGitClientTest(t, mainRepo, "worktree", "add", "-b", "feature/relocate-stats", worktree)
	t.Setenv("STACK_FITNESS_FUNCTIONS_SRC", sourceRepo)

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{worktree}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	target, err := os.Readlink(filepath.Join(worktree, "certs"))
	if err != nil {
		t.Fatalf("readlink worktree certs: %v", err)
	}
	if target != sourceCerts {
		t.Fatalf("worktree certs symlink = %q, want %q", target, sourceCerts)
	}
}

func TestRunInstallHooksProvisioningIsIdempotent(t *testing.T) {
	sourceRepo := t.TempDir()
	sourceCerts := filepath.Join(sourceRepo, "certs")
	if err := os.MkdirAll(sourceCerts, 0o755); err != nil {
		t.Fatalf("mkdir source certs: %v", err)
	}
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		if err := os.WriteFile(filepath.Join(sourceCerts, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	t.Setenv("STACK_FITNESS_FUNCTIONS_SRC", sourceRepo)

	for range 2 {
		var stdout, stderr bytes.Buffer
		if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
			t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
		}
	}

	target, err := os.Readlink(filepath.Join(repo, "certs"))
	if err != nil {
		t.Fatalf("readlink certs: %v", err)
	}
	if target != sourceCerts {
		t.Fatalf("certs symlink = %q, want %q", target, sourceCerts)
	}

	excludePath, err := gitOutput(repo, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		t.Fatalf("resolve info/exclude: %v", err)
	}
	excludeContent, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read info/exclude: %v", err)
	}
	if count := strings.Count(string(excludeContent), "certs/"); count != 1 {
		t.Fatalf("info/exclude contains %d certs/ entries, want 1:\n%s", count, excludeContent)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./internal/client -run 'TestRunInstallHooksProvisionsSharedDevCerts|TestRunInstallHooksProvisionsSharedDevCertsForWorktree|TestRunInstallHooksProvisioningIsIdempotent' -count=1
```

Expected: `FAIL` because `RunInstallHooks` does not currently create `certs`, resolve a shared cert source, or write a local ignore entry.

- [ ] **Step 3: Write minimal implementation**

Extend `RunInstallHooks` with a cert-provisioning step and focused helpers:

```go
func RunInstallHooks(args []string, stdout, stderr io.Writer) error {
	// existing repo resolution...
	installer := hookInstaller{repoRoot: repoRoot, stdout: stdout, stderr: stderr}
	if err := installer.installGitHook("pre-commit", "hookassets/pre-commit.sh"); err != nil {
		return err
	}
	if err := installer.installGitHook("pre-push", "hookassets/pre-push.sh"); err != nil {
		return err
	}
	if err := installer.provisionDevCerts(); err != nil {
		return err
	}
	return installer.installGitGuard()
}

func (installer hookInstaller) provisionDevCerts() error {
	sourceDir, ok, err := installer.discoverDevCertSource()
	if err != nil || !ok {
		return err
	}
	if err := installer.ensureRepoCertsLink(sourceDir); err != nil {
		return err
	}
	return installer.ensureLocalIgnore("certs/")
}
```

Required helper behavior:

```go
func (installer hookInstaller) discoverDevCertSource() (string, bool, error) { ... }
func (installer hookInstaller) ensureRepoCertsLink(sourceDir string) error { ... }
func (installer hookInstaller) ensureLocalIgnore(pattern string) error { ... }
```

Discovery order MUST be:

1. `STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR`
2. `STACK_FITNESS_FUNCTIONS_SRC/certs`
3. executable-adjacent repo root `certs/` when that path contains `client.crt`, `client.key`, and `ca.crt`

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./internal/client -run 'TestRunInstallHooksProvisionsSharedDevCerts|TestRunInstallHooksProvisionsSharedDevCertsForWorktree|TestRunInstallHooksProvisioningIsIdempotent' -count=1
```

Expected: `PASS`

- [ ] **Step 5: Refactor to keep installer complexity small**

Extract tiny helpers instead of growing `RunInstallHooks` or `installGitHook`. Keep the new helpers focused on:

```go
func hasDevCertChain(dir string) bool { ... }
func localIgnorePath(repo string) (string, error) { ... }
func ensureLine(content []byte, line string) []byte { ... }
```

- [ ] **Step 6: Run the focused installer package**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./internal/client -count=1
```

Expected: `PASS`

- [ ] **Step 7: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "fix: provision shared dev certs during hook install" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>\nCo-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

### Task 2: Update Operator Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/runbooks/onboard-new-repository.md`

- [ ] **Step 1: Update the README install-hooks contract**

Revise the install-hooks documentation so it says install-hooks now provisions trusted dev cert access when a shared source is discoverable:

```md
`stack-fitness-functions client install-hooks [repo]`
Installs the embedded Git hooks, provisions repo-local access to the trusted dev
client cert chain at `<repo>/certs` when available, and registers the git guard.
```

- [ ] **Step 2: Update the onboarding runbook**

Replace the manual cert-step wording with the new contract:

```md
`client install-hooks` now handles the local developer cert path too. It links
`<repo>/certs` to the shared trusted `dev-hook-pool` chain when that source is
discoverable, then adds a repo-local ignore entry so the key material never
shows up as tracked content.
```

Also clarify that `STACK_FITNESS_FUNCTIONS_REPO_NAME` is still the override for logical repo names when the working tree basename differs from `configs/<repo-name>`.

- [ ] **Step 3: Verify documentation reflects the runtime contract**

Run:

```bash
rg -n "install-hooks|certs|STACK_FITNESS_FUNCTIONS_REPO_NAME" README.md docs/runbooks/onboard-new-repository.md
```

Expected: both documents describe install-hooks-driven cert provisioning and still call out repo-name override behavior.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/runbooks/onboard-new-repository.md
git commit -m "docs: document install-hooks cert provisioning" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>\nCo-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```
