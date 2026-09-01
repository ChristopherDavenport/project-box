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
- `internal/evidence` — `Append`/`Head`/`Since`/`BySubject`/`Verify`.
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
| Delegation cannot escalate | an agent acting for a user is denied what that user lacks | 0 |
| No implicit allow | a principal with an empty grant set can do nothing | 0 |
| Human vs automation | evidence distinguishes actor from on-behalf-of | 3 |
| Agent determinism | same request + same material → same `agentref` | 2 |
| Tool policy | a denied tool is denied at the stream, by a test that tries it | 2 |
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
| Preemption | incident revokes a campaign lease; preempted land is rejected | 5 |
| Break-glass gating | refused with no open incident | 5 |
| Stale approval | approval of a superseded revision does not count | 5 |
| Self-approval | requester cannot approve, directly or via an agent acting for them | 5 |
| Knowledge staleness | a record bound to a changed file is flagged without marking | 6 |
| Evidence completeness | 25 sampled changes, complete evidence, unaided | 8 |
| Optimistic lane safety | red main opens repair work; promotion refuses unsoaked commits; fencing identical in both lanes | 9 |
| Graph ordering | diamond dependency runs in topo order; failed root parks leaves with cause | 10 |
| Budget refusal | work waits rather than overdrawing a tenant budget | 11 |
| Seat continuity | second run receives predecessor's handoff; escalation parks with agent-attributed reason | 12 |

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
| Landing discipline is per-lane policy | optimistic into main where a Box chooses; env/infra and promotion always gated; fencing never optimistic |
| A lower environment between main and prod | red main is allowed, red prod is not; promotion requires soak plus green |
| Thunderdome adopted as a lane, not a stance | swarm-repair needs the attribution and reversibility this substrate provides |

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

Phases 7 and 8 remain blocked on external resources. Next code-shaped work:
container-sandboxed builders (runner confinement), review sampling/sign-off
endpoints for the control room, and OIDC as a boxgate variant.
