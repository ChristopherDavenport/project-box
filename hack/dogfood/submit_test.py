#!/usr/bin/env python3
"""Tests for submit.py's cache-refresh behaviour (issue #132).

Stdlib only, no gh and no daemon: fetch_issues() and submit_one() are the
seams these tests stub out. Run directly:

    python3 hack/dogfood/submit_test.py

check.sh deliberately does not run this — it's the dogfood gate's own
definition, untouched by this change — so this exists to be run by whoever
next edits submit.py.
"""

import importlib.util
import io
import json
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from unittest import mock

HERE = pathlib.Path(__file__).resolve().parent

_spec = importlib.util.spec_from_file_location("submit", HERE / "submit.py")
submit = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(submit)


ISSUE_1 = {"number": 1, "title": "First issue", "body": "body one"}
ISSUE_2 = {"number": 2, "title": "Second issue", "body": "body two"}
ISSUE_125 = {"number": 125, "title": "Newer issue", "body": "body 125"}


class SubmitCacheRefreshTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.state = pathlib.Path(self._tmp.name)
        self._patches = [
            mock.patch.object(submit, "STATE", self.state),
            mock.patch.object(submit, "BOX", pathlib.Path("/bin/true")),
        ]
        for p in self._patches:
            p.start()
            self.addCleanup(p.stop)

    def write_cache(self, issues):
        self.state.mkdir(parents=True, exist_ok=True)
        (self.state / "issues.json").write_text(json.dumps(issues))

    def run_main(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(sys, "argv", ["submit.py"] + argv):
            with redirect_stdout(out), redirect_stderr(err):
                rc = submit.main()
        return rc, out.getvalue(), err.getvalue()

    def test_missing_from_stale_cache_triggers_one_refresh_and_submits(self):
        self.write_cache([ISSUE_1])
        with mock.patch.object(submit, "fetch_issues", return_value=[ISSUE_1, ISSUE_125]) as fetch, \
             mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")) as sub:
            rc, out, err = self.run_main(["125"])

        fetch.assert_called_once()
        sub.assert_called_once_with(125, "Newer issue", "body 125", "")
        self.assertEqual(rc, 0)
        self.assertIn("#125", out)
        self.assertEqual(
            json.loads((self.state / "issues.json").read_text()),
            [ISSUE_1, ISSUE_125],
        )

    def test_still_missing_after_refresh_reports_and_submits_the_rest(self):
        self.write_cache([ISSUE_1])
        with mock.patch.object(submit, "fetch_issues", return_value=[ISSUE_1]) as fetch, \
             mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")) as sub:
            rc, out, err = self.run_main(["1", "999"])

        fetch.assert_called_once()
        sub.assert_called_once_with(1, "First issue", "body one", "")
        self.assertNotEqual(rc, 0)
        self.assertIn("#999", err)
        self.assertIn("not among gh's open issues for", err)
        self.assertIn(submit.REPO, err)
        self.assertIn("#1", out)

    def test_fresh_fetch_does_not_fetch_twice(self):
        with mock.patch.object(submit, "fetch_issues", return_value=[ISSUE_1]) as fetch, \
             mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")):
            rc, out, err = self.run_main(["999"])

        fetch.assert_called_once()
        self.assertNotEqual(rc, 0)
        self.assertIn("#999", err)

    def test_box_submit_failure_returncode_passes_through_unchanged(self):
        self.write_cache([ISSUE_1, ISSUE_2])
        with mock.patch.object(submit, "submit_one", return_value=(7, "boom")):
            rc, out, err = self.run_main([])

        self.assertEqual(rc, 7)

    def test_task_summary_and_key_are_byte_identical_to_today(self):
        self.write_cache([ISSUE_1])
        captured = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            return mock.Mock(returncode=0, stdout="opened=true", stderr="")

        with mock.patch.object(submit.subprocess, "run", side_effect=fake_run):
            rc, out, err = self.run_main([])

        cmd = captured["cmd"]
        key = cmd[cmd.index("-key") + 1]
        task = cmd[cmd.index("-task") + 1]
        summary = cmd[cmd.index("-summary") + 1]

        self.assertEqual(key, "issue-1")
        self.assertEqual(summary, "First issue")
        expected_task = (
            f"GitHub issue #1: First issue\n\nbody one\n\n---\n\n{submit.CONTRACT}"
        )
        self.assertEqual(task, expected_task)
        self.assertEqual(rc, 0)

    def test_key_suffix_still_applied_when_reporting_missing(self):
        self.write_cache([ISSUE_1])
        with mock.patch.object(submit, "fetch_issues", return_value=[ISSUE_1]), \
             mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")) as sub:
            rc, out, err = self.run_main(["1", "999", "--key-suffix", "lander"])

        sub.assert_called_once_with(1, "First issue", "body one", "-lander")
        self.assertNotEqual(rc, 0)

    def test_no_args_submits_everything_without_missing_checks(self):
        self.write_cache([ISSUE_1, ISSUE_2])
        with mock.patch.object(submit, "fetch_issues") as fetch, \
             mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")) as sub:
            rc, out, err = self.run_main([])

        fetch.assert_not_called()
        self.assertEqual(sub.call_count, 2)
        self.assertEqual(rc, 0)

    def test_cache_age_is_reported_on_stderr_when_cache_preexists(self):
        self.write_cache([ISSUE_1])
        with mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")):
            rc, out, err = self.run_main([])

        self.assertIn("cache issues.json is", err)
        self.assertIn("old", err)

    def test_no_cache_age_reported_on_fresh_fetch(self):
        with mock.patch.object(submit, "fetch_issues", return_value=[ISSUE_1]), \
             mock.patch.object(submit, "submit_one", return_value=(0, "opened=true")):
            rc, out, err = self.run_main([])

        self.assertNotIn("cache issues.json is", err)


if __name__ == "__main__":
    unittest.main()
