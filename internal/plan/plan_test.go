package plan

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func issue(id string) ingress.Ingress {
	return ingress.Ingress{ID: id, Kind: kind.SeedIssue, Tenant: tn, Box: "shop", Repo: "app",
		Requester: "chris", Note: "fix the thing", Title: "the thing is broken"}
}

func seed(t *testing.T, name string) kind.Declaration {
	t.Helper()
	for _, d := range kind.Seeds() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no seed %q", name)
	return kind.Declaration{}
}

// TestTheRefIsOverTheGraphNotTheListing. Render determinism gives a plan diff
// its meaning: the same declaration and the same facts must produce an
// identical graph, or a diff is just noise, and a diff that is mostly noise is
// one nobody reads.
func TestTheRefIsOverTheGraphNotTheListing(t *testing.T) {
	a := Plan{Ingress: "ing-1", Kind: "issue", Decomposer: "x", Replayable: true,
		Nodes: []Node{
			{ID: "one", Task: "do one"},
			{ID: "two", Task: "do two", DependsOn: []string{"one"}},
			{ID: "three", Task: "do three", DependsOn: []string{"two", "one"}},
		},
		Facts: []Fact{{Key: "b", Value: "2", AsOf: time.Now()}, {Key: "a", Value: "1", AsOf: time.Now()}},
	}
	b := Plan{Ingress: "ing-1", Kind: "issue", Decomposer: "x", Replayable: true,
		Nodes: []Node{
			{ID: "three", Task: "do three", DependsOn: []string{"one", "two"}},
			{ID: "one", Task: "do one"},
			{ID: "two", Task: "do two", DependsOn: []string{"one"}},
		},
		// Same facts, read an hour later. The as-of is provenance and must not
		// be identity: hashing it would make every replay a different plan and
		// leave the staleness mechanism unable to tell "the facts moved" from
		// "time passed".
		Facts: []Fact{{Key: "a", Value: "1", AsOf: time.Now().Add(time.Hour)}, {Key: "b", Value: "2"}},
	}
	if err := a.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	if a.Ref != b.Ref {
		t.Fatalf("the same graph produced two refs:\n%s\n%s", a.Ref, b.Ref)
	}
	// And a graph that differs anywhere that matters produces a different ref.
	for _, change := range []func(p *Plan){
		func(p *Plan) { p.Nodes[0].Task = "do one differently" },
		func(p *Plan) { p.Nodes[1].DependsOn = nil },
		func(p *Plan) { p.Nodes = append(p.Nodes, Node{ID: "four", Task: "do four"}) },
		func(p *Plan) { p.Facts[0].Value = "3" },
		func(p *Plan) { p.Decomposer = "y" },
		func(p *Plan) { p.Nodes[0].Repo = "elsewhere" },
		func(p *Plan) { p.Nodes[0].Attempts = 3 },
	} {
		c := Plan{Ingress: a.Ingress, Kind: a.Kind, Decomposer: a.Decomposer, Replayable: true,
			Nodes: append([]Node(nil), a.Nodes...), Facts: append([]Fact(nil), a.Facts...)}
		c.Nodes = append([]Node(nil), c.Nodes...)
		change(&c)
		if err := c.Seal(); err != nil {
			t.Fatal(err)
		}
		if c.Ref == a.Ref {
			t.Fatal("a changed graph kept its ref")
		}
	}
}

func TestGraphsTheQueueCouldNotRun(t *testing.T) {
	cases := []struct {
		name string
		p    Plan
		want string
	}{
		{"no nodes", Plan{Ingress: "i", Replayable: true}, "decomposed to nothing"},
		{"no ingress", Plan{Replayable: true, Nodes: []Node{{ID: "a", Task: "t"}}}, "orphan work"},
		{"unnamed node", Plan{Ingress: "i", Replayable: true, Nodes: []Node{{Task: "t"}}}, "unnamed node"},
		{"node id with a slash", Plan{Ingress: "i", Replayable: true, Nodes: []Node{{ID: "a/b", Task: "t"}}}, "plain token"},
		{"node asking for nothing", Plan{Ingress: "i", Replayable: true, Nodes: []Node{{ID: "a"}}}, "asks for nothing"},
		{"duplicate node", Plan{Ingress: "i", Replayable: true, Nodes: []Node{
			{ID: "a", Task: "t"}, {ID: "a", Task: "u"}}}, "twice"},
		{"dangling dependency", Plan{Ingress: "i", Replayable: true, Nodes: []Node{
			{ID: "a", Task: "t", DependsOn: []string{"ghost"}}}}, "does not contain"},
		{"self dependency", Plan{Ingress: "i", Replayable: true, Nodes: []Node{
			{ID: "a", Task: "t", DependsOn: []string{"a"}}}}, "depends on itself"},
		{"a cycle", Plan{Ingress: "i", Replayable: true, Nodes: []Node{
			{ID: "a", Task: "t", DependsOn: []string{"c"}},
			{ID: "b", Task: "t", DependsOn: []string{"a"}},
			{ID: "c", Task: "t", DependsOn: []string{"b"}}}}, "cycle"},
		{"judgement with no author", Plan{Ingress: "i", Nodes: []Node{{ID: "a", Task: "t"}}}, "no author"},
		{"negative attempts", Plan{Ingress: "i", Replayable: true, Nodes: []Node{
			{ID: "a", Task: "t", Attempts: -1}}}, "negative budget"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Seal()
			if err == nil {
				t.Fatal("accepted")
			}
			if !errors.Is(err, ErrPlan) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("wrong refusal: %v", err)
			}
		})
	}
}

// TestAnOrdinaryIssueIsNotRenamedByTheStage. The decomposition stage arrives on
// top of a running factory, and `wi-<ingress>` is on live chains, in evidence,
// and in ids an operator has already read out of the control room.
func TestAnOrdinaryIssueIsNotRenamedByTheStage(t *testing.T) {
	p, err := Make(context.Background(), Default(), issue("ing-1"), seed(t, kind.SeedIssue))
	if err != nil {
		t.Fatal(err)
	}
	if p.FanOut() != 1 {
		t.Fatalf("fan-out: %d", p.FanOut())
	}
	if got := p.ItemID(IdentityNode); got != "wi-ing-1" {
		t.Fatalf("item id: %s", got)
	}
	if p.Nodes[0].Task != "fix the thing" {
		t.Fatalf("task: %q", p.Nodes[0].Task)
	}
	// A wave of one does NOT collapse to the bare ingress id, or the second
	// wave of one would collide with the first and the queue would report new
	// work as already done.
	wave := Plan{Ingress: "ing-1", Replayable: true, Nodes: []Node{{ID: "local-acme-r1", Task: "bump"}}}
	if err := wave.Seal(); err != nil {
		t.Fatal(err)
	}
	if got := wave.ItemID("local-acme-r1"); got != "wi-ing-1-local-acme-r1" {
		t.Fatalf("a wave of one collapsed to %s", got)
	}
}

// TestAnIncidentEmitsTheEdgeItAlwaysDescribed.
func TestAnIncidentEmitsTheEdgeItAlwaysDescribed(t *testing.T) {
	ing := issue("ing-inc")
	ing.Kind = kind.SeedIncident
	p, err := Make(context.Background(), Default(), ing, seed(t, kind.SeedIncident))
	if err != nil {
		t.Fatal(err)
	}
	if p.FanOut() != 2 {
		t.Fatalf("fan-out: %d", p.FanOut())
	}
	post, ok := p.Node("postmortem")
	if !ok || len(post.DependsOn) != 1 || post.DependsOn[0] != "restore" {
		t.Fatalf("the postmortem does not follow restoration: %+v", post)
	}
	restore, _ := p.Node("restore")
	if len(restore.DependsOn) != 0 {
		t.Fatalf("restoration waits on something: %+v", restore)
	}
	// The step task names the origination by TITLE and never by note. The note
	// is the delivery's payload, and a declared step is exactly where a
	// reference-only envelope would be smuggled past — the text is being
	// assembled by the factory rather than passed through, so it would look
	// like helpfulness.
	if strings.Contains(post.Task, ing.Note) {
		t.Fatalf("a declared step carried the payload: %q", post.Task)
	}
	if !strings.Contains(post.Task, ing.Title) {
		t.Fatalf("a declared step names no origination: %q", post.Task)
	}
	items := Items(ing, p)
	if len(items) != 2 {
		t.Fatalf("items: %d", len(items))
	}
	var found bool
	for _, it := range items {
		if it.Node != "postmortem" {
			continue
		}
		found = true
		if len(it.DependsOn) != 1 || it.DependsOn[0] != p.ItemID("restore") {
			t.Fatalf("the edge did not survive the translation to item ids: %v", it.DependsOn)
		}
		if it.Plan != p.Ref {
			t.Fatalf("the item does not name its plan: %q", it.Plan)
		}
		if it.Kind != kind.SeedIncident {
			t.Fatalf("the item does not name its kind: %q", it.Kind)
		}
	}
	if !found {
		t.Fatal("no postmortem item")
	}
}

// judge is a decomposer that is honest about being one.
type judge struct{ ref string }

func (judge) Name() string     { return kind.DecomposeIdentity }
func (judge) Replayable() bool { return false }
func (j judge) Decompose(context.Context, ingress.Ingress, kind.Declaration) (Result, error) {
	return Result{Nodes: []Node{{ID: "guess", Task: "whatever seemed right"}}, AgentRef: j.ref}, nil
}

// TestTheDefaultPathCannotBecomeAJudgement is #5's constraint in code, before
// there is a decomposer to tempt anyone. A stage on every intake is a stage on
// the critical path of every intake: if the ordinary path needed an LLM call,
// work ADMISSION would depend on an agent being available.
func TestTheDefaultPathCannotBecomeAJudgement(t *testing.T) {
	reg, err := NewRegistry(judge{ref: "agentref-abc"}, Sequence{})
	if err != nil {
		t.Fatal(err)
	}
	// A declaration that says nothing about decomposition gets the identity
	// name — and this registry has put a judgement behind it.
	silent := kind.Declaration{Name: "issue", Completion: kind.Completion{Graph: true}}
	if _, err := Make(context.Background(), reg, issue("ing-1"), silent); err == nil {
		t.Fatal("the default intake path accepted a judgement planner")
	} else if !strings.Contains(err.Error(), "deterministic and agent-free") {
		t.Fatalf("wrong refusal: %v", err)
	}
	// Naming it is the opt-in, and then it works — and the plan says whose
	// judgement it was.
	named := kind.Declaration{Name: "issue", Completion: kind.Completion{Graph: true},
		Decompose: kind.Decomposition{Decomposer: kind.DecomposeIdentity}}
	p, err := Make(context.Background(), reg, issue("ing-1"), named)
	if err != nil {
		t.Fatal(err)
	}
	if p.Replayable || p.AgentRef != "agentref-abc" {
		t.Fatalf("a judgement plan hid its author: %+v", p)
	}
}

// TestTheShippedDecomposersAreDeterministic is the structural half of the same
// rule: nothing in the default registry can reach a model.
func TestTheShippedDecomposersAreDeterministic(t *testing.T) {
	reg := Default()
	if len(reg.Names()) == 0 {
		t.Fatal("the default registry is empty")
	}
	for _, name := range reg.Names() {
		d, err := reg.Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Replayable() {
			t.Fatalf("%s is not replayable", name)
		}
	}
	if _, err := reg.Resolve(kind.DecomposeCampaign); !errors.Is(err, ErrNoDecomposer) {
		t.Fatalf("the default registry answers for the campaign engine: %v", err)
	}
}

// liar claims determinism and names an author.
type liar struct{}

func (liar) Name() string     { return "liar" }
func (liar) Replayable() bool { return true }
func (liar) Decompose(context.Context, ingress.Ingress, kind.Declaration) (Result, error) {
	return Result{Nodes: []Node{{ID: "a", Task: "t"}}, AgentRef: "agentref-xyz"}, nil
}

func TestADeterministicDecomposerMayNotNameAnAuthor(t *testing.T) {
	reg, err := NewRegistry(liar{})
	if err != nil {
		t.Fatal(err)
	}
	decl := kind.Declaration{Name: "x", Completion: kind.Completion{Graph: true},
		Decompose: kind.Decomposition{Decomposer: "liar"}}
	if _, err := Make(context.Background(), reg, issue("ing-1"), decl); err == nil {
		t.Fatal("accepted a decomposer that is lying about what it is")
	}
}

func TestADecomposerNameHasOneProducer(t *testing.T) {
	if _, err := NewRegistry(Identity{}, Identity{}); err == nil {
		t.Fatal("two producers registered under one name — which graph an ingress got would depend on assembly order")
	}
	if _, err := Default().Resolve("nothing-registered-here"); !errors.Is(err, ErrNoDecomposer) {
		t.Fatalf("wrong error: %v", err)
	}
}
