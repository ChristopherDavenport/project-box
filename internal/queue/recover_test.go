package queue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// shortLease is a queue whose claims lapse fast enough to watch. The property
// is about a lease that has expired, not about waiting thirty minutes for one.
func shortLease(t *testing.T, ttl time.Duration) *Queue {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Queue{Store: s, Runner: "runner-1", TTL: ttl}
}

// TestRecoverReturnsWorkAbandonedByADeadRunner. A claimed item was a dead end:
// StateClaimed was written once and read nowhere, and the drain selects only
// pending and waiting. So a runner lost mid-run — crash, SIGKILL, or a drain
// window that elapsed with work in flight — took its item with it. The lease
// lapsed, nothing noticed, and the control room kept reporting it as building.
func TestRecoverReturnsWorkAbandonedByADeadRunner(t *testing.T) {
	qu, ctx := shortLease(t, 40*time.Millisecond), context.Background()
	w := submit(t, qu, "wi-1")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	// The runner dies here: no Release, no Park, no settle of any kind.

	if _, ok, err := qu.Recover(ctx, w); err != nil || ok {
		t.Fatalf("recovered a live claim: ok=%v err=%v", ok, err)
	}

	time.Sleep(80 * time.Millisecond) // the lease lapses

	got, ok, err := qu.Recover(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("an abandoned claim was not recovered; the item is stranded forever")
	}
	if got.State != StatePending {
		t.Fatalf("state = %s, want pending", got.State)
	}
	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State != StatePending {
		t.Fatalf("recovery did not survive the fold: %+v", reloaded)
	}
	// And it is genuinely claimable again, which is the whole point.
	if _, _, err := qu.Claim(ctx, reloaded); err != nil {
		t.Fatalf("a recovered item could not be claimed: %v", err)
	}
}

// TestRecoverLeavesLiveWorkAlone is the half that must never fire. Acquire
// refusing a held ref is the liveness probe, so a runner still working is
// invisible to recovery — otherwise the sweep would be a spawn storm dressed
// as maintenance.
func TestRecoverLeavesLiveWorkAlone(t *testing.T) {
	qu, ctx := shortLease(t, time.Hour), context.Background()
	w := submit(t, qu, "wi-1")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, ok, err := qu.Recover(ctx, w); ok || err != nil {
			t.Fatalf("recovery touched a live claim: ok=%v err=%v", ok, err)
		}
	}
	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State != StateClaimed {
		t.Fatalf("state = %s, want it left claimed", reloaded.State)
	}
}

// TestRecoverIgnoresEveryOtherState. Recovery is about a lease nobody holds,
// not about any item that happens to be sitting still: a parked item has a
// reason and an operator, and sweeping it back into the queue would undo the
// exception queue on a timer.
func TestRecoverIgnoresEveryOtherState(t *testing.T) {
	qu, ctx := shortLease(t, 40*time.Millisecond), context.Background()
	w := park(t, qu, "wi-1", "escalated by mason")
	time.Sleep(80 * time.Millisecond)
	if _, ok, err := qu.Recover(ctx, w); ok || err != nil {
		t.Fatalf("recovery took a parked item back into the queue: ok=%v err=%v", ok, err)
	}
}
