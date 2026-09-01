// Package pump is standing operation: step up ingress and step back, and the
// system consistently builds itself.
//
// Intake turns every ingress into a work item; Drain runs every claimable
// item within the capacity budget; the Daemon ticks both plus whatever
// standing tasks the Box configures — census sweeps, campaign advances,
// ladder promotions. Nothing here decides anything: the queue's graph
// decides order, the meter decides pace, the loop decides outcome, and the
// stop button halts claims exactly as it does for a human-driven run.
package pump

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Pump feeds and drains one tenant's queue.
type Pump struct {
	Store  store.Store
	Tenant tenant.ID
	Runner string
	// Loops routes items to executors, most specific key first:
	// "box/repo" for a Box with several repositories, "box" as the single-
	// executor fallback. Repositories in ONE tenant and ONE box run side by
	// side — their leases key on (tenant, repo, ref), so they contend for
	// nothing.
	Loops map[string]*loop.Loop
	Meter *capacity.Meter
	Queue *queue.Queue
}

// Intake creates a work item for every ingress that lacks one. Item ids are
// deterministic from the ingress, so intake is idempotent however often it
// ticks — the same rule as delivery keys, one level up.
func (p *Pump) Intake(ctx context.Context) (int, error) {
	created := 0
	for rec, err := range p.Store.Log().Since(ctx, p.Tenant, "index/ingress", 1) {
		if err != nil {
			return created, err
		}
		id := "wi-" + rec.Subject
		if _, err := p.Queue.Load(ctx, p.Tenant, id); err == nil {
			continue // already has its item
		}
		ing, ok, err := p.findIngress(ctx, rec.Subject)
		if err != nil {
			return created, err
		}
		if !ok || ing.Kind != ingress.KindIssue {
			continue // campaigns and incidents advance through their own engines
		}
		task := ing.Note
		if task == "" {
			task = ing.Title
		}
		if task == "" {
			task = "attend to ingress " + ing.ID
		}
		if _, err := p.Queue.Submit(ctx, loop.ItemFor(ing, id, task)); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// Drain runs every claimable pending or waiting item, within budget. It
// returns how many items it settled this pass; blocked, stopped, busy and
// over-budget items simply remain for the next tick.
func (p *Pump) Drain(ctx context.Context) (int, error) {
	var candidates []queue.WorkItem
	seen := map[string]bool{}
	for rec, err := range p.Store.Log().Since(ctx, p.Tenant, "index/pending", 1) {
		if err != nil {
			return 0, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		item, err := p.Queue.Load(ctx, p.Tenant, rec.Subject)
		if err != nil {
			return 0, err
		}
		if item.State == queue.StatePending || item.State == queue.StateWaiting {
			candidates = append(candidates, item)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	settled := 0
	var firstErr error
	for _, item := range candidates {
		lp, ok := p.Loops[item.Box+"/"+item.Repo]
		if !ok {
			lp, ok = p.Loops[item.Box]
		}
		if !ok {
			continue // no executor configured for this target; not this pump's work
		}
		release, err := p.Meter.Acquire(ctx, item.ID)
		if errors.Is(err, capacity.ErrBudget) || errors.Is(err, capacity.ErrBusy) {
			break // pacing, not failure: the rest waits for the next tick
		}
		if err != nil {
			return settled, err
		}
		wg.Add(1)
		go func(item queue.WorkItem) {
			defer wg.Done()
			defer release()
			out, err := lp.Run(ctx, item)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				if out.State != queue.StatePending && out.State != queue.StateWaiting {
					settled++
				}
			case errors.Is(err, queue.ErrStopped),
				errors.Is(err, queue.ErrBlocked),
				errors.Is(err, queue.ErrDependencyFailed),
				errors.Is(err, forge.ErrHeld):
				// The queue said "not now" or "not ever" and has already
				// recorded why; the pump moves on.
			default:
				if firstErr == nil {
					firstErr = fmt.Errorf("pump: item %s: %w", item.ID, err)
				}
			}
		}(item)
	}
	wg.Wait()
	return settled, firstErr
}

// Tick is one heartbeat: feed, then drain.
func (p *Pump) Tick(ctx context.Context) (int, error) {
	if _, err := p.Intake(ctx); err != nil {
		return 0, err
	}
	return p.Drain(ctx)
}

func (p *Pump) findIngress(ctx context.Context, id string) (ingress.Ingress, bool, error) {
	head, err := p.Store.Log().Head(ctx, p.Tenant, "ingress/"+id)
	if err != nil || head.Seq == 0 {
		return ingress.Ingress{}, false, err
	}
	var ing ingress.Ingress
	if err := ingressUnmarshal(head.Body, &ing); err != nil {
		return ingress.Ingress{}, false, err
	}
	return ing, true, nil
}

// Daemon ticks the pump and the Box's standing tasks until the context ends.
// This is the "step back" half: after Start, nothing needs a human until the
// exception queue does.
type Daemon struct {
	Every time.Duration
	Pump  *Pump
	// Tasks are the standing jobs beyond the queue: census sweeps, campaign
	// advances, ladder promotions. Each runs every tick; an error is
	// reported to Errs and the daemon carries on — one broken task must not
	// stop the factory's heart.
	Tasks map[string]func(ctx context.Context) error
	// Errs receives task failures; nil discards them (they are still in
	// evidence wherever the task records).
	Errs func(task string, err error)
}

// Run ticks until ctx is done.
func (d *Daemon) Run(ctx context.Context) error {
	t := time.NewTicker(d.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if _, err := d.Pump.Tick(ctx); err != nil && d.Errs != nil {
				d.Errs("pump", err)
			}
			for name, task := range d.Tasks {
				if err := task(ctx); err != nil && d.Errs != nil {
					d.Errs(name, err)
				}
			}
		}
	}
}
