# Container Governance Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify the CALM repository around the containerized server and remote governance model by correcting stale documentation, aligning all hook scripts to the `pre-commit.sh` contract, and purging tracked Python bytecode.

**Architecture:** The container is the authoritative governance layer; `configs/<repo>/config.json` is the governance source of truth mounted into the container; local `.calm/config.json` is an explicitly subordinate developer sandbox. All hooks that can target a remote bridge must enforce HTTPS and forward mTLS credentials consistently.

**Tech Stack:** Bash (shellcheck-compliant), Markdown, Go (build verification only), Docker Compose, git

---

## File Map

| File | Action | What changes |
|------|--------|--------------|
| `Explaination.md` | Rewrite | Remove stale references to `/tmp/calm-bridge` and `.calm/config.json` as primary governance source; redirect to `CONTEXT.md` |
| `README.md` | Modify | Add **Governance Layer Deployment** section near top; reframe project as enterprise governance layer with sandbox subordination |
| `CONTEXT.md` | Modify | Clarify that local `.calm/config.json` cannot weaken enforcement; state container `configs/<repo>/config.json` is authoritative |
| `docs/runbooks/red-green-demo.md` | Modify | Add historical notice banner; retain content, update local binary references from `/tmp/calm-bridge` to `${STACK_FITNESS_FUNCTIONS_BIN:-calm-bridge}` |
| `docs/spec/engineering-spec.md` | Modify | Add historical notice banner (PoC-era spec, superseded by container model) |
| `hooks/pre-push.sh` | Rewrite | Add mTLS forwarding, `STACK_FITNESS_FUNCTIONS_REPO_NAME` resolution, remote-mode logic, HTTPS enforcement on par with `pre-commit.sh` |
| `hooks/pre-tool-use.sh` | Modify | Add HTTPS enforcement block for remote bridge (mirrors the pattern in `pre-commit.sh`) |
| `.claude/settings.json` | Modify | Update `PreToolUse` command to use `STACK_FITNESS_FUNCTIONS_BIN` from the local build path with `STACK_FITNESS_FUNCTIONS_ADDR` pointing to HTTPS when remote, or keep loopback with correct binary path |
| `bin/calm-test` | Modify | Fix hardcoded `/tmp/calm-bridge` default; add `STACK_FITNESS_FUNCTIONS_REPO_NAME` support; label script as local-sandbox-only in usage text |
| `docker-compose.yml` | Modify | Add explicit comment stating production Helm chart is external to this repository |
| `.gitignore` | Verify | Confirm `**/__pycache__/` and `**/*.pyc` already present (they are — no change needed) |
| `hooks/__pycache__/` | Remove | Untrack and remove all `.pyc` files from git index |

---

## Task 1: Purge tracked Python bytecode

**Files:**
- Modify: `.gitignore` (verify — no content change needed)
- Remove from index: `hooks/__pycache__/format-violations.cpython-314.pyc`
- Remove from index: `hooks/__pycache__/test_format_violations.cpython-314-pytest-9.0.2.pyc`

- [ ] **Step 1: Verify .gitignore already covers pycache**

```bash
grep -n '__pycache__' /Users/poconnor/peter_code/calm-poc/.gitignore
grep -n '\.pyc' /Users/poconnor/peter_code/calm-poc/.gitignore
```

Expected output: both patterns present (lines `**/__pycache__/` and `**/*.pyc`).

- [ ] **Step 2: Remove the tracked bytecode files from the git index**

```bash
cd /Users/poconnor/peter_code/calm-poc
git rm --cached hooks/__pycache__/format-violations.cpython-314.pyc
git rm --cached "hooks/__pycache__/test_format_violations.cpython-314-pytest-9.0.2.pyc"
```

Expected: `rm 'hooks/__pycache__/...'` for each file. Files stay on disk; git stops tracking them.

- [ ] **Step 3: Verify the index is clean**

```bash
git ls-files hooks/__pycache__/
```

Expected: no output (empty).

- [ ] **Step 4: Commit**

```bash
git add .gitignore
git commit -m "chore: untrack compiled Python bytecode from hooks/__pycache__

.gitignore already excludes **/__pycache__/ and **/*.pyc; these files
were staged before the rule was committed.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 2: Rewrite Explaination.md

**Files:**
- Modify: `Explaination.md`

The current file describes the hook as reading `.calm/config.json` and references `/tmp/calm-bridge` in the quick-reference table. Both are stale. `CONTEXT.md` already contains the correct architecture. The simplest correct fix is to redirect to `CONTEXT.md` and remove the stale content.

- [ ] **Step 1: Overwrite Explaination.md with a redirect document**

Replace the entire file with:

```markdown
# CALM — How It Works

This document previously described the PoC local architecture.

The current architecture is documented in [CONTEXT.md](CONTEXT.md).
All references to `/tmp/calm-bridge` as a binary path or `.calm/config.json`
as the primary governance source in earlier versions of this file are
superseded by the container governance model described there.
```

- [ ] **Step 2: Commit**

```bash
git add Explaination.md
git commit -m "docs: redirect Explaination.md to CONTEXT.md, remove stale local-only references

Removed stale references to /tmp/calm-bridge and .calm/config.json as the
primary governance source. CONTEXT.md is authoritative.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 3: Update README.md — add Governance Layer Deployment section

**Files:**
- Modify: `README.md`

The README currently describes the container image and Compose deployment but does not frame them as the primary production path, nor does it subordinate local `.calm` mode. A **Governance Layer Deployment** section must appear before the **CLI Tools** section.

- [ ] **Step 1: Insert Governance Layer Deployment section after the Baseline Analysis section**

Insert the following block immediately after the `## Baseline Analysis` section heading and its closing paragraph (after the `dotnet pack` command block and the cold-start measurement line):

```markdown
## Governance Layer Deployment

`calm-bridge` is an enterprise organization-wide governance layer. The containerized service is the **primary production path**. Every governed repository connects to a shared, centrally operated container instance; governance thresholds and enforcement configuration are authoritative only when served from the container.

The local `.calm` mode (described in the CLI tools section below) is a **sandbox environment** for developer iteration and demonstration. It does not substitute for the container layer in any production or CI context.

### Deployment topology

```
┌─────────────────────────────────┐
│  Container (authoritative)      │
│  calm-bridge serve              │
│  configs/<repo>/config.json  ←─ governance source of truth
│  certs/{server,ca}.{crt,key}    │
└──────────────┬──────────────────┘
               │ HTTPS + mTLS
       ┌───────┴────────┐
       │                │
  pre-commit.sh    pre-tool-use.sh
  (developer git)  (AI agent hook)
```

### Environment variables for remote mode

| Variable | Required | Purpose |
|----------|----------|---------|
| `STACK_FITNESS_FUNCTIONS_ADDR` | Yes | Full HTTPS URL, e.g. `https://calm-governance.example:7890` |
| `STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE` | Yes (set to `1`) | Opt-in to non-loopback bridge addresses |
| `STACK_FITNESS_FUNCTIONS_CLIENT_CERT` | Yes (mTLS) | Path to PEM-encoded client certificate |
| `STACK_FITNESS_FUNCTIONS_CLIENT_KEY` | Yes (mTLS) | Path to PEM-encoded client private key |
| `STACK_FITNESS_FUNCTIONS_CLIENT_CA` | Yes (mTLS) | Path to PEM-encoded CA bundle for server verification |
| `STACK_FITNESS_FUNCTIONS_REPO_NAME` | Recommended | Logical repository name (overrides working-tree basename) |

All hooks enforce HTTPS when `STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1` is set. Connections over plain HTTP to a non-loopback address are rejected at the hook layer.
```

- [ ] **Step 2: Verify the section renders without broken Markdown**

```bash
grep -n "Governance Layer" /Users/poconnor/peter_code/calm-poc/README.md
```

Expected: one matching line with the section heading.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): add Governance Layer Deployment section

Frames the containerised service as the primary production path and
explicitly subordinates .calm local mode as a sandbox environment.
Documents all remote-mode environment variables in a single reference table.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 4: Update CONTEXT.md — clarify governance authority

**Files:**
- Modify: `CONTEXT.md`

Two changes are needed:

1. The "Can a developer bypass the hook?" section says a developer "can … weaken a local `.calm/config.json`" without explicitly stating that the container's `configs/<repo>/config.json` governance is unaffected by that local change.
2. The quick-reference table entry for `.calm/config.json` says "Developer-only override for local iteration" but does not explicitly state that it cannot weaken container enforcement.

- [ ] **Step 1: Update the bypass paragraph to clarify container governance is unaffected**

In `CONTEXT.md`, find the paragraph beginning:

```
Yes. Git hooks are local and unversioned. A developer can delete `.git/hooks/pre-commit` or weaken a local `.calm/config.json`. The hook is a **shift-left convenience**, not a security boundary. The containerised bridge still enforces the mounted `configs/<repo>/config.json` governance set.
```

Replace it with:

```
Yes. Git hooks are local and unversioned. A developer can delete `.git/hooks/pre-commit` or modify a local `.calm/config.json`. The hook is a **shift-left convenience**, not a security boundary.

A governed repository **cannot weaken enforcement** via a local `.calm/config.json` file. When the hook connects to the containerised bridge, governance is resolved exclusively from the mounted `configs/<repo>/config.json` inside the container. The local `.calm/config.json` file has no effect on the container layer; it is only consulted when the bridge is running in local developer sandbox mode (loopback address, no remote flag).
```

- [ ] **Step 2: Update the quick-reference table entry for `.calm/config.json`**

Find the row:

```
| `.calm/config.json` | Optional local repository sandbox | Developer-only override for local iteration |
```

Replace with:

```
| `.calm/config.json` | Optional local repository sandbox | Developer sandbox only — **has no effect on container governance**; container always resolves from `configs/<repo>/config.json` |
```

- [ ] **Step 3: Verify the document still renders cleanly**

```bash
grep -n "cannot weaken" /Users/poconnor/peter_code/calm-poc/CONTEXT.md
grep -n "has no effect on container governance" /Users/poconnor/peter_code/calm-poc/CONTEXT.md
```

Expected: one match per grep.

- [ ] **Step 4: Commit**

```bash
git add CONTEXT.md
git commit -m "docs(context): clarify that local .calm/config.json cannot weaken container governance

Container enforcement is resolved exclusively from configs/<repo>/config.json
mounted inside the container. The local .calm file is sandbox-only.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 5: Add historical notice banners to runbook and spec

**Files:**
- Modify: `docs/runbooks/red-green-demo.md`
- Modify: `docs/spec/engineering-spec.md`

Both documents describe the PoC-era local architecture. They remain useful for understanding the system's evolution but must not be read as current architecture. A banner is added at the top of each; the binary path reference in the runbook (`/tmp/calm-bridge`) is also corrected to use the env-var form.

- [ ] **Step 1: Add historical banner to red-green-demo.md**

Insert the following block at the very top of `docs/runbooks/red-green-demo.md` (before the `# CALM PoC Red-Green Demo Runbook` heading):

```markdown
> **Historical document.** This runbook was written for the PoC local-only architecture.
> It describes running `calm-bridge` built to `/tmp/calm-bridge` on loopback.
> For the current container governance model, see [CONTEXT.md](../../CONTEXT.md) and
> [README.md](../../README.md). Steps in this runbook remain valid for local sandbox
> verification but must not be used as production deployment guidance.

---

```

- [ ] **Step 2: Replace hardcoded /tmp/calm-bridge references in the runbook**

In `docs/runbooks/red-green-demo.md`, find:

```
- Build the bridge: `go build -o /tmp/calm-bridge ./cmd/calm-bridge`
- Start the daemon on loopback: `/tmp/calm-bridge serve --addr 127.0.0.1:7890`
- Run commits with `STACK_FITNESS_FUNCTIONS_BIN=/tmp/calm-bridge STACK_FITNESS_FUNCTIONS_ADDR=http://127.0.0.1:7890`
```

Replace with:

```
- Build the bridge: `go build -o .tmp/calm-bridge ./cmd/calm-bridge`
- Start the daemon on loopback: `STACK_FITNESS_FUNCTIONS_BIN=.tmp/calm-bridge .tmp/calm-bridge serve --addr 127.0.0.1:7890`
- Run commits with `STACK_FITNESS_FUNCTIONS_BIN=.tmp/calm-bridge STACK_FITNESS_FUNCTIONS_ADDR=http://127.0.0.1:7890`
```

Find all remaining `/tmp/calm-bridge` occurrences in the runbook:

```bash
grep -n '/tmp/calm-bridge' /Users/poconnor/peter_code/calm-poc/docs/runbooks/red-green-demo.md
```

Replace each remaining `/tmp/calm-bridge` with `.tmp/calm-bridge` so no reference to the ephemeral system temp directory remains.

- [ ] **Step 3: Add historical banner to engineering-spec.md**

Insert the following block at the very top of `docs/spec/engineering-spec.md` (before the YAML front matter or the `# CALM PoC: Engineering Technical Specification` heading, after the front matter block if one exists):

Locate the front-matter block (lines 1–8 ending with `---`). After the closing `---`, insert:

```markdown

> **Historical document.** This specification describes the PoC local-only architecture
> (single-machine daemon, `.calm/config.json` governance, loopback-only bridge).
> The current production architecture uses a containerised service with
> `configs/<repo>/config.json` governance mounted at runtime.
> See [CONTEXT.md](../../CONTEXT.md) and [README.md](../../README.md) for current architecture.

```

- [ ] **Step 4: Verify banners appear at correct positions**

```bash
head -15 /Users/poconnor/peter_code/calm-poc/docs/runbooks/red-green-demo.md
head -20 /Users/poconnor/peter_code/calm-poc/docs/spec/engineering-spec.md
```

Expected: banner text visible near the top of each file.

- [ ] **Step 5: Commit**

```bash
git add docs/runbooks/red-green-demo.md docs/spec/engineering-spec.md
git commit -m "docs: add historical notice banners to runbook and engineering spec

Both documents describe the PoC local-only architecture. The banner
makes clear they are reference history, not current deployment guidance.
Also corrects /tmp/calm-bridge → .tmp/calm-bridge in the runbook.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 6: Update hooks/pre-push.sh — full remote governance contract

**Files:**
- Modify: `hooks/pre-push.sh`

The current `pre-push.sh` is missing: (a) mTLS credential forwarding (`STACK_FITNESS_FUNCTIONS_CLIENT_CERT`, `STACK_FITNESS_FUNCTIONS_CLIENT_KEY`, `STACK_FITNESS_FUNCTIONS_CLIENT_CA`), (b) `STACK_FITNESS_FUNCTIONS_REPO_NAME` resolution, (c) HTTPS enforcement when `STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1`, (d) `remote_mode` flag and content-file handling for remote mode (parallel to `pre-commit.sh`). The script also uses `--repo "$repo"` (absolute path) instead of the logical repo name when in remote mode.

Replace the complete content of `hooks/pre-push.sh` with:

```bash
#!/usr/bin/env bash
# CALM pre-push hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
calm_bridge=${STACK_FITNESS_FUNCTIONS_BIN:-calm-bridge}
addr=${STACK_FITNESS_FUNCTIONS_ADDR:-}
client_cert=${STACK_FITNESS_FUNCTIONS_CLIENT_CERT:-}
client_key=${STACK_FITNESS_FUNCTIONS_CLIENT_KEY:-}
client_ca=${STACK_FITNESS_FUNCTIONS_CLIENT_CA:-}
repo_name=${STACK_FITNESS_FUNCTIONS_REPO_NAME:-}
remote_mode=0
repo_arg=$repo
blocked=0

bridge_addr_is_loopback() {
  python3 - "$1" <<'PY'
import ipaddress
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
if parsed.scheme not in {"http", "https"} or not parsed.hostname:
    sys.exit(1)
if parsed.hostname == "localhost":
    sys.exit(0)
try:
    sys.exit(0 if ipaddress.ip_address(parsed.hostname).is_loopback else 1)
except ValueError:
    sys.exit(1)
PY
}

bridge_addr_is_https() {
  python3 - "$1" <<'PYCHECK'
import sys
from urllib.parse import urlparse

sys.exit(0 if urlparse(sys.argv[1]).scheme == "https" else 1)
PYCHECK
}

if [[ -n "$addr" ]] && ! bridge_addr_is_loopback "$addr"; then
  if [[ "${STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE:-}" != "1" ]]; then
    echo "STACK_FITNESS_FUNCTIONS_ADDR must be loopback unless STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1 is set" >&2
    exit 1
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote STACK_FITNESS_FUNCTIONS_ADDR must use https" >&2
    exit 1
  fi
  remote_mode=1
fi

if [[ -n "$repo_name" ]]; then
  repo_arg=$repo_name
elif [[ "$remote_mode" -eq 1 ]]; then
  repo_arg=$(basename "$repo")
fi

language_for_file() {
  case "$1" in
    *.go) printf 'go' ;;
    *.py) printf 'python' ;;
    *.cs) printf 'csharp' ;;
    *) return 1 ;;
  esac
}

json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin).get(sys.argv[1], ""))' "$1"
}

tmpdir=$(mktemp -d)
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

while read -r local_ref local_sha remote_ref remote_sha; do
  # Skip deletions
  [[ "$local_sha" == "0000000000000000000000000000000000000000" ]] && continue

  null_sha="0000000000000000000000000000000000000000"
  if [[ "$remote_sha" == "$null_sha" ]]; then
    base=$(git merge-base "$local_sha" "origin/HEAD" 2>/dev/null \
           || git merge-base "$local_sha" "origin/main" 2>/dev/null \
           || echo "${local_sha}^")
    range="${base}..${local_sha}"
  else
    range="${remote_sha}..${local_sha}"
  fi

  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    if ! language=$(language_for_file "$file"); then
      continue
    fi

    content_file="$tmpdir/$(echo "$file" | tr '/' '_')"
    if ! git show "${local_sha}:${file}" > "$content_file" 2>/dev/null; then
      continue
    fi

    args=(check --file "$file" --repo "$repo_arg" --content-file "$content_file" --language "$language")
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

    if ! result=$("$calm_bridge" "${args[@]}"); then
      echo "CALM check failed for $file" >&2
      blocked=1
      continue
    fi

    status=$(printf '%s' "$result" | json_field status)
    case "$status" in
      block)
        printf '%s' "$result" | python3 "$(dirname "${BASH_SOURCE[0]}")/format-violations.py" \
          --mode "$status" --file "$file" >&2 || true
        blocked=1
        ;;
      advisory)
        printf '%s' "$result" | python3 "$(dirname "${BASH_SOURCE[0]}")/format-violations.py" \
          --mode "$status" --file "$file" >&2 || true
        ;;
      pass)
        ;;
      *)
        echo "CALM check returned unknown status for $file: ${status:-<empty>}" >&2
        blocked=1
        ;;
    esac
  done < <(git diff --name-only --diff-filter=ACM "$range" -- 2>/dev/null || true)
done

if [[ "$blocked" -ne 0 ]]; then
  exit 1
fi
```

- [ ] **Step 1: Write the file as shown above.**

- [ ] **Step 2: shellcheck verification**

```bash
shellcheck /Users/poconnor/peter_code/calm-poc/hooks/pre-push.sh
```

Expected: no output (exit 0).

- [ ] **Step 3: Diff to confirm key additions**

```bash
git diff hooks/pre-push.sh | grep '^+' | grep -E 'client_cert|client_key|client_ca|repo_name|remote_mode|bridge_addr_is_https'
```

Expected: multiple lines showing the newly added variables and functions.

- [ ] **Step 4: Commit**

```bash
git add hooks/pre-push.sh
git commit -m "feat(hooks): align pre-push.sh to full remote governance contract

Add mTLS credential forwarding, STACK_FITNESS_FUNCTIONS_REPO_NAME resolution, HTTPS
enforcement for non-loopback bridges, and remote_mode flag — matching
the pre-commit.sh implementation. Logical repo name now used as --repo
argument in remote mode.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 7: Update hooks/pre-tool-use.sh — add HTTPS enforcement

**Files:**
- Modify: `hooks/pre-tool-use.sh`

The script already handles `STACK_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA` and `STACK_FITNESS_FUNCTIONS_REPO_NAME`. It is missing only the `bridge_addr_is_https` check and the corresponding enforcement block. The fix is surgical: add the `bridge_addr_is_https` function (copy from `pre-commit.sh`) and the enforcement guard immediately after the loopback guard.

- [ ] **Step 1: Add bridge_addr_is_https function**

In `hooks/pre-tool-use.sh`, find the existing `bridge_addr_is_loopback` function (lines 16–32) and insert the following immediately after it (after the closing `PY` heredoc and before the `if [[ -n "$addr" ...` guard):

```bash
bridge_addr_is_https() {
  python3 - "$1" <<'PYCHECK'
import sys
from urllib.parse import urlparse

sys.exit(0 if urlparse(sys.argv[1]).scheme == "https" else 1)
PYCHECK
}
```

- [ ] **Step 2: Add HTTPS enforcement to the remote-bridge guard**

Find the existing guard block:

```bash
if [[ -n "$addr" && "${STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE:-}" != "1" ]] && ! bridge_addr_is_loopback "$addr"; then
  echo "STACK_FITNESS_FUNCTIONS_ADDR must be loopback unless STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1 is set" >&2
  exit 2
fi
```

Replace with:

```bash
if [[ -n "$addr" ]] && ! bridge_addr_is_loopback "$addr"; then
  if [[ "${STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE:-}" != "1" ]]; then
    echo "STACK_FITNESS_FUNCTIONS_ADDR must be loopback unless STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1 is set" >&2
    exit 2
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote STACK_FITNESS_FUNCTIONS_ADDR must use https" >&2
    exit 2
  fi
fi
```

- [ ] **Step 3: shellcheck verification**

```bash
shellcheck /Users/poconnor/peter_code/calm-poc/hooks/pre-tool-use.sh
```

Expected: no output (exit 0).

- [ ] **Step 4: Diff to confirm the change**

```bash
git diff hooks/pre-tool-use.sh | grep '^+' | grep -E 'bridge_addr_is_https|must use https'
```

Expected: lines showing the new function and error message.

- [ ] **Step 5: Commit**

```bash
git add hooks/pre-tool-use.sh
git commit -m "feat(hooks): add HTTPS enforcement for remote bridge in pre-tool-use.sh

Mirrors the bridge_addr_is_https guard already present in pre-commit.sh.
Non-loopback HTTP bridges are now rejected at the hook layer consistently
across all three hook scripts.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 8: Audit and correct .claude/settings.json PreToolUse command

**Files:**
- Modify: `.claude/settings.json`

The current `PreToolUse` command hardcodes `STACK_FITNESS_FUNCTIONS_BIN=/Users/poconnor/peter_code/calm-poc/.tmp/calm-bridge` and `STACK_FITNESS_FUNCTIONS_ADDR=http://127.0.0.1:7890`. This is intentional — it wires the hook to the locally built binary at the loopback address. The `/usr/bin/true` concern in the spec does not apply here; the hook is active. However, the path hardcodes a specific user's home directory, which will break for any other developer. The correct form uses a relative path resolution via `$PWD`.

- [ ] **Step 1: Update the PreToolUse command to use a portable path**

Replace the current `PreToolUse` hooks array with:

```json
"PreToolUse": [
  {
    "hooks": [
      {
        "command": "STACK_FITNESS_FUNCTIONS_BIN=\"$(pwd)/.tmp/calm-bridge\" STACK_FITNESS_FUNCTIONS_ADDR=http://127.0.0.1:7890 \"$(pwd)/hooks/pre-tool-use.sh\"",
        "type": "command"
      }
    ],
    "matcher": "Edit|Write"
  }
]
```

- [ ] **Step 2: Verify the JSON is valid**

```bash
python3 -c "import json; json.load(open('.claude/settings.json'))" && echo "valid"
```

Expected: `valid`

- [ ] **Step 3: Commit**

```bash
git add .claude/settings.json
git commit -m "chore(config): use portable pwd-relative path in PreToolUse hook command

Replaces hardcoded /Users/poconnor path with \$(pwd) so the hook works
for any developer with the repo checked out at a different location.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 9: Update bin/calm-test — fix binary default, add STACK_FITNESS_FUNCTIONS_REPO_NAME, label as sandbox-only

**Files:**
- Modify: `bin/calm-test`

Three issues:

1. Default `STACK_FITNESS_FUNCTIONS_BIN` is hardcoded to `/tmp/calm-bridge` — must use `.tmp/calm-bridge` relative to the script location, or fall back to a `calm-bridge` on PATH.
2. The script reads governance from the local `.calm/config.json` only; it has no path to use `STACK_FITNESS_FUNCTIONS_REPO_NAME` for remote container checks.
3. The usage text does not label it as sandbox-only.

- [ ] **Step 1: Fix the binary default and add STACK_FITNESS_FUNCTIONS_REPO_NAME support**

In `bin/calm-test`, find:

```bash
bridge="${STACK_FITNESS_FUNCTIONS_BIN:-/tmp/calm-bridge}"
addr="${STACK_FITNESS_FUNCTIONS_ADDR:-http://127.0.0.1:7890}"
```

Replace with:

```bash
bridge="${STACK_FITNESS_FUNCTIONS_BIN:-calm-bridge}"
addr="${STACK_FITNESS_FUNCTIONS_ADDR:-http://127.0.0.1:7890}"
repo_name="${STACK_FITNESS_FUNCTIONS_REPO_NAME:-}"
```

- [ ] **Step 2: Update usage text to declare sandbox-only scope**

In the `usage()` function, find:

```bash
  echo "  Checks all fitness functions for a source file against the running CALM bridge." >&2
```

Replace with:

```bash
  echo "  LOCAL SANDBOX ONLY: checks fitness functions against a locally running CALM bridge." >&2
  echo "  For remote container governance, configure STACK_FITNESS_FUNCTIONS_ADDR, STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1," >&2
  echo "  STACK_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA, and STACK_FITNESS_FUNCTIONS_REPO_NAME, then run the hook directly." >&2
```

Also update the Environment section in `usage()`. Find:

```bash
  echo "    STACK_FITNESS_FUNCTIONS_BIN   path to calm-bridge binary (default: /tmp/calm-bridge)" >&2
  echo "    STACK_FITNESS_FUNCTIONS_ADDR  bridge address (default: http://127.0.0.1:7890)" >&2
```

Replace with:

```bash
  echo "    STACK_FITNESS_FUNCTIONS_BIN   path to calm-bridge binary (default: calm-bridge on PATH)" >&2
  echo "    STACK_FITNESS_FUNCTIONS_ADDR  bridge address (default: http://127.0.0.1:7890)" >&2
  echo "    STACK_FITNESS_FUNCTIONS_REPO_NAME    logical repo name override (default: derived from git root)" >&2
```

- [ ] **Step 3: Use repo_name when invoking the bridge check**

Find the block that resolves `repo` and `relative_file` (near line 41):

```bash
repo=$(git -C "$(dirname "$file_abs")" rev-parse --show-toplevel 2>/dev/null) \
  || { echo "error: file is not inside a git repository" >&2; exit 1; }
relative_file="${file_abs#"$repo"/}"
```

Insert after that block:

```bash
repo_arg="$repo"
if [[ -n "$repo_name" ]]; then
  repo_arg="$repo_name"
fi
```

Then find every occurrence of `--repo "$tmp_dir"` in the `calm-test` main loop (the per-function invocation):

```bash
    "$bridge" check \
      --addr "$addr" \
      --repo "$tmp_dir" \
```

Replace `--repo "$tmp_dir"` with `--repo "$repo_arg"`.

- [ ] **Step 4: shellcheck verification**

```bash
shellcheck /Users/poconnor/peter_code/calm-poc/bin/calm-test
```

Expected: no output (exit 0).

- [ ] **Step 5: Verify the changes**

```bash
grep -n 'STACK_FITNESS_FUNCTIONS_BIN\|STACK_FITNESS_FUNCTIONS_REPO_NAME\|LOCAL SANDBOX\|repo_arg' /Users/poconnor/peter_code/calm-poc/bin/calm-test
```

Expected: lines showing all four patterns present.

- [ ] **Step 6: Commit**

```bash
git add bin/calm-test
git commit -m "feat(bin): fix calm-test binary default, add STACK_FITNESS_FUNCTIONS_REPO_NAME support, label as sandbox-only

- Default binary is now 'calm-bridge' on PATH (not /tmp/calm-bridge)
- STACK_FITNESS_FUNCTIONS_REPO_NAME overrides the --repo argument for remote container checks
- Usage text explicitly labels the tool as a local sandbox utility

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 10: Update docker-compose.yml — clarify Helm chart is external

**Files:**
- Modify: `docker-compose.yml`

The existing comment block already says "Kubernetes production deployments MUST use the Helm chart as authoritative" but does not state explicitly that the Helm chart is not included in this repository. Add one sentence.

- [ ] **Step 1: Update the comment at the top of docker-compose.yml**

Find the existing header comment:

```yaml
# Docker Compose is the local/staging deployment contract for calm-bridge.
# Compose applies restart, mounts, read_only, tmpfs, cap_drop, and security_opt locally.
# Resource enforcement differs by runtime: local Compose treats deploy.resources as
# advisory compatibility metadata, while Swarm has enforced deploy.resources limits.
# Kubernetes production deployments MUST use the Helm chart as authoritative.
# Helm chart is authoritative for pod securityContext, resources.limits,
# ConfigMaps, Secrets, and read-only mounts.
```

Replace with:

```yaml
# Docker Compose is the local/staging deployment contract for calm-bridge.
# Compose applies restart, mounts, read_only, tmpfs, cap_drop, and security_opt locally.
# Resource enforcement differs by runtime: local Compose treats deploy.resources as
# advisory compatibility metadata, while Swarm has enforced deploy.resources limits.
# Kubernetes production deployments MUST use the Helm chart as authoritative.
# The production Helm chart is maintained externally and is NOT included in this repository.
# Helm chart is authoritative for pod securityContext, resources.limits,
# ConfigMaps, Secrets, and read-only mounts.
```

- [ ] **Step 2: Verify the line was added**

```bash
grep -n 'externally and is NOT' /Users/poconnor/peter_code/calm-poc/docker-compose.yml
```

Expected: one matching line.

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "docs(compose): explicitly state production Helm chart is external to this repo

Clarifies that docker-compose.yml is local/staging only; the authoritative
Helm chart for Kubernetes production is maintained separately.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 11: Smoke check verification

This task is verification only — no code changes.

- [ ] **Step 1: Verify no remaining references to /tmp/calm-bridge in tracked files**

```bash
git ls-files | xargs grep -l '/tmp/calm-bridge' 2>/dev/null || echo "clean"
```

Expected: `clean`

- [ ] **Step 2: Verify __pycache__ is no longer tracked**

```bash
git ls-files hooks/__pycache__/
```

Expected: no output.

- [ ] **Step 3: shellcheck all hook scripts**

```bash
shellcheck hooks/pre-commit.sh hooks/pre-push.sh hooks/pre-tool-use.sh
```

Expected: no output (exit 0).

- [ ] **Step 4: Validate .claude/settings.json**

```bash
python3 -c "import json; json.load(open('.claude/settings.json'))" && echo "valid"
```

Expected: `valid`

- [ ] **Step 5: Verify CONTEXT.md governance authority language**

```bash
grep -c "cannot weaken" CONTEXT.md && grep -c "has no effect on container governance" CONTEXT.md
```

Expected: `1` then `1`

- [ ] **Step 6: Verify README.md has Governance Layer section**

```bash
grep -c "Governance Layer Deployment" README.md
```

Expected: `1`

- [ ] **Step 7: Verify historical banners in spec and runbook**

```bash
grep -c "Historical document" docs/runbooks/red-green-demo.md
grep -c "Historical document" docs/spec/engineering-spec.md
```

Expected: `1` then `1`

- [ ] **Step 8: Verify pre-push.sh remote contract is complete**

```bash
grep -E 'client_cert|client_key|client_ca|bridge_addr_is_https|remote_mode' hooks/pre-push.sh | wc -l
```

Expected: at least 8 lines.

- [ ] **Step 9: Verify bin/calm-test no longer defaults to /tmp**

```bash
grep 'STACK_FITNESS_FUNCTIONS_BIN.*tmp' bin/calm-test || echo "clean"
```

Expected: `clean`

- [ ] **Step 10: Commit smoke check results (no-op if no files changed)**

```bash
git status
```

Expected: clean working tree. All tasks committed individually above.

---

## Self-Review Against Spec

| Spec requirement | Task covering it |
|-----------------|-----------------|
| Explaination.md: remove stale `.calm/config.json` and `/tmp/calm-bridge` references | Task 2 |
| README.md: add Governance Layer Deployment section, frame as enterprise layer | Task 3 |
| CONTEXT.md: clarify governed repo cannot weaken via local config | Task 4 |
| red-green-demo.md: historical banner | Task 5 |
| engineering-spec.md: historical banner | Task 5 |
| pre-push.sh: mTLS forwarding, STACK_FITNESS_FUNCTIONS_REPO_NAME, HTTPS enforcement, remote_mode | Task 6 |
| pre-tool-use.sh: HTTPS enforcement for remote bridges | Task 7 |
| .claude/settings.json: audit/restore PreToolUse | Task 8 |
| bin/calm-test: remote container support, sandbox label | Task 9 |
| docker-compose.yml: Helm chart is external | Task 10 |
| Purge __pycache__ from git tracking | Task 1 |
| Smoke check verification | Task 11 |
| shellcheck compliance | Tasks 6, 7, 9 (step-level) + Task 11 |
| No broken /tmp references remaining | Task 5 (runbook), Task 9 (bin), Task 11 (audit) |
