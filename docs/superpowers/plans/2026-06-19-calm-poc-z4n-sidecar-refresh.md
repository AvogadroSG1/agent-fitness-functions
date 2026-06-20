# calm-poc-z4n Sidecar Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `stack-fitness-functions client install-hooks` rewrite legacy sidecar references inside active Git hooks so upgraded repositories execute the new `stack-fitness-functions-*` sidecars instead of orphaned `calm-*` paths.

**Architecture:** Keep the fix inside the hook installer. `handleExistingGitHook` already detects both new and legacy sidecar markers, so the missing behavior is a targeted rewrite of the active hook file during `refreshHookSidecar`, followed by sidecar refresh and optional cleanup of the orphaned legacy sidecar file. Preserve append-mode custom hook bodies and the existing cyclomatic-complexity budget by extracting small helpers instead of expanding `refreshHookSidecar` inline.

**Tech Stack:** Go, `go test`, embedded hook assets, Git hook installer logic in `internal/client`

## Global Constraints

- `stack-fitness-functions` MUST remain the product and hook prefix; FINOS CALM naming survives only where the project already preserves it.
- TDD is REQUIRED: every production change MUST start with a failing test, then minimal code, then refactor.
- The fix MUST work for both `pre-commit` and `pre-push`, because `installGitHook` shares the same refresh path.
- The active hook file MUST stop referencing `calm-<hook>` absolute paths after refresh.
- Legacy sidecar files MAY be deleted after a successful rewrite, but they MUST at minimum become unreferenced.
- Existing append-mode custom hook bodies MUST remain intact.
- `internal/client/client_test.go` complexity guardrails MUST stay green.

---

### Task 1: Add Regression Coverage for Legacy Sidecar Rewrite

**Files:**
- Modify: `internal/client/client_test.go`
- Verify: `internal/client/client_test.go`

- [ ] **Step 1: Write the failing test**

Add this test near the existing install-hooks coverage:

```go
func TestRunInstallHooksRefreshesLegacySidecarReferences(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	for _, hookName := range []string{"pre-commit", "pre-push"} {
		t.Run(hookName, func(t *testing.T) {
			targetHook := filepath.Join(repo, ".git", "hooks", hookName)
			legacySidecar := filepath.Join(repo, ".git", "hooks", "calm-"+hookName)
			if err := os.MkdirAll(filepath.Dir(targetHook), 0o755); err != nil {
				t.Fatalf("mkdir hooks: %v", err)
			}
			if err := os.WriteFile(legacySidecar, []byte("#!/usr/bin/env bash\necho legacy\n"), 0o755); err != nil {
				t.Fatalf("seed legacy sidecar: %v", err)
			}
			legacyHook := fmt.Sprintf("#!/usr/bin/env bash\n%s\n%q\n", legacySidecarMarker(hookName), legacySidecar)
			if err := os.WriteFile(targetHook, []byte(legacyHook), 0o755); err != nil {
				t.Fatalf("seed active hook: %v", err)
			}

			var stdout, stderr bytes.Buffer
			if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
				t.Fatalf("RunInstallHooks returned error: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
			}

			content, err := os.ReadFile(targetHook)
			if err != nil {
				t.Fatalf("read active hook: %v", err)
			}
			newSidecar := filepath.Join(repo, ".git", "hooks", sidecarHookName(hookName))
			if !strings.Contains(string(content), sidecarHookMarker(hookName)) {
				t.Fatalf("active hook missing new marker:\n%s", content)
			}
			if !strings.Contains(string(content), strconv.Quote(newSidecar)) {
				t.Fatalf("active hook missing new sidecar path %q:\n%s", newSidecar, content)
			}
			if strings.Contains(string(content), legacySidecar) {
				t.Fatalf("active hook still references legacy sidecar %q:\n%s", legacySidecar, content)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./internal/client -run TestRunInstallHooksRefreshesLegacySidecarReferences -count=1
```

Expected: `FAIL` because the active hook still contains the legacy quoted sidecar path after `RunInstallHooks`.

- [ ] **Step 3: Write minimal implementation**

Update the installer shape so `refreshHookSidecar` can rewrite the active hook file:

```go
func (installer hookInstaller) handleExistingGitHook(targetHook, hooksDir, hookName, embeddedPath string) (bool, error) {
	content, exists, err := readExistingHook(targetHook, hookName)
	if err != nil || !exists {
		return false, err
	}
	if hookHasSidecar(content, hookName) {
		return true, installer.refreshHookSidecar(targetHook, hooksDir, hookName, embeddedPath)
	}
	if hookIsManaged(content, hookName) {
		return false, nil
	}
	return installer.resolveUnmanagedHook(targetHook, hooksDir, hookName, embeddedPath)
}

func (installer hookInstaller) refreshHookSidecar(targetHook, hooksDir, hookName, embeddedPath string) error {
	sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
	if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
		return err
	}
	if err := installer.rewriteSidecarReference(targetHook, hookName, sidecar); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "updated %s\n", sidecar)
	return nil
}
```

Add focused helpers that replace only the recognized marker/path block and optionally remove the legacy sidecar file after the rewrite succeeds.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./internal/client -run TestRunInstallHooksRefreshesLegacySidecarReferences -count=1
```

Expected: `PASS`

- [ ] **Step 5: Refactor for readability without changing behavior**

Keep the rewrite logic in small helpers such as:

```go
func rewriteLegacySidecarBlock(content []byte, hookName, sidecar string) ([]byte, bool) { ... }
func legacySidecarPath(hooksDir, hookName string) string { ... }
```

The helpers MUST keep `refreshHookSidecar` small enough for the existing complexity budget.

- [ ] **Step 6: Run the focused installer package**

Run:

```bash
GOCACHE=$PWD/.cache/go-build go test ./internal/client -count=1
```

Expected: `PASS`

- [ ] **Step 7: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "fix: refresh legacy sidecar hook references" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>\nCo-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```
