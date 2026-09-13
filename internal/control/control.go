// Package control is the room: the exception queue, the escalation that
// carries a stale one out of band, the review sample, and the sign-off — the
// human surface of a factory whose execution loop has none. Under an
// Availability-scoped report the exception queue's timeliness is a control,
// and under CC4.1 the periodic review of automated changes is what makes
// "fully automated" an easy sell rather than a fight.
//
// Parking is only half of that control. A parked item nobody looks at is
// indistinguishable from work that was never flagged, and announcing it only
// in the room announces it only to people already in the room — so exceptions
// age here, against a stated objective, and past it somebody is told through
// a seam this package knows nothing about.
package control

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// DefaultObjective is how long a parked item may go unattended before the
// exception queue has missed its own stated response time.
//
// Four hours: Gas Town's stale threshold, learned the hard way, and a
// defensible CC7 objective for a factory that runs overnight with nobody
// watching. It is a default and not a law — a Box states its own — but there
// is always one, because an exception queue with no objective has an
// unbounded oldest item and that is a finding waiting to be written.
const DefaultObjective = 4 * time.Hour

// Room is one tenant's control surface.
type Room struct {
	Store  store.Store
	Tenant tenant.ID
	// Actor is the operator's authenticated subject — sign-offs and samples
	// are theirs, not the room's.
	Actor string
	// Objective is the response-time objective the exception queue is
	// measured against, and the interval at which a stale exception is told
	// out of band. Zero means [DefaultObjective].
	//
	// One number rather than two: the age at which a queue is out of
	// compliance is the age at which it should be shouting, and a factory
	// whose alarm threshold and whose objective disagree is measuring one
	// thing and acting on another.
	Objective time.Duration
	// Live is the window into runs still going — see [Room.Sessions]. Nil is
	// valid and means the room can say what a unit IS and not what it is
	// SAYING, which is the state every reader of this package was in before
	// the ACP stream had anywhere to arrive.
	Live *Live
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

func (r *Room) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Room) objective() time.Duration {
	if r.Objective <= 0 {
		return DefaultObjective
	}
	return r.Objective
}

// Exception is a parked item as the room needs it: the item, when it parked,
// how long it has been sitting, and how many times somebody has already been
// told out of band.
//
// The item alone was the whole view, and it could say what was parked and
// never for how long. A queue whose items do not age cannot be late, which
// sounds like a good property and is the absence of one.
type Exception struct {
	queue.WorkItem
	// Parked is when the item LAST entered the exception queue. A retry that
	// parks again restarts the clock, because the wait a person owes this
	// item began then and not at the first attempt. A failed item — settled
	// by [queue.Queue.Fail], which never visits Park — counts from when it
	// became failed instead: the exception queue is where both land, and an
	// item that never parked still needs its own clock.
	Parked time.Time `json:"parked"`
	AgeSec int64     `json:"age_sec"`
	// Escalations counts the out-of-band notices this parking has produced.
	// Bounded — see [Escalator.Max].
	Escalations int `json:"escalations"`
	// Overdue reports the age having passed the room's objective.
	Overdue bool `json:"overdue"`
}

// Exceptions lists items currently parked or failed: everything the factory
// decided a person has to look at. Items whose state moved on since parking
// drop out — the index remembers every parking, the fold shows only what
// still needs eyes.
//
// A failed item has no index/exceptions record at all: [queue.Queue.Fail]
// settles an item straight from claimed, never through Park, so the only
// index that ever heard of it is index/pending. This walks that listing too,
// deduped against the same seen set, so a failed item lists exactly once and
// a retried-then-reopened one is not double-counted between the two walks.
//
// Loudest first, then oldest. Arrival order is the order the factory would
// take work in and exactly the wrong order for a person clearing a queue,
// who needs the critical thing from an hour ago before the routine one from
// yesterday.
func (r *Room) Exceptions(ctx context.Context) ([]Exception, error) {
	now := r.now()
	objective := r.objective()
	told, err := r.escalations(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Exception
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/exceptions", 1) {
		if err != nil {
			return nil, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		if ingress.IsID(rec.Subject) {
			// An origination whose judgement failed before it had any item.
			// Listed like an item — same columns, same verbs — because to
			// the person it needs it IS one: work that stopped, with a
			// reason, waiting on them.
			ex, failed, err := r.failedPlan(ctx, rec.Subject, now, objective, told)
			if err != nil {
				return nil, err
			}
			if failed {
				out = append(out, ex)
			}
			continue
		}
		item, parked, err := r.parked(ctx, rec.Subject)
		if err != nil {
			return nil, err
		}
		if item.State != queue.StateParked {
			continue
		}
		age := now.Sub(parked)
		out = append(out, Exception{
			WorkItem: item, Parked: parked, AgeSec: int64(age.Seconds()),
			Escalations: countAfter(told[rec.Subject], parked),
			Overdue:     age >= objective,
		})
	}
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/pending", 1) {
		if err != nil {
			return nil, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		item, failed, err := r.parked(ctx, rec.Subject)
		if err != nil {
			return nil, err
		}
		if item.State != queue.StateFailed {
			continue
		}
		age := now.Sub(failed)
		// item.Severity is empty here — [queue.Queue.Fail] states none — so
		// it sorts with the existing Rank (below High/Critical, tied with
		// Medium) without this package inventing one. It is not quiet by the
		// same stroke: [Escalator.Sweep] reads severity through
		// [queue.Severity.OrDefault], which resolves empty to
		// [queue.SeverityMedium], and the room's own default threshold is
		// also Medium — so a failed item DOES escalate once it goes stale,
		// same as a park a stage explicitly called Medium. See
		// TestFailedItemEscalatesAtTheDefaultThreshold.
		out = append(out, Exception{
			WorkItem: item, Parked: failed, AgeSec: int64(age.Seconds()),
			Escalations: countAfter(told[rec.Subject], failed),
			Overdue:     age >= objective,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Severity.Rank(), out[j].Severity.Rank()
		if a != b {
			return a > b
		}
		return out[i].Parked.Before(out[j].Parked)
	})
	return out, nil
}

// parked folds one item's chain into the item and the moment it last entered
// the exception queue — parked, or, for an item [queue.Queue.Fail] settled
// without ever parking, failed. One walk for both, for the reason
// [Room.entry] gives: Load would produce the first and throw the second
// away. The caller picks the timestamp that matches the item's current
// state; the other is returned zero.
func (r *Room) parked(ctx context.Context, id string) (queue.WorkItem, time.Time, error) {
	var w queue.WorkItem
	var parkedAt, failedAt time.Time
	found := false
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "workitem/"+id, 1) {
		if err != nil {
			return queue.WorkItem{}, time.Time{}, err
		}
		if !queue.StateRecord(rec.Kind) {
			continue // narrative, not state — see queue.StateRecord
		}
		// Zero first: each body is the whole item, and omitempty means a
		// cleared field is absent rather than empty. See queue.Load.
		w = queue.WorkItem{}
		if err := json.Unmarshal(rec.Body, &w); err != nil {
			return queue.WorkItem{}, time.Time{}, fmt.Errorf("control: item %s record %d (%s): %w", id, rec.Seq, rec.Kind, err)
		}
		found = true
		switch w.State {
		case queue.StateParked:
			parkedAt = rec.At
		case queue.StateFailed:
			failedAt = rec.At
		}
	}
	if !found {
		return queue.WorkItem{}, time.Time{}, fmt.Errorf("%w: %s", queue.ErrNoItem, id)
	}
	if w.State == queue.StateFailed {
		return w, failedAt, nil
	}
	return w, parkedAt, nil
}

// ExceptionHealth is the exception queue measured against its objective —
// the control room's Availability metric, and under an Availability-scoped
// report a control with a number rather than a chart on a wall.
//
// OldestAgeSec is the one that matters. A length says how much there is to
// do; only the oldest item says whether anything is being done, and a queue
// that is drained briskly except for the one item nobody wants looks healthy
// by every other measure.
type ExceptionHealth struct {
	Parked     int                    `json:"parked"`
	BySeverity map[queue.Severity]int `json:"by_severity,omitempty"`
	// Oldest names the item behind OldestAgeSec, so the metric is actionable
	// rather than merely alarming.
	Oldest       string `json:"oldest,omitempty"`
	OldestAgeSec int64  `json:"oldest_age_sec"`
	ObjectiveSec int64  `json:"objective_sec"`
	Overdue      int    `json:"overdue"`
	// Met reports the objective holding: nothing parked longer than it.
	Met bool `json:"met"`
}

// ExceptionHealth measures the queue against the objective in force now.
func (r *Room) ExceptionHealth(ctx context.Context) (ExceptionHealth, error) {
	items, err := r.Exceptions(ctx)
	if err != nil {
		return ExceptionHealth{}, err
	}
	h := ExceptionHealth{
		Parked: len(items), ObjectiveSec: int64(r.objective().Seconds()), Met: true,
	}
	for _, x := range items {
		if h.BySeverity == nil {
			h.BySeverity = map[queue.Severity]int{}
		}
		h.BySeverity[x.Severity.OrDefault()]++
		if x.AgeSec > h.OldestAgeSec || h.Oldest == "" {
			h.Oldest, h.OldestAgeSec = x.ID, x.AgeSec
		}
		if x.Overdue {
			h.Overdue++
			h.Met = false
		}
	}
	return h, nil
}

// QueueEntry is one unit of work as an operator needs to see it: what it is,
// where it came from, how long it has been there, and — the part a counter
// can never carry — why it is where it is.
type QueueEntry struct {
	ID    string      `json:"id"`
	State queue.State `json:"state"`
	Box   string      `json:"box,omitempty"`
	Repo  string      `json:"repo,omitempty"`
	// Ingress, Route and Key are the trail back to the occurrence that asked
	// for this work. Route and Key are empty for ingresses opened before they
	// were recorded.
	Ingress string `json:"ingress,omitempty"`
	Route   string `json:"route,omitempty"`
	Key     string `json:"key,omitempty"`
	// Kind is the declaration this work was opened under, and Plan the
	// content address of the decomposition that called for it — the link the
	// chain gained between the origination and its work.
	//
	// Shown because a fact one reader of the queue has and another does not is
	// worse than one nobody has: an operator looking at six rows from one
	// origination has no other way to ask whether they came from one decision
	// or from six ticks of a decomposer over moving facts. Empty on items
	// submitted before there was a decomposition stage, which is honest — no
	// query reconstructs a plan that was never made.
	Kind string `json:"kind,omitempty"`
	Plan string `json:"plan,omitempty"`
	Node string `json:"node,omitempty"`
	// Summary is the planner's one line for the unit, copied verbatim from
	// the item. Empty on items queued before queue.Item.Summary existed —
	// the web's title() falls back to Task client-side, and nothing here
	// derives a summary from the task server-side.
	Summary string `json:"summary,omitempty"`
	// Task is the instruction's first line, clipped: enough to recognise
	// the unit, never the whole prompt.
	Task string `json:"task"`
	// Note is why the item is in this state — "branch main is leased", a
	// gate's reasons, a park cause. The queue has always written it and
	// nothing has ever shown it.
	Note string `json:"note,omitempty"`
	// Severity is how loudly a parked row is asking; empty on everything
	// else, because only a parking has one.
	Severity queue.Severity `json:"severity,omitempty"`
	// Head is the landed commit; Built one produced and not yet landed.
	Head  string `json:"head,omitempty"`
	Built string `json:"built,omitempty"`
	// Seat and AgentRef are who ran this unit and exactly which agent it was,
	// read off the agent-run record. Empty until an agent starts, and empty
	// forever on a unit built by a shell command — which is a distinction
	// worth showing rather than hiding behind a placeholder.
	Seat     string `json:"seat,omitempty"`
	AgentRef string `json:"agentref,omitempty"`
	// Stage is the furthest point this item has reached, read off the last
	// record on its chain: claimed, building, gating, landing, done. A
	// claimed item was a black box — "state=claimed" and nothing else — and
	// the records that would have said more were never on any feed.
	Stage string `json:"stage,omitempty"`
	// DependsOn is the work graph as declared; BlockedBy is the part of it
	// that has not settled yet. An operator asking why nothing is moving
	// needs the second, and the queue has never shown either.
	DependsOn []string `json:"depends_on,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
	// Ready is the frontier: the drain would take this item, every dependency
	// has settled, and nobody is running it.
	//
	// The state cannot carry it. "Pending" says the same word whether an item
	// is next or blocked behind something that will never settle, so a queue
	// of pending work is indistinguishable from a queue of RUNNABLE work —
	// the one distinction that says whether the factory is working.
	//
	// Deliberately item-local: the stop button and the budget are NOT folded
	// in, because ready work that is not moving is the symptom, and folding
	// the cause in would erase it exactly when it fires. The cause is on
	// /v1/status beside this. Routability is not folded in either, and for a
	// duller reason — whether the pump has an executor for this item's box and
	// repo is the pump's table, which the room does not hold. An item nothing
	// is configured to run still reads ready here.
	Ready bool `json:"ready,omitempty"`
	// Runner holds this item's claim, and LeaseExpiresSec is how long the hold
	// has left — negative once it has lapsed, which [QueueEntry.LeaseLapsed]
	// says outright. Empty on anything not claimed, and empty on a CLAIMED
	// item whose lease nobody holds: a runner died between releasing and
	// recording, and the next drain will recover it.
	Runner          string `json:"runner,omitempty"`
	LeaseExpiresSec int64  `json:"lease_expires_sec,omitempty"`
	LeaseLapsed     bool   `json:"lease_lapsed,omitempty"`
	// Root names the item a cascade started at, empty when this item died on
	// its own account.
	//
	// The note cannot serve. Claim writes the cause as prose and nests its
	// dependency's note inside its own, so the root's reason does survive down
	// the chain — as the tail of a string that grows at every level, which
	// nothing can group by, sort on, or count.
	Root string `json:"root,omitempty"`
	// Opened is the first record in the item's chain, Changed the last.
	Opened     time.Time `json:"opened"`
	Changed    time.Time `json:"changed"`
	AgeSec     int64     `json:"age_sec"`
	InStateSec int64     `json:"in_state_sec"`
}

// Queue lists the work items in any of the given states, oldest first — the
// order the factory would take them. No states means every item the tenant
// has ever submitted.
//
// A count is not a queue. `pending=14` was the entire visibility into a
// morning's work: no way to see what the factory was sitting on, and no way
// to read the notes it had already written about why. Everything here is a
// fold over records that existed the whole time — except the claim holder,
// which is a live lease and the one thing the chain cannot say, because a
// runner does not record its own death.
func (r *Room) Queue(ctx context.Context, states ...queue.State) ([]QueueEntry, error) {
	out, _, err := r.fold(ctx, states...)
	return out, err
}

// Backlog is the work queue counted — and it is the LISTING counted, which is
// the whole of the difference. `pending=14` was a number arrived at by its own
// walk of the index, so the count and the listing were two accounts of one
// queue with nothing holding them to each other: a state either could learn
// and the other not is a count that quietly stops matching the rows under it.
//
// [Room.Queue] is now the single fold, and this is a tally of what it
// returned. That costs a full listing per call where a bare count would walk
// less — stated rather than hidden, and the same trade [ExceptionHealth]
// already makes, because two places counting one thing is two places that can
// disagree.
type Backlog struct {
	// Pending is everything still owed a run, by [queue.Claimable] — the one
	// number an operator watches, and the one that must not drop when work
	// merely changes which non-settled state it is in.
	Pending int `json:"pending"`
	// ByState is every state present with its count. It is what makes Pending
	// a projection rather than an assertion: the roll-up and its parts arrive
	// together, from the same rows, and an operator reading "pending: 3" can
	// see which three states that is.
	ByState map[queue.State]int `json:"by_state,omitempty"`
	// Total is every item the tenant has submitted — what the listing returns
	// unfiltered.
	Total int `json:"total"`
}

// Backlog counts the queue from the listing itself.
func (r *Room) Backlog(ctx context.Context) (Backlog, error) {
	entries, err := r.Queue(ctx)
	if err != nil {
		return Backlog{}, err
	}
	b := Backlog{Total: len(entries)}
	for _, e := range entries {
		if b.ByState == nil {
			b.ByState = map[queue.State]int{}
		}
		b.ByState[e.State]++
		if queue.Claimable(e.State) {
			b.Pending++
		}
	}
	return b, nil
}

// fold is Queue's body, returning the frontier as well so a caller wanting
// more than a row — [Room.Sessions] wants the composed layers — reads it from
// the same single walk rather than opening a second one.
func (r *Room) fold(ctx context.Context, states ...queue.State) ([]QueueEntry, *frontier, error) {
	f := &frontier{
		r: r, now: r.now(), items: map[string]queue.WorkItem{}, errs: map[string]error{},
		q:      &queue.Queue{Store: r.Store, Runner: r.Actor},
		layers: map[string][]Layer{},
	}
	seen := map[string]bool{}
	var out []QueueEntry
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/pending", 1) {
		if err != nil {
			return nil, nil, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		e, err := r.entry(ctx, rec.Subject, f)
		if err != nil {
			return nil, nil, err
		}
		if len(states) > 0 && !slices.Contains(states, e.State) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Opened.Before(out[j].Opened) })
	return out, f, nil
}

// frontier is one fold's shared state: the clock every row is measured
// against, and a memo of the items already read. A row asks after its own
// dependencies and a cascade walk asks after theirs, so without the memo a
// chain of n items re-reads the same chains O(n²) times.
type frontier struct {
	r     *Room
	q     *queue.Queue
	now   time.Time
	items map[string]queue.WorkItem
	errs  map[string]error
	// layers is each item's composition, taken from the agent-run record the
	// walk passes anyway. Out of band rather than on QueueEntry, because a
	// listing of the whole queue does not want every row carrying its agent's
	// layer stack.
	layers map[string][]Layer
}

func (f *frontier) load(ctx context.Context, id string) (queue.WorkItem, error) {
	if w, ok := f.items[id]; ok {
		return w, f.errs[id]
	}
	w, err := f.q.Load(ctx, f.r.Tenant, id)
	f.items[id], f.errs[id] = w, err
	return w, err
}

// dead reports the states a dependency cascades from — see [queue.Queue.Claim].
func dead(s queue.State) bool { return s == queue.StateParked || s == queue.StateFailed }

// cascadeRoot walks a dead item back to the one that died on its own account,
// returning empty when this item is not a cascade at all.
//
// Claim already names the cause when it parks a dependent, in prose that nests
// its dependency's prose — three deep it reads "dependency B parked:
// dependency A parked: the credential was revoked". The fact is in there and
// is not a field, so the graph question an operator actually has ("what is
// this one failure holding up") has no answer. See [QueueEntry.Root].
func (f *frontier) cascadeRoot(ctx context.Context, w queue.WorkItem, seen map[string]bool) string {
	if seen[w.ID] {
		// Only reachable through a dependency cycle, which nothing creates and
		// nothing rejects. Stop rather than recur: a room that hangs on
		// malformed data is worse than one that declines to name a root.
		return ""
	}
	seen[w.ID] = true
	for _, dep := range w.DependsOn {
		d, err := f.load(ctx, dep)
		if err != nil || !dead(d.State) {
			continue
		}
		if root := f.cascadeRoot(ctx, d, seen); root != "" {
			return root
		}
		return d.ID
	}
	return ""
}

// holder reads the live claim over an item: who has it and how long the hold
// has left. The lease is the only part of a row that is not a fold — an item's
// chain records that a runner claimed it and can never record that the runner
// went away, which is exactly the case an operator is looking for.
func (f *frontier) holder(ctx context.Context, e *QueueEntry) error {
	lease, held, err := f.r.Store.Leaser(f.r.Actor).Inspect(ctx, f.r.Tenant,
		queue.ClaimRepo, queue.ClaimRef(e.ID))
	if err != nil {
		return fmt.Errorf("control: item %s claim: %w", e.ID, err)
	}
	if !held {
		return nil
	}
	e.Runner = lease.Runner
	e.LeaseExpiresSec = int64(lease.Expires.Sub(f.now).Seconds())
	e.LeaseLapsed = lease.Expired(f.now)
	return nil
}

// entry folds one item's chain into a listing row. It walks the chain once
// for both the state and the clock: Load would give the first and throw away
// the second.
func (r *Room) entry(ctx context.Context, id string, f *frontier) (QueueEntry, error) {
	now := f.now
	var w queue.WorkItem
	var opened, changed time.Time
	stage := ""
	var ran runRecord
	found, folded := false, false
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "workitem/"+id, 1) {
		if err != nil {
			return QueueEntry{}, err
		}
		if rec.Kind == "agent-run" {
			// Which agent, on whose seat, composed from what. Best effort: a
			// record this reader cannot parse is a thinner row, never a
			// listing that fails. The LAST one wins — a handed-off item has
			// one per session, and the current session is the one running.
			var got runRecord
			if json.Unmarshal(rec.Body, &got) == nil {
				ran = got
			}
		}
		// Only the state transitions carry the item. The narrative records
		// still count for the clock and the stage below — an agent-run is
		// precisely how this row learns it is BUILDING — but their bodies are
		// not items and folding them in was what blanked the running row.
		if queue.StateRecord(rec.Kind) {
			// Zero first: each body is the whole item, and omitempty means a
			// cleared field is absent rather than empty. See queue.Load.
			w = queue.WorkItem{}
			if err := json.Unmarshal(rec.Body, &w); err != nil {
				return QueueEntry{}, fmt.Errorf("control: item %s record %d (%s): %w", id, rec.Seq, rec.Kind, err)
			}
			folded = true
		}
		if !found {
			opened = rec.At
		}
		if s := stageOf(rec.Kind); s != "" {
			stage = s
			changed = rec.At // time in STAGE, not merely time since the last write
		}
		found = true
	}
	if !found {
		return QueueEntry{}, fmt.Errorf("control: no such item %s", id)
	}
	if !folded {
		// Records exist but none of them is a state transition: the chain is
		// narrative-only, which no writer produces and no reader can render.
		return QueueEntry{}, fmt.Errorf("control: item %s has no state record to fold", id)
	}
	e := QueueEntry{
		ID: w.ID, State: w.State, Box: w.Box, Repo: w.Repo, Ingress: w.Ingress,
		Kind: w.Kind, Plan: w.Plan, Node: w.Node, Summary: w.Summary,
		Task: clip(firstLine(w.Task), 200), Note: w.Note, Severity: w.Severity,
		Head: w.Head, Built: w.Built, Seat: ran.Seat, AgentRef: ran.AgentRef,
		Stage: stage, DependsOn: w.DependsOn,
		Opened: opened, Changed: changed,
		AgeSec: int64(now.Sub(opened).Seconds()), InStateSec: int64(now.Sub(changed).Seconds()),
	}
	if f.layers != nil && len(ran.Layers) > 0 {
		f.layers[id] = ran.Layers
	}
	// Which dependencies have not settled — the answer to "why is nothing
	// moving", which the declared list alone does not give.
	for _, dep := range w.DependsOn {
		d, derr := f.load(ctx, dep)
		if derr != nil || d.State != queue.StateDone {
			e.BlockedBy = append(e.BlockedBy, dep)
		}
	}
	switch {
	case w.State == queue.StatePending, w.State == queue.StateHandedOff:
		// The frontier. Waiting is deliberately not here: the drain does take
		// a waiting item, but only to ask a gate whether its answer has
		// arrived, so calling it runnable would report capacity spent on
		// somebody else's decision as work the factory could be doing.
		e.Ready = len(e.BlockedBy) == 0
	case w.State == queue.StateClaimed:
		if err := f.holder(ctx, &e); err != nil {
			return QueueEntry{}, err
		}
	case dead(w.State):
		e.Root = f.cascadeRoot(ctx, w, map[string]bool{})
	}
	if w.Ingress != "" {
		// Best effort: an item whose origination cannot be read is still a
		// row worth showing, just a thinner one.
		//
		// Through the fold, not the head. This was the THIRD reader of an
		// origination's chain to unmarshal whatever record happened to be
		// newest, and the failure was quiet in the way the others were: a plan
		// record and a break-glass record are both valid JSON with no route in
		// them, so the row simply came back with an empty route and an empty
		// key and nothing anywhere said why.
		if ing, ok, err := ingress.Load(ctx, r.Store.Log(), r.Tenant, w.Ingress); err == nil && ok {
			e.Route, e.Key = ing.Route, ing.Key
		}
	}
	return e, nil
}

// stageOf names the point in the run a record marks. Only records that move
// an item forward count: the rest are commentary, and "time in stage" that
// reset on every note would measure nothing.
func stageOf(kind string) string {
	switch kind {
	case "queued":
		return "queued"
	case "claimed":
		return "claimed"
	case "agent-run":
		return "building"
	case "build-resumed", "build-rebased":
		return "resumed"
	case "built", "agent-result":
		return "built"
	case "gate":
		return "gating"
	case "land":
		return "landing"
	case "done":
		return "done"
	case "waiting":
		return "waiting"
	case "handedoff", "handoff-requested":
		return "handedoff"
	case "parked", "escalated":
		return "parked"
	case "failed":
		return "failed"
	case "recovered":
		return "recovered"
	case "retried":
		return "retried"
	case "resolved":
		return "resolved"
	}
	return ""
}

func firstLine(s string) string {
	head, _, _ := strings.Cut(s, "\n")
	return head
}

// clip bounds s to at most n runes, trading the tail for a trailing ellipsis
// when it would otherwise run longer — mirrors web/src/format.js's
// clip(s, n=200), so a paragraph with no newline still makes a short row.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Sample picks n settled items for human review, deterministically from the
// seed: the same seed always names the same set, so a recorded (seed, n)
// makes the selection reproducible by an auditor rather than trusted. The
// selection itself is recorded as evidence before it is returned.
func (r *Room) Sample(ctx context.Context, n int, seed string) ([]queue.WorkItem, error) {
	type ranked struct {
		id   string
		rank uint64
	}
	var all []ranked
	seen := map[string]bool{}
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/landings", 1) {
		if err != nil {
			return nil, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		h := sha256.Sum256([]byte(seed + "\x00" + rec.Subject))
		all = append(all, ranked{id: rec.Subject, rank: binary.BigEndian.Uint64(h[:8])})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].rank < all[j].rank })
	if n > len(all) {
		n = len(all)
	}
	q := &queue.Queue{Store: r.Store, Runner: r.Actor}
	var items []queue.WorkItem
	var ids []string
	for _, s := range all[:n] {
		item, err := q.Load(ctx, r.Tenant, s.id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		ids = append(ids, s.id)
	}
	body, _ := json.Marshal(map[string]any{"seed": seed, "n": n, "items": ids})
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: r.Tenant, Scope: "control/review", Kind: "review-sample",
		Subject: seed, Actor: r.Actor, Body: body,
	}); err != nil {
		return nil, fmt.Errorf("control: sample chosen but not recorded: %w", err)
	}
	return items, nil
}

// SignOff records one reviewed item. Verdict "pass" closes the review;
// anything else is a finding the reviewer wants followed — either way it is
// the CC4.1 record.
func (r *Room) SignOff(ctx context.Context, itemID, verdict, note string) error {
	if verdict == "" {
		return fmt.Errorf("control: a sign-off needs a verdict")
	}
	body, _ := json.Marshal(map[string]string{"verdict": verdict, "note": note})
	_, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: r.Tenant, Scope: "control/review", Kind: "review-signoff",
		Subject: itemID, Actor: r.Actor, Body: body,
	})
	return err
}

// failedPlan renders an origination whose planning failed as an exception,
// or reports that it no longer is one: retried, resolved, or since planned.
func (r *Room) failedPlan(ctx context.Context, id string, now time.Time, objective time.Duration, told map[string][]time.Time) (Exception, bool, error) {
	plans := &plan.Store{Log: r.Store.Log(), Actor: r.Actor}
	st, err := plans.Standing(ctx, r.Tenant, id)
	if err != nil {
		return Exception{}, false, err
	}
	if st.State != "failed" {
		return Exception{}, false, nil
	}
	ing, ok, err := plans.Origination(ctx, r.Tenant, id)
	if err != nil || !ok {
		return Exception{}, false, err
	}
	age := now.Sub(st.At)
	return Exception{
		WorkItem: queue.WorkItem{
			ID: id, Tenant: r.Tenant, Box: ing.Box, Ingress: id, Kind: string(ing.Kind),
			Requester: ing.Requester, Repo: ing.Repo, Task: plan.TaskOf(ing),
			State: queue.StateParked, Note: "planning failed: " + st.Reason, Severity: queue.SeverityMedium,
		},
		Parked: st.At, AgeSec: int64(age.Seconds()),
		Escalations: countAfter(told[id], st.At),
		Overdue:     age >= objective,
	}, true, nil
}
