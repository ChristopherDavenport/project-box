package acp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
	// The session record names its own role and carries the session id — a
	// continuation looking for the newest BUILDER session must be able to
	// tell a planner's record apart from one, even though both ride the same
	// sessionRecord shape.
	if !strings.Contains(kinds["agent-session"], `"session":"sess-1"`) || !strings.Contains(kinds["agent-session"], `"role":"planner"`) {
		t.Errorf("the session record does not carry its id and role:\n%s", kinds["agent-session"])
	}
	// Continuity: what this plan learned wakes the next one.
	wake, ok, err := st.WakeLayer(ctx, 5)
	if err != nil || !ok || !strings.Contains(wake.Instructions, "widgets live under internal/api") {
		t.Fatalf("the planner's handoff did not reach its seat: ok=%v err=%v %q", ok, err, wake.Instructions)
	}
}

// TestThePlannerSessionRecordCarriesEnvNamesNoValues: the planner writes the
// same sessionRecord shape a build does, from a separate call site — a field
// set for the builder alone would make a planner's record read "unknown".
// Not t.Parallel: t.Setenv forbids it.
func TestThePlannerSessionRecordCarriesEnvNamesNoValues(t *testing.T) {
	t.Setenv("ACP_ENV_CANARY", "super-secret-value")
	p, ing, _ := plannerRig(t, "planner")
	p.EnvNames = []string{"ACP_ENV_CANARY"}
	ctx := context.Background()
	if _, err := p.Decompose(ctx, ing, plannerDecl()); err != nil {
		t.Fatalf("decompose: %v", err)
	}
	var session string
	for r, err := range p.Store.Log().Since(ctx, btn, ingress.Scope(ing.ID), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-session" {
			session = string(r.Body)
		}
	}
	if session == "" {
		t.Fatal("no agent-session on the chain")
	}
	if strings.Contains(session, "super-secret-value") {
		t.Fatalf("a credential value reached the evidence chain:\n%s", session)
	}
	var rec struct {
		Env []string `json:"env"`
	}
	if err := json.Unmarshal([]byte(session), &rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rec.Env, ","), "ACP_ENV_CANARY") {
		t.Fatalf("the delivered name is not on the record: %v", rec.Env)
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

// TestAPlannerThatNeverAnsweredIsErrNoAnswer: a transport refusal on
// session/prompt — a session-limit rejection, a mid-response server error —
// is a run nobody judged, not a plan the box should park as an exception.
// pump.intakeOne (internal/pump) leans on errors.Is(err, plan.ErrNoAnswer) to
// tell the two apart, so the rpc message must survive into the error text
// for a plan-failed reason to quote it.
func TestAPlannerThatNeverAnsweredIsErrNoAnswer(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner-noanswer")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	if err == nil || !errors.Is(err, plan.ErrNoAnswer) {
		t.Fatalf("a run that never answered was not reported as plan.ErrNoAnswer: %v", err)
	}
	if !strings.Contains(err.Error(), "session limit") {
		t.Fatalf("the rpc message did not survive into the error text: %v", err)
	}
	var esc *loop.Escalation
	if errors.As(err, &esc) {
		t.Fatalf("a never-answered run was mistaken for an escalation: %v", err)
	}
	// The adapter's own session-limit wording, met on session/prompt itself
	// rather than on a usage_update — no RateLimit report ever arrived, so
	// the window it names and the reset it names are both unknown, and
	// unknown never reads as zero (issue #112).
	var spent *loop.WindowSpent
	if !errors.As(err, &spent) {
		t.Fatalf("a session-limit refusal was not classified as *loop.WindowSpent: %v", err)
	}
	if spent.Window != "" || !spent.ResetsAt.IsZero() {
		t.Fatalf("a refusal naming no reset must not invent one: %+v", spent)
	}
}

// TestAPlannerWindowSpentRefusalCarriesTheReset: a run's own rejected
// RateLimit report, met mid-run rather than at session/prompt, still
// classifies as *loop.WindowSpent and still satisfies plan.ErrNoAnswer — the
// window and the reset it named both survive so pump.dispatchJudgement can
// defer to it instead of failing the origination outright (issue #112).
func TestAPlannerWindowSpentRefusalCarriesTheReset(t *testing.T) {
	p, ing, _ := plannerRig(t, "windowspent")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	if err == nil || !errors.Is(err, plan.ErrNoAnswer) {
		t.Fatalf("a window-spent refusal is still a run nobody judged: %v", err)
	}
	var spent *loop.WindowSpent
	if !errors.As(err, &spent) {
		t.Fatalf("a rejected window was not classified as *loop.WindowSpent: %v", err)
	}
	if spent.Window != "five_hour" {
		t.Fatalf("the window name did not survive: %+v", spent)
	}
	if want := time.Unix(1788491400, 0); !spent.ResetsAt.Equal(want) {
		t.Fatalf("the reset did not survive: got %v want %v", spent.ResetsAt, want)
	}
}

// TestAPlannerEscalationIsNotErrNoAnswer: an escalation is an answer — the
// agent said its piece — so it must not also satisfy plan.ErrNoAnswer, or
// the loop side would retry and defer a run that already told us why it
// stopped.
func TestAPlannerEscalationIsNotErrNoAnswer(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner-escalate")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	var esc *loop.Escalation
	if !errors.As(err, &esc) {
		t.Fatalf("escalation not carried: %v", err)
	}
	if errors.Is(err, plan.ErrNoAnswer) {
		t.Fatalf("an escalation was mistaken for a run that never answered: %v", err)
	}
}

// TestAMalformedPlanIsNotErrNoAnswer: a plan that fails to parse is an
// answered run — the agent replied, just not in the shape asked for — so it
// must park as an exception rather than be retried as if nobody answered.
func TestAMalformedPlanIsNotErrNoAnswer(t *testing.T) {
	p, ing, _ := plannerRig(t, "planner-noplan")
	_, err := p.Decompose(context.Background(), ing, plannerDecl())
	if err == nil || !strings.Contains(err.Error(), "no plan") {
		t.Fatalf("an answer with no plan was accepted: %v", err)
	}
	if errors.Is(err, plan.ErrNoAnswer) {
		t.Fatalf("a malformed plan was mistaken for a run that never answered: %v", err)
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

// TestThePlannerAsksForASummary: the sections layer instructs the planner to
// carry a one-line summary per unit, distinct from the task the builder reads.
func TestThePlannerAsksForASummary(t *testing.T) {
	repo := forge.Repo{Name: "app", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}},
	}}
	instructions := sectionsLayer(repo).Instructions
	if !strings.Contains(instructions, "summary: one line the room shows for this unit; the task is what the builder reads.") {
		t.Fatalf("the planner was not asked for a summary:\n%s", instructions)
	}
}

// TestAPlanCarriesPerUnitSummaries: a planner answer naming "summary" on a
// unit becomes a plan.Node with that Summary set, read-only alongside the
// task — never folded into it.
func TestAPlanCarriesPerUnitSummaries(t *testing.T) {
	repo := forge.Repo{Name: "app", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}},
	}}
	text := "```json\n" +
		`{"units":[{"id":"api-endpoint","task":"Add GET /v1/widgets","summary":"Widgets endpoint","sections":["api"],"depends_on":[]}],"why":"one unit"}` +
		"\n```"
	nodes, err := parsePlan(text, repo)
	if err != nil {
		t.Fatalf("parsePlan: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Summary != "Widgets endpoint" {
		t.Fatalf("the unit's summary was lost: %+v", nodes)
	}
	if nodes[0].Task != "Add GET /v1/widgets" {
		t.Fatalf("the summary displaced the task: %+v", nodes)
	}
}

// TestAUnitWithNoSummaryIsRefused: a unit missing "summary" is refused by
// name, the same way a unit missing "task" is — an absent title must not
// read as an empty one and reach the room.
func TestAUnitWithNoSummaryIsRefused(t *testing.T) {
	repo := forge.Repo{Name: "app", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}},
	}}
	text := "```json\n" +
		`{"units":[{"id":"api-endpoint","task":"Add GET /v1/widgets","sections":["api"],"depends_on":[]}],"why":"one unit, no summary"}` +
		"\n```"
	_, err := parsePlan(text, repo)
	if err == nil || !strings.Contains(err.Error(), "api-endpoint") || !strings.Contains(err.Error(), "no summary") {
		t.Fatalf("a unit with no summary was not refused by id: %v", err)
	}
}

// TestAPlanCarriesPerUnitAttempts: a planner answer naming "attempts" on a
// unit becomes a plan.Node with that attempt budget, mirroring how a
// per-unit model tier carries through; a unit that omits it must read as 0,
// which plan.Node treats as inheriting the box's default, not as a budget
// of zero tries.
func TestAPlanCarriesPerUnitAttempts(t *testing.T) {
	repo := forge.Repo{Name: "app", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}},
	}}
	text := "```json\n" +
		`{"units":[{"id":"api-endpoint","task":"Add GET /v1/widgets","summary":"Widgets endpoint","sections":["api"],"depends_on":[],"attempts":3},` +
		`{"id":"room-view","task":"Render widgets","summary":"Widgets view","sections":["api"],"depends_on":[{"on":"api-endpoint","for":"the widget response type"}]}],"why":"two units"}` +
		"\n```"
	nodes, err := parsePlan(text, repo)
	if err != nil {
		t.Fatalf("parsePlan: %v", err)
	}
	if len(nodes) != 2 || nodes[0].Attempts != 3 {
		t.Fatalf("the unit's attempt budget was lost: %+v", nodes)
	}
	if nodes[1].Attempts != 0 {
		t.Fatalf("a unit that names no attempts must inherit, not carry a stray budget: %+v", nodes[1])
	}
}

// TestAnEdgesReasonSurvivesIntoTheNode: the object shape {"on":...,"for":...}
// parses into both DependsOn (the edge) and EdgeReasons (why), the fields
// [plan.Plan.Seal] folds into the digest and [plan.Plan.validate] requires of
// a judgement plan.
func TestAnEdgesReasonSurvivesIntoTheNode(t *testing.T) {
	repo := forge.Repo{Name: "app", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}},
	}}
	text := "```json\n" +
		`{"units":[{"id":"api-endpoint","task":"Add GET /v1/widgets","summary":"Widgets endpoint","sections":["api"],"depends_on":[]},` +
		`{"id":"room-view","task":"Render widgets","summary":"Widgets view","sections":["api"],"depends_on":[{"on":"api-endpoint","for":"widget.Response"}]}],"why":"two units"}` +
		"\n```"
	nodes, err := parsePlan(text, repo)
	if err != nil {
		t.Fatalf("parsePlan: %v", err)
	}
	view := nodes[1]
	if len(view.DependsOn) != 1 || view.DependsOn[0] != "api-endpoint" {
		t.Fatalf("the edge was lost: %+v", view)
	}
	if view.EdgeReasons["api-endpoint"] != "widget.Response" {
		t.Fatalf("the edge's reason was lost: %+v", view)
	}
}

// TestABareStringEdgeIsRefused: a bare id with no "for" is exactly the
// unreasoned chain #201 exists to close, so parsePlan refuses it in the
// planner's own terms rather than silently accepting an old-schema answer
// that [plan.Plan.validate] would refuse anyway further downstream.
func TestABareStringEdgeIsRefused(t *testing.T) {
	repo := forge.Repo{Name: "app", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"internal/api"}},
	}}
	text := "```json\n" +
		`{"units":[{"id":"api-endpoint","task":"Add GET /v1/widgets","summary":"Widgets endpoint","sections":["api"],"depends_on":[]},` +
		`{"id":"room-view","task":"Render widgets","summary":"Widgets view","sections":["api"],"depends_on":["api-endpoint"]}],"why":"two units"}` +
		"\n```"
	_, err := parsePlan(text, repo)
	if err == nil || !strings.Contains(err.Error(), "room-view") || !strings.Contains(err.Error(), "api-endpoint") || !strings.Contains(err.Error(), `"for"`) {
		t.Fatalf("a bare-string edge with no reason was not refused: %v", err)
	}
}

// TestReplanParsesARevisedNodeAndRecords: issue #182 part 3 — a replan
// judgement answering the same "sections"/"task" shape [plan.ReplanNode]
// carries comes back parsed, and the run leaves the same agent-run,
// agent-session and agent-result trio on the origination's chain that
// Decompose's run does.
func TestReplanParsesARevisedNodeAndRecords(t *testing.T) {
	p, ing, _ := plannerRig(t, "replan")
	ctx := context.Background()
	pl := plan.Plan{Ingress: ing.ID, Nodes: []plan.Node{
		{ID: "api-endpoint", Task: "Add GET /v1/widgets", Sections: []string{"api"}},
	}}
	unit := plan.ReplanUnit{ID: "api-endpoint", Task: "Add GET /v1/widgets", Sections: []string{"api"}}
	esc := plan.ReplanEscalation{
		Reason:        "the widgets view needs the web section too",
		NeedsSections: []string{"web"}, Paths: []string{"web/src/view.go"},
	}
	node, err := p.Replan(ctx, ing, pl, unit, esc)
	if err != nil {
		t.Fatalf("replan: %v", err)
	}
	if !strings.Contains(strings.Join(node.Sections, ","), "api") || !strings.Contains(strings.Join(node.Sections, ","), "web") {
		t.Fatalf("the revised node lost a section: %+v", node)
	}
	if node.Task != "" {
		t.Fatalf("the revised node changed a task the answer left empty: %+v", node)
	}
	kinds := map[string]bool{}
	for r, err := range p.Store.Log().Since(ctx, btn, ingress.Scope(ing.ID), 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[r.Kind] = true
	}
	for _, want := range []string{"agent-run", "agent-prompt", "agent-session", "agent-result", "agent-text"} {
		if !kinds[want] {
			t.Errorf("no %s on the origination's chain", want)
		}
	}
}

// TestAReplanAnsweringGarbageIsRefused: an answer with no JSON object naming
// "sections" is refused, the same way an answer with no plan is for a fresh
// decomposition — a replan judgement that does not hold must not be
// mistaken for a granted one.
func TestAReplanAnsweringGarbageIsRefused(t *testing.T) {
	p, ing, _ := plannerRig(t, "replan-bad")
	pl := plan.Plan{Ingress: ing.ID, Nodes: []plan.Node{
		{ID: "api-endpoint", Task: "Add GET /v1/widgets", Sections: []string{"api"}},
	}}
	unit := plan.ReplanUnit{ID: "api-endpoint", Task: "Add GET /v1/widgets", Sections: []string{"api"}}
	esc := plan.ReplanEscalation{Reason: "the widgets view needs the web section too"}
	_, err := p.Replan(context.Background(), ing, pl, unit, esc)
	if err == nil || !strings.Contains(err.Error(), "no revised node") {
		t.Fatalf("a garbage replan answer was not refused: %v", err)
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
