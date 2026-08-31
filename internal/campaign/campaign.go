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
func (e *Engine) Advance(ctx context.Context, ing ingress.Ingress, c ingress.Campaign, task string) ([]queue.WorkItem, error) {
	if ing.Kind != ingress.KindCampaign {
		return nil, fmt.Errorf("campaign: ingress %s is %s, not a campaign", ing.ID, ing.Kind)
	}
	prior, failures, err := e.wave(ctx, ing)
	if err != nil {
		return nil, err
	}
	if failures > c.Rollout.HaltAfter {
		return nil, fmt.Errorf("%w: %d failure(s), halt threshold %d", ErrHalted, failures, c.Rollout.HaltAfter)
	}
	todo, err := e.Unsatisfied(ctx, c)
	if err != nil {
		return nil, err
	}
	batch := c.Rollout.Batch
	if batch <= 0 {
		batch = 1
	}
	var items []queue.WorkItem
	for _, repoKey := range todo {
		if len(items) >= batch {
			break
		}
		if _, inFlight := prior[repoKey]; inFlight {
			continue // this repo already has an item from an earlier wave
		}
		id := fmt.Sprintf("%s-%s", ing.ID, sanitize(repoKey))
		item, err := e.Queue.Submit(ctx, queue.WorkItem{
			ID: id, Tenant: ing.Tenant, Box: ing.Box, Ingress: ing.ID, Task: task + " [" + repoKey + "]",
		})
		if err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, nil
}

// wave folds the states of the campaign's previously submitted items:
// which repos already have work, and how many ended badly.
func (e *Engine) wave(ctx context.Context, ing ingress.Ingress) (map[string]queue.State, int, error) {
	prior := map[string]queue.State{}
	failures := 0
	// Items are named deterministically from the ingress and repo, so the
	// fold walks the estate rather than needing a listing primitive.
	for repo, rerr := range e.Registry.Repos(ctx, ing.Tenant) {
		if rerr != nil {
			return nil, 0, rerr
		}
		key := estate.Key(repo)
		id := fmt.Sprintf("%s-%s", ing.ID, sanitize(key))
		item, err := e.Queue.Load(ctx, ing.Tenant, id)
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
