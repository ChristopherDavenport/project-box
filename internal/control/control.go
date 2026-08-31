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
	"sort"

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
