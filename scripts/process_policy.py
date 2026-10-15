#!/usr/bin/env python3
"""Validate PR metadata; used by CI and the exact-SHA merge gate."""
import json
import os
import re
import subprocess
import sys

CONVENTIONAL = re.compile(r"^(feat|fix|chore|refactor|docs|test|perf|build|ci|revert)(\([a-z0-9-]+\))?!?: .{3,}$")


def validate(pr, subjects):
    errors = []
    if not CONVENTIONAL.fullmatch(pr.get("title", "")) or len(pr.get("title", "")) > 100:
        errors.append("PR title must use Conventional Commits (max 100 characters)")
    branch = pr.get("head", {}).get("ref", "")
    if not re.fullmatch(r"c/[a-z0-9][a-z0-9/-]+", branch):
        errors.append("branch must use c/<task>-<description>")
    body = pr.get("body") or ""
    if not re.search(r"https://cichongabriel03\.atlassian\.net/browse/PLAT-[1-9][0-9]*", body) and not re.search(r"\bFLOW\b", body):
        errors.append("PR body needs a PLAT Jira URL or explicit FLOW scope")
    for heading in ("Zadanie", "Zmiana", "Weryfikacja", "Ryzyko i rollback"):
        if not re.search(r"^## " + re.escape(heading) + r"\s*$", body, re.M):
            errors.append("missing PR section: " + heading)
    if not subjects:
        errors.append("no commits found")
    for subject in subjects:
        if not CONVENTIONAL.fullmatch(subject) or len(subject) > 100:
            errors.append("invalid commit subject: " + subject)
    return errors


def main():
    with open(os.environ["GITHUB_EVENT_PATH"], encoding="utf-8") as stream:
        event = json.load(stream)
    pr = event["pull_request"]
    # Use the event's immutable base, not a moving branch, to select PR commits.
    subjects = subprocess.check_output(
        ["git", "log", "--format=%s", pr["base"]["sha"] + ".." + pr["head"]["sha"]],
        text=True,
    ).splitlines()
    errors = validate(pr, subjects)
    for error in errors:
        print(error, file=sys.stderr)
    return bool(errors)


if __name__ == "__main__":
    sys.exit(main())
