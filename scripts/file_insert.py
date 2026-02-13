#!/usr/bin/env python3
"""Insert text at a location in an existing file.

Usage: file_insert.py <file> <text> [--after-line N] [--after-match PAT] [--before-match PAT] [--append]

Default (no flags): append to end of file.
"""

import os
import sys
import tempfile


def safe_path(p):
    resolved = os.path.realpath(p)
    cwd = os.path.realpath(os.getcwd())
    if not resolved.startswith(cwd + os.sep) and resolved != cwd:
        print(f"Error: path '{p}' resolves outside working directory", file=sys.stderr)
        sys.exit(1)
    return resolved


def context_around(lines, insert_line, ctx=2):
    """Show context around the insertion point. insert_line is 0-indexed."""
    first = max(0, insert_line - ctx)
    last = min(len(lines), insert_line + ctx + 1)
    result = []
    for i in range(first, last):
        marker = "+" if i == insert_line else " "
        result.append(f"{marker} {i + 1:6} | {lines[i].rstrip()}")
    return "\n".join(result)


def main():
    args = sys.argv[1:]

    # Parse flags
    after_line = None
    after_match = None
    before_match = None
    append_mode = False

    positional = []
    i = 0
    while i < len(args):
        if args[i] == "--after-line" and i + 1 < len(args):
            after_line = int(args[i + 1])
            i += 2
        elif args[i] == "--after-match" and i + 1 < len(args):
            after_match = args[i + 1]
            i += 2
        elif args[i] == "--before-match" and i + 1 < len(args):
            before_match = args[i + 1]
            i += 2
        elif args[i] == "--append":
            append_mode = True
            i += 1
        else:
            positional.append(args[i])
            i += 1

    if len(positional) != 2:
        print("Usage: file_insert.py <file> <text> [--after-line N] [--after-match PAT] [--before-match PAT] [--append]", file=sys.stderr)
        sys.exit(1)

    path = safe_path(positional[0])
    text = positional[1]

    if not os.path.isfile(path):
        print(f"Error: '{positional[0]}' does not exist", file=sys.stderr)
        sys.exit(1)

    with open(path, "r") as f:
        content = f.read()

    lines = content.splitlines(True)

    # Determine insertion point (0-indexed line to insert BEFORE)
    if after_line is not None:
        if after_line < 0 or after_line > len(lines):
            print(f"Error: line {after_line} out of range (file has {len(lines)} lines)", file=sys.stderr)
            sys.exit(1)
        insert_at = after_line  # after line N means insert before line N+1 (0-indexed: N)
        location = f"after line {after_line}"
    elif after_match is not None:
        idx = None
        for j, line in enumerate(lines):
            if after_match in line:
                idx = j
                break
        if idx is None:
            print(f"Error: no line contains '{after_match}'", file=sys.stderr)
            sys.exit(1)
        insert_at = idx + 1
        location = f"after match at line {idx + 1}"
    elif before_match is not None:
        idx = None
        for j, line in enumerate(lines):
            if before_match in line:
                idx = j
                break
        if idx is None:
            print(f"Error: no line contains '{before_match}'", file=sys.stderr)
            sys.exit(1)
        insert_at = idx
        location = f"before match at line {idx + 1}"
    else:
        # Default: append
        insert_at = len(lines)
        location = "end of file"

    # Ensure text ends with newline for clean insertion
    insert_text = text if text.endswith("\n") else text + "\n"

    # Ensure the line before insertion ends with newline
    if insert_at > 0 and lines and not lines[insert_at - 1].endswith("\n"):
        lines[insert_at - 1] += "\n"

    # Insert
    insert_lines = insert_text.splitlines(True)
    new_lines = lines[:insert_at] + insert_lines + lines[insert_at:]
    new_content = "".join(new_lines)

    # Atomic write
    parent = os.path.dirname(path)
    fd, tmp = tempfile.mkstemp(dir=parent, prefix=".wardgate_tmp_")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(new_content)
        os.replace(tmp, path)
    except Exception:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise

    print(f"Inserted {len(insert_lines)} line(s) at {location} in {positional[0]}")
    print()
    print(context_around(new_lines, insert_at))


if __name__ == "__main__":
    main()
