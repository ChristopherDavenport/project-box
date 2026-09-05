// Package kind is what an origination is allowed to be, as a declaration
// rather than as a Go constant.
//
// Three kinds used to exist as constants with behaviour welded to them: two
// string compares decided whether an origination could preempt a lease and
// invoke break-glass, intake admitted one of the three and silently dropped
// the other two, and an incident's completion rule — restored AND a postmortem
// — lived in a doc comment. Read together those were not three variants of one
// flow but three code paths sharing a struct.
//
// The collapse is this project's own argument about layer sourcing, one level
// up: the mechanism stays permissive and policy does the restricting, because
// a type system is the wrong place to express a rule that legitimately varies
// by scope and by environment. Which kinds of work a Box accepts, what
// authority each carries, and how each decomposes vary by both.
//
// The gain is governance before it is extensibility. "What authority does an
// incident carry" was a compile-time fact no reviewer ever saw; as a
// declaration it passes through a ChangeSet and its gates like any other
// change. And a closed set does not prevent miscategorisation, it causes it —
// the fourth thing that shows up gets filed as an issue to avoid a code
// change, and issue's envelope quietly widens to accommodate it.
package kind

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// ErrUnknown reports a name no declaration answers to. Distinct from a
// malformed declaration because intake keys on the difference: an ingress
// whose kind nobody declared is a configuration gap an operator must see,
// never work to guess at.
var ErrUnknown = errors.New("kind: no such declaration")

// ErrCapability marks every refusal about authority — a kind reaching past
// its Box's ceiling, and a capability declared somewhere nobody reviewed.
var ErrCapability = errors.New("kind: capability")

// ErrDeclaration marks a declaration that does not hold together.
var ErrDeclaration = errors.New("kind: declaration")

// Capabilities is the authority work opened under a kind carries.
//
// Every field's zero is the absence of the authority, which is this system's
// rule everywhere: absence means minimum. A Box that has said nothing about
// capabilities holds none, so a kind declaring them under that Box is refused
// at load rather than quietly granted or quietly dropped.
type Capabilities struct {
	// Preempt takes a lease from a live holder. Only an open origination
	// carrying this may; see [ingress.Preempt].
	Preempt bool `yaml:"preempt" json:"preempt,omitempty"`
	// BreakGlass authorizes one emergency bypass, recorded.
	BreakGlass bool `yaml:"break_glass" json:"break_glass,omitempty"`
}

// The issue that opened this collapse named a third capability — landing to an
// environment repository — and it is deliberately not here. These two replace
// real string compares with real enforcement points; a third field with no
// reader would be an authority an operator could declare, see in their config,
// and get nothing from. That is the same call #3 made about a WORM receipt
// carrying no RetainUntil: a control nobody enforces is worse than an absent
// one, because it looks like coverage. The deploy plane still decides by lane
// and by role, and moving that decision here is a change to internal/ladder
// with its own tests.

// Any reports whether this grants anything at all.
func (c Capabilities) Any() bool { return c.Preempt || c.BreakGlass }

// Intersect applies a ceiling. Delegation intersects and never unions, here as
// everywhere else: a kind cannot grant authority its Box does not hold, which
// is what makes a maximal kind definition inert rather than dangerous.
func (c Capabilities) Intersect(ceiling Capabilities) Capabilities {
	return Capabilities{
		Preempt:    c.Preempt && ceiling.Preempt,
		BreakGlass: c.BreakGlass && ceiling.BreakGlass,
	}
}

// Exceeds names the capabilities c claims that the ceiling does not hold,
// sorted. Empty means the declaration fits.
//
// Reported rather than silently intersected at load. Silent narrowing is how
// an operator ends up believing something false about their factory — the
// declaration says the incident kind may break glass, the ceiling says it may
// not, and the only way to discover which won is to have an emergency.
// Both mechanisms are here and only one of them decides, deliberately. The
// intersection is the SEMANTIC — what a kind actually holds under a ceiling —
// and this reports the difference so the refusal can name it. Written in terms
// of Intersect so the two cannot drift: a capability added to one and not the
// other would be a grant that passes validation and then evaporates.
func (c Capabilities) Exceeds(ceiling Capabilities) []string {
	held := c.Intersect(ceiling)
	var out []string
	if c.Preempt && !held.Preempt {
		out = append(out, "preempt")
	}
	if c.BreakGlass && !held.BreakGlass {
		out = append(out, "break_glass")
	}
	return out
}

// String renders the granted set for evidence and for Explain.
func (c Capabilities) String() string {
	var on []string
	if c.Preempt {
		on = append(on, "preempt")
	}
	if c.BreakGlass {
		on = append(on, "break_glass")
	}
	if len(on) == 0 {
		return "(none)"
	}
	return strings.Join(on, ",")
}

// Escalation is how work opened under a kind asks for a person, and it is the
// home issue #4's mechanics never had: the queue could age an exception and
// tell somebody out of band, and the policy it measured against was one number
// for the whole factory.
type Escalation struct {
	// Severity is a FLOOR, not a default. The parking stage still states a
	// severity — a stage that would have forgotten a field cannot forget an
	// argument — and this raises it when the kind is louder than the stage
	// knew. An incident's leaf failing is not routine because the leaf did not
	// know it was an incident.
	//
	// Empty is no floor.
	Severity queue.Severity `yaml:"severity" json:"severity,omitempty"`
	// Objective is how long work of this kind may sit parked before somebody
	// is told. Zero defers to the room's own objective, which is the honest
	// default: a kind that has said nothing has not asked for a tighter
	// promise than the factory's.
	Objective time.Duration `yaml:"objective" json:"objective,omitempty"`
	// MaxReescalations bounds the tellings after the first. Zero defers to the
	// room's bound; negative means one telling and then silence.
	MaxReescalations int `yaml:"max_reescalations" json:"max_reescalations,omitempty"`
	// Route is an opaque label the notifier routes on — a rota, a channel, a
	// queue name. Opaque on purpose: the room decides somebody must be told
	// and knows nothing whatever about telling them.
	Route string `yaml:"route" json:"route,omitempty"`
}

// Floor raises a stated severity to this kind's floor, leaving a louder one
// alone.
func (e Escalation) Floor(stated queue.Severity) queue.Severity {
	if e.Severity == "" {
		return stated
	}
	if stated.AtLeast(e.Severity) {
		return stated
	}
	return e.Severity
}

// Step is one node of a declared sequence — the shape an incident has always
// described in prose. "Finishes when service is restored AND the postmortem
// has landed" is two nodes and an edge, and until there was a declaration
// there was nothing in the system that could hold it.
type Step struct {
	// Name is the node's id within the plan, and it is what a dependent step
	// is named by. It must be a plain token: it becomes part of a work item's
	// id.
	Name string `yaml:"name" json:"name"`
	// Task is what the step asks for.
	Task string `yaml:"task" json:"task"`
}

// Decomposition is the tree this origination constructs, and the decomposer
// that constructs it.
type Decomposition struct {
	// Decomposer names the producer. Empty means [DecomposeIdentity], because
	// the ordinary path must be the cheap one: intake runs this stage on every
	// ingress, and a stage on every intake is a stage on the critical path of
	// every intake.
	Decomposer string `yaml:"decomposer" json:"decomposer,omitempty"`
	// Steps are the sequence decomposer's parameters, in order. Each depends
	// on the one before it.
	Steps []Step `yaml:"steps" json:"steps,omitempty"`
	// Standing marks an origination that decomposes REPEATEDLY, driven by a
	// standing task rather than by intake — a reconciler rather than a
	// request. Intake decomposes once and never again, which is right for a
	// request and would freeze a reconciler at its first wave.
	//
	// This is as far as the re-planning question is answered here, and the
	// answer is deliberately the easy half: a standing origination OPENS A NEW
	// PLAN per wave, so its chain carries one plan per wave and every item
	// names the wave that called for it. Whether a plan whose facts went stale
	// should AMEND an in-flight graph is the hard half and is not answered —
	// amending means reconciling a graph against work already claimed under
	// the old one, and guessing at it here would put the guess on the record
	// as a decision.
	Standing bool `yaml:"standing" json:"standing,omitempty"`
}

// DecomposeIdentity and DecomposeSequence are the deterministic decomposers
// this package's seeds name. Others register themselves; see [plan.Registry].
const (
	// DecomposeIdentity emits a one-node graph: the ordinary issue.
	DecomposeIdentity = "identity"
	// DecomposeSequence emits the declaration's steps as a chain.
	DecomposeSequence = "sequence"
	// DecomposeCampaign emits a wave of a predicate's fan-out. Registered by
	// internal/campaign, which owns the estate read; named here so a seed can
	// reference it without this package importing the engine.
	DecomposeCampaign = "campaign"
)

// Completion is the predicate that says an origination is finished. It used to
// be a doc comment.
type Completion struct {
	// Graph requires every work item the plan produced to have settled done.
	Graph bool `yaml:"graph" json:"graph,omitempty"`
	// Facts names the origination facts that must be present and non-empty.
	// An incident declares restored and postmortem, and the rule that
	// restoration alone does not close it is finally a fact about the
	// declaration rather than about a Go function.
	Facts []string `yaml:"facts" json:"facts,omitempty"`
}

// Declaration is one governed kind.
//
// Five fields, because #17's data envelope is the fifth: what class of data
// this origination's work may touch and which endpoints may see it had a
// mechanism and no governed home, and this is the home.
type Declaration struct {
	Name         string        `yaml:"name" json:"name"`
	Capabilities Capabilities  `yaml:"capabilities" json:"capabilities,omitempty"`
	Escalation   Escalation    `yaml:"escalation" json:"escalation,omitempty"`
	Decompose    Decomposition `yaml:"decompose" json:"decompose,omitempty"`
	Completion   Completion    `yaml:"completion" json:"completion,omitempty"`
	// Envelope is the data handling work under this kind runs within. Nil is
	// undeclared, which a Box with a ceiling refuses — see [agent.Envelope].
	Envelope *agent.Envelope `yaml:"-" json:"envelope,omitempty"`
	// Source is where this declaration came from, and it is the same type the
	// agent plane uses for a layer for the same reason: capability fields come
	// from governed sources only, and [agent.Source.Governed] is already the
	// test. An inline declaration may describe how work decomposes and how it
	// escalates; it may not grant authority.
	Source agent.Source `yaml:"-" json:"-"`
}

// Decomposer resolves the declared decomposer, defaulting to identity.
func (d Declaration) Decomposer() string {
	if d.Decompose.Decomposer == "" {
		return DecomposeIdentity
	}
	return d.Decompose.Decomposer
}

// Validate checks a declaration against itself and against a Box's ceiling.
//
// Two conditions make data-driven capabilities safe, and this is both of them.
// Capability fields must come from a governed source — same rule and same
// reason as an inline base layer meaning org-wide safety rules nobody reviewed
// — and the Box's ceiling bounds what any kind under it can claim.
func (d Declaration) Validate(ceiling Capabilities) error {
	if d.Name == "" {
		return fmt.Errorf("%w: a declaration with no name names nothing", ErrDeclaration)
	}
	if strings.ContainsAny(d.Name, " \t/") {
		return fmt.Errorf("%w: kind %q — a name is a plain token; it rides ingress records and item ids", ErrDeclaration, d.Name)
	}
	if d.Capabilities.Any() && !d.Source.Governed() {
		return fmt.Errorf("%w: kind %q grants %s from an ungoverned source (%s) — a declaration that grants authority must be pinned to a repo and a commit, so a change to what an origination may do goes through the same review as a change to what the factory ships",
			ErrCapability, d.Name, d.Capabilities, sourceName(d.Source))
	}
	if over := d.Capabilities.Exceeds(ceiling); len(over) > 0 {
		return fmt.Errorf("%w: kind %q claims %s, and its box's ceiling holds %s — delegation intersects and never unions",
			ErrCapability, d.Name, strings.Join(over, ","), ceiling)
	}
	switch d.Decomposer() {
	case DecomposeSequence:
		if len(d.Decompose.Steps) == 0 {
			return fmt.Errorf("%w: kind %q decomposes by sequence and declares no steps — an empty sequence produces no work at all, which is the state this kind was already in",
				ErrDeclaration, d.Name)
		}
	default:
		if len(d.Decompose.Steps) > 0 {
			return fmt.Errorf("%w: kind %q declares steps and decomposes by %q, which does not read them — steps nobody reads are a plan an operator believes in and never gets",
				ErrDeclaration, d.Name, d.Decomposer())
		}
	}
	seen := map[string]bool{}
	for i, s := range d.Decompose.Steps {
		switch {
		case s.Name == "":
			return fmt.Errorf("%w: kind %q step %d has no name", ErrDeclaration, d.Name, i)
		case strings.ContainsAny(s.Name, " \t/"):
			return fmt.Errorf("%w: kind %q step %q — a step name is a plain token; it becomes part of a work item's id", ErrDeclaration, d.Name, s.Name)
		case seen[s.Name]:
			return fmt.Errorf("%w: kind %q declares step %q twice — two nodes would share an item id and the later would silently win", ErrDeclaration, d.Name, s.Name)
		case s.Task == "":
			return fmt.Errorf("%w: kind %q step %q asks for nothing", ErrDeclaration, d.Name, s.Name)
		}
		seen[s.Name] = true
	}
	if !d.Completion.Graph && len(d.Completion.Facts) == 0 {
		return fmt.Errorf("%w: kind %q declares no completion — an origination that can never be finished is an entry in the queue forever", ErrDeclaration, d.Name)
	}
	for _, f := range d.Completion.Facts {
		if f == "" {
			return fmt.Errorf("%w: kind %q requires an unnamed fact", ErrDeclaration, d.Name)
		}
	}
	return nil
}

func sourceName(s agent.Source) string {
	switch {
	case s.Kind == "":
		return "unsourced"
	case s.Path != "":
		return string(s.Kind) + " " + s.Path
	default:
		return string(s.Kind)
	}
}

// Set is the declarations one daemon knows, keyed by name.
type Set struct{ m map[string]Declaration }

// NewSet validates every declaration against the ceiling and indexes them.
//
// A later declaration REPLACES an earlier one of the same name rather than
// merging with it, and that is the answer to whether the three current kinds
// survive as seeds. They ship — a factory whose operator must author "issue"
// before submitting one is a factory nobody starts — but a Box redeclaring a
// seeded name gets its own declaration whole. Merging is how a default quietly
// becomes a standard nobody chose: a field the operator never wrote would go
// on granting whatever the seed granted, and the diff would show nothing.
func NewSet(ceiling Capabilities, decls ...Declaration) (*Set, error) {
	s := &Set{m: map[string]Declaration{}}
	for _, d := range decls {
		if err := d.Validate(ceiling); err != nil {
			return nil, err
		}
		s.m[d.Name] = d
	}
	return s, nil
}

// Lookup answers a name.
func (s *Set) Lookup(name string) (Declaration, error) {
	if s == nil {
		return Declaration{}, fmt.Errorf("%w: %q (no declarations are configured)", ErrUnknown, name)
	}
	d, ok := s.m[name]
	if !ok {
		return Declaration{}, fmt.Errorf("%w: %q (declared: %s)", ErrUnknown, name, strings.Join(s.Names(), ", "))
	}
	return d, nil
}

// Names lists the declared kinds, sorted.
func (s *Set) Names() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.m))
	for n := range s.m {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Sets is every box's declarations, keyed by box name.
//
// Per box rather than per daemon, because the CEILING is per box. One merged
// set would let a kind validated against a permissive box's ceiling be used by
// work opened in a restrictive one, which is precisely the delegation-unions
// mistake the ceiling exists to prevent — and it would be invisible, because
// the declaration would look identical from both sides.
type Sets map[string]*Set

// Seeded is the shipped declarations for each named box, under an EMPTY
// ceiling.
//
// The bootstrap posture: no capabilities anywhere, which is the only one that
// is safe with no configuration behind it. A factory assembled this way runs
// the whole loop and cannot preempt a lease or break glass, because nothing
// has said it may.
func Seeded(boxes ...string) Sets {
	out := Sets{}
	for _, b := range boxes {
		set, err := NewSet(Capabilities{}, Seeds()...)
		if err != nil {
			panic("kind: the seeded declarations do not validate: " + err.Error())
		}
		out[b] = set
	}
	return out
}

// Lookup answers a kind name within a box.
func (s Sets) Lookup(box, name string) (Declaration, error) {
	set, ok := s[box]
	if !ok {
		return Declaration{}, fmt.Errorf("%w: box %q declares none", ErrUnknown, box)
	}
	d, err := set.Lookup(name)
	if err != nil {
		return Declaration{}, fmt.Errorf("%w (box %s)", err, box)
	}
	return d, nil
}

// Seed names are the three the system ships with.
const (
	SeedIssue    = "issue"
	SeedCampaign = "campaign"
	SeedIncident = "incident"
)

// Seeds are the declarations that ship, and they are deliberately the three
// kinds that used to be Go constants — same behaviour, now visible.
//
// Their source is inline, which means none of them grants a capability. That
// is not a limitation working around the governance rule; it is the rule
// working. Shipping an incident that preempts by default would put the one
// dangerous authority in the system into a file nobody in the operator's
// organisation ever reviewed. An operator who wants a preempting incident pins
// their own declaration to a commit, and the grant is a reviewable change.
func Seeds() []Declaration {
	inline := func(name string) agent.Source {
		return agent.Source{Kind: agent.SourceInline, Digest: "kind-seed-" + name}
	}
	return []Declaration{
		{
			Name:       SeedIssue,
			Source:     inline(SeedIssue),
			Decompose:  Decomposition{Decomposer: DecomposeIdentity},
			Completion: Completion{Graph: true},
		},
		{
			Name:   SeedCampaign,
			Source: inline(SeedCampaign),
			// Standing, because a campaign is a reconciler: it is never
			// enumerated up front, it generates work for repositories that do
			// not yet satisfy its predicate, and a repository added tomorrow
			// reopens it. Intake decomposing it once would be the enumeration
			// the design refuses.
			Decompose:  Decomposition{Decomposer: DecomposeCampaign, Standing: true},
			Completion: Completion{Graph: true},
		},
		{
			Name:   SeedIncident,
			Source: inline(SeedIncident),
			Escalation: Escalation{
				Severity:  queue.SeverityHigh,
				Objective: 30 * time.Minute,
			},
			Decompose: Decomposition{Decomposer: DecomposeSequence, Steps: []Step{
				{Name: "restore", Task: "restore service"},
				{Name: "postmortem", Task: "write and land the postmortem"},
			}},
			// Both facts, and the graph. Restoration alone does not close an
			// incident, and neither does landing two commits: the facts are
			// what a person asserts about the world, the graph is what the
			// factory did about it, and an incident wants both.
			Completion: Completion{Graph: true, Facts: []string{FactRestored, FactPostmortem}},
		},
	}
}

// FactRestored and FactPostmortem are the incident seed's completion facts.
// Exported because the assertion path needs to name them and a second spelling
// would be an incident that can never close.
const (
	FactRestored   = "restored"
	FactPostmortem = "postmortem"
)
