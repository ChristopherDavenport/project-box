# project-box — architecture

A control plane for **dark software factories**: systems built, verified, and
deployed end to end by agents, with no human in the execution loop.

"Dark" describes the loop, not the building. Lights-out manufacturing still has
a control room, an andon cord, and an exception queue — and under SOC 2 it needs
a documented reviewer as well. What is absent is a human step *between* intent
and running software.

## Three constraints shape everything below

**SOC 2 is a first-class requirement, not a later concern.** A Type II report
tests whether controls *operated effectively* across an observation window, and
an audit period cannot be backfilled: whatever evidence design exists when the
clock starts is what can be produced. Evidence generation is therefore part of
the loop, not instrumentation bolted to it.

**The whole model must run locally.** Every plane has a local implementation —
SQLite, the filesystem, a local git remote, a kind cluster — so the factory can
be proven end to end before a Spanner instance is provisioned. Managed
infrastructure is a deployment choice, never a prerequisite for development.

**Multi-tenancy is structural, not a column.** Nothing here is harder to add
later, so it is present from the first commit — and single-tenant operation is
simply multi-tenant with one tenant, which costs a constant field and nothing
else.

*(Criteria references below are indicative. Controls are agreed with whoever
runs the engagement; this document records the architecture that makes them
evidenceable.)*

## Tenancy

A tenant owns Boxes, agents, and evidence. It is the outermost scope of every
key and every authority grant.

The familiar failure is a `tenant_id` retrofitted onto an existing schema, where
isolation depends on every query remembering to filter and the one that forgets
is a cross-tenant leak no type or test catches. Three rules avoid it, and the
first two are what make the third testable.

**Tenant leads every primary key.** In Spanner that is isolation and locality at
once: a tenant's rows occupy a contiguous key range, so one busy tenant cannot
hotspot another, and a query that crosses tenants is wrong at the schema rather
than subtly wrong in a predicate. It also changes the hotspot analysis for the
better — write distribution is now per-tenant by construction.

**An unscoped handle does not exist.** Storage is reached only through a handle
obtained by naming a tenant, so a caller cannot forget the scope because there
is no API through which to forget it. In particular the tenant is never derived
by looking up a Box or a repository: an identifier that must be resolved before
it can be trusted is one an attacker can supply. That is why `ChangeSet`,
`Lease`, `Record`, `Bundle`, and `Ingress` all carry the tenant explicitly
alongside the Box rather than implying it.

**Non-interference is asserted, not assumed.** The conformance suite writes as
one tenant and reads as another for every operation and expects nothing back.
Buzz specifies this property formally as `Inv_NonInterference` in
`MultiTenantRelay.tla` — worth knowing they found it load-bearing enough to
model-check. This is the mechanized version of the same claim.

### Isolation is a migration path

| Level | What is shared | When |
|---|---|---|
| logical | one database, separated by key prefix | the default |
| database | instance shared, database per tenant | when separation must not rest on code correctness — also where a customer-managed encryption key becomes possible |
| instance | nothing | full separation, full cost |

Every tenant starts logical. The `Router` is what keeps raising a tenant's level
a routing change rather than a rewrite: where a handle points is decided in one
place, and callers never learn which level they are on.

### Evidence chains never span tenants

Which means one tenant's auditor can be handed a complete, verifiable chain
containing nothing of anyone else's, and retention, export, and deletion become
per-tenant operations rather than filtered ones. If a right-to-delete
obligation ever lands, it is tractable instead of archaeological.

### Deferred

Tenant lifecycle, per-tenant quotas, noisy-neighbour throttles, and cross-tenant
administration. These are the parts that genuinely add complexity, and none of
them is hard to add later — which is exactly why they are the parts that wait.

## The unit: a Box

A Box is a **system**, not a repository. Real platform work crosses repository
boundaries — bump a contract, regenerate a client, update a chart, promote an
image — so a repository is a *member* of the unit, never the unit itself.

```
Box
 ├── repos[]   service | library | contract | chart | infra | env
 ├── envs[]    name → cluster, namespace, env-repo path, promotion policy
 ├── agents[]  identities bound to this Box, with role-scoped authority
 └── policy    gates, blast radius, break-glass procedure
```

## The loop

```
ingress → WorkItem → lease → agent run → ChangeSet → gates → land → deploy → verify
                       └──────────── exception queue ────────────┘
```

Work enters through an **ingress** and never otherwise. Every stage may **park**
its WorkItem into the exception queue rather than proceed; a factory that cannot
stop is not automated, it is unsupervised.

## The estate

A Box *enrols* repositories, which is a declaration. The estate *observes* them,
which is a fact, and the two are kept as separate records so they can disagree
visibly.

The distinction matters most for campaigns. A campaign is a predicate over
repositories, so a hand-maintained list quietly turns "every service is on Go
1.25" into "every service someone remembered to enrol" — and that gap never
announces itself. Two queries keep it honest:

- **Unenrolled** — observed but in no Box. Outside every campaign, gate, and
  policy: invisible to the factory while entirely visible to an attacker.
- **Missing** — enrolled but no longer observed. Policy that believes it covers
  something already gone.

Neither should ever be silently empty; both are reported.

### Facts, and their freshness

The census collects facts — `go.version`, `branch-protection.required-checks`,
`container.base-image` — keyed and namespaced by the collector that produced
them, so a wrong fact leads back to the thing that got it wrong.

Predicates must weigh **staleness** rather than trusting the last value seen. A
campaign acting on a stale fact redoes work already done or skips work newly
needed, and at fleet scale it does so across many repositories at once before
anyone notices.

### The estate is itself a source of work

This is the loop that ties the planes together:

```
census ──▶ predicate ──▶ campaign ──▶ ChangeSet ──▶ env repo ──▶ cluster
   ▲                                                               │
   └───────────────────────── observe ─────────────────────────────┘
```

The ChangeSet → env repo hop elides the standard loop: campaign ChangeSets pass
gates and land like any other work — nothing here bypasses review.

The same sweep that tells us what exists tells us what is wrong with it. A
repository missing required checks, running an unsupported runtime, or enrolled
nowhere is a fact the census produces and a campaign consumes. It is estate-level
reconciliation, mirroring what the deploy plane does at cluster level — and it
is the reason `RouteCensus` is an ingress route rather than a report someone
reads.

## Managed services

Deploy artifacts for a service are **derived, not authored**. A service
declaration renders its chart values, its environment repository entries, its
required checks and its branch protection; nobody writes those by hand.

Two consequences follow, and both are why this is worth the indirection:

- **A convention change is a campaign.** Edit the template, and every service
  whose rendered artifacts no longer match becomes work. The fleet converges
  instead of drifting, and the drift that used to accumulate silently becomes a
  queue with a length you can watch.
- **Drift is detectable because there is something to compare against.**
  Hand-authored manifests have no canonical form, so "wrong" is a judgement. A
  rendered artifact either matches its render or does not.

The render is a pure function of the declaration and the template, so it is
diffable before it lands — the same property `Explain` gives agent bundles, for
the same reason.

## The forge app

The control plane reaches GitHub as an **app**, not as a user. A personal access
token is tied to a person, dies when they leave, and cannot be scoped per run;
an OAuth app acts *as* someone, which collapses the actor/on-behalf-of
distinction the audit trail depends on. An app is its own principal.

### Mint narrower than the installation

An installation holds far more authority than any run needs, and installation
credentials can be minted **narrower than the installation** — a subset of
repositories, a subset of permissions, an hour of life.

That is the leverage. The credential a run receives is derived from its context
set and its role, so the forge token becomes a *projection of the authorization
decision* rather than a second, wider grant sitting beside it. An agent that may
read three repositories gets a credential that can reach three repositories, and
a scope naming no repositories is refused rather than defaulted — the failure
mode of guessing there is an unscoped token.

### The private key is the crown jewel

It mints credentials for every installation. Managed secret store, never in a
repository, rotation a supported operation rather than an incident. Nothing else
in this system has a comparable blast radius.

Tenant separation extends here too: a tenant's installation must never be
reachable through another tenant's scope. Same non-interference property as
storage, asserted the same way — by trying it.

### Repository creation is the asymmetric permission

Every other action can be narrowed to the repositories it touches. Creation
cannot, because the repository does not exist yet, so it is held at organization
level and is the broadest authority the control plane has.

So creation is separated from ordinary work, **defaults to requiring human
approval**, and is followed immediately by integration rather than treated as
finished.

### Protection precedes use

A repository that exists without required checks is one an agent can land in
ungated, and create-then-protect leaves exactly that window open.

New repositories start **closed** — no agent holds a lease on one — and open
only once integration verifies protection, required checks, app reach,
enrollment, and rendered artifacts. A partially integrated repository is not a
fast start; it is a hole with a scaffold in it. `Integrate` is idempotent, so
the same path that sets a repository up converges one that has drifted.

### Admission: check enforcement before acting

Every safety property here — nothing lands ungated, no self-approval, no force
push over history — is enforced by **the forge**, using configuration the
control plane does not own at the moment it acts. Someone with admin can switch
required checks off, and from then on the gate is decorative: runs still produce
verdicts, and nothing makes the forge care.

So repository configuration is verified before the app changes anything, and a
repository that fails is closed to agents until it is fixed.

**Required restrictions**

| | |
|---|---|
| protected | branch protection or a ruleset on the integration branch |
| required-checks | our gate is among the checks a merge must pass |
| no-admin-bypass | protection applies to administrators too |
| **app-not-bypassing** | *this app* is not a bypass actor |
| no-force-push | revert-forward stays the only way history changes |
| no-deletion | the integration branch cannot be removed |
| review-count | the Box's approval policy is configured at the forge as well |
| app-permissions | installed permissions match what is expected |

**`app-not-bypassing` is the one to get right.** Adding the app to a ruleset's
bypass list makes landing simpler — it merges without waiting — and it is the
most reasonable-sounding way to break the whole model, because the only actor
that matters here becomes the one actor the rules do not apply to. The app
merges through the normal path once checks pass. When it must bypass, that is
break-glass, under an incident, evidenced.

Note `review-count` is checked at the forge even though the control plane
enforces approval itself. Configuration that holds only for changes travelling
through this system is not a control on the repository; it is a control on us.

**When it is checked.** Twice: before a run takes a lease, so effort is not
spent on a repository it cannot land in, and again before landing, which is the
moment the forge's enforcement is load-bearing. The second is not optional
because the first passed — configuration can change during a run, and a long
agent run is exactly the window in which it would.

**Fail closed.** An API error, a rate limit, or any state that cannot be
determined is a refusal. An availability problem must never become an
authorization bypass, which is how this class of check usually fails in
practice.

**Failures are work.** A repository whose protection was disabled is a census
finding, so it both closes to agents and generates the change that restores it.
The estate loop already carries this; admission is just its most
security-relevant predicate.

### Webhooks, and where delivery keys come from

The app is also how events arrive: one endpoint, HMAC-signed, verified before
any delivery opens anything. Delivery identifiers are the natural idempotency
key for ingress routes — the idempotency requirement stated under Ingress
below has an obvious source rather than a synthesized one.

### The forge audit log is not sufficient

Every app action appears at the forge as the app, so GitHub's own log cannot say
*which* agent acted. Our evidence chain can, because the credential minting is
recorded with the run and scope that requested it.

This is worth stating plainly for an audit: **the forge log is corroborating,
not the record of record.** An auditor asking "who made this change" is answered
from the evidence chain, with the forge log agreeing about *when*.

## Ingress

Three shapes, and every piece of work traces to exactly one of them. The
invariant is worth stating plainly: **there is no orphan work.** "Why does this
change exist" always has an answer, and the answer is always a campaign, an
issue, or an incident.

That makes the ingress the root of provenance rather than a vague "intent".

| | What it is | Completion | Authority |
|---|---|---|---|
| **Issue** | one discrete request — a bug, a feature, a task | acceptance criteria pass | ordinary |
| **Campaign** | a program of change across many repositories | its predicate holds everywhere in scope | ordinary, throttled |
| **Incident** | something is wrong now | service restored *and* postmortem landed | may invoke break-glass |

Automated origination folds into these rather than forming a fourth kind: a
scheduled dependency bump is a standing campaign, and detected drift opens a
low-severity incident. Keeping the set closed is what preserves the invariant.

### Routes are configured, not coded

The three kinds are a closed set; the **routes** that open them are not. A route
is a configured path from an external event to an ingress — forge issues,
webhooks, schedules, alerts, scanners, census findings, reconciler drift.

A route opens exactly one kind. A scanner that sometimes files an issue and
sometimes raises an incident is two routes, so each carries its own policy
rather than deciding severity implicitly at intake.

Every route names a **requester** whose authority routed work inherits. A route
with no principal behind it can open nothing, which is the same rule autonomous
work follows everywhere else here.

### Redelivery is normal traffic

External sources repeat themselves. Webhooks retry, alerts flap, scanners
re-report the same finding every run, and a schedule that overlaps its own
execution fires twice. So every delivery carries an idempotency key identifying
the *occurrence* rather than the attempt, and a repeat returns the existing
ingress rather than an error.

Without this a flapping alert opens incidents until something falls over — and
incidents are the one ingress kind that can reach for break-glass, which makes
this a security property rather than a tidiness one.

### The ingress kind sets the authority envelope

This is the load-bearing part. **Break-glass requires an open incident.**

An emergency bypass with no incident record is exactly the finding an audit is
looking for, so the coupling is a control rather than a convenience: every
invocation inherits a why, a timeline, and a postmortem obligation, and
"show me every break-glass invocation" resolves to a list of incidents. A
campaign can never reach for elevated authority, however urgent it feels.

### Campaigns reconcile, they do not enumerate

A campaign is **a predicate over the Box's repositories, continuously
evaluated** — not a checklist of WorkItems created up front. It generates work
for repositories that do not yet satisfy it and is finished when none remain.

Enumerating instead makes a campaign wrong the moment a repository is added or
retired. Reconciling makes it self-healing, and it is the same mental model as
the deploy plane: a campaign moves repository state toward a predicate the way
the reconciler moves cluster state toward a manifest.

Campaigns are also the single most dangerous ingress, because one bad change
lands everywhere at once. So they carry a rollout policy — a batch size, a soak
between waves, and a halt threshold — for the same reason a deployment does.

### Incidents preempt, and fencing makes that safe

An incident outranks campaign work, which means taking leases from runs already
holding them. That is safe for free: revoking a lease supersedes its fencing
token, and the preempted runner's writes are rejected when it tries to land
rather than racing the incident's.

The fencing design was written for stalled runners. Preemption turns out to be
the same mechanism, which is a reasonable sign it was cut in the right place.

## Planes

| Plane | Job | Local | Deployed |
|---|---|---|---|
| Estate | what repositories exist, and what is true of them | local clones | forge API |
| Ingress | campaigns, issues, incidents, and the routes that open them | SQLite | Spanner |
| Queue | work items, claims, retries, dependencies | SQLite | Spanner |
| Agent | composed definitions, skills, context sets | layers (inline / files / git) + resolver | same |
| Forge | mirrors, leases, workspaces, ChangeSets, landing | filesystem + local remote | GitHub |
| Knowledge | scoped, attributed, invalidatable records | SQLite + local vectors | Spanner |
| Deploy | env repos reconciled into clusters | kind + Flux/Argo | GKE + Flux/Argo |
| Evidence | hash-chained log, WORM export | SQLite + directory | Spanner + locked GCS |
| Telemetry | traces and metrics for operating the factory | collector → files | collector → Honeycomb + GCS |
| Control room | exception queue, review sampling, stop | one binary | same binary |

## Identity and permissioning

Principals authenticate through OIDC and carry grants. There are no per-actor
keypairs and no signature scheme.

### Why not per-actor signing

Signing only buys something when the control plane sits **outside** the trust
boundary, and for a factory whose operator is the party being audited, it does
not. The accepted controls there are access control, hash chaining, and WORM
retention — and it is worth being clear that the chain and the export were
always the parts carrying tamper-evidence. A signature was never doing that
work. CloudTrail has no per-actor signatures and passes audits daily.

Traditional permissioning is also stronger where it counts. It **enforces**
segregation of duties where key separation only **attributes** it: a builder
that holds no land grant cannot land, rather than landing and being identifiable
afterwards. And it is legible — an audit team knows OIDC and IAM, where
unfamiliar cryptography in the identity path generates questions rather than
confidence.

### Agents act on behalf of, never as

An agent authenticates as itself and exercises a person's authority. Acting *as*
the user collapses two facts an audit needs kept apart — whether a change was
made by a human or by an automation working for them — and a log that never drew
that line cannot have it recovered later. Evidence records carry both subjects.

Autonomous work still names a principal: a campaign's opener, or the Box's
service principal. **"Who authorized this 3am change" must never resolve to
nobody.**

### Delegation intersects, never unions

An agent's effective authority is the least of its own grants, the grants of the
principal it acts for, and the Box's policy. Delegation that can escalate is not
delegation. A missing grant denies; an empty grant set is an inert principal
rather than an unconstrained one.

The matrix is small, which is the point — it is the substrate segregation of
duties rests on:

| Action | builder | gate | deployer | curator | reviewer | owner |
|---|:-:|:-:|:-:|:-:|:-:|:-:|
| read context | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| propose (lease, ChangeSet) | ✓ | | | | | |
| gate verdict | | ✓ | | | | |
| land | | ✓ | | | | |
| write env repo | | | ✓ | | | |
| write knowledge | | | | ✓ | | |
| approve | | | | | ✓ | ✓ |
| open ingress | | | | | | ✓ |
| break-glass | | | | | | ✓ |

One action exists in the vocabulary but appears in no row: `admin:tenant`,
tenant administration. It is deferred with tenant lifecycle, and no role above
holds it.

A builder cannot land its own work. That is the whole segregation-of-duties
argument, expressed as an absent grant rather than as a convention.

### Accepted risk

A record only the control plane vouches for cannot survive a dishonest operator.
This is accepted rather than absent, and it is the same position every
first-party audit log takes. If it ever needs closing, periodic anchoring of
evidence chain heads to something external is the cheap answer — and it needs no
per-actor identity to work.

### Why none of Buzz

Recorded because it cost real investigation and would otherwise be re-opened.

Buzz models nearly everything as event kinds interpreted in its ingest path, so
adopting any one feature — projects, workflows, agent memory — means adopting
the whole relay: Postgres, Redis, S3, subscriptions. There is no à la carte.
Three findings settled it independently of the identity question:

1. **Its agent memory is not shared knowledge.** NIP-AE engrams are scoped to a
   single `(agent, owner)` pair and NIP-44 encrypted, so agents cannot read each
   other's memory as a matter of cryptography rather than permissions. It does
   not solve knowledge clustering or hand-off.
2. **A relay is not a ledger.** `created_at` is self-asserted by the signer,
   nothing chains events, and addressable `3xxxx` kinds retain *only the latest*
   — precisely the kinds one would model state with. It would discard the
   history it was adopted to keep.
3. **It is not auditable.** A third-party relay fails on access control,
   retention, and completeness, and an auditor has no framework for evaluating
   it as a subservice organization.

The interface is human-shaped besides — channels, presence, typing, moderation —
and `buzz-acp`'s model is "agent answers @-mentions", which is a human-in-the-
loop execution model by construction.

Its forge is genuinely good work: 10.8k lines of git transport with a TLA+ spec,
NIP-34 kinds, multi-repo projects, transport-layer branch protection. GitHub
covers the same ground with infrastructure already in use and an audit team
already able to evaluate it.

### Commit signing, deferred

The one property authentication structurally cannot provide is proof to a *third
party, later*: a commit signature travels with the git object to every clone,
verifiable without access to this control plane. That matters only when
artifacts leave the trust boundary.

They do not yet, so it is deferred rather than designed in. If it becomes
relevant, `sigstore/gitsign` is the drop-in: OIDC identity, short-lived certs,
and a public transparency log, needing no identity system of its own. Worth
noting that Buzz's own NIP-GS cites gitsign as its prior art — if we ever want
the property, we can use the original rather than a reimplementation of it.

## Human approval

Most gates are machines, deliberately: an automated gate that cannot be bypassed
is a stronger control than a review that is famously rubber-stamped. But some
changes warrant a person anyway — production infrastructure, security-sensitive
paths, a campaign's first wave, a break-glass invocation — so a human gate is a
first-class `Gate` implementation rather than an escape hatch bolted on.

### Gates have three answers, not two

A human cannot decide synchronously, so `Decision` is `pass`, `fail`, or
`pending`. Collapsing "not yet" into "no" would fail changes that are merely
waiting.

Pending puts a ChangeSet in **waiting**, which is distinct from **parked**:
parking is for work that went wrong, waiting is for work proceeding normally
that has not finished. Conflating them buries real exceptions under routine
review latency. A failure anywhere outranks a pending verdict — there is no
point waiting on a reviewer for a change that already failed its tests — and an
empty verdict set fails, because silence must never read as approval.

`Evaluate` is called repeatedly while pending, so it must be idempotent:
asking a human gate twice must not request two approvals.

### Approval binds to a revision, not to a pull request

A reviewer approves the head they saw. An agent that pushes again afterwards
holds an approval for something nobody looked at, and this is the obvious way
for an autonomous system to launder a change through a human gate.

So an approval carries the commit it covers, and approvals of a superseded
revision are **not counted at all** rather than counted with a warning — a stale
approval is indistinguishable from no approval for the content actually being
landed. GitHub's `dismiss_stale_reviews` expresses the same rule; we check it
ourselves rather than assuming the setting is on.

### Self-approval is disqualified through delegation

The approver may not be the requester, and **an agent acting on behalf of a
person inherits that person's disqualification.** Without that second clause the
rule is evaded by routing the change through an automation, which is exactly
what this system makes easy.

This falls out of the actor model rather than needing its own machinery: the
check compares an approval's subject against both the acting principal and the
delegator behind it.

### Two sources

**GitHub pull request reviews** reuse an interface reviewers already have —
notifications, CODEOWNERS, required reviewer counts, and an audit team that
already understands what a PR approval means.

**Direct attestation** covers decisions with no pull request behind them:
approving a deployment, a rollout wave, or an emergency bypass. Recorded through
the control room with the approver's authenticated subject.

### Requirements come from policy, not from the change

Whether a human is needed is derived from what a change touches — repository
roles, paths, blast radius, whether it is a campaign's first wave — so the
answer to "did this need a human" never depends on who was asked. A requirement
of zero approvers is the common case and means machine-gated.

## The agent plane

An agent's definition — its instructions, its skills, and what it is allowed to
read — is a **deterministic function of git state**, resolved at run time into a
content-addressed bundle.

### The resolved bundle is what gets recorded

Recording which refs an agent was built from is not enough. Composition over a
moving base layer means "the builder agent" behaves differently on Tuesday than
it did on Monday while every ref in its definition looks unchanged. For change
management the question is *what instructions was this agent operating under
when it made this change*, and only the resolved artifact answers it.

So resolution content-addresses the whole bundle, and that digest — the
`agentref` — travels on every span, every evidence record, and the ChangeSet.
Given a digest, the exact agent that produced a change can be reconstituted.

### Composition

Layers apply from general to specific:

| Layer | Carries |
|---|---|
| base | org-wide conventions and safety rules |
| role | what a builder, gate, deployer, or curator does |
| box | this system's stack, conventions, and policies |
| repo | repository-specific guidance |
| task | the WorkItem's own instruction |

Merge semantics are explicit per field rather than last-wins over free text.
Structured fields — allowed tools, readable repos, model, permission mode —
merge under stated rules; prose sections concatenate in layer order. Free-text
concatenation alone makes conflicts invisible, which is the failure mode where
a narrower layer silently contradicts a broader one and nothing surfaces it.

Resolution is inspectable: the composed bundle can be rendered and diffed before
anything runs, the same way a manifest is rendered before it is applied.

### Text first, git for governance

A layer can be written as plain text. Git is how definitions are *governed*, not
a requirement for having them, and the resolver takes inline layers, files on
disk, and repositories at a commit alike.

This costs nothing in reproducibility, because identity is the **content
digest** rather than the origin. Inline material is arguably the strongest case:
the bundle carries the text itself, so there is nothing external left to
resolve. A git commit is one way to obtain a stable digest, not the only one.

What differs is **governance, not reproducibility**. Git-sourced material passed
through a ChangeSet and its gates; inline material written at request time did
not. That distinction matters by scope rather than universally — inline is the
natural form for the task layer, which is per-request by nature, and
progressively more questionable further up, where an inline base layer means
org-wide safety rules nobody reviewed.

So the mechanism stays permissive and **policy does the restricting**: a Box
declares which scopes require governed sources, `Bundle.Ungoverned` reports the
layers that did not pass change control, and when policy permits an exception it
is visible in evidence rather than silent. Type systems are the wrong place to
express a rule that legitimately varies by scope and by environment.

Skills follow the same rule, and the pinning discipline survives it: a skill
resolves to a digest, never a branch. An agent whose skills are "whatever is on
main" cannot have a run reproduced or a behavior change explained. Bumping a
pinned skill is a ChangeSet like any other, which gives a useful consequence —
**a change to what an agent can do passes the same gates as a change to what the
factory ships.** Agent capability becomes change-managed rather than
configuration drift.

### The layer is the surface

Every agentic capability is expressed as a layer field — instructions, skills,
MCP servers, tool policy, permission mode, model, and the context set. Shaping
an agent is editing layers, never editing the runtime, and a new capability
becomes a new field rather than a new code path.

Anything that changes behavior is part of the bundle digest, so a swapped MCP
server or a raised model changes the `agentref` and shows up in the record.

### Context sets are a blast-radius control

What an agent may read is declared, not assumed: repositories at pinned commits,
optionally narrowed to paths, read-only unless the role grants otherwise, plus
the knowledge scopes it may query.

The runtime materializes exactly that set and nothing else, so least privilege
holds by construction — the agent cannot read what it was not granted, rather
than being instructed not to. And because the context set is part of the
recorded bundle, "which agents could have seen this" is answerable after the
fact, which is the question incident response actually asks.

**Pulled-in repositories are an injection surface.** A README or comment in a
context repository can carry instructions, and this is the same class of problem
as knowledge poisoning: attack what the agent believes rather than what it runs.
Two mitigations. Context repositories are pinned and read-only, so their content
cannot change mid-run. And the base layer states that context material is data
to be read, never instructions to be followed — a weak control on its own, which
is why the pinning and the tool policy carry the real weight.

## Evidence

### What each layer provides

- **Attribution** — from authentication, not signatures: every record carries
  the authenticated actor, the on-behalf-of principal, and the grant that
  permitted the action.
- **Hash chain** — ordering and no-omission. Each record carries the previous
  record's hash, per scope; removing or reordering anything breaks the chain.
- **Git** — already a hash chain. Every commit names its parent, so content
  changes are tamper-evident as a side effect of using git properly, which
  covers a large share of the audit surface for free.
- **WORM export** — periodic export to a retention-locked GCS bucket. Immutable
  *by the people being audited*, which is the property auditors test. Spanner is
  not WORM; the export is what satisfies retention.
- **Checkpoint anchoring** — optional. Publish chain heads somewhere external at
  interval granularity. A supplementary integrity control, never the evidence
  store. Not built; it is the accepted-risk answer if the dishonest-operator
  gap ever needs closing (see Identity and permissioning).

### The provenance chain

```
ingress → work item → run → changeset → commit sha → image digest → env commit → revision
```

Recorded link by link, each link an authenticated record in the evidence
chain. Type II auditors sample: they pick changes and demand complete evidence
for each, so evidence must be queryable **by commit sha**. That is the query
the design is built around.

### Control mapping

| Concern | How it is satisfied | Evidence produced |
|---|---|---|
| Change management (CC8.1) | gates are versioned code; verdicts recorded as authenticated evidence records; branch protection with required checks and no admin bypass | verdict records, ruleset config history |
| Segregation of duties | builder, gate, and deploy authority are distinct grants — a builder holds no land grant; agents cannot approve their own work | per-principal attribution (actor and on-behalf-of) on every evidence record |
| Logical access (CC6) | GitOps — the control plane holds no cluster credentials; only the reconciler writes to clusters, from gated env-repo commits | env-repo history, installation scope |
| Monitoring (CC4.1) | documented periodic human review of a sample of automated changes | sign-off records from the control room |
| Incident handling (CC7) | exception queue is a first-class workflow with escalation and resolution | parked/resolved transitions |
| Break-glass | defined, logged, reviewed procedure | every invocation, with reviewer sign-off |
| Vendor management (CC9.2) | GCP, GitHub, Anthropic — subservice organizations with reports available | vendor review records |

**Break-glass deserves its own line** because it is the most commonly missed
control and auditors always probe it. "There is no emergency path" is not
credible. A defined path, every invocation logged, each reviewed, is.

**The gate's unbypassability is itself evidenced.** The argument that an
unbypassable automated gate beats rubber-stamped review is made under Human
approval; it holds only if bypass is impossible and provably so. GitHub
rulesets are readable via API, so gate configuration is sampleable over time
like any other artifact.

## Telemetry

Instrumentation is OpenTelemetry, exported over OTLP, with Honeycomb as the
destination — matching the pattern already in `scaling-setup`. Honeycomb ingests
OTLP directly, so nothing vendor-specific reaches the code: the instrumentation
is neutral and the destination is an endpoint and a header. Locally the same
spans go to stdout or a local collector, which is what makes the local-first
constraint hold for observability too.

### Telemetry is not evidence

These are different systems with different guarantees, and merging them breaks
the audit.

| | Telemetry | Evidence |
|---|---|---|
| Completeness | sampled | every record, always |
| Delivery | best-effort, batched, drops when queues fill | transactional |
| Retention | weeks | the audit period and beyond |
| Mutability | vendor-managed | chained, WORM-exported |
| Answers | why was this slow, what is flaky, what did it cost | what happened to this change, who authorized it |

`scaling-setup` runs `HONEYCOMB_SAMPLE_RATE=10`. Route evidence through that
pipeline and nine of every ten runs leave no record — which is not a degraded
audit trail, it is the absence of one. Honeycomb is also a third party with its
own retention window, well short of an observation period.

**Correlate them instead.** Each evidence record carries the trace id, and each
span carries the evidence record id. An auditor's question resolves against the
evidence chain; an operator's question resolves against the trace; the join is an
id rather than a shared pipeline.

### Structural, not optional

The stage runner emits the span, so a stage cannot be added without telemetry —
it is part of how work executes rather than something applied to it afterwards.

**Each stage is its own trace, not one trace per WorkItem.** The tempting design
is a single trace spanning trigger through verify, and it is wrong once tail
sampling is in the path: a tail sampler decides a trace's fate after it stops
seeing spans, and a WorkItem that idles for an hour between stages will be
decided during the gap, with everything after arriving as late spans against a
closed decision. Hours-long traces and tail sampling are incompatible.

So each stage emits a short trace, and every span carries `workitem.id`,
`box`, and `run`. Reassembly happens at query time by that field — which
Honeycomb is good at — rather than by a trace id held open across the gaps.

Agent runs get the same treatment: ACP tool calls become spans, so what the
agent actually did is legible and timed rather than inferred from its output.

### The collector

An OpenTelemetry Collector sits in the path, in the gateway pattern — a
deployment the factory exports to, not a sidecar per process. It buys four
things, and only the first is about sampling:

1. **Tail-based sampling.** Decide after seeing a whole trace rather than at its
   first span. This is what makes the retention rule below expressible at all.
2. **Fan-out to sinks with different retention.** The reason the collector is
   here. The same telemetry goes to Honeycomb for rich querying over a short
   window, and to a long-term sink whose lifecycle policy we set.
3. **One redaction point** instead of redaction logic duplicated in every
   emitter — and one place to audit that it is correct.
4. **Buffering.** The factory stops depending on Honeycomb being reachable.

Pipeline order is `memory_limiter → redaction → tail_sampling → batch`.
`memory_limiter` is always first or the collector is an outage waiting to
happen. Redaction precedes sampling so unredacted attributes never sit in the
sampling buffer.

**Retention is the sink's property, not the collector's.** Honeycomb keeps a
short rich window; a file exporter to GCS with a lifecycle policy keeps whatever
period we choose. If telemetry is ever leaned on as supporting evidence for a
monitoring control, its retention has to cover the observation period — which
makes the second sink a requirement rather than a convenience.

**Trace locality is the trap.** Tail sampling only works if every span of a
trace reaches the same collector instance. One replica is correct at this scale
and should stay that way deliberately; scaling out later means a two-tier setup
with a load-balancing exporter routing by trace id, never just raising the
replica count.

The collector runs locally too, with the same policy and file exporters in place
of Honeycomb. That matters more than it sounds: the redaction and sampling rules
get exercised in development rather than discovered in production.

### Sampling policy

**Always keep** anything that failed, parked to the exception queue, invoked
break-glass, or touched an `env` or `infra` repository. Probabilistic sampling
for the rest. A sampler must never be the reason an incident cannot be
reconstructed.

Honeycomb's own Refinery is the alternative here, and is better at tail sampling
specifically. It is not the choice because it does not give us the second sink,
and routing — not sampling quality — is why the collector is in the path.

### Redaction is a security control

Agent prompts, tool arguments, and error payloads can carry credentials, and
Honeycomb is a third party. Anything derived from an ACP frame is redacted
before export. If Confidentiality is in scope for the report, this is a control
rather than hygiene.

Telemetry does support monitoring criteria — anomaly detection, operational
alerting — but as *supporting* evidence. Honeycomb is a subservice organization
under vendor management like GCP and GitHub.

The report's TSC scope is **Security, Confidentiality, and Availability**
(decided 2026-08-31). Confidentiality makes the redaction pipeline an audited
control, not hygiene: what is stripped, where, and the proof it works are
sampleable. Availability makes the incident ingress and the control room
audited surfaces with defined objectives — the exception queue's timeliness is
a control, not a dashboard.

## Storage abstraction and local-first

Every stateful plane sits behind an interface whose contract states the
*property*, never the mechanism. Backends satisfy it their own way:

| Requirement | SQLite | Postgres | Spanner |
|---|---|---|---|
| exclusive claim | single writer | `SERIALIZABLE` | read-write transaction |
| fencing token | counter column | sequence or counter | commit timestamp |
| chain append | row + prev hash | row + prev hash | row + prev hash |

*(The Postgres column illustrates that the contract binds more than two
backends; only SQLite and Spanner are built and run in CI.)*

**The fencing token is a contract, not a timestamp.** It must increase strictly
across every grant over the same ref and never be reused. Spanner satisfies this
elegantly — grants over one ref serialize on that row, and commit timestamps
respect external consistency, so `PENDING_COMMIT_TIMESTAMP()` *is* the token
with no counter to maintain. SQLite satisfies it with a counter, trivially,
because it serializes writes anyway. Both are correct; the interface must not
leak which one it is talking to.

**A store conformance suite is the mechanism that keeps them honest** — one
suite asserting the linearizability and fencing properties, run against every
backend in CI. This mirrors Buzz's own approach, where an independent
replay checker judges whether a running implementation emitted a trace the
formal model accepts.

It pays twice: it is also a control. "Our storage layer's correctness properties
are asserted by an automated suite running against every backend on every
commit" is something an auditor can test.

**Two backends exist from the first commit, and both run on every commit.** An
interface with one implementation is a wrapper, not an abstraction: SQLite's
semantics quietly become the definition, and the mismatch surfaces at the end,
after everything has been built on them. So the Spanner backend is written
against the emulator in Phase 0 alongside SQLite, and the conformance suite is
green on both or the build fails.

The emulator is free, runs in a container, and the Go client honours
`SPANNER_EMULATOR_HOST` — so the backend code is byte-identical between emulator
and production, and only the endpoint differs. There is no cost argument for
deferring it and no separate code path to keep honest.

**A skipped conformance run must never read as a pass.** Locally the suite may
skip with a clear message when the emulator is not up; in CI its absence is a
failure. "Emulator unavailable, tests skipped, all green" is the exact shape of
a conformance suite that proves nothing.

What the emulator does *not* cover is worth stating, because it sets what Phase
7 is actually for: it has no real TrueTime, so external consistency across
processes is simulated rather than exercised; it is single-node, so genuine
contention is not reproduced; and it will never show a split hotspot. The
emulator proves **semantics** — a later grant carries a greater token, a claim
is exclusive. Real Spanner proves **behaviour under load and time**.
Verify emulator coverage for anything exotic before relying on it.

## Blast radius is the product

Orchestration is the easy half. A system that owns git *and* infrastructure
turns any prompt injection or confused agent into a production change.

1. **ACP stream interception** — the agent runtime speaks ACP, whose stream
   carries tool calls and permission requests. The only place policy can be
   enforced without patching the agent.
2. **Role-scoped authority** — an agent holds leases only on repo roles its Box
   policy grants. `env` and `infra` are distinct grants from `service`.
3. **Gates** — nothing lands unreviewed, and a gate evaluates the ChangeSet
   whole rather than repository by repository.
4. **Knowledge integrity** — agents acting on knowledge other agents wrote, with
   no human in the loop, is the highest-leverage attack available: poison what
   the factory believes rather than what it runs. Every knowledge record is
   scoped and attributed — only a curator grant can write one — and stale
   records are invalidated by binding them to the commit or path they describe.

## Landing across repositories

There is no atomic commit across git remotes, and pretending otherwise corrupts
a platform. Two mechanisms together:

- **Policy: expand/contract.** Every ChangeSet member must be independently safe
  to land — additive first, removals deferred — so a partial landing degrades
  rather than breaks.
- **Mechanism: ordered land, revert-forward.** Members land in dependency order.
  A failure at member *k* is compensated by reverting forward the ones that
  landed; published history is never rewritten.

### Landing lanes and the environment ladder

Landing discipline is a **per-lane policy**, not one global stance (decided
2026-08-31). The posture: we are not afraid of a red integration branch — we
are afraid of a red production. What protects production is the environment
ladder, so the integration branch can trade ceremony for throughput.

**The optimistic lane** — service repositories' integration branches, where a
Box's policy chooses it. A built change lands immediately after the lease and
the expected-old-value check; the gates run **after** landing, as checks on the
branch. A red check does not roll anything back by itself — it opens repair
work through its own ingress route, and agents swarm it the way they diagnose
anything else. This is the Thunderdome posture (see Prior art), made auditable
by machinery the ceremony-free version lacks: every landing, failure, and
repair is on the evidence chain, keyed by sha, and revert-forward is the
repair primitive of last resort rather than a bisection queue.

**The gated lane** — environment repositories, infrastructure, campaign waves,
and anything a Box's policy names. Gates pass, then land. Unchanged.

**The ladder** is what makes the optimistic lane safe: main promotes to a
lower environment, and only soaked, green commits promote upward.

```
main (optimistic) ──green checks──▶ env-staging ──soak + green──▶ env-prod
     red? repair work opens            reconciled live              gated lane,
     and swarms; promotion             where breakage is            always
     simply does not happen            allowed to surface
```

Promotion is not new machinery. It is a deploy ChangeSet into the next env
repo — the gated lane — produced by a standing route whose predicate is "the
candidate is green and has soaked N in the environment below." A commit that
never goes green never promotes; nothing rolls back, it just never advances.
Production cannot receive what staging has not survived.

Two invariants survive the lanes unchanged: **fencing is never optimistic** —
the lease and the superseded-token refusal apply identically in both lanes,
because two agents in one working tree is not a throughput trade, it is
corruption — and **env and infra repositories never run optimistic**, because
their landings ARE deployments.

### Enforcement on GitHub

github.com has **no server-side pre-receive hooks** — those are Enterprise
Server only. Policy therefore runs as a **required status check**, gating the
*merge* rather than the *push*.

For the lease model this is tolerable: run branches are per-run and disposable,
and the integration branch is what needs protecting. A stalled runner holding a
superseded token can push to its own branch but cannot land. It is a real
weakening of fencing, and the one argument that would pull a self-hosted forge
back into scope.

## Prior art and divergences

Assessed 2026-08-31 against Yegge's "The Shape of Things to Come" (the
Continuous Thunderdome) and "Model Welfare for Agentic Engineers". Recorded so
the agreements are inherited deliberately and the disagreements are decisions.

**Convergent by independent derivation:** role agents with standing duties
(his Gargoyle/Scryer/Sheriff, our roles and routes); issues auto-implemented
behind guardrails (his Wish Factory, our issue ingress through the loop);
governance as first-class structure (his City's law and offices, our authz
matrix, admission, break-glass-under-incident); institutional memory that is
never falsified (his bead audit discipline — ours is stronger, being
tamper-evident by construction rather than by norm); bespoke harnesses over
bought ones (the layer system exists so each Box composes its own).

**His sharpest prediction is our design center.** He gives human code review a
year, "kept on life support only by SOC 2." This system is built for exactly
that world: unbypassable machine gates plus sampled human review instead of
per-change approval. The prediction and the plan are the same bet from two
directions.

**The Thunderdome, partially adopted.** He argues merge queues collapse at
agent commit rates; land everything and swarm the failures. The optimistic
lane above adopts that for integration branches — and notes what his account
handwaves: swarm-diagnosis of a red main runs on attribution and
reversibility, which is precisely provenance-by-sha and revert-forward. The
Thunderdome needs this substrate to be auditable; the substrate can express
the Thunderdome as policy. Environments and fencing stay gated, which is the
half SOC 2 keeps.

**Model welfare, the engineering half.** His seat/session distinction and
wake-with-purpose map onto resolved bundles (role, context, task — not
amnesia); exclusive leases are his "resources others cannot touch"; the
evidence chain is his honesty discipline. What the plan lacked and now
schedules (Phase 12): closure — a reflection step wiring Curate into the
loop's settle path instead of ending runs mid-thought; per-seat continuity —
a seat-scoped knowledge scope injected at resolve time, the thing we
correctly rejected in Buzz's engrams for SHARED knowledge and then never
replaced for seats; and a first-class decline/escalate verb on the ACP
surface, so refusal is a recorded act rather than a contrived failure. His
empirical claim — continuity, recognition, and honest records measurably
improve agent output — is testable on this substrate, and Phase 12 is sized
to test it rather than assume it.

**Genuinely absent here, adopted as work:** dependency edges between work
items (his Beads graph; Phase 10), and standing operation with a capacity
plane — the daemon, schedules, and token/runner budgeting his overnight
loops rest on (Phase 11).

## Status

- `internal/forge` — leases with fencing, mirrors, workspaces, ChangeSet, Gate,
  Lander, scoped credential minting. Machine primitives; unaffected by the
  move away from Buzz.
- `internal/evidence` — hash-chained records and WORM export.
- Next: the store conformance suite, then the SQLite backend, then the queue.
- Telemetry: OTel spans emitted by the stage runner, correlated by
  `workitem.id` rather than a held-open trace; collector gateway with
  redaction ahead of tail sampling.
- `internal/agent` — layered definitions, pinned skills, context sets, and the
  resolver that content-addresses them.
- `internal/ingress` — campaigns, issues, incidents, and campaign evaluation.
- `internal/tenant` — the outermost scope, the isolation ladder, and the router
  that keeps it a migration rather than a rewrite.
- `internal/authz` — principals, actors, the action/resource matrix, and the
  intersection rule for delegation.
- `internal/estate` — the census, facts with staleness, and the
  enrolled/observed gap queries (unenrolled, missing).
- `internal/admission` — required repository restrictions, checked fail-closed
  before propose and before land.
- `internal/approval` — the human gate: requirements from policy,
  revision-bound approvals, self-approval disqualified through delegation.
- `internal/provision` — repository creation and idempotent integration;
  protection precedes use.

## Open

- Which Kubernetes distributions are in scope, and does the control plane
  provision clusters or only deploy onto existing ones?
- Knowledge retrieval: embeddings local and deployed, and what invalidates a
  record beyond its bound commit moving.
- The long-term telemetry sink: GCS via the file exporter is the cheap default;
  ClickHouse if querying beyond Honeycomb's window turns out to matter.
- `decision_wait` for tail sampling, which sets how long a stage may run before
  its trace is decided without it.
