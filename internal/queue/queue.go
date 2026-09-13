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
	"strings"
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
	// StateHandedOff is a session that ended mid-change and asked for a fresh
	// one: the agent is not finished and is not stuck.
	//
	// The third state on the same axis as the second, for the same reason.
	// Waiting is work held on somebody else's answer; handed off is work whose
	// session ran out of room with nobody to wait for. Neither needs a person,
	// so filing either in the exception queue would bury the parkings that do
	// under routine ones that do not.
	//
	// Not pending either, though it is claimable like one. Pending says nobody
	// has started this, and an item three sessions deep has been started three
	// times — a state that lies about that is a state an operator cannot use.
	StateHandedOff State = "handedoff"
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

// Claimable names the states a drain may take work in — equivalently, the
// states that are not settled, which is what an operator means by "pending".
//
// One list rather than several. The pump's selection, the pump's settled
// count and the room's backlog read the same vocabulary, and when it was
// spelled out separately in each of them a state added to one and not the
// others had the pump running an item and then reporting it as finished. The
// state vocabulary has already had that failure once, which is why this is a
// function and not a switch three readers each write out.
func Claimable(s State) bool {
	switch s {
	case StatePending, StateWaiting, StateHandedOff:
		return true
	}
	return false
}

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
	// Plan is the content address of the decomposition that called for this
	// item, and Node names it within that plan.
	//
	// The chain reads ingress -> plan -> work item, and the middle link is why
	// the digest rides here rather than being looked up: when one origination
	// becomes six items, the decision about WHICH six determined what got
	// built and what was silently omitted, and a reference to the decomposer
	// would not recover it — a decomposer over moving facts yields a different
	// graph on Tuesday while every reference to it looks unchanged.
	//
	// Empty on items submitted before there was a decomposition stage. Those
	// are readable and unrecoverable, which is the honest state for them: no
	// query reconstructs a plan that was never made.
	Plan string `json:"plan,omitempty"`
	Node string `json:"node,omitempty"`
	// Kind is the declaration this work was opened under, carried from the
	// origination.
	//
	// A denormalization, deliberately. The escalation policy an exception is
	// measured against belongs to its kind, and the sweep reads every parked
	// item on every pass — resolving the kind through the ingress chain would
	// put a chain read per exception on a standing chore, to recover a value
	// that cannot change for the life of the item.
	Kind string `json:"kind,omitempty"`
	// Requester is the principal whose authority this work exercises,
	// inherited from the ingress. It rides every evidence record as
	// OnBehalfOf: an audit must separate a change a person made from one an
	// automation made for them, and a log that never drew that line cannot
	// have it recovered later.
	Requester string `json:"requester,omitempty"`
	// Repo is the target repository within the Box, empty when the Box has
	// exactly one executor.
	Repo string `json:"repo,omitempty"`
	// OriginKey is the delivery key of the ingress this item traces to,
	// inherited unchanged — e.g. "issue-42" for an issue-route ingress. See
	// [ingress.Ingress.Key]. It is not a second lookup of the origination:
	// the loop reads a GitHub issue number off this exact, already-settled
	// shape rather than deriving one some other way.
	OriginKey string `json:"origin_key,omitempty"`
	// DependsOn names items that must settle DONE before this one is
	// claimable — the work graph. A parked or failed dependency parks its
	// dependents with the cause named; execution order is nobody's plan,
	// it emerges from claims against the graph.
	DependsOn []string `json:"depends_on,omitempty"`
	// Sections names the regions of Repo this unit expresses itself in: what
	// it leases before it builds, and what its builder is told to stay
	// within. Empty is the whole repository — it leases every declared
	// section, so a unit that names none and a unit that names one can
	// never run at once.
	Sections []string `json:"sections,omitempty"`
	// Model is the tier the plan chose for this unit, when it chose one;
	// empty inherits the section's, then the role's, then the box's.
	Model string `json:"model,omitempty"`
	// Attempts is the plan's per-unit override of the attempt budget: how
	// many builds this item's claim may spend on a gate or review refusal
	// before it parks — see [loop.Loop.Attempts]. Zero inherits the repo's
	// or box's default.
	Attempts int    `json:"attempts,omitempty"`
	Task     string `json:"task"`
	// Summary is the one line the room shows for this item, distinct from
	// Task: the task is the full paragraph an agent needs, and showing that
	// paragraph as the item's name was the gap this field closes. Empty on
	// items from before this field existed.
	Summary string `json:"summary,omitempty"`
	State   State  `json:"state"`
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
	// Unfinished marks Built as work in progress rather than a change offered
	// for judgement: a session ran out of room, handed off, and left what it
	// had committed in the tree.
	//
	// It is what keeps the two resumes apart. An ordinary resume SKIPS the
	// builder — the commit is already the answer and only the gate has not
	// spoken — and doing that to half a change would gate and land exactly the
	// degraded commit a mid-run handoff exists to prevent. An unfinished
	// resume adopts the same tree and runs the agent again on top of it.
	Unfinished bool `json:"unfinished,omitempty"`
	// Hops counts how many times this item has handed off to a fresh session
	// of itself.
	//
	// It bounds the spend: a session that can summon its own successor
	// indefinitely is an unbounded bill with no landing, so the limit parks
	// with the reason named. And it answers "how many sessions did this change
	// take", which nothing else carries and which is the most direct signal
	// there is about whether an item was sized for one.
	Hops int `json:"hops,omitempty"`
	// Attempt is the build this claim is currently on, or last was, within
	// its attempt budget — see [loop.Loop.Attempts]. It exists so a look at
	// the item shows "attempt 2 of 3" rather than a unit that looks like it
	// built twice for no stated reason; the budget itself is never read from
	// here. Scoped to the current claim: a fresh claim starts counting from
	// nothing, whatever this still says from the one before it.
	Attempt int `json:"attempt,omitempty"`
	// PassedGates names the gates that already returned pass on Built.
	//
	// They are carried rather than re-run while that head stands. A gate that
	// passed on this exact content passed, and asking it again every tick is
	// how an item that is only waiting on a person keeps a machine busy — the
	// cheap gate is the one whose answer can change, and the expensive ones
	// are the deterministic shells. Any change of head clears this, because a
	// different commit is a different question.
	PassedGates []string `json:"passed_gates,omitempty"`
	// RebaseCount is how many times this item's candidate has been replayed
	// onto a moved base with git.Rebase, across every claim: a rebase happens
	// at most once per claim (only in the loop's adopt branch), so a branch
	// that keeps moving would otherwise buy the loop an unbounded run of free
	// rebase-and-regate cycles chasing it, one claim after another. Bounded by
	// the attempt budget (see [loop.Loop.attemptBudget]); past it the loop
	// parks instead of rebasing again and a person decides. Reset wherever the
	// candidate resets: [WorkItem.dropResume] and a fresh (or rebase-refused)
	// build.
	RebaseCount int `json:"rebase_count,omitempty"`
	// Note carries the parked/failed reason.
	Note string `json:"note,omitempty"`
	// Severity is how loudly this parking asks for a person. Set by the stage
	// that parked it, meaningful only while the item is parked, and dropped
	// on the way back off the exception queue.
	Severity Severity `json:"severity,omitempty"`
	// ParkedBy names the dependency whose park or failure cascaded into THIS
	// item's parking. Meaningful only while parked, and cleared on the way
	// back off the exception queue — it is what lets a later release tell
	// "still parked by the thing that just landed" from "parked for its own
	// reasons" or "re-parked by a newer cause".
	ParkedBy string `json:"parked_by,omitempty"`
	// SectionWaits counts how many claims in a row went back to Wait because
	// a section this unit needs was held — the starvation signal the pump's
	// fairness rule reads to name an oldest waiter. Reset to zero the moment
	// the sections this unit needs are actually acquired.
	SectionWaits int `json:"section_waits,omitempty"`
	// YieldTo is the ID of a starved unit this claim should step aside for on
	// this tick, set by the pump on the candidate copy it hands to
	// [loop.Loop.Run]. It is a dispatch hint, not a fact about the item — the
	// json:"-" tag keeps it off every record, since a hint that survived into
	// the chain could be replayed onto a fold that never made the decision.
	YieldTo string `json:"-"`
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
	"handedoff": true,
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

// gateVerdict is a [forge.Verdict] read back off the chain. Unmarshalled by
// field name rather than by importing internal/forge: a gate record's body
// is that type's own default JSON encoding (internal/forge/gate.go), and
// this package already reads other records' bodies the same way rather than
// pull in the gate package for a shape it only needs to read.
type gateVerdict struct {
	Gate     string
	Decision string
	Reason   string
}

// priorVerdicts reads item id's gate records back off its chain and returns
// one [agent.PriorVerdict] per refused attempt, oldest first. Attempts are
// numbered by first appearance of the record's Subject — the head sha each
// verdict judged — and a sha that later carries a non-fail verdict (a free
// recheck that passed) is dropped: it stopped being a refusal. A sha that
// fails more than once keeps only its latest refusal, since a recheck on an
// unchanged commit is one attempt, not two.
func priorVerdicts(ctx context.Context, log evidence.Log, t tenant.ID, id string) ([]agent.PriorVerdict, error) {
	attemptOf := map[string]int{}
	latest := map[string]struct {
		attempt int
		body    gateVerdict
	}{}
	var order []string
	next := 0
	for r, err := range log.Since(ctx, t, scope(id), 0) {
		if err != nil {
			return nil, err
		}
		if r.Kind != "gate" {
			continue
		}
		sha := r.Subject
		n, ok := attemptOf[sha]
		if !ok {
			next++
			n = next
			attemptOf[sha] = n
			order = append(order, sha)
		}
		var body gateVerdict
		if err := json.Unmarshal(r.Body, &body); err != nil {
			return nil, fmt.Errorf("gate record %s: %w", r.ID, err)
		}
		latest[sha] = struct {
			attempt int
			body    gateVerdict
		}{attempt: n, body: body}
	}
	var out []agent.PriorVerdict
	for _, sha := range order {
		v := latest[sha]
		if v.body.Decision != "fail" {
			continue // a pass or a still-pending verdict is not a refusal
		}
		out = append(out, agent.PriorVerdict{
			Gate: v.body.Gate, Decision: v.body.Decision, Reason: v.body.Reason, Attempt: v.attempt,
		})
	}
	return out, nil
}

// ClaimRepo is the pseudo-repository a claim is leased under. A claim IS a
// lease, and a lease names a (repo, ref) pair; no git repository is involved.
const ClaimRepo = "_queue"

// ClaimRef names the ref one item's claim is held on.
//
// Exported because the claim is not only the queue's business: the control
// room reads the same key to say which runner holds an item and when the hold
// lapses. A second spelling of it would have the room report "unclaimed" for
// work a runner is holding, which is the reading an operator must never be
// given.
func ClaimRef(id string) string { return scope(id) }

// Submit records a new pending item. An item with no ingress is refused.
func (q *Queue) Submit(ctx context.Context, w WorkItem) (WorkItem, error) {
	if w.Ingress == "" {
		return WorkItem{}, errors.New("queue: work item traces to no ingress — orphan work is refused")
	}
	w.State = StatePending
	if err := q.record(ctx, w, "queued", w.ID, ""); err != nil {
		return WorkItem{}, err
	}
	// A reverse-dependency index entry per edge, written once here regardless
	// of whether the dependency ever parks: release-time lookup then does not
	// depend on exactly when a cascade park happened.
	for _, dep := range w.DependsOn {
		if err := q.index(ctx, w.Tenant, "dependents/"+dep, w.ID, ""); err != nil {
			return w, err
		}
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
			w.ParkedBy = dep
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
		Tenant: w.Tenant, Repo: ClaimRepo, Ref: ClaimRef(w.ID), Run: w.ID, TTL: q.TTL,
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
	return q.park(ctx, w, lease, sev, reason, true)
}

// ParkKeeping parks like [Queue.Park] but leaves whatever resume point the
// caller already set on w alone, instead of dropping it.
//
// A gate that refused a commit is not the same finality as a build failure or
// a denied authorization: the commit itself is not the thing that was judged
// wrong until a second attempt says so too, and re-asking the gate on retry
// is cheaper than buying the commit again from an agent. Every other park
// still goes through Park and still drops the resume point exactly as
// before — this is the one exception, used only where the caller has just
// set the resume point itself (see [WorkItem.KeepCandidate]).
func (q *Queue) ParkKeeping(ctx context.Context, w WorkItem, lease forge.Lease, sev Severity, reason string) (WorkItem, error) {
	return q.park(ctx, w, lease, sev, reason, false)
}

func (q *Queue) park(ctx context.Context, w WorkItem, lease forge.Lease, sev Severity, reason string, dropResume bool) (WorkItem, error) {
	w.State, w.Note, w.Severity = StateParked, reason, sev.OrDefault()
	if dropResume {
		w.dropResume()
	}
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
// Wait and HandOff are the transitions that keep it, because they are the
// states the same tree is still worth coming back to.
func (w *WorkItem) dropResume() {
	w.Built, w.BuiltBase, w.PassedGates, w.Unfinished = "", "", nil, false
	w.RebaseCount = 0
}

// KeepCandidate records a finished change to come back to instead of buying
// it from an agent twice: the commit, the base it stands on, and the gates
// that have already passed on it.
func (w *WorkItem) KeepCandidate(head, base forge.Commit, passed []string) {
	w.Built, w.BuiltBase, w.PassedGates, w.Unfinished = string(head), string(base), passed, false
}

// KeepUnfinished records work in progress a handed-off session left behind.
//
// A method rather than three assignments at each call site, because the
// difference between the two resume points is one boolean and the cost of
// getting it wrong is asymmetric: a candidate mistaken for unfinished buys a
// second agent run, and unfinished mistaken for a candidate lands half a
// change. No gate has judged this content, so any carried verdict goes.
func (w *WorkItem) KeepUnfinished(head, base forge.Commit) {
	w.Built, w.BuiltBase, w.PassedGates, w.Unfinished = string(head), string(base), nil, true
}

// Wait records a pending gate and frees the claim; the item resumes when the
// gate answers. It keeps any resume point the caller set on the item — see
// [WorkItem.Built].
func (q *Queue) Wait(ctx context.Context, w WorkItem, lease forge.Lease, reason string) (WorkItem, error) {
	w.State, w.Note = StateWaiting, reason
	err := q.record(ctx, w, "waiting", w.ID, reason)
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// HandOff ends a session without settling the item: the agent asked for a
// fresh one, and the same work goes back to the queue for the same seat. See
// [StateHandedOff] for why this is neither waiting nor parked, and
// [WorkItem.Hops] for what bounds it.
//
// The resume point survives, as it does through Wait and for a nearer reason:
// what the session committed is still in the tree, and the successor
// continuing it is the whole difference between a handoff and a park.
//
// The hop is counted here rather than by the caller. The count is the bound,
// and a bound a stage has to remember to increment is a bound that stops
// binding the first time somebody adds a second call site.
func (q *Queue) HandOff(ctx context.Context, w WorkItem, lease forge.Lease, reason string) (WorkItem, error) {
	w.State, w.Note = StateHandedOff, reason
	w.Hops++
	err := q.record(ctx, w, "handedoff", w.ID, reason)
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// Retain persists an in-claim retry's state — the carried [agent.Prior] and
// the attempt number — WITHOUT settling the item, and without dropping the
// claim: every other transition above frees the lease it was handed because
// it moves the item out of running; this one keeps running, so it renews
// instead.
//
// acp.Builder loads its item fresh from the queue on every call rather than
// from what [Loop.Run] holds in memory, so a second build in the same claim
// finds what the first one decided only if this writes it first — the
// caller's contract is to call Retain BEFORE re-entering the build stage.
//
// Lease-checked, not a general setter: prior and attempt are the only fields
// it changes, named as arguments rather than expected on w beforehand, and it
// renews the lease before writing. A runner that has already lost this claim
// — TTL lapsed, another runner recovered it — cannot leave its own record on
// an item it no longer holds.
func (q *Queue) Retain(ctx context.Context, w WorkItem, lease forge.Lease, prior *agent.Prior, attempt int) (WorkItem, forge.Lease, error) {
	renewed, err := q.Store.Leaser(q.Runner).Renew(ctx, lease, q.TTL)
	if err != nil {
		return w, lease, err
	}
	w.Prior, w.Attempt = prior, attempt
	if err := q.record(ctx, w, "claimed", w.ID, fmt.Sprintf("attempt %d", attempt)); err != nil {
		return w, renewed, err
	}
	return w, renewed, nil
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
	if err == nil {
		err = q.releaseDependents(ctx, w.Tenant, w.ID)
	}
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// releaseDependents reopens whatever this item's landing left parked on it.
//
// A dependent parked by cascade stays parked forever unless something
// revisits it: nothing about the dependency landing later ever touched the
// leaf, so an operator had to notice the stale note and retry it by hand.
// This is that revisit, scoped tightly by ParkedBy so it never disturbs a
// leaf parked for its own reasons, or one re-parked by a newer cause since.
func (q *Queue) releaseDependents(ctx context.Context, t tenant.ID, id string) error {
	for rec, err := range q.Store.Log().Since(ctx, t, "index/dependents/"+id, 1) {
		if err != nil {
			return err
		}
		dep, err := q.Load(ctx, t, rec.Subject)
		if err != nil {
			return err
		}
		if dep.State != StateParked || dep.ParkedBy != id {
			continue
		}
		if _, err := q.reopen(ctx, t, dep.ID, q.Runner, StatePending, "retried", fmt.Sprintf("dependency %s landed", id)); err != nil {
			return err
		}
	}
	return nil
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
		Tenant: w.Tenant, Repo: ClaimRepo, Ref: ClaimRef(w.ID), Run: w.ID, TTL: q.TTL,
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
// never recover work it had already been asked to do. A gate-refusal park
// keeps its resume point (see [Queue.ParkKeeping]), and a plain retry keeps
// it too: re-asking the gate on the same commit is the one free recheck a
// refused gate bought. A note that says to rebuild — matched case-
// insensitively for "rebuild" — drops it instead, exactly as a retry always
// used to, for the operator who has already judged the commit itself to be
// the problem.
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
	rebuild := kind == "retried" && strings.Contains(strings.ToLower(note), "rebuild")
	if rebuild {
		// The attempt being abandoned is what the next one is woken
		// knowing: the commit it produced and what refused it, with the
		// operator's words beside the gate's. The reviewer's first refusal
		// on the dogfood named the line and the fix, and the rebuild that
		// followed was made blind on the same task and made the same
		// mistake — a refusal that never reaches the hands is a judgement
		// nobody acts on. It resolves into a layer at wake time, so the
		// rebuilt run's agentref records what it was told.
		//
		// Read from the chain rather than from the park note: the note kept
		// only the first line of the latest refusal, and this is exactly
		// the account a rebuild must not be handed. Unknown never reads as
		// zero, so a chain that cannot be read fails the retry rather than
		// persisting a verdict-less Prior.
		verdicts, verr := priorVerdicts(ctx, q.Store.Log(), w.Tenant, w.ID)
		if verr != nil {
			return WorkItem{}, fmt.Errorf("queue: reopen: reading %s's prior verdicts: %w", id, verr)
		}
		w.Prior = &agent.Prior{Run: w.ID, Repo: w.Repo, Sha: w.Built,
			Verdicts: verdicts, RetriedBy: actor, RetryNote: note}
	}
	w.State, w.Note = to, note
	// Resolve is final and always drops it — closing an exception says there
	// is nothing left to build. Retry keeps it unless the note itself says
	// the commit is the problem, not the gate.
	if kind != "retried" || rebuild {
		w.dropResume()
	}
	// Off the exception queue is off it. Leaving the severity behind would
	// have a retried item come back already shouting, and would restart a
	// later parking's clock with the previous parking's volume.
	w.Severity = ""
	w.ParkedBy = ""
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
	c, err := q.Control(ctx, t)
	return c.Stopped, err
}

// Control is the claims switch as a fact rather than a bit: whether claims
// are stopped, and the record that made it so — who, when, and the reason
// they gave. A room that could only say "stopped" left the operator reading
// the activity feed to learn why, and an andon cord whose pull has no name on
// it is one nobody dares release.
type Control struct {
	Stopped bool `json:"stopped"`
	// Actor pressed it; Reason is what they wrote; At is when. All three are
	// empty on a tenant nobody has ever stopped or resumed.
	Actor  string    `json:"actor,omitempty"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitzero"`
}

// Control reads the head of the control chain.
func (q *Queue) Control(ctx context.Context, t tenant.ID) (Control, error) {
	head, err := q.Store.Log().Head(ctx, t, "control/claims")
	if err != nil {
		return Control{}, err
	}
	if head.Seq == 0 {
		return Control{}, nil
	}
	return Control{
		Stopped: head.Kind == "stop",
		Actor:   head.Actor, Reason: string(head.Body), At: head.At,
	}, nil
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
