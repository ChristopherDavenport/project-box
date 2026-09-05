package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestRetryReachesAFailedPlan: the one exception that exists before any item
// does takes the same verbs, over the same route, and answers in the item's
// shape.
func TestRetryReachesAFailedPlan(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	w.srv.Plans = &plan.Store{Log: w.srv.Store.Log(), Actor: "d"}
	ing, _, err := w.srv.Router.Open(ctx, w.srv.Routes["issues"], ingress.Delivery{Key: "issue-plan", Payload: []byte("plan me")})
	if err != nil {
		t.Fatal(err)
	}
	_ = kind.SeedIssue
	if err := w.srv.Plans.Fail(ctx, ing, "session limit"); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	exs, err := c.Exceptions(ctx)
	if err != nil || len(exs) != 1 || exs[0].ID != ing.ID {
		t.Fatalf("the failed plan is not on the wire as an exception: %+v %v", exs, err)
	}
	item, err := c.Reopen(ctx, "retry", ing.ID, "budget is back")
	if err != nil || item.ID != ing.ID || item.State != queue.StatePending {
		t.Fatalf("retry over the wire: %+v %v", item, err)
	}
	st, err := w.srv.Plans.Standing(ctx, tn, ing.ID)
	if err != nil || st.State != "retried" {
		t.Fatalf("standing after retry: %+v %v", st, err)
	}
	if exs, _ := c.Exceptions(ctx); len(exs) != 0 {
		t.Fatalf("still an exception after retry: %+v", exs)
	}
}

// TestIngressAnswersWhereTheItemPage404s is issue #47's backend half: GET
// /v1/items/{id} 404s on an ing- id today because it only ever calls
// Queue.Load, and a judgement's card has nowhere honest to link. This is the
// route that answers instead — the origination's own facts, not the queue's.
func TestIngressAnswersWhereTheItemPage404s(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	w.srv.Plans = &plan.Store{Log: w.srv.Store.Log(), Actor: "d"}
	ing, _, err := w.srv.Router.Open(ctx, w.srv.Routes["issues"], ingress.Delivery{Key: "issue-judge", Payload: []byte("decompose this")})
	if err != nil {
		t.Fatal(err)
	}
	_ = kind.SeedIssue

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	view, err := c.Ingress(ctx, ing.ID)
	if err != nil {
		t.Fatalf("GET /v1/ingress/%s: %v", ing.ID, err)
	}
	if view.ID != ing.ID || view.Box != ing.Box || view.Repo != ing.Repo {
		t.Fatalf("origination facts: %+v, want %+v", view, ing)
	}
	if view.Standing != "" {
		t.Fatalf("standing = %q before any planning event, want empty", view.Standing)
	}

	if _, err := c.Ingress(ctx, "ing-nonesuch"); err == nil {
		t.Fatal("an unknown ingress id was accepted")
	}
}

// TestPlanGraphOverTheWire: the read-only surface issue #13's decomposition
// stage never grew. Two sealed plans for the same origination, recorded on
// its chain — the fan-out and a diagram over the socket for the newer one,
// the listing carrying both oldest-first, and the diff between them naming
// exactly what a decomposer over moving facts changed.
func TestPlanGraphOverTheWire(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	w.srv.Plans = &plan.Store{Log: w.srv.Store.Log(), Actor: "d"}
	ing, _, err := w.srv.Router.Open(ctx, w.srv.Routes["issues"], ingress.Delivery{Key: "issue-graph", Payload: []byte("plan the graph")})
	if err != nil {
		t.Fatal(err)
	}
	_ = kind.SeedIssue

	p1 := plan.Plan{Ingress: ing.ID, Kind: string(ing.Kind), Decomposer: "manual", Replayable: true,
		Nodes: []plan.Node{
			{ID: "one", Task: "do one"},
			{ID: "two", Task: "do two"},
		}}
	if err := p1.Seal(); err != nil {
		t.Fatal(err)
	}
	if p1, err = w.srv.Plans.Record(ctx, ing, p1); err != nil {
		t.Fatal(err)
	}

	p2 := plan.Plan{Ingress: ing.ID, Kind: string(ing.Kind), Decomposer: "manual", Replayable: true,
		Nodes: []plan.Node{
			{ID: "one", Task: "do one, revised"},
			{ID: "three", Task: "do three"},
		}}
	if err := p2.Seal(); err != nil {
		t.Fatal(err)
	}
	if p2, err = w.srv.Plans.Record(ctx, ing, p2); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	view, err := c.Plan(ctx, p2.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if view.FanOut != 2 {
		t.Fatalf("fan-out: %d", view.FanOut)
	}
	if view.SVG == "" {
		t.Fatal("no diagram")
	}
	if view.Ref != p2.Ref {
		t.Fatalf("wrong plan: %+v", view.Plan)
	}

	summaries, err := c.IngressPlans(ctx, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].Ref != p1.Ref || summaries[1].Ref != p2.Ref {
		t.Fatalf("plans are not listed oldest-first: %+v", summaries)
	}

	delta, err := c.PlanDiff(ctx, p1.Ref, p2.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Added) != 1 || delta.Added[0].ID != "three" {
		t.Fatalf("added: %+v", delta.Added)
	}
	if len(delta.Removed) != 1 || delta.Removed[0].ID != "two" {
		t.Fatalf("removed: %+v", delta.Removed)
	}
	if len(delta.Changed) != 1 || delta.Changed[0].ID != "one" || delta.Changed[0].After.Task != "do one, revised" {
		t.Fatalf("changed: %+v", delta.Changed)
	}

	if _, err := c.Plan(ctx, "plan-nonesuch"); err == nil {
		t.Fatal("an unknown ref was accepted by /v1/plans/{ref}")
	}
	if _, err := c.PlanDiff(ctx, "plan-nonesuch", p2.Ref); err == nil {
		t.Fatal("an unknown ref was accepted as the diff's first plan")
	}
	if _, err := c.PlanDiff(ctx, p1.Ref, "plan-nonesuch"); err == nil {
		t.Fatal("an unknown ref was accepted as the diff's second plan")
	}
}
