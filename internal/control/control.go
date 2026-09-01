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
	// item began then and not at the first attempt.
	Parked time.Time `json:"parked"`
	AgeSec int64     `json:"age_sec"`
	// Escalations counts the out-of-band notices this parking has produced.
	// Bounded — see [Escalator.Max].
	Escalations int `json:"escalations"`
	// Overdue reports the age having passed the room's objective.
	Overdue bool `json:"overdue"`
}

// Exceptions lists items currently parked: everything the factory decided a
// person has to look at. Items whose state moved on since parking drop out —
// the index remembers every parking, the fold shows only what still needs
// eyes.
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
// the exception queue. One walk for both, for the reason [Room.entry] gives:
// Load would produce the first and throw the second away.
func (r *Room) parked(ctx context.Context, id string) (queue.WorkItem, time.Time, error) {
	var w queue.WorkItem
	var at time.Time
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
		if w.State == queue.StateParked {
			at = rec.At
		}
	}
	if !found {
		return queue.WorkItem{}, time.Time{}, fmt.Errorf("%w: %s", queue.ErrNoItem, id)
	}
	return w, at, nil
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
	// Task is the instruction's first line: enough to recognise the unit,
	// never the whole prompt.
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
// fold over records that existed the whole time.
func (r *Room) Queue(ctx context.Context, states ...queue.State) ([]QueueEntry, error) {
	now := r.now()
	seen := map[string]bool{}
	var out []QueueEntry
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/pending", 1) {
		if err != nil {
			return nil, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		e, err := r.entry(ctx, rec.Subject, now)
		if err != nil {
			return nil, err
		}
		if len(states) > 0 && !slices.Contains(states, e.State) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Opened.Before(out[j].Opened) })
	return out, nil
}

// entry folds one item's chain into a listing row. It walks the chain once
// for both the state and the clock: Load would give the first and throw away
// the second.
func (r *Room) entry(ctx context.Context, id string, now time.Time) (QueueEntry, error) {
	var w queue.WorkItem
	var opened, changed time.Time
	stage := ""
	found, folded := false, false
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "workitem/"+id, 1) {
		if err != nil {
			return QueueEntry{}, err
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
		Task: firstLine(w.Task), Note: w.Note, Severity: w.Severity,
		Head: w.Head, Built: w.Built,
		Stage: stage, DependsOn: w.DependsOn,
		Opened: opened, Changed: changed,
		AgeSec: int64(now.Sub(opened).Seconds()), InStateSec: int64(now.Sub(changed).Seconds()),
	}
	// Which dependencies have not settled — the answer to "why is nothing
	// moving", which the declared list alone does not give.
	for _, dep := range w.DependsOn {
		d, derr := (&queue.Queue{Store: r.Store, Runner: r.Actor}).Load(ctx, r.Tenant, dep)
		if derr != nil || d.State != queue.StateDone {
			e.BlockedBy = append(e.BlockedBy, dep)
		}
	}
	if w.Ingress != "" {
		// Best effort: an item whose origination cannot be read is still a
		// row worth showing, just a thinner one.
		if head, err := r.Store.Log().Head(ctx, r.Tenant, "ingress/"+w.Ingress); err == nil && head.Seq > 0 {
			var ing struct {
				Route string `json:"route"`
				Key   string `json:"key"`
			}
			if json.Unmarshal(head.Body, &ing) == nil {
				e.Route, e.Key = ing.Route, ing.Key
			}
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
