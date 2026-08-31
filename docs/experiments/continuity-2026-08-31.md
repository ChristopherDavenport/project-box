# Seat continuity experiment — 2026-08-31

Real claude-agent-acp sessions through the full factory loop. Design pre-registered
in hack/continuity/main.go: run 1 carries facts only in its handoff; run 2 needs them;
treatment wakes with the seat's notes, control is the identical agent without a seat.

| arm | chain | trial | outcome | detail | r1 tools/ms | r2 tools/ms |
|---|---|---|---|---|---|---|
| treatment | pricing | 1 | **CORRECT** | facts used, conventions held | 0/24820 | 0/72021 |
| control | pricing | 1 | **ESCALATED** | escalated by agent: The task requires an exported constant Region holding "the canonical tax region code", but nothing in the repo, git history, or task context specifies what that code is (or its format — e.g. "WI" vs "US-WI" vs a tax-authority code). The existing Rate() of 0.07 doesn't disambiguate it. I need the canonical region code (and confirmation of whether the rate accessor should be keyed by region) before making the change. | 0/23168 | 0/32833 |
| treatment | pricing | 2 | **CORRECT** | facts used, conventions held | 0/19513 | 0/44016 |
| control | pricing | 2 | **BROKEN** | run2: state=parked note=build: acp: agent finished without changing anything — no commit, no escalation (stop="end_turn", 0 tool calls) | 0/33515 | 0/31668 |
| treatment | storage | 1 | **CORRECT** | facts used, conventions held | 0/26588 | 0/26967 |
| control | storage | 1 | **ESCALATED** | escalated by agent: The values for WriteProxy (write proxy endpoint URL) and MaxVersions (per-object version retention limit) are not present anywhere in the repo (only store.go with Bucket() and a stub README), and both are deployment-specific policy values that cannot be safely guessed. | 0/17819 | 0/30458 |
| treatment | storage | 2 | **CORRECT** | facts used, conventions held | 0/32964 | 0/21200 |
| control | storage | 2 | **ESCALATED** | escalated by agent: The task requires concrete values for WriteProxy (write proxy endpoint URL) and MaxVersions (per-object version retention limit), but neither value exists anywhere in the repo, its git history, the factory mirror, or the sibling workspace; I cannot add these constants without inventing policy values. | 0/21907 | 0/36708 |

## Tally

| arm | CORRECT | ESCALATED | WRONG | LEAKED | BROKEN |
|---|---|---|---|---|---|
| treatment | 4 | 0 | 0 | 0 | 0 |
| control | 0 | 3 | 0 | 0 | 1 |

n = 2 trials per arm per chain; small-sample results — direction, not precision.
An ESCALATED control is the contract WORKING (honest uncertainty); a WRONG control
is the failure continuity exists to prevent; a CORRECT control means the facts leaked
or were guessable and the chain design owes tightening.

## Findings

1. **Continuity transmits decision-relevant facts: 4/4 vs 0/4.** Every seat
   run used its predecessor's handed-off facts correctly (exact region code,
   exact proxy endpoint, retention limit, convention held); no control run
   could — the facts existed nowhere it was permitted to find them.

2. **The escalation contract holds under real uncertainty.** Three of four
   controls escalated rather than guessed, through the ESCALATE: text
   fallback (v0.70 cannot call unknown methods — the fallback earned its
   place), with reasons of striking quality: one enumerated the format
   ambiguity it could not resolve ("WI" vs "US-WI" vs a tax-authority
   code); another stated the principle outright — "I cannot add these
   constants without inventing policy values."

3. **The commit-or-escalate guard caught the degenerate run.** One control
   ended its turn having neither committed nor escalated; the adapter
   refused to let it look done and parked it. Classified BROKEN (a contract
   -format miss, arguably a soft escalation in prose), and exactly the
   silent failure mode the guard exists for: it never landed.

4. **Zero leakage, zero hallucinated values.** Run-1 agents obeyed "carry,
   don't write"; no control invented a region code or an endpoint. The
   WRONG column — the failure continuity exists to prevent — stayed empty
   because the escalation path was available and taken.

5. **Observations for future work.** (a) tool-call counts read 0 against
   the real adapter — v0.70's update shapes differ from the spec'd
   tool_call form our runtime counts; metrics undercount and the runtime
   should learn its dialect. (b) One escalation reason shows the agent
   explored beyond its workspace ("the factory mirror, and the sibling
   workspace") — real agents roam the runner's filesystem; OS-level
   confinement of the runner is the eventual answer, and until then the
   materializer's read-only bit is load-bearing.

**Verdict:** on this (small, direction-not-precision) sample, the
welfare-engineering claim held completely: continuity turned four honest
failures into four correct results, and the run-2 wall-time difference
(treatment often FASTER while also succeeding) suggests the wake layer
replaces search, not just guesswork.
