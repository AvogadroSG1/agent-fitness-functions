#!/usr/bin/env python3
"""Rebuild the proposed content of every file a Codex apply_patch payload touches.

Usage: apply-patch-proposals.py <payload.json> <repo root> <output dir>

The repo root MUST already be canonical (pwd -P). On success this exits 0 and
writes one NUL-terminated triple per surviving target, in first-touched order:
<repo-relative path>\\0<content file>\\0<binary flag "0"|"1">\\0. A binary target
has an empty content-file field and nothing written for it. On failure it exits 1
with one diagnostic line on stderr.

Parsing and application port codex-rs/apply-patch (parser, file_update,
seek_sequence) in its default NormalizeToLf mode. Every target, including move
destinations, is contained in the repository before any file is read.
"""

import json
import os
import sys

_BEGIN = "*** Begin Patch"
_END = "*** End Patch"
_ADD = "*** Add File: "
_DELETE = "*** Delete File: "
_UPDATE = "*** Update File: "
_ENVIRONMENT = "*** Environment ID: "
_MOVE = "*** Move to: "
_END_OF_FILE = "*** End of File"
_HEREDOC_OPENERS = ("<<EOF", "<<'EOF'", '<<"EOF"')
_HUNK_HEADERS = ((_ADD, "add"), (_DELETE, "delete"), (_UPDATE, "update"))
_UNICODE_PUNCTUATION = str.maketrans(
    "\u2010\u2011\u2012\u2013\u2014\u2015\u2212"
    "\u2018\u2019\u201a\u201b"
    "\u201c\u201d\u201e\u201f"
    "\u00a0\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u202f\u205f\u3000",
    "-" * 7 + "'" * 4 + '"' * 4 + " " * 13,
)


class PatchError(Exception):
    """A patch that cannot be applied; the message is shown to the agent."""


def _patch_text(payload):
    tool_input = payload.get("tool_input") if isinstance(payload, dict) else None
    if isinstance(tool_input, dict):
        tool_input = tool_input.get("command")
    if not isinstance(tool_input, str):
        raise PatchError("apply_patch payload has no patch text in tool_input.command")
    return tool_input


def _patch_body(text):
    lines = [line[:-1] if line.endswith("\r") else line for line in text.strip().split("\n")]
    try:
        return _strict_body(lines)
    except PatchError:
        if not _is_heredoc(lines):
            raise
        return _strict_body(lines[1:-1])


def _is_heredoc(lines):
    return len(lines) >= 4 and lines[0] in _HEREDOC_OPENERS and lines[-1].endswith("EOF")


def _strict_body(lines):
    if lines[0].strip() != _BEGIN:
        raise PatchError("The first line of the patch must be '*** Begin Patch'")
    if len(lines) < 2 or lines[-1].strip() != _END:
        raise PatchError("The last line of the patch must be '*** End Patch'")
    return lines[1:-1]


def _new_chunk(context):
    return {"context": context, "old": [], "new": [], "eof": False}


def _is_empty(chunk):
    return not chunk["old"] and not chunk["new"]


def _is_marker(text):
    return text == "@@" or text.startswith("@@ ")


def _invalid_header(line):
    return PatchError(f"'{line.strip()}' is not a valid hunk header")


def _unexpected_line(line):
    return PatchError(
        f"Unexpected line found in update hunk: '{line}'. Every line should start with "
        "' ' (context line), '+' (added line), or '-' (removed line)"
    )


def _expected_marker(line):
    return PatchError(f"Expected update hunk to start with a @@ context marker, got: '{line}'")


def _classify(line):
    if line == "":
        return " ", ""
    if line[0] in " +-":
        return line[0], line[1:]
    return None, line


class _Parser:
    """Line-at-a-time apply_patch parser producing an ordered list of hunks."""

    def __init__(self):
        self.hunks = []
        self.mode = "started"

    def parse(self, lines):
        handlers = {"started": self._started, "add": self._add, "delete": self._delete, "update": self._update}
        for line in lines:
            handlers[self.mode](line)
        self._check_last(_END)
        return self.hunks

    def _header(self, text):
        if text == _END:
            raise PatchError("'*** End Patch' must be the last line of the patch")
        if text.startswith(_ENVIRONMENT) and not self.hunks:
            return True
        for prefix, kind in _HUNK_HEADERS:
            if text.startswith(prefix):
                self._start(kind, text[len(prefix):], text)
                return True
        return False

    def _start(self, kind, path, header):
        self._check_last(header)
        self.hunks.append({"kind": kind, "path": path, "contents": "", "move": None, "chunks": []})
        self.mode = kind

    def _check_last(self, line):
        """Reject an update hunk that ends, at *line*, without a single change line."""
        if not self.hunks or self.hunks[-1]["kind"] != "update":
            return
        hunk = self.hunks[-1]
        if not hunk["chunks"]:
            raise PatchError(f"Update file hunk for path '{hunk['path']}' is empty")
        if not _is_empty(hunk["chunks"][-1]):
            return
        if line == _END:
            raise PatchError("Update hunk does not contain any lines")
        raise _unexpected_line(line)

    def _started(self, line):
        if not self._header(line.strip()):
            raise _invalid_header(line)

    def _add(self, line):
        if self._header(line.strip()):
            return
        if not line.startswith("+"):
            raise _invalid_header(line)
        self.hunks[-1]["contents"] += line[1:] + "\n"

    def _delete(self, line):
        if not self._header(line.strip()):
            raise _invalid_header(line)

    def _update(self, line):
        text = line.rstrip()
        if self._header(text) or self._skip_after_eof(text, line) or self._take_move(text):
            return
        chunks = self.hunks[-1]["chunks"]
        if _is_marker(text):
            self._marker(text, line, chunks)
        elif text == _END_OF_FILE:
            self._end_of_file(chunks)
        else:
            self._body_line(line, chunks)

    def _skip_after_eof(self, text, line):
        chunks = self.hunks[-1]["chunks"]
        if not chunks or not chunks[-1]["eof"]:
            return False
        if text == "":
            return True
        if not _is_marker(text):
            raise _expected_marker(line)
        return False

    def _take_move(self, text):
        hunk = self.hunks[-1]
        if hunk["chunks"] or hunk["move"] is not None or not text.startswith(_MOVE):
            return False
        hunk["move"] = text[len(_MOVE):]
        return True

    def _marker(self, text, line, chunks):
        if chunks and _is_empty(chunks[-1]):
            raise _unexpected_line(line)
        chunks.append(_new_chunk(None if text == "@@" else text[3:]))

    def _end_of_file(self, chunks):
        if not chunks or _is_empty(chunks[-1]):
            raise PatchError("Update hunk does not contain any lines")
        chunks[-1]["eof"] = True

    def _body_line(self, line, chunks):
        kind, text = _classify(line)
        if kind is None:
            raise _expected_marker(line) if chunks and not _is_empty(chunks[-1]) else _unexpected_line(line)
        if not chunks:
            chunks.append(_new_chunk(None))
        if kind in " -":
            chunks[-1]["old"].append(text)
        if kind in " +":
            chunks[-1]["new"].append(text)


def _resolve(path, base, repo):
    if not path:
        raise PatchError("empty target path in apply_patch")
    real = os.path.realpath(os.path.join(base, path))
    if real == repo or os.path.commonpath([repo, real]) != repo:
        raise PatchError(f"target resolves outside repository: {path}")
    return os.path.relpath(real, repo)


def _resolve_all(hunks, base, repo):
    for hunk in hunks:
        hunk["rel"] = _resolve(hunk["path"], base, repo)
        hunk["dest"] = hunk["rel"] if hunk["move"] is None else _resolve(hunk["move"], base, repo)


def _read_disk(repo, rel, path):
    try:
        with open(os.path.join(repo, rel), encoding="utf-8", newline="") as handle:
            return handle.read()
    except FileNotFoundError:
        return None
    except UnicodeDecodeError:
        raise PatchError(f"cannot read {path} as UTF-8 text") from None
    except OSError as exc:
        raise PatchError(f"cannot read {path}: {exc.strerror}") from None


class _Files:
    """Virtual file state so later hunks see the output of earlier ones."""

    def __init__(self, repo):
        self.repo = repo
        self.state = {}
        self.order = {}

    def set(self, rel, content):
        if content is not None:
            self.order.setdefault(rel)
        self.state[rel] = content

    def exists(self, rel):
        if rel in self.state:
            return self.state[rel] is not None
        return os.path.isfile(os.path.join(self.repo, rel))

    def read(self, rel, path):
        content = self.state[rel] if rel in self.state else _read_disk(self.repo, rel, path)
        if content is None:
            raise PatchError(f"file to update does not exist: {path}")
        return content

    def survivors(self):
        return [(rel, self.state[rel]) for rel in self.order if self.state[rel] is not None]


def _apply_add(hunk, files):
    files.set(hunk["rel"], hunk["contents"])


def _apply_delete(hunk, files):
    if not files.exists(hunk["rel"]):
        raise PatchError(f"file to delete does not exist: {hunk['path']}")
    files.set(hunk["rel"], None)


def _apply_update(hunk, files):
    content = _derive(files.read(hunk["rel"], hunk["path"]), hunk["chunks"], hunk["path"])
    if hunk["move"] is not None:
        files.set(hunk["rel"], None)
    files.set(hunk["dest"], content)


_APPLIERS = {"add": _apply_add, "delete": _apply_delete, "update": _apply_update}


def _derive(content, chunks, path):
    lines = content.split("\n")
    if lines[-1] == "":
        lines.pop()
    replacements = sorted(_replacements(lines, chunks, path), key=lambda item: item[0])
    for start, length, new in reversed(replacements):
        lines[start:start + length] = new
    if not lines or lines[-1] != "":
        lines.append("")
    return "\n".join(lines)


def _replacements(lines, chunks, path):
    index = 0
    result = []
    for chunk in chunks:
        index = _seek_context(lines, chunk, index, path)
        if not chunk["old"]:
            result.append((_insertion_index(lines), 0, chunk["new"]))
            continue
        start, length, new = _locate(lines, chunk, index, path)
        result.append((start, length, new))
        index = start + length
    return result


def _insertion_index(lines):
    return len(lines) - 1 if lines and lines[-1] == "" else len(lines)


def _seek_context(lines, chunk, index, path):
    context = chunk["context"]
    if context is None:
        return index
    found = _seek(lines, [context], index, False)
    if found is None:
        raise PatchError(f"failed to find context '{context}' in {path}")
    return found + 1


def _locate(lines, chunk, index, path):
    pattern, new = chunk["old"], chunk["new"]
    found = _seek(lines, pattern, index, chunk["eof"])
    if found is None and pattern[-1] == "":
        pattern = pattern[:-1]
        new = new[:-1] if new and new[-1] == "" else new
        found = _seek(lines, pattern, index, chunk["eof"])
    if found is None:
        raise PatchError(f"failed to find expected lines in {path}")
    return found, len(pattern), new


def _normalise(text):
    return text.strip().translate(_UNICODE_PUNCTUATION)


_NORMALIZERS = (str, str.rstrip, str.strip, _normalise)


def _seek(lines, pattern, start, eof):
    if not pattern:
        return start
    if len(pattern) > len(lines):
        return None
    first = len(lines) - len(pattern) if eof else start
    for normalise in _NORMALIZERS:
        found = _scan(lines, pattern, first, normalise)
        if found is not None:
            return found
    return None


def _scan(lines, pattern, first, normalise):
    wanted = [normalise(line) for line in pattern]
    for index in range(first, len(lines) - len(pattern) + 1):
        if all(normalise(lines[index + offset]) == want for offset, want in enumerate(wanted)):
            return index
    return None


def _emit(files, out_dir):
    records = []
    for index, (rel, content) in enumerate(files.survivors()):
        if "\x00" in content:
            records.append(f"{rel}\0\0" + "1\0")
            continue
        target = os.path.join(out_dir, str(index))
        with open(target, "w", encoding="utf-8", newline="") as handle:
            handle.write(content)
        records.append(f"{rel}\0{target}\0" + "0\0")
    sys.stdout.buffer.write("".join(records).encode("utf-8", "surrogateescape"))


def _run(payload_path, repo, out_dir):
    with open(payload_path, encoding="utf-8") as handle:
        payload = json.load(handle)
    hunks = _Parser().parse(_patch_body(_patch_text(payload)))
    cwd = payload.get("cwd")
    _resolve_all(hunks, cwd if isinstance(cwd, str) and cwd else os.getcwd(), repo)
    files = _Files(repo)
    for hunk in hunks:
        _APPLIERS[hunk["kind"]](hunk, files)
    _emit(files, out_dir)


def main():
    try:
        if len(sys.argv) != 4:
            raise PatchError("usage: apply-patch-proposals.py <payload.json> <repo root> <output dir>")
        _run(*sys.argv[1:])
    except PatchError as exc:
        print(exc, file=sys.stderr)
        return 1
    except Exception as exc:
        print(f"unexpected error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
