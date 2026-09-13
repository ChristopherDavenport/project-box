package pump

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// noAnswer is a judgement decomposer whose failures are a planner that never
// answered — a transport error, a mid-response server error, a limit
// refusal — as issue #110 distinguishes from a planner that judged and
// whose judgement does not hold. When charge is set, the failing call also
// charges the meter with an exhausted account-window Limit, simulating the
// acp planner's own Spend having already reported the window closed before
// its error returns.
type noAnswer struct {
	meter   *capacity.Meter
	calls   int
	failFor int
	charge  bool
}

func (*noAnswer) Name() string     { return "judge" }
func (*noAnswer) Replayable() bool { return false }
func (d *noAnswer) Decompose(ctx context.Context, ing ingress.Ingress, _ kind.Declaration) (plan.Result, error) {
	d.calls++
	if d.calls <= d.failFor {
		if d.charge {
			d.meter.Charge(ctx, ing.ID, capacity.Spend{
				Reported: true, Total: 100,
				Limit: &capacity.Limit{Status: "rejected", Window: "five_hour", Utilization: 1, ResetsAt: time.Now().Add(time.Hour).Unix()},
			})
		}
		return plan.Result{}, fmt.Errorf("acp: planner: acp: prompt: rpc -32603: Internal error: You've hit your session limit: %w", plan.ErrNoAnswer)
	}
	return plan.Result{Nodes: []plan.Node{{ID: "work", Task: "do it"}}, AgentRef: "agentref-judge"}, nil
}

// answered is a judgement decomposer whose failure is a judgement that does
// not hold — an escalation, a malformed plan, a parse refusal — carrying no
// [plan.ErrNoAnswer].
type answered struct{ calls int }

func (*answered) Name() string     { return "judge" }
func (*answered) Replayable() bool { return false }
func (d *answered) Decompose(context.Context, ingress.Ingress, kind.Declaration) (plan.Result, error) {
	d.calls++
	return plan.Result{}, errors.New("plan: the graph this judgement produced does not hold together")
}

// deferWorld builds a pump around a judgement decomposer and a Meter the
// test controls directly, so the meter's own pacing — not the decomposer's
// own retry-count games — is what the test drives.
func deferWorld(t *testing.T, judge plan.Decomposer, budget capacity.Budget) (*Pump, *ingress.StoreRouter, *plan.Store, *capacity.Meter) {
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
	meter := &capacity.Meter{Store: s, Tenant: tn, Actor: "pump-runner", Budget: budget}
	p := &Pump{
		Store: s, Tenant: tn, Runner: "pump-runner",
		Queue: &queue.Queue{Store: s, Runner: "pump-runner", TTL: time.Hour},
		Kinds: kind.Sets{"shop": set}, Decomposers: reg, Plans: plans,
		Meter: meter,
	}
	return p, &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "pump-runner"}, plans, meter
}

// TestANoAnswerRunUnderARefusingMeterDefersNotFails is issue #110's incident:
// three originations went to the exception queue as plan-failed when the
// account's five-hour window ran out under the planner. The run's own charge
// reports the window exhausted, so the very next Acquire — before the retry
// even reaches the decomposer again — refuses, and that refusal is what gets
// recorded, not a failure.
func TestANoAnswerRunUnderARefusingMeterDefersNotFails(t *testing.T) {
	judge := &noAnswer{failFor: 1000, charge: true}
	p, router, plans, _ := deferWorld(t, judge, capacity.Budget{Credential: capacity.CredentialPlan, WindowReserve: 0.5})
	judge.meter = p.Meter
	ctx := context.Background()
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-1", Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("first intake: created=%d err=%v", n, err)
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "deferred" {
		t.Fatalf("standing after a no-answer run under a refusing meter: %+v", st)
	}
	if st.Blocked() {
		t.Fatal("a deferred origination must not be blocked — the meter, not the exception queue, holds it")
	}
	if !strings.Contains(st.Reason, "five_hour") || !strings.Contains(st.Reason, "session limit") {
		t.Fatalf("the deferral reason does not name the meter's own refusal and the run's error: %q", st.Reason)
	}
	if judge.calls != 1 {
		t.Fatalf("the planner ran %d times — the retry must stop at the meter's own refusal", judge.calls)
	}

	// The refusal holds: the next tick does not even invoke the decomposer.
	for range 3 {
		if _, err := p.Intake(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if judge.calls != 1 {
		t.Fatalf("the decomposer was invoked %d times while the meter's refusal held", judge.calls)
	}
}

// TestANoAnswerRunUnderAnAdmittingMeterRetriesThenFails: the issue's own
// words are "retried in place under a bound before it parks." A run that
// never answers but whose meter says nothing about the window is retried up
// to DefaultDeferRetries more times, and only THEN does it become the
// ordinary planning failure.
func TestANoAnswerRunUnderAnAdmittingMeterRetriesThenFails(t *testing.T) {
	judge := &noAnswer{failFor: 1000, charge: false}
	p, router, plans, _ := deferWorld(t, judge, capacity.Budget{})
	ctx := context.Background()
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-2", Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("intake: created=%d err=%v", n, err)
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "failed" || !strings.Contains(st.Reason, "session limit") {
		t.Fatalf("standing after the retry bound was spent against an admitting meter: %+v", st)
	}
	if !st.Blocked() {
		t.Fatal("a failed plan must be blocked — an exception, waiting on a person")
	}
	wantCalls := 1 + DefaultDeferRetries
	if judge.calls != wantCalls {
		t.Fatalf("the planner ran %d times, want %d (one try plus %d retries)", judge.calls, wantCalls, DefaultDeferRetries)
	}
}

// TestAnAnsweredJudgementFailsImmediatelyWithNoRetry: an error without
// [plan.ErrNoAnswer] is a planner that DID answer — an escalation, a
// malformed plan, a parse refusal — and today's immediate failure, with no
// retry and no deferral, is unchanged.
func TestAnAnsweredJudgementFailsImmediatelyWithNoRetry(t *testing.T) {
	judge := &answered{}
	p, router, plans, _ := deferWorld(t, judge, capacity.Budget{})
	ctx := context.Background()
	ing, _, err := router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-3", Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("intake: created=%d err=%v", n, err)
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "failed" || !strings.Contains(st.Reason, "does not hold together") {
		t.Fatalf("standing after an answered judgement: %+v", st)
	}
	if judge.calls != 1 {
		t.Fatalf("an answered failure was retried %d times", judge.calls)
	}
}

// TestAPreRunMeterRefusalNeverInvokesTheDecomposer: the meter's refusal
// before the planner runs at all is pure pacing, with no record and no
// planner call — the same posture Drain already takes at claim.
func TestAPreRunMeterRefusalNeverInvokesTheDecomposer(t *testing.T) {
	judge := &noAnswer{failFor: 1000}
	p, router, plans, meter := deferWorld(t, judge, capacity.Budget{Credential: capacity.CredentialPlan, WindowReserve: 0.5})
	ctx := context.Background()
	ing, _, err := router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-4", Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}
	// Seed the meter's account window as already exhausted, before intake
	// ever runs — the refusal Acquire hits on its very first call.
	meter.Charge(ctx, "seed", capacity.Spend{
		Reported: true, Total: 100,
		Limit: &capacity.Limit{Status: "rejected", Window: "five_hour", Utilization: 1, ResetsAt: time.Now().Add(time.Hour).Unix()},
	})

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("intake: created=%d err=%v", n, err)
	}
	if judge.calls != 0 {
		t.Fatalf("the decomposer ran %d times against a meter that refused before the run", judge.calls)
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "" {
		t.Fatalf("a pre-run refusal must write no record at all: %+v", st)
	}
	if st.Blocked() {
		t.Fatal("an origination with no planning record must not be blocked")
	}
}
