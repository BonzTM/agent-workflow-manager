#!/usr/bin/env python3

import argparse
import json
import os
import subprocess
import sys


GO_PREFIXES = ("cmd/", "internal/")


def trimmed(value):
    return value.strip() if isinstance(value, str) else ""


def parse_paths(raw):
    if not trimmed(raw):
        return []
    try:
        decoded = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid JSON for files_changed: {exc}") from exc
    if not isinstance(decoded, list):
        raise ValueError("files_changed must decode to a JSON array")

    paths = []
    seen = set()
    for item in decoded:
        value = trimmed(item)
        if not value:
            continue
        normalized = value.replace("\\", "/")
        if normalized in seen:
            continue
        seen.add(normalized)
        paths.append(normalized)
    return paths


def discover_packages(paths):
    packages = []
    seen = set()
    for path in paths:
        if not path.endswith(".go"):
            continue
        if not path.startswith(GO_PREFIXES):
            continue
        directory = os.path.dirname(path)
        if not directory or directory == ".":
            continue
        package = "./" + directory
        if package in seen:
            continue
        seen.add(package)
        packages.append(package)
    return sorted(packages)


def parse_args():
    parser = argparse.ArgumentParser(
        description="Run go test for packages touched by AWM verify file selection."
    )
    parser.add_argument(
        "--files-changed-json",
        default=os.environ.get("AWM_VERIFY_FILES_CHANGED_JSON", "[]"),
        help="JSON array of repo-relative changed paths (defaults to AWM_VERIFY_FILES_CHANGED_JSON).",
    )
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        changed_paths = parse_paths(args.files_changed_json)
    except ValueError as exc:
        print(f"awm-go-test-targets: {exc}", file=sys.stderr)
        return 2

    packages = discover_packages(changed_paths)
    if not packages:
        print("awm-go-test-targets: skip - no changed Go packages matched")
        return 0

    command = ["go", "test", "-count=1", *packages]
    print(f"awm-go-test-targets: running {' '.join(command)}")
    return subprocess.run(command).returncode


if __name__ == "__main__":
    sys.exit(main())
