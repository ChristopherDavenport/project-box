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

// DefaultStarveAfter is how many section waits in a row mark a candidate
// starved when [Pump.StarveAfter] is unset. Small on purpose: a whole-
// repository unit sharing a box with a stream of narrower ones should surface
// as the next-in-line within a handful of ticks, not sit through a standing
// wait long enough for an operator to have to notice it — see issue #109.
const DefaultStarveAfter = 3

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
	// Meter paces both Drain's claims and, since issue #110, intake's
	// judgement decomposers: a planner run is an account spend exactly like a
	// build, and the account's own window is read nowhere but here. Required
	// whenever a box declares a judgement decomposer.
	Meter *capacity.Meter
	Queue *queue.Queue
	// StarveAfter is how many consecutive section waits mark a candidate as
	// the starved head of its (Box, Repo) group — see [DefaultStarveAfter].
	// Zero uses the default.
	StarveAfter int
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
		if p.judgement(decl) {
			// Dispatched, not run here — see [Pump.dispatchJudgement] and
			// issue #50. It settles on a later tick, either as a recorded
			// plan or a parked origination, or nothing happened this tick
			// because the claim lease or the meter was already spoken for.
			// Either way there is no plan yet for this tick to submit items
			// against.
			if err := p.dispatchJudgement(ctx, ing, decl); err != nil {
				return 0, fmt.Errorf("pump: intake %s: %w", ing.ID, err)
			}
			return 0, nil
		}
		if pl, err = plan.Make(ctx, p.Decomposers, ing, decl); err != nil {
			// A deterministic decomposer's error stays a tick error: it
			// costs nothing to retry and points at config.
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

// drainCandidate is one claimable item together with the executor and Poll
// answer resolved for it this drain — carried alongside the item so the
// fairness pass and the dispatch loop that follows it never resolve either
// twice.
type drainCandidate struct {
	item   queue.WorkItem
	lp     *loop.Loop
	ok     bool // an executor is configured for this item's (box, repo)
	polled bool // true unless the item is held on a person's review
	// holder reports whether this candidate's own run already holds one of
	// its section leases — mid-flight on a rebase, re-gate, or push that
	// only needs a re-claim to finish and free the section. See issue #143:
	// the holder is the only thing that can free the section, so telling it
	// to yield to whoever is waiting on that same section is a wait with no
	// exit.
	holder bool
}

// sectionsIntersect reports whether two units could contend for the same
// section: [queue.WorkItem.Sections] names are plan-declared, compared as
// plain strings, and empty means every section, so it alone is enough to
// force an intersection.
func sectionsIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// fairSectionOrder is issue #109's fix: a unit that names no section leases
// every one, so under a stream of newer, narrower units it can starve —
// every tick another freed section is grabbed by whoever else is waiting
// before it gets a turn. This groups this drain's candidates by (box, repo),
// and within a group whose oldest waiter has reached StarveAfter deferrals in
// a row, names it the head: every OTHER candidate in the group whose section
// set could contend with the head's is told, via YieldTo, to step aside this
// tick without even trying its own lease — and the head is moved to the
// front of the dispatch order, so that among candidates who DO hold a
// section this tick, the head is the one Drain spends a seat on first.
// A yielded unit no longer needs protecting from the capacity-exhausted
// break at all: issue #120 moved the section check ahead of Meter.Acquire,
// so a unit told to wait for it anyway never consumes a slot to learn that.
//
// Issue #143: a candidate that already [drainCandidate.holder]s one of the
// sections in play is never told to yield, no matter whose head it would
// otherwise defer to — the holder is the only thing that can free its
// section, so yielding it to the head is a wait with no exit. Holders are
// moved to the very front of the dispatch order, ahead of the head itself:
// finishing a holder's re-claim (rebase, re-gate, push) is what actually
// frees the section the head is waiting on, and with a tight capacity budget
// the dispatch-loop's break on a busy or exhausted meter would otherwise hand
// the only seat to the head every tick, leaving the holder — the one thing
// that could unblock it — never claimed at all.
//
// A candidate not [drainCandidate.ok] or not [drainCandidate.polled] is
// skipped entirely — held on a person or unrouted, it is not contending for
// a section this tick either way.
//
// Issue #143 (corrected): under a full capacity budget, Drain's dispatch
// loop breaks the moment a slot is busy or exhausted (see the break below),
// so whoever sits first in this order takes the only seat — and submission
// order alone prices a unit that owes only a rebase, a re-gate, and a push
// the same as one that has not even started a build. A [landReady] candidate
// is moved ahead of every non-head, non-holder candidate that would start a
// fresh build, even when nobody in its group has starved: finishing a
// landing frees its section sooner than any build does, and every build
// that lands first is a rebase the waiter pays for. It still yields to a
// starved head whose sections it shares, the same as any other candidate —
// a starved head and a land-ready waiter are both already waiting, and the
// head is by definition the older of the two — but among the rest it goes
// first.
//
// Issue #166: the same shape applies to the land window. A group's land
// window is one branch (Branch is repo-level config, so the (box, repo)
// group is already the right granularity), and a candidate parked there with
// a passed, kept candidate — Built set, not Unfinished, at least one passed
// gate — that has gone back to Wait at or past StarveAfter turns in a row is
// that group's land head. Unlike a section head, a land head's yield is not
// scoped by section: every other candidate in the group is told to step
// aside regardless of what it touches, because they all feed the same
// branch the head is trying to land on, and a candidate that lands there
// first is exactly the race this closes. A land head wins its group
// outright — no section head is chosen for a group that already has one —
// because a unit already reviewed and merely waiting on the branch is closer
// to done than one still contending for a section to even start building,
// and the rebase bound this answers only means what it says if the unit
// already in line is not the one made to yield to newer work. The holder
// exemption and the ordering below apply identically to both kinds of head.
func (p *Pump) fairSectionOrder(cands []drainCandidate) []drainCandidate {
	bound := p.StarveAfter
	if bound <= 0 {
		bound = DefaultStarveAfter
	}
	type groupKey struct{ box, repo string }
	groups := map[groupKey][]int{}
	var groupOrder []groupKey
	for i, c := range cands {
		if !c.ok || !c.polled {
			continue
		}
		k := groupKey{c.item.Box, c.item.Repo}
		if _, seen := groups[k]; !seen {
			groupOrder = append(groupOrder, k)
		}
		groups[k] = append(groups[k], i)
	}

	heads := map[int]bool{}
	for _, k := range groupOrder {
		idxs := groups[k] // submission order — oldest first, which is the tiebreak

		landHead := -1
		for _, i := range idxs {
			it := cands[i].item
			if it.Built == "" || it.Unfinished || len(it.PassedGates) == 0 {
				continue
			}
			if it.LandWaits < bound {
				continue
			}
			if landHead == -1 || it.LandWaits > cands[landHead].item.LandWaits {
				landHead = i
			}
		}
		if landHead != -1 {
			heads[landHead] = true
			for _, i := range idxs {
				if i == landHead || cands[i].holder {
					continue
				}
				cands[i].item.YieldTo = cands[landHead].item.ID
			}
			continue // a land head wins its group outright; no section head this tick
		}

		head := -1
		for _, i := range idxs {
			if cands[i].item.SectionWaits < bound {
				continue
			}
			if head == -1 || cands[i].item.SectionWaits > cands[head].item.SectionWaits {
				head = i
			}
		}
		if head == -1 {
			continue // nobody in this group has starved yet
		}
		heads[head] = true
		for _, i := range idxs {
			if i == head || cands[i].holder {
				continue
			}
			if sectionsIntersect(cands[head].item.Sections, cands[i].item.Sections) {
				cands[i].item.YieldTo = cands[head].item.ID
			}
		}
	}
	ordered := make([]drainCandidate, 0, len(cands))
	for i, c := range cands {
		if c.holder && !heads[i] {
			ordered = append(ordered, c)
		}
	}
	for i, c := range cands {
		if heads[i] {
			ordered = append(ordered, c)
		}
	}
	for i, c := range cands {
		if !heads[i] && !c.holder && landReady(c.item) {
			ordered = append(ordered, c)
		}
	}
	for i, c := range cands {
		if !heads[i] && !c.holder && !landReady(c.item) {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// landReady reports whether item is a kept commit already offered for
// judgement — a re-claim that owes only a rebase, a re-gate, and a push,
// never a fresh agent session. Unfinished excludes a mid-session handoff:
// that Built commit is unreviewable work in progress and still owes a build.
// See [queue.WorkItem.Built] and [queue.WorkItem.Unfinished].
func landReady(item queue.WorkItem) bool {
	return item.Built != "" && !item.Unfinished
}

// Drain runs every claimable item, within budget. It returns how many items
// it settled this pass; blocked, stopped, section-busy and over-budget items
// simply remain for the next tick. A section-busy item never spends a seat
// to learn it must wait (issue #120): its section is taken, and checked,
// before the meter is ever asked.
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

	// lp and polled are resolved once per candidate, here, rather than inside
	// the dispatch loop below: the fairness pass right after this needs both
	// to know who is actually contending for a section this tick, and Poll
	// can call a gate — asking it twice would double the very cost Poll
	// exists to avoid.
	dcs := make([]drainCandidate, len(candidates))
	for i, item := range candidates {
		lp, ok := p.loopFor(item.Box, item.Repo)
		dcs[i] = drainCandidate{item: item, lp: lp, ok: ok}
		if !ok {
			continue // no executor configured for this target; not this pump's work
		}
		// A held item is asked whether its review has answered, and if it has
		// not, it is left completely alone: no slot taken, no lease, no
		// record. Running it through the loop to learn "still pending" is how
		// an idle factory writes thousands of records a day saying nothing
		// happened. It is also not contending for a section this tick, which
		// is why the fairness pass below skips it too.
		dcs[i].polled = lp.Poll(ctx, item)
		dcs[i].holder = lp.HoldsSection(ctx, item)
	}
	dcs = p.fairSectionOrder(dcs)

	var wg sync.WaitGroup
	var mu sync.Mutex
	settled := 0
	var firstErr error
	recordErr := func(item queue.WorkItem, err error) {
		switch {
		case err == nil, errors.Is(err, queue.ErrStopped),
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
	}
	for _, dc := range dcs {
		if !dc.ok || !dc.polled {
			continue
		}
		item, lp := dc.item, dc.lp

		// Sections are taken BEFORE a seat is spent (issue #120): a candidate
		// whose section is busy learns that for the cost of a claim and a
		// wait, never a meter seat, and a busy section is not meter pacing —
		// it does not break the dispatch order for whoever comes after.
		grant, busy, terr := lp.TakeSections(ctx, item)
		if terr == nil && busy != "" {
			_, err := lp.WaitOnSection(ctx, item, busy)
			mu.Lock()
			recordErr(item, err)
			mu.Unlock()
			continue
		}
		// A hard error (an undeclared section) is rare and belongs to Run's
		// existing park path, exactly as before: dispatched with a seat and
		// no grant, so leaseSections runs again inside Run and parks it.

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
			grant.Release()
			break
		}
		if err != nil {
			grant.Release()
			return settled, err
		}
		wg.Add(1)
		p.inflight.Add(1)
		go func(item queue.WorkItem, grant *loop.SectionGrant) {
			defer wg.Done()
			defer p.inflight.Done()
			defer release()
			out, err := lp.RunHolding(ctx, item, grant)
			mu.Lock()
			defer mu.Unlock()
			if err == nil && !queue.Claimable(out.State) {
				settled++
			}
			recordErr(item, err)
		}(item, grant)
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

// planClaimRepo is the pseudo-repository a judgement dispatch is leased
// under — a namespace of its own, distinct from [queue.ClaimRepo], so a work
// item's claim and an origination's judgement claim can never contend for
// the same (repo, ref) pair.
const planClaimRepo = "_plan"

// meterPacing reports whether err is one of the meter's own pacing refusals
// rather than an infrastructure failure — the same set [Pump.Drain] treats as
// "wait for the next tick," plus [capacity.ErrUnpriced], which a planner run
// billed under a cost budget can also hit.
func meterPacing(err error) bool {
	return errors.Is(err, capacity.ErrBudget) || errors.Is(err, capacity.ErrBusy) ||
		errors.Is(err, capacity.ErrUnmetered) || errors.Is(err, capacity.ErrUnpriced) ||
		errors.Is(err, capacity.ErrCredential)
}

// dispatchJudgement moves a judgement decomposer off the intake tick — see
// issue #50. Inline, a 4-8 minute planning session stopped every claim in the
// box and serialized concurrent originations; dispatched, a judgement is a
// unit of work like any other — claimed, run under the meter's concurrency,
// settled by a later tick.
//
// Dispatch is claimed like a unit: the lease over (p.Tenant, planClaimRepo,
// ing.ID) is the one-judgement-per-origination mutual exclusion, the same
// shape a work item's own claim lease already gives one runner. [forge.ErrHeld]
// means a judgement is already in flight, this runner or another, so dispatch
// skips silently and the next tick tries again; a lease that lapsed because
// its holder crashed mid-run is recovered the same way [queue.Queue.Recover]
// recovers a claim — taking the lease IS the proof the previous holder is
// gone, no separate liveness check needed.
//
// The meter is acquired next, exactly as [Pump.Drain] acquires it before a
// claim runs — never [capacity.Meter.Start], which is a build-only charge;
// billing a planner's start here would reopen issue #45's double-count. A
// pacing refusal (busy, over budget, an unmetered or unpriced window, a
// credential brake) releases the lease and skips silently, with no record:
// the meter's own window is why nothing happened, and the next tick's
// Acquire is the retry — the same posture Drain already takes at claim. This
// leaves no PRE-dispatch use for the retry-in-place scheme issue #110 built
// for a judgement run inline in the tick ([plan.Store.Defer],
// [plan.DeferredKind]): once dispatch is off the tick there is no tick left
// to retry a refused Acquire within, and a meter refusal before dispatch is
// already the same silent, next-tick-retried pacing every claim gets.
//
// Issue #112 revives Defer for the case Acquire cannot see: a dispatched run
// that gets past Acquire and THEN has its own provider account report the
// window spent mid-flight ([loop.WindowSpent]). That is not this pacing —
// the meter's own budget never refused anything here — so it is recorded
// rather than silently retried, bounded by [loop.MaxWindowDefers] the same
// way a claim's own build-or-gate deferral is.
//
// Once both are held, [plan.Store.Claim] records the dispatch and the
// judgement runs in a goroutine registered with p.inflight — so Quiesce
// waits for a paid planner run exactly as it waits for a build — releasing
// the lease and the meter slot on the way out. Any [plan.Make] error fails
// the origination via [plan.Store.Fail]: a failed judgement parks the
// origination, not the tick (PLAN.md §6), identical to the failure this fork
// always gave a judgement that answered. A plan is recorded via
// [plan.Store.Record] on success; the items it calls for are submitted by a
// later tick's ordinary planned path.
func (p *Pump) dispatchJudgement(ctx context.Context, ing ingress.Ingress, decl kind.Declaration) error {
	leaser := p.Store.Leaser(p.Runner)
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: p.Tenant, Repo: planClaimRepo, Ref: ing.ID, Run: "plan-" + ing.ID, TTL: p.Queue.TTL,
	})
	if errors.Is(err, forge.ErrHeld) {
		return nil // a judgement is already in flight; the next tick tries again
	}
	if err != nil {
		return err
	}
	release, err := p.Meter.Acquire(ctx, "plan-"+ing.ID)
	if err != nil {
		leaser.Release(ctx, lease)
		if meterPacing(err) {
			return nil // pacing, not failure — the same posture Drain takes at claim
		}
		return err
	}
	if err := p.Plans.Claim(ctx, ing); err != nil {
		release()
		leaser.Release(ctx, lease)
		return err
	}
	p.inflight.Add(1)
	go func() {
		defer p.inflight.Done()
		defer release()
		defer leaser.Release(ctx, lease)
		// Nothing downstream of this goroutine is still waiting on this
		// tick's return, so a write failure here has nowhere synchronous to
		// surface — the same posture Release's own dropped error already
		// takes throughout this package.
		pl, err := plan.Make(ctx, p.Decomposers, ing, decl)
		if err != nil {
			var ws *loop.WindowSpent
			if errors.As(err, &ws) {
				if resetsAt, ok := ws.ResolveReset(p.Meter); ok {
					if standing, serr := p.Plans.Standing(ctx, ing.Tenant, ing.ID); serr == nil {
						if standing.Defers < loop.MaxWindowDefers {
							body := fmt.Sprintf(`{"window":%q,"resets_at":%d}`, ws.Window, resetsAt.Unix())
							_ = p.Plans.Defer(ctx, ing, body)
							return
						}
						_ = p.Plans.Fail(ctx, ing, fmt.Sprintf(
							"deferred for the account window %d times without answering, and the limit is %d — failing rather than deferring again: %s",
							standing.Defers+1, loop.MaxWindowDefers, ws.Error()))
						return
					}
				}
				// Unknown never reads as zero: no reset named and no account
				// window on record either (or the standing could not be
				// read), so this falls through to today's Fail path.
			}
			_ = p.Plans.Fail(ctx, ing, err.Error())
			return
		}
		_, _ = p.Plans.Record(ctx, ing, pl)
	}()
	return nil
}
