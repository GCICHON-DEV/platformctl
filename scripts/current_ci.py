"""Verify current GitHub Actions runs, never an unscoped status-name rollup."""
from urllib.parse import urlencode


def positive(value):
    return type(value) is int and value > 0


def select_run(runs, workflow, repo, number, sha, branch):
    if not runs:
        raise ValueError("missing pull_request workflow run: " + workflow["path"])
    seen = set()
    for run in runs:
        links = run.get("pull_requests") or []
        matches = [pr for pr in links if pr.get("number") == number
                   and pr.get("url") == f"https://api.github.com/repos/{repo}/pulls/{number}"
                   and pr.get("head", {}).get("sha") == sha
                   and pr.get("head", {}).get("ref") == branch
                   and pr.get("head", {}).get("repo", {}).get("url") == f"https://api.github.com/repos/{repo}"
                   and pr.get("base", {}).get("ref") == "main"
                   and pr.get("base", {}).get("repo", {}).get("url") == f"https://api.github.com/repos/{repo}"]
        if (run.get("workflow_id") != workflow["id"] or run.get("path") != workflow["path"]
                or run.get("event") != "pull_request" or run.get("head_sha") != sha
                or run.get("head_branch") != branch
                or run.get("repository", {}).get("full_name") != repo
                or run.get("head_repository", {}).get("full_name") != repo
                or len(matches) != 1
                or not all(positive(run.get(key)) for key in ("id", "workflow_id", "run_number", "run_attempt"))
                or run.get("url") != f"https://api.github.com/repos/{repo}/actions/runs/{run.get('id')}"):
            raise ValueError("workflow run provenance mismatch: " + workflow["path"])
        if run["id"] in seen:
            raise ValueError("duplicate workflow run record")
        seen.add(run["id"])
    newest = max(run["run_number"] for run in runs)
    latest = [run for run in runs if run["run_number"] == newest]
    if len(latest) != 1:
        raise ValueError("ambiguous latest workflow run")
    return latest[0]


def jobs_pass(jobs, run, required):
    found = {}
    for job in jobs:
        if (not all(positive(job.get(key)) for key in ("id", "run_id", "run_attempt"))
                or job.get("run_id") != run["id"]
                or job.get("run_attempt") != run["run_attempt"]
                or job.get("head_sha") != run["head_sha"]
                or job.get("head_branch") != run["head_branch"]
                or job.get("run_url") != run["url"]):
            raise ValueError("job provenance mismatch")
        name = job.get("name")
        if name in required:
            if name in found:
                raise ValueError("duplicate required job in current attempt: " + name)
            found[name] = job
    for name in required:
        job = found.get(name, {})
        if job.get("status") != "completed" or job.get("conclusion") != "success":
            raise ValueError("current required job is not completed/success: " + name)


def verify_current_ci(repo, number, sha, branch, required, api, collection):
    groups = {"process.yml": {"process-policy"}, "ci.yml": required - {"process-policy"}}
    snapshots = []
    for filename, names in groups.items():
        workflow = api(f"repos/{repo}/actions/workflows/{filename}")
        if (not positive(workflow.get("id")) or workflow.get("state") != "active"
                or workflow.get("path") != ".github/workflows/" + filename
                or workflow.get("url") != f"https://api.github.com/repos/{repo}/actions/workflows/{workflow.get('id')}"):
            raise ValueError("untrusted required workflow: " + filename)
        query = urlencode({"head_sha": sha, "branch": branch, "event": "pull_request", "per_page": 100})
        path = f"repos/{repo}/actions/workflows/{workflow['id']}/runs?{query}"
        runs = collection(path, "workflow_runs")
        # GitHub caps filtered run searches at 1000; never assume completeness.
        if len(runs) >= 1000:
            raise ValueError("workflow run search limit reached")
        run = select_run(runs, workflow, repo, number, sha, branch)
        current = api(f"repos/{repo}/actions/runs/{run['id']}")
        select_run([current], workflow, repo, number, sha, branch)
        if any(current.get(key) != run.get(key) for key in ("id", "workflow_id", "run_number", "run_attempt")):
            raise ValueError("workflow attempt changed during verification")
        if current.get("status") != "completed" or current.get("conclusion") != "success":
            raise ValueError("latest workflow run is not completed/success: " + filename)
        jobs = collection(f"repos/{repo}/actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs?per_page=100", "jobs")
        jobs_pass(jobs, current, names)
        snapshots.append((workflow, path, current))
    # A rerun/new run may appear while another workflow's jobs are fetched.
    for workflow, path, before in snapshots:
        latest = select_run(collection(path, "workflow_runs"), workflow, repo, number, sha, branch)
        if any(latest.get(key) != before.get(key) for key in
               ("id", "run_number", "run_attempt", "status", "conclusion")):
            raise ValueError("current CI changed during verification; retry")
