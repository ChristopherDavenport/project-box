package plan

import (
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// Items is the plan as work, in the order the plan declared.
//
// This is the first producer of [queue.WorkItem.DependsOn] outside a test.
// Phase 10 built and property-tested a graph engine — a diamond claimed in
// topological order, a dead root cascading leaf by leaf with the cause named —
// against a graph nothing in production could construct an edge for. The
// decomposition stage is the author it was missing.
//
// Node ids become item ids through [Plan.ItemID], so a node's dependency on
// another node is an item's dependency on another item and the queue needs to
// know nothing about plans.
func Items(ing ingress.Ingress, p Plan) []queue.WorkItem {
	out := make([]queue.WorkItem, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		repo := n.Repo
		if repo == "" {
			repo = ing.Repo
		}
		var deps []string
		for _, d := range n.DependsOn {
			deps = append(deps, p.ItemID(d))
		}
		out = append(out, queue.WorkItem{
			ID: p.ItemID(n.ID), Tenant: ing.Tenant, Box: ing.Box,
			Ingress: ing.ID, Plan: p.Ref, Node: n.ID, Kind: string(ing.Kind),
			Requester: ing.Requester, Repo: repo,
			Task: n.Task, Depth: ing.Depth, DependsOn: deps,
			Sections: n.Sections, Model: n.Model,
			// Repair work wakes with the attempt it follows rather than cold;
			// the origination is where that travels, the same way the depth
			// does. A fanned-out repair gives every node the same predecessor,
			// which is correct: they are all following the one attempt.
			Prior: ing.Prior,
		})
	}
	return out
}
