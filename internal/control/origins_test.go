package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
)

// openOrigination admits one delivery through the real router, so its own
// index/ingress record and its RecordOpen carry the exact shapes
// [Room.Origins] reads — never a hand-authored substitute for the writer
// this fold depends on.
func openOrigination(t *testing.T, r *Room, key, title, note string) ingress.Ingress {
	t.Helper()
	router := &ingress.StoreRouter{Store: r.Store, Tenant: tn, Runner: "d"}
	route := ingress.Route{Name: "issues", Requester: "chris", Box: "shop", Repo: "app", Opens: "issue"}
	ing, _, err := router.Open(context.Background(), route, ingress.Delivery{Key: key, Title: title, Payload: []byte(note)})
	if err != nil {
		t.Fatal(err)
	}
	return ing
}

// recordPlan seals and records a plan the way intake does.
func recordPlan(t *testing.T, r *Room, ing ingress.Ingress, nodes ...plan.Node) plan.Plan {
	t.Helper()
	p := plan.Plan{Ingress: ing.ID, Kind: string(ing.Kind), Decomposer: "manual", Replayable: true, Nodes: nodes}
	if err := p.Seal(); err != nil {
		t.Fatal(err)
	}
	store := &plan.Store{Log: r.Store.Log(), Actor: "d"}
	p, err := store.Record(context.Background(), ing, p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// appendRec appends one evidence record with a JSON body, the way every real
// writer this fold reads from does — [forge.Verdict] marshalled directly for
// gate records, hand-shaped maps for the hand-formatted JSON bodies loop.go
// and git.go write.
func appendRec(t *testing.T, r *Room, scope, kind, subject, actor string, body any) evidence.Record {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: scope, Kind: kind, Subject: subject, Actor: actor, Body: b,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// twoUnitStory builds the issue's own fixture: a two-unit plan where the
// first unit's first attempt is refused by review, its second attempt
// passes and then rebases — carrying the review verdict forward rather than
// judging the replayed patch twice — and the second unit builds clean. Used
// by both the shape test and the fact-ref verification test so the two do
// not drift into different fixtures.
func twoUnitStory(t *testing.T) (*Room, OriginStory) {
	t.Helper()
	r, _ := setup(t)
	ctx := context.Background()

	ing := openOrigination(t, r, "issue-77", "checkout is broken", "fix checkout")
	p := recordPlan(t, r, ing,
		plan.Node{ID: "a", Task: "fix the cart", Summary: "unit a"},
		plan.Node{ID: "b", Task: "fix the receipt", Summary: "unit b"},
	)
	itemA, itemB := p.ItemID("a"), p.ItemID("b")
	scopeA, scopeB := "workitem/"+itemA, "workitem/"+itemB

	// Unit A, attempt 1: refused by review.
	appendRec(t, r, scopeA, "built", "shaA1", "runner-1", map[string]any{})
	appendRec(t, r, scopeA, "gate", "shaA1", "runner-1", forge.Verdict{Gate: "check", Decision: forge.DecisionPass})
	appendRec(t, r, scopeA, "gate", "shaA1", "runner-1", forge.Verdict{Gate: "review", Decision: forge.DecisionFail, Reason: "needs more tests"})
	ran(t, r, itemA, "mason", "ref-a1")
	agentResultOn(t, r, scopeA, "ref-a1", 1000, 100, 0.01)

	// Unit A, attempt 2: passes review, then rebases — the carry.
	appendRec(t, r, scopeA, "built", "shaA2", "runner-1", map[string]any{})
	appendRec(t, r, scopeA, "gate", "shaA2", "runner-1", forge.Verdict{Gate: "check", Decision: forge.DecisionPass})
	reviewRec := appendRec(t, r, scopeA, "gate", "shaA2", "runner-1", forge.Verdict{Gate: "review", Decision: forge.DecisionPass})
	ran(t, r, itemA, "mason", "ref-a2")
	agentResultOn(t, r, scopeA, "ref-a2", 1500, 200, 0.02)

	appendRec(t, r, scopeA, "build-rebased", "shaA2r", "runner-1",
		map[string]any{"was": "shaA2", "from": "base-old", "onto": "base-new"})
	appendRec(t, r, scopeA, "gate-carried", "shaA2r", "runner-1",
		map[string]any{"gate": "review", "reviewed": "shaA2", "original": reviewRec.ID, "patch_id": "pid-1"})

	appendRec(t, r, "changeset/cs-"+itemA, "land", "shaA2r", "runner-1",
		map[string]any{"repo": "app", "branch": "main", "was": "base-new", "now": "shaA2r"})
	appendRec(t, r, scopeA, "issue-closed", "shaA2r", "runner-1",
		map[string]any{"issue": "77", "commit": "shaA2r", "pr_url": "https://github.com/acme/app/pull/501", "closed_by": "forge"})

	// Unit B: one clean attempt, one landing, no review gate, no close.
	appendRec(t, r, scopeB, "built", "shaB1", "runner-1", map[string]any{})
	appendRec(t, r, scopeB, "gate", "shaB1", "runner-1", forge.Verdict{Gate: "check", Decision: forge.DecisionPass})
	ran(t, r, itemB, "mason", "ref-b1")
	agentResultOn(t, r, scopeB, "ref-b1", 800, 80, 0.008)
	appendRec(t, r, "changeset/cs-"+itemB, "land", "shaB1", "runner-1",
		map[string]any{"repo": "app", "branch": "main", "was": "base-new", "now": "shaB1"})

	stories, err := r.Origins(ctx, "issue-77")
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 {
		t.Fatalf("stories = %d, want 1", len(stories))
	}
	return r, stories[0]
}

// TestOriginsStoryFoldsAttemptsRebaseLandingAndClose is the issue's own first
// test: a two-unit plan with a refused first attempt and one rebase — the
// story has two units, three attempts, two review verdicts, one landing per
// unit, one close, and totals equal to the sum of the cost facts it lists.
func TestOriginsStoryFoldsAttemptsRebaseLandingAndClose(t *testing.T) {
	_, s := twoUnitStory(t)

	if len(s.Units) != 2 {
		t.Fatalf("units = %d, want 2", len(s.Units))
	}

	var attempts, reviews, landings, closes int
	var unitA UnitStory
	for _, u := range s.Units {
		attempts += len(u.Attempts)
		landings += len(u.Landings)
		if u.Closed != nil {
			closes++
		}
		for _, a := range u.Attempts {
			if a.Review != nil {
				reviews++
			}
		}
		if len(u.Rebases) > 0 {
			unitA = u
		}
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if reviews != 2 {
		t.Fatalf("review verdicts = %d, want 2", reviews)
	}
	if landings != 2 {
		t.Fatalf("landings = %d, want 2 (one per unit)", landings)
	}
	if closes != 1 {
		t.Fatalf("closes = %d, want 1", closes)
	}

	// The rebased attempt's review verdict is a carry, not a fresh
	// judgement, and its decision resolves from the same walk.
	if len(unitA.Attempts) != 2 {
		t.Fatalf("unit a attempts = %d, want 2", len(unitA.Attempts))
	}
	carried := unitA.Attempts[1].Review
	if carried == nil || !carried.Carried || carried.Decision != string(forge.DecisionPass) {
		t.Fatalf("carried review verdict = %+v", carried)
	}
	refused := unitA.Attempts[0].Review
	if refused == nil || refused.Carried || refused.Decision != string(forge.DecisionFail) {
		t.Fatalf("refused review verdict = %+v", refused)
	}

	wantTokens, wantWall := int64(380), int64(3300)
	wantCost := 0.038
	if s.Totals.Sessions != 3 || s.Totals.Covered != 3 {
		t.Fatalf("totals sessions/covered = %d/%d, want 3/3", s.Totals.Sessions, s.Totals.Covered)
	}
	if s.Totals.Tokens != wantTokens || s.Totals.WallMS != wantWall {
		t.Fatalf("totals tokens/wall = %d/%d, want %d/%d", s.Totals.Tokens, s.Totals.WallMS, wantTokens, wantWall)
	}
	if diff := s.Totals.CostUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("totals cost = %v, want %v", s.Totals.CostUSD, wantCost)
	}
	if s.Totals.Attempts != 3 {
		t.Fatalf("totals attempts = %d, want 3", s.Totals.Attempts)
	}
}

// TestOriginsResubmissionIsItsOwnOrigination is the issue's own second test:
// a key with two originations — a resubmission with a '-suffix' key counts
// as its own — both appear, newest first.
func TestOriginsResubmissionIsItsOwnOrigination(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()

	first := openOrigination(t, r, "issue-99", "first attempt", "n1")
	second := openOrigination(t, r, "issue-99-lander", "resubmitted", "n2")

	stories, err := r.Origins(ctx, "issue-99")
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 2 {
		t.Fatalf("stories = %d, want 2 (the origination and its resubmission)", len(stories))
	}
	if stories[0].IngressID != second.ID {
		t.Fatalf("stories[0] = %s, want the resubmission %s newest first", stories[0].IngressID, second.ID)
	}
	if stories[1].IngressID != first.ID {
		t.Fatalf("stories[1] = %s, want the original %s", stories[1].IngressID, first.ID)
	}
	if stories[0].Key != "issue-99-lander" || stories[1].Key != "issue-99" {
		t.Fatalf("keys = %q, %q", stories[0].Key, stories[1].Key)
	}

	// An unrelated key naming neither must not match by accident.
	if none, err := r.Origins(ctx, "issue-999"); err != nil || len(none) != 0 {
		t.Fatalf("issue-999 matched %d stories, want 0: %v", len(none), err)
	}
}

// TestOriginsFactRefsResolveOnTheChain is the issue's own third test: every
// fact line's (scope, seq) resolves via Log.Since to a chain record of the
// stated kind — an auditor's "show your work" checked mechanically rather
// than trusted.
func TestOriginsFactRefsResolveOnTheChain(t *testing.T) {
	r, s := twoUnitStory(t)
	ctx := context.Background()

	verify := func(label string, fr FactRef, wantKind string) {
		t.Helper()
		found := false
		for rec, err := range r.Store.Log().Since(ctx, tn, fr.Scope, fr.Seq) {
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if rec.Seq != fr.Seq {
				t.Fatalf("%s: Since(%d) yielded seq %d first", label, fr.Seq, rec.Seq)
			}
			if rec.Kind != wantKind {
				t.Fatalf("%s: chain record at (%s,%d) has kind %q, story claims %q", label, fr.Scope, fr.Seq, rec.Kind, wantKind)
			}
			if rec.Kind != fr.Kind || rec.Actor != fr.Actor || !rec.At.Equal(fr.At) {
				t.Fatalf("%s: FactRef %+v does not match chain record %+v", label, fr, rec)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("%s: (%s,%d) resolved to nothing", label, fr.Scope, fr.Seq)
		}
	}

	verify("ingress", s.Ingress.FactRef, "ingress-open")
	for i, pf := range s.Plans {
		verify("plan", pf.FactRef, "planned")
		_ = i
	}
	for _, u := range s.Units {
		for _, a := range u.Attempts {
			if a.Built != nil {
				verify("built", a.Built.FactRef, "built")
			}
			if a.Check != nil {
				kind := "gate"
				if a.Check.Carried {
					kind = "gate-carried"
				}
				verify("check", a.Check.FactRef, kind)
			}
			if a.Review != nil {
				kind := "gate"
				if a.Review.Carried {
					kind = "gate-carried"
				}
				verify("review", a.Review.FactRef, kind)
			}
		}
		for _, rb := range u.Rebases {
			verify("rebase", rb.FactRef, rb.Kind)
		}
		for _, l := range u.Landings {
			verify("land", l.FactRef, "land")
		}
		if u.Closed != nil {
			verify("closed", u.Closed.FactRef, "issue-closed")
		}
	}
}
