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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
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

// DefaultLandPreemptions is how many times in a row a land-ready candidate
// may go ahead of a starved section head it shares a section with, when
// [Pump.LandPreemptions] is unset. Small on purpose: a land-ready unit a
// land-step from freeing its section should not make a starved head wait
// out a whole fresh build-and-review cycle to learn that, but a head that
// keeps losing its turn to a stream of newer land-ready arrivals still has
// to win eventually — see issue #207.
const DefaultLandPreemptions = 2

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
	// LandPreemptions bounds how many times in a row a land-ready candidate
	// may go ahead of a starved section head it shares a section with
	// instead of yielding — see [DefaultLandPreemptions] and issue #207.
	// Zero uses the default.
	LandPreemptions int
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
	// Replanner asks the planner to revise a unit whose builder escalation
	// [loop.WidenTests] could not grant in place — see [plan.Replanner] and
	// issue #182. Nil-safe: a nil Replanner means [Pump.Replan] parks a
	// unit awaiting one as an exception, exactly as an unwidenable
	// escalation always has, so a Box with no planner configured sees
	// nothing new.
	Replanner plan.Replanner

	mu sync.RWMutex // guards Loops swaps against in-flight drains
	// inflight tracks running item executions for graceful drain: on
	// shutdown the daemon stops claiming and WAITS here — killing a paid
	// agent run mid-item is safe under fencing, but it is waste, and waste
	// with a half-written story.
	inflight sync.WaitGroup
	// runs accumulates the outcome of every dispatched run goroutine that
	// finishes after the Drain call that started it has already returned —
	// see [Pump.Drain] and issue #196. Its own mutex, never p.mu: p.mu
	// guards Loops swaps against a drain in flight, and a run goroutine
	// posting its outcome is unrelated traffic that must never contend with
	// that swap.
	runs runTally
	// builds tracks the cancel func for every build dispatch currently in
	// flight, keyed by item id, so [Pump.CancelBuild] can stop the one
	// goroutine actually running it rather than merely being outrun by a
	// fence (issue #270). Its own mutex, never p.mu, for the same reason
	// runs has one: CancelBuild fires from a queue hook or a loop's
	// reapGhost, arbitrary goroutines that must never contend with a Loops
	// swap.
	builds buildRegistry
	// wireOnce installs both cancellation hooks — p.Queue.Interrupt and
	// every configured Loop's Interrupt — the first time [Pump.Tick] or
	// [Pump.Drain] runs. Nothing constructs a Pump through a function; the
	// daemon builds the struct literal directly, so there is no single
	// place to wire eagerly, and paying a sync.Once check on every later
	// call is cheaper than a constructor nobody would be forced to use.
	wireOnce sync.Once
}

// buildCancel is one build dispatch's cancel func, held behind a pointer so
// [buildRegistry.deregister] can tell "the entry I registered" from "a later
// dispatch's entry that happens to share this item's id" by identity rather
// than by comparing func values, which Go does not allow. This is the
// generation guard: a superseded build's goroutine deregisters only its own
// entry, never a successor's that has since claimed the same id.
type buildCancel struct {
	cancel context.CancelFunc
}

// buildRegistry maps an in-flight build's item id to the means of stopping
// it. See [Pump.builds] for why it carries its own mutex.
type buildRegistry struct {
	mu   sync.Mutex
	live map[string]*buildCancel
}

// register records cancel under id, returning the entry the caller must hand
// back to [buildRegistry.deregister] when the build ends.
func (r *buildRegistry) register(id string, cancel context.CancelFunc) *buildCancel {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live == nil {
		r.live = make(map[string]*buildCancel)
	}
	entry := &buildCancel{cancel: cancel}
	r.live[id] = entry
	return entry
}

// deregister removes id's entry, but only if it is still exactly the entry
// the caller registered — a re-dispatch of the same id (Recover, then a
// later tick's re-claim) has since overwritten it with its own, and that
// entry belongs to the new build, not this one.
func (r *buildRegistry) deregister(id string, entry *buildCancel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[id] == entry {
		delete(r.live, id)
	}
}

// cancel stops the build registered under id, if any. A miss — no build
// registered, or one that already finished and deregistered itself — is a
// silent no-op: canceling something that no longer exists is not an error,
// it is the common case.
func (r *buildRegistry) cancel(id string) {
	r.mu.Lock()
	entry := r.live[id]
	r.mu.Unlock()
	if entry != nil {
		entry.cancel()
	}
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
	// A reload swaps the whole table, and a freshly swapped-in Loop has
	// never had [loop.Loop.Interrupt] wired — [Pump.ensureWired]'s Once has
	// already fired by the time any reload happens, so nothing else will
	// wire this new table.
	p.wireLoops()
}

// CancelBuild stops the build running under item id, if [Pump.builds] has
// one registered. It is installed as both p.Queue.Interrupt and every
// configured Loop's Interrupt by [Pump.ensureWired] — a supersede or reopen
// reaches it through the queue, a stalled ghost through reapGhost — so
// either caller stops the actual goroutine instead of merely being outrun by
// the fence it already lost to.
//
// No-ops on an id with nothing registered: settled work, a judgement or
// replan dispatch (never registered here — see the comments at their
// dispatch sites), or a build that already finished on its own.
func (p *Pump) CancelBuild(id string) {
	p.builds.cancel(id)
}

// ensureWired installs both cancellation hooks exactly once: p.Queue's
// Interrupt, fired by [queue.Queue.Recover] and the two directions of
// [queue.Queue.Retry]/[queue.Queue.Resolve]'s reopen, and p.CancelBuild on
// every currently-configured Loop, fired by [loop.Loop]'s reapGhost. Called
// at the top of Tick and Drain — tests call Drain directly, so Tick alone
// would leave them unwired — rather than in a constructor, because nothing
// builds a Pump through one; the daemon assembles the struct literal
// directly.
func (p *Pump) ensureWired() {
	p.wireOnce.Do(func() {
		p.Queue.Interrupt = p.CancelBuild
		p.wireLoops()
	})
}

// wireLoops sets [loop.Loop.Interrupt] to p.CancelBuild on every
// currently-configured loop.
func (p *Pump) wireLoops() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, lp := range p.Loops {
		lp.Interrupt = p.CancelBuild
	}
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

// settledFilter reports whether err is one the queue has already recorded
// the reason for — "not now" (stopped, blocked, held) or "not ever"
// (a dependency failed, or a stale claim a successor already superseded) —
// so neither Drain's own dispatch-time errors nor a run goroutine's outcome
// treat it as a failure worth surfacing. queue.ErrSuperseded is the same
// shape as the rest: [loop.Loop.dropped] already narrated it on the item's
// own chain (issue #266), so surfacing it again here as a pump-level error
// would draw an operator's eye to a build that was never going to resurrect
// anything.
func settledFilter(err error) bool {
	return err == nil || errors.Is(err, queue.ErrStopped) ||
		errors.Is(err, queue.ErrBlocked) ||
		errors.Is(err, queue.ErrDependencyFailed) ||
		errors.Is(err, queue.ErrSuperseded) ||
		errors.Is(err, forge.ErrHeld)
}

// runTally accumulates what dispatched run goroutines settle between one
// Drain call and the next: a count of items that reached a terminal,
// non-claimable state, and the first error worth surfacing. It outlives any
// single Drain call — goroutines it was handed keep writing to it long
// after Drain returned — so every access is under its own mutex.
type runTally struct {
	mu    sync.Mutex
	count int
	err   error
}

// record folds one run's outcome in, exactly as Drain's own recordErr did
// before the outcome moved off Drain's stack and onto this shared tally.
func (t *runTally) record(item queue.WorkItem, out queue.WorkItem, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil && !queue.Claimable(out.State) {
		t.count++
	}
	if !settledFilter(err) && t.err == nil {
		t.err = fmt.Errorf("pump: item %s: %w", item.ID, err)
	}
}

// swap atomically takes ownership of t's accumulated count and error,
// resetting t to zero — "a count read from the runs that finished since the
// last tick", a projection over completions, never a second reading of
// them: whatever it hands back here, it will never hand back again.
func (t *runTally) swap() (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	count, err := t.count, t.err
	t.count, t.err = 0, nil
	return count, err
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
// that lands first is a rebase the waiter pays for.
//
// Issue #207: a land-ready candidate that shares a section with the group's
// starved head no longer yields to it — it goes ahead of the head instead.
// A minute of landing is not a build: the old rule made a candidate one
// land-step from freeing its section wait out a head that could spend
// twenty minutes on a fresh build and review, holding that section the
// whole time — when letting the almost-done candidate through first would
// have cost both sides nothing. This is bounded, so the head still wins
// eventually: each time it is preempted this way — at least one land-ready
// candidate sharing its section went ahead of it — [queue.WorkItem.SectionPreemptions]
// on the head's own item increments, and once that reaches
// [Pump.LandPreemptions] (default [DefaultLandPreemptions]) the head goes
// regardless, exactly as before this issue, until its sections are finally
// granted and the counter resets alongside SectionWaits. The holder
// exemption above is unaffected — a holder is never yielded to anything.
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
//
// Issue #233: a land-ready unit needing two sections could acquire neither.
// It names [loop, daemon], say, so it [sectionsIntersect]s with EVERY
// section head this scheme ever names — a loop-only head or a daemon-only
// head both contend with it — and is told to yield far more often than any
// single-section unit is, while single-section units that do not intersect
// today's head sail past it and keep taking the other section out from
// under it. [loop.Loop.acquireSections] now answers half of this by keeping
// a partial grant across a busy name instead of releasing it (so the unit
// can hold one section while it waits out the other), but a partial holder
// still needs to become a section head to ever get a turn at the second
// one. So a land-ready candidate naming more than one section (or none,
// which already intersects everything and has the same shape, worse) is
// preferred for headship, at the same StarveAfter bound, over an ordinary
// single-section starver: among such candidates this picks the one with the
// most SectionWaits, and only when there is none does headship fall back to
// today's scan. Once named head, the existing intersection-scoped YieldTo
// already covers every section it names at once — that is "the head of all
// its sections at once" — and the existing holder exemption and ordering
// carry it the rest of the way once [loop.Loop.HoldsSection] reports it a
// partial holder. A group with no multi-section land-ready starver behaves
// exactly as it did before this issue.
func (p *Pump) fairSectionOrder(cands []drainCandidate) []drainCandidate {
	bound := p.StarveAfter
	if bound <= 0 {
		bound = DefaultStarveAfter
	}
	landBound := p.LandPreemptions
	if landBound <= 0 {
		landBound = DefaultLandPreemptions
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
	preempting := map[int]bool{}
	for _, k := range groupOrder {
		idxs := groups[k] // priority-then-age — the sort Drain applies before calling this

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

		// Issue #233: a land-ready candidate naming more than one section (or
		// none) is preferred for headship over an ordinary single-section
		// starver, so it becomes the head of every section it names at once
		// instead of losing the intersection race to whichever single-section
		// head gets picked instead. Only when no such candidate has starved
		// does headship fall back to the plain max-SectionWaits scan below,
		// which is exactly what already ran here before this issue.
		head := -1
		for _, i := range idxs {
			it := cands[i].item
			if !landReady(it) || len(it.Sections) == 1 {
				continue
			}
			if it.SectionWaits < bound {
				continue
			}
			if head == -1 || it.SectionWaits > cands[head].item.SectionWaits {
				head = i
			}
		}
		if head == -1 {
			for _, i := range idxs {
				if cands[i].item.SectionWaits < bound {
					continue
				}
				if head == -1 || cands[i].item.SectionWaits > cands[head].item.SectionWaits {
					head = i
				}
			}
		}
		if head == -1 {
			continue // nobody in this group has starved yet
		}
		heads[head] = true
		// Issue #207: a land-ready candidate sharing a section with the head
		// goes ahead of it instead of yielding, unless the head has already
		// been preempted this way landBound times — see the doc comment
		// above. Every other candidate that intersects the head's sections
		// still yields exactly as before.
		exempt := cands[head].item.SectionPreemptions < landBound
		preempted := false
		for _, i := range idxs {
			if i == head || cands[i].holder {
				continue
			}
			if !sectionsIntersect(cands[head].item.Sections, cands[i].item.Sections) {
				continue
			}
			if exempt && landReady(cands[i].item) {
				preempting[i] = true
				preempted = true
				continue
			}
			cands[i].item.YieldTo = cands[head].item.ID
		}
		if preempted {
			cands[head].item.Preempted = true
		}
	}
	ordered := make([]drainCandidate, 0, len(cands))
	for i, c := range cands {
		if c.holder && !heads[i] {
			ordered = append(ordered, c)
		}
	}
	// Issue #207: a land-ready candidate exempted from yielding to a head
	// must actually dispatch before that head — otherwise exempting it from
	// YieldTo would do nothing, since the head still sits earlier in this
	// order and Drain's dispatch loop claims sections in order. It goes
	// ahead of every head, not just the one it preempted: it is, by
	// definition, closer to done than any candidate still contending for a
	// section to even start building.
	for i, c := range cands {
		if preempting[i] && !c.holder {
			ordered = append(ordered, c)
		}
	}
	for i, c := range cands {
		if heads[i] {
			ordered = append(ordered, c)
		}
	}
	for i, c := range cands {
		if !heads[i] && !preempting[i] && !c.holder && landReady(c.item) {
			ordered = append(ordered, c)
		}
	}
	for i, c := range cands {
		if !heads[i] && !preempting[i] && !c.holder && !landReady(c.item) {
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

// Drain claims every claimable item within budget and dispatches each into
// its own goroutine, then returns without waiting for any of them to
// finish — a tick feeds the box; it does not wait for the meal it served
// (issue #196). The int it returns is not this pass's own work: it is a
// count of runs dispatched by an EARLIER Drain that have finished settling
// since, drained from [Pump.runs] as a single projection over completions,
// never a second reading of them. A run this pass dispatches is counted by
// some later Drain instead, once it finishes — its error, if any, surfaces
// then too, through that later call's return.
//
// Blocked, stopped, section-busy and over-budget items simply remain for
// the next tick. A section-busy item never spends a seat to learn it must
// wait (issue #120): its section is taken, and checked, before the meter is
// ever asked. [Pump.Quiesce] is the barrier for anyone who needs every
// dispatched run to have actually finished, not just been started.
func (p *Pump) Drain(ctx context.Context) (int, error) {
	p.ensureWired()
	settled, tallyErr := p.runs.swap()

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
			return settled, errors.Join(tallyErr, err)
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
		//
		// AwaitingReplan is claimable by state (StateWaiting) but is not
		// this loop's business: it is waiting on [plan.Replanner], not a
		// build to dispatch, and [Pump.Replan] is the separate step — like
		// intake — that folds it back in once the planner (or the lack of
		// one) has answered.
		if queue.Claimable(item.State) && !item.AwaitingReplan {
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
	// Priority reorders dispatch only — higher first, ties broken by the
	// existing oldest-first record order (SortStableFunc), so this is
	// "priority, then age", never a second seat or a change to leasing.
	// fairSectionOrder's classes still outrank it: a starved head goes
	// ahead of a newer urgent candidate exactly as #109 requires.
	slices.SortStableFunc(dcs, func(a, b drainCandidate) int {
		return b.item.Priority.OrDefault().Rank() - a.item.Priority.OrDefault().Rank()
	})
	dcs = p.fairSectionOrder(dcs)

	// firstErr accumulates only this pass's own dispatch-time errors — the
	// section wait and claim failures below, both resolved synchronously
	// before any goroutine is ever started. Nothing else writes here, so
	// unlike [Pump.runs] it needs no mutex of its own.
	var firstErr error
	recordErr := func(item queue.WorkItem, err error) {
		if !settledFilter(err) && firstErr == nil {
			firstErr = fmt.Errorf("pump: item %s: %w", item.ID, err)
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
			recordErr(item, err)
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
			return settled, errors.Join(tallyErr, err)
		}

		// Claimed here, synchronously, before the goroutine below is even
		// started — not inside it. The first attempt at this issue claimed
		// only once the goroutine reached RunHolding, so a second candidate
		// contending for the very section just granted above, later in this
		// same loop, could still find this item unclaimed and have
		// [loop.Loop.reapGhost] mistake its live grant for an abandoned one
		// and reap it out from under it. Claiming before moving on to the
		// next candidate closes that window: whatever this loop looks at
		// next sees this item genuinely StateClaimed.
		claim := forge.Lease{}
		if grant != nil {
			var claimed queue.WorkItem
			var cerr error
			claimed, claim, cerr = lp.Claim(ctx, item)
			if cerr != nil {
				grant.Release()
				release()
				recordErr(item, cerr)
				continue
			}
			item = claimed
		}

		// Registered BEFORE the goroutine starts, not inside it: a supersede
		// arriving between this line and the goroutine's first statement must
		// still find an entry to cancel, not a window where the build is
		// dispatched but not yet stoppable.
		bctx, cancel := context.WithCancel(ctx)
		entry := p.builds.register(item.ID, cancel)
		p.inflight.Add(1)
		go func(item queue.WorkItem, claim forge.Lease, grant *loop.SectionGrant, bctx context.Context, cancel context.CancelFunc, entry *buildCancel) {
			defer p.inflight.Done()
			defer release()
			defer func() {
				p.builds.deregister(item.ID, entry)
				cancel()
			}()
			out, err := lp.RunHolding(bctx, item, claim, grant)
			p.runs.record(item, out, err)
		}(item, claim, grant, bctx, cancel, entry)
	}
	return settled, errors.Join(unreadable, firstErr, tallyErr)
}

// Tick is one heartbeat: feed, answer replans, then drain.
//
// Intake's error no longer short-circuits the drain. An intake that could not
// read one chain has still fed every other ingress, and the work already in
// the queue is owed its tick regardless — the alternative is what happened in
// practice, a factory that stopped doing anything because one item's chain
// had a record in it nobody could parse. Replan is the same posture: one
// unreadable replan wait must not stop the drain either.
//
// Replan runs as its own step, between intake and drain, never inside
// Drain's own candidate loop (see [Pump.Replan]'s doc for why): a unit it
// answers this tick is pending again in time for this same tick's Drain to
// claim it, the same ordering intake's newly-submitted items already get.
func (p *Pump) Tick(ctx context.Context) (int, error) {
	p.ensureWired()
	_, intakeErr := p.Intake(ctx)
	_, replanErr := p.Replan(ctx)
	settled, drainErr := p.Drain(ctx)
	return settled, errors.Join(intakeErr, replanErr, drainErr)
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
// The meter is acquired next, from its judgement seat pool
// ([capacity.Meter.AcquireJudgement], issue #202) rather than the pool
// [Pump.Drain] acquires from before a claim runs — a burst of originations
// must not fill every build seat with planning, and a burst of builds must
// not leave originations unplanned, so the two draw from separate ceilings
// over the same window. Never [capacity.Meter.Start], which is a build-only
// charge; billing a planner's start here would reopen issue #45's
// double-count. A pacing refusal (busy, over budget, an unmetered or
// unpriced window, a credential brake) releases the lease and skips
// silently, with no record: the meter's own window is why nothing happened,
// and the next tick's AcquireJudgement is the retry — the same posture Drain
// already takes at claim. This leaves no PRE-dispatch use for the
// retry-in-place scheme issue #110 built for a judgement run inline in the
// tick ([plan.Store.Defer], [plan.DeferredKind]): once dispatch is off the
// tick there is no tick left to retry a refused AcquireJudgement within, and
// a meter refusal before dispatch is already the same silent,
// next-tick-retried pacing every claim gets.
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
	release, err := p.Meter.AcquireJudgement(ctx, "plan-"+ing.ID)
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
	// Not registered in [Pump.builds]: [Pump.CancelBuild] and the hooks that
	// call it (issue #270) are scoped to builds — a judgement decomposition
	// is a planner call, not a build a supersede could ever race, since
	// nothing claims a queue item's lease until the plan it produces is
	// submitted as one.
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

// replanClaimRepo is the pseudo-repository a replan dispatch is leased
// under — a namespace of its own, distinct from [planClaimRepo] and
// [queue.ClaimRepo], so a replan dispatch can never contend with a judgement
// dispatch or a work item's own claim for the same (repo, ref) pair.
const replanClaimRepo = "_replan"

// Replan is intake's counterpart for issue #182's other pacing gate: every
// unit [loop.Loop.RunHolding] left [queue.WorkItem.AwaitingReplan] gets one
// answer attempt per tick, off the drain's own dispatch loop exactly as a
// judgement decomposition is off the intake tick (see [Pump.dispatchJudgement]).
//
// Folding "index/replans" rather than filtering [Pump.Drain]'s own
// "index/pending" fold is deliberate: a unit answering the planner is not a
// build to dispatch, and mixing the two folds would put a step that can
// call a model on the same critical path as #109/#143's fairness ordering —
// exactly the coupling issue #50 already paid to take a judgement
// decomposer off of. One unreadable wait must not stop the rest, the same
// posture [Pump.Intake] and [Pump.Drain] already take.
func (p *Pump) Replan(ctx context.Context) (int, error) {
	dispatched := 0
	var unreadable error
	seen := map[string]bool{}
	for rec, err := range p.Store.Log().Since(ctx, p.Tenant, "index/replans", 1) {
		if err != nil {
			return dispatched, err
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
		if !item.AwaitingReplan {
			continue // already answered or parked by an earlier tick
		}
		if err := p.dispatchReplan(ctx, item); err != nil {
			unreadable = errors.Join(unreadable, err)
			continue
		}
		dispatched++
	}
	return dispatched, unreadable
}

// dispatchReplan answers, or dispatches asking, one unit's replan wait.
//
// With no [Pump.Replanner] configured this settles synchronously: the unit
// parks as an exception with the builder's own escalation reason, exactly
// as an unwidenable escalation always has before replanning existed —
// [plan.Replanner] being unconfigured must never silently strand a unit in
// its wait forever.
//
// With one configured, dispatch mirrors [Pump.dispatchJudgement]: a lease
// over (p.Tenant, replanClaimRepo, item.ID) is the one-replan-per-unit
// mutual exclusion — [forge.ErrHeld] means a replan for this unit is already
// in flight, this runner or another, so dispatch skips silently and the next
// tick tries again, and a lapsed lease is recovered by the same
// taking-the-lease-is-the-proof doctrine [queue.Queue.Recover] uses. The
// meter is acquired next with [capacity.Meter.Acquire], never
// [capacity.Meter.Start] — a planner run is not a build, and billing it a
// start here would reopen issue #45's double-count exactly as
// dispatchJudgement's own doc explains. A pacing refusal releases the lease
// and skips silently; the next tick's Acquire is the retry.
func (p *Pump) dispatchReplan(ctx context.Context, item queue.WorkItem) error {
	esc, err := latestReplanEscalation(ctx, p.Store.Log(), p.Tenant, item.ID)
	if err != nil {
		return err
	}
	if p.Replanner == nil {
		return p.parkReplanWait(ctx, item, esc.Reason)
	}
	leaser := p.Store.Leaser(p.Runner)
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: p.Tenant, Repo: replanClaimRepo, Ref: item.ID, Run: "replan-" + item.ID, TTL: p.Queue.TTL,
	})
	if errors.Is(err, forge.ErrHeld) {
		return nil // a replan for this unit is already in flight; the next tick tries again
	}
	if err != nil {
		return err
	}
	release, err := p.Meter.Acquire(ctx, "replan-"+item.ID)
	if err != nil {
		leaser.Release(ctx, lease)
		if meterPacing(err) {
			return nil // pacing, not failure — the same posture Drain takes at claim
		}
		return err
	}
	p.inflight.Add(1)
	// Not registered in [Pump.builds], for the same reason
	// dispatchJudgement's own dispatch is not: a replan is a planner call
	// under its own replanClaimRepo lease, never a build a supersede on the
	// item's own queue claim could race.
	go func() {
		defer p.inflight.Done()
		defer release()
		defer leaser.Release(ctx, lease)
		p.runReplan(ctx, item, esc)
	}()
	return nil
}

// parkReplanWait reopens a replan wait as an exception, carrying reason —
// the builder's own escalation alone when nothing else went wrong, or that
// plus the planner's own failure when it answered with one. The unit has no
// active claim at this point ([queue.Queue.AwaitReplan] already released
// it), so the lease [queue.Queue.Park] releases is the zero value — a
// no-op release, matching every leaser's own contract for a token that
// names no held lease.
func (p *Pump) parkReplanWait(ctx context.Context, item queue.WorkItem, reason string) error {
	q := &queue.Queue{Store: p.Store, Runner: p.Runner, TTL: p.Queue.TTL}
	item.AwaitingReplan = false
	_, err := q.Park(ctx, item, forge.Lease{}, queue.SeverityMedium, reason)
	return err
}

// runReplan is dispatchReplan's async half: it asks [Pump.Replanner], and
// either amends the unit and returns it to a claimable state, or parks it as
// an exception carrying both the builder's escalation and whatever went
// wrong asking the planner. Nothing here has anywhere synchronous to report
// an error to, the same posture [Pump.dispatchJudgement]'s own goroutine
// takes — a write failure surfaces only in whatever it left on the chain.
func (p *Pump) runReplan(ctx context.Context, item queue.WorkItem, esc plan.ReplanEscalation) {
	fail := func(reason string) { _ = p.parkReplanWait(ctx, item, esc.Reason+"; "+reason) }

	ing, ok, err := p.findIngress(ctx, item.Ingress)
	if err != nil {
		fail(fmt.Sprintf("replan failed: reading origination %s: %s", item.Ingress, err))
		return
	}
	if !ok {
		fail(fmt.Sprintf("replan failed: origination %s no longer exists", item.Ingress))
		return
	}
	pl, ok, err := p.Plans.ByRef(ctx, p.Tenant, item.Plan)
	if err != nil {
		fail(fmt.Sprintf("replan failed: reading plan %s: %s", item.Plan, err))
		return
	}
	if !ok {
		fail(fmt.Sprintf("replan failed: plan %s no longer exists", item.Plan))
		return
	}
	revised, err := p.Replanner.Replan(ctx, ing, pl,
		plan.ReplanUnit{ID: item.ID, Task: item.Task, Sections: item.Sections}, esc)
	if err != nil {
		fail("replan failed: " + err.Error())
		return
	}
	amended, err := amendPlanNode(pl, item.Node, revised)
	if err != nil {
		fail("replan failed: " + err.Error())
		return
	}
	if _, err := p.Plans.Record(ctx, ing, amended); err != nil {
		fail("replan failed: recording the revised plan: " + err.Error())
		return
	}

	before := item.Sections
	item.Sections = revised.Sections
	if revised.Task != "" {
		item.Task = revised.Task
	}
	body, _ := json.Marshal(struct {
		Reason         string   `json:"reason"`
		NeedsSections  []string `json:"needs_sections"`
		Paths          []string `json:"paths"`
		SectionsBefore []string `json:"sections_before"`
		SectionsAfter  []string `json:"sections_after"`
	}{esc.Reason, esc.NeedsSections, esc.Paths, before, item.Sections})
	if _, err := p.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: "plan-amended", Subject: item.ID, Actor: p.Runner, OnBehalfOf: item.Requester,
		Body: body,
	}); err != nil {
		fail("replan failed: recording plan-amended: " + err.Error())
		return
	}

	q := &queue.Queue{Store: p.Store, Runner: p.Runner, TTL: p.Queue.TTL}
	_, _ = q.AnswerReplan(ctx, item, "plan amended: "+esc.Reason)
}

// amendPlanNode returns p with unit's node replaced by what the planner
// revised, re-sealed under a fresh content address: a node's Sections or
// Task changed is a different plan, by the same rule any other change to
// WHERE or WHAT a node does is (see [Plan.Seal]).
func amendPlanNode(p plan.Plan, nodeID string, revised plan.ReplanNode) (plan.Plan, error) {
	idx := slices.IndexFunc(p.Nodes, func(n plan.Node) bool { return n.ID == nodeID })
	if idx == -1 {
		return plan.Plan{}, fmt.Errorf("pump: replan: plan %s names no node %q", p.Ref, nodeID)
	}
	amended := p
	amended.Nodes = slices.Clone(p.Nodes)
	amended.Nodes[idx].Sections = revised.Sections
	if revised.Task != "" {
		amended.Nodes[idx].Task = revised.Task
	}
	amended.Ref = ""
	if err := amended.Seal(); err != nil {
		return plan.Plan{}, err
	}
	return amended, nil
}

// latestReplanEscalation reads the newest "replan-requested" record off a
// unit's own chain — the builder's structured escalation
// [loop.Loop.RunHolding] recorded the moment it put the unit into its
// replan wait. There is exactly one live wait per unit at a time (a unit
// answered or parked leaves [queue.WorkItem.AwaitingReplan] false, which
// keeps it out of [Pump.Replan]'s fold entirely), so "newest" and "the one
// this wait is about" are the same record; reading the newest rather than
// requiring exactly one is what lets a unit replan more than once across
// its life without this needing to change.
func latestReplanEscalation(ctx context.Context, log evidence.Log, t tenant.ID, id string) (plan.ReplanEscalation, error) {
	var esc plan.ReplanEscalation
	found := false
	for rec, err := range log.Since(ctx, t, "workitem/"+id, 1) {
		if err != nil {
			return plan.ReplanEscalation{}, err
		}
		if rec.Kind != "replan-requested" {
			continue
		}
		if err := json.Unmarshal(rec.Body, &esc); err != nil {
			return plan.ReplanEscalation{}, fmt.Errorf("pump: replan: reading %s's escalation: %w", id, err)
		}
		found = true
	}
	if !found {
		return plan.ReplanEscalation{}, fmt.Errorf("pump: replan: %s carries AwaitingReplan but has no replan-requested record", id)
	}
	return esc, nil
}
