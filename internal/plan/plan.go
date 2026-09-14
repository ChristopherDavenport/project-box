// Package plan is the decomposition an origination gets before any of its
// work is claimed.
//
// The decomposition is the most consequential judgement the factory used to
// leave unrecorded. The published chain was
//
//	ingress -> work item -> run -> changeset -> commit sha -> ...
//
// and when one origination became six items, the decision about WHICH six
// determined what got built, in what order, and what was silently omitted —
// and it sat between the first two links leaving no artifact at all. An
// auditor sampling a change could reconstitute the agent and read the diff,
// and could not recover why this was the work. The chain now reads
//
//	ingress -> plan -> work item -> run -> changeset -> commit sha -> ...
//
// and the plan is content-addressed, with that digest riding every item it
// produced. Recording only WHICH decomposer ran would be insufficient for the
// same reason recording an agent's refs is insufficient: a decomposer over
// moving facts yields a different graph on Tuesday while every reference to it
// looks unchanged.
package plan

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// planDomain separates this hash from every other in the system, the way the
// agentref's domain does. Versioned, so a change to what a plan is made of is
// a change every later reader can see rather than a silent re-pointing of the
// same names at different content.
const planDomain = "project-box/planref/v1"

// ErrPlan marks a graph that does not hold together.
var ErrPlan = errors.New("plan")

// ErrNoAnswer marks a planner run that never answered: a transport error, a
// mid-response server error, or a limit refusal, as distinct from a planner
// that answered with an escalation or a plan the parse refuses. A decomposer
// wraps it around whatever it saw at the seam where the run stopped
// talking — the acp planner wraps it at its rt.Run seam — so intake can tell
// "the account's window closed on this run" from "the planner judged and
// that judgement does not hold."
var ErrNoAnswer = errors.New("plan: the planner run never answered")

// Node is one unit of work the plan calls for.
type Node struct {
	// ID names this node within the plan, and it is what a dependent names.
	// It becomes part of the work item's id, so it is a plain token.
	ID string `json:"id"`
	// Task is what the work asks for.
	Task string `json:"task"`
	// Summary is the one line a human reads for this unit, distinct from
	// Task: the task is the full paragraph an agent needs, and a control
	// room that showed that paragraph as the unit's name was the gap this
	// field closes. Empty falls back to the task's first line.
	Summary string `json:"summary,omitempty"`
	// Repo is the target repository when the plan fans across several; empty
	// inherits the origination's.
	Repo string `json:"repo,omitempty"`
	// DependsOn names nodes in this plan that must settle done first. This is
	// the work graph, and until there was a decomposition stage nothing
	// outside a test had ever written one.
	DependsOn []string `json:"depends_on,omitempty"`
	// EdgeReasons names, for a dependency in DependsOn, the symbol or file the
	// edge waits for — keyed by the dependency's node id. A judgement plan's
	// edge with no entry here is a chain nothing asked to justify itself,
	// which [Plan.validate] refuses; a replayable decomposer's chain is a
	// function of a governed declaration and is exempt. Kept parallel to
	// DependsOn rather than folded into a typed edge so old record bodies —
	// and [queue.WorkItem.DependsOn], which this is copied into — keep
	// unmarshaling exactly as they always have.
	EdgeReasons map[string]string `json:"edge_reasons,omitempty"`
	// Sections names the regions of the repository this unit is expressed
	// in. It is the second half of what a decomposition decides: not only
	// WHAT the units are and in what order, but WHERE each one works — which
	// is what lets two of them run at once and keeps two of them from
	// colliding. Empty is the whole repository.
	Sections []string `json:"sections,omitempty"`
	// Model is the tier this unit should run on, when the decomposer judged
	// one — the cheapest model the unit's difficulty allows. Empty leaves
	// the section's, the role's or the box's default in force.
	Model string `json:"model,omitempty"`
	// Attempts overrides this node's attempt budget: how many builds its
	// claim may spend on a gate or review refusal before it parks — see
	// [queue.WorkItem.Attempts]. Zero inherits the repo's or box's default.
	Attempts int `json:"attempts,omitempty"`
}

// Fact is one input a decomposer read, and when it read it.
//
// AsOf is recorded and deliberately NOT hashed. A replayable decomposer over
// unchanged facts must re-derive an identical graph, and folding the read time
// into the digest would make every replay a different plan — which would leave
// the staleness mechanism unable to tell "the facts moved" from "time passed".
// The value is identity; the as-of is provenance.
type Fact struct {
	Key   string    `json:"key"`
	Value string    `json:"value"`
	AsOf  time.Time `json:"as_of"`
}

// Plan is one resolved decomposition.
type Plan struct {
	// Ingress is the origination this plan decomposes. Every item traces to a
	// plan and every plan to an ingress, which strengthens no-orphan-work
	// rather than weakening it.
	Ingress string `json:"ingress"`
	// Kind is the declaration in force when this plan was made.
	Kind string `json:"kind"`
	// Decomposer named the producer, and Replayable is its own claim about
	// itself.
	//
	// A predicate over facts is replayable: the same inputs re-derive the same
	// graph, exactly like render determinism, and re-running it later to get a
	// DIFFERENT graph is how a plan built on stale facts announces itself. An
	// agent planner is not replayable, only reconstitutable, and its record
	// carries the agentref instead. Claiming replay for the judgement case
	// would be dishonest, so the field is explicit rather than assumed.
	Decomposer string `json:"decomposer"`
	Replayable bool   `json:"replayable"`
	// AgentRef identifies the agent that produced a judgement-based plan,
	// empty for a deterministic one.
	AgentRef string `json:"agentref,omitempty"`
	// Facts are what the decomposer read.
	Facts []Fact `json:"facts,omitempty"`
	// Nodes is the graph.
	Nodes []Node `json:"nodes"`
	// Ref is the content address, filled by [Plan.Seal].
	Ref string `json:"ref,omitempty"`
	// At is when the plan was made. Provenance, not identity: excluded from
	// the digest for the reason [Fact.AsOf] is.
	At time.Time `json:"at"`
	// Depth is 1 + the longest dependency chain ending at any node — a node
	// with no dependencies has depth 1. Width is the largest number of nodes
	// sharing one depth. Both are projections of the graph [Plan.Seal] already
	// validated, computed for the operator who wants to see whether a
	// decomposition fanned out or chained without recounting the edges by
	// hand, and — like At — excluded from the digest: they follow from the
	// graph rather than adding to what it means.
	Depth int `json:"depth,omitempty"`
	Width int `json:"width,omitempty"`
}

// Seal validates the graph and fills [Plan.Ref].
//
// The digest is over the GRAPH rather than over the listing: nodes and their
// dependencies are canonicalized into a stable order first, so a decomposer
// that emits the same graph in a different order produces the same ref. That
// is what makes "the same declaration and the same facts produce an identical
// graph" a property a test can assert, and it is the precondition for a plan
// diff meaning anything at all — an ordering difference rendered as a diff is
// noise, and a diff that is mostly noise is one nobody reads.
func (p *Plan) Seal() error {
	if err := p.validate(); err != nil {
		return err
	}
	h := sha256.New()
	put := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(planDomain)
	put(p.Ingress)
	put(p.Kind)
	put(p.Decomposer)
	if p.Replayable {
		put("replayable")
	} else {
		put("judgement")
	}
	put(p.AgentRef)

	facts := slices.Clone(p.Facts)
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Key != facts[j].Key {
			return facts[i].Key < facts[j].Key
		}
		return facts[i].Value < facts[j].Value
	})
	for _, f := range facts {
		put("fact:" + f.Key + "=" + f.Value)
	}

	nodes := slices.Clone(p.Nodes)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for _, n := range nodes {
		deps := slices.Clone(n.DependsOn)
		slices.Sort(deps)
		put("node:" + n.ID + ":" + n.Repo + ":" + n.Task + ":" + strings.Join(deps, "\x00"))
		// Where and on what: the same graph expressed in different sections
		// or on a different tier is a different plan, because it runs
		// differently and costs differently.
		sections := slices.Clone(n.Sections)
		slices.Sort(sections)
		if len(sections) > 0 || n.Model != "" || n.Attempts != 0 {
			put("where:" + n.ID + ":" + strings.Join(sections, "\x00") + ":" + n.Model + ":" + strconv.Itoa(n.Attempts))
		}
		// Why: only when a node carries a reason at all, so every plan sealed
		// before edges could carry one keeps byte-identical ref — this does not
		// bump planDomain.
		if len(n.EdgeReasons) > 0 {
			keys := make([]string, 0, len(n.EdgeReasons))
			for k := range n.EdgeReasons {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+"="+n.EdgeReasons[k])
			}
			put("reasons:" + n.ID + ":" + strings.Join(parts, "\x00"))
		}
	}
	p.Ref = "plan-" + hex.EncodeToString(h.Sum(nil))[:32]

	byID := make(map[string]Node, len(p.Nodes))
	for _, n := range p.Nodes {
		byID[n.ID] = n
	}
	depth := nodeDepths(byID)
	widthAt := map[int]int{}
	for _, d := range depth {
		widthAt[d]++
		if d > p.Depth {
			p.Depth = d
		}
	}
	for _, w := range widthAt {
		if w > p.Width {
			p.Width = w
		}
	}
	return nil
}

// nodeDepths computes each node's depth: 1 + the max depth of its
// dependencies, 1 for a node with none. Callable only after [Plan.validate]
// has confirmed the graph is acyclic — the recursion below does not
// itself guard against a cycle.
func nodeDepths(byID map[string]Node) map[string]int {
	depth := make(map[string]int, len(byID))
	var compute func(id string) int
	compute = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		max := 0
		for _, d := range byID[id].DependsOn {
			if v := compute(d); v > max {
				max = v
			}
		}
		depth[id] = 1 + max
		return depth[id]
	}
	for id := range byID {
		compute(id)
	}
	return depth
}

// validate refuses a graph the queue could not run.
func (p *Plan) validate() error {
	if p.Ingress == "" {
		return fmt.Errorf("%w: a plan that traces to no ingress is orphan work one level up", ErrPlan)
	}
	if len(p.Nodes) == 0 {
		return fmt.Errorf("%w: %s decomposed to nothing — a decomposer that produces no work must say so as a refusal, never as an empty graph", ErrPlan, p.Ingress)
	}
	if !p.Replayable && p.AgentRef == "" {
		return fmt.Errorf("%w: %s is not replayable and names no agentref — a judgement nobody can reconstitute is a judgement with no author", ErrPlan, p.Ingress)
	}
	byID := make(map[string]Node, len(p.Nodes))
	for _, n := range p.Nodes {
		switch {
		case n.ID == "":
			return fmt.Errorf("%w: %s has an unnamed node", ErrPlan, p.Ingress)
		case strings.ContainsAny(n.ID, " \t/"):
			return fmt.Errorf("%w: %s node %q — a node id is a plain token; it becomes part of a work item's id", ErrPlan, p.Ingress, n.ID)
		case n.Task == "":
			return fmt.Errorf("%w: %s node %q asks for nothing", ErrPlan, p.Ingress, n.ID)
		}
		if _, dup := byID[n.ID]; dup {
			return fmt.Errorf("%w: %s declares node %q twice — two nodes would share one work item and the later would silently win", ErrPlan, p.Ingress, n.ID)
		}
		for _, s := range n.Sections {
			if s == "" || strings.ContainsAny(s, " \t/") {
				return fmt.Errorf("%w: %s node %q names section %q — a section name is a plain token; it becomes a lease ref", ErrPlan, p.Ingress, n.ID, s)
			}
		}
		if strings.ContainsAny(n.Model, " \t\n") {
			return fmt.Errorf("%w: %s node %q names model %q — a tier is the value the agent lists, not a sentence", ErrPlan, p.Ingress, n.ID, n.Model)
		}
		if n.Attempts < 0 {
			return fmt.Errorf("%w: %s node %q attempts %d — a negative budget", ErrPlan, p.Ingress, n.ID, n.Attempts)
		}
		byID[n.ID] = n
	}
	for _, n := range p.Nodes {
		for _, d := range n.DependsOn {
			if d == n.ID {
				return fmt.Errorf("%w: %s node %q depends on itself — it would block forever, and the queue would report it as merely waiting", ErrPlan, p.Ingress, n.ID)
			}
			if _, ok := byID[d]; !ok {
				return fmt.Errorf("%w: %s node %q depends on %q, which this plan does not contain", ErrPlan, p.Ingress, n.ID, d)
			}
		}
	}
	// A cycle is not a slow plan, it is work that can never be claimed: the
	// queue blocks a node until its dependencies are DONE, so a ring of them
	// sits pending forever and reads as an idle factory rather than a broken
	// graph.
	if cyc := firstCycle(byID); cyc != nil {
		return fmt.Errorf("%w: %s has a dependency cycle (%s) — every node in it would block forever and none of them would look broken",
			ErrPlan, p.Ingress, strings.Join(cyc, " -> "))
	}
	return nil
}

// firstCycle returns one cycle's node ids, or nil.
func firstCycle(byID map[string]Node) []string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := map[string]int{}
	var stack []string
	var walk func(id string) []string
	walk = func(id string) []string {
		colour[id] = grey
		stack = append(stack, id)
		deps := slices.Clone(byID[id].DependsOn)
		slices.Sort(deps) // deterministic reporting: the same graph names the same cycle
		for _, d := range deps {
			switch colour[d] {
			case grey:
				at := slices.Index(stack, d)
				return append(slices.Clone(stack[at:]), d)
			case white:
				if c := walk(d); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		colour[id] = black
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if colour[id] == white {
			if c := walk(id); c != nil {
				return c
			}
		}
	}
	return nil
}

// ItemID is the queue id for one of this plan's nodes.
//
// An IDENTITY plan keeps the id the queue has always used for an ingress. The
// decomposition stage arrives on top of a running factory, and an ordinary
// issue must not be renamed by it: `wi-<ingress>` is on live chains, in
// evidence, and in ids an operator has already read out of the control room.
// Everything else names its node.
//
// The carve-out is tied to [IdentityNode] rather than to "this plan has one
// node", and the difference is load-bearing. A standing origination decomposes
// a wave at a time, and a wave of one would otherwise collapse to the bare
// ingress id — so the second wave of one would collide with the first, and the
// queue would report the new work as already done.
func (p Plan) ItemID(nodeID string) string {
	if len(p.Nodes) == 1 && p.Nodes[0].ID == IdentityNode {
		return "wi-" + p.Ingress
	}
	return "wi-" + p.Ingress + "-" + nodeID
}

// FanOut is how many units this plan calls for — the number an operator has to
// see before anything claims one.
func (p Plan) FanOut() int { return len(p.Nodes) }

// Node answers by id.
func (p Plan) Node(id string) (Node, bool) {
	for _, n := range p.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}
