package plan

import (
	"context"
	"encoding/json"
	"fmt"

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
