#!/usr/bin/env python3
"""Read file contents with numbered lines or raw output."""

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
    args = sys.argv[1:]
    raw = "--raw" in args
    if raw:
        args.remove("--raw")

    if len(args) != 1:
        print("Usage: file_read.py <file> [--raw]", file=sys.stderr)
        sys.exit(1)

    path = safe_path(args[0])

    if not os.path.isfile(path):
        print(f"Error: '{args[0]}' does not exist or is not a file", file=sys.stderr)
        sys.exit(1)

    with open(path, "r") as f:
        if raw:
            sys.stdout.write(f.read())
        else:
            for i, line in enumerate(f, 1):
                # Strip trailing newline for display, add our own
                sys.stdout.write(f"{i:6} | {line}")
            # Ensure trailing newline if file didn't end with one
            if line and not line.endswith("\n"):
                sys.stdout.write("\n")


if __name__ == "__main__":
    main()
