package queue

import (
	"context"
	"encoding/json"
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
	var order []string
	done := map[string]bool{}

	var wg sync.WaitGroup
	for w := range 3 { // three concurrent runners racing the same graph
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner := &Queue{Store: qu.Store, Runner: "runner-" + string(rune('a'+w)), TTL: time.Hour}
			for range 40 {
				mu.Lock()
				if len(order) == 4 {
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
					order = append(order, pick.ID)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(order) != 4 {
		t.Fatalf("graph did not drain: %v", order)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	for _, edge := range [][2]string{{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"}} {
		if pos[edge[0]] > pos[edge[1]] {
			t.Fatalf("edge %s->%s violated: order %v", edge[0], edge[1], order)
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

// TestDoneReleasesDependentsParkedByIt: the counterpart to
// TestFailedRootParksTheLeaves — once the cause of a cascade park lands, the
// dependents it parked come back, each carrying the record of why.
func TestDoneReleasesDependentsParkedByIt(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	root := submit(t, qu, "root")
	b := submit(t, qu, "B", "root")
	c := submit(t, qu, "C", "root")

	w, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "gate failed hard"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := qu.Claim(ctx, b); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("B should cascade-park: %v", err)
	}
	if _, _, err := qu.Claim(ctx, c); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("C should cascade-park: %v", err)
	}
	gotB, _ := qu.Load(ctx, tn, "B")
	if gotB.State != StateParked || gotB.ParkedBy != "root" {
		t.Fatalf("B: %+v", gotB)
	}
	gotC, _ := qu.Load(ctx, tn, "C")
	if gotC.State != StateParked || gotC.ParkedBy != "root" {
		t.Fatalf("C: %+v", gotC)
	}

	// root lands.
	w, lease, err = qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Done(ctx, w, lease, forge.Commit("sha-root")); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"B", "C"} {
		got, err := qu.Load(ctx, tn, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != StatePending {
			t.Fatalf("%s should be released to pending, got: %+v", id, got)
		}
		if got.ParkedBy != "" {
			t.Fatalf("%s should have ParkedBy cleared, got: %+v", id, got)
		}
		found := false
		for rec, err := range qu.Store.Log().Since(ctx, tn, scope(id), 1) {
			if err != nil {
				t.Fatal(err)
			}
			if rec.Kind == "retried" && string(rec.Body) != "" {
				var reopened WorkItem
				if err := json.Unmarshal(rec.Body, &reopened); err != nil {
					t.Fatal(err)
				}
				if reopened.Note == "dependency root landed" {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("%s: expected a retried record noting the dependency landed", id)
		}
	}
}

// TestDoneLeavesAnUnrelatedParkUntouched: an item parked for its own gate
// failure — never a cascade — must not be disturbed when unrelated work
// reaches Done.
func TestDoneLeavesAnUnrelatedParkUntouched(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	standalone := submit(t, qu, "standalone")
	other := submit(t, qu, "other")

	w, lease, err := qu.Claim(ctx, standalone)
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

	got, err := qu.Load(ctx, tn, "standalone")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateParked || got.Note != "its own gate failed" {
		t.Fatalf("standalone should stay parked for its own reason, got: %+v", got)
	}
}

// TestExceptionsIndexDropsReleasedItems mirrors the fold internal/control's
// Room.Exceptions performs: after release, an item's current state is
// StatePending, so folding by current state no longer surfaces it even
// though the exceptions index still remembers the original parking.
func TestExceptionsIndexDropsReleasedItems(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	root := submit(t, qu, "root")
	leaf := submit(t, qu, "leaf", "root")

	w, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "gate failed hard"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, leaf); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("leaf should cascade-park: %v", err)
	}

	w, lease, err = qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Done(ctx, w, lease, forge.Commit("sha-root")); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	stillException := map[string]bool{}
	for rec, err := range qu.Store.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		got, err := qu.Load(ctx, tn, rec.Subject)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == StateParked {
			stillException[rec.Subject] = true
		}
	}
	if stillException["leaf"] {
		t.Fatalf("leaf was released and must no longer fold as an exception")
	}
}

// TestReparkAfterRetryLeavesDependentsAlone: root parks, cascades to B,
// gets retried, and parks again for a different reason — release only ever
// fires from Done, so parking root a second time must leave B exactly where
// the first cascade left it.
func TestReparkAfterRetryLeavesDependentsAlone(t *testing.T) {
	qu := q(t)
	ctx := context.Background()
	root := submit(t, qu, "root")
	b := submit(t, qu, "B", "root")

	w, lease, err := qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "first failure"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, b); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("B should cascade-park: %v", err)
	}

	// root retried and parks again, for a different reason — never landing.
	if _, err := qu.Retry(ctx, tn, "root", "operator", "manual retry"); err != nil {
		t.Fatal(err)
	}
	w, lease, err = qu.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Park(ctx, w, lease, SeverityHigh, "second, unrelated failure"); err != nil {
		t.Fatal(err)
	}

	gotB, err := qu.Load(ctx, tn, "B")
	if err != nil {
		t.Fatal(err)
	}
	if gotB.State != StateParked || gotB.ParkedBy != "root" {
		t.Fatalf("B must stay untouched by root's second park (never a Done), got: %+v", gotB)
	}
}
