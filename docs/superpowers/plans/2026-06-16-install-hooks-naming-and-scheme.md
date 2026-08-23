# install-hooks: post-rename naming + default scheme/mTLS fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `agent-fitness-functions client install-hooks` emit product-named artifacts (not `calm-*`) and make every generated/installed hook reach the HTTPS+mTLS server by default, mirroring the proven `bin/agent-fitness-functions-test` convention.

**Architecture:** Two defects, both fallout from the `calm-bridge → agent-fitness-functions` rename (ADR 0001). (1) The Go installer (`internal/client/client.go`) writes `calm-*` hook filenames, stamps `# CALM …` markers, detects managed hooks by the `CALM` marker, and matches the Claude `PreToolUse` entry on `calm-git-guard`. (2) `client validate` defaults `--addr` to `http://localhost:7890` and the generated hook scripts leave `addr` empty + forward `--client-*` only when env vars are set, so a fresh install cannot authenticate to the HTTPS+mTLS container server. We rename the artifacts (recognizing legacy markers for idempotent upgrade), flip the default scheme to `https://127.0.0.1:7890`, and teach the hooks to auto-discover `<repo>/certs` exactly as the test helper does.

**Tech Stack:** Go 1.x (`internal/client`), Bash hook scripts (`internal/client/hookassets/` + the byte-identical `hooks/` twins), Go table tests (`go test`).

**Scope decisions locked with the requester (2026-06-16):**
- **Twin parity:** `hooks/{pre-commit,pre-push}.sh` are byte-identical twins of `internal/client/hookassets/{pre-commit,pre-push}.sh` (the `hooks/` copies are the directly-tested, README-referenced ones; the `hookassets/` copies are embedded by `install-hooks`). There is **no** generator keeping them in sync. Every script edit in this plan is applied to **both** copies in the same step. (No separate parity-assertion test — keep it manual + diff verification.)
- **Runtime product strings:** Rename product-surface runtime strings (`CALM check …`, `CALM git-guard:`, `Fix CALM violations`) to the product name. **Do NOT** touch FINOS CALM data surface: `.calm/`, `configs/`, `calm-poc`, the `calm` CLI, and `format-violations.py`'s `calm_node`/`calm_check`/docstring fields are left exactly as-is.
- **`pre-tool-use.sh` included:** It shares the identical empty-`addr`/ungated-cert bug and is the agent-time mTLS client, so it receives the same scheme/cert-discovery fix plus product-string renames. It is not a twin of any embedded asset.

**Beads issue:** `calm-poc-6sa`. Claim before starting (`bd update calm-poc-6sa --claim`); do NOT use TaskCreate/TodoWrite. Close at the end.

**Build/test commands (run from repo root):**
```bash
# Targeted Go test (use per task):
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run TestName -v

# Full Go suite touched by this plan:
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test . ./internal/client ./hooks

# Build:
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go build ./cmd/agent-fitness-functions
```

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `internal/client/client.go` | CLI client + hook installer | Naming constants/helpers; legacy-marker recognition; git-guard rename + legacy upsert; default `--addr` → https |
| `internal/client/client_test.go` | Installer tests | Update name/marker assertions; add legacy-upgrade test; add default-scheme test |
| `internal/client/hookassets/pre-commit.sh` | Embedded pre-commit (installed) | Header; https default; cert discovery; runtime strings |
| `internal/client/hookassets/pre-push.sh` | Embedded pre-push (installed) | Same as pre-commit |
| `internal/client/hookassets/git-guard.sh` | Embedded PreToolUse guard | Header + deny message |
| `hooks/pre-commit.sh` | **Twin** — tested + README-referenced | Identical to hookassets/pre-commit.sh |
| `hooks/pre-push.sh` | **Twin** | Identical to hookassets/pre-push.sh |
| `hooks/git-guard.sh` | **Twin** | Identical to hookassets/git-guard.sh |
| `hooks/pre-tool-use.sh` | Agent-time PreToolUse hook (tested) | https default; cert discovery; runtime strings |
| `hooks/pre_commit_test.go` | pre-commit script tests | Add cert-discovery + cert-absent tests |
| `hooks/pre_push_test.go` | **new** pre-push script tests | Add cert-discovery test (pre-push had zero coverage) |
| `hooks/pre_tool_use_test.go` | pre-tool-use script tests | Add cert-discovery test |

---

## Naming reference (use these EXACT strings everywhere)

- Hook product prefix: `agent-fitness-functions`
- Managed-hook marker (header line, signals "our hook, safe to overwrite"): `# agent-fitness-functions <hook> hook` → substring `agent-fitness-functions <hook> hook`
- Legacy managed marker (recognize only): substring `CALM <hook> hook`
- Sidecar marker (append-mode block): `# agent-fitness-functions <hook> hook (sidecar)`
- Legacy sidecar marker (recognize only): `# CALM <hook> hook (sidecar)`
- Sidecar filename: `agent-fitness-functions-<hook>` (was `calm-<hook>`)
- Git-guard filename / settings match: `agent-fitness-functions-git-guard` (legacy recognize: `calm-git-guard`)
- Default daemon URL: `https://127.0.0.1:7890`

> Order matters: the sidecar marker **contains** the managed marker as a substring (`… hook (sidecar)` ⊃ `… hook`). The detection `switch` checks the sidecar case first — preserve that order.

---

## Task 1: Default `client validate` `--addr` to HTTPS loopback

**Files:**
- Modify: `internal/client/client.go:35`
- Test: `internal/client/client_test.go` (add one test)

- [ ] **Step 1: Write the failing test**

Add to `internal/client/client_test.go`. This locks the new default deterministically without depending on whether anything is listening on port 7890 (a real Docker server may be): a `RoundTripper` that always errors forces `isHealthy` false, so `ensureDaemon` calls `starter(addr)` with the resolved default, which we capture.

```go
type alwaysErrTransport struct{}

func (alwaysErrTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unreachable")
}

func TestRunCheckDefaultsToHTTPSLoopback(t *testing.T) {
	var captured string
	starter := func(addr string) error {
		captured = addr
		return errors.New("stop after capture")
	}
	client := &http.Client{Transport: alwaysErrTransport{}}

	err := RunCheck(
		[]string{"--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		io.Discard, client, starter,
	)
	if err == nil {
		t.Fatalf("RunCheck succeeded, want starter error")
	}
	if captured != "https://127.0.0.1:7890" {
		t.Fatalf("daemon addr = %q, want https://127.0.0.1:7890", captured)
	}
}
```

Add `"errors"` and `"io"` to the test file's imports if not present (currently neither is imported in `client_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run TestRunCheckDefaultsToHTTPSLoopback -v`
Expected: FAIL — `daemon addr = "http://localhost:7890", want https://127.0.0.1:7890`.

- [ ] **Step 3: Change the default**

In `internal/client/client.go`, line 35, change:

```go
	addr := flags.String("addr", "https://127.0.0.1:7890", "daemon base URL")
```

(was `"http://localhost:7890"`).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run TestRunCheckDefaultsToHTTPSLoopback -v`
Expected: PASS.

- [ ] **Step 5: Confirm no regression in the existing RunCheck test**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run TestRunCheckPostsValidationRequest -v`
Expected: PASS (that test passes an explicit `--addr`, so the default is unused).

- [ ] **Step 6: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "fix(client): default validate --addr to https loopback for mTLS server

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 2: Rename installed hook artifacts; recognize legacy markers

**Files:**
- Modify: `internal/client/client.go` (constants + `RunInstallHooks` + `installGitHook`)
- Test: `internal/client/client_test.go` (update existing assertions; add legacy-upgrade test)

- [ ] **Step 1: Update existing test assertions to the new names (these now fail)**

In `internal/client/client_test.go`:

> **Do NOT change the `os.Stat` hook-list loop in `TestRunInstallHooksInstallsEmbeddedHooksIntoFreshRepo`** (the `[]string{"pre-commit", "pre-push", "calm-git-guard"}` at line ~91). That loop stats the *actual installed files*, and Task 2 does **not** rename the guard file — `installGitGuard` still writes `calm-git-guard` until Task 3. Renaming the loop entry here would stat a nonexistent file and fail. Task 3 renames the guard file AND this loop entry AND the settings assertion (line ~118) together. Leave both `calm-git-guard` references in this test untouched in Task 2.

`TestRunInstallHooksAppendModeInstallsSidecar` — change the sidecar path (line ~180) and marker (line ~193):
```go
	sidecar := filepath.Join(repo, ".git", "hooks", "agent-fitness-functions-pre-commit")
```
```go
	if !strings.Contains(string(existing), "# agent-fitness-functions pre-commit hook (sidecar)") {
		t.Fatalf("existing hook missing sidecar block:\n%s", existing)
	}
```

(The git-guard settings assertions at lines ~116 and ~136 are handled in Task 3.)

- [ ] **Step 2: Add the legacy-upgrade idempotency test (also failing)**

Add to `internal/client/client_test.go`. Seeds a legacy `# CALM pre-commit hook` managed hook and asserts `install-hooks` recognizes it and upgrades it in place (overwrites rather than refusing as a foreign hook).

> **Scope note (resolves a cross-task ordering issue):** This test asserts only the *behavioral* upgrade — the legacy `echo legacy` body is gone. That fully proves legacy-marker recognition: if the `CALM` marker were NOT recognized as managed, `RunInstallHooks` would refuse the foreign hook and return an error (caught by the `err != nil` check), so a clean overwrite proves recognition worked. We deliberately do **not** assert the new `# agent-fitness-functions pre-commit hook` header string here, because that header lives in the embedded `pre-commit.sh`, which is not renamed until **Task 4**. Task 4 Step 1 adds the marker-string assertion to this same test (red until Task 4's header rename, green after). Keeps every task green at its own boundary.

```go
func TestRunInstallHooksUpgradesLegacyCalmHook(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	legacy := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("#!/usr/bin/env bash\n# CALM pre-commit hook\necho legacy\n"), 0o755); err != nil {
		t.Fatalf("seed legacy hook: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatalf("read upgraded hook: %v", err)
	}
	if strings.Contains(string(content), "echo legacy") {
		t.Fatalf("legacy hook was not overwritten (legacy CALM marker not recognized):\n%s", content)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run 'TestRunInstallHooks' -v`
Expected: FAIL — new git-guard filename absent, sidecar path/marker mismatch, legacy hook refused or not upgraded.

- [ ] **Step 4: Add naming constants and helper functions**

In `internal/client/client.go`, immediately after the `embeddedHooks` var block (after line 29), add:

```go
// Hook artifact naming. Generated git-hook artifacts carry the product name.
// FINOS CALM surfaces (.calm/, configs/, calm-poc, the calm CLI) are unaffected.
const hookProductPrefix = "agent-fitness-functions"

const (
	gitGuardName       = hookProductPrefix + "-git-guard"
	legacyGitGuardName = "calm-git-guard"
)

func managedHookMarker(hook string) string  { return hookProductPrefix + " " + hook + " hook" }
func legacyHookMarker(hook string) string   { return "CALM " + hook + " hook" }
func sidecarHookMarker(hook string) string  { return "# " + hookProductPrefix + " " + hook + " hook (sidecar)" }
func legacySidecarMarker(hook string) string { return "# CALM " + hook + " hook (sidecar)" }
func sidecarHookName(hook string) string    { return hookProductPrefix + "-" + hook }
```

- [ ] **Step 5: Simplify `RunInstallHooks` to drop the marker literals**

Replace the two `installGitHook` calls in `RunInstallHooks` (lines 97–100) with:

```go
	if err := installer.installGitHook("pre-commit", "hookassets/pre-commit.sh"); err != nil {
		return err
	}
	if err := installer.installGitHook("pre-push", "hookassets/pre-push.sh"); err != nil {
		return err
	}
```

- [ ] **Step 6: Rewrite `installGitHook` with legacy recognition + new names**

Replace the entire `installGitHook` method (lines 112–170) with:

```go
func (installer hookInstaller) installGitHook(hookName, embeddedPath string) error {
	targetHook, err := installer.gitHookPath(hookName)
	if err != nil {
		return err
	}
	hooksDir := filepath.Dir(targetHook)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}
	if _, err := os.Stat(targetHook); err == nil {
		content, err := os.ReadFile(targetHook)
		if err != nil {
			return fmt.Errorf("reading existing %s hook: %w", hookName, err)
		}
		hasSidecar := bytes.Contains(content, []byte(sidecarHookMarker(hookName))) ||
			bytes.Contains(content, []byte(legacySidecarMarker(hookName)))
		isManaged := bytes.Contains(content, []byte(managedHookMarker(hookName))) ||
			bytes.Contains(content, []byte(legacyHookMarker(hookName)))
		switch {
		case hasSidecar:
			sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
			if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
				return err
			}
			if err := installer.writeFormatter(hooksDir); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(installer.stdout, "updated %s\n", sidecar)
			return nil
		case !isManaged:
			if os.Getenv("AGENT_FITNESS_FUNCTIONS_HOOK_APPEND") == "1" {
				sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
				if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
					return err
				}
				if err := installer.writeFormatter(hooksDir); err != nil {
					return err
				}
				block := fmt.Sprintf("\n%s\n%q\n", sidecarHookMarker(hookName), sidecar)
				if err := appendFile(targetHook, []byte(block)); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(installer.stdout, "appended %s call to %s (sidecar: %s)\n", hookProductPrefix, targetHook, sidecar)
				return nil
			}
			if os.Getenv("AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE") != "1" {
				_, _ = fmt.Fprintf(installer.stderr, "refusing to overwrite existing unmanaged %s hook: %s\n", hookName, targetHook)
				_, _ = fmt.Fprintln(installer.stderr, "set AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1 to replace it, or AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1 to append")
				return errors.New("refusing to overwrite existing unmanaged hook")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking existing %s hook: %w", hookName, err)
	}
	if err := installer.writeEmbeddedExecutable(embeddedPath, targetHook); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", targetHook)
	return nil
}
```

> Note: `AGENT_FITNESS_FUNCTIONS_HOOK_APPEND`/`_OVERWRITE` env var names are unchanged — only the user-facing "non-CALM" wording becomes "unmanaged". The append/overwrite test (`…RefusesExistingNonCalmHook`) asserts on `AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1`, which is preserved.

- [ ] **Step 7: Run tests to verify they pass**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run 'TestRunInstallHooks' -v`
Expected: PASS (including the new `…UpgradesLegacyCalmHook`). The git-guard settings assertions still fail — that is Task 3.

- [ ] **Step 8: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "feat(client): name installed git hooks agent-fitness-functions-*, recognize legacy CALM markers

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 3: Rename git-guard; recognize legacy `calm-git-guard` in settings

**Files:**
- Modify: `internal/client/client.go` (`installGitGuard`, `upsertGitGuard`)
- Test: `internal/client/client_test.go` (update assertions; add legacy settings upgrade test)

- [ ] **Step 1: Update existing git-guard settings assertions (now failing)**

In `internal/client/client_test.go`:

`TestRunInstallHooksInstallsEmbeddedHooksIntoFreshRepo` — the `os.Stat` hook-list loop (line ~91), now that the guard file is renamed in this task:
```go
	for _, hook := range []string{"pre-commit", "pre-push", "agent-fitness-functions-git-guard"} {
```

`TestRunInstallHooksInstallsEmbeddedHooksIntoFreshRepo` — the settings assertion (line ~118):
```go
	if !strings.Contains(string(settingsContent), "agent-fitness-functions-git-guard") {
		t.Fatalf("settings missing git guard entry:\n%s", settingsContent)
	}
```

`TestRunInstallHooksIsIdempotent` (line ~136):
```go
	if count := strings.Count(string(settingsContent), "agent-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("agent-fitness-functions-git-guard appears %d times, want 1:\n%s", count, settingsContent)
	}
```

- [ ] **Step 2: Add the legacy settings-upgrade test (failing)**

Seeds a `.claude/settings.json` whose `PreToolUse` entry points at a legacy `calm-git-guard`, and asserts `install-hooks` rewrites that single entry to the new path (no duplication, legacy name gone):

```go
func TestRunInstallHooksUpgradesLegacyGitGuardSettings(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")

	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	legacySettings := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/legacy/.git/hooks/calm-git-guard"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(legacySettings), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := RunInstallHooks([]string{repo}, &stdout, &stderr); err != nil {
		t.Fatalf("RunInstallHooks returned error: %v\nstderr=%s", err, stderr.String())
	}

	content, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(content), "calm-git-guard") {
		t.Fatalf("legacy calm-git-guard still present after upgrade:\n%s", content)
	}
	if count := strings.Count(string(content), "agent-fitness-functions-git-guard"); count != 1 {
		t.Fatalf("agent-fitness-functions-git-guard appears %d times, want 1:\n%s", count, content)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run 'TestRunInstallHooks' -v`
Expected: FAIL — guard still written as `calm-git-guard`; legacy settings entry not upgraded.

- [ ] **Step 4: Rename the guard filename**

In `installGitGuard` (line 173), change:
```go
	guardPath, err := installer.gitHookPath(gitGuardName)
```
(was `installer.gitHookPath("calm-git-guard")`).

- [ ] **Step 5: Recognize both names in `upsertGitGuard` and update messages**

In `upsertGitGuard`, replace the match block (lines 241–249) with:

```go
			command, _ := hook["command"].(string)
			if strings.Contains(command, gitGuardName) || strings.Contains(command, legacyGitGuardName) {
				preToolUse[index] = newEntry
				hooks["PreToolUse"] = preToolUse
				if command == guardPath {
					return gitGuardName + " already configured", nil
				}
				return "updated " + gitGuardName + " path", nil
			}
```

And the final return (line 253):
```go
	return "added " + gitGuardName + " to PreToolUse hooks", nil
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -v`
Expected: PASS (all `internal/client` tests, including both legacy-upgrade tests).

- [ ] **Step 7: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "feat(client): rename git-guard to agent-fitness-functions-git-guard, upgrade legacy settings

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 4: pre-commit.sh (both twins) — https default + cert discovery + strings

**Files:**
- Modify: `internal/client/hookassets/pre-commit.sh` AND `hooks/pre-commit.sh` (identical edits)
- Test: `hooks/pre_commit_test.go` (add two tests)

- [ ] **Step 1a: Restore the deferred marker assertion from Task 2**

In `internal/client/client_test.go`, `TestRunInstallHooksUpgradesLegacyCalmHook` now gets the header-string assertion that was deferred from Task 2 (the embedded `pre-commit.sh` header gains `# agent-fitness-functions pre-commit hook` in Step 3 of this task). Add, right after the `echo legacy` check:

```go
	if !strings.Contains(string(content), "# agent-fitness-functions pre-commit hook") {
		t.Fatalf("upgraded hook missing new marker:\n%s", content)
	}
```

This assertion is RED until Step 3 of this task renames the header, then GREEN — verify in Step 7.

- [ ] **Step 1: Write the failing cert-discovery tests**

Add to `hooks/pre_commit_test.go`. These mirror the helper's `TestStackFitnessFunctionsTestPassesMTLS` shape, reusing the existing `initGitRepo`, `runGit`, `writeFile`, `fakeFitnessBin`, `hookScriptPath`, and `readFile` helpers (all already defined in this package):

```go
func TestPreCommitForwardsDiscoveredMTLSCerts(t *testing.T) {
	repo := initGitRepo(t)
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")

	certDir := filepath.Join(repo, "certs")
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		writeFile(t, filepath.Join(certDir, name), "x")
	}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
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

func TestPreCommitOmitsCertFlagsWhenAbsent(t *testing.T) {
	repo := initGitRepo(t)
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPath(t))
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	if !strings.Contains(got, "--addr https://127.0.0.1:7890") {
		t.Errorf("missing https default addr:\n%s", got)
	}
	if strings.Contains(got, "--client-cert") {
		t.Errorf("expected no --client-cert when certs absent:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./hooks -run 'TestPreCommit(ForwardsDiscoveredMTLSCerts|OmitsCertFlagsWhenAbsent)' -v`
Expected: FAIL — current script leaves `addr` empty (no `--addr` forwarded) and never discovers `<repo>/certs`.

- [ ] **Step 3: Edit BOTH pre-commit.sh copies — header + variable block**

In **both** `internal/client/hookassets/pre-commit.sh` and `hooks/pre-commit.sh`:

Change line 2 from `# CALM pre-commit hook` to:
```bash
# agent-fitness-functions pre-commit hook
```

Replace the variable block (current lines 6–10):
```bash
stack_fitness_functions_bin=${AGENT_FITNESS_FUNCTIONS_BIN:-agent-fitness-functions}
addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-}
client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-}
client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-}
client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-}
```
with:
```bash
stack_fitness_functions_bin=${AGENT_FITNESS_FUNCTIONS_BIN:-agent-fitness-functions}
# The container/production server serves HTTPS with mandatory mTLS, so default to
# an https loopback addr and auto-discover dev client credentials in <repo>/certs.
# Explicit AGENT_FITNESS_FUNCTIONS_CLIENT_* env vars win (12-factor precedence).
addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-https://127.0.0.1:7890}
cert_dir=${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR:-$repo/certs}
client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-$cert_dir/client.crt}
client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-$cert_dir/client.key}
client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-$cert_dir/ca.crt}
```

> `repo` is defined on the preceding line (`repo=$(git rev-parse --show-toplevel)`), so `$cert_dir` resolves correctly.

- [ ] **Step 4: Edit BOTH copies — flag forwarding (gate certs on file existence)**

In **both** copies, replace the per-flag block (current lines 100–111):
```bash
  if [[ -n "$addr" ]]; then
    args+=(--addr "$addr")
  fi
  if [[ -n "$client_cert" ]]; then
    args+=(--client-cert "$client_cert")
  fi
  if [[ -n "$client_key" ]]; then
    args+=(--client-key "$client_key")
  fi
  if [[ -n "$client_ca" ]]; then
    args+=(--client-ca "$client_ca")
  fi
```
with:
```bash
  args+=(--addr "$addr")
  # Pass mTLS client cert+key only as a pair (the client requires both together);
  # omit when the files are absent so a plain-HTTP local server still works.
  if [[ -f "$client_cert" && -f "$client_key" ]]; then
    args+=(--client-cert "$client_cert" --client-key "$client_key")
  fi
  if [[ -f "$client_ca" ]]; then
    args+=(--client-ca "$client_ca")
  fi
```

- [ ] **Step 5: Edit BOTH copies — runtime product strings**

In **both** copies, rename the three runtime messages (current lines 114, 120, 137). Change `CALM check` → `agent-fitness-functions check`:
```bash
    echo "agent-fitness-functions check failed for $file" >&2
```
```bash
    echo "agent-fitness-functions check returned invalid JSON for $file" >&2
```
```bash
      echo "agent-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
```

- [ ] **Step 6: Verify the two copies are still byte-identical**

Run: `diff internal/client/hookassets/pre-commit.sh hooks/pre-commit.sh && echo IDENTICAL`
Expected: `IDENTICAL` (no diff output).

- [ ] **Step 7: Run tests to verify they pass**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./hooks -run TestPreCommit -v`
Expected: PASS — new cert tests pass and all existing `TestPreCommit*` tests (loopback rejection, userinfo bypass, block/advisory, unknown status) still pass. (Existing tests assert substrings like `must be loopback`, `unknown status` that are unaffected by the rename; the `https://127.0.0.1:7890` default is loopback so the loopback guard stays satisfied.)

Also re-run the deferred-assertion test from Step 1a, now that the header is renamed:
Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./internal/client -run TestRunInstallHooksUpgradesLegacyCalmHook -v`
Expected: PASS (the `# agent-fitness-functions pre-commit hook` marker assertion is now satisfied by the renamed embedded header).

- [ ] **Step 8: Commit**

```bash
git add internal/client/hookassets/pre-commit.sh hooks/pre-commit.sh hooks/pre_commit_test.go internal/client/client_test.go
git commit -m "fix(hooks): pre-commit defaults to https + discovers <repo>/certs for mTLS

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 5: pre-push.sh (both twins) — https default + cert discovery + strings

**Files:**
- Modify: `internal/client/hookassets/pre-push.sh` AND `hooks/pre-push.sh` (identical edits)
- Test: `hooks/pre_push_test.go` (new file — pre-push currently has zero coverage)

- [ ] **Step 1: Write the failing cert-discovery test**

Create `hooks/pre_push_test.go`. pre-push reads ref updates on stdin and diffs a commit range, so the test builds two commits and feeds a `<local-ref> <local-sha> <remote-ref> <remote-sha>` line. It reuses package helpers (`initGitRepo`, `runGit`, `writeFile`, `fakeFitnessBin`, `hookScriptPathFor`, `readFile`) and adds one local helper to capture a sha:

```go
package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPathFor(t, "pre-push.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader("refs/heads/main " + headSHA + " refs/heads/main " + baseSHA + "\n")
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FITNESS_FUNCTIONS_LOG="+logPath,
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
```

> `hookScriptPathFor` is defined in `hooks/pre_tool_use_test.go` and returns `<cwd>/<name>`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./hooks -run TestPrePushForwardsDiscoveredMTLSCerts -v`
Expected: FAIL — no `--addr` forwarded, certs not discovered.

- [ ] **Step 3: Edit BOTH pre-push.sh copies — header + variable block**

In **both** `internal/client/hookassets/pre-push.sh` and `hooks/pre-push.sh`:

Change line 2 from `# CALM pre-push hook` to:
```bash
# agent-fitness-functions pre-push hook
```

Replace the variable block (current lines 6–10) with the SAME block as Task 4 Step 3 (the `stack_fitness_functions_bin` line through the four `client_*` lines, including the `cert_dir` line and the three-line comment). `repo` is defined on line 5 in this script too.

- [ ] **Step 4: Edit BOTH copies — flag forwarding**

In **both** copies, replace the per-flag block (current lines 104–115):
```bash
    if [[ -n "$addr" ]]; then
      args+=(--addr "$addr")
    fi
    if [[ -n "$client_cert" ]]; then
      args+=(--client-cert "$client_cert")
    fi
    if [[ -n "$client_key" ]]; then
      args+=(--client-key "$client_key")
    fi
    if [[ -n "$client_ca" ]]; then
      args+=(--client-ca "$client_ca")
    fi
```
with (note this block is indented one extra level inside the inner `while` loop — match the surrounding indentation):
```bash
    args+=(--addr "$addr")
    # Pass mTLS client cert+key only as a pair (the client requires both together);
    # omit when the files are absent so a plain-HTTP local server still works.
    if [[ -f "$client_cert" && -f "$client_key" ]]; then
      args+=(--client-cert "$client_cert" --client-key "$client_key")
    fi
    if [[ -f "$client_ca" ]]; then
      args+=(--client-ca "$client_ca")
    fi
```

- [ ] **Step 5: Edit BOTH copies — runtime product strings**

In **both** copies, rename the two runtime messages (current lines 118, 137): `CALM check` → `agent-fitness-functions check`:
```bash
      echo "agent-fitness-functions check failed for $file" >&2
```
```bash
        echo "agent-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
```

- [ ] **Step 6: Verify the two copies are byte-identical**

Run: `diff internal/client/hookassets/pre-push.sh hooks/pre-push.sh && echo IDENTICAL`
Expected: `IDENTICAL`.

- [ ] **Step 7: Run test to verify it passes**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./hooks -run TestPrePush -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/client/hookassets/pre-push.sh hooks/pre-push.sh hooks/pre_push_test.go
git commit -m "fix(hooks): pre-push defaults to https + discovers <repo>/certs for mTLS

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 6: pre-tool-use.sh — https default + cert discovery + strings

**Files:**
- Modify: `hooks/pre-tool-use.sh` (no twin — single file)
- Test: `hooks/pre_tool_use_test.go` (add one test)

- [ ] **Step 1: Write the failing cert-discovery test**

Add to `hooks/pre_tool_use_test.go`. The PreToolUse hook reads a JSON payload on stdin (`{"tool_input":{"file_path":..,"content":..}}`) and forwards a `client validate`. Model the test on the existing tests in that file (they pipe a JSON payload and read `logPath`):

```go
func TestPreToolUseForwardsDiscoveredMTLSCerts(t *testing.T) {
	repo := initGitRepo(t)
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")

	certDir := filepath.Join(repo, "certs")
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		writeFile(t, filepath.Join(certDir, name), "x")
	}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AGENT_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	payload := `{"tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	command := exec.Command("bash", hookScriptPathFor(t, "pre-tool-use.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader(payload)
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, out)
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
```

> If `initGitRepo`/`runGit`/`writeFile` are not visible from `pre_tool_use_test.go`, note they live in the same `hooks` package (`pre_commit_test.go`) — no import needed. Verify `strings`, `os`, `os/exec`, `path/filepath` are imported in `pre_tool_use_test.go` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./hooks -run TestPreToolUseForwardsDiscoveredMTLSCerts -v`
Expected: FAIL — no `--addr`/certs forwarded.

- [ ] **Step 3: Edit the variable block**

In `hooks/pre-tool-use.sh`, replace the variable block (current lines 5–9):
```bash
stack_fitness_functions_bin=${AGENT_FITNESS_FUNCTIONS_BIN:-agent-fitness-functions}
addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-}
client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-}
client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-}
client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-}
```
with the same block from Task 4 Step 3 (`repo` is defined on line 4):
```bash
stack_fitness_functions_bin=${AGENT_FITNESS_FUNCTIONS_BIN:-agent-fitness-functions}
# The container/production server serves HTTPS with mandatory mTLS, so default to
# an https loopback addr and auto-discover dev client credentials in <repo>/certs.
# Explicit AGENT_FITNESS_FUNCTIONS_CLIENT_* env vars win (12-factor precedence).
addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-https://127.0.0.1:7890}
cert_dir=${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR:-$repo/certs}
client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-$cert_dir/client.crt}
client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-$cert_dir/client.key}
client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-$cert_dir/ca.crt}
```

- [ ] **Step 4: Edit the flag forwarding**

Replace the per-flag block (current lines 191–202):
```bash
if [[ -n "$addr" ]]; then
  args+=(--addr "$addr")
fi
if [[ -n "$client_cert" ]]; then
  args+=(--client-cert "$client_cert")
fi
if [[ -n "$client_key" ]]; then
  args+=(--client-key "$client_key")
fi
if [[ -n "$client_ca" ]]; then
  args+=(--client-ca "$client_ca")
fi
```
with:
```bash
args+=(--addr "$addr")
# Pass mTLS client cert+key only as a pair (the client requires both together);
# omit when the files are absent so a plain-HTTP local server still works.
if [[ -f "$client_cert" && -f "$client_key" ]]; then
  args+=(--client-cert "$client_cert" --client-key "$client_key")
fi
if [[ -f "$client_ca" ]]; then
  args+=(--client-ca "$client_ca")
fi
```

- [ ] **Step 5: Edit runtime product strings**

Rename the `CALM check` strings (current lines 125, 131, 186, 205, 211, 227). Change `CALM check` → `agent-fitness-functions check` in each:
```bash
    echo "Skipping agent-fitness-functions check for file outside repository: $file_path" >&2
```
```bash
  echo "Skipping agent-fitness-functions check for unsupported file type: $file" >&2
```
```bash
  echo "agent-fitness-functions check blocked binary content for supported source file: $file" >&2
```
```bash
  echo "agent-fitness-functions check failed for $file" >&2
```
```bash
  echo "agent-fitness-functions check returned invalid JSON for $file" >&2
```
```bash
    echo "agent-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test ./hooks -run TestPreToolUse -v`
Expected: PASS — new cert test passes; existing tests (assert substrings `outside repository`, `blocked binary content`, `invalid JSON`, `must be loopback`, `Invalid PreToolUse payload`) still pass since only the `CALM check` prefix changed.

- [ ] **Step 7: Commit**

```bash
git add hooks/pre-tool-use.sh hooks/pre_tool_use_test.go
git commit -m "fix(hooks): pre-tool-use defaults to https + discovers <repo>/certs for mTLS

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 7: git-guard.sh (both twins) — header + deny message rename

**Files:**
- Modify: `internal/client/hookassets/git-guard.sh` AND `hooks/git-guard.sh` (identical edits)

> No test asserts on the git-guard header or deny text today; existing `internal/client` and `hooks` suites cover behavior. This is a product-string rename verified by the full suite + grep.

- [ ] **Step 1: Edit BOTH git-guard.sh copies**

In **both** `internal/client/hookassets/git-guard.sh` and `hooks/git-guard.sh`:

Change line 2:
```bash
# agent-fitness-functions git-guard PreToolUse hook
```
Change line 3 (the descriptive comment `# Blocks git commands that bypass CALM enforcement…`) — leave "CALM enforcement" as a concept reference, OR for consistency rewrite to:
```bash
# Blocks git commands that bypass fitness-function enforcement before they execute.
```
Change the `deny()` body (current lines 22–23):
```bash
  echo "agent-fitness-functions git-guard: $1" >&2
  echo "  Fix fitness-function violations in the code rather than bypassing enforcement." >&2
```

Also rename the three product-surface CALM strings inside the `deny "..."` call arguments (these are product references, not FINOS CALM data surface):
- `'git commit --no-verify' is blocked. CALM hooks must run.` → `... agent-fitness-functions hooks must run.`
- `'git commit -n' (--no-verify shorthand) is blocked. CALM hooks must run.` → `... agent-fitness-functions hooks must run.`
- `'git merge/pull --ff-only' is blocked when CALM enforcement is active. Use a regular merge or rebase so CALM pre-commit fires.` → `... when agent-fitness-functions enforcement is active. Use a regular merge or rebase so the agent-fitness-functions pre-commit hook fires.`

After all edits, `grep -n "CALM" <both files>` MUST return zero matches.

- [ ] **Step 2: Verify the two copies are byte-identical**

Run: `diff internal/client/hookassets/git-guard.sh hooks/git-guard.sh && echo IDENTICAL`
Expected: `IDENTICAL`.

- [ ] **Step 3: Confirm no FINOS CALM data surface was touched**

Run: `grep -n "calm_node\|calm_check\|\.calm/\|configs/" hooks/format-violations.py | head`
Expected: matches still present and unchanged (we did not touch `format-violations.py`).

- [ ] **Step 4: Commit**

```bash
git add internal/client/hookassets/git-guard.sh hooks/git-guard.sh
git commit -m "refactor(hooks): rename git-guard product strings to agent-fitness-functions

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-opus-4-8"
```

---

## Task 8: Full verification, parity sweep, build, and close-out

**Files:** none (verification only)

- [ ] **Step 1: Run the full touched test suite**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go test . ./internal/client ./hooks`
Expected: `ok` for all three packages.

- [ ] **Step 2: Build the binary**

Run: `GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go build ./cmd/agent-fitness-functions`
Expected: no output (success).

- [ ] **Step 3: Verify twin parity for all three shared scripts**

Run:
```bash
for f in pre-commit.sh pre-push.sh git-guard.sh; do
  diff "internal/client/hookassets/$f" "hooks/$f" >/dev/null && echo "$f IDENTICAL" || echo "$f DIVERGED"
done
```
Expected: `pre-commit.sh IDENTICAL`, `pre-push.sh IDENTICAL`, `git-guard.sh IDENTICAL`.

- [ ] **Step 4: Confirm no stray product-surface `calm-`/`CALM` artifacts remain**

Run:
```bash
grep -rn "calm-pre-commit\|calm-pre-push\|calm-git-guard\|CALM pre-commit hook\|CALM pre-push hook\|CALM check\|CALM git-guard" \
  internal/client hooks --include="*.go" --include="*.sh" | grep -v "_test.go"
```
Expected: no output. (Legacy markers `CALM <hook> hook` may still appear inside `client.go` ONLY as recognized-legacy string literals in `legacyHookMarker`/`legacySidecarMarker`/`legacyGitGuardName` — those are intentional and excluded by reading the matches. If only those legacy-recognition literals appear, that is correct.)

- [ ] **Step 5: End-to-end smoke — fresh install names**

Run:
```bash
tmp=$(mktemp -d) && git -C "$tmp" init -q && \
GOCACHE=$(pwd)/.tmp/go-build GOMODCACHE=$(pwd)/.tmp/go-mod go run ./cmd/agent-fitness-functions client install-hooks "$tmp" && \
ls "$tmp/.git/hooks" | grep -E 'pre-commit|pre-push|agent-fitness-functions-git-guard' && \
grep -o 'agent-fitness-functions-git-guard' "$tmp/.claude/settings.json" && \
rm -rf "$tmp"
```
Expected: lists `pre-commit`, `pre-push`, `agent-fitness-functions-git-guard`; prints `agent-fitness-functions-git-guard` from settings. No `calm-*` files.

- [ ] **Step 6: Close the beads issue and finish the session**

```bash
bd close calm-poc-6sa --reason="install-hooks renamed to agent-fitness-functions-* with legacy recognition; hooks default to https + discover <repo>/certs for mTLS"
bd dolt pull
git status   # confirm everything committed
```
Follow the project session-close protocol (commit/merge per `CLAUDE.md`).

---

## Self-Review (completed against the spec)

- **Spec coverage:**
  - Defect 1 (calm-* identity) → Tasks 2 (hook artifacts/markers) + 3 (git-guard) + 4–7 (script headers/strings). ✔
  - Defect 2 (http default vs https+mTLS server) → Task 1 (client default) + Tasks 4/5/6 (hook addr default + cert discovery). ✔
  - Decision 1 (rename + legacy recognition, idempotent upgrade) → Task 2 (managed/sidecar legacy markers) + Task 3 (legacy `calm-git-guard` settings upgrade), each with a dedicated legacy-upgrade test. ✔
  - Decision 2 (mirror test-helper scheme/mTLS: addr default, cert-dir discovery, env precedence, gate on file existence) → Tasks 4/5/6 variable block + file-existence-gated forwarding. ✔
  - Testing items 1–4 → Task 2 (naming + legacy), Task 3 (git-guard legacy), Task 1 (scheme default), Tasks 4/5/6 (cert discovery present/absent). ✔
  - Out-of-scope (server defaults, FINOS CALM names, no full migration tool) → respected; legacy append-mode `calm-<hook>` call-line rewrite is explicitly NOT performed (idempotent re-run path only). ✔
- **Beyond-spec scope (requester-approved):** twin parity for all three shared scripts (Tasks 4/5/7 + parity diffs in Task 8); `pre-tool-use.sh` scheme/cert fix (Task 6); product-string renames excluding `format-violations.py` FINOS surface.
- **Placeholder scan:** none — every code/script/test step shows full content.
- **Type/name consistency:** `hookProductPrefix`, `managedHookMarker`, `legacyHookMarker`, `sidecarHookMarker`, `legacySidecarMarker`, `sidecarHookName`, `gitGuardName`, `legacyGitGuardName` used identically across Tasks 2, 3, and the embedded script header in Task 4 (`# agent-fitness-functions pre-commit hook` matches `managedHookMarker("pre-commit")`). Default URL `https://127.0.0.1:7890` consistent across Task 1 (Go) and Tasks 4/5/6 (scripts).
