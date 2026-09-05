#!/usr/bin/env python3
"""How full is the account's window, before the factory spends against it.

The factory runs on a subscription whose five-hour window is shared with the
human on the same machine and invisible to the meter until a run ends. This
asks the harness the cheapest question it answers — one session, one
one-word prompt — and prints the rate-limit report it forwards on the first
usage update: status, which window, utilization, and when it resets.

Costs a few hundred tokens. Run it before starting a round, and again before
resuming claims after a stop.

  ./hack/dogfood/window.py            # human-readable
  ./hack/dogfood/window.py --json     # the report as the adapter sent it
"""
import json
import os
import queue
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone

ADAPTER = os.environ.get("BOX_ACP", "claude-agent-acp")
CWD = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def main() -> int:
    p = subprocess.Popen([ADAPTER], cwd=CWD, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=subprocess.DEVNULL, text=True, bufsize=1)
    inbox: "queue.Queue[dict]" = queue.Queue()

    def reader():
        for line in p.stdout:
            line = line.strip()
            if line:
                try:
                    inbox.put(json.loads(line))
                except json.JSONDecodeError:
                    pass
    threading.Thread(target=reader, daemon=True).start()

    nid = 0

    def call(method, params):
        nonlocal nid
        nid += 1
        p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": nid, "method": method, "params": params}) + "\n")
        p.stdin.flush()
        return nid

    def reply(rid, timeout):
        end = time.time() + timeout
        while time.time() < end:
            try:
                m = inbox.get(timeout=0.5)
            except queue.Empty:
                continue
            if m.get("id") == rid and "method" not in m:
                return m
            notes.append(m)
        return None

    notes: list = []
    call("initialize", {"protocolVersion": 1, "clientCapabilities": {"fs": {"readTextFile": False, "writeTextFile": False}}})
    reply(1, 30)
    rid = call("session/new", {"cwd": CWD, "mcpServers": [], "_meta": {"claudeCode": {"options": {
        "settingSources": ["user"], "strictMcpConfig": True, "mcpServers": {}}}}})
    res = reply(rid, 60)
    if not res or "error" in res:
        print("session/new failed:", res, file=sys.stderr)
        return 2
    sid = res["result"]["sessionId"]
    # The cheapest model the adapter lists, so the probe itself is not the spend.
    for o in res["result"].get("configOptions", []):
        if o["id"] == "model":
            cheap = next((v["value"] for v in o.get("options", []) if v["value"] == "haiku"), None)
            if cheap:
                call("session/set_config_option", {"sessionId": sid, "configId": "model", "value": cheap})
                reply(nid, 30)
    rid = call("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": "Reply with the single word OK."}]})
    reply(rid, 120)
    limit = None
    for m in notes:
        if m.get("method") == "session/update":
            meta = (m["params"].get("update") or {}).get("_meta") or {}
            if "_claude/rateLimit" in meta:
                limit = meta["_claude/rateLimit"]
    p.terminate()
    if limit is None:
        print("the harness sent no rate-limit report on this session (an API key, or a dialect change)")
        return 1
    if "--json" in sys.argv:
        print(json.dumps(limit))
        return 0
    resets = limit.get("resetsAt")
    when = datetime.fromtimestamp(resets, tz=timezone.utc).astimezone().strftime("%H:%M %Z") if resets else "?"
    util = limit.get("utilization")
    pct = f"{util * 100:.0f}%" if isinstance(util, (int, float)) else "?"
    print(f"{limit.get('status', '?')}: {limit.get('rateLimitType', '?')} window at {pct}, resets {when}")
    return 0 if limit.get("status") == "allowed" else 3


if __name__ == "__main__":
    raise SystemExit(main())
