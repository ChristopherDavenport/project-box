package estate

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// StoreRegistry records the estate on the evidence log. Event-sourced on
// purpose: what was believed about a repository, and when, is itself audit
// material — "the census said the checks were on" is a claim about a moment,
// and a mutable row cannot testify about moments.
//
// Chains: estate/index (one record per repo, first observation only, so the
// estate is enumerable), estate/repo/<key> (observations and facts,
// latest-wins on read), estate/enroll/<key> (enrollment history).
type StoreRegistry struct {
	Store  store.Store
	Tenant tenant.ID
	Actor  string
}

const (
	indexScope  = "estate/index"
	repoScope   = "estate/repo/"
	enrollScope = "estate/enroll/"
)

// Key is the canonical repository key: host/owner/name.
func Key(r Repo) string { return r.Host + "/" + r.Owner + "/" + r.Name }

// Observe records what the census saw. First sight of a key also lands it in
// the index chain, which is what makes the estate enumerable at all.
func (g *StoreRegistry) Observe(ctx context.Context, r Repo) error {
	r.Tenant = g.Tenant
	if r.Observed.IsZero() {
		r.Observed = time.Now().UTC()
	}
	key := Key(r)
	head, err := g.Store.Log().Head(ctx, g.Tenant, repoScope+key)
	if err != nil {
		return err
	}
	if head.Seq == 0 {
		if err := g.append(ctx, indexScope, "estate-indexed", key, r); err != nil {
			return err
		}
	}
	return g.append(ctx, repoScope+key, "estate-observed", key, r)
}

// Record stores facts the census collected about one repository.
func (g *StoreRegistry) Record(ctx context.Context, repoKey string, facts []Fact) error {
	for _, f := range facts {
		f.Repo = repoKey
		if f.Observed.IsZero() {
			f.Observed = time.Now().UTC()
		}
		if err := g.append(ctx, repoScope+repoKey, "estate-fact", repoKey, f); err != nil {
			return err
		}
	}
	return nil
}

// Enroll binds a repository to a Box and role.
func (g *StoreRegistry) Enroll(ctx context.Context, e Enrollment) error {
	return g.append(ctx, enrollScope+e.Repo, "estate-enrolled", e.Repo, e)
}

// Repos folds the estate: every indexed repository at its latest observation.
func (g *StoreRegistry) Repos(ctx context.Context, t tenant.ID) iter.Seq2[Repo, error] {
	return func(yield func(Repo, error) bool) {
		for rec, err := range g.Store.Log().Since(ctx, t, indexScope, 1) {
			if err != nil {
				yield(Repo{}, err)
				return
			}
			r, _, err := g.latest(ctx, t, rec.Subject)
			if err != nil {
				yield(Repo{}, err)
				return
			}
			if !yield(r, nil) {
				return
			}
		}
	}
}

// Facts folds one repository's facts, latest per key.
func (g *StoreRegistry) Facts(ctx context.Context, repoKey string) ([]Fact, error) {
	latest := map[string]Fact{}
	var order []string
	for rec, err := range g.Store.Log().Since(ctx, g.Tenant, repoScope+repoKey, 1) {
		if err != nil {
			return nil, err
		}
		if rec.Kind != "estate-fact" {
			continue
		}
		var f Fact
		if err := json.Unmarshal(rec.Body, &f); err != nil {
			return nil, err
		}
		if _, seen := latest[f.Key]; !seen {
			order = append(order, f.Key)
		}
		latest[f.Key] = f
	}
	out := make([]Fact, 0, len(order))
	for _, k := range order {
		out = append(out, latest[k])
	}
	return out, nil
}

// Enrollment returns a repository's current enrollment, if any.
func (g *StoreRegistry) Enrollment(ctx context.Context, repoKey string) (Enrollment, bool, error) {
	head, err := g.Store.Log().Head(ctx, g.Tenant, enrollScope+repoKey)
	if err != nil || head.Seq == 0 {
		return Enrollment{}, false, err
	}
	var e Enrollment
	if err := json.Unmarshal(head.Body, &e); err != nil {
		return Enrollment{}, false, err
	}
	return e, true, nil
}

// Unenrolled returns observed repositories belonging to no Box — outside
// every campaign, gate, and policy, which is why an empty answer is reported
// rather than assumed.
func (g *StoreRegistry) Unenrolled(ctx context.Context, t tenant.ID) ([]Repo, error) {
	var out []Repo
	for r, err := range g.Repos(ctx, t) {
		if err != nil {
			return nil, err
		}
		if _, ok, err := g.Enrollment(ctx, Key(r)); err != nil {
			return nil, err
		} else if !ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// Missing returns enrollments whose repository the census no longer sees:
// policy that believes it covers something already gone.
func (g *StoreRegistry) Missing(ctx context.Context, t tenant.ID, notSeenFor time.Duration) ([]Enrollment, error) {
	var out []Enrollment
	cutoff := time.Now().Add(-notSeenFor)
	for r, err := range g.Repos(ctx, t) {
		if err != nil {
			return nil, err
		}
		key := Key(r)
		e, ok, err := g.Enrollment(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok && (r.Archived || r.Observed.Before(cutoff)) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (g *StoreRegistry) latest(ctx context.Context, t tenant.ID, key string) (Repo, bool, error) {
	var r Repo
	found := false
	for rec, err := range g.Store.Log().Since(ctx, t, repoScope+key, 1) {
		if err != nil {
			return Repo{}, false, err
		}
		if rec.Kind != "estate-observed" {
			continue
		}
		if err := json.Unmarshal(rec.Body, &r); err != nil {
			return Repo{}, false, err
		}
		found = true
	}
	if !found {
		return Repo{}, false, fmt.Errorf("estate: indexed repo %s has no observation", key)
	}
	return r, true, nil
}

func (g *StoreRegistry) append(ctx context.Context, scope, kind, subject string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = g.Store.Log().Append(ctx, evidence.Record{
		Tenant: g.Tenant, Scope: scope, Kind: kind, Subject: subject,
		Actor: g.Actor, Body: body,
	})
	return err
}
