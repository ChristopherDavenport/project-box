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
	"time"

	"github.com/ChristopherDavenport/project-box/internal/authz"
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
	Store   store.Store
	Runner  string
	Mirror  *git.Mirror
	Repo    forge.Repo
	Branch  string
	Builder Builder
	Gates   Gates
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
	// flaky check, and nobody is summoned for it. Returns the repair ingress
	// id. Nil means even a confirmed red is recorded but nobody is summoned
	// — configure it; see OpenRepair.
	Red func(ctx context.Context, item queue.WorkItem, head forge.Commit, reason string) (string, error)
	TTL time.Duration
}

// OpenRepair wires Red to an ingress route: the red commit's sha is the
// delivery key, so however many post-land checks report the same red, one
// repair item exists.
func OpenRepair(router *ingress.StoreRouter, route ingress.Route) func(context.Context, queue.WorkItem, forge.Commit, string) (string, error) {
	return func(ctx context.Context, _ queue.WorkItem, head forge.Commit, reason string) (string, error) {
		ing, _, err := router.Open(ctx, route, ingress.Delivery{Key: "red-" + string(head), Payload: []byte(reason)})
		if err != nil {
			return "", err
		}
		return ing.ID, nil
	}
}

// ItemFor turns an admitted ingress into a work item. The ingress link is
// what keeps "why does this change exist" answerable from the other end.
func ItemFor(ing ingress.Ingress, id, task string) queue.WorkItem {
	return queue.WorkItem{ID: id, Tenant: ing.Tenant, Box: ing.Box, Ingress: ing.ID,
		Requester: ing.Requester, Repo: ing.Repo, Task: task}
}

// Run drives one item from claim to settled state. The returned item reports
// where it ended: done, parked, waiting, or failed — an error return means
// the loop itself broke, not that the change was judged and refused.
func (l *Loop) Run(ctx context.Context, item queue.WorkItem) (out queue.WorkItem, rerr error) {
	if err := forge.ValidateLane(l.Lane, l.Repo.Role); err != nil {
		return item, err
	}
	// The workspace outlives the lease for exactly as long as the resume point
	// does: held while the item waits with a commit to come back to, handed
	// back to Prune the moment it settles or gives that commit up.
	defer func() {
		if out.State == queue.StateWaiting && out.Built != "" {
			l.Mirror.Hold(item.ID, l.Repo.Name)
			return
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
		return q.Park(ctx, item, claim, deny)
	}

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
			// than two builds that happened to agree.
			l.record(ctx, item, "build-resumed", string(head),
				fmt.Sprintf(`{"base":%q,"waiting_on":%q}`, base, item.Note))
		default:
			// The branch moved while this change waited or lost its turn to
			// land. Replay rather than rebuild: the commit already exists, and
			// buying it a second time from an agent is exactly the cost that
			// makes concurrency on one branch not worth having. The gates run
			// again below, on the replayed content, which is what makes
			// reusing the commit safe rather than merely cheap.
			replayed, rerr := git.Rebase(ctx, adopted.Dir, base, forge.Commit(item.BuiltBase))
			if rerr == nil {
				ws, head, resumed = adopted, replayed, true
				l.record(ctx, item, "build-rebased", string(replayed),
					fmt.Sprintf(`{"was":%q,"from":%q,"onto":%q}`, item.Built, item.BuiltBase, base))
			} else {
				// A conflict is a judgement call, and the agent is the one
				// who makes those. Recorded, then built afresh.
				l.record(ctx, item, "rebase-refused", item.Built,
					fmt.Sprintf(`{"onto":%q,"reason":%q}`, base, rerr.Error()))
			}
		}
	}
	if !resumed {
		ws, err = l.Mirror.Checkout(ctx, wsLease, base)
		if err != nil {
			return q.Fail(ctx, item, claim, err.Error())
		}
		if serr := telemetry.Stage(ctx, "build", item, func(ctx context.Context) error {
			var berr error
			head, berr = l.Builder.Build(ctx, ws, item.Task)
			if berr == nil {
				l.record(ctx, item, "built", string(head), "")
			}
			return berr
		}); serr != nil {
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
				item, _ = q.Park(ctx, item, claim, esc.Error())
				return l.settle(ctx, item), nil
			}
			item, _ = q.Park(ctx, item, claim, "build: "+serr.Error())
			return l.settle(ctx, item), nil
		}
	}

	// The gating ChangeSet carries no lease: the branch is not ours yet, and
	// gates judge content rather than authority. The land window fills it in.
	cs := forge.ChangeSet{
		ID: "cs-" + item.ID, Tenant: item.Tenant, Box: item.Box, Run: item.ID,
		Intent: item.Task,
		Members: []forge.Member{{
			Repo: l.Repo.Name, Branch: l.Branch, Base: base, Head: head,
		}},
	}

	if l.Lane == forge.LaneOptimistic {
		return l.runOptimistic(ctx, q, item, claim, ws, cs, base, head)
	}

	var verdicts []forge.Verdict
	var gateInfra error
	gerr := telemetry.Stage(ctx, "gate", item, func(ctx context.Context) error {
		for _, g := range l.Gates(ws) {
			v, err := g.Evaluate(ctx, cs)
			if err != nil {
				// Infrastructure trouble is never recorded as a rejected change.
				gateInfra = fmt.Errorf("gate %s could not conclude: %v", g.Name(), err)
				return gateInfra
			}
			body, _ := json.Marshal(v)
			l.record(ctx, item, "gate", string(head), string(body))
			verdicts = append(verdicts, v)
		}
		if forge.Resolve(verdicts) == forge.DecisionFail {
			return fmt.Errorf("gates failed") // marks the stage span failed; the reasons go to evidence and the park note
		}
		return nil
	})
	if gateInfra != nil {
		return q.Fail(ctx, item, claim, gateInfra.Error())
	}
	_ = gerr
	switch forge.Resolve(verdicts) {
	case forge.DecisionFail:
		return q.Park(ctx, item, claim, reasons(verdicts))
	case forge.DecisionPending:
		// Carry the commit forward so the next tick re-enters at the gate
		// rather than at the builder. Waiting on a person is routine and can
		// last days; it must not cost an agent run per tick to do it.
		item.Built, item.BuiltBase = string(head), string(base)
		return q.Wait(ctx, item, claim, reasons(verdicts))
	}

	if deny, err := l.authorize(ctx, item, authz.ActionLand); err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	} else if deny != "" {
		return q.Park(ctx, item, claim, deny)
	}
	landLease, notOurTurn, err := l.openLandWindow(ctx, item, base)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	if notOurTurn != "" {
		item.Built, item.BuiltBase = string(head), string(base)
		return q.Wait(ctx, item, claim, notOurTurn)
	}
	defer leaser.Release(ctx, landLease)
	cs.Members[0].Lease = landLease
	if lerr := telemetry.Stage(ctx, "land", item, func(ctx context.Context) error {
		_, err := git.NewLander(l.Mirror, l.Store.Log()).Land(ctx, cs)
		if err == nil {
			telemetry.Outcome(ctx, queue.StateDone, string(head))
		}
		return err
	}); lerr != nil {
		return q.Park(ctx, item, claim, "land: "+lerr.Error())
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

func short(c forge.Commit) string {
	if len(c) > 12 {
		return string(c[:12])
	}
	return string(c)
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
		return q.Park(ctx, item, claim, deny)
	}
	landLease, notOurTurn, err := l.openLandWindow(ctx, item, base)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	if notOurTurn != "" {
		item.Built, item.BuiltBase = string(head), string(base)
		return q.Wait(ctx, item, claim, notOurTurn)
	}
	defer l.Store.Leaser(l.Runner).Release(ctx, landLease)
	cs.Members[0].Lease = landLease
	if lerr := telemetry.Stage(ctx, "land", item, func(ctx context.Context) error {
		_, err := git.NewLander(l.Mirror, l.Store.Log()).Land(ctx, cs)
		return err
	}); lerr != nil {
		// The landing itself was refused — a superseded token or a moved
		// branch. Nothing reached main, so there is nothing to repair.
		return q.Park(ctx, item, claim, "land: "+lerr.Error())
	}

	var verdicts []forge.Verdict
	var infra error
	telemetry.Stage(ctx, "post-land-check", item, func(ctx context.Context) error {
		for _, g := range l.Gates(ws) {
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
		return q.Park(ctx, item, claim, infra.Error())
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
			return q.Park(ctx, item, claim, "landed red, unconfirmed: "+reason+"; "+cerr.Error())
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
			repair, rerr := l.Red(ctx, item, head, reason)
			if rerr != nil {
				return q.Park(ctx, item, claim, note+" — AND opening repair failed: "+rerr.Error())
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
		for _, g := range l.Gates(ws) {
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

// authorize checks one action when enforcement is on, records the decision
// either way, and returns the park-worthy reason on denial. The evidence
// record carries the grant that permitted the action — why it was allowed,
// not only that it happened.
func (l *Loop) authorize(ctx context.Context, item queue.WorkItem, action authz.Action) (string, error) {
	if l.Authz == nil {
		return "", nil
	}
	actor := authz.Actor{Principal: authz.Principal{Subject: l.Runner, Kind: authz.KindAgent, Tenant: item.Tenant}}
	if item.Requester != "" && item.Requester != l.Runner {
		p := authz.Principal{Subject: item.Requester, Kind: authz.KindUser, Tenant: item.Tenant}
		actor.OnBehalfOf = &p
	}
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
	if body == "" {
		b, _ := json.Marshal(item)
		body = string(b)
	}
	l.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: kind, Subject: subject, Actor: l.Runner, OnBehalfOf: item.Requester,
		Body: []byte(body), Trace: telemetry.TraceID(ctx),
	})
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
