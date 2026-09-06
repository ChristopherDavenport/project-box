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
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
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
	// Kinds is the declaration set intake resolves an origination's kind
	// against, and Decomposers the producers those declarations name. Plans
	// records what each decomposition decided.
	//
	// All three are required for intake to run, and none of them has a
	// forgiving default. A pump with no declarations admits nothing, loudly,
	// which is the honest failure: the alternative — falling back to a
	// built-in identity plan for a kind nobody declared — is precisely the
	// silent widening of an envelope that made the closed enum a mistake.
	Kinds       kind.Sets
	Decomposers *plan.Registry
	Plans       *plan.Store

	mu sync.RWMutex // guards Loops swaps against in-flight drains
	// inflight tracks running item executions for graceful drain: on
	// shutdown the daemon stops claiming and WAITS here — killing a paid
	// agent run mid-item is safe under fencing, but it is waste, and waste
	// with a half-written story.
	inflight sync.WaitGroup
}

// Quiesce blocks until every in-flight run settles or the context ends,
// reporting whether the pump drained clean.
func (p *Pump) Quiesce(ctx context.Context) bool {
	done := make(chan struct{})
	go func() { p.inflight.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// SetLoops swaps the executor table — the heart of config reload. In-flight
// runs keep the loop they started with; the next drain sees the new table.
func (p *Pump) SetLoops(loops map[string]*loop.Loop) {
	p.mu.Lock()
	p.Loops = loops
	p.mu.Unlock()
}

// SetKinds swaps the declaration table, under the same lock and for the same
// reason: a reload that changed what a box may open work under while a drain
// was reading the old table would decompose one origination against two
// policies.
func (p *Pump) SetKinds(kinds kind.Sets) {
	p.mu.Lock()
	p.Kinds = kinds
	p.mu.Unlock()
}

func (p *Pump) kinds() kind.Sets {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Kinds
}

func (p *Pump) loopFor(box, repo string) (*loop.Loop, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if lp, ok := p.Loops[box+"/"+repo]; ok {
		return lp, true
	}
	lp, ok := p.Loops[box]
	return lp, ok
}

// Intake decomposes every origination that has not been decomposed yet, and
// submits the graph its plan called for.
//
// Universal, which is the change. Intake used to admit issues and silently
// step over everything else — a campaign advanced through its own engine, and
// an incident produced no work at all and was an authority token wearing a
// work item's struct. Now every ingress runs the decomposition step and emits
// a graph: an ordinary issue emits a one-node graph, a campaign emits its
// predicate's fan-out, and an incident emits restore-then-postmortem as the
// edge it always described in prose.
//
// Idempotency has moved one link up and is otherwise the same rule as delivery
// keys. The PLAN is what intake checks for, not the item: item ids are still
// deterministic, but a plan that has been recorded must never be recomputed,
// because a decomposer over moving facts would produce a second graph on the
// next tick and every item on the first would be orphaned from the decision
// that made it.
func (p *Pump) Intake(ctx context.Context) (int, error) {
	created := 0
	var unreadable error
	for rec, err := range p.Store.Log().Since(ctx, p.Tenant, "index/ingress", 1) {
		if err != nil {
			return created, err
		}
		n, err := p.intakeOne(ctx, rec.Subject)
		created += n
		if err != nil {
			// One origination that cannot be decomposed must not be a gate
			// across the door: the rest of the factory's intake still runs,
			// and the error still surfaces. Same posture the unreadable-chain
			// case has taken since a single corrupt item stopped 31
			// consecutive ticks from doing anything.
			unreadable = errors.Join(unreadable, err)
		}
	}
	return created, unreadable
}

// intakeOne decomposes and submits one origination's graph.
func (p *Pump) intakeOne(ctx context.Context, ingressID string) (int, error) {
	ing, ok, err := p.findIngress(ctx, ingressID)
	if err != nil {
		return 0, fmt.Errorf("pump: intake %s: %w", ingressID, err)
	}
	if !ok {
		return 0, nil
	}
	decl, err := p.kinds().Lookup(ing.Box, string(ing.Kind))
	if err != nil {
		// A kind nobody declared is a configuration gap, and the work waits
		// rather than being guessed at. Refusing here is the same call the
		// config makes about a repo with neither an agent nor a build command:
		// an operator who configured a thing and got nothing should be told,
		// not left to infer it from a queue that never grew.
		return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, err)
	}
	if decl.Decompose.Standing {
		// A reconciler's decomposition belongs to its standing task, which
		// throttles it into waves and knows when the last one soaked. Intake
		// planning it would enumerate a campaign, which is the one thing a
		// campaign is defined by not doing.
		return 0, nil
	}
	standing, err := p.Plans.Standing(ctx, p.Tenant, ing.ID)
	if err != nil {
		return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, err)
	}
	if standing.Blocked() {
		// A judgement that failed and nobody has retried, or an origination
		// somebody gave up on. It is in the exception queue with its reason;
		// see [plan.Store.Fail] for why it is not tried again here.
		return 0, nil
	}
	pl, planned, err := p.Plans.Latest(ctx, p.Tenant, ing.ID)
	if err != nil {
		return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, err)
	}
	if !planned {
		if pl, err = plan.Make(ctx, p.Decomposers, ing, decl); err != nil {
			if p.judgement(decl) {
				// The seat whose failure parks the one origination that asked
				// for a judgement. A deterministic decomposer's error stays a
				// tick error: it costs nothing to retry and points at config.
				if ferr := p.Plans.Fail(ctx, ing, err.Error()); ferr != nil {
					return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, ferr)
				}
				return 0, nil
			}
			return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, err)
		}
		// Recorded BEFORE the work exists. A graph whose items predate the
		// plan that called for them is a plan reconstructed from its
		// consequences, and the whole reason the decomposition is recorded is
		// that it cannot be.
		if pl, err = p.Plans.Record(ctx, ing, pl); err != nil {
			return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, err)
		}
	}
	created := 0
	for _, item := range plan.Items(ing, pl) {
		switch _, err := p.Queue.Load(ctx, p.Tenant, item.ID); {
		case err == nil:
			continue // already has its item
		case !errors.Is(err, queue.ErrNoItem):
			// Absence is the only answer that may create work. Treating an
			// unreadable chain as "no item yet" re-queued a parked item on
			// every tick, which is a spend loop wearing an idempotency key.
			return created, fmt.Errorf("pump: intake %s: %w", item.ID, err)
		}
		if _, err := p.Queue.Submit(ctx, item); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// Drain runs every claimable item, within budget. It returns how many items
// it settled this pass; blocked, stopped, busy and over-budget items simply
// remain for the next tick.
func (p *Pump) Drain(ctx context.Context) (int, error) {
	var candidates []queue.WorkItem
	// One unreadable item must not cost every other item its tick. Aborting
	// the pass here meant a single corrupt chain stopped the whole factory
	// draining — 31 consecutive ticks did nothing at all — while the cause
	// scrolled past in the log as one line among many. Collect and carry on:
	// the healthy work runs, and the error still surfaces.
	var unreadable error
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
			unreadable = errors.Join(unreadable, err)
			continue
		}
		// A claimed item whose runner is gone is work nobody is doing and
		// nobody was going to: the drain never looked at claimed items, so a
		// runner lost mid-run took its item with it permanently.
		if item.State == queue.StateClaimed {
			recovered, ok, rerr := p.Queue.Recover(ctx, item)
			if rerr != nil {
				unreadable = errors.Join(unreadable, rerr)
				continue
			}
			if !ok {
				continue // still held; not this pump's business
			}
			item = recovered
		}
		// Handed off is claimable too, and this is the re-dispatch the verb
		// promises: a session that asked for a fresh one gets one on the next
		// tick, from the same executor and therefore the same seat.
		if queue.Claimable(item.State) {
			candidates = append(candidates, item)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	settled := 0
	var firstErr error
	for _, item := range candidates {
		lp, ok := p.loopFor(item.Box, item.Repo)
		if !ok {
			continue // no executor configured for this target; not this pump's work
		}
		// A held item is asked whether its review has answered, and if it has
		// not, it is left completely alone: no slot taken, no lease, no
		// record. Running it through the loop to learn "still pending" is how
		// an idle factory writes thousands of records a day saying nothing
		// happened.
		if !lp.Poll(ctx, item) {
			continue
		}
		release, err := p.Meter.Acquire(ctx, item.ID)
		if errors.Is(err, capacity.ErrBudget) || errors.Is(err, capacity.ErrBusy) ||
			errors.Is(err, capacity.ErrUnmetered) || errors.Is(err, capacity.ErrCredential) {
			// Pacing, not failure: the rest waits for the next tick. An
			// unmetered window is pacing too — the meter has lost track of
			// spend, and the factory holds until the window turns over
			// rather than running on an account it cannot read. A credential
			// mismatch or an unnamed one under a brake is the same shape: a
			// factory that has stopped on purpose because it no longer knows
			// — or was never told — which account it is spending, not a
			// tick that failed.
			break
		}
		if err != nil {
			return settled, err
		}
		wg.Add(1)
		p.inflight.Add(1)
		go func(item queue.WorkItem) {
			defer wg.Done()
			defer p.inflight.Done()
			defer release()
			out, err := lp.Run(ctx, item)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				if !queue.Claimable(out.State) {
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
	return settled, errors.Join(unreadable, firstErr)
}

// Tick is one heartbeat: feed, then drain.
//
// Intake's error no longer short-circuits the drain. An intake that could not
// read one chain has still fed every other ingress, and the work already in
// the queue is owed its tick regardless — the alternative is what happened in
// practice, a factory that stopped doing anything because one item's chain
// had a record in it nobody could parse.
func (p *Pump) Tick(ctx context.Context) (int, error) {
	_, intakeErr := p.Intake(ctx)
	settled, drainErr := p.Drain(ctx)
	return settled, errors.Join(intakeErr, drainErr)
}

func (p *Pump) findIngress(ctx context.Context, id string) (ingress.Ingress, bool, error) {
	return ingress.Load(ctx, p.Store.Log(), p.Tenant, id)
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

// judgement reports whether a declaration's decomposer is one that judges —
// not replayable — as opposed to one that derives.
func (p *Pump) judgement(decl kind.Declaration) bool {
	d, err := p.Decomposers.Resolve(decl.Decomposer())
	return err == nil && !d.Replayable()
}
