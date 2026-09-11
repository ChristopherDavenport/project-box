package queue

import (
	"context"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// TestRetainPersistsPriorAndAttemptWithoutSettling is issue #81's mid-claim
// write: a claim that is about to rebuild in place must leave what it decided
// where acp.Builder actually reads it — the queue — before it runs the
// builder again, and it must still be running afterward: neither settled nor
// waiting, because the claim is not done.
func TestRetainPersistsPriorAndAttemptWithoutSettling(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, claim, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}

	prior := &agent.Prior{Run: w.ID, Repo: "app", Sha: "deadbeef", Reason: "attempt 2 of 3; attempt 1 was refused: build-and-test: fail (red)"}
	retained, renewed, err := qu.Retain(ctx, w, claim, prior, 2)
	if err != nil {
		t.Fatal(err)
	}
	if retained.State != StateClaimed {
		t.Fatalf("state = %s, want claimed — retaining must not settle the item", retained.State)
	}
	if retained.Prior == nil || retained.Prior.Reason != prior.Reason {
		t.Fatalf("prior not carried on the returned item: %+v", retained.Prior)
	}
	if retained.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", retained.Attempt)
	}
	if renewed.Token != claim.Token {
		t.Fatalf("renewal changed the fencing token: %v -> %v", claim.Token, renewed.Token)
	}

	// The successor's own load — exactly what acp.Builder.item does — must
	// see it too, or persisting it in memory only would have bought nothing.
	loaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateClaimed {
		t.Fatalf("fold: state = %s, want claimed", loaded.State)
	}
	if loaded.Prior == nil || loaded.Prior.Sha != "deadbeef" {
		t.Fatalf("fold did not carry prior: %+v", loaded.Prior)
	}
	if loaded.Attempt != 2 {
		t.Fatalf("fold: attempt = %d, want 2", loaded.Attempt)
	}

	// The claim is still ours: a second claim attempt must still see it held.
	if _, _, err := qu.Claim(ctx, loaded); err == nil {
		t.Fatal("retain released the claim; a second claim over it should have been refused")
	}
}

// TestRetainIsLeaseChecked: a runner that has already lost its claim — TTL
// lapsed, another runner recovered it — must not be able to leave its own
// record on an item it no longer holds. Retain renews before writing, so a
// stale lease is refused exactly as a renew of it would be.
func TestRetainIsLeaseChecked(t *testing.T) {
	qu := shortLease(t, 30*time.Millisecond)
	ctx := context.Background()
	w := submit(t, qu, "wi-1")
	w, claim, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond) // the claim lapses

	if _, _, err := qu.Retain(ctx, w, claim, &agent.Prior{Reason: "late"}, 2); err == nil {
		t.Fatal("retain over a lapsed lease should have been refused")
	}

	// Nothing was written: the item still folds to its claimed state with no
	// prior attached.
	loaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Prior != nil {
		t.Fatalf("a refused retain must not have written anything: %+v", loaded.Prior)
	}
}
