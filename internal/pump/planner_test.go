package pump

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// flaky is a judgement decomposer that fails a number of times first — a
// planner that has hit its session limit.
type flaky struct{ calls, failFor int }

func (*flaky) Name() string     { return "judge" }
func (*flaky) Replayable() bool { return false }
func (f *flaky) Decompose(context.Context, ingress.Ingress, kind.Declaration) (plan.Result, error) {
	f.calls++
	if f.calls <= f.failFor {
		return plan.Result{}, errors.New("acp: prompt: You've hit your session limit")
	}
	return plan.Result{Nodes: []plan.Node{{ID: "work", Task: "do it", Sections: []string{"api"}}}, AgentRef: "agentref-judge"}, nil
}

func judgementWorld(t *testing.T, judge *flaky) (*Pump, *ingress.StoreRouter, *plan.Store, *control.Room) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	decl := kind.Declaration{Name: kind.SeedIssue, Completion: kind.Completion{Graph: true},
		Source:    agent.Source{Kind: agent.SourceInline, Digest: "test"},
		Decompose: kind.Decomposition{Decomposer: "judge"}}
	set, err := kind.NewSet(kind.Capabilities{}, append(kind.Seeds(), decl)...)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := plan.NewRegistry(plan.Identity{}, judge)
	if err != nil {
		t.Fatal(err)
	}
	plans := &plan.Store{Log: s.Log(), Actor: "pump-runner"}
	p := &Pump{
		Store: s, Tenant: tn, Runner: "pump-runner",
		Queue: &queue.Queue{Store: s, Runner: "pump-runner", TTL: time.Hour},
		Kinds: kind.Sets{"shop": set}, Decomposers: reg, Plans: plans,
		// An unbudgeted meter: these tests are about the judgement seat's own
		// failure handling, not the meter's pacing, so the meter must admit
		// every acquire rather than nil-panic on one.
		Meter: &capacity.Meter{Store: s, Tenant: tn, Actor: "pump-runner"},
	}
	return p, &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "pump-runner"},
		plans, &control.Room{Store: s, Tenant: tn, Actor: "chris"}
}

func openIssue(t *testing.T, router *ingress.StoreRouter, key string) ingress.Ingress {
	t.Helper()
	ing, _, err := router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key, Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}
	return ing
}

// TestAFailedJudgementIsAnExceptionNotATickLoop. The seat whose failure
// parks the one origination that asked for a judgement: the failure is on the
// origination's chain, in the exception queue with its reason, and intake
// does not run the planner again until a person retries — a planner out of
// budget re-run every tick would be a spend loop wearing an intake.
func TestAFailedJudgementIsAnExceptionNotATickLoop(t *testing.T) {
	judge := &flaky{failFor: 1}
	p, router, plans, room := judgementWorld(t, judge)
	ctx := context.Background()
	ing := openIssue(t, router, "issue-1")

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("first intake: created=%d err=%v", n, err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil || st.State != "failed" || !strings.Contains(st.Reason, "session limit") {
		t.Fatalf("standing after a failed judgement: %+v %v", st, err)
	}
	// The room lists it as an exception, in the item's shape, with the reason.
	exs, err := room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exs) != 1 || exs[0].ID != ing.ID || !strings.Contains(exs[0].Note, "planning failed: ") || exs[0].Task != "do it" {
		t.Fatalf("the room does not list the failed plan: %+v", exs)
	}
	// And the next ticks do not spend on it.
	for range 3 {
		if _, err := p.Intake(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if judge.calls != 1 {
		t.Fatalf("the planner was re-run %d times on a failed origination", judge.calls)
	}

	// A person sends it back; the retried judgement is dispatched off this
	// tick exactly like the first one was, so this tick only kicks it off —
	// the plan is recorded once it settles, and the item is submitted by the
	// tick after that, carrying where the judgement put it.
	if err := plans.Retry(ctx, tn, ing.ID, "chris", "budget is back"); err != nil {
		t.Fatal(err)
	}
	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("intake right after retry dispatches, it does not submit: created=%d err=%v", n, err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the retried judgement")
	}
	if n, err := p.Intake(ctx); err != nil || n != 1 {
		t.Fatalf("intake after the retried judgement settled: created=%d err=%v", n, err)
	}
	item, err := p.Queue.Load(ctx, tn, "wi-"+ing.ID)
	if err != nil || item.State != queue.StatePending || len(item.Sections) != 1 || item.Sections[0] != "api" {
		t.Fatalf("the retried plan's item: %+v %v", item, err)
	}
	if exs, _ := room.Exceptions(ctx); len(exs) != 0 {
		t.Fatalf("a planned origination is still listed as an exception: %+v", exs)
	}
	// Retrying what is not failed is refused: the item exists now.
	if err := plans.Retry(ctx, tn, ing.ID, "chris", "again"); err == nil {
		t.Fatal("a planned origination could be retried as if it had failed")
	}
}

// TestAResolvedPlanIsNeitherRetriedNorListed: giving up is the other verb,
// and it is final until somebody changes something.
func TestAResolvedPlanIsNeitherRetriedNorListed(t *testing.T) {
	judge := &flaky{failFor: 100}
	p, router, plans, room := judgementWorld(t, judge)
	ctx := context.Background()
	ing := openIssue(t, router, "issue-2")
	if _, err := p.Intake(ctx); err != nil {
		t.Fatal(err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}
	if err := plans.Resolve(ctx, tn, ing.ID, "chris", "not worth a plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Intake(ctx); err != nil {
		t.Fatal(err)
	}
	if judge.calls != 1 {
		t.Fatalf("a resolved origination was planned again (%d calls)", judge.calls)
	}
	if exs, _ := room.Exceptions(ctx); len(exs) != 0 {
		t.Fatalf("a resolved origination is still an exception: %+v", exs)
	}
}
