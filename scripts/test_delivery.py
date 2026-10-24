import unittest
from process_policy import validate
from merge_pr import formal_reviews_pass, review_record


class DeliveryTests(unittest.TestCase):
    def pr(self):
        return {"title": "ci(flow): enforce delivery gates", "head": {"ref": "c/plat-1-flow"},
                "body": "https://cichongabriel03.atlassian.net/browse/PLAT-1\n" +
                        "\n".join("## " + name for name in ("Zadanie", "Zmiana", "Weryfikacja", "Ryzyko i rollback"))}

    def record(self, **overrides):
        fields = {"Commit": "a" * 40, "Implementer": "/root", "Reviewer": "/root/reviewer",
                  "Verdict": "APPROVED", "Blocking findings": "0"}
        fields.update(overrides)
        return {"id": 1, "updated_at": "2026-10-15", "user": {"login": "GCICHON-DEV"},
                "body": "<!-- platformctl-review:v1 -->\n" + "\n".join(k + ": " + v for k, v in fields.items())}

    def test_metadata_accepts_valid_pr(self):
        self.assertEqual(validate(self.pr(), ["ci(flow): enforce delivery gates"]), [])

    def test_metadata_rejects_bad_branch_title_body_and_commits(self):
        pr = self.pr()
        pr.update(title="update", body="", head={"ref": "main"})
        self.assertGreaterEqual(len(validate(pr, ["WIP"])), 7)

    def test_exact_review(self):
        review_record([self.record()], "a" * 40, "GCICHON-DEV")
        for change in ({"Commit": "b" * 40}, {"Verdict": "CHANGES_REQUESTED"},
                       {"Blocking findings": "1"}, {"Reviewer": "/root"}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                review_record([self.record(**change)], "a" * 40, "GCICHON-DEV")

    def test_untrusted_comment_cannot_approve(self):
        record = self.record()
        record["user"]["login"] = "external"
        with self.assertRaises(ValueError):
            review_record([record], "a" * 40, "GCICHON-DEV")

    def test_newer_rejection_overrides_approval(self):
        rejected = self.record(Verdict="CHANGES_REQUESTED")
        rejected.update(id=2, updated_at="2026-10-16")
        with self.assertRaises(ValueError):
            review_record([self.record(), rejected], "a" * 40, "GCICHON-DEV")

    def test_formal_changes_requested_blocks_until_resolved(self):
        changes = {"user": {"login": "reviewer"}, "state": "CHANGES_REQUESTED"}
        approved = {"user": {"login": "reviewer"}, "state": "APPROVED"}
        with self.assertRaises(ValueError):
            formal_reviews_pass([changes])
        formal_reviews_pass([changes, approved])


if __name__ == "__main__":
    unittest.main()
