package authz

import (
	"context"
	"fmt"
)

// Grant is one unit of granted authority: a set of actions on a resource
// pattern, held by a subject within a tenant.
type Grant struct {
	// ID names the grant in a [Decision] and in evidence, so a record shows
	// which authority permitted an action, not merely that one did.
	ID      string
	Subject string
	// Tenant is required. There is no cross-tenant grant; even the operator
	// acts within a tenant.
	Tenant  string
	Actions []Action
	// Box, Repo, RepoRole, and Env narrow the grant. Empty means any — on the
	// GRANT side only. A grant naming just a tenant covers every Box in it;
	// the resource being checked is always concrete.
	Box      string
	Repo     string
	RepoRole string
	Env      string
}

// covers reports whether the grant permits action on r.
func (g Grant) covers(action Action, r Resource) bool {
	if g.Tenant == "" || g.Tenant != string(r.Tenant) {
		return false // an unscoped grant is a bug, not a wildcard
	}
	ok := false
	for _, a := range g.Actions {
		if a == action {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	match := func(pat, val string) bool { return pat == "" || pat == val }
	return match(g.Box, r.Box) && match(g.Repo, r.Repo) &&
		match(g.RepoRole, r.RepoRole) && match(g.Env, r.Env)
}

// Source answers which grants a subject holds. Implementations are read-only;
// granting and revoking are changes with their own evidence, not methods here.
type Source interface {
	GrantsFor(ctx context.Context, subject string) ([]Grant, error)
}

// Static is an in-memory Source, for tests and for wiring before grants live
// in the store.
type Static map[string][]Grant

// GrantsFor returns the subject's grants. A subject with no entry has no
// grants — inert, not unconstrained.
func (s Static) GrantsFor(_ context.Context, subject string) ([]Grant, error) {
	return s[subject], nil
}

// Evaluator is the reference [Authorizer]: deny by default, delegation as an
// intersection.
type Evaluator struct {
	Grants Source
}

// Can applies the two rules the architecture states.
//
// A missing grant denies: there is no implicit allow anywhere, so an empty
// grant set is an inert principal rather than an unconstrained one.
//
// Delegation intersects, never unions: when an actor works on behalf of a
// principal, BOTH must hold a covering grant. An agent's effective authority
// is the least of its own and its delegator's, so routing a change through an
// automation can never widen what the person behind it could do — and a
// powerful agent lends none of its power to a weak requester.
func (e *Evaluator) Can(ctx context.Context, a Actor, action Action, r Resource) (Decision, error) {
	own, err := e.match(ctx, a.Principal.Subject, action, r)
	if err != nil {
		return Decision{}, err
	}
	if own == nil {
		return Decision{Reason: fmt.Sprintf("no grant held by %q covers %s", a.Principal.Subject, action)}, nil
	}
	if a.OnBehalfOf == nil {
		return Decision{Allow: true, Grant: own.ID,
			Reason: fmt.Sprintf("grant %s held by %q covers %s", own.ID, a.Principal.Subject, action)}, nil
	}
	behalf, err := e.match(ctx, a.OnBehalfOf.Subject, action, r)
	if err != nil {
		return Decision{}, err
	}
	if behalf == nil {
		return Decision{Reason: fmt.Sprintf("delegator %q holds no grant covering %s — delegation intersects, never unions", a.OnBehalfOf.Subject, action)}, nil
	}
	return Decision{Allow: true, Grant: own.ID,
		Reason: fmt.Sprintf("grant %s held by %q and grant %s held by delegator %q both cover %s",
			own.ID, a.Principal.Subject, behalf.ID, a.OnBehalfOf.Subject, action)}, nil
}

func (e *Evaluator) match(ctx context.Context, subject string, action Action, r Resource) (*Grant, error) {
	grants, err := e.Grants.GrantsFor(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("authz: loading grants for %q: %w", subject, err)
	}
	for i := range grants {
		if grants[i].covers(action, r) {
			return &grants[i], nil
		}
	}
	return nil, nil
}
