package queue

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func q(t *testing.T) *Queue {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
}

func submit(t *testing.T, q *Queue, id string, deps ...string) WorkItem {
	t.Helper()
	w, err := q.Submit(context.Background(), WorkItem{
		ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: id, DependsOn: deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestDiamondExecutesInTopologicalOrder is the Phase 10 exit criterion:
// A -> (B, C) -> D under concurrent runners, order emerging from claims
// against the graph rather than from anyone's plan.
func TestDiamondExecutesInTopologicalOrder(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	items := map[string]WorkItem{
		"A": submit(t, qu, "A"),
		"B": submit(t, qu, "B", "A"),
		"C": submit(t, qu, "C", "A"),
		"D": submit(t, qu, "D", "B", "C"),
	}

	var mu sync.Mutex
	done := map[string]bool{}
	drained := 0

	var wg sync.WaitGroup
	for w := range 3 { // three concurrent runners racing the same graph
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner := &Queue{Store: qu.Store, Runner: "runner-" + string(rune('a'+w)), TTL: time.Hour}
			for range 40 {
				mu.Lock()
				if drained == 4 {
					mu.Unlock()
					return
				}
				var pick WorkItem
				found := false
				for id, it := range items {
					if !done[id] {
						pick, found = it, true
						break
					}
				}
				mu.Unlock()
				if !found {
					return
				}
				claimed, lease, err := runner.Claim(ctx, pick)
				if errors.Is(err, ErrBlocked) || errors.Is(err, forge.ErrHeld) {
					continue // not its turn, or another runner has it
				}
				if err != nil {
					t.Errorf("claim %s: %v", pick.ID, err)
					return
				}
				if _, err := runner.Done(ctx, claimed, lease, forge.Commit("sha-"+pick.ID)); err != nil {
					t.Errorf("done %s: %v", pick.ID, err)
					return
				}
				mu.Lock()
				if !done[pick.ID] {
					done[pick.ID] = true
					drained++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// The order goroutines happened to reach a mutex is not the order the
	// queue produced. Read that back from each item's own chain instead:
	// Claim appends "claimed" and Done appends "done" to scope(id), and the
	// store stamps At at Append time, so At (not Seq, which only orders
	// within one scope) is what lets records from different items compare.
	firstAt := func(id, kind string) time.Time {
		t.Helper()
		for rec, err := range qu.Store.Log().Since(ctx, tn, scope(id), 0) {
			if err != nil {
				t.Fatalf("read %s chain: %v", id, err)
			}
			if rec.Kind == kind {
				return rec.At
			}
		}
		t.Fatalf("%s has no %q record", id, kind)
		return time.Time{}
	}

	for id := range items {
		firstAt(id, "claimed")
		firstAt(id, "done")
	}
	for _, edge := range [][2]string{{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"}} {
		doneX := firstAt(edge[0], "done")
		claimedY := firstAt(edge[1], "claimed")
		if claimedY.Before(doneX) {
			t.Fatalf("edge %s->%s violated: %s claimed at %s before %s done at %s",
				edge[0], edge[1], edge[1], claimedY, edge[0], doneX)
		}
	}
}

// TestFailedRootParksTheLeaves: the other half — a parked dependency
// cascades, and every leaf names its cause.
func TestFailedRootParksTheLeaves(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	root := submit(t, qu, "root")
	mid := submit(t, qu, "mid", "root")
	leaf := submit(t, qu, "leaf", "mid")

	w, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "gate failed hard"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := qu.Claim(ctx, mid); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("mid should cascade-park: %v", err)
	}
	got, err := qu.Load(ctx, tn, "mid")
	if err != nil || got.State != StateParked || !strings.Contains(got.Note, "root parked") {
		t.Fatalf("mid: %+v %v", got, err)
	}
	// And the leaf, whose direct dependency is now the parked mid.
	if _, _, err := qu.Claim(ctx, leaf); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("leaf should cascade-park: %v", err)
	}
	got, _ = qu.Load(ctx, tn, "leaf")
	if got.State != StateParked || !strings.Contains(got.Note, "mid parked") {
		t.Fatalf("leaf must name ITS cause (mid), got: %+v", got)
	}
	// A pending (not failed) dependency merely blocks.
	late := submit(t, qu, "late")
	dep := submit(t, qu, "dep-on-late", "late")
	if _, _, err := qu.Claim(ctx, dep); !errors.Is(err, ErrBlocked) {
		t.Fatalf("pending dep should block, not fail: %v", err)
	}
	_ = late
}

// TestDoneReleasesCascadeParkedDependents is issue #56: a dependent parked by
// cascade stayed parked forever, even after the dependency it named as cause
// later landed. A diamond — root parking both b and c — should see both
// released back to pending the moment root reaches Done, each carrying a
// "retried" record naming root as the reason.
func TestDoneReleasesCascadeParkedDependents(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	root := submit(t, qu, "root")
	b := submit(t, qu, "b", "root")
	c := submit(t, qu, "c", "root")

	w, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "gate failed hard"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, b); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("b should cascade-park: %v", err)
	}
	if _, _, err := qu.Claim(ctx, c); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("c should cascade-park: %v", err)
	}
	for _, id := range []string{"b", "c"} {
		got, err := qu.Load(ctx, tn, id)
		if err != nil || got.State != StateParked || got.ParkedBy != "root" {
			t.Fatalf("%s: %+v %v", id, got, err)
		}
	}

	// root is retried and lands this time.
	w, err = qu.Retry(ctx, tn, "root", Identity{Name: "operator"}, "flaky gate, retrying")
	if err != nil {
		t.Fatal(err)
	}
	w, lease, err = qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Done(ctx, w, lease, forge.Commit("sha-root")); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"b", "c"} {
		got, err := qu.Load(ctx, tn, id)
		if err != nil || got.State != StatePending {
			t.Fatalf("%s should be released to pending: %+v %v", id, got, err)
		}
		if got.Note != "dependency root landed" {
			t.Fatalf("%s note: %q", id, got.Note)
		}
		if got.ParkedBy != "" {
			t.Fatalf("%s ParkedBy should be cleared on release: %q", id, got.ParkedBy)
		}
	}

	// The exception index, folded by current state the way
	// internal/control.Room.Exceptions does, no longer surfaces b or c.
	for rec, err := range qu.Store.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject != "b" && rec.Subject != "c" {
			continue
		}
		got, err := qu.Load(ctx, tn, rec.Subject)
		if err != nil || got.State == StateParked {
			t.Fatalf("%s still reads as an open exception after release: %+v %v", rec.Subject, got, err)
		}
	}
}

// TestDoneLeavesSelfParkedItemAlone: an item parked for its own gate failure
// (not a cascade) must not be disturbed when an unrelated item reaches Done.
func TestDoneLeavesSelfParkedItemAlone(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	other := submit(t, qu, "other")
	leaf := submit(t, qu, "leaf")

	w, lease, err := qu.Claim(ctx, leaf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "its own gate failed"); err != nil {
		t.Fatal(err)
	}

	w, lease, err = qu.Claim(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Done(ctx, w, lease, forge.Commit("sha-other")); err != nil {
		t.Fatal(err)
	}

	got, err := qu.Load(ctx, tn, "leaf")
	if err != nil || got.State != StateParked || got.Note != "its own gate failed" {
		t.Fatalf("leaf parked for its own reasons should be untouched: %+v %v", got, err)
	}
}

// TestParkAfterReleaseDoesNotRecascade: the release only fires on Done. If a
// retried root parks again (for a different reason) instead of landing, an
// already-released dependent is left exactly as it was — demonstrating the
// fix does not fire on anything but Done.
func TestParkAfterReleaseDoesNotRecascade(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	root := submit(t, qu, "root")
	b := submit(t, qu, "b", "root")

	w, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "first failure"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, b); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("b should cascade-park: %v", err)
	}
	before, err := qu.Load(ctx, tn, "b")
	if err != nil {
		t.Fatal(err)
	}

	w, err = qu.Retry(ctx, tn, "root", Identity{Name: "operator"}, "retry")
	if err != nil {
		t.Fatal(err)
	}
	w, lease, err = qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "second failure, different cause"); err != nil {
		t.Fatal(err)
	}

	after, err := qu.Load(ctx, tn, "b")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != before.State || after.Note != before.Note || after.ParkedBy != before.ParkedBy {
		t.Fatalf("b should be untouched by root parking again (not Done): before %+v, after %+v", before, after)
	}
}
