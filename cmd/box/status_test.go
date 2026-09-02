package main

import (
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestTheBreakdownReadsTheSameEveryTime: Go randomises map iteration, so a
// tally rendered in map order prints differently on two runs against an
// unchanged queue — an operator diffing `box status` would read movement into
// a factory that had not moved. The states have no rank, so alphabetical is
// the order, and it has to be an order rather than whatever came out.
func TestTheBreakdownReadsTheSameEveryTime(t *testing.T) {
	tally := map[queue.State]int{
		queue.StateWaiting: 1, queue.StateDone: 9, queue.StatePending: 4,
		queue.StateParked: 2, queue.StateClaimed: 1,
	}
	const want = "claimed=1 done=9 parked=2 pending=4 waiting=1"
	for range 20 {
		if got := byState(tally); got != want {
			t.Fatalf("byState = %q, want %q", got, want)
		}
	}
	if got := byState(nil); got != "" {
		t.Errorf("an empty queue should render nothing, got %q", got)
	}
}
