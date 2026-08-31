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
	Task    string `json:"task"`
	State   State  `json:"state"`
	// Head is the commit the item produced, once it has.
	Head string `json:"head,omitempty"`
	// Note carries the parked/failed reason.
	Note string `json:"note,omitempty"`
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
	return w, q.record(ctx, w, "queued", w.ID, "")
}

// Claim takes the item's lease. The lease, not the state fold, is the
// exclusivity: two runners folding the same chain still cannot both claim.
func (q *Queue) Claim(ctx context.Context, w WorkItem) (WorkItem, forge.Lease, error) {
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

// Park moves the item to the exception queue and frees its claim.
func (q *Queue) Park(ctx context.Context, w WorkItem, lease forge.Lease, reason string) (WorkItem, error) {
	w.State, w.Note = StateParked, reason
	err := q.record(ctx, w, "parked", w.ID, reason)
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

func (q *Queue) record(ctx context.Context, w WorkItem, kind, subject, note string) error {
	body, err := json.Marshal(w)
	if err != nil {
		return err
	}
	_, err = q.Store.Log().Append(ctx, evidence.Record{
		Tenant: w.Tenant, Scope: scope(w.ID),
		Kind: kind, Subject: subject, Actor: q.Runner, Body: body,
	})
	return err
}
