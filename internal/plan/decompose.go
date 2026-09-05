package plan

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
)

// ErrNoDecomposer reports a name nothing is registered under.
var ErrNoDecomposer = errors.New("plan: no such decomposer")

// Result is what a decomposer produced: the graph, the facts it read, and —
// for a judgement-based planner — who judged.
type Result struct {
	Nodes []Node
	Facts []Fact
	// AgentRef identifies the planner for a decomposer that is not
	// replayable. Required of one, refused of the other: a deterministic
	// decomposer naming an agent is a decomposer lying about what it is.
	AgentRef string
}

// Decomposer turns one origination into a graph.
//
// Implementations must be side-effect free with respect to the queue: Make
// calls them before anything is recorded, and a decomposer that submitted work
// itself would make the plan a description of what already happened rather
// than a thing an operator can see first.
type Decomposer interface {
	// Name is the token a declaration references.
	Name() string
	// Replayable is the decomposer's own claim about itself: whether the same
	// inputs re-derive the same graph. See [Plan.Replayable].
	Replayable() bool
	Decompose(ctx context.Context, ing ingress.Ingress, decl kind.Declaration) (Result, error)
}

// Registry resolves a declaration's decomposer name to its producer.
type Registry struct{ m map[string]Decomposer }

// NewRegistry indexes decomposers by name.
func NewRegistry(ds ...Decomposer) (*Registry, error) {
	r := &Registry{m: map[string]Decomposer{}}
	for _, d := range ds {
		if err := r.Register(d); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds a decomposer. A duplicate name is refused rather than
// overwritten: two producers under one name means the graph an ingress got
// depends on assembly order, which is not a property anyone can review.
func (r *Registry) Register(d Decomposer) error {
	if d.Name() == "" {
		return fmt.Errorf("plan: a decomposer with no name cannot be referenced by a declaration")
	}
	if prev, dup := r.m[d.Name()]; dup {
		return fmt.Errorf("plan: decomposer %q is registered twice (%T and %T) — which graph an ingress got would depend on assembly order", d.Name(), prev, d)
	}
	r.m[d.Name()] = d
	return nil
}

// Resolve answers a name.
func (r *Registry) Resolve(name string) (Decomposer, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: %q (no decomposers are registered)", ErrNoDecomposer, name)
	}
	d, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %s)", ErrNoDecomposer, name, strings.Join(r.Names(), ", "))
	}
	return d, nil
}

// Names lists the registered decomposers, sorted.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.m))
	for n := range r.m {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Default is the deterministic, agent-free set that ships.
//
// Every decomposer in it is replayable and none of them can reach a model.
// That is the constraint rather than a coincidence: intake runs decomposition
// on EVERY ingress, so a stage on every intake is a stage on the critical path
// of every intake, and if the ordinary path needed an LLM call then work
// admission would depend on an agent being available — exactly what the
// no-agent invariant forbids. An agent planner is opt-in per Box and per kind,
// which puts it back in a seat whose failure parks one item.
func Default() *Registry {
	r, err := NewRegistry(Identity{}, Sequence{})
	if err != nil {
		panic("plan: the default registry does not build: " + err.Error())
	}
	return r
}

// Make resolves the declaration's decomposer, runs it, and seals the plan.
//
// This is the only place a plan comes into being, and the only place the
// no-agent constraint can be enforced mechanically: a declaration that says
// nothing about decomposition gets the identity decomposer, and if whatever is
// registered under that name is not replayable then the default path has
// acquired a judgement — which is the commit worth failing at, not the quarter
// somebody notices intake stopped when a model was down.
func Make(ctx context.Context, reg *Registry, ing ingress.Ingress, decl kind.Declaration) (Plan, error) {
	name := decl.Decomposer()
	d, err := reg.Resolve(name)
	if err != nil {
		return Plan{}, err
	}
	if decl.Decompose.Decomposer == "" && !d.Replayable() {
		return Plan{}, fmt.Errorf("plan: the default decomposer %q is not replayable — the ordinary intake path must be deterministic and agent-free, or admitting work depends on an agent being available; name a judgement planner on the kind declaration to opt in",
			name)
	}
	res, err := d.Decompose(ctx, ing, decl)
	if err != nil {
		return Plan{}, fmt.Errorf("plan: %s decomposing %s: %w", name, ing.ID, err)
	}
	if d.Replayable() && res.AgentRef != "" {
		return Plan{}, fmt.Errorf("plan: %s claims to be replayable and named an agentref — a deterministic decomposer with an author is one of the two lying", name)
	}
	p := Plan{
		Ingress: ing.ID, Kind: string(ing.Kind),
		Decomposer: name, Replayable: d.Replayable(), AgentRef: res.AgentRef,
		Facts: res.Facts, Nodes: res.Nodes,
	}
	if err := p.Seal(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// IdentityNode is the single node an identity plan produces.
const IdentityNode = "work"

// Identity emits a one-node graph: the ordinary issue, which needs no
// decomposition and must not pay for one.
type Identity struct{}

// Name satisfies [Decomposer].
func (Identity) Name() string { return kind.DecomposeIdentity }

// Replayable satisfies [Decomposer]. Trivially: the graph is a function of the
// origination alone.
func (Identity) Replayable() bool { return true }

// Decompose emits the ingress as one unit of work.
func (Identity) Decompose(_ context.Context, ing ingress.Ingress, _ kind.Declaration) (Result, error) {
	return Result{Nodes: []Node{{ID: IdentityNode, Task: TaskOf(ing), Repo: ing.Repo, Summary: ing.Title}}}, nil
}

// TaskOf is what an origination asks for in one line, falling back through the
// same order intake used before there was a decomposition stage: the
// delivery's own words, then the title, then a bare instruction to attend to
// it. The last is not a placeholder — an ingress that arrived with no text is
// still work somebody has to look at, and dropping it would be work
// disappearing plausibly.
func TaskOf(ing ingress.Ingress) string {
	switch {
	case ing.Note != "":
		return ing.Note
	case ing.Title != "":
		return ing.Title
	default:
		return "attend to ingress " + ing.ID
	}
}

// Sequence emits the declaration's steps as a chain, each depending on the one
// before it.
//
// This is what finally holds an incident's own sentence. "It finishes when
// service is restored AND the postmortem has landed — restoration alone does
// not close it" is two nodes and an edge, and for as long as the kind was a Go
// constant there was nothing in the system that could represent it.
type Sequence struct{}

// Name satisfies [Decomposer].
func (Sequence) Name() string { return kind.DecomposeSequence }

// Replayable satisfies [Decomposer]. The graph is a function of the
// declaration, which is governed and pinned.
func (Sequence) Replayable() bool { return true }

// Decompose chains the declared steps.
func (Sequence) Decompose(_ context.Context, ing ingress.Ingress, decl kind.Declaration) (Result, error) {
	if len(decl.Decompose.Steps) == 0 {
		return Result{}, fmt.Errorf("kind %q declares no steps", decl.Name)
	}
	nodes := make([]Node, 0, len(decl.Decompose.Steps))
	prev := ""
	for _, s := range decl.Decompose.Steps {
		n := Node{ID: s.Name, Task: stepTask(ing, s), Repo: ing.Repo}
		if prev != "" {
			n.DependsOn = []string{prev}
		}
		nodes = append(nodes, n)
		prev = s.Name
	}
	return Result{Nodes: nodes}, nil
}

// stepTask names the origination a declared step belongs to, using the TITLE
// and never the note.
//
// The note is the delivery's payload, and under a reference-only data envelope
// it may not travel at all — see [agent.Envelope]. A declared step is exactly
// the place that rule would be smuggled past, because the text is being
// assembled by the factory rather than passed through, and it would look like
// helpfulness. The title is the human-readable handle an operator gave the
// origination; a box that classifies its titles too can leave them empty.
func stepTask(ing ingress.Ingress, s kind.Step) string {
	if ing.Title == "" {
		return s.Task
	}
	return s.Task + " (" + ing.Title + ")"
}
