package control

import (
	"context"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// openWithSource is openOrigination with a Source on the delivery — the two
// diverge only in that one field, so a test comparing a sourced origination
// against an unsourced one is comparing the fold's own handling of Source,
// not two differently-shaped originations.
func openWithSource(t *testing.T, r *Room, key, title string, src ingress.Source) ingress.Ingress {
	t.Helper()
	router := &ingress.StoreRouter{Store: r.Store, Tenant: tn, Runner: "d"}
	route := ingress.Route{Name: "issues", Requester: "chris", Box: "shop", Repo: "app", Opens: "issue"}
	ing, _, err := router.Open(context.Background(), route, ingress.Delivery{Key: key, Title: title, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	return ing
}

// TestQueueEntryCarriesTheSourceOfItsOrigination: the fold that already
// copies Route and Key onto a QueueEntry (control.go's entry) must copy
// Source alongside them, and an ingress opened before sources were recorded
// must not fabricate one.
func TestQueueEntryCarriesTheSourceOfItsOrigination(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	withSrc := openWithSource(t, r, "issue-1", "has a source", ingress.Source{
		Subject: "alice", Method: "token", Channel: "route:issues", Run: "",
	})
	withoutSrc := openWithSource(t, r, "issue-2", "has no source", ingress.Source{})

	if _, err := q.Submit(ctx, queue.WorkItem{ID: "wi-src", Tenant: tn, Box: "shop", Ingress: withSrc.ID, Task: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Submit(ctx, queue.WorkItem{ID: "wi-nosrc", Tenant: tn, Box: "shop", Ingress: withoutSrc.ID, Task: "t"}); err != nil {
		t.Fatal(err)
	}

	rows, err := r.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]QueueEntry{}
	for _, e := range rows {
		byID[e.ID] = e
	}

	got := byID["wi-src"]
	if got.SourceSubject != "alice" || got.SourceMethod != "token" || got.SourceChannel != "route:issues" {
		t.Fatalf("source did not ride the fold: %+v", got)
	}

	nosrc := byID["wi-nosrc"]
	if nosrc.SourceSubject != "" || nosrc.SourceMethod != "" || nosrc.SourceChannel != "" || nosrc.SourceRun != "" {
		t.Fatalf("an origination opened without a source must not read as having one: %+v", nosrc)
	}
}

// TestExceptionCarriesTheSourceOfAFailedJudgement: failedPlan already has the
// ingress in hand — it must carry the source subject onto the exception row
// it renders, staying empty rather than fabricating one when the ingress
// predates sources.
func TestExceptionCarriesTheSourceOfAFailedJudgement(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()

	withSrc := openWithSource(t, r, "issue-3", "failed with a source", ingress.Source{
		Subject: "bob", Method: "oidc", Channel: "route:issues",
	})
	withoutSrc := openWithSource(t, r, "issue-4", "failed without a source", ingress.Source{})

	plans := &plan.Store{Log: r.Store.Log(), Actor: "d"}
	if err := plans.Fail(ctx, withSrc, "no decomposer answered"); err != nil {
		t.Fatal(err)
	}
	if err := plans.Fail(ctx, withoutSrc, "no decomposer answered"); err != nil {
		t.Fatal(err)
	}

	rows, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Exception{}
	for _, e := range rows {
		byID[e.ID] = e
	}

	got, ok := byID[withSrc.ID]
	if !ok {
		t.Fatalf("the sourced failed judgement did not list as an exception: %+v", rows)
	}
	if got.SourceSubject != "bob" {
		t.Fatalf("the exception row did not carry the source subject: %+v", got)
	}

	nosrc, ok := byID[withoutSrc.ID]
	if !ok {
		t.Fatalf("the unsourced failed judgement did not list as an exception: %+v", rows)
	}
	if nosrc.SourceSubject != "" {
		t.Fatalf("a failed judgement opened without a source must not read as having one: %+v", nosrc)
	}
	if nosrc.Requester != "chris" {
		t.Fatalf("the requester fallback the room needs is missing: %+v", nosrc)
	}
}
