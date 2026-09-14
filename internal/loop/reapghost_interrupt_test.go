package loop

import (
	"context"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestReapGhostInterruptsANonSelfLeakGhost is issue #270's loop-side half:
// a holder [reapGhost] proves is not running may still be a live builder
// stalled past its TTL (#211), so it must be canceled before the lease is
// released and a successor re-acquires it — two workers must never edit one
// tree.
func TestReapGhostInterruptsANonSelfLeakGhost(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()

	ghost := e.submit(t, "ghost", nil) // left pending: never claimed, so its lease is a ghost
	ref := "probe/" + ghost.ID
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: ghost.ID, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	var interrupted string
	e.loop.Interrupt = func(id string) { interrupted = id }

	claimant := e.submit(t, "claimant", nil)
	req := forge.LeaseRequest{Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: claimant.ID, TTL: time.Hour}
	_, holder, note, ok := e.loop.reapGhost(ctx, claimant, req)
	if !ok {
		t.Fatalf("expected the pending ghost to be reaped: holder=%s note=%s", holder, note)
	}
	if interrupted != ghost.ID {
		t.Fatalf("Interrupt fired for %q, want the ghost's own id %q", interrupted, ghost.ID)
	}
}

// TestReapGhostNeverInterruptsASelfLeak is the branch reapGhost's own doc
// forbids touching: cur.Run == req.Run means the "ghost" is the caller's own
// leaked earlier attempt. Canceling it would cancel the very run asking to
// reap its own lease.
func TestReapGhostNeverInterruptsASelfLeak(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()

	item := e.submit(t, "self-leak", nil)
	ref := "probe/" + item.ID
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: item.ID, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	fired := false
	e.loop.Interrupt = func(string) { fired = true }

	req := forge.LeaseRequest{Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: item.ID, TTL: time.Hour}
	_, _, _, ok := e.loop.reapGhost(ctx, item, req)
	if !ok {
		t.Fatal("expected the self-leaked lease to be reaped")
	}
	if fired {
		t.Fatal("Interrupt must never fire on the self-leak branch")
	}
}

// TestReapGhostNeverInterruptsAProtectedWaitingReservation is issue #233's
// exemption: a Waiting land-ready holder still naming the contested ref is
// [Loop.acquireSections]'s own kept reservation, not a ghost. Nothing is
// reaped here, so nothing should be told to stop either.
func TestReapGhostNeverInterruptsAProtectedWaitingReservation(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}

	owner := e.submit(t, "reservation-owner", nil)
	ref := "probe/" + owner.ID
	claimed, lease, err := q.Claim(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	claimed.KeepCandidate("deadbeefdeadbeef", "base", nil)
	owner, err = q.Wait(ctx, claimed, lease, "waiting on something else")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: owner.ID, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	fired := false
	e.loop.Interrupt = func(string) { fired = true }

	claimant := e.submit(t, "waits-behind-the-reservation", nil)
	req := forge.LeaseRequest{Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: claimant.ID, TTL: time.Hour}
	_, holder, note, ok := e.loop.reapGhost(ctx, claimant, req)
	if ok {
		t.Fatalf("expected the #233 land-ready reservation to be protected, not reaped: holder=%s note=%s", holder, note)
	}
	if fired {
		t.Fatal("Interrupt must not fire when nothing was reaped")
	}
}

// TestReapGhostToleratesANilInterrupt: every existing construction of Loop
// leaves Interrupt unset, so a genuine reap must not panic without one.
func TestReapGhostToleratesANilInterrupt(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()

	ghost := e.submit(t, "nil-interrupt-ghost", nil)
	ref := "probe/" + ghost.ID
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: ghost.ID, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	claimant := e.submit(t, "nil-interrupt-claimant", nil)
	req := forge.LeaseRequest{Tenant: tn, Repo: e.repo.Name, Ref: ref, Run: claimant.ID, TTL: time.Hour}
	if _, _, _, ok := e.loop.reapGhost(ctx, claimant, req); !ok {
		t.Fatal("expected the pending ghost to be reaped even with a nil Interrupt")
	}
}
