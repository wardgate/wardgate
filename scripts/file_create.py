#!/usr/bin/env python3
"""Create a new file. Fails if the file already exists."""

import os
import sys


def safe_path(p):
    resolved = os.path.realpath(p)
    cwd = os.path.realpath(os.getcwd())
    if not resolved.startswith(cwd + os.sep) and resolved != cwd:
        print(f"Error: path '{p}' resolves outside working directory", file=sys.stderr)
        sys.exit(1)
    return resolved


def main():
    if len(sys.argv) != 3:
        print("Usage: file_create.py <file> <content>", file=sys.stderr)
        sys.exit(1)

    path = safe_path(sys.argv[1])
    content = sys.argv[2]

    if os.path.exists(path):
        print(f"Error: '{sys.argv[1]}' already exists (use update to overwrite)", file=sys.stderr)
        sys.exit(1)

    # Create parent directories as needed
    parent = os.path.dirname(path)
    if parent and not os.path.isdir(parent):
        os.makedirs(parent, exist_ok=True)

    with open(path, "w") as f:
        f.write(content)

    size = len(content.encode("utf-8"))
    print(f"Created {sys.argv[1]} ({size} bytes)")


if __name__ == "__main__":
    main()
