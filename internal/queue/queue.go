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
	"time"

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
)

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
	// DependsOn names items that must settle DONE before this one is
	// claimable — the work graph. A parked or failed dependency parks its
	// dependents with the cause named; execution order is nobody's plan,
	// it emerges from claims against the graph.
	DependsOn []string `json:"depends_on,omitempty"`
	Task      string   `json:"task"`
	State     State    `json:"state"`
	// Head is the commit the item produced, once it has.
	Head string `json:"head,omitempty"`
	// Note carries the parked/failed reason.
	Note string `json:"note,omitempty"`
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
func (q *Queue) Park(ctx context.Context, w WorkItem, lease forge.Lease, reason string) (WorkItem, error) {
	w.State, w.Note = StateParked, reason
	err := q.record(ctx, w, "parked", w.ID, reason)
	if err == nil {
		err = q.index(ctx, w.Tenant, "exceptions", w.ID, reason)
	}
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// Wait records a pending gate and frees the claim; the item resumes when the
// gate answers.
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
	err := q.record(ctx, w, "failed", w.ID, reason)
	q.Store.Leaser(q.Runner).Release(ctx, lease)
	return w, err
}

// Load folds an item's chain back into its current state.
func (q *Queue) Load(ctx context.Context, t tenant.ID, id string) (WorkItem, error) {
	var w WorkItem
	found := false
	for r, err := range q.Store.Log().Since(ctx, t, scope(id), 1) {
		if err != nil {
			return WorkItem{}, err
		}
		if err := json.Unmarshal(r.Body, &w); err != nil {
			return WorkItem{}, fmt.Errorf("queue: item %s record %d: %w", id, r.Seq, err)
		}
		found = true
	}
	if !found {
		return WorkItem{}, fmt.Errorf("queue: no such item %s", id)
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
