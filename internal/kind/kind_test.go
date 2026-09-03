package kind

import (
	"errors"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

func pinned(name string) agent.Source {
	return agent.Source{Kind: agent.SourceGit, Repo: "https://example.test/policy.git",
		Commit: "5f2c8e7d1a3b4c6d8e0f1a2b3c4d5e6f70819293", Path: "kinds/" + name + ".yaml", Digest: "tree-" + name}
}

// TestTheSeedsAreInert is the property that makes shipping seeds defensible at
// all. They exist so a factory works out of the box; they must not be how an
// authority nobody reviewed arrives with it.
func TestTheSeedsAreInert(t *testing.T) {
	set, err := NewSet(Capabilities{}, Seeds()...)
	if err != nil {
		t.Fatalf("the seeds do not validate under an empty ceiling: %v", err)
	}
	for _, name := range set.Names() {
		d, err := set.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		if d.Capabilities.Any() {
			t.Fatalf("seed %q ships with %s — a shipped grant is a grant nobody in the operator's organisation reviewed", name, d.Capabilities)
		}
		if d.Source.Governed() {
			t.Fatalf("seed %q claims a governed source it does not have", name)
		}
	}
	if len(set.Names()) != 3 {
		t.Fatalf("seeds: %v", set.Names())
	}
}

// TestTheIncidentSeedHoldsItsOwnSentence. "It finishes when service is
// restored AND the postmortem has landed" was a doc comment for the whole life
// of the closed enum, and there was nothing in the system that could represent
// it. Now it is two nodes, an edge, and two required facts.
func TestTheIncidentSeedHoldsItsOwnSentence(t *testing.T) {
	set, err := NewSet(Capabilities{}, Seeds()...)
	if err != nil {
		t.Fatal(err)
	}
	d, err := set.Lookup(SeedIncident)
	if err != nil {
		t.Fatal(err)
	}
	if d.Decomposer() != DecomposeSequence || len(d.Decompose.Steps) != 2 {
		t.Fatalf("incident does not decompose into a sequence: %+v", d.Decompose)
	}
	if d.Decompose.Steps[0].Name != "restore" || d.Decompose.Steps[1].Name != "postmortem" {
		t.Fatalf("steps: %+v", d.Decompose.Steps)
	}
	if !d.Completion.Graph || len(d.Completion.Facts) != 2 {
		t.Fatalf("completion: %+v", d.Completion)
	}
	if d.Escalation.Floor(queue.SeverityLow) != queue.SeverityHigh {
		t.Fatal("an incident's parked leaf can still reach the queue as routine")
	}
	if d.Escalation.Floor(queue.SeverityCritical) != queue.SeverityCritical {
		t.Fatal("the floor lowered a louder severity")
	}
}

// TestADeclarationReplacesASeedWhole is the answer to whether the three
// current kinds survive as seeds. They ship, and a Box redeclaring a seeded
// name gets its own declaration entire — merging is how a default quietly
// becomes a standard nobody chose, because a field the operator never wrote
// would go on granting whatever the seed granted with the diff showing
// nothing.
func TestADeclarationReplacesASeedWhole(t *testing.T) {
	own := Declaration{
		Name:       SeedIncident,
		Source:     pinned(SeedIncident),
		Decompose:  Decomposition{Decomposer: DecomposeIdentity},
		Completion: Completion{Graph: true},
	}
	set, err := NewSet(Capabilities{}, append(Seeds(), own)...)
	if err != nil {
		t.Fatal(err)
	}
	d, err := set.Lookup(SeedIncident)
	if err != nil {
		t.Fatal(err)
	}
	if d.Decomposer() != DecomposeIdentity {
		t.Fatalf("the seed's decomposer survived: %s", d.Decomposer())
	}
	if len(d.Completion.Facts) != 0 {
		t.Fatalf("the seed's completion facts leaked into a declaration that did not ask for them: %v", d.Completion.Facts)
	}
	if d.Escalation.Severity != "" {
		t.Fatalf("the seed's severity floor leaked in: %q", d.Escalation.Severity)
	}
}

func TestDeclarationsThatDoNotHoldTogether(t *testing.T) {
	base := func() Declaration {
		return Declaration{Name: "thing", Source: pinned("thing"), Completion: Completion{Graph: true}}
	}
	cases := []struct {
		name string
		make func() Declaration
	}{
		{"no name", func() Declaration { d := base(); d.Name = ""; return d }},
		{"name with a slash", func() Declaration { d := base(); d.Name = "a/b"; return d }},
		{"sequence with no steps", func() Declaration {
			d := base()
			d.Decompose = Decomposition{Decomposer: DecomposeSequence}
			return d
		}},
		{"steps nobody reads", func() Declaration {
			d := base()
			d.Decompose = Decomposition{Decomposer: DecomposeIdentity, Steps: []Step{{Name: "a", Task: "do a"}}}
			return d
		}},
		{"a step twice", func() Declaration {
			d := base()
			d.Decompose = Decomposition{Decomposer: DecomposeSequence, Steps: []Step{
				{Name: "a", Task: "do a"}, {Name: "a", Task: "do a again"}}}
			return d
		}},
		{"a step asking for nothing", func() Declaration {
			d := base()
			d.Decompose = Decomposition{Decomposer: DecomposeSequence, Steps: []Step{{Name: "a"}}}
			return d
		}},
		{"no completion at all", func() Declaration { d := base(); d.Completion = Completion{}; return d }},
		{"an unnamed required fact", func() Declaration {
			d := base()
			d.Completion = Completion{Facts: []string{""}}
			return d
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.make().Validate(Capabilities{}); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// TestASetIsPerBoxBecauseTheCeilingIs. One merged set would let a declaration
// validated against a permissive box's ceiling be used by work opened in a
// restrictive one, and it would be invisible: the declaration looks identical
// from both sides.
func TestASetIsPerBoxBecauseTheCeilingIs(t *testing.T) {
	loud := Declaration{Name: "urgent", Source: pinned("urgent"),
		Capabilities: Capabilities{BreakGlass: true}, Completion: Completion{Graph: true}}
	permissive, err := NewSet(Capabilities{BreakGlass: true}, append(Seeds(), loud)...)
	if err != nil {
		t.Fatal(err)
	}
	strict, err := NewSet(Capabilities{}, Seeds()...)
	if err != nil {
		t.Fatal(err)
	}
	sets := Sets{"ops": permissive, "shop": strict}
	if d, err := sets.Lookup("ops", "urgent"); err != nil || !d.Capabilities.BreakGlass {
		t.Fatalf("ops lost its grant: %+v %v", d, err)
	}
	if _, err := sets.Lookup("shop", "urgent"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("a strict box saw a permissive box's kind: %v", err)
	}
	if _, err := sets.Lookup("nowhere", SeedIssue); !errors.Is(err, ErrUnknown) {
		t.Fatalf("an undeclared box answered: %v", err)
	}
	// And the strict box cannot acquire it by declaring it: the ceiling
	// refuses at load rather than narrowing in silence.
	if _, err := NewSet(Capabilities{}, loud); !errors.Is(err, ErrCapability) {
		t.Fatalf("a strict box declared a grant it does not hold: %v", err)
	}
}

func TestEscalationDefersWhereItSaysNothing(t *testing.T) {
	var e Escalation
	if e.Floor(queue.SeverityLow) != queue.SeverityLow {
		t.Fatal("an unstated floor raised a severity")
	}
	if e.Objective != 0 || e.MaxReescalations != 0 {
		t.Fatal("an unstated escalation is not zero")
	}
	e = Escalation{Objective: 30 * time.Minute}
	if e.Objective != 30*time.Minute {
		t.Fatal("objective")
	}
}
