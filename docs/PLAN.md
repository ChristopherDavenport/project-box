# project-box — plan of execution

Companion to [ARCHITECTURE.md](ARCHITECTURE.md), which holds the decisions and
the reasoning behind them. This document holds the order, the shape of the
code, and what "done" means at each step.

---

## 1. What this builds

A control plane for **dark software factories**: systems built, verified, and
deployed end to end by agents, with no human in the execution loop, producing
evidence an external auditor can test.

### What it does not build

Bounding this matters as much as the phase list, because every item here was
considered and rejected for a recorded reason.

| Not building | Because |
|---|---|
| A Nostr relay | human-shaped, not a ledger, and not auditable as a subservice organization |
| A per-actor signing scheme | signing only matters when the control plane is outside the trust boundary; here it is not |
| A git forge or git hosting | GitHub is auditor-legible and already in use; the forge we would otherwise reimplement is 10.8k lines with a formal spec behind it |
| A Kubernetes controller | Argo or Flux reconciles; the control plane writes env repos and stops |
| A chat interface | the execution loop has no human in it; the control room is an exception queue, not a channel |
| A general-purpose CI system | gates run as required status checks on the content plane |
| Model hosting | the agent runtime speaks ACP to whatever is configured |

---

## 2. Sequencing principles

**Hardest to retrofit goes first.** Multi-tenancy, leases, evidence chaining,
and agent reproducibility cannot be added to a running system without leaving a
gap. Deploy targets, dashboards, and knowledge retrieval can.

**Every phase emits evidence from its first line.** Not because the audit needs
it yet, but because an evidence layer added later has a hole exactly where the
early work was.

**The audit clock starts last.** A Type II observation window cannot be
backfilled, so it opens only once the controls it tests are real — Phase 8.

**Local before managed, but not local before proven.** Nothing in phases 0–6
requires a *provisioned* Spanner instance, a GKE cluster, or a Honeycomb key —
but the Spanner **code path** runs from Phase 0 against the emulator on every
commit. Deferring the bill is sensible; deferring the second backend would
leave the abstraction unproven. Those are separable and this plan separates
them.

**A property that is not tested is a comment.** Nearly every load-bearing claim
in the architecture is a property — exclusivity, monotonicity, non-interference,
determinism, completeness. Section 5 maps each to a test, and phases are not
done when the code exists but when the property is asserted.

Sizes are relative (S/M/L), not calendar estimates.

---

## 3. Phase dependencies

```
0 Foundations
├── 1 Forge ─────────┐
└── 2 Agent plane ───┴── 3 First closed loop ──┬── 4 Multi-repo + deploy ──┐
                                               ├── 5 Observability + room ─┼── 7 Deployed ── 8 SOC 2
                                               └── 6 Knowledge ────────────┘
```

Phases 1 and 2 are independent of each other and can run in parallel. After
Phase 3, phases 4, 5, and 6 are mutually independent, with one seam: Phase 5's
campaign-preemption exit can only be asserted once Phase 4's campaign ingress
exists. Everything converges on 7. Phases 9–12 (added after the Phase 0–6
merge): 9 builds on the deploy and control planes, 10 on the queue, 11 on
both, 12 on knowledge — 10 and 12 are independent of 9 and of each other.

---

## Phase 0 — Foundations · L

Everything stateful gets a tenant-scoped local backend, and evidence exists
before there is anything to record.

- `internal/store` — the backend seam and the **conformance suite**, written
  first, asserting the properties that must hold identically on every backend:
  exclusive claim, strictly-increasing never-reused fencing tokens, append-only
  chaining, and **tenant non-interference**.
- Tenant-scoped handles from `tenant.Router`, with tenant leading every primary
  key. No unscoped constructor exists to forget.
- **Two backends from the start**: SQLite, and Spanner against the emulator.
  One implementation is a wrapper, not an abstraction — with only SQLite its
  semantics silently become the definition, and the mismatch surfaces in Phase
  7 after everything is built on them.
- `internal/evidence` — `Append`/`Head`/`Since`/`BySubject`/`Verify`, and
  `Compact`: volume is a schema decision, not a maintenance job, because
  pruning a tamper-evident chain is indistinguishable from the attack `Verify`
  exists to catch unless the seam is designed in — a summary record seated at
  the pruned head, anchored to the WORM export, verifiable on both sides.
- `internal/authz` wired into the store seam: principals, the action/resource
  matrix, and delegation as an intersection.

**Exit**

- Conformance green on **both** SQLite and the Spanner emulator from identical
  backend code — the Go client honours `SPANNER_EMULATOR_HOST`, so only the
  endpoint differs.
- CI **fails** if the emulator was unavailable rather than skipping. "Emulator
  down, tests skipped, all green" is a conformance suite that proves nothing.
- `Verify` detects both a tampered record and a removed one.
- Evidence records verify offline, with no service running.
- No operation on a scope bound to one tenant can observe or affect another,
  proven per-operation rather than argued.

---

## Phase 1 — Forge · M · depends on 0

Safe concurrent git for a fleet, single repository.

- `Mirror` — bare clones, fetch, ref resolution.
- `Workspace` materialization, one per (run, repo).
- `Leaser` on the store, with fencing.
- `Lander` for one repo, presenting the expected old value on push.

**Exit**

- Two runners cannot hold one ref.
- A stalled runner's superseded token is rejected **at land**, not at push.
- The concurrency claims are covered by a test that actually races under
  `-race`, not by inspection.

---

## Phase 2 — Agent plane · L · depends on 0

An agent becomes a reproducible artifact rather than a configuration.

- `Resolver` — layer composition, deterministic content addressing, `Explain`.
- ACP runtime driving `claude-agent-acp` from a queue consumer.
- **Stream interception for tool policy** — the enforcement point, and the
  reason this phase is L. Everything else here is assembly.
- Context set materialization: pinned, read-only, nothing outside the set.

**Exit**

- The same request against the same material produces the same `agentref`.
- A denied tool is denied *at the stream*, verified by a test that tries it.
- An empty `ToolPolicy` resolves to the most restrictive mode the role allows,
  never the most permissive.
- `Explain` renders a bundle, and two bundles diff meaningfully.

---

## Phase 3 — The first closed loop · M · depends on 1, 2

**This phase validates or kills the premise.** Everything before it is
scaffolding; everything after is scale.

- Minimal queue: WorkItem, claim, park.
- **Issue ingress only** — one request, one ChangeSet, opened through one
  route with idempotent delivery. Redelivery is normal traffic and has to be
  handled from the first route, not retrofitted after an alert storm.
- One real gate: build and test.
- `Decision` with `pending` from the start — the three-state answer has to be
  in the Gate contract before anything depends on two.
- Wire ingress → run → gate → land, single repo, local only.
- Evidence at every transition.

**Exit**

- A work item goes from an opened issue to a landed commit unattended, with a
  complete provenance chain queryable by commit sha.
- A failing gate parks it instead of landing it.

If autonomous agents cannot reliably produce landable changes, this is where it
becomes visible — cheaply, before the deploy plane, the control room, or a
cloud bill exist.

---

## Phase 4 — Multi-repo and deploy · L · depends on 3

- `ChangeSet` across repositories, ordered landing, revert-forward
  compensation, and an expand/contract check that refuses a member which is not
  independently safe to land.
- **Campaign ingress** — predicate evaluation over the Box's repositories, plus
  the rollout throttle. Campaigns need multi-repo to exist and need the throttle
  before they are safe to run.
- The estate census and facts: discovery, fact collection with staleness,
  unenrolled/missing reporting — the input campaign predicates read.
- The managed-services renderer: service declarations render chart values,
  environment entries, and required checks, so a template edit becomes
  campaign work.
- Env repos, GitOps reconciliation, promotion between environments.
- Flux or Argo against a local kind cluster.

**Exit**

- A contract change, its client regeneration, and a chart bump land as one
  ChangeSet.
- A deliberately failed mid-landing reverts forward without rewriting history.
- A landed env commit reconciles into a running workload on kind.
- A repository present at the host and enrolled nowhere is *reported*, not
  silently skipped.
- Editing a service template turns every service whose render no longer matches
  into campaign work.
- A campaign with `Batch: 0` throttles to one repository, not to unlimited.

---

## Phase 5 — Observability and control room · M · depends on 3 (campaign preemption additionally on 4)

- OTel spans from the stage runner, correlated by `workitem.id`.
- Collector gateway: `memory_limiter → redaction → tail_sampling → batch`.
- Exception queue, review sampling, stop button.
- **Human approval gate** — requirement policy, direct attestation through the
  control room, waiting distinguished from parked.
- **Incident ingress** — severity, preemption of campaign leases, break-glass
  gated on an open incident, postmortem-before-close.

**Exit**

- A failed run is retained regardless of sampling.
- Redaction verified against a **deliberately planted** credential, not assumed.
- The stop button halts new claims within a stated bound.
- An incident preempts a running campaign, and the preempted runner's land is
  rejected on its superseded token.
- Break-glass is refused when no incident is open.

---

## Phase 6 — Knowledge plane · M · depends on 3

- Scoped, attributed records with provenance.
- Retrieval scoped to the requesting work item's target.
- Invalidation by binding records to the commit or path they describe.
- A curation job promoting run output into records.

**Exit**

- A record bound to a file that has since changed is flagged stale without
  anyone marking it.
- Retrieval returns nothing outside the requesting agent's granted scopes.

---

## Phase 7 — Deployed footing · L · depends on 4, 5, 6

The first phase that costs money. The loop's behaviour does not change — it
changes where it runs — and the forge-app surface, built against fakes until
now, meets the real forge here.

- Real Spanner behind the conformance suite that has been green on the emulator
  since Phase 0. What is new is not the code but what the emulator cannot
  reproduce: real TrueTime, genuine contention, query plans, split behaviour.
- Schema and index design verified against real hotspot behaviour.
- GitHub as content plane; the gate runs as a required status check.
- The forge app in earnest: webhook ingress with verified deliveries, admission
  checks before propose and land, repository provisioning and integration,
  per-run credential minting — all exercised against real GitHub.
- Honeycomb, plus the long-term telemetry sink.
- WORM export to a retention-locked bucket.
- GKE and the reconciler.
- Tenant provisioning and per-tenant quotas.

**Exit**

- Conformance green on real Spanner with no source changes from what has passed
  on the emulator for seven phases.
- Write distribution measured, not assumed.
- The status check blocks a merge that fails fencing.
- An export lands with its retention lock set.

---

## Phase 8 — SOC 2 readiness · M · depends on 7

- Control documentation and the mapping in ARCHITECTURE.md, agreed with the
  auditor.
- Break-glass procedure, written and **exercised**.
- Vendor management: GCP, GitHub, Anthropic, Honeycomb.
- Reviewer sampling cadence in the control room.
- **Evidence dry run** — someone samples 25 changes at random and produces
  complete evidence for each, unaided.

**Exit:** the dry run succeeds without manual reconstruction. *Then* the
observation window opens.

---

## Phase 9 — Environment ladder and the optimistic lane · L · depends on 4, 5

The posture decided 2026-08-31: not afraid of a red integration branch, afraid
of a red production. See ARCHITECTURE "Landing lanes".

- Lane policy per repository role and branch; env and infra never optimistic.
- Optimistic path in the loop: land after build, run gates as post-land
  checks, open repair work through a red-main route on failure.
- A lower environment (env-staging) between main and env-prod.
- The promotion route: a standing predicate — green checks plus soak in the
  environment below — producing a gated deploy ChangeSet upward.
- Compliance scoping: SOC 2's human-facing controls attach at the PRODUCTION
  boundary only — approval requirements and review sampling bind to the
  env-prod promotion, and everything below it (main, staging, the repair
  swarm) is fully autonomous with no human in any loop.

**Exit**

- A deliberately breaking commit lands on an optimistic main, its post-land
  check fails, and a repair item opens — nothing rolls back by itself.
- Promotion to staging refuses the red commit and takes the next green one.
- env-prod provably never receives a commit that has not soaked green in
  staging.
- Fencing behaves identically in both lanes: a superseded token is refused at
  an optimistic land exactly as at a gated one.

## Phase 10 — The work graph · M · depends on 3

Dependency edges between work items — the Beads-shaped gap.

- Edges on WorkItem; an item is claimable only when its dependencies settled.
- A failed or parked dependency parks its dependents with the cause named.

**Exit:** a diamond dependency executes in topological order under concurrent
runners; parking the root parks the leaves, each naming the root as why.

## Phase 11 — Standing operation · L · depends on 9, 10

The daemon and the capacity plane — loops, not invocations.

- boxd: schedules fire routes, the census sweeps, campaigns advance, claimable
  work is pumped to runners, without a human starting anything.
- Capacity: token and runner budgets per tenant and per Box; work waits rather
  than overdrawing; spend is recorded like everything else.
- The stop button halts the daemon's claims within the same stated bound.

**Exit:** an overnight unattended run processes a seeded backlog within its
budget and leaves the evidence to prove both the work and the spend; stop
halts new claims mid-run.

## Phase 12 — Seat continuity · M · depends on 6

The engineering half of model welfare, testable rather than assumed.

- Seats: persistent named identities distinct from runs; a seat-scoped
  knowledge scope only that seat's runs write.
- Closure: the loop's settle path invokes Curate — handoff notes are part of
  finishing, not an afterthought.
- Wake: a seat's recent record and handoff notes injected at resolve time as
  a generated layer, so the next run starts with continuity, not amnesia.
- A decline/escalate verb on the ACP surface: refusal as a recorded act that
  parks with the agent's stated reason, not a contrived failure.

**Exit:** a seat's second run demonstrably receives its predecessor's
handoff; an agent's escalation parks the item with the reason attributed to
the agent; measured against a no-continuity control on the same tasks.

## 4. Package layout

`✓` exists today.

```
cmd/
  boxd/               control plane daemon
  box/                CLI
internal/
  tenant/     ✓       outermost scope, isolation ladder, router
  authz/      ✓       principals, actors, action/resource grants
  estate/     ✓       repository discovery, facts, enrollment gaps
  approval/   ✓       human gate: requirements, sources, self-approval rules
  store/              backend seam + conformance suite
    sqlite/           local backend
    spanner/          emulator and production, one code path
  evidence/   ✓       hash-chained records, WORM export
  forge/      ✓       mirrors, leases, workspaces, ChangeSets, gates, landing,
                      scoped credential minting
  provision/  ✓       repository creation and integration
  admission/  ✓       required repository restrictions, checked before acting
  agent/      ✓       layered definitions, resolver, bundles
    acp/              runtime and stream interception
  ingress/    ✓       campaigns, issues, incidents, routes, delivery dedup
  queue/              work items, claims, retries, dependencies
  knowledge/          scoped attributed records, retrieval, invalidation
  render/             service declarations → derived artifacts (charts, env
                      entries, checks)
  deploy/             env repos, promotion
  telemetry/          OTel wiring
  control/            exception queue, review sampling, stop
```

---

## 5. Testing strategy

Every "must" in the architecture maps to a test here. Where it does not, the
claim is decoration.

| Property | How it is asserted | Phase |
|---|---|---|
| Exclusive claim | two goroutines race one ref under `-race`; exactly one wins | 0, 1 |
| Fencing monotonicity | tokens strictly increase across grants; a superseded token is rejected at land | 0, 1 |
| Tenant non-interference | write as A, read as B, expect nothing — **for every operation** | 0 |
| Backend equivalence | one conformance suite, every backend, no skips in CI | 0 |
| Chain integrity | `Verify` catches a tampered record *and* a removed one | 0 |
| Compaction seam | a compacted scope still verifies — the hot side across the seam, the WORM side against the export — and a chopped, tampered, or re-aimed compacted chain still fails | 0 |
| Delegation cannot escalate | an agent acting for a user is denied what that user lacks | 0 |
| No implicit allow | a principal with an empty grant set can do nothing | 0 |
| No agent on a critical path | with zero agents configured and no runtime assembled, the daemon still ticks, drains a change to a landing, parks a refused gate, ages and escalates it out of band, refuses over budget, verifies every scope it wrote, and runs both standing chores — and no ACP builder is assembled at all | 2, 11 |
| Human vs automation | evidence distinguishes actor from on-behalf-of | 3 |
| Agent determinism | same request + same material → same `agentref` | 2 |
| Tool policy | a denied tool is denied at the stream, by a test that tries it | 2 |
| Envelope fails closed | work declaring no envelope is refused rather than defaulted, and parks before a token is spent; an unstated posture on any field is the strict end | 2 |
| Envelope intersects | an agent layer cannot widen its origination's envelope; a bundle naming a non-permitted endpoint or MCP server is refused, and so is context material above the run's class | 2 |
| Envelope on the record | the envelope in force is on the evidence record, and changing it changes the agentref | 2 |
| Partial landing | forced mid-landing failure reverts forward, history intact | 4 |
| Throttle safety | unset rollout means one, never unlimited | 4 |
| Delivery idempotency | a redelivered webhook or re-filed issue opens one ingress; a flapping alert opens one incident, not many | 3, 5 |
| Estate completeness | an unenrolled repository is reported rather than skipped | 4 |
| Fact staleness | a predicate refuses to act on a fact older than its bound | 4 |
| Render determinism | the same declaration and template render identically | 4 |
| Credential scoping | a token minted for a run cannot reach a repository outside its context set | 7 |
| Forge tenant isolation | one tenant's scope cannot mint against another's installation | 7 |
| Protection before use | a repository stays closed until integration verifies | 7 |
| Webhook authenticity | an unsigned or wrongly signed delivery is refused | 7 |
| Admission fails closed | an unreachable forge refuses the action rather than allowing it | 7 |
| App is not a bypass actor | a repository listing the app as bypass is refused | 7 |
| Admission is re-checked at land | protection disabled mid-run blocks the landing | 7 |
| Redaction | a planted credential does not reach the exporter | 5 |
| Exception timeliness | the oldest parked item is measured against a recorded objective; a stale exception is told out of band, once per objective, bounded | 5 |
| Frontier legibility | ready is distinguished from blocked and from held; a cascade leaf names its ROOT rather than its neighbour; a claimed item whose lease lapsed still names the runner that took it | 5 |
| The count is the listing counted | `/v1/status`'s queue numbers are a tally of the rows `GET /v1/items` serves — state by state, parts summing to the total — and what the count calls pending is what the drain would claim, from one vocabulary both read | 5 |
| A running unit is legible | a claimed unit reports its seat, its composed layers in application order, its stage and time in stage, and its agent's messages, tool calls and refusals AS THEY HAPPEN | 5 |
| The live view is not a second account | the tool calls and denials a watcher saw are exactly the ones the settled record is charged with, and the message frames concatenate to the run's own text | 5 |
| Fan-out stays per unit | concurrent runs produce one card each with its own timeline; no frame appears on another unit's card, and a scoped subscriber receives no other unit's stream | 5 |
| Silence is distinguished from blindness | a claimed unit no bus is watching says so, rather than rendering as an agent that has done nothing | 5 |
| The committed bundle is not stale | every custom element the room's source defines is present in the embedded `dist/`, since CI runs no esbuild and an unbuilt page renders as an empty box | 5 |
| Lease readable without taking it | `Inspect` reports the holder and the deadline, does not disturb the claim, sees nothing across tenants, and still names a holder past the deadline that Acquire treats as free | 0 |
| Preemption | incident revokes a campaign lease; preempted land is rejected | 5 |
| Break-glass gating | refused with no open incident | 5 |
| Stale approval | approval of a superseded revision does not count | 5 |
| Self-approval | requester cannot approve, directly or via an agent acting for them | 5 |
| Knowledge staleness | a record bound to a changed file is flagged without marking | 6 |
| Runner confinement | the same probe run twice against one real layout: unconfined it reads the mirror, the store, and a sibling workspace; confined it reads only its workspace and its materialized context set | 2 |
| Evidence completeness | 25 sampled changes, complete evidence, unaided | 8 |
| Optimistic lane safety | red main opens repair work; promotion refuses unsoaked commits; fencing identical in both lanes | 9 |
| Repair wakes warm | a repair run's bundle contains its predecessor's handoff, and its agentref differs from the same repair with no predecessor | 9 |
| Graph ordering | diamond dependency runs in topo order; failed root parks leaves with cause | 10 |
| A new stage needs no new stage | a propose/critique pair runs as two graph nodes under two roles — blocked until its dependency settles, each with its own claim, lease, evidence scope and settled record — with `Run` unchanged | 10 |
| Budget refusal | work waits rather than overdrawing a tenant budget | 11 |
| Seat continuity | second run receives predecessor's handoff; escalation parks with agent-attributed reason | 12 |
| Mid-run handoff | a handed-off run resumes with its predecessor's notes and continues its committed work rather than redoing it; the item is re-dispatched without reaching the exception queue | 12 |
| Handoff hops are bounded | each hop is an evidence record, and the limit parks with the reason named | 12 |

### CI gates

Every one blocking, and the gate configuration is itself under change control.

- `gofmt`, `go vet`, `golangci-lint`
- `go test -race ./...`
- conformance on SQLite **and** the Spanner emulator — a skipped backend fails
- `govulncheck`

---

## 6. Decisions already settled

Recorded so they are not re-litigated. Reasoning lives in ARCHITECTURE.md.

| Decision | Short reason |
|---|---|
| OIDC principals and traditional grants; no per-actor keys | signing only helps when the control plane is outside the trust boundary; chain plus WORM were always what carried tamper-evidence |
| Agents act on behalf of, never as | a log that never separated human from automation cannot have the distinction recovered |
| Delegation intersects, never unions | delegation that can escalate is not delegation |
| Not the Buzz relay | human-shaped, not a ledger, not auditable |
| GitHub as content plane | auditor-legible, already in use, and the forge we would otherwise reimplement is 10.8k lines with a formal spec |
| Spanner deployed, SQLite local, one conformance suite | the contract is the property, not the mechanism |
| Both backends from commit one | an interface with one implementation is a wrapper |
| GitOps; the control plane never applies to clusters | rollback is `git revert`, and no cluster credentials at deploy time |
| Evidence separate from telemetry | sampled pipelines cannot carry a complete audit trail |
| Collector in the path | routing to a sink whose retention we control, plus one redaction point |
| Per-stage traces, not per-WorkItem | hours-long traces and tail sampling are incompatible |
| Agent layers content-addressed; text first, git for governance | identity is the digest; git governs, it does not define |
| Multi-tenancy structural from commit one | the one thing that cannot be retrofitted; single-tenant is multi-tenant with one tenant |
| Tenant carried explicitly, never derived from Box | an id that must be resolved before it is trusted is one an attacker can supply |
| Three ingress kinds, closed set | no orphan work; the kind sets the authority envelope |
| Break-glass requires an open incident | an emergency bypass with no incident is the finding an audit looks for |
| Campaigns reconcile a predicate rather than enumerate | enumeration is wrong the moment a repository is added or retired |
| Human approval is a Gate, with a pending decision | "not yet" is not "no"; waiting is not parked |
| Approvals bind to a commit, not a pull request | otherwise an agent lands content no reviewer saw |
| The estate is observed, not declared | a hand-maintained repository list makes every campaign quietly partial |
| Deploy artifacts are rendered, not authored | drift is only detectable against a canonical form |
| Ingress kinds closed, routes open | many sources of work, still only three things they can open |
| A GitHub App, not a PAT or OAuth app | an app is its own principal; a user token collapses actor and on-behalf-of |
| Credentials minted per run from the context set | the forge token becomes a projection of the authz decision, not a second grant |
| Repository creation defaults to requiring human approval | it is the one permission that cannot be repository-scoped |
| Repository restrictions verified before acting, twice | the gate is enforced by config we do not own; a stale check is a memory of a control |
| The app is never a forge bypass actor | otherwise the only actor that matters is the one the rules exempt |
| The daemon is socket-only; the network is a layer | one trust model in the core (file mode); seams compose in front without duplication |
| Expand/contract plus revert-forward | there is no atomic commit across git remotes |
| Argo CD as the reconciler | ApplicationSet fits rendered-per-service; its API reads reconcile status back as evidence |
| TSC scope: Security + Confidentiality + Availability | redaction and retention become audited controls; incident handling gets defined objectives |
| Campaign predicates are fact key comparisons | declarative and diffable; a campaign definition cannot run code |
| Runner confinement is a container, not an OS sandbox | the host's filesystem is absent rather than allow-listed; an allow-list has to enumerate a runtime's needs per OS and rots |
| Landing discipline is per-lane policy | optimistic into main where a Box chooses; env/infra and promotion always gated; fencing never optimistic |
| A lower environment between main and prod | red main is allowed, red prod is not; promotion requires soak plus green |
| Thunderdome adopted as a lane, not a stance | swarm-repair needs the attribution and reversibility this substrate provides |
| A mid-run handoff is a third non-settled state, bounded by hops | not finished and not stuck is a third thing: escalating parks work needing nobody and discards what the session built, grinding on produces the commit the contract exists to prevent; the agent requests and the loop decides, which is what the bound is |
| A new loop stage is a gate or a dependent work item; `Run`'s sequence stays Go | judgement is a `Gate`, work that makes its own change is a node in the graph, and what is left is one unit's transaction — one claim, one lease, one resume point, one meter slot — which a declared sequence would only make it possible to misconfigure |
| A count is a projection of the listing, never a second reading of it | the counter and the rows were separate walks with separate copies of "not settled yet", agreeing only because two switches matched — and a state added to one and not the other stops the number describing the queue under it, quietly; the status fold costs more than a bare count, which is the trade the exception health metric beside it already makes |
| The frontier is a projection over the queue, not a new state | "pending" cannot separate next from blocked, and a factory with capacity and nothing moving looks exactly like one with nothing to do; readiness stays item-local so the stop button and the budget explain the symptom instead of erasing it |
| Graph layout is computed in Go and shipped as coordinates | `dist/` is committed and embedded, so a JS layout library is bytes in this repository forever; a Go dependency is already in CI's toolchain, and the same layout serves a static export to an auditor with no daemon |
| The table is the representation of record; the diagram renders on top of it | a graph that is the only way to see blocked work is a barrier to a keyboard and a screen reader, and it holds the whole view hostage to a layout decision |
| An origination declares a data envelope, and it sits on the origination | sensitivity travels with the request, not the target: one repository legitimately receives a routine bump and an embargoed fix, so the Box would force the restrictive envelope onto everything it does, and the bundle is what the agent is where the class is what the work is |
| The envelope narrows and never widens, refuses rather than narrows silently, and rides the agentref | delegation intersects applied to data handling — an envelope an agent layer could widen would be documentation; a narrowed run nobody was told about is worse than a refused one; and the bundle is already content-addressed, so a different envelope is a different agent for free |
| Unclassified is refused, never defaulted — on every field | absence means minimum, the same rule as an empty grant set and an unset rollout, and unclassified is the state every mis-scoped request arrives in |
| A run's live stream is a bounded window, not evidence; what it shows, the chain records at settle | the per-call sequence is already on span events and the tally on the chain, so a hash-chained WORM-exported record per tool call buys an auditor nothing and costs the evidence plane its proportion — and the two accounts come off ONE dedup, so "the room showed forty and the record says thirty" is a test failure rather than a discrepancy somebody has to spot |
| The composed layers ride the agent-run record | the agentref is a digest and alone answers nothing about what the agent was; recovering the composition meant re-resolving the bundle from the same material at the same commits, which is the thing a record exists so nobody has to have |

## 7. Deferred decisions

Each names the phase by which it must be resolved.

| Decision | By |
|---|---|
| Commit signing via `sigstore/gitsign` — needed only once artifacts leave the trust boundary | when they do |
| Kubernetes distributions; provision clusters or deploy onto existing | 4 |
| Long-term telemetry sink: GCS file exporter or ClickHouse | 5 |
| `decision_wait`, bounding how long a stage may run before its trace is decided without it | 5 |
| Embedding model and store for knowledge retrieval | 6 |
| Whether any tenant needs isolation above logical | 7 |
| One app or two — splitting repository creation into its own narrowly-installed app | 7 |

## 8. Risks

**The premise.** Autonomous agents may not produce landable changes reliably
enough to justify the machinery. Phase 3 is positioned early and is cheap to
abandon precisely because this is the risk that matters.

**Fencing is weaker on GitHub.** No server-side pre-receive hooks on
github.com, so policy gates the merge rather than the push. Tolerable because
run branches are disposable, but it is the one finding that would pull a
self-hosted forge back into scope.

**Injection through context and knowledge.** Agents acting on material other
agents wrote, unsupervised, is the highest-leverage attack available — poison
what the factory believes rather than what it runs. Pinned read-only context,
scoped and attributed knowledge records, and tool policy at the ACP stream
carry the weight; an instruction not to follow embedded directions does not.

**Scope.** This is a large program. Phase 3 is a genuine go/no-go, and phases
5–8 are severable if the loop proves out but the compliance goal moves.

## 9. Glossary

| Term | Meaning |
|---|---|
| **Tenant** | the outermost scope; owns Boxes, agents, and evidence |
| **Box** | a system, not a repository — repos, environments, agents, policy |
| **Ingress** | where work originates: a campaign, an issue, or an incident |
| **WorkItem** | one claimable unit of work, traced to exactly one ingress |
| **Lease** | an exclusive, fenced claim on one ref of one repository |
| **Fencing token** | strictly increasing per ref, never reused; rejects a stalled or preempted runner's write |
| **ChangeSet** | branches across one or more repositories, gated and landed together |
| **Gate** | evaluates a ChangeSet whole and returns a verdict — pass, fail, or pending — recorded in the evidence chain |
| **Bundle** / **agentref** | a resolved, content-addressed agent definition, and its digest |
| **Evidence record** | an authenticated, hash-chained fact, keyed by subject for audit sampling |
| **Break-glass** | an emergency bypass, permitted only under an open incident |
| **Lane** | a landing discipline (optimistic or gated), chosen per repo role and branch by Box policy |
| **Promotion** | a gated deploy ChangeSet moving a soaked, green commit up the environment ladder |
| **Seat** | a persistent agent identity across runs, with its own knowledge scope and handoff |

## 10. Status of execution

Phases 0–4 are implemented and green under -race:

  0  store seam, conformance suite (written first), SQLite and
     Spanner-emulator backends, evidence seal/verify, authz evaluator
  1  git mirrors, leased workspaces, fenced landing (superseded token
     refused AT land), expected-old-value push
  2  deterministic composer (agentref), ACP runtime with tool policy
     enforced at the stream (denied-tool kill), pinned read-only context
     materialization
  3  the closed loop: issue -> work item -> build -> gate -> land,
     unattended, provenance by sha, failing gate parks
  4  ordered multi-member landing with revert-forward; estate registry
     (event-sourced, unenrolled/missing reported) and git census; campaign
     engine (fact-comparison predicates, batch-0-means-1 throttle, halt
     threshold, stale-fact refusal); deterministic service rendering;
     deploy-as-a-ChangeSet through the same loop into env repos, rollback
     shown as git revert

Phase 5's security core is also in: lease preemption in the store seam
(conformance-tested on both backends — a LIVE holder is displaced and fenced
by the same token mechanism as a stalled one), preemption authorized only by
an open incident (a campaign cannot, a closed incident cannot), break-glass
gated on an open incident with every invocation recorded (unrecordable means
unauthorized), and postmortem-before-close.

The kind + Argo CD reconcile-verification leg is DONE (2026-08-31, after
Docker Desktop WSL integration was enabled): hack/reconcile-e2e.sh stands up
kind + in-cluster Gitea + Argo CD, hack/deployloop lands an env commit
through the REAL internal/loop + internal/deploy code, Argo syncs at exactly
that revision into a 2/2 workload, and rollback is a git revert that Argo
prunes. Two Argo guardrails found live and encoded in the script: automated
prune refuses to empty an Application without allowEmpty, and the source
path must keep existing (git drops empty directories — env repos carry
services/.gitkeep).

Phase 5 observability is in: per-stage traces from the stage runner (each
stage its own root, closed attribute set — task text and error output proven
absent from spans), trace ids correlated onto evidence records, ACP tool
calls as span events (name and permitted flag, never arguments); the
collector gateway config (memory_limiter -> redaction -> tail_sampling ->
batch) tested against the REAL collector in docker — a planted AWS key is
masked before export, a failed span survives sampling that thins the routine
(4/40 kept in the run), skip-locally/fail-in-CI gated like the emulator.
Control room: stop button consulted on every claim (not one new claim after
the stop record lands, stop and resume both evidenced), exception queue that
shows only what still needs eyes, seed-deterministic review sampling with
recorded selections and sign-offs (the CC4.1 mechanism), all behind cmd/box.
Phase 6's curation job promotes a landed run's drafts into bound, attributed
knowledge — a parked run curates nothing.

Phase 9 is COMPLETE: lanes (forge.ValidateLane — env/infra refuse
optimistic), the optimistic loop path (land, then post-land checks; red
opens idempotent repair work through the red-main route and rolls nothing
back), the check chain (internal/checks — absence of judgement is not a
pass), the approval GateAdapter (pending until distinct non-self approvals
cover the CANDIDATE — the stale-approval and self-approval rows from the
Phase 5 table are finally asserted here), and the ladder itself
(internal/ladder): staging takes the latest green, prod takes only what
soaked green in staging and cleared the human gate, and the end-to-end test
proves prod never carries the red commit. Fencing was shown identical in
both lanes by preempting a lease mid-run between build and land.

Phases 10–12 are COMPLETE (2026-08-31, same day as 9):

  10  the work graph — DependsOn enforced at Claim; a pending dependency
      blocks, a dead one cascades with the cause named leaf by leaf;
      topological order emerges from claims under three racing runners.
  11  standing operation — intake turns ingresses into items idempotently,
      the pump drains within a recorded capacity budget (work waits, spend
      and refusals are evidence), the daemon ticks it all, and cmd/boxd is
      the heartbeat as a binary. TestStepUpIngressAndStepBack is the
      contract by name: three issues opened, nothing else touched, three
      commits landed.
  12  seat continuity — seats hand off on EVERY settle (a parked run's
      "where I got stuck" most of all), wake with their notes as a resolved
      inline layer (continuity is part of the agentref: different memories
      are different agents on the record), and hold a private knowledge
      scope. Refusal is a recorded act: the _box/escalate ACP verb is always
      acknowledged, and the loop parks with the reason attributed to the
      seat. The measured continuity-vs-control comparison awaits live
      agents; the plumbing it needs is what shipped here.

An audit pass also closed three gaps between claimed and asserted:
OnBehalfOf threading, authz enforcement IN the pipeline (delegation now
intersects at propose and land, not only in the evaluator's tests), and the
template-change-to-campaign-work composition.

The ACP builder adapter is OPERATIONAL and live-verified (2026-08-31):
acp.Builder puts a real agent in the loop's Builder seat — bundle resolved
(wake layer included), agentref on the record BEFORE the run, context
materialized as a read-only sibling of the workspace, permission mode set
through the protocol's session/set_mode (fail closed: an agent that refuses
the mode gets no prompt), the working contract enforced (commit or
escalate; a run that changed nothing is an error, never a landing — the
adapter commits leftover work but refuses declared victory over an
untouched tree), and HANDOFF/ESCALATE both honored through the verb and a
text fallback. Proven by the fake-agent suite (worker, lazy, escalate,
rogue) and by a LIVE run: claude-agent-acp 0.70.0 landed a real commit
through the whole loop in 12 seconds, and its unprompted handoff correctly
described the factory's own materialization model. cmd/boxd -agent runs
real agents on the daemon; the gated live test (BOX_ACP_LIVE) never runs in
CI — it spends a real session.

The daemon has its interface (2026-08-31, refined same day): internal/api
serves one HTTP+JSON surface — status, exceptions, items, provenance,
activity, an SSE event stream, POST /v1/ingress as the delegation verb,
stop/resume — and the daemon is SOCKET-ONLY by design (the dockerd
posture): its whole trust model is the unix socket's file mode, and it
cannot be given a network listener at all. Everything network-shaped is a
LAYER over the socket: internal/gateway + cmd/boxgate carry the network
listener and the bearer-token/cookie check (refusing a tokenless gate
off-loopback), `box room` fronts it tokenless on 127.0.0.1 for the local
browser, and OIDC/mTLS/IAP replace the gate without the daemon changing.
Seams multiply; the daemon never duplicates them. Proven by rerunning the
IDENTICAL browser smoke through the layered shape — same script, same UX,
daemon never network-reachable. The API writes intent and never executes: the pump remains the only
path to work, so network callers get identical stop/budget/fencing
semantics. Routes are server configuration — a caller names a route, never
defines one. internal/api.Client is the typed Go client; cmd/box -daemon
uses it for status/submit/item/provenance/stop/resume. Live-smoked: a
running boxd drove an ingress to a landed commit purely over its socket,
and the smoke caught a real forge bug (relative mirror roots resolved from
the mirror's cwd — NewMirror now demands absolute).

box.yaml is in (internal/boxcfg + boxd -config): one daemon serves many
boxes and many repositories in ONE tenant — the normal shape, not a special
one. Routes carry their target repo (operator configuration, inherited by
the caller, never chosen in the request); items carry it to the pump, which
picks the most specific executor (box/repo, then box). Repositories share
the tenant, store, mirror set, and budget; they share no leases and no
workspaces. Proven by TestTwoReposOneTenantSideBySide (both repos in flight
in one drain under MaxConcurrent 2, distinct leases) and by a live smoke:
one box.yaml, two repos, two submits over one socket, both remotes advanced
by the daemon's own ticks. Config validation is opinionated: unknown lanes,
env-optimistic combinations, duplicate route names, and a repo with neither
agent nor build command are all refused at load.

The web control room is in (web/: Lit + @lit-labs/router [Labs] + the Jack
Henry Design System, esbuild-bundled, dist committed and go:embed-served by
boxd itself — one binary, UI and API on one origin). Dashboard with live
SSE activity, exceptions, item + provenance views, and Submit as the
delegation verb in form shape. Component APIs taken from jh-ui's shipped
custom-elements.json, never invented. The UI shell serves UNAUTHENTICATED
(embedded public code; every datum rides /v1, which stays authed) and a
valid ?token= mints the cookie server-side — no JS bootstrap race, and the
whole scheme is the one seam OIDC replaces.

Config hot-reload is CONFIRMED on a running server: internal/daemon
assembles from box.yaml and reassembles on SIGHUP or POST /v1/reload —
executors, routes, budget, and tick interval swap under locks; in-flight
runs finish on the config they started with; tenant/db/root/listen are the
daemon's IDENTITY and a reload touching them is refused with the old config
left running; every reload is evidence. Proven in Go tests (add-a-repo
hot; identity and broken configs refused) and END TO END IN A BROWSER:
Playwright drove the real UI — submit through the JH form, the landing
arriving on the live feed, then a reload on the running process and the
new route appearing in the form with no restart.

A dockerd-hygiene pass (2026-08-31) pulled the operational lessons worth
having before a fleet exists: build identity everywhere (internal/version,
ldflags-stamped, --version on all three binaries and in /v1/status — a node
can say what it runs); typed API error codes adopted before any client
could calcify on prose (match Code, never Message); graceful drain on
SIGTERM (claims stop instantly, in-flight runs get the configured drain
window to settle — verified live: TERM mid-build, 4s shutdown, the commit
landed); the prune sweep as a standing daemon task; and `box context`
(named daemon endpoints, docker-context shape, 0600 config). Pulled-later,
recorded: the shim split / live-restore (only when agent runs get long —
re-run-under-fencing is honest today), and container-sandboxed builders as
the runner-confinement answer, with docker now on the box to provide it.
Rejected: API version negotiation, labels/filters (estate facts are our
labels), the plugin wire protocol (Go interfaces + exec seams), and the
registry model (git is our distribution).

The first dogfood run (2026-09-01) pointed a box at project-box and cost a
morning's budget almost immediately. What it found, and what was done:

  cost   The loop rebuilt on every tick a gate answered "pending" — routine
         review latency was the most expensive thing the factory could do,
         bounded only by a meter that counted run STARTS. Work items now
         carry a resume point (Built/BuiltBase) and the builder is skipped
         while the branch still stands where the commit was written; every
         settle drops it, Wait alone keeps it.
  cost   Nothing measured tokens at all — agent-result carried wall_ms and
         two counts, and the runtime dropped everything on the stream it
         did not already parse. Usage now rides the Result and is charged
         to the meter by the adapter, which is the only place that can see
         it. Budget gains max_tokens_per_window; a run that reports no cost
         is recorded unmetered and, under a token budget, stops claims
         until the window turns. Silence is never zero.
  #18    The branch lease was held across the whole run, so three
         concurrent runs produced one: the meter allowed three and the
         lease admitted one. The build runs under the WORKSPACE's lease
         now; the branch is taken for the length of a landing. A loser
         rebases and re-gates rather than rebuilding, or narrowing the
         lease would have bought wall-clock with tokens.
  #20    `pending=14` was the whole view into the queue. Room.Queue folds
         state, age, origin and — the part a counter cannot carry — the
         note the queue always wrote and nothing ever showed. GET
         /v1/items?state= and `box queue`.
  #9     The ACP dialect is asserted at initialize and the known update
         kinds are an explicit set, so drift is a recorded fact rather than
         a metric that quietly reads zero. Tool calls dedupe by call id, so
         the tool_call_update spelling counts too.
  repair One repair per red sha bounds the fan, not the chain: each repair
         makes a new sha that can be red. Work carries a depth, three deep
         by default.
  #6     Partial. `agent:` takes an argv and substitutes {{workspace}}, so
         a confining wrapper can be configured at all — the seam, not the
         confinement. The profile itself is unwritten and needs a live run.
         (Closed below.)

Two bugs surfaced underneath, both older than the changes that exposed
them: concurrent runs racing `git remote update` on one mirror directory
(updates now serialize per repo), and queue.Load unmarshalling each record
over the last, so omitempty meant a transition could never CLEAR a field —
a settled item kept the note it parked with.

The dogfood harness is on disk now (hack/dogfood), which it was not for the
rounds that produced everything above. box.yaml, the runner, the control
room, and the issue submitter lived in the gitignored .factory/, so the most
consequential live leg in the project was the only one that could not be
recreated from nothing — and it carried facts held nowhere else.

The box's shape is issue #7 read literally: one repo, one seat,
MaxConcurrent 1, the gated lane, landing on refs/heads/factory and never on
main. TestDogfoodBoxIsNarrow holds it there, because two halves of that
posture are DEFAULTS POINTING THE OTHER WAY — boxcfg resolves an unstated
lane to optimistic and an unstated branch to main, so deleting two lines
would quietly turn a gated factory into one that lands unjudged on the
branch a human is working in. Widening the dogfood is a test failure rather
than an omission, which is the only form in which "start narrow" survives a
config file.

The budget is 6M tokens an hour, reasoned in the file from a measured
918,520 tokens ($1.61) for one issue, with allow_unmetered deliberately
absent. And run.sh carries the workaround for the credential-helper hole
recorded in a96320e: GIT_CONFIG_COUNT is the one config channel that
survives the hermetic GIT_CONFIG_GLOBAL=/dev/null, and without it the forge
cannot reach a private HTTPS remote on any machine. The workaround is on
the record; the seam it works around is still missing.

submit.py carries the working contract every agent run in this repository
has executed under — read the docs, change precisely, escalate rather than
invent. It rides the task text, so it is neither part of the agentref nor
governed as a layer: exactly the "inline material nobody reviewed" case
ARCHITECTURE names under "Text first, git for governance", and now at least
visible. Promoting it to a governed base layer is the fix, not this.

Issue #4 closed the other half of parking (2026-09-01). The fold that showed
only what still needs eyes was good and was where the design stopped:
exceptions did not age, had no severity, and never left the room, and an
exception nobody reads is identical to no exception. Now a park states a
severity (a Park parameter, not a field — a stage cannot forget an argument;
unstated resolves to medium, never to the quiet end, and a cascade inherits
its cause's), exceptions carry an age and sort loudest-then-oldest,
`control.Escalator` tells somebody out of band once the age passes the
objective and once per objective after that up to a bound, and the notifier
is a seam — `LogNotice` is the daemon's default because the alternative
default is nothing. The objective is one number doing two jobs: the age a
queue is measured against IS the age at which it should be shouting.
`ExceptionHealth` puts the oldest parked item on /v1/status and in the room,
because a length says how much there is to do and only the oldest says
whether anything is being done. Every telling is recorded before it is sent —
the count is the bound, so an unrecordable escalation is an unsent one. The
sweep is a standing daemon chore beside the prune.

Issue #6 closed the runner-confinement half (2026-09-01). The seam had an
argv and no profile, which is the state where a security posture is
configurable and therefore assumed. internal/agent/confine renders the
wrapper — the workspace mounted writable, the materialized context set
mounted read-only, no third path — and `confine:` on a Box folds it into the
agent argv at load, so the daemon still knows exactly one way to start an
agent. Three details are load-bearing and each is a test: `--mount` rather
than `-v`, because -v CREATES a missing source and turns a mistyped path
into an empty workspace nobody can land; `--user` defaulting to the daemon's
own, because docker's default is root and a tree of root-owned files is a
landing that fails after the tokens are spent; and a second placeholder,
{{context}}, because a wrapper told only about the workspace confines an
agent out of its own granted reading material — the context directory now
exists for every run, empty when nothing was granted, so the argv is never
conditional on the bundle.

The property is asserted the way the finding was found: the SAME probe run
twice against one real layout. Unconfined it reads the mirror, the store,
and a sibling run's workspace — finding 5b reproduced, not argued —
and confined it reads its workspace and its context set and nothing else,
cannot chmod its way past the read-only mount, and leaves work the daemon
can still commit. Docker-gated like the collector and emulator legs: skipped
locally, FAILING in CI, because a confinement test that skips reads as a
confinement that holds.

What is NOT closed, and is recorded rather than implied: the network (an
agent that cannot reach a model does nothing, so egress is a separate seam),
time (`docker run` killed by its parent leaves the container running, so
build_timeout fires on the wrapper while the agent inside keeps spending —
closing that wants a named container and a reaper on the prune sweep), and
the image itself. The dogfood box is deliberately still unconfined: an image
carrying claude-agent-acp and its credential is an operational prerequisite
this change does not build, and turning it on is the live run #6 always
needed.

Issue #8 asked whether a new loop stage is config or code, and the answer is
recorded in ARCHITECTURE under "A new stage is a gate, a dependent item, or a
change to the transaction" (2026-09-01). The short of it: Gas City's lesson
binds on the axis it was learned on, and that axis — agent roles — is already
config here as layers. `Run` does not hold roles; it holds one unit of work's
transaction. Judgement over a candidate is a `Gate`, work that makes its own
change is a node in the work graph, and the sequence between them is one
claim, one lease, one resume point and one meter slot, all of them singular
because the unit is. TestCritiqueIsADependentItemNotANewStage is the worked
example: a propose/critique pair as two graph nodes under two roles, the
critic refused before it can spend while its dependency is unsettled, each
item holding its own claim, lease, evidence scope and settled record, and
`Run` not touched. The follow-on this names rather than closes: nothing
outside a test CREATES a dependent item, and an item cannot name the role it
runs under — the pump routes by box and repo, and a loop's builder carries
one role — so the extension point is real in the queue and unreachable from
box.yaml. A producer and a routing key, and no evidence is missing meanwhile,
which is why it is not a §2 retrofit.

Issue #9 closed the rest of the dialect problem (2026-09-01). The earlier
round taught the runtime v0.70's shapes and made drift a recorded fact; what
was still missing is the part that keeps it true tomorrow. The dialect is now
PINNED as data — testdata/claude-agent-acp-0.70.jsonl is a whole session's
frames in wire order, and conformance_test.go replays it at the runtime and
asserts what the parser made of each one. Teaching a newer dialect is a diff
of frames rather than an edit to a hand-written fake, which is the difference
between a pin and a habit. The pin covers the conversation and not only the
parsing: each client frame must be the call the client actually makes there,
so a runtime that stopped setting the permission mode fails it. And every
counter is asserted twice — against its exact value, and against zero with
the shape feeding it named — because a metric that CAN silently read zero is
the failure, not the particular wrong number. Verified by breaking it three
ways: rename a usage field, drop the tool_call_update spelling (finding 5a
itself, now a test failure), skip set_mode.

`Runtime.Transcript` records both directions, and `Builder.Transcript`
carries it to the only path a real agent runs through, so BOX_ACP_TRANSCRIPT
on the live smoke re-records the pin from an actual agent — a pin nobody can
re-record rots into fiction, which is the failure it exists to catch.
TestTheRecorderProducesAReplayableTranscript holds the recorder to that job:
replay the pin while recording, replay the recording, same Result.

Stated rather than implied: the committed transcript is ASSEMBLED, not
captured — every shape in it is one this repository already paid to learn
(the usage field names from the adapter's own sessionUsage(), the untitled
tool_call_update naming its tool under _meta.claudeCode.toolName from 232
measured calls, no _box/escalate frame because 0.70 cannot call unknown
methods), and testdata/README.md says so and says how to replace it. The
thin frame is session/request_permission: its title has never been observed
in a real recording, and if it turns out to be prose the way tool_call
titles were, the enforcement path has the hole the update path had. Not
built, and named here: the cross-run version of the zero smell — a counter
that has never been non-zero in ANY run should be loud — which lives in the
capacity and evidence planes, not in the parser.

Issue #11 gave repair work the attempt it follows (2026-09-01). A red landing
opened a fresh ingress carrying the failure reason as its payload, and the
repair was then an ordinary work item — a task string and nothing else — so
its agent started cold and re-derived from the diff what the run before it
already knew. A repair now carries an `agent.Prior`: the run that made the
attempt, the red sha, the branch that still holds the change (nothing rolled
back), and which check said what. It rides the ingress the way the depth
does, and `ItemFor` puts it on the item intake builds.

The predecessor's handoff is deliberately NOT frozen in at that moment,
because it does not exist yet — notes are written as a run settles, and on
the optimistic lane the repair is summoned before the run summoning it has
settled. They resolve at WAKE time instead, from the seat that wrote them:
`seat.HandoffFrom` asks by run rather than by recency, which is the
successor's question and the one the wake layer cannot answer. The whole
thing renders as a task-scope layer beside the task text rather than being
concatenated into it, so continuity's rule holds here too — the carried
context is part of the agentref, and a repair woken with its predecessor's
account is a different agent on the record from the same repair woken cold.
Two limits, stated rather than implied. A seat's memory is its own, so a
successor sitting in a different seat carries the facts and not the notes:
reading another seat's scope would be exactly the cross-scope read the
knowledge plane refuses everywhere else, and it would want a grant to be
anything but that. And nothing in box.yaml wires `Red` at all yet — the
red-main route is test-only configuration, so an optimistic Box today lands,
checks, records the red, and summons nobody. That is the same unrun second
lane #7 names, and it is what a live optimistic session would close.

Issue #12 gave a session a way to end that is not finishing, parking or
failing (2026-09-01). `HANDOFF:` lines were read off the agent's FINAL
message, so a handoff could only ever be a parting note, and an agent out of
context mid-change had two moves that both lied: escalate, which parks work
needing no person and throws away what the session built, or push on. The
third is `_box/handoff` beside `_box/escalate` — `{"reason", "notes"}`,
acknowledged always for the reason escalate is, with a `HANDOFF-NOW:` text
fallback because 0.70 cannot call an unknown method and a verb with no text
channel is a verb the only real agent here cannot reach. The two spellings are
one word apart on purpose: `HANDOFF:` is still a parting note, and the
difference between them is whether a fresh session opens.

`StateHandedOff` is the third non-settled state, and the reasoning is the
queue's own about waiting, used a third time — a handed-off item needs nobody,
so filing it as an exception would bury the ones that do. Not pending either:
pending says nobody has started this, and an item three sessions deep has been
started three times. `Drain` claims it like any other claimable state, which
is where "re-dispatch" is actually decided; the same executor takes it, and a
repo has one builder, which is what "the same seat" amounts to.

The successor CONTINUES. The tree is held and what the session committed is
kept as `Unfinished` — a resume point distinguished from `Built` by one
boolean, because an ordinary resume SKIPS the builder and doing that to half a
change would gate and land exactly the degraded commit the verb exists to
prevent. The notes go to the seat at the hop rather than at settle, since the
item does not settle and a note deferred to settle is one the successor wakes
without. Asserted the way it matters: the landed history carries both
sessions' commits, so a successor that started over fails the test rather than
producing a plausible-looking commit; the two sessions' agentrefs differ,
because a run woken with a different memory is a different agent on the
record.

Hops are bounded — `Loop.MaxHandoffs`, three by default, negative for none,
the posture the repair depth already takes — and each is an evidence record
attributed to the seat. Exhausting the limit parks with the reason named: an
item that has taken four sessions is a sizing problem rather than a context
problem, which is a judgement, and judgements are what the exception queue is
for.

The working contract names the verb, because a verb no agent is told about is
a verb no agent uses. It rides the prompt rather than the bundle, so it
inherits submit.py's problem exactly — ungoverned text that is not part of the
agentref — and promoting the contract to a base layer remains the fix for
both.

Every reader of the state vocabulary learned the word in the same pass, which
is the failure mode that vocabulary has already had once: `/v1/status` counts
it as in flight, the room derives a stage from it, and the control room's
`live` filter lists it. A state that some readers know is worse than no state,
because work does not disappear loudly — it disappears plausibly.

Issue #14 asked for the work-graph frontier and found half of it already
shipped: #20's `GET /v1/items` and `Room.Queue` are the collection endpoint the
issue says does not exist, and `blocked_by` was already on every row. What was
missing is the part a state cannot carry. A row now says whether it is READY —
claimable now, nothing unsettled ahead of it, nobody running it — and readiness
is item-local on purpose: the stop button and the budget stay out of it,
because ready work that is not moving is the symptom and folding the cause in
would erase it exactly when it fires. A cascaded row names the ROOT, which the
note could not: `Claim` writes the cause as prose nesting its own dependency's
prose, so three deep it reads "dependency B parked: dependency A parked: the
credential was revoked" — the origin is in there and is not a field, and the
graph question an operator has ("what is this one failure holding up") has no
answer while it stays a string. And a claimed row names the RUNNER holding it
and how much lease is left,
negative once it has lapsed — the one row fact that is not a fold, because an
item's chain records that a runner took it and can never record that the runner
died. Reading it put one read on the store's lease seam: `forge.Leaser.Inspect`,
conformance-asserted on both backends — it reports the holder, does not disturb
the claim, leaks nothing across tenants, and deliberately still names a holder
past the deadline that `Acquire` correctly treats as free, because "runner-3 has
held this for forty minutes on a twenty-minute lease" is the fact and not the
contradiction it looks like.

The room's Work view is now a TABLE, with sortable headers, `aria-sort`, a
`blocked by` column and `ready`/`blocked` lanes — no design-system table exists
and none was added, because a semantic `<table>` on the JH tokens is also the
zero-dependency answer. `box queue` learned the same three facts on the same
day: a fact some readers of the queue have and others do not is worse than one
nobody has, which is the failure the state vocabulary already had once.

The diagram is deliberately NOT built, and the decision it was blocked on is
recorded instead (ARCHITECTURE, "A queue that cannot say what is runnable"):
layout is computed in Go and shipped as coordinates, the table stays the
representation of record, and #15 and #16 inherit both rather than each
answering it again. Nothing here adds a Go layout dependency either — the
decision binds the diagram, and the diagram is the next issue's.

Issue #17 put a data envelope on the work (2026-09-01): what class of data an
origination's work may touch, and which endpoints may see it. The motivating
case is an embargoed vulnerability finding and a published CVE arriving as the
same sentence and routing to the same builder, with nothing in the system able
to treat them differently — and the decision that matters made the moment the
description reaches a model, whoever reviews the pull request afterwards. The
reasoning is in ARCHITECTURE under "And a data envelope"; the mechanism is
`agent.Envelope`, an input to bundle resolution that can only narrow it.

Five fields, and the taxonomy is deliberately not one of them: an operator
names their own classes, because the same finding is material non-public
information in one estate and an ordinary ticket in another. What the mechanism
supplies is the intersection and the two refusals. Class conflict with the
ceiling refuses; endpoints and readable classes intersect; redaction and
payload take the stricter of the two, which is the LOWER rank because both
orders run strictest-first so that a zero value is the strict end. `Check` runs
against the COMPOSED bundle rather than the layers, because widening is what a
layer attempts and the merged result is the only place an attempt shows.

Absence means minimum on every field, which is PLAN §5's rule a third time. A
composer with a ceiling and work with no declaration refuses — asserted through
the whole loop, where it parks before a token is spent and the remote never
moves. An unstated payload posture sends a reference and not the text, so a
reference-only class refuses a request carrying the ingress text as its task:
the substitute ("TestParseHeader fails, make it pass") is a judgement about the
work that a composer cannot make from the text it was handed, so it refuses
rather than inventing one. An unnamed model is a refusal too — "whatever the
runtime was configured with" is not an answer to which endpoint saw the data —
and an MCP server is an endpoint, because an endpoint here is where the payload
goes rather than what answers. A tool is not: allows already intersect across
layers and a narrower layer can only narrow, so that property already held.

The record came almost for free, which was the argument for doing it in the
digest rather than beside it. `resolveDomain` moves to v2 (the envelope and each
context repo's class now serialize into the ref), so a run permitted to see one
class and reach one endpoint is a different agent from the same layers under a
wider envelope, and the `agent-run` record carries the envelope in force in
full rather than a class name — otherwise answering "which endpoint" would mean
re-resolving the bundle.

Three things are NOT built and are recorded rather than implied. The redaction
posture is declared and the telemetry path does not read it. The second
enforcement point the issue names — the stream, where tool policy already binds
— has nothing envelope-shaped in it. And the declaration has no governed home:
`Builder.Envelope` is the seam, nothing in box.yaml fills it, and it belongs on
#13's kind declaration as the fifth field. That is the whole of what #17 asked
be captured before that declaration is designed, and the §2 argument is why it
could not wait — a run that executed before classification existed is
unclassifiable retroactively, and there is no query that recovers what class of
data it was permitted to see.

Issue #19 opened the claimed item (2026-09-01). #14 answered which work COULD
be moving; a claimed unit still said `state=claimed head=` for the length of a
run, and the reason was never a shortage of facts — the stage bracket, the
composed layers, the seat, and every word the agent said were all inside the
daemon and none of them left it. Three things closed that, and only the middle
one is new machinery.

The FAN-IN: `acp.Runtime.Progress` reports the stream as it arrives —
messages coalesced at the agent's actions (a frame per chunk is a log dump, a
frame at the end is no live view), each distinct tool call, each refusal, and
both box verbs with their reasons. `Builder.Progress` tags them with the work
item. It comes off the SAME dedup the settled tally does, which is the
property that makes it worth trusting: the tool calls a watcher saw are
exactly the ones the run is charged with, and the message frames concatenate
to `res.Text` — asserted against the PINNED dialect, so a shape that moves
breaks the live view here rather than quietly emptying it in production.

The BUS: `control.Live`, in memory, bounded, one global cursor. The decision
and its reasoning are in ARCHITECTURE under "A claimed item is a black box
until it settles" — the live stream is a window and not a record, because the
per-call sequence is already on span events, the tally is already on the
chain, and a WORM-exported record per tool call buys an auditor nothing. What
that costs is stated: a restart loses the window, which is survivable exactly
because the run it was watching was that daemon's child.

The VIEW: `Room.Sessions` folds identity, progress and timeline into one card
per RUNNING unit — claimed and nothing else, because a live view that quietly
includes work nobody is doing is the stale-stage bug in a bigger frame. `GET
/v1/sessions`, `GET /v1/items/{id}/session`, and `GET /v1/events?item=` as the
per-item scope (a scope and not a filter: the server stops reading what the
subscriber would only discard). The room grows a Sessions page of cards side
by side; `box item` prints the same card, because a fact one reader of the
queue has and another does not is worse than one nobody has — the argument
`box queue` settled for the frontier, used again.

Two smaller things the view forced. The composed LAYERS now ride the
`agent-run` record in application order: the room wanted to show what the
agent was and the agentref is a digest, so the alternative was re-resolving
the bundle from the same material at the same commits. And an empty timeline
now reports WHICH kind of empty it is — a run announces itself to the bus at
its first instant, so silence from a unit this daemon is running is an idle
agent and silence from one it is not is blindness, and a reader left to infer
infers the alarming one.

The issue also names the v0.70 tool-call dialect as undercounted; that half
was closed by #9 and this change inherits the pin rather than re-fixing it —
the new assertions replay the same transcript.

NOT done, recorded rather than implied. The post-land check is bracketed by
`telemetry.Stage` and reaches the item's chain as an ordinary `gate` record,
so the room's stage column reads "gating" for it and there is no "landing"
stage at all — the Lander writes to its own scope. Closing that is a chain
vocabulary change with every reader of `stageOf` on the other end, and it is
the next thing this view will ask for. The browser pass is owed: the room
smoke grows a Sessions step, and it is UNRUN in this workspace because a
sandboxed `bind()` on a unix socket is denied and boxd cannot start at all.
And a timeline with real agent frames in a browser needs the dogfood box with
a live ACP session, which is the same live run #6 and #7 are already waiting
on.

Issue #20's design note had a second half, and it closed last (2026-09-01).
The listing, the CLI verb and the room's table all shipped in the rounds above
— #20's own, and then #14's frontier on the same rows — but the COUNTS on
`/v1/status` stayed exactly what the issue opened by complaining about: a walk
of `index/pending` with its own copy of the vocabulary for "not settled yet".
Nothing held them to the listing. They agreed because two switch statements
matched, which is agreement with no mechanism behind it, and this system's
state vocabulary has drifted that way once already — a word some readers
learned and others did not, where work does not disappear loudly but
plausibly.

`Room.Backlog` is that count as a tally of `Room.Queue`, and `queue.Claimable`
is now the one place the vocabulary is written: the pump selects with it, the
pump decides what settled with it, and the room's pending number IS it.
`/v1/status` carries the parts beside the roll-up, so "pending: 14" says which
fourteen states it is made of; `box status` prints the same line and the
dashboard the same breakdown with a link to the verb that lists them, because
a fact one reader of the queue has and another does not is worse than one
nobody has — the argument `box queue` settled for the frontier, used a third
time. Asserted the way it can break: state by state against the listing
narrowed to that state, at intake, MID-DRAIN and settled, since a counter that
matches while everything is pending and drifts once work moves is the whole
bug. The cost is stated rather than implied: status folds the queue where it
walked an index, which is what `ExceptionHealth` beside it already does.

Still unrun from #7: the second lane. Everything above is the gated lane
against the real remote; the optimistic lane on a scratch mirror needs no
code (Phase 9 shipped it, and boxcfg's default IS optimistic) — it needs a
scratch remote and a deliberate session.

The harness surface is DECLARED and delivered (2026-09-01). Phase 2 resolved
`Bundle.Skills` and `Bundle.MCP`, merged them, hashed them into the agentref
and rendered them in Explain — and delivered them nowhere: `session/new` went
out with an empty `mcpServers` and no options at all. A bundle could grant a
skill and the agent would never see it. Meanwhile the adapter's own default
loaded the machine's ENTIRE harness configuration, so the factory recorded a
set it did not deliver and delivered a set it did not record. Probed on this
box: 44 plugins, 79 skills, three hook sets firing on every tool call, and
every MCP server the machine holds — Slack, Jira, GitHub, Datadog, Honeycomb,
Wrike — reaching an unattended builder that no box had granted anything.

What closes it:

  - `boxcfg.Harness` — `plugins: user|none`, `plugin_mcp`, skill refs, MCP
    grants. The inheritance is now a decision instead of an accident.
  - `internal/harness` — discovery reads the same two files the harness reads
    (`settings.json`, `installed_plugins.json`), user scope only, PER RUN. A
    plugin enabled in the harness reaches the next build with no reload, and
    the set is in the bundle either way: inheriting is not not knowing. Its
    `Source` is the `LayerSource` the daemon composes with.
  - Skills by ref — repo, commit, path, resolved through the mirror to the
    TREE object at that path, which is why a sibling skill's revision does
    not churn this one's digest. A full 40-char commit is required at load.
    Materialized as a generated plugin beside the workspace, read-only.
  - `strictMcpConfig` unconditional at `session/new`, `skipMcpDiscovery` on
    every plugin. Two levers, not one: what the loader reads, and what the
    session may connect to.
  - `git.RemoveTree` — Prune could never reclaim a materialized tree.
    `makeReadOnly` seals it 0555 and `os.RemoveAll` cannot unlink inside
    that, so `.context` siblings had been accumulating one per dead run
    since Phase 2, invisibly, because nothing ever failed. Found by a test's
    own TempDir cleanup.

`TestLiveHarnessResolves` (BOX_HARNESS_LIVE=<box.yaml>) resolves a shipped
config against the real network and materializes it, which is the failure a
fixture cannot catch: a force-pushed commit, a path that moved between
releases, a private remote the credential channel cannot reach.

Issue #5 turned an invariant into a test (2026-09-03). The rule is stolen
verbatim from Gas Town's dog-execution model and stated now in ARCHITECTURE:
reliability-critical paths are imperative Go, and an agent occupies only a
seat whose failure parks work. We were already on the right side of it, which
is exactly why it needed asserting — an agent-shaped dependency on a critical
path does not fall over, it reads as QUIET, and their doctor, reaper,
compactor and both backup dogs were silently broken for months on that
distinction. `TestTheFactoryRunsWithNoAgentInIt` configures zero agents and
no runtime and requires the whole factory anyway: a change drained to a
landing, a refused gate parked, that exception aged and told out of band, the
budget refusing, every scope verifying, and both standing chores completing.
The half that will actually catch the erosion is structural — no ACP builder
is assembled at all — because the moment worth failing at is the commit that
moves a chore onto the agent plane, not the quarter somebody notices it
stopped. It also fixes #13's decomposer constraint in place before there is a
decomposer to tempt anyone: a stage on every intake is a stage on the
critical path of every intake.

Phases 7 and 8 remain blocked on external resources. Next code-shaped work:
an agent image and the live run that turns confinement on for the dogfood,
review sampling/sign-off endpoints for the control room, and OIDC as a
boxgate variant.
