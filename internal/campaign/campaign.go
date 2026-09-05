// Package campaign turns a predicate over the estate into throttled work.
//
// A campaign is continuously evaluated, never enumerated up front: it
// generates work for repositories that do not yet satisfy its predicate and
// is finished when none remain — and a repository added tomorrow reopens it.
// The predicate is a fact key comparison, deliberately declarative: a
// campaign definition is diffable, auditable, and cannot run code.
package campaign

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/estate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// Predicate is one fact key comparison: "<key> <op> <want>", e.g.
// "go.version >= 1.25". Ops: = != < <= > >= exists. Values that parse as
// dotted numbers compare numerically segment by segment (1.9 < 1.25 is a
// version fact's truth), everything else compares lexically.
type Predicate struct {
	Key, Op, Want string
}

// ParsePredicate reads the textual form.
func ParsePredicate(s string) (Predicate, error) {
	fields := strings.Fields(s)
	switch {
	case len(fields) == 2 && fields[1] == "exists":
		return Predicate{Key: fields[0], Op: "exists"}, nil
	case len(fields) == 3:
		switch fields[1] {
		case "=", "!=", "<", "<=", ">", ">=":
			return Predicate{Key: fields[0], Op: fields[1], Want: fields[2]}, nil
		}
	}
	return Predicate{}, fmt.Errorf("campaign: cannot parse predicate %q — want \"<key> <op> <value>\" or \"<key> exists\"", s)
}

// Holds reports whether a fact value satisfies the predicate.
func (p Predicate) Holds(value string, present bool) bool {
	if p.Op == "exists" {
		return present
	}
	if !present {
		return false
	}
	c := compare(value, p.Want)
	switch p.Op {
	case "=":
		return c == 0
	case "!=":
		return c != 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	}
	return false
}

func compare(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	numeric := true
	for _, s := range append(append([]string{}, as...), bs...) {
		if _, err := strconv.Atoi(s); err != nil {
			numeric = false
			break
		}
	}
	if !numeric {
		return strings.Compare(a, b)
	}
	for i := 0; i < len(as) || i < len(bs); i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ErrStale reports a fact too old to act on. A campaign acting on a stale
// fact redoes finished work or skips new work across the whole fleet at once,
// so the engine refuses rather than guessing.
var ErrStale = errors.New("campaign: fact is stale — refusing to act; run the census")

// ErrHalted reports that failures crossed the campaign's halt threshold.
var ErrHalted = errors.New("campaign: halted — prior wave failed")

// Engine evaluates campaigns against the estate and advances them as work.
type Engine struct {
	Registry *estate.StoreRegistry
	Queue    *queue.Queue
	// Plans records each wave's decomposition on the origination's chain.
	Plans *plan.Store
	// MaxFactAge bounds how old a fact may be before the engine refuses it.
	MaxFactAge time.Duration
}

// Unsatisfied implements [ingress.Evaluator]: the enrolled repositories in
// scope whose facts do not satisfy the predicate.
func (e *Engine) Unsatisfied(ctx context.Context, c ingress.Campaign) ([]string, error) {
	p, err := ParsePredicate(c.Predicate)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []string
	for r, rerr := range e.Registry.Repos(ctx, e.Registry.Tenant) {
		if rerr != nil {
			return nil, rerr
		}
		key := estate.Key(r)
		enr, enrolled, err := e.Registry.Enrollment(ctx, key)
		if err != nil {
			return nil, err
		}
		if !enrolled || !inScope(c, enr) {
			continue
		}
		facts, err := e.Registry.Facts(ctx, key)
		if err != nil {
			return nil, err
		}
		value, present, observed := "", false, time.Time{}
		for _, f := range facts {
			if f.Key == p.Key {
				value, present, observed = f.Value, true, f.Observed
			}
		}
		if present && e.MaxFactAge > 0 && (estate.Fact{Observed: observed}).Stale(now, e.MaxFactAge) {
			return nil, fmt.Errorf("%w: %s %s observed %s", ErrStale, key, p.Key, observed.Format(time.RFC3339))
		}
		if !p.Holds(value, present) {
			out = append(out, key)
		}
	}
	return out, nil
}

// inScope: both narrowings empty means every enrolled repo; otherwise a repo
// qualifies by role or by canonical name.
func inScope(c ingress.Campaign, e estate.Enrollment) bool {
	if len(c.Roles) == 0 && len(c.Repos) == 0 {
		return true
	}
	for _, r := range c.Roles {
		if r == e.Role {
			return true
		}
	}
	for _, name := range c.Repos {
		if name == e.Repo {
			return true
		}
	}
	return false
}

// Advance creates the next wave of work for a campaign, throttled by its
// rollout. Batch zero means ONE, never unlimited; a prior wave's failures at
// or past the halt threshold stop the campaign instead of spreading it.
//
// The wave is now a recorded PLAN, which is what a campaign always lacked. Its
// fan-out was computed, thrown away, and visible afterwards only as however
// many items happened to appear — so "which repositories did this wave choose,
// and which did it leave out" was a question with no artifact behind it. Each
// wave opens its own plan on the origination's chain and every item names the
// wave that called for it; see [kind.Decomposition.Standing] for why a new
// plan rather than an amended one.
//
// Halting is a decision about whether to plan at all, so it comes first: a
// halted campaign records no plan, because a plan is a statement that this is
// the work, and a halted campaign's answer is that there is none.
func (e *Engine) Advance(ctx context.Context, ing ingress.Ingress, decl kind.Declaration) ([]queue.WorkItem, error) {
	if ing.Campaign == nil {
		return nil, fmt.Errorf("campaign: ingress %s carries no campaign parameters — a predicate is what there is to reconcile toward", ing.ID)
	}
	_, failures, err := e.wave(ctx, ing)
	if err != nil {
		return nil, err
	}
	if failures > ing.Campaign.Rollout.HaltAfter {
		return nil, fmt.Errorf("%w: %d failure(s), halt threshold %d", ErrHalted, failures, ing.Campaign.Rollout.HaltAfter)
	}
	reg, err := plan.NewRegistry(Decomposer{Engine: e})
	if err != nil {
		return nil, err
	}
	p, err := plan.Make(ctx, reg, ing, decl)
	if err != nil {
		return nil, err
	}
	if p, err = e.Plans.Record(ctx, ing, p); err != nil {
		return nil, err
	}
	var items []queue.WorkItem
	for _, w := range plan.Items(ing, p) {
		item, err := e.Queue.Submit(ctx, w)
		if err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, nil
}

// Decomposer is the campaign engine as a [plan.Decomposer]: one wave of a
// predicate's fan-out, throttled.
//
// A campaign stops being a kind that happens to fan out and becomes one
// decomposer among several. What used to make it special was that it bypassed
// intake and produced work through its own engine; what makes it different now
// is one declared field — that its decomposition is standing rather than
// once — and the estate read it needs, which is why it lives here rather than
// in the plan package.
type Decomposer struct{ Engine *Engine }

// Name satisfies [plan.Decomposer].
func (Decomposer) Name() string { return kind.DecomposeCampaign }

// Replayable satisfies [plan.Decomposer].
//
// True, and it is the honest answer rather than the flattering one: the wave
// is a pure function of the predicate, the enrolled estate's facts, and which
// repositories already have work. Re-running it against FRESHER facts yielding
// a different graph is not a violation of that — it is the mechanism by which
// a plan built on stale facts announces itself.
func (Decomposer) Replayable() bool { return true }

// Decompose emits one wave: the unsatisfied repositories in scope, minus those
// already carrying work from an earlier wave, capped by the rollout batch.
func (d Decomposer) Decompose(ctx context.Context, ing ingress.Ingress, _ kind.Declaration) (plan.Result, error) {
	if ing.Campaign == nil {
		return plan.Result{}, fmt.Errorf("ingress %s carries no campaign parameters", ing.ID)
	}
	c := *ing.Campaign
	prior, _, err := d.Engine.wave(ctx, ing)
	if err != nil {
		return plan.Result{}, err
	}
	todo, err := d.Engine.Unsatisfied(ctx, c)
	if err != nil {
		return plan.Result{}, err
	}
	batch := c.Rollout.Batch
	if batch <= 0 {
		batch = 1
	}
	now := time.Now()
	// The predicate is the first fact the plan read, and the unsatisfied set
	// is the rest. Recorded so a wave can be read back as a decision rather
	// than as an outcome: "these were the repositories that did not satisfy
	// `go.version >= 1.25` at this moment, and this batch is the ones I took."
	facts := []plan.Fact{{Key: "predicate", Value: c.Predicate, AsOf: now}}
	task := plan.TaskOf(ing)
	var nodes []plan.Node
	for _, repoKey := range todo {
		facts = append(facts, plan.Fact{Key: "unsatisfied/" + repoKey, Value: c.Predicate, AsOf: now})
		if len(nodes) >= batch {
			continue // out of scope for THIS wave, still a fact this plan read
		}
		if _, inFlight := prior[repoKey]; inFlight {
			continue // this repo already has an item from an earlier wave
		}
		nodes = append(nodes, plan.Node{ID: sanitize(repoKey), Task: task + " [" + repoKey + "]"})
	}
	if len(nodes) == 0 {
		return plan.Result{}, fmt.Errorf("nothing to do this wave: %d unsatisfied, %d already in flight", len(todo), len(prior))
	}
	return plan.Result{Nodes: nodes, Facts: facts}, nil
}

// wave folds the states of the campaign's previously submitted items:
// which repos already have work, and how many ended badly.
func (e *Engine) wave(ctx context.Context, ing ingress.Ingress) (map[string]queue.State, int, error) {
	prior := map[string]queue.State{}
	failures := 0
	// Items are named deterministically from the origination and the repo, so
	// the fold walks the estate rather than needing a listing primitive.
	for repo, rerr := range e.Registry.Repos(ctx, ing.Tenant) {
		if rerr != nil {
			return nil, 0, rerr
		}
		key := estate.Key(repo)
		item, err := e.Queue.Load(ctx, ing.Tenant, itemID(ing.ID, key))
		if err != nil {
			continue // no item for this repo yet
		}
		prior[key] = item.State
		if item.State == queue.StateParked || item.State == queue.StateFailed {
			failures++
		}
	}
	return prior, failures, nil
}

// itemID is the queue id a campaign's node takes, and it must agree with
// [plan.Plan.ItemID] exactly. Two spellings would make the wave fold blind to
// the wave the plan submitted, and a campaign that cannot see its own prior
// work re-submits it every tick.
func itemID(ingressID, repoKey string) string {
	return "wi-" + ingressID + "-" + sanitize(repoKey)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			return r
		default:
			return '-'
		}
	}, s)
}
