package pump

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// fakeReplanner is issue #182 part 2's test double for [plan.Replanner]: no
// model, just the answer (or refusal) the test wants to hand back, and the
// arguments it was actually called with, so a test can assert the pump
// handed the planner the right unit and escalation rather than merely that
// something was called.
type fakeReplanner struct {
	answer plan.ReplanNode
	err    error

	calls  int
	origin ingress.Ingress
	pl     plan.Plan
	unit   plan.ReplanUnit
	esc    plan.ReplanEscalation
}

func (f *fakeReplanner) Replan(_ context.Context, origin ingress.Ingress, p plan.Plan, unit plan.ReplanUnit, esc plan.ReplanEscalation) (plan.ReplanNode, error) {
	f.calls++
	f.origin, f.pl, f.unit, f.esc = origin, p, unit, esc
	return f.answer, f.err
}

// replanWorld builds a pump wired for [Pump.Replan] alone — no loop, no git,
// no builder — because dispatching and answering a replan touches only the
// queue and the plan store, exactly as [judgementWorld] tests
// [Pump.dispatchJudgement] without a builder either. It records one plan
// with one node ("work", section "loop") and submits the item that node
// produces, already pending.
func replanWorld(t *testing.T) (*Pump, *queue.Queue, *plan.Store, ingress.Ingress, plan.Plan, queue.WorkItem) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	q := &queue.Queue{Store: s, Runner: "pump-runner", TTL: time.Hour}
	plans := &plan.Store{Log: s.Log(), Actor: "pump-runner"}
	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "pump-runner"}
	ing, _, err := router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "replan-1", Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}

	pl := plan.Plan{
		Ingress: ing.ID, Kind: string(ing.Kind), Decomposer: "identity", Replayable: true,
		Nodes: []plan.Node{{ID: "work", Task: "do it", Sections: []string{"loop"}}},
	}
	if err := pl.Seal(); err != nil {
		t.Fatal(err)
	}
	pl, err = plans.Record(context.Background(), ing, pl)
	if err != nil {
		t.Fatal(err)
	}

	item := plan.Items(ing, pl)[0]
	item, err = q.Submit(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}

	p := &Pump{
		Store: s, Tenant: tn, Runner: "pump-runner", Queue: q,
		Plans: plans, Meter: &capacity.Meter{Store: s, Tenant: tn, Actor: "pump-runner"},
	}
	return p, q, plans, ing, pl, item
}

// awaitReplan puts item into the exact wait [loop.Loop.RunHolding] would
// have — [queue.Queue.AwaitReplan] plus the "replan-requested" record it
// writes right before — without going through a real build or escalation,
// the same shortcut [judgementWorld]'s tests take around a real judgement
// run.
func awaitReplan(t *testing.T, q *queue.Queue, item queue.WorkItem, esc plan.ReplanEscalation) queue.WorkItem {
	t.Helper()
	ctx := context.Background()
	body, err := json.Marshal(esc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: "replan-requested", Subject: item.ID, Actor: "mason", Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	item, err = q.AwaitReplan(ctx, item, forge.Lease{}, esc.Reason)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestReplanNilReplannerParksAsException: [Pump.Replanner] unconfigured
// means a unit awaiting one must not strand forever — it parks as an
// exception with the builder's own escalation reason, exactly as an
// unwidenable escalation always has before replanning existed.
func TestReplanNilReplannerParksAsException(t *testing.T) {
	p, q, _, _, _, item := replanWorld(t)
	ctx := context.Background()
	esc := plan.ReplanEscalation{
		Reason:        "escalated by mason: need the agent section too",
		NeedsSections: []string{"agent"}, Paths: []string{"internal/agent/foo.go"},
	}
	item = awaitReplan(t, q, item, esc)

	if n, err := p.Replan(ctx); err != nil || n != 1 {
		t.Fatalf("replan: dispatched=%d err=%v", n, err)
	}
	item, err := q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("expected the item parked with no planner configured, got %s: %s", item.State, item.Note)
	}
	if item.AwaitingReplan {
		t.Fatal("AwaitingReplan survived the park")
	}
	if !strings.Contains(item.Note, "escalated by mason") {
		t.Fatalf("park note does not carry the builder's escalation: %q", item.Note)
	}
}

// TestReplanFailingReplannerParksWithBothReasons: a configured planner that
// declines carries its own reason on the park alongside the builder's —
// the exception queue sees the unit only when neither policy nor planner
// answered, and an operator reading the park should not have to guess which
// half spoke.
func TestReplanFailingReplannerParksWithBothReasons(t *testing.T) {
	p, q, _, _, _, item := replanWorld(t)
	ctx := context.Background()
	esc := plan.ReplanEscalation{
		Reason:        "escalated by mason: need the agent section too",
		NeedsSections: []string{"agent"}, Paths: []string{"internal/agent/foo.go"},
	}
	item = awaitReplan(t, q, item, esc)

	fake := &fakeReplanner{err: fmt.Errorf("the planner declines: this decomposition already accounted for the agent section")}
	p.Replanner = fake
	if n, err := p.Replan(ctx); err != nil || n != 1 {
		t.Fatalf("replan: dispatched=%d err=%v", n, err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched replan")
	}
	item, err := q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("expected the item parked after a failing replan, got %s: %s", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "escalated by mason") || !strings.Contains(item.Note, "the planner declines") {
		t.Fatalf("park note does not carry both the builder's and the planner's reasons: %q", item.Note)
	}
	if fake.calls != 1 {
		t.Fatalf("replanner called %d times, want 1", fake.calls)
	}
}

// TestReplanAnsweredAmendsSectionsAndReturnsToClaimable is issue #182 part
// 2's success path: the planner is asked with the right unit and
// escalation, its revised sections land on the item, a "plan-amended"
// record names the escalation it answers, the revised node is recorded as
// an ordinary planned record on the origination, and the item goes back to
// a claimable state with whatever resume point it already carried
// untouched — a replan decides WHERE and WHAT a unit works, never buys back
// a build.
func TestReplanAnsweredAmendsSectionsAndReturnsToClaimable(t *testing.T) {
	p, q, plans, ing, pl, item := replanWorld(t)
	ctx := context.Background()

	// A resume point already on the item when the escalation happened —
	// this is the "kept commit" issue #182 says a replan must not disturb.
	item.Built, item.BuiltBase = "deadbeef", "cafefeed"
	if _, err := q.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID, Kind: "waiting", Subject: item.ID,
		Body: mustMarshalItem(t, item),
	}); err != nil {
		t.Fatal(err)
	}

	esc := plan.ReplanEscalation{
		Reason:        "escalated by mason: need the agent section too",
		NeedsSections: []string{"agent"}, Paths: []string{"internal/agent/foo.go"},
	}
	item = awaitReplan(t, q, item, esc)
	if item.Built != "deadbeef" {
		t.Fatalf("AwaitReplan lost the resume point: %+v", item)
	}

	fake := &fakeReplanner{answer: plan.ReplanNode{Sections: []string{"loop", "agent"}}}
	p.Replanner = fake
	if n, err := p.Replan(ctx); err != nil || n != 1 {
		t.Fatalf("replan: dispatched=%d err=%v", n, err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched replan")
	}

	if fake.calls != 1 {
		t.Fatalf("replanner called %d times, want 1", fake.calls)
	}
	if fake.origin.ID != ing.ID {
		t.Fatalf("replanner asked about origination %q, want %q", fake.origin.ID, ing.ID)
	}
	if fake.pl.Ref != pl.Ref {
		t.Fatalf("replanner given plan %q, want the latest %q", fake.pl.Ref, pl.Ref)
	}
	if fake.unit.ID != item.ID || fake.unit.Task != item.Task {
		t.Fatalf("replanner given unit %+v, want id=%s task=%s", fake.unit, item.ID, item.Task)
	}
	if fake.esc.Reason != esc.Reason {
		t.Fatalf("replanner given escalation %+v, want reason %q", fake.esc, esc.Reason)
	}

	out, err := q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.State != queue.StatePending {
		t.Fatalf("expected the item back to pending, got %s: %s", out.State, out.Note)
	}
	if out.AwaitingReplan {
		t.Fatal("AwaitingReplan survived the answer")
	}
	if want := []string{"loop", "agent"}; !equalStrings(out.Sections, want) {
		t.Fatalf("sections after replan = %v, want %v", out.Sections, want)
	}
	if out.Task != item.Task {
		t.Fatalf("task changed though the planner named none: %q", out.Task)
	}
	if out.Built != "deadbeef" || out.BuiltBase != "cafefeed" {
		t.Fatalf("the kept commit did not survive the replan: built=%q base=%q", out.Built, out.BuiltBase)
	}

	var amended int
	for r, err := range q.Store.Log().Since(ctx, tn, "workitem/"+item.ID, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "plan-amended" {
			amended++
			if !strings.Contains(string(r.Body), "agent") {
				t.Fatalf("plan-amended record does not name the escalation it answers: %s", r.Body)
			}
		}
	}
	if amended != 1 {
		t.Fatalf("expected exactly one plan-amended record, got %d", amended)
	}

	latest, ok, err := plans.Latest(ctx, tn, ing.ID)
	if err != nil || !ok {
		t.Fatalf("latest plan after replan: ok=%v err=%v", ok, err)
	}
	if latest.Ref == pl.Ref {
		t.Fatal("the revised plan was not recorded as a new planned record")
	}
	n, ok := latest.Node("work")
	if !ok || !equalStrings(n.Sections, []string{"loop", "agent"}) {
		t.Fatalf("revised plan's node = %+v, want sections [loop agent]", n)
	}
}

func mustMarshalItem(t *testing.T, w queue.WorkItem) []byte {
	t.Helper()
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
