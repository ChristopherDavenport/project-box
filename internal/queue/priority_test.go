package queue

import (
	"context"
	"testing"
)

// TestPrioritySurvivesSubmitWaitAndReload. The queue is event-sourced and
// record() marshals the whole item, so a plan-set Priority should persist
// through a transition exactly as Model and Attempts already do — this is
// the witness that Wait's mutate-in-place path does not drop it.
func TestPrioritySurvivesSubmitWaitAndReload(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w, err := qu.Submit(ctx, WorkItem{
		ID: "wi-1", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "wi-1",
		Priority: PriorityUrgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Priority != PriorityUrgent {
		t.Fatalf("priority did not survive Submit: %q", w.Priority)
	}
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w, err = qu.Wait(ctx, w, lease, "waiting on a section")
	if err != nil {
		t.Fatal(err)
	}
	if w.Priority != PriorityUrgent {
		t.Fatalf("priority did not survive Wait: %q", w.Priority)
	}
	got, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != PriorityUrgent {
		t.Fatalf("priority did not survive the reload: %q", got.Priority)
	}
}

// TestAnUnstatedPriorityIsNotTheLowestOne mirrors the severity precedent: a
// plan that never sets Priority reads as [PriorityNormal], never as
// [PriorityLow] — unset must not silently sink to the bottom of the queue.
func TestAnUnstatedPriorityIsNotTheLowestOne(t *testing.T) {
	if got := Priority("").OrDefault(); got != PriorityNormal {
		t.Fatalf("an unset priority resolved to %q, want normal", got)
	}
	if got := Priority("whenever").OrDefault(); got != PriorityNormal {
		t.Fatalf("an unrecognised priority resolved to %q, want normal", got)
	}
	if !(PriorityUrgent.Rank() > PriorityHigh.Rank() &&
		PriorityHigh.Rank() > PriorityNormal.Rank() &&
		PriorityNormal.Rank() > PriorityLow.Rank()) {
		t.Fatal("priority ordering is wrong: low < normal < high < urgent must all hold")
	}
}
