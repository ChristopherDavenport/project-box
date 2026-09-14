package queue

import (
	"context"
	"testing"
	"time"
)

// TestRecoverInterruptsAStalledGhost is issue #270's queue-side half: Recover
// proves the prior holder no longer holds the claim lease, but a runner
// stalled past its TTL (#211) may still be alive and writing. Interrupt is
// the hook that stops it, fired with the item's own id.
func TestRecoverInterruptsAStalledGhost(t *testing.T) {
	qu, ctx := shortLease(t, 40*time.Millisecond), context.Background()
	w := submit(t, qu, "wi-1")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}

	var interrupted string
	qu.Interrupt = func(id string) { interrupted = id }

	time.Sleep(80 * time.Millisecond) // the lease lapses
	if _, ok, err := qu.Recover(ctx, w); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("expected the stalled claim to be recovered")
	}
	if interrupted != "wi-1" {
		t.Fatalf("Interrupt fired for %q, want %q", interrupted, "wi-1")
	}
}

// TestRecoverDoesNotInterruptALiveClaim: Acquire refusing a held ref means
// nobody has been proven gone, so nothing should be told to stop.
func TestRecoverDoesNotInterruptALiveClaim(t *testing.T) {
	qu, ctx := shortLease(t, time.Hour), context.Background()
	w := submit(t, qu, "wi-1")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}

	fired := false
	qu.Interrupt = func(string) { fired = true }

	if _, ok, err := qu.Recover(ctx, w); ok || err != nil {
		t.Fatalf("recovery touched a live claim: ok=%v err=%v", ok, err)
	}
	if fired {
		t.Fatal("Interrupt fired for a claim nobody proved abandoned")
	}
}

// TestResolveInterruptsAGhostBuild covers the terminal direction of reopen:
// an operator closing an exception says nothing more is owed, and any build
// still running under the item's old claim must stop rather than keep
// spending toward an outcome nobody will read.
func TestResolveInterruptsAGhostBuild(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "escalated by mason")

	var interrupted string
	qu.Interrupt = func(id string) { interrupted = id }

	if _, err := qu.Resolve(ctx, tn, "wi-1", Identity{Name: "chris"}, "agreed — closing it"); err != nil {
		t.Fatal(err)
	}
	if interrupted != "wi-1" {
		t.Fatalf("Interrupt fired for %q on Resolve, want %q", interrupted, "wi-1")
	}
}

// TestRetryInterruptsAGhostBuild covers the other reopen direction: a retry
// is about to let a successor dispatch on the same item, so a ghost still
// holding the old lease must be canceled first, not merely outrun.
func TestRetryInterruptsAGhostBuild(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "gate timed out")

	var interrupted string
	qu.Interrupt = func(id string) { interrupted = id }

	if _, err := qu.Retry(ctx, tn, "wi-1", Identity{Name: "chris"}, "try again"); err != nil {
		t.Fatal(err)
	}
	if interrupted != "wi-1" {
		t.Fatalf("Interrupt fired for %q on Retry, want %q", interrupted, "wi-1")
	}
}

// TestNilInterruptIsSafe: every existing construction of Queue leaves
// Interrupt unset, so Recover and reopen must both tolerate a nil hook.
func TestNilInterruptIsSafe(t *testing.T) {
	qu, ctx := shortLease(t, 40*time.Millisecond), context.Background()
	w := submit(t, qu, "wi-1")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok, err := qu.Recover(ctx, w); err != nil || !ok {
		t.Fatalf("recover with a nil Interrupt: ok=%v err=%v", ok, err)
	}

	park(t, qu, "wi-2", "gate timed out")
	if _, err := qu.Retry(ctx, tn, "wi-2", Identity{Name: "chris"}, "try again"); err != nil {
		t.Fatalf("retry with a nil Interrupt: %v", err)
	}
}
