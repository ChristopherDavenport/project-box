package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// closeStub is the loop's PRLander seam filled with a stub that reports
// GitHub answered a close request for every member it lands, when asked
// to — the same delegate-to-the-real-inner-Lander shape as [prStub], with
// one addition: it can stamp [forge.Placement.IssueClosed] on demand, which
// is what lets these tests exercise the LOOP's own reaction to that field
// (issue #108's [Loop.recordIssueClosed]) without a real GitHub call.
type closeStub struct {
	inner  *git.Lander
	closes bool
	cs     forge.ChangeSet
}

func (s *closeStub) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	s.cs = cs
	landing, err := s.inner.Land(ctx, cs)
	if err == nil && s.closes {
		for i := range landing.Landed {
			landing.Landed[i].IssueClosed = true
		}
	}
	return landing, err
}

func wireCloseStub(e *env, closes bool) *closeStub {
	stub := &closeStub{closes: closes}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander {
		stub.inner = inner
		return stub
	}
	return stub
}

// recordPlan seals and records a two-node, dependency-free plan under ing,
// then submits both of its items — returning them in declaration order so a
// test can run and inspect each independently.
func recordPlan(t *testing.T, e *env, ing ingress.Ingress) []queue.WorkItem {
	t.Helper()
	ctx := context.Background()
	p := plan.Plan{
		Ingress: ing.ID, Kind: string(ing.Kind), Decomposer: "test", Replayable: true,
		Nodes: []plan.Node{
			{ID: "a", Task: "unit a"},
			{ID: "b", Task: "unit b"},
		},
	}
	if err := p.Seal(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	ps := &plan.Store{Log: e.store.Log(), Actor: "test"}
	p, err := ps.Record(ctx, ing, p)
	if err != nil {
		t.Fatalf("record plan: %v", err)
	}
	items := plan.Items(ing, p)
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	for i, it := range items {
		submitted, err := q.Submit(ctx, it)
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		items[i] = submitted
	}
	return items
}

// TestCompletesPlanFalseUntilTheLastSiblingLands is issue #108's core
// assertion: CompletesPlan is the loop's own read of the plan's OTHER
// items, true only once every one of them has already settled
// [queue.StateDone].
func TestCompletesPlanFalseUntilTheLastSiblingLands(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()
	stub := wireCloseStub(e, false)

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-108a"})
	if err != nil {
		t.Fatal(err)
	}
	items := recordPlan(t, e, ing)

	first, err := e.loop.Run(ctx, items[0])
	if err != nil {
		t.Fatalf("run a: %v", err)
	}
	if first.State != queue.StateDone {
		t.Fatalf("unit a did not land: %+v", first)
	}
	if stub.cs.CompletesPlan {
		t.Fatal("CompletesPlan = true landing unit a, want false: unit b has not settled yet")
	}

	second, err := e.loop.Run(ctx, items[1])
	if err != nil {
		t.Fatalf("run b: %v", err)
	}
	if second.State != queue.StateDone {
		t.Fatalf("unit b did not land: %+v", second)
	}
	if !stub.cs.CompletesPlan {
		t.Fatal("CompletesPlan = false landing unit b, want true: every other unit of the plan is now done")
	}
}

// TestCompletesPlanTrueForAPlanlessItem is issue #108: an item with no
// plan — [ItemFor] builds exactly these, for work with no decomposition —
// is trivially the whole graph, so its own landing always completes it.
func TestCompletesPlanTrueForAPlanlessItem(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()
	stub := wireCloseStub(e, false)

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-108b"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-108b", "no plan here"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("did not land: %+v", item)
	}
	if !stub.cs.CompletesPlan {
		t.Fatal("CompletesPlan = false for a plan-less item, want true")
	}
}

// TestCompletesPlanFalseWhenASiblingIsParked is issue #108: a sibling that
// went wrong is not a sibling that settled, and an unknown graph must never
// read as complete.
func TestCompletesPlanFalseWhenASiblingIsParked(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()
	stub := wireCloseStub(e, false)

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-108c"})
	if err != nil {
		t.Fatal(err)
	}
	items := recordPlan(t, e, ing)

	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	claimedB, claim, err := q.Claim(ctx, items[1])
	if err != nil {
		t.Fatalf("claim b: %v", err)
	}
	if _, err := q.Park(ctx, claimedB, claim, queue.SeverityMedium, "simulated failure"); err != nil {
		t.Fatalf("park b: %v", err)
	}

	first, err := e.loop.Run(ctx, items[0])
	if err != nil {
		t.Fatalf("run a: %v", err)
	}
	if first.State != queue.StateDone {
		t.Fatalf("unit a did not land: %+v", first)
	}
	if stub.cs.CompletesPlan {
		t.Fatal("CompletesPlan = true, want false: sibling b is parked, not done")
	}
}

// TestIssueClosedRecordFollowsAConfirmedClose is issue #108 part 3: the
// item chain gains an "issue-closed" record only when landing confirms
// GitHub actually answered (forge.Placement.IssueClosed), and gains nothing
// when it did not.
func TestIssueClosedRecordFollowsAConfirmedClose(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()
	stub := wireCloseStub(e, true)

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-108d"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-108d", "closes on landing"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("did not land: %+v", item)
	}
	if stub.cs.DrivingIssue != "108d" {
		t.Fatalf("DrivingIssue = %q, want 108d", stub.cs.DrivingIssue)
	}

	found := false
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/wi-108d", 1) {
		if err != nil {
			t.Fatalf("since: %v", err)
		}
		if r.Kind == "issue-closed" {
			if !strings.Contains(string(r.Body), "108d") {
				t.Fatalf("issue-closed body %s does not name the issue", r.Body)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no issue-closed record on the item chain after a confirmed close")
	}
}

// TestNoIssueClosedRecordWithoutAConfirmedClose is
// [TestIssueClosedRecordFollowsAConfirmedClose]'s negative half: a Lander
// that never confirmed the close (a plain git.Lander, or a PRLander whose
// GitHub calls never answered) must never leave an issue-closed record
// behind — an unanswered close records nothing.
func TestNoIssueClosedRecordWithoutAConfirmedClose(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()
	wireCloseStub(e, false)

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-108e"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-108e", "never closes"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("did not land: %+v", item)
	}

	for r, err := range e.store.Log().Since(ctx, tn, "workitem/wi-108e", 1) {
		if err != nil {
			t.Fatalf("since: %v", err)
		}
		if r.Kind == "issue-closed" {
			t.Fatalf("issue-closed record found for a landing that never confirmed a close: %+v", r)
		}
	}
}
