#!/usr/bin/env python3
"""Replace exact text in a file with context feedback.

Usage: file_patch.py <file> <old> <new> [--all] [--normalize-ws]

Default: replace first exact match.
--all:          replace all occurrences
--normalize-ws: collapse whitespace for matching, preserve original in replacement
"""

import os
import re
import sys
import tempfile


def safe_path(p):
    resolved = os.path.realpath(p)
    cwd = os.path.realpath(os.getcwd())
    if not resolved.startswith(cwd + os.sep) and resolved != cwd:
        print(f"Error: path '{p}' resolves outside working directory", file=sys.stderr)
        sys.exit(1)
    return resolved


def normalize_ws(s):
    """Collapse runs of whitespace to single spaces, strip edges."""
    return re.sub(r"\s+", " ", s).strip()


def find_line_col(text, pos):
    """Return (line_number, col) for a character position in text. 1-indexed."""
    line = text[:pos].count("\n") + 1
    last_nl = text.rfind("\n", 0, pos)
    col = pos - last_nl  # 1-indexed
    return line, col


def context_lines(text, start, end, ctx=3):
    """Return numbered context lines around a match span."""
    lines = text.splitlines(True)
    match_start_line = text[:start].count("\n")
    match_end_line = text[:end].count("\n")

    first = max(0, match_start_line - ctx)
    last = min(len(lines), match_end_line + ctx + 1)

    result = []
    for i in range(first, last):
        marker = ">" if match_start_line <= i <= match_end_line else " "
        result.append(f"{marker} {i + 1:6} | {lines[i].rstrip()}")
    return "\n".join(result)


def find_closest_match(content, old_text):
    """Find the closest substring match using normalized whitespace comparison.

    Returns (original_snippet, line_number, similarity) or None.
    """
    target = normalize_ws(old_text)
    if not target:
        return None

    lines = content.splitlines(True)
    best_score = 0
    best_snippet = None
    best_line = 0

    # Estimate window size from the old_text line count
    old_lines = old_text.count("\n") + 1
    window = max(old_lines, 1)

    for i in range(len(lines)):
        end = min(i + window + 2, len(lines))  # slight overreach for whitespace
        for j in range(i + 1, end + 1):
            chunk = "".join(lines[i:j])
            norm_chunk = normalize_ws(chunk)
            if not norm_chunk:
                continue

            # Simple ratio: length of common prefix + suffix vs total
            score = _similarity(target, norm_chunk)
            if score > best_score:
                best_score = score
                best_snippet = chunk.rstrip("\n")
                best_line = i + 1

    if best_score > 0.5:
        return best_snippet, best_line, best_score
    return None


def _similarity(a, b):
    """Simple similarity ratio between two strings."""
    if not a or not b:
        return 0.0
    # Count matching chars from start and end
    prefix = 0
    for ca, cb in zip(a, b):
        if ca == cb:
            prefix += 1
        else:
            break
    suffix = 0
    for ca, cb in zip(reversed(a[prefix:]), reversed(b[prefix:])):
        if ca == cb:
            suffix += 1
        else:
            break
    matched = prefix + suffix
    total = max(len(a), len(b))
    return matched / total


def main():
    args = sys.argv[1:]
    replace_all = "--all" in args
    norm_ws = "--normalize-ws" in args
    for flag in ("--all", "--normalize-ws"):
        while flag in args:
            args.remove(flag)

    if len(args) != 3:
        print("Usage: file_patch.py <file> <old> <new> [--all] [--normalize-ws]", file=sys.stderr)
        sys.exit(1)

    path = safe_path(args[0])
    old_text = args[1]
    new_text = args[2]

    if not os.path.isfile(path):
        print(f"Error: '{args[0]}' does not exist", file=sys.stderr)
        sys.exit(1)

    with open(path, "r") as f:
        content = f.read()

    if norm_ws:
        # Build a mapping from normalized positions to original spans
        target_norm = normalize_ws(old_text)
        matches = []
        lines = content.splitlines(True)
        old_line_count = old_text.count("\n") + 1
        window = max(old_line_count, 1)

        for i in range(len(lines)):
            for j in range(i + 1, min(i + window + 3, len(lines) + 1)):
                chunk = "".join(lines[i:j])
                if normalize_ws(chunk) == target_norm:
                    start = sum(len(lines[k]) for k in range(i))
                    matches.append((start, start + len(chunk)))
                    break

        if not matches:
            _report_no_match(content, old_text, args[0])
            sys.exit(1)

        if not replace_all:
            matches = matches[:1]

        # Apply replacements in reverse order to preserve positions
        new_content = content
        for start, end in reversed(matches):
            new_content = new_content[:start] + new_text + new_content[end:]

        _write_atomic(path, new_content)
        count = len(matches)
        print(f"Replaced {count} occurrence(s) in {args[0]}")
        # Show context of first match
        line, _ = find_line_col(content, matches[0][0])
        print(f"\nFirst match at line {line}:")
        print(context_lines(new_content, matches[0][0], matches[0][0] + len(new_text)))
    else:
        # Exact match mode
        count = content.count(old_text)
        if count == 0:
            _report_no_match(content, old_text, args[0])
            sys.exit(1)

        if replace_all:
            new_content = content.replace(old_text, new_text)
            _write_atomic(path, new_content)
            pos = new_content.find(new_text)
            line, _ = find_line_col(new_content, pos)
            print(f"Replaced {count} occurrence(s) in {args[0]}")
            print(f"\nFirst replacement at line {line}:")
            print(context_lines(new_content, pos, pos + len(new_text)))
        else:
            pos = content.find(old_text)
            new_content = content[:pos] + new_text + content[pos + len(old_text):]
            _write_atomic(path, new_content)
            line, _ = find_line_col(new_content, pos)
            print(f"Replaced 1 occurrence in {args[0]}")
            print(f"\nReplacement at line {line}:")
            print(context_lines(new_content, pos, pos + len(new_text)))


def _report_no_match(content, old_text, filename):
    """Report no match found and suggest closest alternative."""
    print(f"Error: no match found in {filename}", file=sys.stderr)
    closest = find_closest_match(content, old_text)
    if closest:
        snippet, line, score = closest
        print(f"\nClosest match at line {line} ({score:.0%} similar):", file=sys.stderr)
        # Show the snippet with context
        lines = content.splitlines()
        first = max(0, line - 2)
        last = min(len(lines), line + snippet.count("\n") + 2)
        for i in range(first, last):
            marker = ">" if line - 1 <= i <= line - 1 + snippet.count("\n") else " "
            print(f"{marker} {i + 1:6} | {lines[i]}", file=sys.stderr)


def _write_atomic(path, content):
    """Write content to file atomically via temp + rename."""
    parent = os.path.dirname(path)
    fd, tmp = tempfile.mkstemp(dir=parent, prefix=".wardgate_tmp_")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(content)
        os.replace(tmp, path)
    except Exception:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


if __name__ == "__main__":
    main()
