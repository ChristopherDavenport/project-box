package control

import (
	"fmt"
	"testing"
)

// TestTheWindowIsBounded: the bus runs inside a daemon designed to be left
// alone overnight, so "we keep the frames" has to mean a stated number of
// them. One measured session made 110 tool calls in 21 minutes; unbounded is
// a leak with a schedule.
func TestTheWindowIsBounded(t *testing.T) {
	l := &Live{Keep: 4}
	for i := range 10 {
		l.Append("wi-1", Frame{Kind: "tool", Tool: fmt.Sprintf("t%d", i)})
	}
	got := l.Item("wi-1")
	if len(got) != 4 {
		t.Fatalf("kept %d frames, want 4", len(got))
	}
	// The window keeps the NEWEST, because what a live view is for is what is
	// happening now — the beginning of a run is the chain's job.
	if got[0].Tool != "t6" || got[3].Tool != "t9" {
		t.Errorf("window = %v … %v, want t6 … t9", got[0].Tool, got[3].Tool)
	}
	if l.Head() != 10 {
		t.Errorf("head = %d, want 10 — the sequence numbers the frames issued, not the frames kept", l.Head())
	}
}

// TestSinceIsACursor: a subscriber holds the last sequence it saw and asks for
// what followed. One cursor over the whole bus rather than one per item,
// because the fan-out case is a room watching every unit at once.
func TestSinceIsACursor(t *testing.T) {
	l := &Live{}
	a := l.Append("wi-1", Frame{Kind: "started"})
	l.Append("wi-2", Frame{Kind: "started"})
	l.Append("wi-1", Frame{Kind: "tool", Tool: "Read"})

	if got := l.Since(0); len(got) != 3 {
		t.Fatalf("Since(0) = %d frames, want the whole window", len(got))
	}
	after := l.Since(a.Seq)
	if len(after) != 2 || after[0].Item != "wi-2" || after[1].Tool != "Read" {
		t.Fatalf("Since(%d) = %+v", a.Seq, after)
	}
	if got := l.Since(l.Head()); got != nil {
		t.Errorf("Since(head) = %+v, want nothing — a caught-up subscriber is caught up", got)
	}
}

// TestFramesDoNotLeakBetweenUnits is the fan-out property at its smallest.
// Two runs at once must read as two runs, not as one interleaved feed: the
// question an operator has is whether THIS unit is moving.
func TestFramesDoNotLeakBetweenUnits(t *testing.T) {
	l := &Live{}
	l.Append("wi-1", Frame{Kind: "tool", Tool: "Read"})
	l.Append("wi-2", Frame{Kind: "tool", Tool: "Bash"})
	l.Append("wi-1", Frame{Kind: "tool", Tool: "Edit"})

	one, two := l.Item("wi-1"), l.Item("wi-2")
	if len(one) != 2 || one[0].Tool != "Read" || one[1].Tool != "Edit" {
		t.Errorf("wi-1 timeline = %+v", one)
	}
	if len(two) != 1 || two[0].Tool != "Bash" {
		t.Errorf("wi-2 timeline = %+v", two)
	}
}

// TestSilenceIsNotBlindness: an empty timeline means two very different
// things, and only one of them is a run to go and look at. The bus is asked
// which, rather than the reader being left to assume the alarming one.
func TestSilenceIsNotBlindness(t *testing.T) {
	l := &Live{}
	l.Append("wi-mine", Frame{Kind: "started"})
	if !l.Watching("wi-mine") {
		t.Error("a unit this bus has frames for reads as unwatched")
	}
	if l.Watching("wi-somebody-elses") {
		t.Error("a unit this bus never saw reads as watched — an empty timeline would then read as an idle agent")
	}
	var absent *Live
	if absent.Watching("wi-mine") || absent.Item("wi-mine") != nil || absent.Head() != 0 {
		t.Error("a nil bus must answer as a daemon nothing reports to, not panic")
	}
}
