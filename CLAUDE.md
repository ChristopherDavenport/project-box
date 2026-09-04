# project-box

A control plane for dark software factories: agents build, verify, and land
changes end to end, with SOC 2 evidence produced by the loop itself and
everything provable locally before anything is provisioned. This repository
also builds itself: the dogfood box in `hack/dogfood` runs agents against
these very issues, so the builder reading this may be one.

## The design docs are the record, not the reading list

`docs/ARCHITECTURE.md` and `docs/PLAN.md` are 86KB each. Do not read either
whole. Loaded once, they sit in every later turn's context; on a
120-tool-call run that was about a fifth of the entire bill. Read the one
section your change touches, by heading:

- `docs/PLAN.md` §6 "Decisions already settled" is the table of what is not
  up for re-litigation. A change that contradicts a row must say so in its
  commit message; contradicting one silently is wrong even when it compiles.
- `docs/PLAN.md` §10 is where execution stands.
- `docs/ARCHITECTURE.md` by heading: Tenancy · The loop · The forge app ·
  Ingress · Identity and permissioning · Human approval · The agent plane ·
  Evidence · Telemetry · Storage abstraction and local-first · Landing across
  repositories · The daemon is a socket. `grep -n '^## '` lists them;
  `sed -n` the range you need.

## Rules a change most often breaks

- The tenant is explicit on every key and every record, never derived.
- Evidence is append-only and hash-chained, written where the fact is known
  by the actor that knows it, with on-behalf-of threaded through. It is
  separate from telemetry: a sampled pipeline cannot carry an audit trail.
- Unknown never reads as zero. Unmetered spend, unclassified data, an absent
  judgement, an unreported cost: each refuses rather than defaults.
- Delegation intersects, never unions; an envelope narrows, never widens.
- The daemon is socket-only. The API writes intent, the pump is the only
  executor, and anything network-shaped is a layer in front.
- Gates judge content; landing is fenced by the lease token and an
  expected-old-value push. Approvals bind to a commit, never a pull request.
- What an agent could do is recorded before it runs (the agentref); what it
  was actually given is asserted at the wire and recorded as answered, never
  assumed from a default.
- A count is a projection of the listing, never a second reading of it.

## How to work here

- Load the `go` skill before writing Go and the `sqlite` skill before touching
  the store. They carry the idioms this repository expects; the dogfood box
  pins both by commit and they arrive as `box-skills:go` and
  `box-skills:sqlite`.
- A property that is not tested is a comment. Tests assert behaviour, and the
  store's conformance suite runs against both backends.
- `go build ./... && go test ./...` is the check the dogfood gates on; add
  `-race` when touching anything concurrent. README.md states the web bundle
  rule in force.
- Read narrowly: `grep -n` and `sed -n` ranges over whole files; run the
  package you changed while iterating and the full suite once at the end.
  Every byte loaded is re-read on every later turn.
- Commit messages name the finding in a sentence, in this repository's voice
  ("A token is not a unit of money"); the body says why.
- Commit; never push. Landing is the factory's job. If the issue is a
  question, a decision, or already satisfied, escalate with the reason rather
  than inventing a change.
