package plan

import (
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
)

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

// TestItemsCopiesTheIngressOriginKey: the delivery key is the origination's,
// copied never derived — Items must carry it onto every planned item exactly
// as the ingress holds it, issue-shaped or not, or no PR built from that item
// can ever say Fixes (issue #142).
func TestItemsCopiesTheIngressOriginKey(t *testing.T) {
	for _, key := range []string{"issue-110", "campaign-7"} {
		ing := ingress.Ingress{ID: "ing-1", Kind: kind.SeedIssue, Tenant: tn, Box: "shop",
			Repo: "app", Requester: "chris", Key: key}
		p := Plan{Ingress: "ing-1", Replayable: true,
			Nodes: []Node{{ID: IdentityNode, Task: "fix the thing"}}}
		if err := p.Seal(); err != nil {
			t.Fatal(err)
		}
		items := Items(ing, p)
		if len(items) != 1 {
			t.Fatalf("items: %d", len(items))
		}
		if got := items[0].OriginKey; got != key {
			t.Fatalf("origin key: %q, want %q", got, key)
		}
	}
}
