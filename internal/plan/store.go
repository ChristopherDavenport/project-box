package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// RecordKind is the evidence kind a plan is written under.
const RecordKind = "planned"

// Store reads and writes plans on the origination's own evidence chain.
//
// The durability class is decided here and it is deliberately not a new tier.
// A plan lives in `ingress/<id>`, which makes it a BOUNDED per-entity chain —
// the same class as `workitem/<id>` and `changeset/<id>`, and therefore
// already excluded from the compaction sweep by name. One plan per intake,
// bounded by originations rather than by ticks, riding a chain an auditor
// sampling that change already asks for by subject.
//
// The alternative was an ephemeral tier for identity plans, on the argument
// that a one-node plan over one issue is scratch. That is the failure to
// avoid, and it has a worked example: in Gas Town mail, agent state and
// workflow steps all became durable records until a compactor, a reaper and an
// ephemeral tier had to be built to rescue the ledger. A second durability
// class needs its own promotion rule, its own reaper and its own answer to
// "was this plan the one that produced the landed work" — asked, unavoidably,
// after the plan would have been reaped. One record per origination is cheaper
// than the machinery that would decide which ones to keep.
type Store struct {
	Log evidence.Log
	// Actor is who records the plan — the daemon, since decomposition is
	// imperative Go on the intake path.
	Actor string
}

// Record writes a sealed plan to the origination's chain and returns it with
// its recorded time.
//
// The plan is recorded BEFORE any of its work is submitted. An operator asking
// "what is this origination about to do" must be able to be answered from the
// chain rather than from the queue, and a graph whose items exist before the
// plan that called for them is a plan reconstructed from its consequences.
func (s *Store) Record(ctx context.Context, ing ingress.Ingress, p Plan) (Plan, error) {
	if p.Ref == "" {
		return Plan{}, fmt.Errorf("%w: %s is unsealed — Seal computes the content address the items will carry", ErrPlan, ing.ID)
	}
	if p.Ingress != ing.ID {
		return Plan{}, fmt.Errorf("%w: plan names ingress %s, recorded against %s", ErrPlan, p.Ingress, ing.ID)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	rec, err := s.Log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: ingress.Scope(ing.ID),
		Kind: RecordKind, Subject: p.Ref,
		Actor: s.Actor, OnBehalfOf: ing.Requester,
		Body: body,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("%w: recording %s for %s: %w", ErrPlan, p.Ref, ing.ID, err)
	}
	p.At = rec.At
	return p, nil
}

// Latest returns the newest plan for an origination, if it has one.
func (s *Store) Latest(ctx context.Context, t tenant.ID, ingressID string) (Plan, bool, error) {
	all, err := s.All(ctx, t, ingressID)
	if err != nil || len(all) == 0 {
		return Plan{}, false, err
	}
	return all[len(all)-1], true, nil
}

// All returns every plan recorded for an origination, oldest first.
//
// More than one is not an error and is the honest shape for a standing
// reconciler: a campaign decomposes a wave at a time, so its chain carries one
// plan per wave and each item names the wave that called for it.
func (s *Store) All(ctx context.Context, t tenant.ID, ingressID string) ([]Plan, error) {
	var out []Plan
	for rec, err := range s.Log.Since(ctx, t, ingress.Scope(ingressID), 1) {
		if err != nil {
			return nil, err
		}
		if rec.Kind != RecordKind {
			continue // the chain carries the origination and its acts too
		}
		var p Plan
		if err := json.Unmarshal(rec.Body, &p); err != nil {
			return nil, fmt.Errorf("%w: reading %s of %s: %w", ErrPlan, rec.Subject, ingressID, err)
		}
		if p.At.IsZero() {
			p.At = rec.At
		}
		out = append(out, p)
	}
	return out, nil
}

// ByRef reads one plan by its content address, wherever it landed. A ref is
// a sha256-derived content address, so a collision is not a concern the way
// it would be for a mutable key — the first match [evidence.Log.BySubject]
// yields is returned, false with no error when nothing names it.
//
// BySubject walks every scope naming the subject regardless of which
// ingress chain it is on — the same primitive [evidence.Provenance] uses —
// so a diff view can compare two plans without either caller having to know
// which origination produced them.
func (s *Store) ByRef(ctx context.Context, t tenant.ID, ref string) (Plan, bool, error) {
	for rec, err := range s.Log.BySubject(ctx, t, ref) {
		if err != nil {
			return Plan{}, false, err
		}
		if rec.Kind != RecordKind {
			continue
		}
		var p Plan
		if err := json.Unmarshal(rec.Body, &p); err != nil {
			return Plan{}, false, fmt.Errorf("%w: reading %s: %w", ErrPlan, ref, err)
		}
		if p.At.IsZero() {
			p.At = rec.At
		}
		return p, true, nil
	}
	return Plan{}, false, nil
}

// The kinds a planning failure and its resolution are written under, on the
// same chain as RecordKind.
const (
	FailedKind   = "plan-failed"
	RetriedKind  = "plan-retried"
	ResolvedKind = "plan-resolved"
	// DeferredKind marks a judgement run that never answered — the meter's
	// own pacing, not a judgement that the plan does not hold. It is written
	// on the same chain as the others but carries none of the exception's
	// consequences: see [Store.Defer].
	DeferredKind = "plan-deferred"
)

// Standing is where an origination stands with respect to planning: nothing
// yet, planned, failed and waiting for a person, sent back for another
// attempt, given up on, or deferred by the meter's own pacing.
type Standing struct {
	// State is "", planned, failed, retried, resolved, or deferred — the
	// newest of the planning events on the chain.
	State  string
	Reason string
	At     time.Time
}

// Blocked reports that intake must not plan this origination now: a
// judgement failed and nobody has retried it, or somebody resolved it. A
// planner out of budget re-run every tick would be a spend loop wearing an
// intake, and an origination a person gave up on is not work.
func (s Standing) Blocked() bool { return s.State == "failed" || s.State == "resolved" }

// Standing folds the origination's chain for its newest planning event.
func (s *Store) Standing(ctx context.Context, t tenant.ID, ingressID string) (Standing, error) {
	var st Standing
	for rec, err := range s.Log.Since(ctx, t, ingress.Scope(ingressID), 1) {
		if err != nil {
			return Standing{}, err
		}
		switch rec.Kind {
		case RecordKind:
			st = Standing{State: "planned", At: rec.At}
		case FailedKind:
			st = Standing{State: "failed", Reason: string(rec.Body), At: rec.At}
		case RetriedKind:
			st = Standing{State: "retried", Reason: string(rec.Body), At: rec.At}
		case ResolvedKind:
			st = Standing{State: "resolved", Reason: string(rec.Body), At: rec.At}
		case DeferredKind:
			st = Standing{State: "deferred", Reason: string(rec.Body), At: rec.At}
		}
	}
	return st, nil
}

// Fail records a judgement decomposer's failure on the origination's chain
// and puts the origination in the exception queue.
//
// This is the seat whose failure parks the one origination that asked for a
// judgement. There is no item yet to park — the plan is what would have made
// one — so the origination itself is the exception: the room lists it with
// the reason, the escalator ages it, and intake leaves it alone until a
// person retries or resolves it. Recorded rather than returned as a tick
// error, because a tick error is retried next tick, and a planner that is
// out of budget re-run every fifteen seconds is exactly the spend loop the
// meter exists to prevent.
func (s *Store) Fail(ctx context.Context, ing ingress.Ingress, reason string) error {
	if _, err := s.Log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: ingress.Scope(ing.ID), Kind: FailedKind, Subject: ing.ID,
		Actor: s.Actor, OnBehalfOf: ing.Requester, Body: []byte(reason),
	}); err != nil {
		return fmt.Errorf("%w: recording the failure of %s: %w", ErrPlan, ing.ID, err)
	}
	_, err := s.Log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: "index/exceptions", Kind: "indexed", Subject: ing.ID,
		Actor: s.Actor, Body: []byte(reason),
	})
	return err
}

// Defer records that a judgement run never answered — the meter's own
// pacing, not a judgement the plan does not hold — on the origination's
// chain.
//
// Unlike [Store.Fail], this writes NO index/exceptions record: a deferral is
// not an exception. Nobody gave up on the origination and nobody needs to
// look at it; the meter's window did the refusing and the meter's window
// will do the admitting, on its own clock. Intake keeps seeing the
// origination as pending — [Standing.Blocked] stays false for "deferred" —
// and retries it the next tick the meter allows a start.
func (s *Store) Defer(ctx context.Context, ing ingress.Ingress, reason string) error {
	if _, err := s.Log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: ingress.Scope(ing.ID), Kind: DeferredKind, Subject: ing.ID,
		Actor: s.Actor, OnBehalfOf: ing.Requester, Body: []byte(reason),
	}); err != nil {
		return fmt.Errorf("%w: recording the deferral of %s: %w", ErrPlan, ing.ID, err)
	}
	return nil
}

// Retry sends a failed planning back for another attempt; Resolve gives up on
// it. Both are an operator's act — the actor is the person, and the one
// transition off the exception queue is theirs, as it is for an item.
func (s *Store) Retry(ctx context.Context, t tenant.ID, ingressID, actor, note string) error {
	return s.reopen(ctx, t, ingressID, actor, RetriedKind, note)
}

// Resolve gives up on planning an origination. See [Store.Retry].
func (s *Store) Resolve(ctx context.Context, t tenant.ID, ingressID, actor, note string) error {
	return s.reopen(ctx, t, ingressID, actor, ResolvedKind, note)
}

func (s *Store) reopen(ctx context.Context, t tenant.ID, ingressID, actor, kind, note string) error {
	st, err := s.Standing(ctx, t, ingressID)
	if err != nil {
		return err
	}
	if st.State != "failed" {
		return fmt.Errorf("%w: planning of %s is %q, not failed — only a failed plan can be retried or resolved", ErrPlan, ingressID, st.State)
	}
	ing, ok, err := s.origination(ctx, t, ingressID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s has no origination record", ErrPlan, ingressID)
	}
	_, err = s.Log.Append(ctx, evidence.Record{
		Tenant: t, Scope: ingress.Scope(ingressID), Kind: kind, Subject: ingressID,
		Actor: actor, OnBehalfOf: ing.Requester, Body: []byte(note),
	})
	return err
}

// Origination reads the ingress an origination's chain opened with.
func (s *Store) Origination(ctx context.Context, t tenant.ID, ingressID string) (ingress.Ingress, bool, error) {
	return s.origination(ctx, t, ingressID)
}

func (s *Store) origination(ctx context.Context, t tenant.ID, ingressID string) (ingress.Ingress, bool, error) {
	for rec, err := range s.Log.Since(ctx, t, ingress.Scope(ingressID), 1) {
		if err != nil {
			return ingress.Ingress{}, false, err
		}
		if rec.Kind != ingress.RecordOpen {
			continue
		}
		var ing ingress.Ingress
		if err := json.Unmarshal(rec.Body, &ing); err != nil {
			return ingress.Ingress{}, false, fmt.Errorf("%w: reading the origination of %s: %w", ErrPlan, ingressID, err)
		}
		return ing, true, nil
	}
	return ingress.Ingress{}, false, nil
}
