package pump

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// openAs admits one delivery under a named kind, which is the operator's
// routing decision: a route opens exactly one kind, so a scanner that
// sometimes files an issue and sometimes raises an incident is two routes.
func (w *world) openAs(t *testing.T, route, k, key, task string) ingress.Ingress {
	t.Helper()
	ing, _, err := w.router.Open(context.Background(), ingress.Route{
		Name: route, Kind: ingress.RouteIssue, Opens: ingress.Kind(k), Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key, Payload: []byte(task)})
	if err != nil {
		t.Fatal(err)
	}
	return ing
}

// TestEveryOriginationGetsAPlan is the collapse arriving at intake.
//
// Intake used to admit issues and step silently over everything else: a
// campaign advanced through its own engine and an incident produced no work at
// all, so two of the three kinds were an authority token and a bespoke engine
// wearing a work item's struct. Now every origination runs the decomposition
// step and emits a graph, and the graph is a recorded artifact between the
// ingress and its items.
func TestEveryOriginationGetsAPlan(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	ing := w.openAs(t, "issues", kind.SeedIssue, "issue-1", "fix module alpha")

	n, err := w.pump.Intake(ctx)
	if err != nil || n != 1 {
		t.Fatalf("intake: %d %v", n, err)
	}
	item, err := w.q.Load(ctx, tn, "wi-"+ing.ID)
	if err != nil {
		t.Fatalf("an ordinary issue was renamed by the decomposition stage: %v", err)
	}
	if item.Plan == "" || !strings.HasPrefix(item.Plan, "plan-") {
		t.Fatalf("the item does not name the decomposition that called for it: %q", item.Plan)
	}
	if item.Kind != kind.SeedIssue || item.Node != plan.IdentityNode {
		t.Fatalf("item: kind=%q node=%q", item.Kind, item.Node)
	}

	// The plan is on the origination's chain, and it was recorded BEFORE the
	// work: a graph whose items predate the plan that called for them is a
	// plan reconstructed from its consequences.
	p, found, err := w.pump.Plans.Latest(ctx, tn, ing.ID)
	if err != nil || !found {
		t.Fatalf("no plan recorded: %v %v", err, found)
	}
	if p.Ref != item.Plan || p.FanOut() != 1 || !p.Replayable {
		t.Fatalf("plan: %+v", p)
	}
	if p.Decomposer != kind.DecomposeIdentity {
		t.Fatalf("decomposer: %s", p.Decomposer)
	}

	// Idempotent on the PLAN, not merely on the item id. A decomposer over
	// moving facts would produce a second graph on the next tick and orphan
	// every item on the first from the decision that made it.
	if n, err := w.pump.Intake(ctx); err != nil || n != 0 {
		t.Fatalf("second intake created work: %d %v", n, err)
	}
	all, err := w.pump.Plans.All(ctx, tn, ing.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("plans after two ticks: %d %v", len(all), err)
	}
}

// TestAnIncidentIsAGraphTheQueueEnforces. This is the first time anything
// outside a test writes an edge: Phase 10 built and property-tested a graph
// engine — a diamond in topological order, a dead root cascading leaf by leaf
// — against a graph nothing in production could construct an edge for.
func TestAnIncidentIsAGraphTheQueueEnforces(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	ing := w.openAs(t, "pages", kind.SeedIncident, "page-1", "checkout is down")

	if n, err := w.pump.Intake(ctx); err != nil || n != 2 {
		t.Fatalf("intake: %d %v", n, err)
	}
	restore, err := w.q.Load(ctx, tn, "wi-"+ing.ID+"-restore")
	if err != nil {
		t.Fatal(err)
	}
	post, err := w.q.Load(ctx, tn, "wi-"+ing.ID+"-postmortem")
	if err != nil {
		t.Fatal(err)
	}
	if len(post.DependsOn) != 1 || post.DependsOn[0] != restore.ID {
		t.Fatalf("the postmortem does not follow restoration: %v", post.DependsOn)
	}
	if post.Plan != restore.Plan {
		t.Fatal("two nodes of one decomposition name different plans")
	}

	// The edge is enforced where it has always been enforced, by the queue.
	if _, _, err := w.q.Claim(ctx, post); !errors.Is(err, queue.ErrBlocked) {
		t.Fatalf("the postmortem was claimable before restoration: %v", err)
	}
	claimed, lease, err := w.q.Claim(ctx, restore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.q.Done(ctx, claimed, lease, "abc123"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.q.Claim(ctx, post); err != nil {
		t.Fatalf("restoration settled and the postmortem still blocked: %v", err)
	}
}

// TestAKindNobodyDeclaredAdmitsNothing. A configuration gap must be told, not
// guessed at: falling back to a built-in identity plan for an undeclared kind
// is precisely the silent widening of an envelope that made the closed enum a
// mistake in the first place.
func TestAKindNobodyDeclaredAdmitsNothing(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	ing := w.openAs(t, "scans", "security-advisory", "adv-1", "CVE-2026-0001 in a dependency")

	n, err := w.pump.Intake(ctx)
	if n != 0 {
		t.Fatalf("undeclared work was admitted: %d", n)
	}
	if err == nil || !errors.Is(err, kind.ErrUnknown) {
		t.Fatalf("wrong refusal: %v", err)
	}
	if _, err := w.q.Load(ctx, tn, "wi-"+ing.ID); !errors.Is(err, queue.ErrNoItem) {
		t.Fatalf("an item exists for an undeclared kind: %v", err)
	}

	// And one origination nobody can decompose is not a gate across the door:
	// an ordinary issue arriving beside it still gets in.
	good := w.openAs(t, "issues", kind.SeedIssue, "issue-2", "fix module beta")
	if _, err := w.pump.Intake(ctx); err == nil {
		t.Fatal("the undeclared origination stopped being reported")
	}
	if _, err := w.q.Load(ctx, tn, "wi-"+good.ID); err != nil {
		t.Fatalf("one broken origination shut the door on the rest: %v", err)
	}
}

// TestAStandingOriginationIsIntakesBusinessToLeaveAlone. A campaign is a
// reconciler: it is never enumerated up front, and intake decomposing it once
// would be exactly that enumeration.
func TestAStandingOriginationIsIntakesBusinessToLeaveAlone(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	ing := w.openAs(t, "schedules", kind.SeedCampaign, "camp-1", "bump go everywhere")

	n, err := w.pump.Intake(ctx)
	if err != nil || n != 0 {
		t.Fatalf("intake enumerated a campaign: %d %v", n, err)
	}
	if _, found, err := w.pump.Plans.Latest(ctx, tn, ing.ID); err != nil || found {
		t.Fatalf("intake planned a standing origination: %v %v", found, err)
	}
}
