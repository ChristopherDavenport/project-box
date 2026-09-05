package acp

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/seat"
)

// plannerRig is a planner over the rig's repository with two sections, and an
// origination to plan.
func plannerRig(t *testing.T, scenario string) (*Planner, ingress.Ingress, *seat.Seat) {
	t.Helper()
	s, lp, router, _, _ := rig(t, scenario)
	repo := lp.Repo
	repo.Sections = []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}, Check: "go test ./internal/api/..."},
		{Name: "web", Purpose: "the room", Paths: []string{"web"}},
	}
	st := &seat.Seat{Name: "mason-planner", Base: &knowledge.Base{Store: s, Tenant: btn}}
	p := &Planner{
		Store: s, Tenant: btn, Box: "shop",
		Command:  []string{os.Args[0]},
		Env:      []string{"ACP_FAKE=" + scenario},
		Composer: &agent.Composer{Source: layers()},
		Seat:     st, Mirror: lp.Mirror, Repos: []forge.Repo{repo},
		Root: t.TempDir(),
	}
	ing, _, err := router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-widgets", Payload: []byte("add widgets to the room")})
	if err != nil {
		t.Fatal(err)
	}
	return p, ing, st
}

func plannerDecl() kind.Declaration {
	return kind.Declaration{Name: kind.SeedIssue, Completion: kind.Completion{Graph: true},
		Decompose: kind.Decomposition{Decomposer: PlannerName}}
}

// TestThePlannerAnswersInUnits: one origination becomes a graph of units —
// each with its task, its sections, what it waits on, and its tier — and the
// judgement is on the origination's chain under an agentref like any run.
func TestThePlannerAnswersInUnits(t *testing.T) {
	p, ing, st := plannerRig(t, "planner")
	ctx := context.Background()
	res, err := p.Decompose(ctx, ing, plannerDecl())
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	if len(res.Nodes) != 2 {
		t.Fatalf("nodes: %+v", res.Nodes)
	}
	api, view := res.Nodes[0], res.Nodes[1]
	if api.ID != "api-endpoint" || api.Sections[0] != "api" || api.Model != "haiku" || !strings.Contains(api.Task, `{"widgets":[]}`) {
		t.Errorf("the endpoint unit lost something: %+v", api)
	}
	if view.ID != "room-view" || view.Sections[0] != "web" || len(view.DependsOn) != 1 || view.DependsOn[0] != "api-endpoint" {
		t.Errorf("the view unit lost its dependency or section: %+v", view)
	}
	if res.AgentRef == "" {
		t.Error("a judgement with no agentref")
	}
	if len(res.Facts) != 1 || res.Facts[0].Key != "repo/app/head" || len(res.Facts[0].Value) != 40 {
		t.Errorf("the plan does not record the head it was judged over: %+v", res.Facts)
	}
	// On the origination's chain: the run, the session, the result, the
	// text — the same account a build leaves, under the planner role.
	kinds := map[string]string{}
	for r, err := range p.Store.Log().Since(ctx, btn, ingress.Scope(ing.ID), 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[r.Kind] = string(r.Body)
	}
	for _, want := range []string{"agent-run", "agent-prompt", "agent-session", "agent-result", "agent-text"} {
		if _, ok := kinds[want]; !ok {
			t.Errorf("no %s on the origination's chain (have %v)", want, keysOf(kinds))
		}
	}
	if !strings.Contains(kinds["agent-run"], `"role":"planner"`) || !strings.Contains(kinds["agent-run"], `"mode":"plan"`) {
		t.Errorf("the planner ran as something else:\n%s", kinds["agent-run"])
	}
	if !strings.Contains(kinds["agent-prompt"], "## Sections of app") || !strings.Contains(kinds["agent-prompt"], "the HTTP surface") {
		t.Errorf("the planner was not shown the sections:\n%s", kinds["agent-prompt"])
	}
	// Continuity: what this plan learned wakes the next one.
	wake, ok, err := st.WakeLayer(ctx, 5)
	if err != nil || !ok || !strings.Contains(wake.Instructions, "widgets live under internal/api") {
		t.Fatalf("the planner's handoff did not reach its seat: ok=%v err=%v %q", ok, err, wake.Instructions)
	}
}

// TestThePlanSealsAndBecomesItems runs the planner through the plan package:
// registered by box, opted into by the declaration, sealed as a judgement
// with its author, and turned into items that carry where and on what.
func TestThePlanSealsAndBecomesItems(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner")
	ps := &Planners{}
	ps.Set("shop", p)
	reg, err := plan.NewRegistry(plan.Identity{}, ps)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := plan.Make(context.Background(), reg, ing, plannerDecl())
	if err != nil {
		t.Fatalf("make: %v", err)
	}
	if pl.Replayable || pl.AgentRef == "" || pl.Decomposer != PlannerName {
		t.Fatalf("the plan misdescribes its author: %+v", pl)
	}
	items := plan.Items(ing, pl)
	if len(items) != 2 || items[0].Sections[0] != "api" || items[0].Model != "haiku" {
		t.Fatalf("items lost where or on what: %+v", items)
	}
	if items[1].DependsOn[0] != pl.ItemID("api-endpoint") {
		t.Fatalf("the dependency did not become an item edge: %+v", items[1])
	}
	// The same plan over the same repository is the same plan; a box with no
	// planner is refused, not planned by nobody.
	if _, err := (&Planners{}).Decompose(context.Background(), ing, plannerDecl()); err == nil || !strings.Contains(err.Error(), "no agent") {
		t.Fatalf("a box without a planner was not refused: %v", err)
	}
}

// TestAPlanNamingAnUndeclaredSectionIsRefused: the plan and the repository
// disagree about where a unit works, and the refusal names both sides.
func TestAPlanNamingAnUndeclaredSectionIsRefused(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner-bad")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	if err == nil || !strings.Contains(err.Error(), `"db"`) || !strings.Contains(err.Error(), "api") {
		t.Fatalf("an undeclared section was not refused with both sides named: %v", err)
	}
}

// TestThePlannerMayEscalate: a question, a decision, or work already done is
// a refusal in the agent's own words, attributed to its seat.
func TestThePlannerMayEscalate(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner-escalate")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	var esc *loop.Escalation
	if !errors.As(err, &esc) || esc.Seat != "mason-planner" || !strings.Contains(esc.Reason, "question") {
		t.Fatalf("escalation not carried: %v", err)
	}
}

// TestAnAnswerWithNoPlanIsAnError: silence is not an identity plan.
func TestAnAnswerWithNoPlanIsAnError(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner-noplan")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	if err == nil || !strings.Contains(err.Error(), "no plan") {
		t.Fatalf("an answer with no plan was accepted: %v", err)
	}
}

// TestLastObjectIsStringAware: a brace inside a task's prose must not end
// the object early.
func TestLastObjectIsStringAware(t *testing.T) {
	text := `thinking {not json} then ` + "```json\n" + `{"units":[{"id":"a","task":"print {\"x\":1} and } stop","sections":[]}]}` + "\n```"
	obj, ok := lastObjectWith(text, `"units"`)
	if !ok || !strings.HasSuffix(obj, `"sections":[]}]}`) {
		t.Fatalf("object mis-bounded: ok=%v %q", ok, obj)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestThePlannerSaysWhatItIsDoing: a judgement is a run like any other and
// the room should be able to watch it. The hook fires as the builder's does
// — the start first, then the stream — tagged with the plan's run, which is
// the id the live bus and the run log file it under.
func TestThePlannerSaysWhatItIsDoing(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner")
	var mu sync.Mutex
	var runs []string
	var got []Update
	p.Progress = func(_ context.Context, run string, u Update) {
		mu.Lock()
		defer mu.Unlock()
		runs = append(runs, run)
		got = append(got, u)
	}
	if _, err := p.Decompose(context.Background(), ing, plannerDecl()); err != nil {
		t.Fatalf("decompose: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 || got[0].Kind != UpdateStarted {
		t.Fatalf("the stream did not begin with the run starting: %+v", got)
	}
	for i, run := range runs {
		if run != "plan-"+ing.ID {
			t.Errorf("frame %d tagged %q, want plan-%s", i, run, ing.ID)
		}
	}
}
