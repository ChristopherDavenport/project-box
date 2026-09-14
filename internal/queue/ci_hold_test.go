package queue

import (
	"context"
	"testing"
)

// parkKeepingAfterCIWaits is parkKeeping with a kept candidate that already
// carries a nonzero CIWaits and CIRerunsSpent — the shape [loop.Loop] leaves
// behind when a stuck or red independent CI finally parks the item (issue
// #273).
func parkKeepingAfterCIWaits(t *testing.T, qu *Queue, id, reason string, ciWaits, ciRerunsSpent int) WorkItem {
	t.Helper()
	w := submit(t, qu, id)
	w, lease, err := qu.Claim(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepCandidate("sha-built", "sha-base", []string{"build-and-test"})
	w.CIWaits = ciWaits
	w.CIRerunsSpent = ciRerunsSpent
	w, err = qu.ParkKeeping(context.Background(), w, lease, SeverityMedium, reason)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestRetryWithRebuildResetsTheCICounters. A fresh candidate is a fresh CI
// conversation (issue #273): a rebuild retry takes the dropResume path
// exactly as it does for RebaseCount, so CIWaits and CIRerunsSpent must not
// carry into the next candidate the way they would if only the resume point
// were dropped and the counters left standing.
func TestRetryWithRebuildResetsTheCICounters(t *testing.T) {
	qu, ctx := q(t), context.Background()
	parkKeepingAfterCIWaits(t, qu, "wi-1", "gate refused", 40, 1)

	parked, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if parked.CIWaits != 40 || parked.CIRerunsSpent != 1 {
		t.Fatalf("setup: the parked counters must persist through the fold before the retry resets them: CIWaits=%d CIRerunsSpent=%d", parked.CIWaits, parked.CIRerunsSpent)
	}

	got, err := qu.Retry(ctx, tn, "wi-1", Identity{Name: "chris"}, "rebuild from scratch")
	if err != nil {
		t.Fatal(err)
	}
	if got.CIWaits != 0 {
		t.Fatalf("CIWaits = %d, want reset to 0", got.CIWaits)
	}
	if got.CIRerunsSpent != 0 {
		t.Fatalf("CIRerunsSpent = %d, want reset to 0", got.CIRerunsSpent)
	}

	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CIWaits != 0 || reloaded.CIRerunsSpent != 0 {
		t.Fatalf("the reset did not survive the fold: %+v", reloaded)
	}
}

// TestFailDropsTheCICounters. [Queue.Fail] drops the resume point via the
// same dropResume every other settle uses — a failed item's next attempt
// starts a fresh CI conversation exactly as a rebuild retry does.
func TestFailDropsTheCICounters(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepCandidate("sha-built", "sha-base", nil)
	w.CIWaits = 12
	w.CIRerunsSpent = 1

	w, err = qu.Fail(ctx, w, lease, CauseChange, "the change itself is wrong")
	if err != nil {
		t.Fatal(err)
	}
	if w.CIWaits != 0 || w.CIRerunsSpent != 0 {
		t.Fatalf("Fail should drop the CI counters along with the rest of the resume point: %+v", w)
	}
}
