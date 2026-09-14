package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestSelfDeployRollbackAppearsAsAnException: internal/daemon's rollback
// resolver writes exactly two records — the standing control/self-deploy
// chain, and an index/exceptions record naming the same head — with no web
// change needed to show it. This is the room's half: the fold has to
// recognize the shape and render it like any other exception.
func TestSelfDeployRollbackAppearsAsAnException(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	head := "deadbeefcafefeed"

	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: selfDeployRolledBackKind,
		Subject: head, Actor: "boxd", Body: []byte("self-deploy: box shop good -> deadbeefcafefeed"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: selfDeploySubjectPrefix + head, Actor: "boxd",
		Body: []byte("self-deploy: box shop good -> deadbeefcafefeed"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exceptions: %+v", got)
	}
	ex := got[0]
	if want := selfDeploySubjectPrefix + shortCommit(head); ex.ID != want {
		t.Fatalf("id = %q, want %q", ex.ID, want)
	}
	if ex.Severity != queue.SeverityHigh {
		t.Fatalf("severity = %q, want high", ex.Severity)
	}
	if ex.Note != "self-deploy: box shop good -> deadbeefcafefeed" {
		t.Fatalf("note = %q, want the marker's reason", ex.Note)
	}
	if ex.State != queue.StateParked {
		t.Fatalf("state = %q, want parked — the room's existing shape", ex.State)
	}
}

// TestSelfDeployExceptionCountsItsOwnEscalation: recordEscalation keys the
// escalation chain by the exception's ID — the short-head synthetic id, not
// the full head — so the fold's own lookup into that chain must use the same
// key. It used to look up the full head, which never matched a record
// recordEscalation ever wrote, so Escalations stayed 0 forever and
// Escalator.Sweep's nth reset to 1 on every pass: an unbounded,
// unsilenceable re-escalation.
func TestSelfDeployExceptionCountsItsOwnEscalation(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	head := "deadbeefcafefeed"

	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: selfDeployRolledBackKind,
		Subject: head, Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: selfDeploySubjectPrefix + head, Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}
	id := selfDeploySubjectPrefix + shortCommit(head)
	body, err := json.Marshal(Notice{Tenant: tn, Item: id, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: escalationScope, Kind: escalationKind,
		Subject: id, Actor: "boxd", Body: body,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exceptions: %+v", got)
	}
	if got[0].Escalations != 1 {
		t.Fatalf("escalations = %d, want 1 — recordEscalation's own key must count", got[0].Escalations)
	}
}

// TestSelfDeployExceptionDropsOutAfterALaterSuccessfulDeploy: the fold reads
// the standing chain for the head, not the index record's own body, so a
// later record for the same subject settles it — same as a work item that
// moved past parked.
func TestSelfDeployExceptionDropsOutAfterALaterSuccessfulDeploy(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	head := "deadbeefcafefeed"

	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: selfDeployRolledBackKind,
		Subject: head, Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: selfDeploySubjectPrefix + head, Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}
	// A later record for the same head — e.g. an operator forced a retry
	// that this time came up.
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: "self-deployed",
		Subject: head, Actor: "boxd", Body: []byte("self-deploy: box shop good -> deadbeefcafefeed"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("exceptions: %+v, want none — the rollback was superseded", got)
	}
}

// selfDeployPassRecord appends a self-deploy-pass record for box the way
// internal/daemon's recordSelfDeployPass does — decision and reason only,
// the fields deployStarvedException actually reads.
func selfDeployPassRecord(t *testing.T, r *Room, box, decision, reason string) {
	t.Helper()
	body, err := json.Marshal(selfDeployPassBody{Decision: decision, Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: selfDeployPassKind,
		Subject: "pass/" + box, Actor: "boxd", Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDeployStarvedAppearsAsAnException: internal/daemon's self-deploy chore
// writes exactly two records on the Nth consecutive abandon — the pass
// record it already writes for every abandon, and an index/exceptions
// record naming the box. This is the room's half: the fold has to recognize
// the shape (issue #254) and render it like any other exception.
func TestDeployStarvedAppearsAsAnException(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	box := "shop"

	selfDeployPassRecord(t, r, box, selfDeployPassAbandoned, "drain window elapsed with work in flight")
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: deployStarvedSubjectPrefix + box, Actor: "boxd",
		Body: []byte("self-deploy on box shop has abandoned 3 consecutive passes"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exceptions: %+v", got)
	}
	ex := got[0]
	if want := deployStarvedSubjectPrefix + box; ex.ID != want {
		t.Fatalf("id = %q, want %q", ex.ID, want)
	}
	if ex.Severity != queue.SeverityHigh {
		t.Fatalf("severity = %q, want high", ex.Severity)
	}
	if ex.Note != "drain window elapsed with work in flight" {
		t.Fatalf("note = %q, want the latest pass record's own reason", ex.Note)
	}
	if ex.State != queue.StateParked {
		t.Fatalf("state = %q, want parked — the room's existing shape", ex.State)
	}
}

// TestDeployStarvedExceptionDropsOutAfterALaterPassSupersedesIt: the fold
// reads the latest control/self-deploy pass record for the box, not the
// index record's own body, so a later pass that is no longer abandoned
// (a fence deploy, a refusal, or simply finding the box current) settles it
// — even though the index/exceptions record itself is never revisited.
func TestDeployStarvedExceptionDropsOutAfterALaterPassSupersedesIt(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	box := "shop"

	selfDeployPassRecord(t, r, box, selfDeployPassAbandoned, "drain window elapsed with work in flight")
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: deployStarvedSubjectPrefix + box, Actor: "boxd",
		Body: []byte("self-deploy on box shop has abandoned 3 consecutive passes"),
	}); err != nil {
		t.Fatal(err)
	}
	// A later pass for the same box — e.g. the fence deployed on the pass
	// after the Nth abandon.
	selfDeployPassRecord(t, r, box, "deploying", "self-deploy: box shop good -> deadbeef")

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("exceptions: %+v, want none — a later pass superseded the run of abandons", got)
	}
}
