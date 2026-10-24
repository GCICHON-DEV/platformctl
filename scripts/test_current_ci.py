import copy
import unittest
from unittest.mock import patch

from current_ci import jobs_pass, select_run, verify_current_ci
from merge_pr import collection


REPO = "GCICHON-DEV/platformctl"
SHA = "a" * 40
BRANCH = "c/plat-51-current-ci"
ROOT = "https://api.github.com/repos/" + REPO


def workflow(filename="process.yml", identity=1):
    return {"id": identity, "path": ".github/workflows/" + filename,
            "state": "active", "url": ROOT + f"/actions/workflows/{identity}"}


def run(identity=10, sequence=1, attempt=1, wf=None):
    wf = wf or workflow()
    return {"id": identity, "workflow_id": wf["id"], "path": wf["path"],
            "run_number": sequence, "run_attempt": attempt,
            "head_sha": SHA, "head_branch": BRANCH, "event": "pull_request",
            "repository": {"full_name": REPO}, "head_repository": {"full_name": REPO},
            "status": "completed", "conclusion": "success",
            "url": ROOT + f"/actions/runs/{identity}",
            "pull_requests": [{"number": 12, "url": ROOT + "/pulls/12",
                               "head": {"sha": SHA, "ref": BRANCH, "repo": {"url": ROOT}},
                               "base": {"ref": "main", "repo": {"url": ROOT}}}]}


def job(current=None, name="process-policy", identity=100):
    current = current or run()
    return {"id": identity, "name": name, "run_id": current["id"],
            "run_attempt": current["run_attempt"], "head_sha": SHA, "head_branch": BRANCH,
            "run_url": current["url"], "status": "completed", "conclusion": "success"}


class CurrentCITests(unittest.TestCase):
    def select(self, runs):
        return select_run(runs, workflow(), REPO, 12, SHA, BRANCH)

    def test_latest_run_uses_sequence_not_success_or_updated_old_rerun(self):
        old, new = run(attempt=8), run(11, 2)
        old["updated_at"] = "2099-01-01"
        self.assertEqual(self.select([new, old]), new)
        for state in ("failure", None):
            new["conclusion"] = state
            # Selection cannot fall back to the old success.
            self.assertEqual(self.select([old, new]), new)

    def test_missing_duplicate_and_ambiguous_runs_fail_closed(self):
        for runs in ([], [run(), run()], [run(), run(11)]):
            with self.subTest(runs=runs), self.assertRaises(ValueError):
                self.select(runs)

    def test_run_provenance(self):
        mutations = [
            {"path": ".github/workflows/fake.yml"}, {"workflow_id": 99},
            {"repository": {"full_name": "attacker/repo"}},
            {"head_repository": {"full_name": "attacker/repo"}},
            {"head_sha": "b" * 40}, {"head_branch": "other"},
            {"event": "workflow_dispatch"}, {"event": "pull_request_target"},
            {"run_attempt": 0}, {"run_number": True}, {"id": -1},
            {"url": "https://attacker.invalid/run"}, {"pull_requests": []},
        ]
        for mutation in mutations:
            candidate = run()
            candidate.update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.select([candidate])
        for section, field, value in (("head", "sha", "b" * 40), ("head", "ref", "other"),
                                      ("base", "ref", "develop")):
            candidate = run()
            candidate["pull_requests"][0][section][field] = value
            with self.subTest(section=section, field=field), self.assertRaises(ValueError):
                self.select([candidate])
        for mutation in ({"number": 13}, {"url": ROOT + "/pulls/13"}):
            candidate = run()
            candidate["pull_requests"][0].update(mutation)
            with self.assertRaises(ValueError):
                self.select([candidate])
        for section in ("head", "base"):
            candidate = run()
            candidate["pull_requests"][0][section]["repo"]["url"] = "https://attacker.invalid/repo"
            with self.assertRaises(ValueError):
                self.select([candidate])

    def test_current_jobs_reject_pending_failed_skipped_missing_duplicate_and_spoof(self):
        current = run(attempt=2)
        jobs_pass([job(current)], current, {"process-policy"})
        for mutation in ({"status": "queued"}, {"conclusion": "failure"}, {"conclusion": "skipped"},
                         {"name": "fake"}, {"run_id": 99}, {"head_sha": "b" * 40},
                         {"run_attempt": 1}, {"head_branch": "other"}, {"run_url": "fake"}, {"id": 0}):
            candidate = job(current)
            candidate.update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                jobs_pass([candidate], current, {"process-policy"})
        for jobs in ([], [job(current), job(current, identity=101)]):
            with self.assertRaises(ValueError):
                jobs_pass(jobs, current, {"process-policy"})

    def fixture(self, mutation=None):
        workflows = {"process.yml": workflow(), "ci.yml": workflow("ci.yml", 2)}
        runs = {key: run(wf=value, identity=value["id"] * 10) for key, value in workflows.items()}
        if mutation:
            mutation(workflows, runs)

        def api(path):
            if "/workflows/" in path:
                return copy.deepcopy(workflows[path.rsplit("/", 1)[1]])
            return copy.deepcopy(next(value for value in runs.values() if str(value["id"]) == path.rsplit("/", 1)[1]))

        def listing(path, field):
            if field == "workflow_runs":
                identifier = int(path.split("/workflows/")[1].split("/")[0])
                return [copy.deepcopy(next(value for value in runs.values() if value["workflow_id"] == identifier))]
            current = next(value for value in runs.values() if str(value["id"]) == path.split("/runs/")[1].split("/")[0])
            self.assertIn(f"/attempts/{current['run_attempt']}/jobs?", path)
            names = ["process-policy"] if current["workflow_id"] == 1 else ["quality", "template-contract"]
            return [job(current, name, identity=index + 100) for index, name in enumerate(names)]

        return api, listing

    def verify(self, api, listing):
        verify_current_ci(REPO, 12, SHA, BRANCH, {"process-policy", "quality", "template-contract"}, api, listing)

    def test_end_to_end_current_attempt_and_duplicate_historical_success(self):
        api, listing = self.fixture(lambda w, r: r["process.yml"].update(run_attempt=2))

        def history(path, field):
            values = listing(path, field)
            if field == "workflow_runs":
                old = copy.deepcopy(values[0])
                old["id"] -= 1
                values[0]["run_number"] = 2
                old["run_number"] = 1
                old["url"] = ROOT + f"/actions/runs/{old['id']}"
                return values + [old]
            return values

        # A fixture with two legal historical successes; latest is still exact.
        def with_sequence(path):
            value = api(path)
            if "/runs/" in path:
                value["run_number"] = 2
            return value
        self.verify(with_sequence, history)

    def test_latest_failure_pending_and_workflow_spoof_block(self):
        for status, conclusion in (("completed", "failure"), ("queued", None), ("in_progress", None)):
            api, listing = self.fixture(lambda w, r: r["process.yml"].update(status=status, conclusion=conclusion))
            with self.subTest(status=status), self.assertRaises(ValueError):
                self.verify(api, listing)
        for field, value in (("path", "fake.yml"), ("id", 0), ("state", "disabled_manually"), ("url", "fake")):
            api, listing = self.fixture(lambda w, r: w["process.yml"].update({field: value}))
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.verify(api, listing)

    def test_attempt_or_new_run_race_blocks(self):
        for change in ("attempt", "new-run"):
            api, listing = self.fixture()
            calls = {}

            def changing(path, field):
                values = listing(path, field)
                calls[path] = calls.get(path, 0) + 1
                if field == "workflow_runs" and calls[path] > 1:
                    if change == "attempt":
                        values[0]["run_attempt"] += 1
                    else:
                        values[0].update(id=99, run_number=2, url=ROOT + "/actions/runs/99")
                return values

            with self.subTest(change=change), self.assertRaises(ValueError):
                self.verify(api, changing)

    def test_collection_pagination_requires_complete_stable_total(self):
        import json
        for pages, expected in (([{"total_count": 2, "jobs": [1]}, {"total_count": 2, "jobs": [2]}], [1, 2]),
                                ([{"total_count": 2, "jobs": [1]}], None),
                                ([{"total_count": 1, "jobs": [1]}, {"total_count": 2, "jobs": [2]}], None)):
            with patch("merge_pr.run", return_value=json.dumps(pages)):
                if expected is None:
                    with self.assertRaises(ValueError):
                        collection("path", "jobs")
                else:
                    self.assertEqual(collection("path", "jobs"), expected)


if __name__ == "__main__":
    unittest.main()
