// Package loop wires the first closed loop: an opened issue becomes a work
// item, a builder produces a change in a leased workspace, gates judge it,
// and the lander pushes it — unattended, with evidence at every transition
// and the whole chain queryable by the landed commit.
//
// This is the phase that validates or kills the premise: everything before it
// is scaffolding, everything after is scale.
package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/checks"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
)

// Builder produces a change in a workspace and returns the commit to land.
// The ACP runtime satisfies this for real agents; tests satisfy it with a
// scripted change — the loop cannot tell, which is the point of the seam.
type Builder interface {
	Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error)
}

// Gates builds the gate set for a materialized workspace. A closure rather
// than a slice because gates run IN the workspace the builder wrote.
type Gates func(ws forge.Workspace) []forge.Gate

// Loop runs work items end to end for one repo of one Box.
type Loop struct {
	Store  store.Store
	Runner string
	Mirror *git.Mirror
	Repo   forge.Repo
	Branch string
	// PRLander, when set, wraps the plain git.Lander a land constructs so
	// the change lands through an opened GitHub pull request
	// (git.NewPRLander) rather than a direct fenced push — the same inner
	// git mechanics either way, never replaced. Nil preserves exactly
	// today's behavior: git.NewLander(l.Mirror, l.Store.Log()).Land.
	PRLander func(inner *git.Lander) forge.Lander
	Builder  Builder
	Gates    Gates
	// Approval, when set, builds this run's human-approval gate — a seam
	// rather than a widening of the Gates signature, because the requester
	// an approval gate must disqualify is per item, not per workspace (see
	// [Gates]). Nil leaves an item's gate set exactly l.Gates(ws), which is
	// today's behavior with no approval boundary configured. Appended LAST
	// by [Loop.gatesFor], behind the command gate and behind #62's reviewer:
	// the loop's existing stop-at-first-fail means a change that failed its
	// tests never reaches a person.
	Approval func(item queue.WorkItem) forge.Gate
	// Lane is the landing discipline; empty means gated. Env and infra
	// repositories refuse the optimistic lane at Run.
	Lane forge.Lane
	// Authz, when set, makes the loop enforce authority instead of assuming
	// it: propose is checked before the build and land before the landing,
	// with the runner as principal and the item's requester as delegator —
	// so delegation intersects HERE, in the pipeline, not only in the
	// evaluator's own tests. Nil skips enforcement (bootstrap, tests).
	Authz authz.Authorizer
	// Handoff, when set, runs on every settle so closure is part of
	// finishing: the seat writes its notes before the lights go out, whether
	// the run landed or parked. Errors are recorded, never fatal — losing a
	// note must not unsettle settled work.
	Handoff func(ctx context.Context, item queue.WorkItem) error
	// Red opens repair work when an optimistic landing goes red and the red
	// survives one re-run — a red that does not reproduce is recorded as a
	// flaky check, and nobody is summoned for it. It is handed the attempt
	// the repair follows, so the summons is worth answering: the sha, the
	// branch that still carries it, and which check said what. Returns the
	// repair ingress id. Nil means even a confirmed red is recorded but
	// nobody is summoned — configure it; see OpenRepair.
	Red func(ctx context.Context, item queue.WorkItem, prior agent.Prior) (string, error)
	TTL time.Duration
	// MaxHandoffs bounds how many fresh sessions one item may ask for before
	// it parks instead. Zero means [DefaultMaxHandoffs]; negative means no bound,
	// which is a decision an operator can make and the factory will not make
	// for them — the same posture OpenRepair's depth takes, and for the same
	// reason: both are the factory summoning itself.
	MaxHandoffs int
	// Attempts bounds how many builds one claim may spend on a gate or
	// review refusal before it parks instead. Zero means [DefaultAttempts];
	// there is no "negative means unbounded" case as [MaxHandoffs] has,
	// because boxcfg refuses a non-positive attempts: at Load, so this field
	// is never asked to mean anything but a real budget or the default.
	//
	// A refusal under budget goes straight to the next build in the SAME
	// claim rather than parking: the gate's verdict is feedback the very
	// next attempt can act on, told through the carried [agent.Prior], and
	// parking it instead would spend a person's attention on exactly the
	// judgement a retry could have used itself (issue #81). Only a refusal
	// on the last attempt parks.
	Attempts int
	// GateRetries bounds how many more times a gate that returned an error —
	// not a verdict — is re-asked about the SAME [forge.ChangeSet] before the
	// item parks instead. Zero means [DefaultGateRetries]; there is no
	// "negative means unbounded" case, for the same reason [Attempts] has
	// none: boxcfg will refuse a non-positive value at Load (a follow-up
	// unit), so this field is never asked to mean anything but a real bound
	// or the default.
	//
	// A retry answers a flaky miss, not a wrong change: the gate said
	// nothing about the change at all, so asking again is free of judgement
	// and spends no attempt (issue #83). That is the whole difference from
	// [Attempts] — a refusal is feedback the next attempt can act on, but an
	// error is nobody's verdict yet, so it is the SAME ask repeated rather
	// than a new build.
	GateRetries int
	// GateBackoff returns how long to wait before re-asking a gate that just
	// errored, given the try number that just failed (1 for the first).
	// Nil means a modest default that grows with each try — long enough
	// that a transient blip (a rate limit, a flaked network call) usually
	// clears before the next ask, short enough that a real outage still
	// parks inside a tick or two. Tests set it to a function returning zero
	// so a retrying gate does not slow the suite down.
	GateBackoff func(try int) time.Duration
	// BuildTimeout bounds one build. Zero means [DefaultBuildTimeout]; a
	// negative value means no bound, which an operator may choose.
	//
	// Nothing bounded an agent session before: the lease TTL does not, because
	// Checkout renews it and no one kills the process when it lapses. An agent
	// that hangs or loops ran until the daemon stopped, spending the whole
	// time. A clock is the difference between a runaway costing a bounded
	// amount and costing whatever it likes.
	BuildTimeout time.Duration
	// Meter, when set, charges the run's start the moment the builder is
	// actually invoked — not when the pump claims the unit, and not when a
	// claim only reaches leaseSections before going back to Wait. The pump's
	// own Acquire already took the concurrency slot and checked the budget
	// before Run was ever called; Meter.Start here just marks that one
	// acquired slot turned into a real build. Nil skips charging, which is
	// safe: most test constructions of Loop do not set it.
	Meter *capacity.Meter
}

// DefaultBuildTimeout bounds a build when the Box states none. Long enough
// for real work, short enough that a wedged session is an incident tonight
// rather than an invoice next month.
const DefaultBuildTimeout = 30 * time.Minute

// MaxRepairDepth bounds how far the factory will chase its own tail when no
// depth is configured: a repair, a repair of that, a repair of that.
const MaxRepairDepth = 3

// DefaultMaxHandoffs bounds how many times one item may ask for a fresh
// session when the Box states no limit of its own.
//
// An item that has taken four sessions and is still not finished has stopped
// being a context problem and become a sizing one, and that is a judgement —
// which is what the exception queue is for. Three is the same number the
// repair chain uses, for the same reason: it is enough to be worth having and
// few enough that nobody discovers the bill afterwards.
const DefaultMaxHandoffs = 3

// DefaultAttempts bounds how many builds one claim may spend on a gate or
// review refusal when nothing configures a bound. Two is enough to turn a
// single flaky miss, or a mistake the refusal itself named plainly enough to
// fix, into a landed change without a person — and few enough that a change
// genuinely wrong does not run up a bill arguing with the same gate.
const DefaultAttempts = 2

// DefaultGateRetries bounds how many more times a gate that errored is
// re-asked when nothing configures a bound. Two more tries — three asks in
// all — is enough to clear a flaky miss without turning a genuinely broken
// gate into a long wait before the park a person will see anyway.
const DefaultGateRetries = 2

// defaultGateBackoffUnit is the base of [Loop.gateBackoff]'s default growth:
// try 1 waits one unit, try 2 waits two, and so on. Seconds, because a gate
// error is infrastructure trouble worth a short pause, not a rate limit
// worth minutes.
const defaultGateBackoffUnit = 2 * time.Second

// OpenRepair wires Red to an ingress route: the red commit's sha is the
// delivery key, so however many post-land checks report the same red, one
// repair item exists.
//
// The attempt travels with the delivery rather than being flattened into the
// payload the repair reads as its task. A repair opened as new work is a task
// string and nothing else, and its agent then re-derives from the diff what
// the previous run already knew; carried as [agent.Prior] it resolves into a
// layer at wake time, which keeps the merge semantics explicit and puts the
// carried context in the agentref.
//
// maxDepth bounds the CHAIN, which sha-keyed idempotency cannot. One repair
// per red sha is not a bound when each repair produces a new sha that can
// itself be red: an optimistic lane over a genuinely broken tree will open
// repair after repair, each one a real agent session, and the only thing
// standing in front of that is the budget. Zero uses [MaxRepairDepth];
// negative means no bound, which is a decision an operator can make and the
// factory will not make for them.
func OpenRepair(router *ingress.StoreRouter, route ingress.Route, maxDepth int) func(context.Context, queue.WorkItem, agent.Prior) (string, error) {
	if maxDepth == 0 {
		maxDepth = MaxRepairDepth
	}
	return func(ctx context.Context, item queue.WorkItem, prior agent.Prior) (string, error) {
		if maxDepth > 0 && item.Depth >= maxDepth {
			// Deep enough. The red is on the check chain either way, so
			// promotion will not take the commit; what stops here is the
			// factory summoning itself again.
			return "", fmt.Errorf("repair chain reached depth %d without going green — stopping rather than opening another: %s",
				item.Depth, prior.Reason)
		}
		ing, _, err := router.Open(ctx, route, ingress.Delivery{
			Key: "red-" + prior.Sha, Payload: []byte(prior.Reason), Depth: item.Depth + 1,
			Prior: &prior,
		})
		if err != nil {
			return "", err
		}
		return ing.ID, nil
	}
}

// ItemFor turns an admitted ingress into ONE work item that the caller names
// itself. The ingress link is what keeps "why does this change exist"
// answerable from the other end.
//
// This is not the intake path. Intake decomposes an origination into a graph
// and takes the item ids from the plan — see [plan.Items]. What is left here
// is the case where the factory opens a single unit whose name is meaningful
// on its own: a ladder promotion to a named environment at a named candidate,
// a deploy of a named image. Those carry no plan, and the field says so rather
// than pointing at one that was never made.
func ItemFor(ing ingress.Ingress, id, task string) queue.WorkItem {
	return queue.WorkItem{ID: id, Tenant: ing.Tenant, Box: ing.Box, Ingress: ing.ID,
		Kind:      string(ing.Kind),
		Requester: ing.Requester, Repo: ing.Repo, Task: task, Depth: ing.Depth,
		OriginKey: ing.Key,
		// Repair work wakes with the attempt it follows rather than cold; the
		// ingress is where that travels, the same way the depth does.
		Prior: ing.Prior}
}

// gatesFor is this run's gate set: l.Gates(ws), with the item's approval gate
// (see [Loop.Approval]) appended last when the seam is set.
func (l *Loop) gatesFor(item queue.WorkItem, ws forge.Workspace) []forge.Gate {
	base := l.Gates(ws)
	if l.Approval == nil {
		return base
	}
	out := make([]forge.Gate, len(base)+1)
	copy(out, base)
	out[len(base)] = l.Approval(item)
	return out
}

// diffJudge reports whether g's verdict depends only on the diff's content
// rather than the base it was evaluated against — see [forge.DiffJudge] —
// found out by a type assertion so the loop need not import any concrete
// gate's package to ask.
func diffJudge(g forge.Gate) bool {
	dj, ok := g.(forge.DiffJudge)
	return ok && dj.JudgesDiff()
}

// findGate returns the gate named name among gates, or nil.
func findGate(gates []forge.Gate, name string) forge.Gate {
	for _, g := range gates {
		if g.Name() == name {
			return g
		}
	}
	return nil
}

// gateCarriedRecord is a "gate-carried" record's body: a rebase replayed a
// patch equal to the one a gate already judged, so that gate's verdict rides
// forward rather than judging the same content a second time. Reviewed is
// the commit the ORIGINAL verdict named at Subject in the chain; Original is
// that verdict record's own chain id, so an auditor lands on the one
// judgement this carry rests on rather than being told merely that some
// judgement exists somewhere.
type gateCarriedRecord struct {
	Gate     string `json:"gate"`
	Reviewed string `json:"reviewed"`
	Original string `json:"original"`
	PatchID  string `json:"patch_id"`
}

// carryDiffGates is the rebase-carry half of the gate stage's carry logic
// (issue #95): of the gate names in passed, it keeps only those that both
// name a gate in gates satisfying [forge.DiffJudge] and have a findable
// original verdict on the chain, and for each one it kept it writes a
// "gate-carried" record at newHead naming that original verdict and the
// patch-id the rebase found equal — never a fresh "gate" judgement, so an
// auditor sees one judgement and one carry rather than two judgements.
//
// A name whose original verdict cannot be found is dropped rather than
// carried on faith: this repo's rule that unknown never reads as zero
// applies to provenance as much as to anything else it governs.
func (l *Loop) carryDiffGates(ctx context.Context, item queue.WorkItem, gates []forge.Gate, passed []string, reviewed, newHead forge.Commit, patchID string) []string {
	var out []string
	for _, name := range passed {
		g := findGate(gates, name)
		if g == nil || !diffJudge(g) {
			continue
		}
		original, ok := l.findGateVerdict(ctx, item, name, reviewed)
		if !ok {
			continue
		}
		body, _ := json.Marshal(gateCarriedRecord{
			Gate: name, Reviewed: string(reviewed), Original: original, PatchID: patchID,
		})
		l.record(ctx, item, "gate-carried", string(newHead), string(body))
		out = append(out, name)
	}
	return out
}

// findGateVerdict scans item's chain for the latest "gate" record on subject
// reviewed whose body names gate with a pass — mirrors the read pattern in
// [queue.priorVerdicts]: unmarshalling only the shared fields rather than
// importing internal/forge for a shape that is already the record's own
// default JSON encoding.
func (l *Loop) findGateVerdict(ctx context.Context, item queue.WorkItem, gate string, reviewed forge.Commit) (string, bool) {
	type verdictBody struct {
		Gate     string
		Decision string
	}
	found := ""
	for r, err := range l.Store.Log().Since(ctx, item.Tenant, "workitem/"+item.ID, 0) {
		if err != nil {
			return "", false
		}
		if r.Kind != "gate" || r.Subject != string(reviewed) {
			continue
		}
		var body verdictBody
		if json.Unmarshal(r.Body, &body) != nil {
			continue
		}
		if body.Gate == gate && body.Decision == string(forge.DecisionPass) {
			found = r.ID // keep scanning: a later record on this subject wins
		}
	}
	return found, found != ""
}

// Poll asks whether a held item's outstanding gate has answered yet, and
// writes nothing whatsoever: no claim, no lease, no evidence. It reports
// whether anything has changed, so the pump can leave a still-waiting item
// entirely alone.
//
// A check is not a transition. An item waiting on a person is re-drained
// every tick for as long as the review takes, and running it through Run to
// learn "still pending" costs a claim record, an authorization record, a
// resume record, a gate verdict and a waiting record — five or more, every
// fifteen seconds, for days. That is a chain that says nothing happened, at
// length, and then has to be compacted away (issue #3). The review is held;
// asking after it should leave no mark.
//
// Anything it cannot answer cleanly reports changed, so the doubt is resolved
// by the real path rather than by a shortcut that decided to skip.
func (l *Loop) Poll(ctx context.Context, item queue.WorkItem) bool {
	if item.State != queue.StateWaiting || item.Built == "" {
		return true // nothing is being held; this is not our question
	}
	ws, ok := l.Mirror.HeldWorkspace(item.ID, l.Repo.Name, forge.Commit(item.BuiltBase))
	if !ok {
		return true // the tree is gone; Run decides whether to adopt or rebuild
	}
	cs := forge.ChangeSet{
		ID: "cs-" + item.ID, Tenant: item.Tenant, Box: item.Box, Run: item.ID,
		Intent: item.Task,
		Members: []forge.Member{{
			Repo: l.Repo.Name, Branch: l.Branch,
			Base: forge.Commit(item.BuiltBase), Head: forge.Commit(item.Built),
			Summary: item.Summary,
		}},
	}
	outstanding := 0
	for _, g := range l.gatesFor(item, ws) {
		if slices.Contains(item.PassedGates, g.Name()) {
			continue // carried; it cannot change its mind about this head
		}
		outstanding++
		v, err := g.Evaluate(ctx, cs)
		if err != nil || v.Decision != forge.DecisionPending {
			// An answer, or a gate that could not give one. Either way the
			// held state is over and Run must record what happens next.
			return true
		}
	}
	// Nothing outstanding means the item is waiting on the branch rather than
	// on a person, and its turn may have come.
	return outstanding == 0
}

// Run drives one item from claim to settled state. The returned item reports
// where it ended: done, parked, waiting, or failed — an error return means
// the loop itself broke, not that the change was judged and refused.
func (l *Loop) Run(ctx context.Context, item queue.WorkItem) (out queue.WorkItem, rerr error) {
	if err := forge.ValidateLane(l.Lane, l.Repo.Role); err != nil {
		return item, err
	}
	// The workspace outlives the lease for exactly as long as the resume point
	// does: held while the item waits with a commit to come back to, has
	// handed off with a change its successor continues, or parked with a
	// commit a gate refused and a retry may re-ask about, handed back to
	// Prune the moment it settles or gives that commit up. A park only ever
	// carries Built when the gate-fail branch below kept it — every other
	// park still empties it, so the guard already tells the two apart.
	defer func() {
		switch out.State {
		case queue.StateWaiting, queue.StateHandedOff, queue.StateParked:
			if out.Built != "" {
				l.Mirror.Hold(item.ID, l.Repo.Name)
				return
			}
		}
		l.Mirror.Discard(item.ID, l.Repo.Name)
	}()
	q := &queue.Queue{Store: l.Store, Runner: l.Runner, TTL: l.TTL}
	item, claim, err := q.Claim(ctx, item)
	if err != nil {
		return item, fmt.Errorf("loop: claim: %w", err)
	}

	if deny, err := l.authorize(ctx, item, authz.ActionPropose); err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	} else if deny != "" {
		// A grant the factory does not hold. The control worked, and it will
		// keep working on every retry until a person changes something.
		return q.Park(ctx, item, claim, queue.SeverityHigh, deny)
	}

	// WHERE this unit works, held before it builds. Two units in disjoint
	// sections of one repository run at once; two in the same section take
	// turns; a unit that names no section is the whole repository and waits
	// for every section to be free. A wait, not a park: nobody needs to look
	// at a unit whose turn has not come, and next tick it tries again.
	//
	// This is what makes fan-out safe rather than merely possible. The land
	// window already serializes the branch and replays a moved base, so two
	// units were never going to corrupt each other — they were going to
	// conflict, and a conflicting replay is rebuilt by an agent, which is the
	// most expensive thing the factory does. The lease spends a tick to save
	// a session.
	releaseSections, busy, err := l.leaseSections(ctx, item)
	if err != nil {
		return q.Park(ctx, item, claim, queue.SeverityMedium, err.Error())
	}
	if busy != "" {
		return q.Wait(ctx, item, claim, busy)
	}
	defer releaseSections()

	leaser := l.Store.Leaser(l.Runner)
	ref := "refs/heads/" + l.Branch
	// The workspace's lease, not the branch's. A build is a whole agent
	// session, so holding the branch across it serialized every unit of work
	// on a repo even when the changes could not possibly collide: a budget of
	// three concurrent runs produced exactly one, because the meter allowed
	// three and the lease admitted one (issue #18). The branch is taken later,
	// for the length of a landing rather than the length of a session.
	//
	// This lease is no less real. It is the authority the workspace exists
	// under, the claim Checkout renews, and what lets Prune reap the trees of
	// a runner that died. It is keyed by run, so nothing contends for it —
	// which is the point: it protects a tree, and trees were never shared.
	wsLease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: item.Tenant, Repo: l.Repo.Name, Ref: "run/" + item.ID, Run: item.ID, TTL: l.TTL,
	})
	if err != nil {
		return q.Wait(ctx, item, claim, fmt.Sprintf("workspace for %s is leased: %v", item.ID, err))
	}
	defer leaser.Release(ctx, wsLease)

	if err := l.Mirror.Fetch(ctx, l.Repo); err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	base, err := l.Mirror.Resolve(ctx, l.Repo, ref)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	// A resume point from an earlier attempt: this item already built a commit
	// and is back because a gate could not answer yet, or because it lost a
	// landing race. Rebuilding would buy the same change for the price of
	// another agent run — the most expensive thing the factory does.
	var ws forge.Workspace
	var head forge.Commit
	resumed := false
	// Set only in the rebase branch below, on a clean replay whose patch-id
	// against the moved base equals the one the kept commit had against its
	// old base: the sha changed but the diff a diff-judging gate cares about
	// did not, so that gate's verdict is carried forward in the gate stage
	// instead of re-run (issue #95).
	rebaseCarriesDiff := false
	rebasePatchID := ""
	if item.Built != "" {
		adopted, ok, aerr := l.Mirror.Adopt(ctx, wsLease, forge.Commit(item.Built), base)
		if aerr != nil {
			return q.Fail(ctx, item, claim, aerr.Error())
		}
		switch {
		case !ok:
			// No tree to take — a reclaimed disk, another runner. Build again:
			// rebuilding is always sound and reusing is not.
		case item.BuiltBase == string(base):
			ws, head, resumed = adopted, forge.Commit(item.Built), true
			// On the record, so the chain shows one commit judged twice rather
			// than two builds that happened to agree — or, when the resume is
			// unfinished, one change continued rather than two attempts at it.
			l.record(ctx, item, "build-resumed", string(head),
				fmt.Sprintf(`{"base":%q,"waiting_on":%q,"unfinished":%v}`, base, item.Note, item.Unfinished))
		default:
			// The branch moved while this change waited or lost its turn to
			// land. Replay rather than rebuild: the commit already exists, and
			// buying it a second time from an agent is exactly the cost that
			// makes concurrency on one branch not worth having. The gates run
			// again below, on the replayed content, which is what makes
			// reusing the commit safe rather than merely cheap.
			//
			// Bounded by the same budget as a build attempt (issue #95): a
			// rebase happens at most once per claim (only here), so a branch
			// that never stops moving would otherwise buy an unbounded run of
			// free rebase-and-regate cycles, one claim after another, with the
			// loop always one step behind. Past the bound a person decides.
			if budget := l.attemptBudget(item); item.RebaseCount >= budget {
				return q.ParkKeeping(ctx, item, claim, queue.SeverityMedium, fmt.Sprintf(
					"rebased onto a moving branch %d times, at the attempt budget of %d — parking rather than chasing it again",
					item.RebaseCount, budget))
			}
			// The OLD patch-id, read while item.Built is still HEAD in this
			// tree and its objects are certainly reachable: git.Rebase is
			// about to move HEAD out from under it.
			oldPatchID, oidErr := git.PatchID(ctx, adopted.Dir, forge.Commit(item.BuiltBase), forge.Commit(item.Built))
			replayed, rerr := git.Rebase(ctx, adopted.Dir, base, forge.Commit(item.BuiltBase))
			if rerr == nil {
				ws, head, resumed = adopted, replayed, true
				item.RebaseCount++
				newPatchID, nidErr := git.PatchID(ctx, adopted.Dir, base, replayed)
				// Equal, non-empty patch-ids on a clean rebase: the same patch
				// on a new base, so a diff-judging gate's verdict still
				// applies — carried below, in the gate stage. Anything else,
				// including either side erroring, is never treated as equal:
				// unknown never reads as zero.
				if oidErr == nil && nidErr == nil && oldPatchID != "" && oldPatchID == newPatchID {
					rebaseCarriesDiff, rebasePatchID = true, newPatchID
				}
				l.record(ctx, item, "build-rebased", string(replayed),
					fmt.Sprintf(`{"was":%q,"from":%q,"onto":%q,"unfinished":%v,"old_patch_id":%q,"new_patch_id":%q}`,
						item.Built, item.BuiltBase, base, item.Unfinished, oldPatchID, newPatchID))
			} else {
				// A conflict is a judgement call, and the agent is the one
				// who makes those. Recorded, then built afresh.
				l.record(ctx, item, "rebase-refused", item.Built,
					fmt.Sprintf(`{"onto":%q,"reason":%q}`, base, rerr.Error()))
			}
		}
	}
	if !resumed {
		// A fresh build (including one built afresh after a rebase conflict):
		// nothing here was replayed, so the rebase count starts over.
		item.RebaseCount = 0
		ws, err = l.Mirror.Checkout(ctx, wsLease, base)
		if err != nil {
			return q.Fail(ctx, item, claim, err.Error())
		}
	}
	// The gating ChangeSet carries no lease: the branch is not ours yet, and
	// gates judge content rather than authority. The land window fills it in.
	var cs forge.ChangeSet
	var verdicts []forge.Verdict
	var passedNow []string
	// The builder runs unless a FINISHED change was resumed. An unfinished
	// resume is the mid-run handoff: the successor takes the same tree at the
	// same partial commit and goes on from there, which is the difference
	// between handing off and parking. Skipping the builder on half a change
	// would gate and land it, which is the degraded commit the verb exists to
	// avoid producing.
	//
	// Bounded at [Loop.attemptBudget]: a resumed commit that skipped the
	// builder and still fails its gate gets exactly one pass built afresh —
	// informed by that failing verdict, now on its own evidence chain (see
	// acp.Builder.carried) — rather than another free recheck; the free
	// recheck spends none of the budget, because attempts are builds. Past
	// that, every refusal under budget goes straight to the next build in
	// this SAME claim, told what refused it and how many earlier attempts
	// this claim has already spent (see agent.Prior) — persisted before the
	// builder runs again, because acp.Builder loads its item from the queue
	// itself rather than from what Run holds here (see [queue.Queue.Retain]).
	// Only a refusal on the LAST attempt parks (issue #81).
	skipBuild := resumed && !item.Unfinished
	budget := l.attemptBudget(item)
	attempt := 0
	var history []string
	for {
		if !skipBuild {
			attempt++
			// A build about to run replaces head with a commit the rebase
			// branch above never saw, so the patch-id comparison it made no
			// longer says anything about what head will be once this
			// returns — carrying it forward would be applying an old
			// gate's verdict to a change nobody diffed against it (issue
			// #95, confirmed by review: a within-claim rebuild after a
			// carried rebase must not keep riding the stale comparison).
			rebaseCarriesDiff, rebasePatchID = false, ""
			if serr := telemetry.Stage(ctx, "build", item, func(ctx context.Context) error {
				bctx, cancelBuild := l.buildDeadline(ctx)
				defer cancelBuild()
				if l.Meter != nil {
					l.Meter.Start(ctx, item.ID)
				}
				var berr error
				head, berr = l.Builder.Build(bctx, ws, item.Task)
				if berr == nil {
					l.record(ctx, item, "built", string(head), "")
				} else if bctx.Err() != nil && ctx.Err() == nil {
					// The clock ran out, not the caller. Say so plainly: "context
					// deadline exceeded" on a parked item tells nobody which
					// deadline or how long it was.
					berr = fmt.Errorf("build exceeded its %s budget and was stopped: %w", l.buildTimeout(), berr)
				}
				return berr
			}); serr != nil {
				var ho *Handoff
				if errors.As(serr, &ho) {
					return l.handOff(ctx, q, item, claim, base, ho)
				}
				var esc *Escalation
				if errors.As(serr, &esc) {
					// The agent's own act, attributed to the agent: actor is the
					// seat, not the runner that hosted it.
					l.Store.Log().Append(ctx, evidence.Record{
						Tenant: item.Tenant, Scope: "workitem/" + item.ID,
						Kind: "escalated", Subject: item.ID, Actor: esc.Seat,
						OnBehalfOf: item.Requester, Body: []byte(esc.Reason),
						Trace: telemetry.TraceID(ctx),
					})
					// An agent declining is a considered act, not an emergency:
					// somebody has to read it, and it can wait for the morning.
					item, _ = q.Park(ctx, item, claim, queue.SeverityMedium, esc.Error())
					return l.settle(ctx, item), nil
				}
				item, _ = q.Park(ctx, item, claim, queue.SeverityMedium, "build: "+serr.Error())
				return l.settle(ctx, item), nil
			}
		}

		cs = forge.ChangeSet{
			ID: "cs-" + item.ID, Tenant: item.Tenant, Box: item.Box, Run: item.ID,
			Intent: item.Task, DrivingIssue: drivingIssue(item.OriginKey),
			Members: []forge.Member{{
				Repo: l.Repo.Name, Branch: l.Branch, Base: base, Head: head,
				Summary: item.Summary,
			}},
		}

		if l.Lane == forge.LaneOptimistic {
			return l.runOptimistic(ctx, q, item, claim, ws, cs, base, head)
		}

		// Gates already satisfied on THIS head are carried, not re-run. An item
		// waiting on a person is re-drained every tick for as long as the review
		// takes, and re-running the whole suite each time is how a worker that is
		// doing nothing keeps a machine busy: the deterministic shells — the
		// build, the test run — cannot change their mind about a commit that has
		// not moved. The gate whose answer CAN change is the human one, and it is
		// a store read. Any change of head clears the carry, because replayed or
		// rebuilt content is a different question — UNLESS the rebase branch
		// above found that question already answered: the sha changed but the
		// diff a diff-judging gate reads did not (issue #95).
		gates := l.gatesFor(item, ws)
		carried := item.PassedGates
		switch {
		case string(head) == item.Built:
			// Unchanged: every gate that already passed on this exact content
			// still applies.
		case rebaseCarriesDiff:
			// Same patch, new base: only the gates that judge the diff rather
			// than the base ride forward, and each one that does gets its own
			// carry record naming the verdict it rests on — see
			// [Loop.carryDiffGates]. A human approval never satisfies
			// [forge.DiffJudge] and so is never among them: it binds to the
			// exact commit a person looked at (docs/PLAN.md §6), not to a
			// patch shape.
			carried = l.carryDiffGates(ctx, item, gates, item.PassedGates, forge.Commit(item.Built), head, rebasePatchID)
		default:
			carried = nil
		}
		verdicts = nil
		passedNow = nil
		var gateInfra, gateWait error
		gerr := telemetry.Stage(ctx, "gate", item, func(ctx context.Context) error {
			for _, g := range gates {
				if slices.Contains(carried, g.Name()) {
					// No new evidence beyond what was already written above
					// (the same-head case needs none; the rebase-carry case
					// wrote its own "gate-carried" record): the gate ran once
					// on this content and its verdict is already on the
					// chain. Recording it again would be minting a judgement
					// nobody made.
					reason := "passed on " + short(head) + ", carried while the head stands"
					if rebaseCarriesDiff && string(head) != item.Built {
						reason = fmt.Sprintf("carried across a rebase: patch-id %s of the reviewed commit %s equals the patch-id of %s",
							rebasePatchID, short(forge.Commit(item.Built)), short(head))
					}
					verdicts = append(verdicts, forge.Verdict{
						Gate: g.Name(), Decision: forge.DecisionPass, Reason: reason,
					})
					passedNow = append(passedNow, g.Name())
					continue
				}
				v, tries, err := l.evaluateGate(ctx, item, g, cs)
				if err != nil {
					if errors.Is(err, capacity.ErrBudget) || errors.Is(err, capacity.ErrUnmetered) || errors.Is(err, capacity.ErrUnpriced) {
						// The meter's own refusal, not infrastructure trouble a
						// retry could clear: no gate today can hit this (the
						// meter refuses at pump.Acquire, before Run), so it can
						// only reach here by construction (a fake gate in
						// tests). Retrying or parking would both be the loop
						// second-guessing a rule that is not its to relax.
						gateWait = fmt.Errorf("gate %s: %w", g.Name(), err)
						return gateWait
					}
					// Infrastructure trouble is never recorded as a rejected
					// change, even after every retry this bound allowed.
					gateInfra = fmt.Errorf("gate %s could not conclude after %d tries: %v", g.Name(), tries, err)
					return gateInfra
				}
				vr := gateVerdictRecord{Verdict: v}
				if tries > 1 {
					vr.Try = tries
				}
				body, _ := json.Marshal(vr)
				rec := l.recordEvidence(ctx, item, "gate", string(head), string(body))
				if rec.ID != "" {
					// The chain's own id, threaded onto the verdict it
					// belongs to, so a Lander that publishes verdicts
					// outside this process (git.PRLander's GitHub check)
					// gives its reader a way back to the chain instead of
					// just an assertion.
					v.Evidence = append(v.Evidence, forge.EvidenceRef{Kind: forge.EvidenceKindRecord, URI: rec.ID})
				}
				if v.Decision == forge.DecisionPass {
					passedNow = append(passedNow, g.Name())
				}
				verdicts = append(verdicts, v)
				if v.Decision == forge.DecisionFail {
					// forge.Resolve's own rule, applied before the loop pays
					// for the gates after this one: a failure outranks a
					// pending verdict, so asking a reviewer to read a diff
					// that already failed its tests judges nothing Resolve
					// will not already discard.
					break
				}
			}
			if forge.Resolve(verdicts) == forge.DecisionFail {
				return fmt.Errorf("gates failed") // marks the stage span failed; the reasons go to evidence and the park note
			}
			return nil
		})
		if gateWait != nil {
			// The unit waits exactly as a refused start at pump.Acquire waits:
			// the window will turn, and nothing about this commit was judged.
			item.KeepCandidate(head, base, passedNow)
			return q.Wait(ctx, item, claim, gateWait.Error())
		}
		if gateInfra != nil {
			// Infrastructure trouble spends no attempt: the budget is for a
			// judgement the loop received and could act on, and an
			// inconclusive gate never gave one — that holds through every
			// retry, not just the first ask.
			//
			// Parking keeps the commit (issue #76) rather than failing it:
			// nothing here judged the change wrong, so a person's Retry
			// should buy the free recheck at the gate that [queue.ParkKeeping]
			// exists for, not a fresh build. SeverityMedium matches the
			// existing gate-refusal park below — this needs a look, but nothing
			// about it is an emergency tonight.
			item.KeepCandidate(head, base, passedNow)
			return q.ParkKeeping(ctx, item, claim, queue.SeverityMedium, gateInfra.Error())
		}
		_ = gerr

		if skipBuild && attempt == 0 && forge.Resolve(verdicts) == forge.DecisionFail {
			// The one free recheck a resumed park bought just failed again,
			// and it spent nothing: attempts are builds. Fall through to the
			// builder — same claim, same lease — and judge what it produces
			// as this claim's first real attempt.
			skipBuild = false
			continue
		}

		if forge.Resolve(verdicts) == forge.DecisionFail {
			reason := reasons(verdicts)
			if attempt < budget {
				// Under budget: feedback, not an exception. Told to the next
				// build through the SAME layer a repair wakes with — the
				// refused head, which gate said no, and every earlier
				// attempt this claim has already made, so a builder on its
				// third attempt is told it is the third rather than starting
				// cold on a commit that already failed twice.
				next := attempt + 1
				history = append(history, fmt.Sprintf("attempt %d was refused: %s", attempt, reason))
				prior := &agent.Prior{
					Run: item.ID, Repo: l.Repo.Name, Sha: string(head),
					Checks:  failing(verdicts),
					Reason:  fmt.Sprintf("attempt %d of %d; %s", next, budget, strings.Join(history, "; ")),
					Attempt: next,
					Of:      budget,
				}
				l.record(ctx, item, "attempt", string(head),
					fmt.Sprintf(`{"attempt":%d,"of":%d,"reason":%q}`, next, budget, reason))
				var rerr error
				item, claim, rerr = q.Retain(ctx, item, claim, prior, next)
				if rerr != nil {
					return q.Fail(ctx, item, claim, fmt.Sprintf("retaining attempt %d of %d: %v", next, budget, rerr))
				}
				continue
			}
			// The budget ran out. A refusal is never a pass by exhaustion —
			// nothing lands because attempts ran out — so this parks exactly
			// like any single gate refusal always has: kept as a resume
			// point, in case a human Retry (a fresh claim, a fresh budget)
			// wants the one free recheck that buys. See
			// [queue.Queue.ParkKeeping].
			item.KeepCandidate(head, base, passedNow)
			return q.ParkKeeping(ctx, item, claim, queue.SeverityMedium,
				fmt.Sprintf("refused on attempt %d of %d: %s", attempt, budget, reason))
		}

		if forge.Resolve(verdicts) == forge.DecisionPending {
			// Carry the commit forward so the next tick re-enters at the gate
			// rather than at the builder, and the satisfied gates with it so it
			// re-enters at the ONE gate that has not answered. Waiting on a
			// person is routine and can last days; it must cost neither an agent
			// run nor a test suite per tick to do it.
			item.KeepCandidate(head, base, passedNow)
			return q.Wait(ctx, item, claim, reasons(verdicts))
		}
		break
	}

	// Only the gated lane reaches here with a resolved verdict in hand — a
	// Lander that publishes it (git.PRLander's GitHub check) reads it off
	// the ChangeSet rather than a second, out-of-band channel.
	cs.Verdicts = verdicts

	if deny, err := l.authorize(ctx, item, authz.ActionLand); err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	} else if deny != "" {
		return q.Park(ctx, item, claim, queue.SeverityHigh, deny)
	}
	landLease, notOurTurn, err := l.openLandWindow(ctx, item, base)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	if notOurTurn != "" {
		// The gates passed on this head; if the branch was merely busy, next
		// tick lands without re-running them. If it moved, the replay changes
		// the head and the carry drops itself.
		item.KeepCandidate(head, base, passedNow)
		return q.Wait(ctx, item, claim, notOurTurn)
	}
	defer leaser.Release(ctx, landLease)
	cs.Members[0].Lease = landLease
	if lerr := telemetry.Stage(ctx, "land", item, func(ctx context.Context) error {
		_, err := l.lander(git.NewLander(l.Mirror, l.Store.Log())).Land(ctx, cs)
		if err == nil {
			telemetry.Outcome(ctx, queue.StateDone, string(head))
		}
		return err
	}); lerr != nil {
		// Keeping the commit: a landing the remote refused says nothing
		// about the change — every verdict on it stands — so a retry
		// resumes here, at the landing, and rebuilds nothing. The first
		// landing through the reviewer onto main was refused twice at
		// this step (a token scope, then a check run only an App may
		// post), and each retry built, checked and reviewed the same
		// change again.
		item.KeepCandidate(head, base, passedNow)
		return q.ParkKeeping(ctx, item, claim, queue.SeverityMedium, "land: "+lerr.Error())
	}
	// Green by construction: the gates passed before the land. Recorded all
	// the same, because the promotion predicate reads only the check chain.
	checks.Write(ctx, l.Store, item.Tenant, l.Repo.Name, string(head),
		checks.Status{Green: true, Lane: string(forge.LaneGated)}, l.Runner)
	item, err = q.Done(ctx, item, claim, head)
	return l.settle(ctx, item), err
}

// openLandWindow takes the branch for the length of a landing and confirms it
// still stands where this change was built. An empty reason means the window
// is open and the caller owns the lease until it releases it.
//
// This is the only part of a run that holds the branch. Fetch, checkout, the
// build, and in the gated lane the gates too, all run leaseless — narrowing
// the region the lease covers, never widening what may pass through it. The
// landing is fenced exactly as before: the leaser judges the token and the
// push presents the expected old value.
//
// A busy branch, or one that moved while we were building, is "not our turn"
// rather than anyone's problem. The caller waits with its commit kept as a
// resume point, and next tick replays it onto the new base for the price of a
// git operation and a gate run instead of another agent session.
func (l *Loop) openLandWindow(ctx context.Context, item queue.WorkItem, base forge.Commit) (forge.Lease, string, error) {
	leaser := l.Store.Leaser(l.Runner)
	ref := "refs/heads/" + l.Branch
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: item.Tenant, Repo: l.Repo.Name, Ref: ref, Run: item.ID, TTL: l.TTL,
	})
	if err != nil {
		return forge.Lease{}, fmt.Sprintf("branch %s is landing another change: %v", l.Branch, err), nil
	}
	// The mirror's own ref does not move when the lander pushes, so it has to
	// be asked again before it can be trusted as the expected old value.
	if err := l.Mirror.Fetch(ctx, l.Repo); err != nil {
		leaser.Release(ctx, lease)
		return forge.Lease{}, "", err
	}
	now, err := l.Mirror.Resolve(ctx, l.Repo, ref)
	if err != nil {
		leaser.Release(ctx, lease)
		return forge.Lease{}, "", err
	}
	if now != base {
		leaser.Release(ctx, lease)
		return forge.Lease{}, fmt.Sprintf("branch %s moved from %s to %s while this change was building",
			l.Branch, short(base), short(now)), nil
	}
	return lease, "", nil
}

func (l *Loop) buildTimeout() time.Duration {
	if l.BuildTimeout == 0 {
		return DefaultBuildTimeout
	}
	return l.BuildTimeout
}

// buildDeadline bounds one build, unless the Box asked for no bound.
func (l *Loop) buildDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	d := l.buildTimeout()
	if d < 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

func short(c forge.Commit) string {
	if len(c) > 12 {
		return string(c[:12])
	}
	return string(c)
}

// drivingIssue reads the GitHub issue number off an issue-route ingress's
// delivery key, carried onto the item as OriginKey (e.g. "issue-42"). Any
// other shape — a campaign or incident key, or none at all — is left empty
// rather than guessed at; see [queue.WorkItem.OriginKey] and
// [ingress.Ingress.Key].
func drivingIssue(originKey string) string {
	n, ok := strings.CutPrefix(originKey, "issue-")
	if !ok {
		return ""
	}
	return n
}

// lander is the one seam both land call sites share: the plain git.Lander a
// land constructs, wrapped by PRLander when the Box configures one. The
// underlying git mechanics are never replaced, only wrapped.
func (l *Loop) lander(inner *git.Lander) forge.Lander {
	if l.PRLander != nil {
		return l.PRLander(inner)
	}
	return inner
}

// runOptimistic is land-then-check: the lease and the expected-old-value
// check guard the landing exactly as in the gated lane — fencing is never
// optimistic — and the gates run afterwards as checks on the branch. A red
// check is re-run once before repair work opens — confirm the red before
// summoning anyone; nothing rolls back by itself.
func (l *Loop) runOptimistic(ctx context.Context, q *queue.Queue, item queue.WorkItem, claim forge.Lease, ws forge.Workspace, cs forge.ChangeSet, base, head forge.Commit) (queue.WorkItem, error) {
	if deny, err := l.authorize(ctx, item, authz.ActionLand); err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	} else if deny != "" {
		return q.Park(ctx, item, claim, queue.SeverityHigh, deny)
	}
	landLease, notOurTurn, err := l.openLandWindow(ctx, item, base)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	if notOurTurn != "" {
		item.KeepCandidate(head, base, nil)
		return q.Wait(ctx, item, claim, notOurTurn)
	}
	defer l.Store.Leaser(l.Runner).Release(ctx, landLease)
	cs.Members[0].Lease = landLease
	var landing forge.Landing
	if lerr := telemetry.Stage(ctx, "land", item, func(ctx context.Context) error {
		var err error
		landing, err = l.lander(git.NewLander(l.Mirror, l.Store.Log())).Land(ctx, cs)
		return err
	}); lerr != nil {
		// The landing itself was refused — a superseded token or a moved
		// branch. Nothing reached main, so there is nothing to repair, and
		// the commit is kept for the retry to land as it is; no gate has
		// run on this lane yet, so none is carried.
		item.KeepCandidate(head, base, nil)
		return q.ParkKeeping(ctx, item, claim, queue.SeverityMedium, "land: "+lerr.Error())
	}

	var verdicts []forge.Verdict
	var infra error
	telemetry.Stage(ctx, "post-land-check", item, func(ctx context.Context) error {
		for _, g := range l.gatesFor(item, ws) {
			v, err := g.Evaluate(ctx, cs)
			if err != nil {
				infra = fmt.Errorf("check %s could not conclude: %v", g.Name(), err)
				return infra
			}
			body, _ := json.Marshal(v)
			l.record(ctx, item, "gate", string(head), string(body))
			verdicts = append(verdicts, v)
		}
		if forge.Resolve(verdicts) != forge.DecisionPass {
			return fmt.Errorf("post-land checks not green")
		}
		return nil
	})
	if infra != nil {
		// The change landed; the judgement failed. That is an unchecked
		// commit on main — record red so promotion never takes it, and park
		// the item for a person.
		checks.Write(ctx, l.Store, item.Tenant, l.Repo.Name, string(head),
			checks.Status{Green: false, Lane: string(forge.LaneOptimistic), Note: infra.Error()}, l.Runner)
		return q.Park(ctx, item, claim, queue.SeverityHigh, infra.Error())
	}

	switch forge.Resolve(verdicts) {
	case forge.DecisionPending:
		// Below the production boundary there are no humans in any loop; a
		// gate that cannot answer without one does not belong in this lane.
		return item, fmt.Errorf("loop: optimistic lane cannot wait on a human gate — human approval binds at the production boundary; move gate to the promotion")
	case forge.DecisionFail:
		reason := reasons(verdicts)
		// Confirm the red before summoning anyone: a flaky check reds a
		// different sha every time, and sha-keyed idempotency in OpenRepair
		// cannot collapse those. The re-run keeps repair work for reds that
		// reproduce.
		confirmed, cerr := checks.Confirm(ctx, l.Store, item.Tenant, l.Repo.Name, string(head),
			string(forge.LaneOptimistic), reason, l.Runner,
			func(ctx context.Context) (bool, string, error) { return l.recheck(ctx, item, ws, cs, head) })
		if cerr != nil {
			// One red observed and no second judgement. The red record
			// stands — promotion never takes the commit — and the rest is a
			// person's call.
			return q.Park(ctx, item, claim, queue.SeverityHigh,
				"landed red, unconfirmed: "+reason+"; "+cerr.Error())
		}
		if !confirmed {
			// The red did not reproduce: the flaky record is the fact, the
			// commit is green on the chain, and nobody is summoned.
			item.Note = "landed green on re-run; flaky check recorded: " + reason
			item, derr := q.Done(ctx, item, claim, head)
			return l.settle(ctx, item), derr
		}
		note := "landed red: " + reason
		if l.Red != nil {
			// What the repair wakes knowing. The branch is named because
			// nothing rolled back — the change is still there, and a repair
			// that starts by making it again is the waste this carries
			// against. The notes are deliberately absent: this run has not
			// settled yet, so its handoff is not written, and the successor
			// resolves it at wake time from the seat that wrote it.
			// The reason names the PR it repairs when one was opened for
			// this landing, so the repair references it rather than
			// re-deriving it from the branch.
			priorReason := reason
			if len(landing.Landed) > 0 && landing.Landed[0].PRURL != "" {
				priorReason = fmt.Sprintf("%s; pr: %s", reason, landing.Landed[0].PRURL)
			}
			prior := agent.Prior{
				Run: item.ID, Repo: l.Repo.Name, Branch: l.Branch, Sha: string(head),
				Checks: failing(verdicts), Reason: priorReason,
				Verdicts: asPriorVerdicts(verdicts, 1),
			}
			repair, rerr := l.Red(ctx, item, prior)
			if rerr != nil {
				// The lane's whole safety argument is that a red main
				// summons a repair swarm. Main is red and the summons
				// failed, so the mechanism this lane rests on is the
				// thing that broke.
				return q.Park(ctx, item, claim, queue.SeverityCritical,
					note+" — AND opening repair failed: "+rerr.Error())
			}
			l.record(ctx, item, "repair-opened", string(head), fmt.Sprintf(`{"ingress":%q}`, repair))
			note += "; repair opened: " + repair
		}
		// The item landed its change; main being red is now the repair
		// item's problem. Done, with the note carrying the trail.
		item.Note = note
		item, derr := q.Done(ctx, item, claim, head)
		return l.settle(ctx, item), derr
	}

	checks.Write(ctx, l.Store, item.Tenant, l.Repo.Name, string(head),
		checks.Status{Green: true, Lane: string(forge.LaneOptimistic)}, l.Runner)
	item, derr := q.Done(ctx, item, claim, head)
	return l.settle(ctx, item), derr
}

// recheck is the second look at a red landing: the same gates over the same
// workspace, each verdict recorded like the first pass, under its own stage
// so the trace shows the confirmation ran. It reports whether the gates now
// pass and, if not, why; an error means the look could not conclude.
func (l *Loop) recheck(ctx context.Context, item queue.WorkItem, ws forge.Workspace, cs forge.ChangeSet, head forge.Commit) (bool, string, error) {
	var verdicts []forge.Verdict
	var infra error
	telemetry.Stage(ctx, "confirm-red", item, func(ctx context.Context) error {
		for _, g := range l.gatesFor(item, ws) {
			v, err := g.Evaluate(ctx, cs)
			if err != nil {
				infra = fmt.Errorf("check %s could not conclude: %v", g.Name(), err)
				return infra
			}
			body, _ := json.Marshal(v)
			l.record(ctx, item, "gate", string(head), string(body))
			verdicts = append(verdicts, v)
		}
		if forge.Resolve(verdicts) != forge.DecisionPass {
			return fmt.Errorf("red confirmed")
		}
		return nil
	})
	if infra != nil {
		return false, "", infra
	}
	if forge.Resolve(verdicts) == forge.DecisionPass {
		return true, "", nil
	}
	return false, reasons(verdicts), nil
}

// RequesterActor is the actor a run acts as: the runner itself as an agent,
// delegating for item.Requester when the two differ. authorize uses it to
// check authority; internal/daemon's human-approval gate uses the SAME
// derivation to disqualify a requester's own approval (directly, or through
// an agent acting for them — approval.SelfApproved compares against both
// the principal and OnBehalfOf) — one construction rather than two that
// could drift apart.
func RequesterActor(runner string, item queue.WorkItem) authz.Actor {
	actor := authz.Actor{Principal: authz.Principal{Subject: runner, Kind: authz.KindAgent, Tenant: item.Tenant}}
	if item.Requester != "" && item.Requester != runner {
		p := authz.Principal{Subject: item.Requester, Kind: authz.KindUser, Tenant: item.Tenant}
		actor.OnBehalfOf = &p
	}
	return actor
}

// authorize checks one action when enforcement is on, records the decision
// either way, and returns the park-worthy reason on denial. The evidence
// record carries the grant that permitted the action — why it was allowed,
// not only that it happened.
func (l *Loop) authorize(ctx context.Context, item queue.WorkItem, action authz.Action) (string, error) {
	if l.Authz == nil {
		return "", nil
	}
	actor := RequesterActor(l.Runner, item)
	d, err := l.Authz.Can(ctx, actor, action, authz.Resource{
		Tenant: item.Tenant, Box: item.Box, Repo: l.Repo.Name, RepoRole: string(l.Repo.Role),
	})
	if err != nil {
		return "", err
	}
	l.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: "authz", Subject: string(action), Actor: l.Runner,
		OnBehalfOf: item.Requester, Grant: d.Grant, Body: []byte(d.Reason),
		Trace: telemetry.TraceID(ctx),
	})
	if !d.Allow {
		return "not authorized: " + d.Reason, nil
	}
	return "", nil
}

// Escalation is an agent declining or escalating instead of grinding: a
// recorded act with the agent's own reason, not a contrived failure. The
// loop parks the item with the reason attributed to the seat.
type Escalation struct {
	Seat   string
	Reason string
}

// Error satisfies error.
func (e *Escalation) Error() string {
	return fmt.Sprintf("escalated by %s: %s", e.Seat, e.Reason)
}

// Handoff is an agent asking mid-run for a fresh session: not finished, and
// not stuck.
//
// That is the case neither of the other two answers tells the truth about. An
// escalation parks work nobody needs to look at and throws away what the
// session built; grinding on produces the degraded commit the working
// contract exists to prevent. "I am not finished, I am not stuck, and here is
// where I am" is a third thing, so it gets a third transition.
//
// The agent requests and the loop decides — the split worth taking from Gas
// City's request-restart, where the controller and not the agent ends the
// session. Here the deciding is [Loop.MaxHandoffs]: past it the item parks
// with the reason named, because a session able to summon its own successor
// forever is an unbounded spend with no landing.
type Handoff struct {
	Seat   string
	Reason string
	// Commit is what the session left, already committed in its workspace;
	// empty when it committed nothing. The tree is held and the successor
	// adopts it, so the next session continues the change rather than paying
	// an agent to make it again.
	Commit forge.Commit
}

// Error satisfies error.
func (h *Handoff) Error() string {
	return fmt.Sprintf("handed off by %s: %s", h.Seat, h.Reason)
}

// handOff ends the session and returns the item to the queue for a fresh one,
// unless the hops are spent.
func (l *Loop) handOff(ctx context.Context, q *queue.Queue, item queue.WorkItem, claim forge.Lease, base forge.Commit, h *Handoff) (queue.WorkItem, error) {
	// The agent's own act, attributed to the agent: actor is the seat, not the
	// runner that hosted it, exactly as an escalation is. Every hop is a
	// record, so "how many sessions did this change take" is answered from the
	// chain rather than estimated from it.
	l.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: "handoff-requested", Subject: item.ID, Actor: h.Seat,
		OnBehalfOf: item.Requester, Body: []byte(h.Reason),
		Trace: telemetry.TraceID(ctx),
	})
	if limit := l.maxHandoffs(); limit > 0 && item.Hops >= limit {
		// Spent. An item that has taken the limit and is still not finished
		// has stopped being a context problem and become a sizing one, which
		// is a judgement — and judgements are what the exception queue is for.
		// The reason is named rather than summarised: "handed off too often"
		// tells nobody what the sessions were trying to do.
		item, _ = q.Park(ctx, item, claim, queue.SeverityMedium, fmt.Sprintf(
			"handed off %d times without finishing, and the limit is %d — parking rather than opening another session: %s",
			item.Hops, limit, h.Reason))
		return l.settle(ctx, item), nil
	}
	if h.Commit != "" && h.Commit != base {
		// What the session got to, kept for its successor. Nothing landed and
		// no gate has judged it, so this is a tree to continue in rather than
		// a candidate to re-offer — see [queue.WorkItem.Unfinished].
		item.KeepUnfinished(h.Commit, base)
	}
	return q.HandOff(ctx, item, claim, h.Reason)
}

func (l *Loop) maxHandoffs() int {
	if l.MaxHandoffs == 0 {
		return DefaultMaxHandoffs
	}
	return l.MaxHandoffs
}

// attemptBudget resolves this item's attempt budget: the plan's per-node
// override wins, then the loop's own (repo-else-box, resolved at assemble),
// then [DefaultAttempts] — the same zero-means-default precedent
// [Loop.maxHandoffs] follows.
func (l *Loop) attemptBudget(item queue.WorkItem) int {
	switch {
	case item.Attempts > 0:
		return item.Attempts
	case l.Attempts > 0:
		return l.Attempts
	default:
		return DefaultAttempts
	}
}

// gateRetries bounds how many more times one gate is re-asked after it
// errors, before the item parks. Mirrors [Loop.attemptBudget]'s "zero means
// default" rule with no item-level override, because a gate error is the
// loop's own infrastructure concern, not a per-item budget.
func (l *Loop) gateRetries() int {
	if l.GateRetries > 0 {
		return l.GateRetries
	}
	return DefaultGateRetries
}

// gateBackoff returns how long to wait before re-asking a gate that just
// failed try, honoring [Loop.GateBackoff] when set.
func (l *Loop) gateBackoff(try int) time.Duration {
	if l.GateBackoff != nil {
		return l.GateBackoff(try)
	}
	return time.Duration(try) * defaultGateBackoffUnit
}

// gateVerdictRecord is the loop-local envelope for a concluded gate's
// verdict on the chain: [forge.Verdict] embedded so existing readers see
// exactly the keys they always have, plus which try concluded when a retry
// is the one that answered. forge.Verdict itself carries no such field —
// how many times the loop had to ask is the loop's bookkeeping, not the
// gate's judgement.
type gateVerdictRecord struct {
	forge.Verdict
	Try int `json:"try,omitempty"`
}

// evaluateGate re-asks g about cs while it returns an error, up to
// [Loop.gateRetries] more times, waiting [Loop.gateBackoff] between tries. It
// returns as soon as g concludes — a verdict, pass or fail, is never
// retried — and the try number that concluded (or, if every try errored,
// the try count and the LAST error).
//
// A meter refusal (capacity.ErrBudget, ErrUnmetered, ErrUnpriced) is
// returned immediately, un-retried, on the first try that hits it: the
// window's exhaustion, or a run that reported blind under a budget, is the
// meter's own rule standing, not infrastructure trouble a re-ask could
// clear. See the caller's Wait branch.
//
// Every erroring try — including the first, which is not yet a "retry" but
// is still a try nobody may act on as a verdict — is recorded on the chain,
// keyed to the SAME head the eventual verdict will be, in its own nested
// telemetry stage: the whole point of the bound is that a re-ask leaves a
// trace, not a silent redo.
func (l *Loop) evaluateGate(ctx context.Context, item queue.WorkItem, g forge.Gate, cs forge.ChangeSet) (forge.Verdict, int, error) {
	head := cs.Members[0].Head
	maxTries := 1 + l.gateRetries()
	var lastErr error
	for try := 1; try <= maxTries; try++ {
		v, err := g.Evaluate(ctx, cs)
		if err == nil {
			return v, try, nil
		}
		if errors.Is(err, capacity.ErrBudget) || errors.Is(err, capacity.ErrUnmetered) || errors.Is(err, capacity.ErrUnpriced) {
			return forge.Verdict{}, try, err
		}
		lastErr = err
		_ = telemetry.Stage(ctx, "gate-retry", item, func(ctx context.Context) error {
			l.record(ctx, item, "gate-try", string(head),
				fmt.Sprintf(`{"gate":%q,"try":%d,"error":%q}`, g.Name(), try, err.Error()))
			return err
		})
		if try == maxTries {
			break
		}
		if wait := l.gateBackoff(try); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return forge.Verdict{}, try, ctx.Err()
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			// A canceled ctx is not retried even when the backoff itself is
			// zero (tests): propagate it rather than spending one more try
			// on a context nobody is waiting for the result of.
			return forge.Verdict{}, try, ctx.Err()
		}
	}
	return forge.Verdict{}, maxTries, lastErr
}

// settle runs the handoff hook after any settle; closure over termination.
func (l *Loop) settle(ctx context.Context, item queue.WorkItem) queue.WorkItem {
	if l.Handoff == nil {
		return item
	}
	if err := l.Handoff(ctx, item); err != nil {
		l.record(ctx, item, "handoff-failed", item.ID, err.Error())
	}
	return item
}

func (l *Loop) record(ctx context.Context, item queue.WorkItem, kind, subject, body string) {
	l.recordEvidence(ctx, item, kind, subject, body)
}

// recordEvidence is [Loop.record] with the appended record handed back, for
// the one call site that needs to thread the chain's own id onward (the gate
// verdict, onto its [forge.Verdict]) rather than merely append and forget.
func (l *Loop) recordEvidence(ctx context.Context, item queue.WorkItem, kind, subject, body string) evidence.Record {
	if body == "" {
		b, _ := json.Marshal(item)
		body = string(b)
	}
	rec, _ := l.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: kind, Subject: subject, Actor: l.Runner, OnBehalfOf: item.Requester,
		Body: []byte(body), Trace: telemetry.TraceID(ctx),
	})
	return rec
}

// failing names the gates that did not pass, in the order they answered.
// "main is red" is a fact nobody can act on; "the test gate is red" is a
// place to start, and it is the half of the reason a successor can search on.
func failing(vs []forge.Verdict) []string {
	var out []string
	for _, v := range vs {
		if v.Decision != forge.DecisionPass {
			out = append(out, v.Gate)
		}
	}
	return out
}

// asPriorVerdicts renders a batch of in-hand verdicts as [agent.PriorVerdict]
// entries at a single attempt number, for a Prior built while the verdicts
// that produced it are still in scope — a fail is a refusal worth carrying;
// a pass or a pending answer is not.
func asPriorVerdicts(vs []forge.Verdict, attempt int) []agent.PriorVerdict {
	var out []agent.PriorVerdict
	for _, v := range vs {
		if v.Decision == forge.DecisionFail {
			out = append(out, agent.PriorVerdict{Gate: v.Gate, Decision: string(v.Decision), Reason: v.Reason, Attempt: attempt})
		}
	}
	return out
}

func reasons(vs []forge.Verdict) string {
	out := ""
	for _, v := range vs {
		if v.Decision != forge.DecisionPass {
			if out != "" {
				out += "; "
			}
			line := v.Reason
			if i := indexByte(line, '\n'); i >= 0 {
				line = line[:i]
			}
			out += fmt.Sprintf("%s: %s (%s)", v.Gate, v.Decision, line)
		}
	}
	if out == "" {
		out = "no gate examined the change"
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// SectionRef is the lease ref a section of a repository is held under.
func SectionRef(name string) string { return "section/" + name }

// leaseSections takes the section leases a unit needs, in one order for every
// taker so two units cannot deadlock over two sections. It returns a release
// for what it holds; a non-empty busy names the section somebody else holds,
// and who, and means nothing is held. An error names a section the plan and
// the repository disagree about.
func (l *Loop) leaseSections(ctx context.Context, item queue.WorkItem) (release func(), busy string, err error) {
	for _, n := range item.Sections {
		if _, ok := l.Repo.Section(n); !ok {
			return nil, "", fmt.Errorf("section %q is not declared by repository %s (declared: %v) — the plan and the repository disagree about where this unit works",
				n, l.Repo.Name, l.Repo.SectionNames())
		}
	}
	names := item.Sections
	if len(names) == 0 {
		names = l.Repo.SectionNames() // the whole repository is every section
	}
	if len(names) == 0 {
		return func() {}, "", nil
	}
	names = slices.Clone(names)
	slices.Sort(names)
	leaser := l.Store.Leaser(l.Runner)
	var held []forge.Lease
	release = func() {
		for _, h := range held {
			leaser.Release(ctx, h)
		}
	}
	for _, n := range names {
		lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
			Tenant: item.Tenant, Repo: l.Repo.Name, Ref: SectionRef(n), Run: item.ID, TTL: l.TTL,
		})
		if errors.Is(err, forge.ErrHeld) {
			holder := ""
			if cur, ok, ierr := leaser.Inspect(ctx, item.Tenant, l.Repo.Name, SectionRef(n)); ierr == nil && ok {
				holder = " by " + cur.Run
			}
			release()
			return nil, fmt.Sprintf("section %s of %s is held%s", n, l.Repo.Name, holder), nil
		}
		if err != nil {
			release()
			return nil, "", err
		}
		held = append(held, lease)
	}
	return release, "", nil
}
