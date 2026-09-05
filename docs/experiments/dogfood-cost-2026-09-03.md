# Dogfood round 4: what one run cost, and why — 2026-09-03

Fresh store, three open issues submitted, `claude-agent-acp` 0.70.0 on the
machine's subscription. Read back from the evidence chain on 2026-09-04.

| unit | outcome | wall | tool calls | tokens | cache reads | context/call | cost |
|---|---|---|---|---|---|---|---|
| #15 | escalated (depends on #13) | 69s | 7 | 424,763 | 339,953 | ~49K | $2.17 |
| #16 | built, landed `04d1855` → PR #37 | 28.4 min | 120 | 25,097,530 | 24,712,662 | ~206K | $35.20 |
| #22 | parked: "You've hit your session limit · resets 10:20pm" | 4.7 min | — | unmetered | — | — | $3.94 |

The #16 run alone consumed the account's five-hour window. Round 3 had the
same shape (25.4M). The factory's own meter refused nothing: its budget is
tokens per hour of its own, and the window that bound is the account's,
shared with the human on the same machine.

## Causes, all in the factory

1. **The model was nobody's decision.** The adapter was never told a model,
   so the builder inherited `~/.claude/settings.json`: `fable[1m]`. A
   session/new probe on 2026-09-04 listed what the adapter offers — default,
   opus[1m], claude-fable-5-1[1m], fable[1m], sonnet, haiku — with effort
   inherited as `xhigh`. The agentref did not record the model, so the chain
   could not explain the cost after the fact.
2. **The contract front-loaded 43K tokens.** `hack/dogfood/submit.py` told
   every run to read `docs/ARCHITECTURE.md` and `docs/PLAN.md` before
   changing anything: 172KB, in context for all 120 turns — roughly a fifth
   of the cache reads.
3. **The task was in the prompt twice.** The composer ends the instructions
   with the task layer; the builder appended the task again under `## Task`.
4. **The limit signal was thrown away.** The adapter forwards the SDK's
   `rate_limit_event` on every usage update (status, window, utilization,
   reset time); the runtime parsed only tokens and cost from that metadata.
5. **Skills: delivered, not used.** The probe showed `box-skills:go` and
   `box-skills:sqlite` among 66 commands offered to the session. The #16
   run's tool tally was Bash 51, Edit 36, Read 29, Write 4 — no Skill
   invocation. Delivery and use were indistinguishable on the record until
   the `agent-session` record existed.

## What changed (branch `session-surface`)

- ACP's session surface modelled as the protocol defines it: config options
  by id with categories, modes, available commands, `usage_update`'s
  `used`/`size`, the full `SessionUpdate` union.
- `model:` and `session:` on the box, applied through
  `session/set_config_option`, asserted back, refused otherwise.
- `agent-session` record: what the agent answered. `agent-result` carries
  model, peak context, window, and the account's rate-limit report.
- Dogfood box: `model: sonnet`. CLAUDE.md replaces the read-both-docs
  contract. The task appears once.

## Round 5 measures

Same issues, Sonnet, the brief instead of the docs. Compare tokens, context
per call, cost, and whether the change lands. Then: a brake denominated in
the account's window, and whose credential the factory runs on.
