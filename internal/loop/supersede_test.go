package loop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// blockingBuilder signals started, then blocks until release delivers the
// error the incident's own stale builder eventually returned — a stand-in
// for a build still running long after an operator has already closed the
// item out from under it.
type blockingBuilder struct {
	started chan struct{}
	release chan error
}

func (b *blockingBuilder) Build(ctx context.Context, _ forge.Workspace, _ string) (forge.Commit, error) {
	close(b.started)
	select {
	case err := <-b.release:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestResolveSurvivesAStaleAbortedBuild is issue #266's own incident,
// reproduced without waiting out a real TTL: a build is still in flight when
// its claim is superseded — the same thing [queue.Queue.Recover]'s re-Claim
// does to a runner that stalled past its lease — and the operator resolves
// the item while that stale build is still running. When the stale build
// finally returns (here, an abort — the transport dropping the call, the
// loop's abort-retry path), the fence in [queue.Queue.Wait] must refuse to
// overwrite the operator's resolve back to waiting, and the loop must not
// resurrect it either.
func TestResolveSurvivesAStaleAbortedBuild(t *testing.T) {
	e := setup(t, []string{"true"})
	e.loop.TTL = 30 * time.Millisecond // short enough to supersede without waiting an hour
	ctx := context.Background()
	builder := &blockingBuilder{started: make(chan struct{}), release: make(chan error, 1)}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-supersede-1"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, err := q.Submit(ctx, ItemFor(ing, "wi-supersede-1", "do a thing"))
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		out queue.WorkItem
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, rerr := e.loop.Run(ctx, item)
		done <- result{out, rerr}
	}()

	select {
	case <-builder.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the build never started")
	}

	// The claim's nominal TTL lapses — the build is still running, exactly
	// the "still mine, just slow" case Inspect is built to tolerate — and a
	// successor takes it, precisely as Recover's own re-Claim would.
	time.Sleep(60 * time.Millisecond)
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: queue.ClaimRepo, Ref: queue.ClaimRef(item.ID), Run: item.ID, TTL: time.Hour,
	}); err != nil {
		t.Fatalf("superseding the stale claim: %v", err)
	}

	// The operator's own sequence: fail it, then resolve it — using the
	// lease the supersession above just granted, exactly as a fresh runner
	// legitimately would.
	failLease, ok, err := e.store.Leaser("runner-1").Inspect(ctx, tn, queue.ClaimRepo, queue.ClaimRef(item.ID))
	if err != nil || !ok {
		t.Fatalf("inspecting the superseding claim: ok=%v err=%v", ok, err)
	}
	q2 := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	failed, err := q2.Fail(ctx, item, failLease, queue.CauseChange, "operator killed it early")
	if err != nil {
		t.Fatalf("failing the item: %v", err)
	}
	if _, err := q2.Resolve(ctx, tn, failed.ID, queue.Identity{Name: "chris"}, "already fixed by hand"); err != nil {
		t.Fatalf("resolving the item: %v", err)
	}

	before := chainLen(t, e, item.ID)

	// Now the stale build finally returns — the abort the transport really
	// dropped, arriving long after the operator has moved on.
	builder.release <- &Aborted{Reason: "server error mid-response"}

	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the stale build's settle never returned — the seat never freed")
	}

	if !errors.Is(res.err, queue.ErrSuperseded) {
		t.Fatalf("loop.Run error = %v, want queue.ErrSuperseded", res.err)
	}
	// Before the fence, this is exactly where the incident's own bug lived:
	// item, _ = q.Wait(...) discarded the refusal, and the returned item
	// reported StateWaiting with a nil error even though nothing was ever
	// written — the false success the stale build's caller (the pump) acted
	// on.
	if res.out.State == queue.StateWaiting {
		t.Fatalf("the stale build's outcome reported waiting despite the refused write: %+v", res.out)
	}

	reloaded, err := q2.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State != queue.StateResolved {
		t.Fatalf("state after the stale build settled = %s, want resolved (the operator's resolve must survive)", reloaded.State)
	}

	// One narrative record naming the drop — nothing that changes what the
	// item folds to.
	after := chainLen(t, e, item.ID)
	if after != before+1 {
		t.Fatalf("chain grew by %d records settling a superseded build, want exactly 1 (a narrative record, no state transition)", after-before)
	}
}
