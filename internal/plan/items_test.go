package plan

import "testing"

// TestItemsCopiesTheNodesSummary. Items is the only place a node's fields
// become a queue.WorkItem's, and Summary has to make that crossing the same
// way Task always has, or the room ends up reading the field it needs off a
// row that never carried it.
func TestItemsCopiesTheNodesSummary(t *testing.T) {
	p := Plan{Ingress: "ing-1", Replayable: true,
		Nodes: []Node{{ID: IdentityNode, Task: "fix the thing", Summary: "the thing is broken"}}}
	if err := p.Seal(); err != nil {
		t.Fatal(err)
	}
	items := Items(issue("ing-1"), p)
	if len(items) != 1 {
		t.Fatalf("items: %d", len(items))
	}
	if got := items[0].Summary; got != "the thing is broken" {
		t.Fatalf("summary: %q", got)
	}
}

// TestItemsCopiesTheNodesAttempts: a node's per-unit attempt-budget override
// has to make the same crossing Model already does, or a plan that named one
// would silently lose it the moment it became work (issue #81).
func TestItemsCopiesTheNodesAttempts(t *testing.T) {
	p := Plan{Ingress: "ing-1", Replayable: true,
		Nodes: []Node{{ID: IdentityNode, Task: "fix the thing", Attempts: 4}}}
	if err := p.Seal(); err != nil {
		t.Fatal(err)
	}
	items := Items(issue("ing-1"), p)
	if len(items) != 1 {
		t.Fatalf("items: %d", len(items))
	}
	if got := items[0].Attempts; got != 4 {
		t.Fatalf("attempts: %d, want 4", got)
	}
}
