#!/usr/bin/env python3
"""Put the repository's own open issues into the factory as work.

One ingress per issue, keyed `issue-<n>` so re-running this is idempotent —
intake turns an ingress into a work item exactly once, and a second submit
of the same key reports opened=false rather than queueing a duplicate.

Issues are read from ../project-box-state/issues.json, fetched with `gh`
when that file is absent. Pass issue numbers to submit only those. The cache
is a convenience, never a source of truth: if a requested number is missing
from a pre-existing cache, it is refetched once and re-checked before this
script gives up on it.

CONTRACT below is the instruction every agent run in this repository has
executed under since round 1. It rides the task text rather than the agent's
layer stack, so it is neither part of the agentref nor governed as a layer —
which is why it belongs on the record here rather than in a local file. See
ARCHITECTURE "Text first, git for governance".
"""

import json
import pathlib
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent.parent
# Beside the repository, not inside it (issue #73): a path under the
# checkout carries the operator's tree as a prefix, and a builder can infer
# and write to that tree from its own cwd.
STATE = ROOT.parent / "project-box-state"
BOX = ROOT / "bin" / "box"
SOCKET = "unix:" + str(STATE / "boxd.sock")
REPO = "ChristopherDavenport/project-box"
# Must match hack/dogfood/box.yaml: the tenant scopes every key, and the
# route is the vocabulary the daemon's config defines.
TENANT = "davenport"
ROUTE = "issues"

# The repo-specific half of the working contract. The general half — commit
# or escalate, hand off rather than degrade — is the runtime's, in the prompt.
#
# This used to open with "Read docs/ARCHITECTURE.md and docs/PLAN.md before
# changing anything". Those are 86KB each; loaded at turn one they sat in
# every later turn's context, and on round 4's 120-call run they were about a
# fifth of the whole bill. CLAUDE.md is the brief now — the SDK loads it with
# project scope — and it names the sections to read instead of the files.
CONTRACT = """\
You are working in a checkout of project-box itself. CLAUDE.md is the brief.
The recorded decisions are docs/PLAN.md section 6 and their reasoning is in
docs/ARCHITECTURE.md by heading: read the section your change touches, never
the file. A change that contradicts a recorded decision without saying so is
wrong even when it compiles.

Make the change precisely and minimally, with the tests that assert it — a
property that is not tested is a comment. Commit your work. If the issue is a
question, a decision, or is already satisfied by the code as it stands, do not
invent a change: escalate and say why.
"""


def fetch_issues() -> list:
    """The one gh call this script makes — a seam so tests can avoid gh."""
    out = subprocess.run(
        ["gh", "issue", "list", "--repo", REPO, "--state", "open",
         "--limit", "200", "--json", "number,title,body"],
        capture_output=True, text=True, check=True,
    ).stdout
    return json.loads(out)


def _write_cache(issues: list) -> None:
    STATE.mkdir(parents=True, exist_ok=True)
    (STATE / "issues.json").write_text(json.dumps(issues))


def load_issues() -> tuple:
    """Returns (issues, from_cache). from_cache is True when these came from
    a pre-existing issues.json rather than a fetch made this run — the
    caller uses that to decide whether a refetch-on-miss is still owed."""
    cache = STATE / "issues.json"
    if cache.exists():
        return json.loads(cache.read_text()), True
    issues = fetch_issues()
    _write_cache(issues)
    return issues, False


def refresh_cache() -> list:
    """A cache is a convenience, never a source of truth: when a requested
    issue isn't in it, re-fetch once from gh and overwrite the cache file."""
    issues = fetch_issues()
    _write_cache(issues)
    return issues


def _human_age(seconds: float) -> str:
    minutes = max(0.0, seconds) / 60
    if minutes < 60:
        return f"{max(1, int(minutes))}m"
    hours = minutes / 60
    if hours < 48:
        return f"{int(hours)}h"
    return f"{int(hours / 24)}d"


def submit_one(n: int, title: str, body: str, suffix: str) -> tuple:
    """Runs `box submit` for one issue — a seam so tests can avoid box/the
    daemon. Returns (returncode, status line)."""
    task = f"GitHub issue #{n}: {title}\n\n{body.strip()}\n\n---\n\n{CONTRACT}"
    res = subprocess.run(
        [str(BOX), "-tenant", TENANT, "-daemon", SOCKET, "submit",
         "-route", ROUTE, "-key", f"issue-{n}{suffix}", "-task", task,
         "-summary", title],
        capture_output=True, text=True,
    )
    status = res.stdout.strip() or res.stderr.strip()
    return res.returncode, status


def main() -> int:
    issues, from_cache = load_issues()
    if from_cache:
        age = time.time() - (STATE / "issues.json").stat().st_mtime
        print(f"cache issues.json is {_human_age(age)} old", file=sys.stderr)

    args = sys.argv[1:]
    # An issue the factory has already been through keeps its key, so a
    # second submit of it reports opened=false: intake is idempotent on the
    # key, by design. When the issue is still open because only half of it
    # landed, --key-suffix names the second pass — `issue-22-lander` — and
    # the record shows two originations for one issue, which is the truth.
    suffix = ""
    if "--key-suffix" in args:
        i = args.index("--key-suffix")
        suffix = "-" + args[i + 1]
        del args[i:i + 2]
    only = {int(a) for a in args if a.isdigit()}

    missing_status = 0
    if only:
        missing = only - {i["number"] for i in issues}
        # A cache is a convenience, never a source of truth: a number it
        # hasn't heard of gets one refetch before this script gives up on
        # it. A fresh fetch this run has already answered that question.
        if missing and from_cache:
            issues = refresh_cache()
            missing = only - {i["number"] for i in issues}
        for n in sorted(missing):
            print(f"#{n} not among gh's open issues for {REPO}", file=sys.stderr)
        if missing:
            missing_status = 1
        issues = [i for i in issues if i["number"] in only]

    for issue in sorted(issues, key=lambda i: i["number"]):
        n, title = issue["number"], issue["title"]
        rc, status = submit_one(n, title, issue["body"], suffix)
        print(f"#{n:<3} {title[:48]:<50} {status}")
        if rc != 0:
            return rc
    return missing_status


if __name__ == "__main__":
    raise SystemExit(main())
