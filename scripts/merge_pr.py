#!/usr/bin/env python3
"""Fail-closed merge with owner-attested independent review of an exact SHA."""
import argparse
import json
import re
import subprocess
import sys
from process_policy import validate
from current_ci import verify_current_ci

REPOS = {
    "GCICHON-DEV/platformctl": {"process-policy", "quality", "template-contract"},
    "GCICHON-DEV/platformctl-templates": {"process-policy", "template-contract"},
}


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def api(path):
    return json.loads(run("gh", "api", path))


def review_record(comments, sha, owner):
    records = []
    for comment in comments:
        body = comment.get("body", "")
        if comment.get("user", {}).get("login") != owner or "<!-- platformctl-review:v1 -->" not in body:
            continue
        fields = dict(re.findall(r"^([A-Za-z ]+):[ \t]*(.*)$", body, re.M))
        if fields.get("Commit") == sha:
            records.append((comment.get("updated_at", ""), comment.get("id", 0), fields))
    if not records:
        raise ValueError("missing owner-attested independent review for this SHA")
    fields = max(records, key=lambda item: (item[0], item[1]))[2]
    implementer, reviewer = fields.get("Implementer", "").strip(), fields.get("Reviewer", "").strip()
    if not implementer or not reviewer or implementer == reviewer:
        raise ValueError("reviewer must be named and independent of the implementer")
    if fields.get("Verdict") != "APPROVED" or fields.get("Blocking findings") != "0":
        raise ValueError("latest review record does not approve this SHA")


def formal_reviews_pass(reviews):
    latest = {}
    for review in reviews:
        if review.get("state") in {"APPROVED", "CHANGES_REQUESTED", "DISMISSED"}:
            latest[review["user"]["login"]] = review["state"]
    if "CHANGES_REQUESTED" in latest.values():
        raise ValueError("a formal GitHub review requests changes")


def paged(path):
    pages = json.loads(run("gh", "api", "--paginate", "--slurp", path))
    return [item for page in pages for item in page]


def collection(path, field):
    pages = json.loads(run("gh", "api", "--paginate", "--slurp", path))
    items = [item for page in pages for item in page[field]]
    if not pages or any(page.get("total_count") != len(items) for page in pages):
        raise ValueError("incomplete or changing CI collection: " + field)
    return items


def check(repo, number, sha):
    if repo not in REPOS:
        raise ValueError("repository is outside the platformctl delivery scope")
    pr = api(f"repos/{repo}/pulls/{number}")
    if pr["state"] != "open" or pr["draft"] or pr["base"]["ref"] != "main":
        raise ValueError("PR must be open, ready, and target main")
    if pr["head"]["sha"] != sha or pr["head"]["repo"]["full_name"] != repo:
        raise ValueError("HEAD changed or PR comes from a fork")
    commits = paged(f"repos/{repo}/pulls/{number}/commits")
    errors = validate(pr, [item["commit"]["message"].splitlines()[0] for item in commits])
    if errors:
        raise ValueError("; ".join(errors))
    comparison = api(f"repos/{repo}/compare/main...{sha}")
    if comparison["behind_by"] != 0 or comparison["status"] not in {"ahead", "identical"}:
        raise ValueError("main advanced; synchronize, rerun CI, and review the new SHA")
    verify_current_ci(repo, number, sha, pr["head"]["ref"], REPOS[repo], api, collection)
    review_record(paged(f"repos/{repo}/issues/{number}/comments"), sha, repo.split("/")[0])
    formal_reviews_pass(paged(f"repos/{repo}/pulls/{number}/reviews"))
    if pr.get("mergeable") is not True:
        raise ValueError("GitHub mergeability is not confirmed yet")
    return pr


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True, choices=REPOS)
    parser.add_argument("--pr", type=int, required=True)
    parser.add_argument("--approved-head", required=True)
    parser.add_argument("--merge", action="store_true")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.approved_head):
        parser.error("--approved-head must be the full immutable SHA")
    try:
        check(args.repo, args.pr, args.approved_head)
        print("PASS: metadata, main, CI, independent review and exact HEAD")
        if args.merge:
            # Re-read immediately before merge; strict server checks protect a moving main.
            check(args.repo, args.pr, args.approved_head)
            subprocess.run(["gh", "pr", "merge", str(args.pr), "--repo", args.repo,
                            "--squash", "--match-head-commit", args.approved_head], check=True)
    except (ValueError, subprocess.CalledProcessError) as error:
        print("BLOCKED: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
