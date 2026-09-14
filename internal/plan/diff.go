package plan

import (
	"slices"
	"sort"
)

// NodeChange is one node present in both plans whose declaration differs.
type NodeChange struct {
	ID            string
	Before, After Node
}

// Delta is what changed between two plans' graphs, compared by node ID —
// the same identity a dependent names and an item id carries, so a diff
// reads in the vocabulary the graph itself uses.
type Delta struct {
	Added   []Node
	Removed []Node
	Changed []NodeChange
}

// Diff compares two plans by node ID: a node only in b is Added, a node only
// in a is Removed, and a node in both is Changed when its Task, Repo, Model,
// or its DependsOn or Sections sets differ. DependsOn and Sections compare
// as sets — the same order-insensitivity [Plan.Seal] already gives the
// graph's ref, so a decomposer that emits the same edges in a different
// order does not manufacture a diff nobody asked for. All three slices are
// sorted by ID, extending Seal's own ordering guarantee to diffs.
func Diff(a, b Plan) Delta {
	byA := make(map[string]Node, len(a.Nodes))
	for _, n := range a.Nodes {
		byA[n.ID] = n
	}
	byB := make(map[string]Node, len(b.Nodes))
	for _, n := range b.Nodes {
		byB[n.ID] = n
	}

	var d Delta
	for _, n := range b.Nodes {
		if _, ok := byA[n.ID]; !ok {
			d.Added = append(d.Added, n)
		}
	}
	for _, n := range a.Nodes {
		if _, ok := byB[n.ID]; !ok {
			d.Removed = append(d.Removed, n)
		}
	}
	for id, an := range byA {
		bn, ok := byB[id]
		if ok && !sameNode(an, bn) {
			d.Changed = append(d.Changed, NodeChange{ID: id, Before: an, After: bn})
		}
	}

	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].ID < d.Removed[j].ID })
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].ID < d.Changed[j].ID })
	return d
}

// sameNode reports whether two nodes with the same ID declare the same
// work: same task, repo and tier, the same DependsOn and Sections sets
// regardless of the order either side listed them in, and the same reason
// for each edge — a dependency re-justified on different grounds is a
// different judgement even when the edge itself is unchanged.
func sameNode(a, b Node) bool {
	return a.Task == b.Task && a.Repo == b.Repo && a.Model == b.Model &&
		sameSet(a.DependsOn, b.DependsOn) && sameSet(a.Sections, b.Sections) &&
		sameReasons(a.EdgeReasons, b.EdgeReasons)
}

// sameReasons compares two edge-reason maps for equality.
func sameReasons(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := slices.Clone(a), slices.Clone(b)
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}
