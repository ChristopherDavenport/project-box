package pump

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// buildTally counts concurrent Build invocations across two different
// [forge.Builder] values sharing one tally, so a redispatch's peak
// concurrency is measured against the run it superseded rather than in
// isolation — the assertion that matters is "the two never overlapped", not
// "each one alone ran serially".
type buildTally struct {
	running int32
	peak    int32
}

func (b *buildTally) enter() (leave func()) {
	n := atomic.AddInt32(&b.running, 1)
	for {
		peak := atomic.LoadInt32(&b.peak)
		if n <= peak || atomic.CompareAndSwapInt32(&b.peak, peak, n) {
			break
		}
	}
	return func() { atomic.AddInt32(&b.running, -1) }
}

// countingBuilder wraps a [forge.Builder] to report every Build into a
// shared [buildTally].
type countingBuilder struct {
	tally *buildTally
	inner interface {
		Build(context.Context, forge.Workspace, string) (forge.Commit, error)
	}
}

func (c countingBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	leave := c.tally.enter()
	defer leave()
	return c.inner.Build(ctx, ws, task)
}

// TestCancelBuildStopsASupersededRun is issue #270's pump-level assertion:
// a build still running when its claim is superseded must be STOPPED, not
// merely outrun by the fence. [queue.Queue.Recover] is the supersede here —
// the #211 collision, a runner stalled past its TTL — and it reaches the
// dispatched goroutine only because [Pump.ensureWired] installed
// p.CancelBuild as p.Queue.Interrupt.
//
// Three things must all hold: the blocked builder's ctx is actually
// canceled — proved indirectly, since [blockingBuilder] only unblocks on
// b.release or ctx.Done(), and nothing here ever closes b.release, so a
// prompt, bounded Quiesce is possible only if the cancel reached it — the
// meter seat the canceled run held is released (a subsequent Acquire under
// MaxConcurrent 1 succeeds), and the successor that redispatches on the
// next tick never overlaps the run it replaced (a shared [buildTally]
// proves peak concurrency of one across both).
func TestCancelBuildStopsASupersededRun(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()

	tally := &buildTally{}
	blocker := newBlockingBuilder(blockAll)
	w.pump.Loops["shop"].Builder = countingBuilder{tally: tally, inner: blocker}

	item := w.submitItem(t, "issue-1", "fix module alpha", nil)

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitBuildEntered(t, blocker, 1)

	claimed, err := w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.State != queue.StateClaimed {
		t.Fatalf("expected the dispatched item to be claimed, got %s", claimed.State)
	}

	// The store's own clock, not the wall clock: the claim's TTL is real
	// (an hour, [setup]'s default), and jumping it forward is what proves
	// this is the stalled-runner case (#211) rather than a live one —
	// Recover's own Acquire only supersedes a lease past its expiry.
	w.advance(2 * time.Hour)

	if _, ok, err := w.pump.Queue.Recover(ctx, claimed); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("recover did not supersede the stalled claim")
	}

	// b.release is never closed: the only way the blocked build ends is its
	// ctx firing. A bounded Quiesce that still succeeds is the cancel.
	qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
	defer qcancel()
	if !w.pump.Quiesce(qctx) {
		t.Fatal("quiesce did not settle the canceled run — the build's context was never canceled")
	}

	// The deferred release() ran: under MaxConcurrent 1, a fresh Acquire
	// only succeeds if the canceled run actually gave its seat back.
	release, err := w.meter.Acquire(ctx, "probe")
	if err != nil {
		t.Fatalf("meter seat was not released after the cancel: %v", err)
	}
	release()

	// The successor's own dispatch, sharing tally with the run it replaced.
	w.pump.Loops["shop"].Builder = countingBuilder{tally: tally, inner: fixer{}}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the redispatch")
	}

	final, err := w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != queue.StateDone {
		t.Fatalf("expected the recovered item to complete on redispatch, got %+v", final)
	}
	if peak := atomic.LoadInt32(&tally.peak); peak > 1 {
		t.Fatalf("two builders ran concurrently on the same item (peak=%d)", peak)
	}
}

// TestCancelBuildNoOpsOnAnUnregisteredID is the miss case [Pump.CancelBuild]
// documents: settled work, or an id nothing ever dispatched, must not panic
// or block.
func TestCancelBuildNoOpsOnAnUnregisteredID(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	done := make(chan struct{})
	go func() {
		w.pump.CancelBuild("no-such-item")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelBuild on an unregistered id did not return")
	}
}
