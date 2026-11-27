// Package ladder is the environment ladder: main promotes to staging, and
// only soaked, green commits promote to production.
//
// The ladder is what makes the optimistic lane safe — and it is also the
// compliance boundary. Everything below the production promotion is
// autonomous end to end; the human-facing controls (the approval gate,
// review sampling) bind at the env-prod promotion and only there. A commit
// that never goes green never promotes; nothing rolls back, it just never
// advances.
//
// Promotion is not new machinery: it is a gated deploy ChangeSet into the
// next environment's repo, produced by the same loop that lands everything
// else, from a standing predicate — green, plus soak in the environment
// below.
package ladder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/checks"
	"github.com/ChristopherDavenport/project-box/internal/deploy"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	git "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/render"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Sentinel outcomes. Only real failures are bare errors; these are the
// ladder saying "not yet", which callers poll rather than fear.
var (
	ErrNoCandidate      = errors.New("ladder: no green candidate")
	ErrUpToDate         = errors.New("ladder: environment already carries the candidate")
	ErrNotSoaked        = errors.New("ladder: candidate has not soaked in the environment below")
	ErrAwaitingApproval = errors.New("ladder: promotion is waiting on human approval — the production boundary")
)

// Env is one rung: an environment repository and the gates its landings run.
type Env struct {
	Name      string
	Namespace string
	Repo      forge.Repo // RoleEnv
	// Gates for the promotion landing. Staging carries machine gates only;
	// prod adds the approval gate — the one place a human appears.
	Gates loop.Gates
}

// Ladder promotes one service through its environments.
type Ladder struct {
	Store   store.Store
	Tenant  tenant.ID
	Runner  string
	Router  *ingress.StoreRouter
	Mirror  *git.Mirror
	Source  forge.Repo // the service repo whose check chain feeds candidates
	Service render.Service
	Staging Env
	Prod    Env
	// Soak is how long a candidate must hold in staging before prod may take
	// it.
	Soak time.Duration
	// Now is the clock soak is judged by; nil means time.Now. Tests inject
	// the store's clock so soak and evidence agree on what time it is.
	Now func() time.Time
	TTL time.Duration
}

func (l *Ladder) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func promoScope(source, env string) string { return "ladder/" + source + "/" + env }

// Promoted returns the sha an environment currently carries, and when it
// arrived.
func (l *Ladder) Promoted(ctx context.Context, env string) (string, time.Time, bool, error) {
	head, err := l.Store.Log().Head(ctx, l.Tenant, promoScope(l.Source.Name, env))
	if err != nil || head.Seq == 0 {
		return "", time.Time{}, false, err
	}
	return head.Subject, head.At, true, nil
}

// PromoteToStaging takes the latest green candidate from the source's check
// chain. A red landing is simply never the candidate — refusal is the
// absence of advancement, not a rollback.
func (l *Ladder) PromoteToStaging(ctx context.Context) (string, error) {
	cand, ok, err := checks.LatestGreen(ctx, l.Store, l.Tenant, l.Source.Name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoCandidate
	}
	cur, _, has, err := l.Promoted(ctx, l.Staging.Name)
	if err != nil {
		return "", err
	}
	if has && cur == cand {
		return cand, ErrUpToDate
	}
	return cand, l.promote(ctx, l.Staging, cand)
}

// PromoteToProd takes what staging carries — never the source directly, so
// production provably cannot receive a commit that did not pass through the
// rung below — once it has soaked green for the configured time and the
// prod gates (including the human one) pass.
func (l *Ladder) PromoteToProd(ctx context.Context) (string, error) {
	cand, at, has, err := l.Promoted(ctx, l.Staging.Name)
	if err != nil {
		return "", err
	}
	if !has {
		return "", fmt.Errorf("%w: staging carries nothing", ErrNoCandidate)
	}
	if soaked := l.now().Sub(at); soaked < l.Soak {
		return cand, fmt.Errorf("%w: %s of %s", ErrNotSoaked, soaked.Round(time.Second), l.Soak)
	}
	if green, err := checks.IsGreen(ctx, l.Store, l.Tenant, l.Source.Name, cand); err != nil {
		return "", err
	} else if !green {
		return cand, fmt.Errorf("%w: %s went red after staging took it", ErrNoCandidate, short(cand))
	}
	cur, _, has, err := l.Promoted(ctx, l.Prod.Name)
	if err != nil {
		return "", err
	}
	if has && cur == cand {
		return cand, ErrUpToDate
	}
	return cand, l.promote(ctx, l.Prod, cand)
}

// promote lands the candidate into an environment repo through the ordinary
// loop, gated lane — a promotion IS a deploy ChangeSet, nothing more.
func (l *Ladder) promote(ctx context.Context, env Env, cand string) error {
	ing, _, err := l.Router.Open(ctx, ingress.Route{
		Name: "promote-" + env.Name, Kind: ingress.RouteSchedule,
		Opens: kind.SeedIssue, Box: l.Service.Box, Requester: l.Runner,
	}, ingress.Delivery{Key: env.Name + "-" + cand})
	if err != nil {
		return err
	}
	svc := l.Service
	svc.Tag = short(cand) // the env repo names exactly what it runs
	run := &loop.Loop{
		Store: l.Store, Runner: l.Runner, Mirror: l.Mirror,
		Repo: env.Repo, Branch: env.Repo.DefaultBranch,
		Builder: deploy.Builder{Service: svc, Target: render.Target{Env: env.Name, Namespace: env.Namespace}, Identity: l.Mirror.Identity},
		Gates:   env.Gates, Lane: forge.LaneGated, TTL: l.TTL,
	}
	q := &queue.Queue{Store: l.Store, Runner: l.Runner, TTL: l.TTL}
	item, err := q.Submit(ctx, loop.ItemFor(ing, "promote-"+env.Name+"-"+short(cand),
		fmt.Sprintf("promote %s %s to %s", l.Service.Name, short(cand), env.Name)))
	if err != nil {
		return err
	}
	item, err = run.Run(ctx, item)
	if err != nil {
		return err
	}
	switch item.State {
	case queue.StateDone:
		_, err := l.Store.Log().Append(ctx, evidence.Record{
			Tenant: l.Tenant, Scope: promoScope(l.Source.Name, env.Name),
			Kind: "promoted", Subject: cand, Actor: l.Runner,
			Body: []byte(fmt.Sprintf(`{"env":%q,"env_commit":%q}`, env.Name, item.Head)),
		})
		return err
	case queue.StateWaiting:
		return fmt.Errorf("%w: %s", ErrAwaitingApproval, item.Note)
	default:
		return fmt.Errorf("ladder: promotion to %s ended %s: %s", env.Name, item.State, item.Note)
	}
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
