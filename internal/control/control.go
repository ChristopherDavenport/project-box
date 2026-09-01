// Package control is the room: the exception queue, the review sample, and
// the sign-off — the human surface of a factory whose execution loop has
// none. Under an Availability-scoped report the exception queue's timeliness
// is a control, and under CC4.1 the periodic review of automated changes is
// what makes "fully automated" an easy sell rather than a fight.
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

// Room is one tenant's control surface.
type Room struct {
	Store  store.Store
	Tenant tenant.ID
	// Actor is the operator's authenticated subject — sign-offs and samples
	// are theirs, not the room's.
	Actor string
}

// Exceptions lists items currently parked: everything the factory decided a
// person has to look at. Items whose state moved on since parking drop out —
// the index remembers every parking, the fold shows only what still needs
// eyes.
func (r *Room) Exceptions(ctx context.Context) ([]queue.WorkItem, error) {
	q := &queue.Queue{Store: r.Store, Runner: r.Actor}
	seen := map[string]bool{}
	var out []queue.WorkItem
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/exceptions", 1) {
		if err != nil {
			return nil, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		item, err := q.Load(ctx, r.Tenant, rec.Subject)
		if err != nil {
			return nil, err
		}
		if item.State == queue.StateParked {
			out = append(out, item)
		}
	}
	return out, nil
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
	// Head is the landed commit; Built one produced and not yet landed.
	Head  string `json:"head,omitempty"`
	Built string `json:"built,omitempty"`
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
	now := time.Now().UTC()
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
	found := false
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "workitem/"+id, 1) {
		if err != nil {
			return QueueEntry{}, err
		}
		if err := json.Unmarshal(rec.Body, &w); err != nil {
			return QueueEntry{}, fmt.Errorf("control: item %s record %d: %w", id, rec.Seq, err)
		}
		if !found {
			opened = rec.At
		}
		changed = rec.At
		found = true
	}
	if !found {
		return QueueEntry{}, fmt.Errorf("control: no such item %s", id)
	}
	e := QueueEntry{
		ID: w.ID, State: w.State, Box: w.Box, Repo: w.Repo, Ingress: w.Ingress,
		Task: firstLine(w.Task), Note: w.Note, Head: w.Head, Built: w.Built,
		Opened: opened, Changed: changed,
		AgeSec: int64(now.Sub(opened).Seconds()), InStateSec: int64(now.Sub(changed).Seconds()),
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
