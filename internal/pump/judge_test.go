package pump

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// judgeKind is the declared kind a judged origination opens under in these
// tests — distinct from kind.SeedIssue so a world can carry a judged
// origination and an ordinary deterministic issue side by side, exactly as a
// real Box mixing kinds does.
const judgeKind = "judged-issue"

// fakeJudge is a judgement decomposer whose Decompose blocks until the test
// closes release — synchronized with channels, never a sleep, so a test that
// claims two calls overlapped is proving it rather than hoping a delay was
// long enough. entered is signaled once per call, on entry, before it
// blocks: a test drains it to know a dispatch actually reached the
// decomposer.
type fakeJudge struct {
	entered chan struct{}
	release chan struct{}
	fail    error // returned instead of a plan, once released, when set

	mu    sync.Mutex
	calls int
}

func newFakeJudge() *fakeJudge {
	return &fakeJudge{entered: make(chan struct{}, 16), release: make(chan struct{})}
}

func (*fakeJudge) Name() string     { return "fake-judge" }
func (*fakeJudge) Replayable() bool { return false }

func (f *fakeJudge) Decompose(ctx context.Context, ing ingress.Ingress, _ kind.Declaration) (plan.Result, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	f.entered <- struct{}{}
	select {
	case <-f.release:
	case <-ctx.Done():
		return plan.Result{}, ctx.Err()
	}
	if f.fail != nil {
		return plan.Result{}, f.fail
	}
	return plan.Result{Nodes: []plan.Node{{ID: "work", Task: "judged " + ing.ID}}, AgentRef: "agentref-fake"}, nil
}

func (f *fakeJudge) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// waitEntered blocks until n calls have reached fakeJudge.Decompose, failing
// the test rather than hanging forever if dispatch never happened.
func waitEntered(t *testing.T, judge *fakeJudge, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-judge.entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for judgement %d/%d to enter", i+1, n)
		}
	}
}

// withJudge wires a fake judgement decomposer into a rig built by setup(),
// declared under judgeKind beside the seeded kinds. It must run before the
// pump's first Tick or Drain: Kinds and Decomposers are read with no lock
// outside SetKinds/SetLoops, which is safe only because nothing is
// concurrently ticking yet.
func withJudge(t *testing.T, w *world, judge plan.Decomposer) {
	t.Helper()
	reg := plan.Default()
	if err := reg.Register(judge); err != nil {
		t.Fatal(err)
	}
	decl := kind.Declaration{
		Name:       judgeKind,
		Source:     agent.Source{Kind: agent.SourceInline, Digest: "test"},
		Completion: kind.Completion{Graph: true},
		Decompose:  kind.Decomposition{Decomposer: judge.Name()},
	}
	set, err := kind.NewSet(kind.Capabilities{}, append(kind.Seeds(), decl)...)
	if err != nil {
		t.Fatal(err)
	}
	w.pump.Decomposers = reg
	w.pump.SetKinds(kind.Sets{"shop": set})
}

// openJudged opens an origination under judgeKind, the fake judgement
// decomposer's own declaration.
func (w *world) openJudged(t *testing.T, key string) ingress.Ingress {
	t.Helper()
	ing, _, err := w.router.Open(context.Background(), ingress.Route{
		Name: "judged", Kind: ingress.RouteIssue, Opens: judgeKind, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key, Payload: []byte("do it")})
	if err != nil {
		t.Fatal(err)
	}
	return ing
}

// TestTwoJudgementsRunConcurrentlyUnderTheSharedBudget: dispatch does not
// wait on the run it starts, so Tick returns having only kicked two
// judgements off — and under MaxConcurrent 2 both decomposers reach
// Decompose at once rather than the second waiting for the first's slot.
func TestTwoJudgementsRunConcurrentlyUnderTheSharedBudget(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	judge := newFakeJudge()
	withJudge(t, w, judge)
	ctx := context.Background()

	w.openJudged(t, "j-1")
	w.openJudged(t, "j-2")

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// Tick already returned above — dispatch is fire-and-forget — and both
	// decomposers are in flight together, which only happens if the second
	// dispatch's Meter.Acquire succeeded while the first judgement still
	// held its slot.
	waitEntered(t, judge, 2)

	close(judge.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgements")
	}
	if n := judge.callCount(); n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
}

// TestAReadyUnitLandsWhileAJudgementIsInFlight: under MaxConcurrent 2, a
// judgement occupies one slot and an ordinary claimable item still gets the
// other — the meter it shares with Drain is the same account, not a second
// one that happens to look similar.
func TestAReadyUnitLandsWhileAJudgementIsInFlight(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	judge := newFakeJudge()
	withJudge(t, w, judge)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	w.openJudged(t, "j-1")
	item := w.submitItem(t, "issue-1", "fix module alpha", nil)

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitEntered(t, judge, 1)

	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := w.q.Load(ctx, tn, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == queue.StateDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the ready unit never landed while the judgement was in flight: %+v", got)
		}
		if _, err := w.pump.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("the judgement was invoked %d times while it was still in flight", n)
	}

	close(judge.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}
}

// TestAJudgementCountsAgainstTheBound: under MaxConcurrent 1, a judgement in
// flight holds the box's only slot — a tick claims nothing until it is
// released, and the unit runs immediately after.
func TestAJudgementCountsAgainstTheBound(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	judge := newFakeJudge()
	withJudge(t, w, judge)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	w.openJudged(t, "j-1")
	item := w.submitItem(t, "issue-1", "fix module alpha", nil)

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitEntered(t, judge, 1)

	if n, err := w.pump.Drain(ctx); err != nil || n != 0 {
		t.Fatalf("drain while the judgement holds the only slot: settled=%d err=%v", n, err)
	}
	got, err := w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != queue.StatePending {
		t.Fatalf("the item ran while the judgement held the box's only slot: %+v", got)
	}

	close(judge.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := w.q.Load(ctx, tn, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == queue.StateDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the unit never ran after the judgement released its slot: %+v", got)
		}
		if _, err := w.pump.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAFailedJudgementParksTheOriginationNotTheTick: any plan.Make error —
// dispatched or not — still fails the origination into the exception queue,
// and the tick that settles it returns no error of its own.
func TestAFailedJudgementParksTheOriginationNotTheTick(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	judge := newFakeJudge()
	judge.fail = errors.New("plan: the graph this judgement produced does not hold together")
	withJudge(t, w, judge)
	ctx := context.Background()

	ing := w.openJudged(t, "j-1")

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitEntered(t, judge, 1)
	close(judge.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}

	st, err := w.pump.Plans.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "failed" || !st.Blocked() {
		t.Fatalf("standing after a failed judgement: %+v", st)
	}
	found := false
	for rec, err := range w.store.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == ing.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("a failed judgement did not write an index/exceptions record")
	}

	// A later tick sees the origination is blocked, does not re-invoke the
	// decomposer, and still returns no error of its own.
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("a blocked origination was re-judged: calls=%d", n)
	}
}

// TestTheClaimLeaseDispatchesOnce: while the claim lease is held, another
// tick dispatches nothing — the decomposer is invoked exactly once and there
// is exactly one plan-claimed record on the chain.
func TestTheClaimLeaseDispatchesOnce(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	judge := newFakeJudge()
	withJudge(t, w, judge)
	ctx := context.Background()

	ing := w.openJudged(t, "j-1")

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitEntered(t, judge, 1)

	for range 3 {
		if _, err := w.pump.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-judge.entered:
		t.Fatal("the decomposer was invoked a second time while its claim lease was still held")
	default:
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}

	claims := 0
	for rec, err := range w.store.Log().Since(ctx, tn, ingress.Scope(ing.ID), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == plan.ClaimedKind {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("plan-claimed records = %d, want 1", claims)
	}

	close(judge.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched judgement")
	}
}

// TestQuiesceWaitsForAnInFlightJudgement: a judgement is a paid run exactly
// like a build, so shutdown must wait for it rather than abandon it mid-run.
func TestQuiesceWaitsForAnInFlightJudgement(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	judge := newFakeJudge()
	withJudge(t, w, judge)
	ctx := context.Background()

	w.openJudged(t, "j-1")
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitEntered(t, judge, 1)

	done := make(chan bool, 1)
	go func() { done <- w.pump.Quiesce(ctx) }()

	select {
	case <-done:
		t.Fatal("quiesce returned while the judgement was still in flight")
	case <-time.After(200 * time.Millisecond):
	}

	close(judge.release)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("quiesce reported it did not drain clean")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quiesce did not return after the judgement was released")
	}
}
