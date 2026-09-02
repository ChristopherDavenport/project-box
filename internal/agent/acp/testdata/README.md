# The pinned ACP dialect

`claude-agent-acp-0.70.jsonl` is one whole session's frames in wire order, both
directions, one JSON object per line: `{"from":"client"|"agent","frame":…}`.
`conformance_test.go` replays it at the runtime and asserts what the parser
made of each shape. It is the same discipline the agent plane already applies
to skills — a skill resolves to a digest, never a branch — applied to the one
protocol that made per-provider adapters unnecessary and still has versions.

A shape that moves now fails a test. Last time one moved, the tool-call counter
read 0 for eight experiment runs and the only thing that noticed was a human
reading a column of zeros (`docs/experiments/continuity-2026-08-31.md`,
finding 5a).

## Where it came from

**Assembled, not captured.** Every shape in it is one this repository has
already paid to learn, and the price is recorded next to each:

- the five `usage` field names are claude-agent-acp 0.70's `sessionUsage()`,
  read from its source after the two cache counters were spelled with the
  Anthropic API's names instead and silently read zero (`wireUsage` in
  `acp.go`);
- `tool_call_update` carrying no title, and naming its tool only under
  `_meta.claudeCode.toolName`, is what 232 tool calls across four dogfood runs
  showed — 91 of them arrived with nothing else to match on;
- a `title` that is prose rather than a tool name (here the whole `cd … && go
  build` command line) is the same measurement: one run of 53 calls tallied 46
  "distinct tools";
- `usage_update` carrying `cost` is the only place the agent says what a turn
  cost in money;
- there is no `_box/escalate` frame because 0.70 cannot call unknown methods.
  That is why the adapter keeps an `ESCALATE:` text fallback
  (`continuity-2026-08-31`, finding 2). `_box/handoff` is absent for the same
  reason and keeps a `HANDOFF-NOW:` fallback for it; both verbs are exercised
  against the scripted fake, which is where an unobserved shape belongs.

**The thin frame is `session/request_permission`.** The fake-agent suite has
always modelled its `toolCall.title` as a bare tool name, and no run recorded
one to check. If a live recording shows prose there — as `tool_call` titles
turned out to — then the permission path has the hole the update path had, and
the enforcement it carries is matching on the wrong string. This pin is where
that will show.

## Refreshing it from a real agent

`Runtime.Transcript` records; `Builder.Transcript` carries it to the only path
a real agent runs through. The live smoke wires both:

```sh
BOX_ACP_LIVE=/path/to/claude-agent-acp \
BOX_ACP_TRANSCRIPT="$PWD/internal/agent/acp/testdata/live.jsonl" \
  go test ./internal/agent/acp -run TestLiveClaudeAgentBuilds
```

Then diff `live.jsonl` against this file. The diff is the dialect change; the
conformance test, re-run with the new file in place, is what says whether the
runtime still parses it. A recording is run material — it carries the prompt
and the agent's output — so review one before committing it.
