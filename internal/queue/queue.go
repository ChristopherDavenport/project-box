// Package queue is the minimal work queue of the first closed loop: submit,
// claim, park, finish.
//
// State is not a mutable row. An item's state is a fold over its evidence
// scope — the transitions ARE the records — so "evidence at every transition"
// is not instrumentation on the queue, it is the queue. Claims are leases, so
// exclusivity and fencing come from the store's conformance-tested properties
// rather than a second locking scheme.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// State is where an item is in its life.
type State string

const (
	StatePending State = "pending"
	StateClaimed State = "claimed"
	// StateWaiting is a pending gate: work proceeding normally that has not
	// finished. Distinct from parked — conflating them buries real exceptions
	// under routine review latency.
	StateWaiting State = "waiting"
	// StateParked is the exception queue: something went wrong and a person
	// or a later run has to look.
	StateParked State = "parked"
	StateDone   State = "done"
	StateFailed State = "failed"
	// StateResolved is an exception a person closed by hand: the item needed
	// no landing, and someone said so on the record.
	//
	// Not StateDone, which means a commit reached the branch and feeds the
	// landings index that review sampling draws from — an auditor pulling a
	// sampled item expects a sha. Not StateFailed either: an agent reporting
	// that an issue was already implemented did its job, and filing that
	// verdict as a failure would make the one honest answer look like the
	// crash it is meant to be distinguished from.
	StateResolved State = "resolved"
)

// Severity is how loudly a parking asks for a person, set by the stage that
// parked it — the only place that knows whether a failing unit test and a
// revoked credential are the same emergency.
//
// It is not a priority: nothing claims work in severity order. It exists so
// the control room can decide who has to be told and how soon, which is the
// difference between an exception queue and a list nobody opens.
type Severity string

const (
	// SeverityLow is an exception the queue itself is enough for: somebody
	// will see it when they next look, and nobody needs telling before then.
	SeverityLow Severity = "low"
	// SeverityMedium is the default, and it is deliberately not the quietest
	// one. A stage that did not say has not thereby said "routine" — silence
	// must never read as "nothing to see", here as everywhere else.
	SeverityMedium Severity = "medium"
	// SeverityHigh is work that has stopped and will stay stopped until a
	// person acts: an authority the factory does not hold, a judgement it
	// could not obtain, a landing it cannot explain.
	SeverityHigh Severity = "high"
	// SeverityCritical is a mechanism the factory depends on having failed,
	// so the ordinary recovery path is gone too.
	SeverityCritical Severity = "critical"
)

// Rank orders severities. Anything unrecognised ranks with [SeverityMedium];
// see that constant for why an unknown value is not the quiet one.
func (s Severity) Rank() int {
	switch s {
	case SeverityLow:
		return 1
	case SeverityHigh:
		return 3
	case SeverityCritical:
		return 4
	}
	return 2
}

// OrDefault resolves an unset or unrecognised severity to [SeverityMedium].
func (s Severity) OrDefault() Severity {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return s
	}
	return SeverityMedium
}

// AtLeast reports whether s is at least as loud as other.
func (s Severity) AtLeast(other Severity) bool { return s.Rank() >= other.Rank() }

// WorkItem is one claimable unit of work.
type WorkItem struct {
	ID     string    `json:"id"`
	Tenant tenant.ID `json:"tenant"`
	Box    string    `json:"box"`
	// Ingress is the origination this item traces to. Required: there is no
	// orphan work, and the queue is where that invariant is enforced rather
	// than hoped for.
	Ingress string `json:"ingress"`
	// Requester is the principal whose authority this work exercises,
	// inherited from the ingress. It rides every evidence record as
	// OnBehalfOf: an audit must separate a change a person made from one an
	// automation made for them, and a log that never drew that line cannot
	// have it recovered later.
	Requester string `json:"requester,omitempty"`
	// Repo is the target repository within the Box, empty when the Box has
	// exactly one executor.
	Repo string `json:"repo,omitempty"`
	// DependsOn names items that must settle DONE before this one is
	// claimable — the work graph. A parked or failed dependency parks its
	// dependents with the cause named; execution order is nobody's plan,
	// it emerges from claims against the graph.
	DependsOn []string `json:"depends_on,omitempty"`
	Task      string   `json:"task"`
	State     State    `json:"state"`
	// Depth is how many times the factory has reopened this line of work on
	// its own initiative, inherited from the ingress. Externally delivered
	// work is 0; see loop.OpenRepair for what bounds it.
	Depth int `json:"depth,omitempty"`
	// Prior is the attempt this item follows, inherited from the ingress
	// when the factory opened this work to repair one of its own: the red
	// sha, which check reported it, and the branch that still carries the
	// change. It resolves into a layer at wake time, so a repair run's
	// bundle — and therefore its agentref — records what it was woken
	// knowing. See [agent.Prior].
	Prior *agent.Prior `json:"prior,omitempty"`
	// Head is the commit the item produced, once it has.
	Head string `json:"head,omitempty"`
	// Built is a commit this item has already produced but not landed: the
	// resume point for an item waiting on a gate. Kept apart from Head, which
	// is what a settled item landed — an item that is merely waiting must not
	// read as finished to anything folding the chain.
	//
	// The point of carrying it is cost. A waiting item is re-drained every
	// tick, and rebuilding is the single most expensive thing the factory
	// does; the commit is already made, and the gate that said "not yet" will
	// judge the same commit next time.
	Built string `json:"built,omitempty"`
	// BuiltBase is the branch commit Built was written on top of. The resume
	// is sound only while the branch still stands there: past that the change
	// was judged against a base that no longer exists, and a rebuild is the
	// honest answer rather than a cheap one.
	BuiltBase string `json:"built_base,omitempty"`
	// PassedGates names the gates that already returned pass on Built.
	//
	// They are carried rather than re-run while that head stands. A gate that
	// passed on this exact content passed, and asking it again every tick is
	// how an item that is only waiting on a person keeps a machine busy — the
	// cheap gate is the one whose answer can change, and the expensive ones
	// are the deterministic shells. Any change of head clears this, because a
	// different commit is a different question.
	PassedGates []string `json:"passed_gates,omitempty"`
	// Note carries the parked/failed reason.
	Note string `json:"note,omitempty"`
	// Severity is how loudly this parking asks for a person. Set by the stage
	// that parked it, meaningful only while the item is parked, and dropped
	// on the way back off the exception queue.
	Severity Severity `json:"severity,omitempty"`
}

// ErrStopped reports that the stop button is down: no new claims, tenant-wide,
// until an operator resumes. In-flight work finishes or parks on its own; the
// stop is about not STARTING anything while a human decides.
var ErrStopped = errors.New("queue: claims are stopped")

// ErrBlocked reports unsettled dependencies: not a failure, just not yet.
var ErrBlocked = errors.New("queue: dependencies not yet settled")

// ErrDependencyFailed reports that a dependency parked or failed; the item
// has been parked in cascade, naming the cause.
var ErrDependencyFailed = errors.New("queue: dependency parked or failed")

// ErrNoItem reports that an id names no work item.
//
// Distinct from a store or decoding failure on purpose, because intake keys
// on the difference: "there is no item yet" means create one, while "I could
// not tell" must never mean create a second. Reading any error as absence is
// how one unreadable chain became a new queued record on every tick.
var ErrNoItem = errors.New("queue: no such item")

// stateKinds are the record kinds whose body is the WHOLE work item.
//
// An item's chain carries two sorts of record and always has. The state
// transitions ARE the item: their body is the marshalled WorkItem, and
// folding them in order is what [Queue.Load] means. Everything else is the
// narrative — which agent ran under which ref, what a gate decided, why a
// seat escalated, what a handoff failed on — and its body is whatever that
// event needed to say, which is frequently prose.
//
// So the fold must SELECT, not merely unmarshal. It used to unmarshal every
// record in the scope, and both halves of that mistake were load-bearing:
// valid JSON that was not an item (an agent-run's agentref) zeroed the item
// for the length of the run, and prose (an escalation's reason) failed
// outright and took down every reader at once — Load, the pending drain, the
// queue listing, and the exception queue that existed to show the escalation.
// A factory stopped draining because an agent said, correctly, that it had
// nothing to do.
var stateKinds = map[string]bool{
	"queued":    true,
	"claimed":   true,
	"built":     true,
	"waiting":   true,
	"parked":    true,
	"done":      true,
	"failed":    true,
	"resolved":  true,
	"retried":   true,
	"recovered": true,
}

// StateRecord reports whether a record kind carries the whole work item and
// therefore takes part in the fold.
//
// This is the contract between the writers and the readers of an item's
// chain, and it binds both directions: a writer using one of these kinds MUST
// write the whole item, and a writer of anything else must not. TestFoldSkips
// holds the writers to it.
func StateRecord(kind string) bool { return stateKinds[kind] }

// StateKinds is that vocabulary as a sorted list.
//
// Exported because the fold is not its only reader: the control room derives
// a stage column from the same kinds, and the two drifted the moment three
// were added here and nowhere else. A recovered item went on reporting
// "building" — the stale stage of the run that abandoned it — which is the
// one reading an operator must never be given, since it names work in
// progress that nobody is doing. Whoever reads this vocabulary can now be
// held to covering all of it.
func StateKinds() []string {
	out := make([]string, 0, len(stateKinds))
	for k := range stateKinds {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Queue submits, claims, and settles work items.
type Queue struct {
	Store  store.Store
	Runner string
	// TTL bounds a claim; a runner that dies mid-item frees it by expiry.
	TTL time.Duration
}

func scope(id string) string { return "workitem/" + id }

// Submit records a new pending item. An item with no ingress is refused.
func (q *Queue) Submit(ctx context.Context, w WorkItem) (WorkItem, error) {
	if w.Ingress == "" {
		return WorkItem{}, errors.New("queue: work item traces to no ingress — orphan work is refused")
	}
	w.State = StatePending
	if err := q.record(ctx, w, "queued", w.ID, ""); err != nil {
		return WorkItem{}, err
	}
	return w, q.index(ctx, w.Tenant, "pending", w.ID, w.Task)
}

// Claim takes the item's lease. The lease, not the state fold, is the
// exclusivity: two runners folding the same chain still cannot both claim.
//
// Every claim first consults the stop flag, which is the stop button's stated
// bound: not one new claim after the stop record lands.
func (q *Queue) Claim(ctx context.Context, w WorkItem) (WorkItem, forge.Lease, error) {
	if stopped, err := q.Stopped(ctx, w.Tenant); err != nil {
		return w, forge.Lease{}, err
	} else if stopped {
		return w, forge.Lease{}, ErrStopped
	}
	for _, dep := range w.DependsOn {
		d, err := q.Load(ctx, w.Tenant, dep)
		if err != nil {
			return w, forge.Lease{}, fmt.Errorf("queue: dependency %s of %s: %w", dep, w.ID, err)
		}
		switch d.State {
		case StateDone:
		case StateParked, StateFailed:
			// Cascade: the dependent parks too, and its note names the
			// cause — a leaf must never park mysteriously when its root is
			// what died.
			w.State = StateParked
			w.Note = fmt.Sprintf("dependency %s %s: %s", dep, d.State, d.Note)
			// The cause's severity, not a fresh one. A leaf must no more
			// read as routine than it may park mysteriously when its root
			// is what died.
			w.Severity = d.Severity.OrDefault()
			if rerr := q.record(ctx, w, "parked", w.ID, w.Note); rerr != nil {
				return w, forge.Lease{}, rerr
			}
			if rerr := q.index(ctx, w.Tenant, "exceptions", w.ID, w.Note); rerr != nil {
				return w, forge.Lease{}, rerr
			}
			return w, forge.Lease{}, fmt.Errorf("%w: %s is %s", ErrDependencyFailed, dep, d.State)
		default:
			return w, forge.Lease{}, fmt.Errorf("%w: %s is %s", ErrBlocked, dep, d.State)
		}
	}
	lease, err := q.Store.Leaser(q.Runner).Acquire(ctx, forge.LeaseRequest{
		Tenant: w.Tenant, Repo: "_queue", Ref: scope(w.ID), Run: w.ID, TTL: q.TTL,
	})
	if err != nil {
		return w, forge.Lease{}, err
	}
	w.State = StateClaimed
	if err := q.record(ctx, w, "claimed", w.ID, ""); err != nil {
		return w, lease, err
	}
	return w, lease, nil
}

// Park moves the item to the exception queue and frees its claim. The
// exception index is what the control room lists; an unparked exception queue
// is a factory nobody can triage.
//
// The severity is the parking stage's to state, and it is a parameter rather
// than a field so that stating it is not optional. A stage that would have
// forgotten a field cannot forget an argument, and the room's routing is only
// as good as the loudest thing it is told.
func (q *Queue) Park(ctx context.Context, w WorkItem, lease forge.Lease, sev Severity, reason string) (WorkItem, error) {
	w.State, w.Note, w.Severity = StateParked, reason, sev.OrDefault()
	w.dropResume()
	err := q.record(ctx, w, "parked", w.ID, reason)
	if err == nil {
		err = q.index(ctx, w.Tenant, "exceptions", w.ID, reason)
	}
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// dropResume clears the resume point. Every settle does this: the built
// commit either landed or was judged and refused, so a later attempt must
// build afresh rather than re-offer a commit whose verdict is on the record.
// Wait is the one transition that keeps it, because waiting is the one state
// the same commit is still the answer to.
func (w *WorkItem) dropResume() { w.Built, w.BuiltBase, w.PassedGates = "", "", nil }

// Wait records a pending gate and frees the claim; the item resumes when the
// gate answers. It keeps any resume point the caller set on the item — see
// [WorkItem.Built].
func (q *Queue) Wait(ctx context.Context, w WorkItem, lease forge.Lease, reason string) (WorkItem, error) {
	w.State, w.Note = StateWaiting, reason
	err := q.record(ctx, w, "waiting", w.ID, reason)
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// Done settles the item, keyed by the commit it produced — the record an
// auditor sampling that sha will pull first.
func (q *Queue) Done(ctx context.Context, w WorkItem, lease forge.Lease, head forge.Commit) (WorkItem, error) {
	w.State, w.Head = StateDone, string(head)
	w.dropResume()
	err := q.record(ctx, w, "done", string(head), "")
	if err == nil {
		// The landings index feeds review sampling: an auditor-shaped list of
		// every settled change, cheap to walk.
		err = q.index(ctx, w.Tenant, "landings", w.ID, string(head))
	}
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// Fail settles the item unsuccessfully.
func (q *Queue) Fail(ctx context.Context, w WorkItem, lease forge.Lease, reason string) (WorkItem, error) {
	w.State, w.Note = StateFailed, reason
	w.dropResume()
	err := q.record(ctx, w, "failed", w.ID, reason)
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// Recover returns to the queue a claimed item whose runner is gone.
//
// StateClaimed was written in one place and read in none. Drain selects only
// pending and waiting, so an item whose runner died mid-run — a crash, a
// SIGKILL, a drain window that elapsed with work still in flight — stayed
// claimed forever: its lease lapsed after the TTL, nothing noticed, and the
// control room went on reporting it as building. The shutdown path even says
// so out loud, "fencing makes re-runs safe", which is true and beside the
// point. Fencing makes a re-run SAFE. It does not make one HAPPEN.
//
// Liveness needs no new primitive. Acquire already refuses a ref that a live
// claim holds, so taking the lease is itself the proof the holder is gone; a
// runner still working gets ErrHeld and is left entirely alone. A runner that
// is alive but stalled past its TTL is recovered and re-run, which is the
// stalled-runner case the fencing was built for in the first place.
func (q *Queue) Recover(ctx context.Context, w WorkItem) (WorkItem, bool, error) {
	if w.State != StateClaimed {
		return w, false, nil
	}
	leaser := q.Store.Leaser(q.Runner)
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: w.Tenant, Repo: "_queue", Ref: scope(w.ID), Run: w.ID, TTL: q.TTL,
	})
	switch {
	case errors.Is(err, forge.ErrHeld):
		return w, false, nil // somebody is still working on it
	case err != nil:
		return w, false, err
	}
	w.State = StatePending
	w.Note = "recovered: claimed by a runner that is no longer holding its lease"
	w.dropResume()
	rerr := q.record(ctx, w, "recovered", w.ID, w.Note)
	// Hold it only long enough to prove it was free. Releasing here rather
	// than deferring keeps the next claim — possibly this same tick's — from
	// contending with the probe that freed it.
	leaser.Release(ctx, lease)
	if rerr != nil {
		return w, false, rerr
	}
	return w, true, q.index(ctx, w.Tenant, "pending", w.ID, w.Note)
}

// Resolve closes a parked item without a landing: the operator read the
// exception, agreed there is nothing to build, and said so.
//
// The exception queue had no way out. StateParked was written in one place
// and read in five, and nothing anywhere moved an item off it — so every
// parked item was permanent, the queue only ever grew, and the verdict an
// escalating agent had gone to the trouble of writing ("this issue is already
// implemented, close it") was a recommendation the system could not act on.
// An exception queue nobody can clear is a list, not a queue.
func (q *Queue) Resolve(ctx context.Context, t tenant.ID, id, actor, why string) (WorkItem, error) {
	if why == "" {
		return WorkItem{}, errors.New("queue: resolve needs a reason — closing an exception is a judgement and the record must carry whose")
	}
	return q.reopen(ctx, t, id, actor, StateResolved, "resolved", why)
}

// Retry returns a parked item to the queue for another attempt, with the
// operator's note replacing the one it parked with.
//
// The counterpart to Resolve, and the reason parking could not simply be
// terminal: an item parks for transient reasons too — a flaky gate, a
// timeout, an agent that ran out of room — and without this the factory could
// never recover work it had already been asked to do. The resume point is
// already gone (Park drops it), so a retry builds afresh rather than
// re-offering a commit whose verdict is on the record.
func (q *Queue) Retry(ctx context.Context, t tenant.ID, id, actor, note string) (WorkItem, error) {
	return q.reopen(ctx, t, id, actor, StatePending, "retried", note)
}

// reopen is the one transition off the exception queue, in both directions it
// can go. Only a parked item may take it: an operator resolving something
// still running would race the runner that holds its lease.
func (q *Queue) reopen(ctx context.Context, t tenant.ID, id, actor string, to State, kind, note string) (WorkItem, error) {
	w, err := q.Load(ctx, t, id)
	if err != nil {
		return WorkItem{}, err
	}
	if w.State != StateParked {
		return w, fmt.Errorf("queue: item %s is %s, not parked — only an exception can be %s", id, w.State, kind)
	}
	w.State, w.Note = to, note
	w.dropResume()
	// Off the exception queue is off it. Leaving the severity behind would
	// have a retried item come back already shouting, and would restart a
	// later parking's clock with the previous parking's volume.
	w.Severity = ""
	// The actor is the operator, not the runner: this is the one transition a
	// person makes by hand, and an audit that could not tell it from the
	// daemon's own work would be missing the only human act in the loop.
	if _, err := q.Store.Log().Append(ctx, evidence.Record{
		Tenant: w.Tenant, Scope: scope(w.ID), Kind: kind, Subject: w.ID,
		Actor: actor, OnBehalfOf: w.Requester, Body: mustMarshal(w),
	}); err != nil {
		return w, err
	}
	if to == StatePending {
		// Back into the drain's index. The drain folds it by subject and
		// would find the original queued entry anyway, but the live event
		// stream grows its follow set from new index/pending records alone —
		// so without this a retried item runs unwatched until someone
		// reconnects.
		return w, q.index(ctx, w.Tenant, "pending", w.ID, note)
	}
	return w, nil
}

func mustMarshal(w WorkItem) []byte {
	b, err := json.Marshal(w)
	if err != nil {
		// WorkItem is plain data; a marshal failure here is a programming
		// error, not a runtime condition.
		panic("queue: marshalling a work item: " + err.Error())
	}
	return b
}

// Load folds an item's chain back into its current state.
func (q *Queue) Load(ctx context.Context, t tenant.ID, id string) (WorkItem, error) {
	var w WorkItem
	found := false
	for r, err := range q.Store.Log().Since(ctx, t, scope(id), 1) {
		if err != nil {
			return WorkItem{}, err
		}
		if !StateRecord(r.Kind) {
			continue // narrative, not state — see stateKinds
		}
		// Start from zero for every state record. Each body is the WHOLE
		// item, and omitempty leaves a cleared field out of the JSON entirely
		// — so unmarshalling over the previous record would carry forward
		// exactly the values a transition meant to drop. A settled item kept
		// the note it parked with, and would have kept its resume point too.
		w = WorkItem{}
		if err := json.Unmarshal(r.Body, &w); err != nil {
			// A state record that will not parse is real corruption, not a
			// record of another shape: say which one, because the reader that
			// hits it next has no other way to find it.
			return WorkItem{}, fmt.Errorf("queue: item %s record %d (%s): %w", id, r.Seq, r.Kind, err)
		}
		found = true
	}
	if !found {
		return WorkItem{}, fmt.Errorf("%w: %s", ErrNoItem, id)
	}
	return w, nil
}

// Stop halts new claims for the tenant. The stop is a record, so who pressed
// it, when, and why survives — and Resume is a second record, never an erasure.
func (q *Queue) Stop(ctx context.Context, t tenant.ID, actor, why string) error {
	return q.controlRecord(ctx, t, "stop", actor, why)
}

// Resume lifts a stop.
func (q *Queue) Resume(ctx context.Context, t tenant.ID, actor, why string) error {
	return q.controlRecord(ctx, t, "resume", actor, why)
}

// Stopped reports the current stop state: the head of the control chain.
func (q *Queue) Stopped(ctx context.Context, t tenant.ID) (bool, error) {
	head, err := q.Store.Log().Head(ctx, t, "control/claims")
	if err != nil {
		return false, err
	}
	return head.Seq > 0 && head.Kind == "stop", nil
}

func (q *Queue) controlRecord(ctx context.Context, t tenant.ID, kind, actor, why string) error {
	_, err := q.Store.Log().Append(ctx, evidence.Record{
		Tenant: t, Scope: "control/claims", Kind: kind, Subject: "claims",
		Actor: actor, Body: []byte(why),
	})
	return err
}

// index appends to a tenant-wide listing chain; the fold of an index plus
// Load per item is how the control room enumerates without the store growing
// a listing primitive.
func (q *Queue) index(ctx context.Context, t tenant.ID, name, subject, note string) error {
	_, err := q.Store.Log().Append(ctx, evidence.Record{
		Tenant: t, Scope: "index/" + name, Kind: "indexed", Subject: subject,
		Actor: q.Runner, Body: []byte(note),
	})
	return err
}

func (q *Queue) record(ctx context.Context, w WorkItem, kind, subject, note string) error {
	body, err := json.Marshal(w)
	if err != nil {
		return err
	}
	_, err = q.Store.Log().Append(ctx, evidence.Record{
		Tenant: w.Tenant, Scope: scope(w.ID),
		Kind: kind, Subject: subject, Actor: q.Runner, OnBehalfOf: w.Requester,
		Body: body,
	})
	return err
}
