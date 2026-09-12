#!/usr/bin/env python3
"""Put the repository's own open issues into the factory as work.

One ingress per issue, keyed `issue-<n>` so re-running this is idempotent —
intake turns an ingress into a work item exactly once, and a second submit
of the same key reports opened=false rather than queueing a duplicate.

Issues are read from ../project-box-state/issues.json, fetched with `gh`
when that file is absent. Pass issue numbers to submit only those.

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


def load_issues() -> list:
    cache = STATE / "issues.json"
    if not cache.exists():
        STATE.mkdir(parents=True, exist_ok=True)
        out = subprocess.run(
            ["gh", "issue", "list", "--repo", REPO, "--state", "open",
             "--limit", "200", "--json", "number,title,body"],
            capture_output=True, text=True, check=True,
        ).stdout
        cache.write_text(out)
    return json.loads(cache.read_text())


def main() -> int:
    issues = load_issues()
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
    if only:
        issues = [i for i in issues if i["number"] in only]

    for issue in sorted(issues, key=lambda i: i["number"]):
        n, title = issue["number"], issue["title"]
        task = f"GitHub issue #{n}: {title}\n\n{issue['body'].strip()}\n\n---\n\n{CONTRACT}"
        res = subprocess.run(
            [str(BOX), "-tenant", TENANT, "-daemon", SOCKET, "submit",
             "-route", ROUTE, "-key", f"issue-{n}{suffix}", "-task", task,
             "-summary", title],
            capture_output=True, text=True,
        )
        status = res.stdout.strip() or res.stderr.strip()
        print(f"#{n:<3} {title[:48]:<50} {status}")
        if res.returncode != 0:
            return res.returncode
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
