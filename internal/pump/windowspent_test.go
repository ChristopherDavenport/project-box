package pump

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
)

// windowSpentJudge is a judgement decomposer whose failure is the run's own
// provider account report that the window is exhausted, met mid-run rather
// than at the pump's own pre-dispatch Acquire (issue #112) — wrapped exactly
// as the acp planner will wrap it, around [plan.ErrNoAnswer].
type windowSpentJudge struct {
	calls   int
	failFor int
	ws      loop.WindowSpent
}

func (*windowSpentJudge) Name() string     { return "judge" }
func (*windowSpentJudge) Replayable() bool { return false }
func (d *windowSpentJudge) Decompose(context.Context, ingress.Ingress, kind.Declaration) (plan.Result, error) {
	d.calls++
	if d.calls <= d.failFor {
		w := d.ws
		return plan.Result{}, fmt.Errorf("acp: planner: %w: %w", plan.ErrNoAnswer, &w)
	}
	return plan.Result{Nodes: []plan.Node{{ID: "work", Task: "do it"}}, AgentRef: "agentref-judge"}, nil
}

func openWindowIssue(t *testing.T, router *ingress.StoreRouter, key string) ingress.Ingress {
	t.Helper()
	ing, _, err := router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key, Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}
	return ing
}

// TestAWindowSpentJudgementDefersRatherThanFails: a dispatched judgement
// whose run reports the account's own window spent — not the meter's pacing,
// which never refused anything here — defers rather than fails: the
// origination stays pending (Standing.Blocked is false) and the deferred
// record carries the window and the reset the run named.
func TestAWindowSpentJudgementDefersRatherThanFails(t *testing.T) {
	judge := &windowSpentJudge{failFor: 1000, ws: loop.WindowSpent{
		Window: "five_hour", ResetsAt: time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC), Reason: "session limit",
	}}
	p, router, plans, _ := deferWorld(t, judge, capacity.Budget{})
	ctx := context.Background()
	ing := openWindowIssue(t, router, "issue-window-1")

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("intake: created=%d err=%v", n, err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "deferred" {
		t.Fatalf("standing after a window-spent run: %+v", st)
	}
	if st.Blocked() {
		t.Fatal("a deferred origination must stay pending — intake retries it once the meter admits")
	}
	if !strings.Contains(st.Reason, "five_hour") {
		t.Fatalf("deferred reason should name the window: %q", st.Reason)
	}
	if judge.calls != 1 {
		t.Fatalf("the planner ran %d times — one dispatch, one deferral", judge.calls)
	}
}

// TestAWindowSpentJudgementPastTheBoundFails: a judgement that keeps
// reporting the window spent, dispatch after dispatch, eventually fails with
// the streak named instead of deferring forever — the same bound a work
// item's own build-or-gate deferral takes.
func TestAWindowSpentJudgementPastTheBoundFails(t *testing.T) {
	judge := &windowSpentJudge{failFor: 1000, ws: loop.WindowSpent{
		Window: "five_hour", ResetsAt: time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC), Reason: "session limit",
	}}
	p, router, plans, _ := deferWorld(t, judge, capacity.Budget{})
	ctx := context.Background()
	ing := openWindowIssue(t, router, "issue-window-bound")

	for i := 0; i <= loop.MaxWindowDefers; i++ {
		if _, err := p.Intake(ctx); err != nil {
			t.Fatalf("intake: %v", err)
		}
		if !p.Quiesce(ctx) {
			t.Fatal("quiesce did not settle the dispatched judgement")
		}
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "failed" {
		t.Fatalf("deferring past the bound should fail the origination, got %+v", st)
	}
	if !st.Blocked() {
		t.Fatal("a failed plan must be blocked — an exception, waiting on a person")
	}
	if !strings.Contains(st.Reason, fmt.Sprintf("%d", loop.MaxWindowDefers)) {
		t.Fatalf("failure reason should say how many times it deferred: %q", st.Reason)
	}
	if judge.calls != loop.MaxWindowDefers+1 {
		t.Fatalf("planner should have run once per dispatch, got %d", judge.calls)
	}
}

// TestAWindowSpentJudgementWithNoResetAnywhereFailsLikeAnyOtherNoAnswer: a
// zero-reset window-spent report with no account window on record anywhere
// is not a bounded deferral — unknown never reads as zero — so it fails
// exactly like today's plain [plan.ErrNoAnswer] path.
func TestAWindowSpentJudgementWithNoResetAnywhereFailsLikeAnyOtherNoAnswer(t *testing.T) {
	judge := &windowSpentJudge{failFor: 1000, ws: loop.WindowSpent{
		Window: "five_hour", Reason: "session limit, no reset named",
	}}
	p, router, plans, _ := deferWorld(t, judge, capacity.Budget{}) // no meter charge: no window on record
	ctx := context.Background()
	ing := openWindowIssue(t, router, "issue-window-noreset")

	if n, err := p.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("intake: created=%d err=%v", n, err)
	}
	if !p.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}
	st, err := plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "failed" {
		t.Fatalf("a window-spent report with no reset anywhere should fail, got %+v", st)
	}
	if judge.calls != 1 {
		t.Fatalf("the planner ran %d times — dispatch is a single attempt", judge.calls)
	}
}
