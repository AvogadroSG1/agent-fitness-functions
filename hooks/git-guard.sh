#!/usr/bin/env bash
# agent-fitness-functions git-guard PreToolUse hook
#
# Blocks git commands that bypass fitness-function enforcement before they
# execute (git commit --no-verify/-n, --no-gpg-sign, merge/pull --ff-only,
# push --force/-f).
#
# Design (RFC 2119 terms):
#   - The hook MUST tokenize the shell command with Python's shlex (posix
#     mode, punctuation_chars=";|&()<>\n") rather than matching regexes
#     against the raw string. Tokenizing means quoted text (e.g. a commit
#     message containing the literal string "--no-verify") is inert data,
#     and flags are attributed to the specific git invocation and
#     subcommand that owns them rather than to the whole command line.
#     Newline is added to punctuation_chars so a bare, unquoted newline
#     acts as a command separator (mirroring ';'), while a newline embedded
#     inside a quoted argument remains part of that single token.
#   - The token stream is split into simple-command segments at operator
#     tokens (';', '|', '&', '(', ')', '<', '>', the synthesized '&&'/'||'
#     tokens, and newline). Each segment is scanned independently for a
#     'git' invocation, its subcommand, and the flags that follow it.
#   - Before tokenizing, heredoc bodies are stripped (strip_heredocs): the
#     introducer line (containing the '<<DELIM' marker) is kept so its git
#     invocation is still scanned, but the body lines up to the matching
#     delimiter line are dropped. This MUST happen because (a) heredoc body
#     text is data, not a command, so scanning it for flags is a false-
#     positive source, and (b) an apostrophe or other unbalanced quote
#     character inside a heredoc body would otherwise make the whole
#     command look unterminated to the tokenizer and force the command onto
#     the degraded fallback path unnecessarily.
#   - A segment whose command word is bash/sh/zsh/dash/ksh with a '-c'-style
#     flag (e.g. -c, -lc), or whose command word is eval, is a shell
#     wrapper: the hook recurses into the wrapped command-string argument
#     with the same scan, because 'bash -c "git commit -n"' is a bypass
#     that a token-boundary check would otherwise miss entirely (the whole
#     quoted body is inert data to a naive scan). This recursion is scoped
#     to actual command words at the start of a segment, so quoted text
#     that merely mentions "bash -c" inside a commit message (which is a
#     single argument value, never its own segment) is correctly left
#     alone. 'ssh host "..."' is deliberately OUT OF SCOPE: that command
#     runs on a remote host against a remote repository, where this local
#     hook has no visibility or enforcement authority anyway. Shell
#     wrappers nested as ARGUMENTS to command runners (e.g.
#     'xargs bash -c "..."', 'timeout 5 bash -c "..."', 'parallel',
#     'find -exec sh -c ... +') are likewise OUT OF SCOPE: the runner
#     list is unbounded, so the recursion only fires when the wrapper is
#     the segment's own command word.
#   - Variable-indirected subcommands and flags (e.g. `git $SUB -n`, or
#     `FLAG=-n; git commit $FLAG`) are OUT OF SCOPE: the hook has no shell
#     execution context, so it cannot resolve variable expansions, and
#     treating an unresolved '$SUB' token as a subcommand would be both
#     unreliable and easy to defeat in the other direction.
#   - All of this MUST run in a single python3 invocation per Bash call
#     (previously up to three separate python3 spawns per call).
#   - If the command cannot be tokenized (e.g. an unterminated quote, or
#     bash ANSI-C $'...' quoting which posix shlex does not understand),
#     the hook retries once with a trailing newline appended (heals a
#     dangling bash line-continuation) and then, if it still fails to
#     parse, SHOULD fail closed by degrading to narrow regex checks against
#     the raw string: long-flag checks (--no-verify, --no-gpg-sign,
#     --ff-only, --force) plus segment-scoped short-flag checks restricted
#     to text between a 'git commit'/'git push' anchor and the next segment
#     boundary (';', '|', '&', or newline). The degraded short-flag regexes
#     are intentionally cruder than the tokenizer's per-argument attribution
#     (they can still be fooled, e.g. by a short flag mentioned inside an
#     unparseable segment's own message text) but this is an accepted
#     tradeoff: the command could not be safely parsed at all, so failing
#     closed on a plausible-looking bypass is preferred over failing open.
#     A separate, non-blocking gap: the short-flag-cluster patterns require
#     letters only (`-[a-zA-Z]+`), so a digit-bearing cluster like `-f4`
#     (e.g. some non-git tools' short options) will not match; this is a
#     known, accepted limitation rather than a design goal.
#   - Bypasses hidden inside $(...) command substitutions or backtick-quoted
#     spans are extracted from the original string with a balanced-paren/
#     backtick scan and recursively re-checked (same recursion depth cap as
#     shell-wrapper recursion). This substitution extraction is a
#     heuristic: it does not itself understand quoting, so it can be fooled
#     by adversarial input, but it materially closes the
#     'echo "$(git commit -n -m x)"' class of bypass.
set -euo pipefail

payload=$(cat)

deny() {
  echo "agent-fitness-functions git-guard: $1" >&2
  echo "  Fix fitness-function violations in the code rather than bypassing enforcement." >&2
  exit 2
}

verdict=$(python3 - "$payload" <<'PY'
import json
import re
import shlex
import sys

payload = sys.argv[1]
try:
    parsed = json.loads(payload)
    if not isinstance(parsed, dict):
        tool_input = {}
    else:
        tool_input = parsed.get("tool_input") or parsed.get("args") or parsed
        if not isinstance(tool_input, dict):
            tool_input = {}
    command = tool_input.get("command") or tool_input.get("cmd", "")
    if not isinstance(command, str):
        command = ""
except Exception:
    command = ""

if not command:
    print("ALLOW")
    sys.exit(0)

# ---- rule table -----------------------------------------------------------
# Per git subcommand: the option names that consume a following value token
# (so their argument is never mistaken for a bypass flag), and the function
# that inspects the remaining flag tokens for a bypass.

COMMIT_VALUE_OPTS = {
    "-m", "-F", "-c", "-C", "-t",
    "--message", "--file", "--author", "--date",
    "--fixup", "--squash", "--template", "--trailer", "--cleanup",
}
PUSH_VALUE_OPTS = {
    "-o", "--push-option", "--receive-pack", "--repo", "--exec",
}
GIT_GLOBAL_VALUE_OPTS = {
    "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path",
}

NO_VERIFY_MSG = (
    "'git commit --no-verify' is blocked. agent-fitness-functions hooks must run."
)
NO_VERIFY_SHORT_MSG = (
    "'git commit -n' (--no-verify shorthand) is blocked. "
    "agent-fitness-functions hooks must run."
)
NO_GPG_SIGN_MSG = "'git commit --no-gpg-sign' is blocked."
FF_ONLY_MSG = (
    "'git merge/pull --ff-only' is blocked when agent-fitness-functions "
    "enforcement is active. Use a regular merge or rebase so the "
    "agent-fitness-functions pre-commit hook fires."
)
FORCE_PUSH_MSG = (
    "'git push --force' / 'git push -f' is blocked. "
    "Use --force-with-lease if you must force push."
)

SHORT_CLUSTER = re.compile(r"-[a-zA-Z]+")


def check_commit_flags(flags):
    for tok in flags:
        if tok == "--no-verify":
            return NO_VERIFY_MSG
        if tok == "--no-gpg-sign":
            return NO_GPG_SIGN_MSG
        if SHORT_CLUSTER.fullmatch(tok) and "n" in tok:
            return NO_VERIFY_SHORT_MSG
    return None


def check_merge_pull_flags(flags):
    for tok in flags:
        if tok == "--ff-only":
            return FF_ONLY_MSG
    return None


def check_push_flags(flags):
    for tok in flags:
        if tok == "--force":
            return FORCE_PUSH_MSG
        if SHORT_CLUSTER.fullmatch(tok) and "f" in tok:
            return FORCE_PUSH_MSG
    return None


RULES = {
    "commit": (COMMIT_VALUE_OPTS, check_commit_flags),
    "merge": (set(), check_merge_pull_flags),
    "pull": (set(), check_merge_pull_flags),
    "push": (PUSH_VALUE_OPTS, check_push_flags),
}

OPERATOR_CHARS = set(";|&()<>\n")


def is_operator_token(tok):
    return len(tok) > 0 and all(c in OPERATOR_CHARS for c in tok)


# Heredoc introducer detector: '<<' or '<<-', an optional matching quote
# around the delimiter word, and the delimiter itself (backreferenced so
# "<<'EOF'" and '<<EOF' both match correctly).
HEREDOC_RE = re.compile(r"<<-?\s*([\"']?)(\w+)\1")


def strip_heredocs(cmd):
    # Drop heredoc BODY lines (they are data, not command text) while
    # keeping the introducer line itself so its git invocation is still
    # scanned normally. Operates on raw text, line by line, before
    # tokenization; see header comment for rationale.
    lines = cmd.split("\n")
    out = []
    i = 0
    n = len(lines)
    while i < n:
        line = lines[i]
        match = HEREDOC_RE.search(line)
        if match:
            delimiter = match.group(2)
            out.append(line)
            i += 1
            while i < n and lines[i].strip() != delimiter:
                i += 1
            if i < n:
                i += 1  # also skip the delimiter line itself
            continue
        out.append(line)
        i += 1
    return "\n".join(out)


def tokenize(cmd):
    lex = shlex.shlex(cmd, posix=True, punctuation_chars=";|&()<>\n")
    lex.whitespace = " \t\r"
    lex.whitespace_split = True
    return list(lex)


def split_segments(tokens):
    segments = []
    current = []
    for tok in tokens:
        if is_operator_token(tok):
            if current:
                segments.append(current)
                current = []
        else:
            current.append(tok)
    if current:
        segments.append(current)
    return segments


def find_git_subcommand(segment):
    git_idx = None
    for i, tok in enumerate(segment):
        if tok == "git" or tok.endswith("/git"):
            git_idx = i
            break
    if git_idx is None:
        return None, None
    i = git_idx + 1
    n = len(segment)
    while i < n:
        tok = segment[i]
        if tok in GIT_GLOBAL_VALUE_OPTS:
            i += 2
            continue
        if tok.startswith("-"):
            i += 1
            continue
        return tok, i + 1
    return None, None


def collect_flags(segment, start, value_opts):
    flags = []
    i = start
    n = len(segment)
    while i < n:
        tok = segment[i]
        if tok == "--":
            break
        if tok in value_opts:
            i += 2
            continue
        if "=" in tok and tok.split("=", 1)[0] in value_opts:
            i += 1
            continue
        flags.append(tok)
        i += 1
    return flags


# ---- shell-wrapper recursion -----------------------------------------------
# A segment that itself invokes bash/sh/... -c "..." or eval "..." can hide
# a bypass inside a single quoted argument, which never gets its own segment
# and so is invisible to the git scan above. Find that argument (if any) and
# recurse the whole scan on it. See header comment for the ssh/variable
# out-of-scope carve-outs.

WRAPPER_PREFIX_CMDS = {"command", "env", "nohup", "time"}
SHELL_WRAPPERS = {"bash", "sh", "zsh", "dash", "ksh"}
ENV_ASSIGNMENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=")


def command_word(segment):
    # Return (index, basename) of the first real command word in a segment,
    # skipping leading env-var assignments and known no-op prefix commands
    # (env, nohup, time, command).
    i = 0
    n = len(segment)
    while i < n:
        tok = segment[i]
        if ENV_ASSIGNMENT.match(tok):
            i += 1
            continue
        base = tok.rsplit("/", 1)[-1]
        if base in WRAPPER_PREFIX_CMDS:
            i += 1
            continue
        return i, base
    return None, None


def find_wrapper_target(segment):
    idx, word = command_word(segment)
    if word is None:
        return None
    n = len(segment)
    if word in SHELL_WRAPPERS:
        i = idx + 1
        found_c_flag = False
        while i < n and segment[i].startswith("-"):
            if "c" in segment[i]:
                found_c_flag = True
            i += 1
        if found_c_flag and i < n:
            return segment[i]
        return None
    if word == "eval":
        rest = segment[idx + 1 :]
        if rest:
            return " ".join(rest)
        return None
    return None


# ---- degraded fallback ------------------------------------------------------
# Only used when the command cannot be tokenized at all (e.g. an
# unterminated quote, or bash ANSI-C $'...' quoting). Long-flag checks plus
# segment-scoped short-flag checks (see header comment for the fail-closed
# tradeoff this represents).
DEGRADED_RULES = [
    (re.compile(r"git\s+commit\b.*--no-verify"), NO_VERIFY_MSG),
    (re.compile(r"git\s+commit\b.*--no-gpg-sign"), NO_GPG_SIGN_MSG),
    (re.compile(r"git\s+(merge|pull)\b.*--ff-only"), FF_ONLY_MSG),
]

DEGRADED_SHORT_COMMIT_RE = re.compile(
    r"git\s+commit\b[^;|&\n]*(^|\s)-[a-zA-Z]*n[a-zA-Z]*(\s|$)", re.MULTILINE
)
DEGRADED_SHORT_PUSH_RE = re.compile(
    r"git\s+push\b[^;|&\n]*(^|\s)-[a-zA-Z]*f[a-zA-Z]*(\s|$)", re.MULTILINE
)


def degraded_force_push(cmd):
    if not re.search(r"git\s+push\b", cmd):
        return False
    if not re.search(r"--force\b", cmd):
        return False
    if re.search(r"--force-with-lease\b", cmd):
        return False
    return True


def degraded_check(cmd):
    for pattern, message in DEGRADED_RULES:
        if pattern.search(cmd):
            return message + " (command could not be fully parsed)"
    if degraded_force_push(cmd):
        return FORCE_PUSH_MSG + " (command could not be fully parsed)"
    if DEGRADED_SHORT_COMMIT_RE.search(cmd):
        return NO_VERIFY_SHORT_MSG + " (command could not be fully parsed)"
    if DEGRADED_SHORT_PUSH_RE.search(cmd):
        return FORCE_PUSH_MSG + " (command could not be fully parsed)"
    return None


def extract_substitutions(cmd):
    # Heuristic extraction of $(...) and backtick-quoted spans from the raw
    # string, so bypasses hidden in command substitutions still get scanned.
    # Does not itself understand quoting (see header comment: residual
    # limitation). NOTE: the backtick character is built via chr(96) rather
    # than written literally, because this Python source lives inside a
    # bash "$(python3 ... <<'PY' ... PY)" command substitution, and even
    # though the heredoc delimiter is quoted (so its body is never
    # expanded), bash's parser still lexically pairs up literal backtick
    # characters wherever they appear in the script — including inside this
    # heredoc — because a backquote pair is itself another form of command
    # substitution syntax that the parser must at least tokenize. Two
    # unrelated single-backtick string literals here would otherwise form
    # an accidental pairing and break the whole script's bash syntax.
    backtick = chr(96)
    subs = []
    i = 0
    n = len(cmd)
    while i < n:
        if cmd[i] == "$" and i + 1 < n and cmd[i + 1] == "(":
            depth = 1
            j = i + 2
            while j < n and depth > 0:
                if cmd[j] == "(":
                    depth += 1
                elif cmd[j] == ")":
                    depth -= 1
                j += 1
            subs.append(cmd[i + 2 : j - 1 if depth == 0 else j])
            i = j
        elif cmd[i] == backtick:
            j = cmd.find(backtick, i + 1)
            if j == -1:
                break
            subs.append(cmd[i + 1 : j])
            i = j + 1
        else:
            i += 1
    return subs


def scan_command(cmd, depth=0):
    if depth > 5:
        return None

    cmd = strip_heredocs(cmd)

    parsed_ok = True
    tokens = []
    try:
        tokens = tokenize(cmd)
    except ValueError:
        try:
            tokens = tokenize(cmd + "\n")
        except ValueError:
            parsed_ok = False

    if parsed_ok:
        for segment in split_segments(tokens):
            subcommand, start = find_git_subcommand(segment)
            if subcommand is not None:
                rule = RULES.get(subcommand)
                if rule is not None:
                    value_opts, checker = rule
                    flags = collect_flags(segment, start, value_opts)
                    message = checker(flags)
                    if message:
                        return message
            wrapper_target = find_wrapper_target(segment)
            if wrapper_target is not None:
                message = scan_command(wrapper_target, depth + 1)
                if message:
                    return message
    else:
        # Fail closed rather than silently allowing an unparseable command
        # through untouched; still fall through to substitution extraction
        # below so a bypass hidden in $(...) / backticks is not missed just
        # because the outer command also failed to tokenize.
        degraded_message = degraded_check(cmd)
        if degraded_message:
            return degraded_message

    for inner in extract_substitutions(cmd):
        message = scan_command(inner, depth + 1)
        if message:
            return message
    return None


verdict = scan_command(command)
if verdict:
    print("DENY")
    print(verdict)
else:
    print("ALLOW")
PY
)

status=$(printf '%s\n' "$verdict" | head -n1)
if [[ "$status" == "DENY" ]]; then
  message=$(printf '%s\n' "$verdict" | tail -n +2)
  deny "$message"
fi

exit 0
