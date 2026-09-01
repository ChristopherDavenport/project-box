package queue

import (
	"context"
	"testing"
)

// TestParkCarriesTheStagesSeverity. The stage that gives up is the only thing
// that knows whether a failing unit test and a revoked credential are the
// same emergency, and before this the queue threw that away — every parking
// arrived at the control room indistinguishable from every other, which is
// why nothing could route any of them.
func TestParkCarriesTheStagesSeverity(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityCritical, "repair could not be opened"); err != nil {
		t.Fatal(err)
	}
	got, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != SeverityCritical {
		t.Fatalf("severity did not survive the fold: %q", got.Severity)
	}
}

// TestAnUnstatedSeverityIsNotTheQuietOne. The default has to point the loud
// way: a stage that said nothing has not thereby said "routine", and an
// unset value resolving to low is how an exception queue goes silent one
// forgotten argument at a time.
func TestAnUnstatedSeverityIsNotTheQuietOne(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w, err = qu.Park(ctx, w, lease, "", "something went wrong and nobody said how badly")
	if err != nil {
		t.Fatal(err)
	}
	if w.Severity != SeverityMedium {
		t.Fatalf("unstated severity resolved to %q, want medium", w.Severity)
	}
	if !w.Severity.AtLeast(SeverityMedium) || SeverityLow.AtLeast(SeverityMedium) {
		t.Fatalf("severity ordering is wrong: low must be quieter than the default")
	}
	if got := Severity("catastrophic").OrDefault(); got != SeverityMedium {
		t.Fatalf("an unrecognised severity resolved to %q, want medium", got)
	}
}

// TestCascadeParkInheritsTheCausesSeverity. A leaf must no more read as
// routine than it may park mysteriously: the thing that killed it was
// critical, and a queue that shows the root shouting and the leaves
// shrugging is describing one incident as several unrelated ones.
func TestCascadeParkInheritsTheCausesSeverity(t *testing.T) {
	qu, ctx := q(t), context.Background()
	root := submit(t, qu, "root")
	leaf := submit(t, qu, "leaf", "root")

	root, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, root, lease, SeverityCritical, "the forge is unreachable"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, leaf); err == nil {
		t.Fatal("a leaf of a parked root was claimable")
	}
	got, err := qu.Load(ctx, tn, "leaf")
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != SeverityCritical {
		t.Fatalf("leaf severity = %q, want the root's critical", got.Severity)
	}
}

// TestReopeningDropsTheSeverity. Off the exception queue is off it. A
// retried item that came back already shouting would arrive at its next
// parking with the previous parking's volume, and a resolved one would sit
// in the record claiming an urgency somebody has just stood down.
func TestReopeningDropsTheSeverity(t *testing.T) {
	for _, tc := range []struct {
		verb   string
		reopen func(*Queue) (WorkItem, error)
	}{
		{"retry", func(qu *Queue) (WorkItem, error) {
			return qu.Retry(context.Background(), tn, "wi-1", "chris", "try again")
		}},
		{"resolve", func(qu *Queue) (WorkItem, error) {
			return qu.Resolve(context.Background(), tn, "wi-1", "chris", "handled by hand")
		}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			qu, ctx := q(t), context.Background()
			w := submit(t, qu, "wi-1")
			w, lease, err := qu.Claim(ctx, w)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := qu.Park(ctx, w, lease, SeverityHigh, "gate could not conclude"); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.reopen(qu); err != nil {
				t.Fatal(err)
			}
			got, err := qu.Load(ctx, tn, "wi-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Severity != "" {
				t.Fatalf("a reopened item still carries severity %q", got.Severity)
			}
		})
	}
}
