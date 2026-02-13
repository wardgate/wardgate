#!/usr/bin/env python3
"""Replace entire file contents. Fails if the file does not exist."""

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


def main():
    if len(sys.argv) != 3:
        print("Usage: file_update.py <file> <content>", file=sys.stderr)
        sys.exit(1)

    path = safe_path(sys.argv[1])
    content = sys.argv[2]

    if not os.path.isfile(path):
        print(f"Error: '{sys.argv[1]}' does not exist (use create for new files)", file=sys.stderr)
        sys.exit(1)

    # Atomic write: write to temp file in same directory, then rename
    parent = os.path.dirname(path)
    fd, tmp = tempfile.mkstemp(dir=parent, prefix=".wardgate_tmp_")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(content)
        os.replace(tmp, path)
    except Exception:
        # Clean up temp file on failure
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise

    size = len(content.encode("utf-8"))
    print(f"Updated {sys.argv[1]} ({size} bytes)")


if __name__ == "__main__":
    main()
