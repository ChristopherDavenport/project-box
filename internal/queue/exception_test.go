package queue

import (
	"context"
	"strings"
	"testing"
)

// park drives an item to the exception queue the way a run does.
func park(t *testing.T, qu *Queue, id, reason string) WorkItem {
	t.Helper()
	w := submit(t, qu, id)
	w, lease, err := qu.Claim(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	w, err = qu.Park(context.Background(), w, lease, SeverityMedium, reason)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestResolveClosesAnExceptionWithoutALanding. StateParked used to be written
// in one place and read in five, with nothing anywhere moving an item off it:
// every exception was permanent and the queue only grew. The agent's own
// verdict — "this issue is already implemented, close it" — was a
// recommendation the system had no verb to act on.
func TestResolveClosesAnExceptionWithoutALanding(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "escalated by mason: already implemented by ab8ccfb")

	got, err := qu.Resolve(ctx, tn, "wi-1", "chris", "agreed — closing the issue instead")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateResolved {
		t.Fatalf("state = %s, want resolved", got.State)
	}

	// It survives the fold, and it leaves the exception queue by doing so.
	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State != StateResolved || reloaded.Note != "agreed — closing the issue instead" {
		t.Fatalf("resolve did not survive the fold: %+v", reloaded)
	}

	// Resolved is NOT done: review sampling draws on the landings index, and
	// an auditor pulling a sampled item expects a sha to exist.
	for rec, err := range qu.Store.Log().Since(ctx, tn, "index/landings", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == "wi-1" {
			t.Fatal("a resolved item reached the landings index; sampling would offer an auditor a change that never landed")
		}
	}
}

// TestResolveIsAttributedToTheOperator. Every other transition on an item is
// the runner's; this is the one a person makes by hand, and an audit that
// could not tell them apart would be missing the only human act in the loop.
func TestResolveIsAttributedToTheOperator(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "gate flake")
	if _, err := qu.Resolve(ctx, tn, "wi-1", "chris", "not worth a retry"); err != nil {
		t.Fatal(err)
	}
	var actor string
	for rec, err := range qu.Store.Log().Since(ctx, tn, scope("wi-1"), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "resolved" {
			actor = rec.Actor
		}
	}
	if actor != "chris" {
		t.Fatalf("resolved record actor = %q, want the operator, not %q", actor, "runner-1")
	}
}

// TestRetryReturnsParkedWorkToTheQueue. Without this an item that parked for
// a transient reason — a flaky gate, a timeout — was work the factory could
// never attempt again.
func TestRetryReturnsParkedWorkToTheQueue(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "gate timed out")

	got, err := qu.Retry(ctx, tn, "wi-1", "chris", "gate was flaky; try again")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StatePending {
		t.Fatalf("state = %s, want pending", got.State)
	}
	// Claimable again is the whole point.
	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, reloaded); err != nil {
		t.Fatalf("a retried item could not be claimed: %v", err)
	}
}

// TestOnlyAnExceptionCanBeReopened. An operator resolving something still
// running would race the runner holding its lease.
func TestOnlyAnExceptionCanBeReopened(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	if _, _, err := qu.Claim(ctx, w); err != nil {
		t.Fatal(err)
	}

	for _, verb := range []string{"resolve", "retry"} {
		var err error
		if verb == "resolve" {
			_, err = qu.Resolve(ctx, tn, "wi-1", "chris", "closing")
		} else {
			_, err = qu.Retry(ctx, tn, "wi-1", "chris", "again")
		}
		if err == nil {
			t.Fatalf("%s took a claimed item off a queue it was never on", verb)
		}
		if !strings.Contains(err.Error(), "not parked") {
			t.Fatalf("%s: err = %v, want it to name the state", verb, err)
		}
	}
}

// TestResolveDemandsAReason. Closing an exception is a judgement, and an
// audit needs whose and why — the same bar stop and resume are held to.
func TestResolveDemandsAReason(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "escalated")
	if _, err := qu.Resolve(ctx, tn, "wi-1", "chris", ""); err == nil {
		t.Fatal("an exception was closed with no reason on the record")
	}
}

// TestHandoffKeepsTheWorkAndTheItem is the queue's half of issue #12: a
// session ended, the item did not.
//
// The two things it must not do are the two ways this state could have been
// spelled with an existing one. Parking it would file work needing nobody in
// the queue that exists to name work needing somebody — the argument the
// gate's pending decision already made about waiting, on a third axis. And
// dropping the resume point would throw away the change the session got
// partway through, which is the escalate-shaped outcome the verb was added to
// avoid.
func TestHandoffKeepsTheWorkAndTheItem(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-hop")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepUnfinished("abc123", "base456")

	w, err = qu.HandOff(ctx, w, lease, "out of room with the migration half applied")
	if err != nil {
		t.Fatal(err)
	}
	if w.State != StateHandedOff || w.Hops != 1 {
		t.Fatalf("handoff: %+v", w)
	}

	got, err := qu.Load(ctx, tn, "wi-hop")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateHandedOff || got.Hops != 1 {
		t.Fatalf("the hop did not survive the fold: %+v", got)
	}
	if got.Built != "abc123" || got.BuiltBase != "base456" || !got.Unfinished {
		t.Fatalf("the session's work was dropped by the transition: %+v", got)
	}
	if got.Note != "out of room with the migration half applied" {
		t.Fatalf("note = %q", got.Note)
	}

	// It is claimable again, by the same runner, with no operator in between.
	if _, _, err := qu.Claim(ctx, got); err != nil {
		t.Fatalf("a handed-off item could not be claimed for its fresh session: %v", err)
	}

	// And it is not on the exception queue's index, which is what the control
	// room lists as needing a person.
	for rec, err := range qu.Store.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == "wi-hop" {
			t.Fatal("a handed-off item was filed as an exception; it needs no person")
		}
	}
}

// TestSettlingDropsTheUnfinishedMark: every settle drops the resume point, and
// the mark that says what KIND of resume point it was has to go with it. A
// stale "unfinished" outliving its commit would have a later attempt adopt a
// tree that is not there and, worse, run the agent over a change already
// judged.
func TestSettlingDropsTheUnfinishedMark(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-hop-settled")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepUnfinished("abc123", "base456")
	if _, err := qu.Park(ctx, w, lease, SeverityMedium, "gave up"); err != nil {
		t.Fatal(err)
	}
	got, err := qu.Load(ctx, tn, "wi-hop-settled")
	if err != nil {
		t.Fatal(err)
	}
	if got.Unfinished || got.Built != "" {
		t.Fatalf("a settled item kept its unfinished resume point: %+v", got)
	}
}
