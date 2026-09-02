package control

import (
	"context"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestTheCountIsTheListingCounted is the design note of issue #20: the counts
// on /v1/status must be DERIVABLE from the listing rather than a separate
// truth.
//
// They used to be two accounts of one queue. The listing folded each item's
// chain; the count did its own walk of index/pending with its own copy of the
// not-yet-settled vocabulary, and the two agreed only by coincidence of two
// switch statements matching — the kind of agreement that lasts exactly until
// a state is added to one of them. This asserts the agreement structurally:
// every number the room publishes is a tally of rows the room also serves.
func TestTheCountIsTheListingCounted(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	// One of every shape a queue actually holds: work nobody has started,
	// work held on a gate, work between sessions, work a person must look at,
	// and work that finished.
	submit(t, q, "wi-fresh")

	waiting := submit(t, q, "wi-waiting")
	_, lease, err := q.Claim(ctx, waiting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Wait(ctx, waiting, lease, "branch main is leased"); err != nil {
		t.Fatal(err)
	}

	handed := submit(t, q, "wi-handed")
	_, lease, err = q.Claim(ctx, handed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.HandOff(ctx, handed, lease, "out of room, not out of work"); err != nil {
		t.Fatal(err)
	}

	parked := submit(t, q, "wi-parked")
	_, lease, err = q.Claim(ctx, parked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, parked, lease, queue.SeverityMedium, "check: fail (go vet)"); err != nil {
		t.Fatal(err)
	}

	b, err := r.Backlog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all, err := r.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != len(all) {
		t.Fatalf("total %d is not the unfiltered listing's %d", b.Total, len(all))
	}

	// State by state, the tally is what a caller narrowing the listing to that
	// state would count for themselves. This is the whole property: there is
	// no number here a client could not have derived from /v1/items.
	sum := 0
	for state, n := range b.ByState {
		rows, err := r.Queue(ctx, state)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != n {
			t.Errorf("backlog says %d %s, the listing filtered to %s returns %d",
				n, state, state, len(rows))
		}
		sum += n
	}
	if sum != b.Total {
		t.Fatalf("the parts sum to %d and the total says %d", sum, b.Total)
	}

	// And the roll-up is those parts under one vocabulary rather than a second
	// reading of the rows.
	want := 0
	for state, n := range b.ByState {
		if queue.Claimable(state) {
			want += n
		}
	}
	if b.Pending != want {
		t.Fatalf("pending %d is not the claimable states' %d: %+v", b.Pending, want, b.ByState)
	}
}

// TestPendingIsEverythingStillOwedARun pins the content of that vocabulary on
// the two states most easily left out, both of which would make the only
// number an operator watches drop while the work was still there.
//
// Waiting is routine review latency, not finished. Handed off is a session
// that ran out of room and asked for a fresh one — the item has been started
// three times and is owed a fourth, which is the reading the count must give.
func TestPendingIsEverythingStillOwedARun(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	waiting := submit(t, q, "wi-waiting")
	_, lease, err := q.Claim(ctx, waiting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Wait(ctx, waiting, lease, "gate has not answered"); err != nil {
		t.Fatal(err)
	}
	handed := submit(t, q, "wi-handed")
	_, lease, err = q.Claim(ctx, handed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.HandOff(ctx, handed, lease, "out of context"); err != nil {
		t.Fatal(err)
	}
	parked := submit(t, q, "wi-parked")
	_, lease, err = q.Claim(ctx, parked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, parked, lease, queue.SeverityMedium, "needs a person"); err != nil {
		t.Fatal(err)
	}

	b, err := r.Backlog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Pending != 2 {
		t.Fatalf("want waiting and handed-off both still owed a run, got pending=%d: %+v",
			b.Pending, b.ByState)
	}
	// Parked is not pending: it is owed a PERSON, and counting it twice would
	// make the exception queue's number a subset of a number that already
	// includes it.
	if b.ByState[queue.StateParked] != 1 {
		t.Fatalf("parked missing from the tally: %+v", b.ByState)
	}
	if b.Total != 3 {
		t.Fatalf("total %d, want 3", b.Total)
	}
}

// TestAnEmptyQueueCountsNothing: the fold must produce a zero, not an error or
// a nil map read as an outage. `box status` on a factory with nothing to do is
// the most common call there is.
func TestAnEmptyQueueCountsNothing(t *testing.T) {
	r, _ := setup(t)
	b, err := r.Backlog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 0 || b.Pending != 0 || len(b.ByState) != 0 {
		t.Fatalf("empty queue: %+v", b)
	}
}
