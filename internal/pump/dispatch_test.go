package pump

import (
	"context"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// blockingBuilder blocks Build for any task its gate accepts, until the test
// closes release — channel-synchronized like fakeJudge in judge_test.go,
// never a sleep. A task the gate declines builds normally through fixer,
// never touching entered or release at all: this is what lets one test run
// a genuinely blocked build alongside an ordinary one that lands quickly.
type blockingBuilder struct {
	gate    func(task string) bool
	entered chan struct{}
	release chan struct{}
}

func newBlockingBuilder(gate func(task string) bool) *blockingBuilder {
	return &blockingBuilder{gate: gate, entered: make(chan struct{}, 16), release: make(chan struct{})}
}

func (b *blockingBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if !b.gate(task) {
		return fixer{}.Build(ctx, ws, task)
	}
	b.entered <- struct{}{}
	select {
	case <-b.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return fixer{}.Build(ctx, ws, task)
}

// blockAll gates every task, for tests that only need one build in flight
// at a time and do not care which task it was.
func blockAll(string) bool { return true }

// waitBuildEntered blocks until n calls have reached blockingBuilder.Build
// and are waiting on release, failing the test rather than hanging forever
// if dispatch never happened.
func waitBuildEntered(t *testing.T, b *blockingBuilder, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-b.entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for build %d/%d to enter", i+1, n)
		}
	}
}

// TestDrainDispatchesAndReturnsWhileABuildBlocks is issue #196's own property,
// direct: Drain claims and dispatches, then returns — it does not wait for
// the run it just started. Tick 1 dispatches A into a build that blocks;
// with A still blocked and MaxConcurrent 2, tick 2 returns promptly and
// dispatches B alongside it. The property under test is not that B
// eventually runs — every test in this package already assumes dispatch
// eventually finishes — it is that tick 2 RETURNED while A was still
// blocked: a Drain that still waited on its own dispatched runs, as it did
// before this issue, would never reach tick 2's dispatch of B in time for
// this to observe it at all.
func TestDrainDispatchesAndReturnsWhileABuildBlocks(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	blocker := newBlockingBuilder(blockAll)
	for _, lp := range w.pump.Loops {
		lp.Builder = blocker
	}

	w.open(t, "issue-a", "fix module a")
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitBuildEntered(t, blocker, 1) // A is now blocked mid-build

	w.open(t, "issue-b", "fix module b")
	tick2Done := make(chan struct{})
	go func() {
		if _, err := w.pump.Tick(ctx); err != nil {
			t.Error(err)
		}
		close(tick2Done)
	}()
	select {
	case <-tick2Done:
	case <-time.After(5 * time.Second):
		t.Fatal("tick 2 did not return promptly — Drain waited on A's still-blocked build")
	}
	// Nothing has released A: it is provably still blocked at the moment
	// tick 2 already returned above.
	waitBuildEntered(t, blocker, 1) // B, dispatched by tick 2, has reached Build too

	// Releasing both at once lets them race for the same branch lease — one
	// branch lease admits one run at a time (issue #18), so the loser lands
	// waiting, needing a re-claim, not a failure. Settle, then drain the one
	// extra pass that re-claims it, quiescing between each — same reasoning
	// as api_test.go's own passes over a shared branch.
	close(blocker.release)
	for range 2 {
		if !w.pump.Quiesce(ctx) {
			t.Fatal("quiesce did not settle the pass's dispatched build")
		}
		if _, err := w.pump.Drain(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle both dispatched builds")
	}
	for _, key := range []string{"issue-a", "issue-b"} {
		ing, _, err := w.router.Open(ctx, ingress.Route{
			Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
		}, ingress.Delivery{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		item, err := w.q.Load(ctx, tn, "wi-"+ing.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.State != queue.StateDone {
			t.Fatalf("%s: %+v", key, item)
		}
	}
}

// TestQuiesceWaitsForAnInFlightBuild mirrors
// TestQuiesceWaitsForAnInFlightJudgement for the run side of issue #196: a
// build is a paid run exactly like a judgement, so shutdown must wait for it
// rather than abandon it mid-run.
func TestQuiesceWaitsForAnInFlightBuild(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()
	blocker := newBlockingBuilder(blockAll)
	for _, lp := range w.pump.Loops {
		lp.Builder = blocker
	}

	w.open(t, "issue-a", "fix module a")
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitBuildEntered(t, blocker, 1)

	done := make(chan bool, 1)
	go func() { done <- w.pump.Quiesce(ctx) }()

	select {
	case <-done:
		t.Fatal("quiesce returned while the build was still in flight")
	case <-time.After(200 * time.Millisecond):
	}

	close(blocker.release)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("quiesce reported it did not drain clean")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quiesce did not return after the build was released")
	}
}

// TestACascadeDependentIsReleasedOnTheTickAfterItsDependencyLands is issue
// #196's third property. Before this fix, Drain would not return from the
// tick that dispatched a blocking build until that build finished, so a
// dependent waiting only on a different, already-finished sibling could not
// even be CONSIDERED until the tick after the whole burst — the still-
// blocked build included — settled. Now a tick claims the dependent as soon
// as its own dependency has landed, with the unrelated blocking build still
// genuinely in flight underneath it.
func TestACascadeDependentIsReleasedOnTheTickAfterItsDependencyLands(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	blocker := newBlockingBuilder(func(task string) bool { return task == "fix module a" })
	for _, lp := range w.pump.Loops {
		lp.Builder = blocker
	}

	b := w.submitItem(t, "b-1", "fix module b", nil)
	c := w.submitItem(t, "c-1", "fix module c", func(wi *queue.WorkItem) {
		wi.DependsOn = []string{b.ID}
	})
	a := w.submitItem(t, "a-1", "fix module a", nil)

	// One drain claims and dispatches all three candidates: A blocks; B,
	// disjoint from the gate, is free to build and land; C's own Claim
	// refuses on B's unsettled dependency (queue.ErrBlocked), a dispatch-time
	// refusal Drain has always handled synchronously, so C is simply left
	// pending for a later pass.
	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	waitBuildEntered(t, blocker, 1) // A is now blocked mid-build

	waitState(t, w, b.ID, queue.StateDone, "B never landed while A was blocked")

	// A is still genuinely in flight: nothing has released it yet.
	if got, err := w.q.Load(ctx, tn, a.ID); err != nil || got.State != queue.StateClaimed {
		t.Fatalf("A should still be running, got %+v %v", got, err)
	}

	// The tick right after B lands claims C — not the tick after A, still
	// blocked, eventually settles.
	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	waitState(t, w, c.ID, queue.StateDone, "C was never claimed once its dependency landed")

	if got, err := w.q.Load(ctx, tn, a.ID); err != nil || got.State != queue.StateClaimed {
		t.Fatalf("A should still have been running while C claimed and landed, got %+v %v", got, err)
	}

	close(blocker.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle A after it was released")
	}
}

// waitState polls id's state until it reaches want, failing with msg if a
// generous deadline passes first — used where the property under test is
// deliberately decoupled from some OTHER, still-running item, so a Quiesce
// (which would wait on that other item too) is the wrong barrier.
func waitState(t *testing.T, w *world, id string, want queue.State, msg string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := w.q.Load(context.Background(), tn, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %+v", msg, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
