package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// chainLen counts every record on an item's own chain — the fence's whole
// point is that a superseded settle writes NONE of them.
func chainLen(t *testing.T, qu *Queue, ctx context.Context, id string) int {
	t.Helper()
	n := 0
	for _, err := range qu.Store.Log().Since(ctx, tn, scope(id), 1) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	return n
}

// supersede simulates the incident's own shape (issue #266): a claim's TTL
// lapses while its holder is still building, and a successor — exactly what
// [Queue.Recover]'s re-Claim does — takes the ref out from under it. Acquire
// alone proves the point: a live claim would return [forge.ErrHeld] here,
// so a second Acquire succeeding IS the supersession.
func supersede(t *testing.T, qu *Queue, id string) {
	t.Helper()
	if _, err := qu.Store.Leaser(qu.Runner).Acquire(context.Background(), forge.LeaseRequest{
		Tenant: tn, Repo: ClaimRepo, Ref: ClaimRef(id), Run: id, TTL: time.Hour,
	}); err != nil {
		t.Fatalf("superseding claim: %v", err)
	}
}

// TestSettleWithASupersededLeaseIsRefused is issue #266's fence, at the
// point every settle transition shares: a stale holder's lease no longer
// names the outstanding grant, so its outcome must not be written — the
// bug that let a resolved item's abandoned builder overwrite it back to
// "waiting" hours later.
func TestSettleWithASupersededLeaseIsRefused(t *testing.T) {
	qu, ctx := shortLease(t, 40*time.Millisecond), context.Background()

	cases := []struct {
		name   string
		settle func(w WorkItem, lease forge.Lease) error
	}{
		{"Wait", func(w WorkItem, lease forge.Lease) error {
			_, err := qu.Wait(ctx, w, lease, "stale")
			return err
		}},
		{"Park", func(w WorkItem, lease forge.Lease) error {
			_, err := qu.Park(ctx, w, lease, SeverityMedium, "stale")
			return err
		}},
		{"ParkKeeping", func(w WorkItem, lease forge.Lease) error {
			_, err := qu.ParkKeeping(ctx, w, lease, SeverityMedium, "stale")
			return err
		}},
		{"Fail", func(w WorkItem, lease forge.Lease) error {
			_, err := qu.Fail(ctx, w, lease, CauseChange, "stale")
			return err
		}},
		{"Done", func(w WorkItem, lease forge.Lease) error {
			_, err := qu.Done(ctx, w, lease, forge.Commit("sha-stale"))
			return err
		}},
		{"HandOff", func(w WorkItem, lease forge.Lease) error {
			_, err := qu.HandOff(ctx, w, lease, "stale")
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := "wi-" + c.name
			w := submit(t, qu, id)
			w, lease, err := qu.Claim(ctx, w)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(80 * time.Millisecond) // the claim lapses
			supersede(t, qu, id)

			before := chainLen(t, qu, ctx, id)
			if err := c.settle(w, lease); !errors.Is(err, ErrSuperseded) {
				t.Fatalf("settle with a superseded lease = %v, want ErrSuperseded", err)
			}
			if after := chainLen(t, qu, ctx, id); after != before {
				t.Fatalf("a superseded settle wrote %d record(s); a stale holder's outcome is not evidence about the item", after-before)
			}

			// The successor's claim is untouched: still held, at the token
			// this fence checked against.
			reloaded, err := qu.Load(ctx, tn, id)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.State != StateClaimed {
				t.Fatalf("state = %s, want the successor's claim left alone at claimed", reloaded.State)
			}
		})
	}
}

// TestSettleWithAZeroLeaseIsNeverFenced covers the deliberate exception: a
// caller that never held a real claim — Token 0, which sqlite's Acquire
// never grants — must not be refused by a fence meant for stale holders.
func TestSettleWithAZeroLeaseIsNeverFenced(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Wait(ctx, w, forge.Lease{}, "no real claim held"); err != nil {
		t.Fatalf("a zero-value lease was fenced: %v", err)
	}
}
